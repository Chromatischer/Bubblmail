package classify

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/data"
)

// ResultMsg is sent on the result channel after a message is classified.
type ResultMsg struct {
	MessageID int64
	Account   string
	Category  string
	Err       error
}

// Queue is a background worker that classifies messages using the LLM client
// and caches results in SQLite. It is deliberately single-threaded to avoid
// hammering the API (classification is cheap; throughput is not critical).
type Queue struct {
	client  *Client
	store   *cache.Store
	results chan ResultMsg

	jobs  chan classifyJob
	once  sync.Once
	seen  map[int64]struct{}
	seenM sync.Mutex

	queued   int64
	inFlight int64
}

type classifyJob struct {
	msg *data.Message
}

// NewQueue creates and starts a classification queue. results is a channel
// the caller reads to learn about completed classifications. It must not be nil.
func NewQueue(client *Client, store *cache.Store, results chan ResultMsg) *Queue {
	q := &Queue{
		client:  client,
		store:   store,
		results: results,
		jobs:    make(chan classifyJob, 512),
		seen:    make(map[int64]struct{}),
	}
	q.start()
	return q
}

func (q *Queue) start() {
	q.once.Do(func() {
		go q.worker()
	})
}

// Submit enqueues messages for classification. Messages that are already
// queued or have a cached result are silently skipped.
func (q *Queue) Submit(msgs []*data.Message) {
	for _, m := range msgs {
		if m == nil || m.ID == 0 {
			continue
		}
		if !q.markSeen(m.ID) {
			continue // already queued
		}
		select {
		case q.jobs <- classifyJob{msg: m}:
			atomic.AddInt64(&q.queued, 1)
		default:
			// queue full — clear seen so it will be retried next sync
			q.clearSeen(m.ID)
		}
	}
}

// Stats returns current queue statistics.
func (q *Queue) Stats() (queued, inFlight int) {
	return int(atomic.LoadInt64(&q.queued)), int(atomic.LoadInt64(&q.inFlight))
}

func (q *Queue) worker() {
	for job := range q.jobs {
		atomic.AddInt64(&q.queued, -1)
		atomic.AddInt64(&q.inFlight, 1)
		q.process(job.msg)
		atomic.AddInt64(&q.inFlight, -1)
	}
}

func (q *Queue) process(msg *data.Message) {
	defer q.clearSeen(msg.ID)

	// Check if already classified in cache
	existing, err := q.store.GetCategory(msg.ID)
	if err == nil && existing != "" {
		return // already done
	}

	from := ""
	if len(msg.From) > 0 {
		from = msg.From[0].Address
		if msg.From[0].Name != "" {
			from = msg.From[0].Name + " <" + from + ">"
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	category, confidence, err := q.client.Classify(ctx, from, msg.Subject, msg.Snippet)
	if err != nil {
		select {
		case q.results <- ResultMsg{MessageID: msg.ID, Account: msg.AccountName, Err: err}:
		default:
		}
		return
	}

	// Always cache — even NONE — so we don't retry on every sync
	storeCategory := category
	if storeCategory == CategoryNone {
		storeCategory = CategoryNone // explicit sentinel
	}
	_ = q.store.UpsertCategory(msg.ID, storeCategory, q.client.model, confidence)

	if category != CategoryNone {
		select {
		case q.results <- ResultMsg{
			MessageID: msg.ID,
			Account:   msg.AccountName,
			Category:  category,
		}:
		default:
		}
	}
}

// markSeen tries to mark the message as seen. Returns true if it was newly
// added (i.e. safe to enqueue), false if it was already present.
func (q *Queue) markSeen(id int64) bool {
	q.seenM.Lock()
	defer q.seenM.Unlock()
	if _, ok := q.seen[id]; ok {
		return false
	}
	q.seen[id] = struct{}{}
	return true
}

func (q *Queue) clearSeen(id int64) {
	q.seenM.Lock()
	defer q.seenM.Unlock()
	delete(q.seen, id)
}
