package embeddings

import (
	"container/heap"
	"sort"

	"github.com/bubblmail/bubblmail/data"
)

type SearchHit struct {
	Message  *data.Message
	Score    float32
	Semantic bool
	Similar  bool
}

type StreamSearchMsg struct {
	Seq     int
	Results []*SearchHit
	Loading bool
	Err     error
}

type TopKItem struct {
	Message *data.Message
	Score   float32
}

type minHeap []TopKItem

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].Score < h[j].Score }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x interface{}) {
	*h = append(*h, x.(TopKItem))
}
func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

type TopKHeap struct {
	k    int
	heap minHeap
}

func NewTopKHeap(k int) *TopKHeap {
	if k < 0 {
		k = 0
	}
	return &TopKHeap{k: k}
}

func (t *TopKHeap) Add(msg *data.Message, score float32) {
	if t.k == 0 || msg == nil {
		return
	}
	if len(t.heap) < t.k {
		heap.Push(&t.heap, TopKItem{Message: msg, Score: score})
		return
	}
	if len(t.heap) > 0 && score > t.heap[0].Score {
		t.heap[0] = TopKItem{Message: msg, Score: score}
		heap.Fix(&t.heap, 0)
	}
}

func (t *TopKHeap) ItemsSorted() []TopKItem {
	items := make([]TopKItem, len(t.heap))
	copy(items, t.heap)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})
	return items
}

func TopK(msgs []*data.Message, vectors [][]float32, norms []float32, query []float32, queryNorm float32, k int) []TopKItem {
	if k <= 0 || len(msgs) == 0 {
		return nil
	}
	if queryNorm == 0 {
		queryNorm = VectorNorm(query)
	}
	var h minHeap
	for i, msg := range msgs {
		score := CosineSimilarity(query, queryNorm, vectors[i], norms[i])
		if len(h) < k {
			heap.Push(&h, TopKItem{Message: msg, Score: score})
			continue
		}
		if len(h) > 0 && score > h[0].Score {
			h[0] = TopKItem{Message: msg, Score: score}
			heap.Fix(&h, 0)
		}
	}
	items := make([]TopKItem, len(h))
	copy(items, h)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})
	return items
}

func MergeHits(semantic []TopKItem, similar []TopKItem) []*SearchHit {
	byID := make(map[int64]*SearchHit)
	for _, h := range semantic {
		if h.Message == nil {
			continue
		}
		entry, ok := byID[h.Message.ID]
		if !ok {
			entry = &SearchHit{Message: h.Message, Score: h.Score}
			byID[h.Message.ID] = entry
		}
		entry.Semantic = true
		if h.Score > entry.Score {
			entry.Score = h.Score
		}
	}
	for _, h := range similar {
		if h.Message == nil {
			continue
		}
		entry, ok := byID[h.Message.ID]
		if !ok {
			entry = &SearchHit{Message: h.Message, Score: h.Score}
			byID[h.Message.ID] = entry
		}
		entry.Similar = true
		if h.Score > entry.Score {
			entry.Score = h.Score
		}
	}
	result := make([]*SearchHit, 0, len(byID))
	for _, h := range byID {
		result = append(result, h)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			return result[i].Message.Date.After(result[j].Message.Date)
		}
		return result[i].Score > result[j].Score
	})
	return result
}
