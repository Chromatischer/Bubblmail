package ui

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/embeddings"
)

type embeddingQueue struct {
	cfg    config.EmbeddingsConfig
	client *embeddings.Client
	store  *cache.Store

	jobs  chan embedJob
	once  sync.Once
	seen  map[int64]string
	seenM sync.Mutex

	queued   int64
	inFlight int64
}

type embedJob struct {
	msg  *data.Message
	body string
}

func newEmbeddingQueue(cfg config.EmbeddingsConfig, client *embeddings.Client, store *cache.Store) *embeddingQueue {
	q := &embeddingQueue{
		cfg:    cfg,
		client: client,
		store:  store,
		jobs:   make(chan embedJob, cfg.BatchSize*4),
		seen:   make(map[int64]string),
	}
	q.start()
	return q
}

func (q *embeddingQueue) start() {
	q.once.Do(func() {
		workers := q.cfg.BatchSize
		if workers <= 0 {
			workers = 4
		}
		if workers > 32 {
			workers = 32
		}
		for i := 0; i < workers; i++ {
			go q.worker()
		}
	})
}

func (q *embeddingQueue) Enqueue(msg *data.Message, body string) {
	if msg == nil {
		return
	}
	content, hash := q.prepareContent(msg, body)
	if content == "" {
		return
	}
	if q.markSeen(msg.ID, hash) {
		return
	}
	job := embedJob{msg: msg, body: content}
	select {
	case q.jobs <- job:
		atomic.AddInt64(&q.queued, 1)
	default:
		// queue full, drop; next body fetch will retry
		q.clearSeen(msg.ID, hash)
	}
}

func (q *embeddingQueue) worker() {
	for job := range q.jobs {
		atomic.AddInt64(&q.queued, -1)
		atomic.AddInt64(&q.inFlight, 1)
		q.embed(job.msg, job.body)
		atomic.AddInt64(&q.inFlight, -1)
	}
}

func (q *embeddingQueue) embed(msg *data.Message, content string) {
	if msg == nil || content == "" {
		return
	}
	hash := embeddings.HashContent(content)
	upToDate, err := q.store.EmbeddingUpToDate(msg.ID, q.cfg.Model, hash)
	if err != nil || upToDate {
		q.clearSeen(msg.ID, hash)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	vecs, err := q.client.EmbedTexts(ctx, []string{content})
	if err != nil || len(vecs) == 0 {
		q.clearSeen(msg.ID, hash)
		return
	}
	vec := vecs[0]
	norm := embeddings.VectorNorm(vec)
	_ = q.store.SaveEmbedding(msg.ID, q.cfg.Model, vec, norm, hash)
	q.clearSeen(msg.ID, hash)
}

func (q *embeddingQueue) prepareContent(msg *data.Message, body string) (string, string) {
	content := strings.TrimSpace(msg.Subject + "\n" + body)
	if content == "" {
		return "", ""
	}
	maxChars := q.cfg.MaxContentChars
	if maxChars <= 0 {
		maxChars = 8000
	}
	if len(content) > maxChars {
		content = content[:maxChars]
	}
	return content, embeddings.HashContent(content)
}

func (q *embeddingQueue) markSeen(msgID int64, hash string) bool {
	q.seenM.Lock()
	defer q.seenM.Unlock()
	if prev, ok := q.seen[msgID]; ok && prev == hash {
		return true
	}
	q.seen[msgID] = hash
	return false
}

func (q *embeddingQueue) clearSeen(msgID int64, hash string) {
	q.seenM.Lock()
	defer q.seenM.Unlock()
	if prev, ok := q.seen[msgID]; ok && prev == hash {
		delete(q.seen, msgID)
	}
}

type embeddingStats struct {
	Queued   int
	InFlight int
}

func (q *embeddingQueue) Stats() embeddingStats {
	if q == nil {
		return embeddingStats{}
	}
	return embeddingStats{
		Queued:   int(atomic.LoadInt64(&q.queued)),
		InFlight: int(atomic.LoadInt64(&q.inFlight)),
	}
}

// BackfillAccount queues embeddings for cached bodies in an account.
func (q *embeddingQueue) BackfillAccount(account string, force bool) (int, error) {
	if q == nil {
		return 0, nil
	}
	msgs, bodies, err := q.store.ListBodiesForEmbedding(account, q.cfg.Model, force)
	if err != nil {
		return 0, err
	}
	queued := 0
	for i, msg := range msgs {
		if msg == nil {
			continue
		}
		body := bodies[i]
		content, hash := q.prepareContent(msg, body)
		if content == "" {
			continue
		}
		if q.markSeen(msg.ID, hash) {
			continue
		}
		select {
		case q.jobs <- embedJob{msg: msg, body: content}:
			atomic.AddInt64(&q.queued, 1)
			queued++
		default:
			q.clearSeen(msg.ID, hash)
			return queued, nil
		}
	}
	return queued, nil
}
