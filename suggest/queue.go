package suggest

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/data"
)

type ResultMsg struct {
	MessageID int64
	Account   string
	Event     *data.SuggestedEvent
	Err       error
}

type Queue struct {
	client  *Client
	store   *cache.Store
	results chan ResultMsg

	jobs  chan *data.Message
	seen  map[int64]struct{}
	seenM sync.Mutex
	once  sync.Once

	queued   int64
	inFlight int64
}

func NewQueue(client *Client, store *cache.Store, results chan ResultMsg) *Queue {
	q := &Queue{
		client:  client,
		store:   store,
		results: results,
		jobs:    make(chan *data.Message, 256),
		seen:    make(map[int64]struct{}),
	}
	q.once.Do(func() { go q.worker() })
	return q
}

func (q *Queue) Submit(msg *data.Message) {
	if msg == nil || msg.ID == 0 {
		return
	}
	if !q.markSeen(msg.ID) {
		return
	}
	select {
	case q.jobs <- msg:
		atomic.AddInt64(&q.queued, 1)
	default:
		q.clearSeen(msg.ID)
	}
}

func (q *Queue) Stats() (queued, inFlight int) {
	return int(atomic.LoadInt64(&q.queued)), int(atomic.LoadInt64(&q.inFlight))
}

func (q *Queue) worker() {
	for msg := range q.jobs {
		atomic.AddInt64(&q.queued, -1)
		atomic.AddInt64(&q.inFlight, 1)
		q.process(msg)
		atomic.AddInt64(&q.inFlight, -1)
	}
}

func (q *Queue) process(msg *data.Message) {
	defer q.clearSeen(msg.ID)
	if existing, err := q.store.GetSuggestedEvent(msg.ID); err == nil && existing != nil && existing.GenerationOK {
		return
	}
	bodyText, bodyHTML, err := q.store.GetBody(msg.ID)
	if err != nil {
		q.emit(ResultMsg{MessageID: msg.ID, Account: msg.AccountName, Err: err})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	ev, err := q.client.Extract(ctx, msg, bodyText, bodyHTML)
	if ev != nil {
		_ = q.store.UpsertSuggestedEvent(ev)
	}
	q.emit(ResultMsg{MessageID: msg.ID, Account: msg.AccountName, Event: ev, Err: err})
}

func (q *Queue) emit(msg ResultMsg) {
	select {
	case q.results <- msg:
	default:
	}
}

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
