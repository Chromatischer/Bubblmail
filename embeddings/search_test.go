package embeddings

import (
	"testing"
	"time"

	"github.com/bubblmail/bubblmail/data"
)

func TestTopKReturnsHighestScoresSorted(t *testing.T) {
	msgs := []*data.Message{
		{ID: 1, Subject: "low"},
		{ID: 2, Subject: "high"},
		{ID: 3, Subject: "middle"},
	}
	vectors := [][]float32{
		{0, 1},
		{1, 0},
		{0.6, 0.8},
	}
	norms := []float32{
		VectorNorm(vectors[0]),
		VectorNorm(vectors[1]),
		VectorNorm(vectors[2]),
	}

	got := TopK(msgs, vectors, norms, []float32{1, 0}, 1, 2)

	if len(got) != 2 {
		t.Fatalf("TopK len = %d, want 2", len(got))
	}
	if got[0].Message.ID != 2 {
		t.Fatalf("top message ID = %d, want 2", got[0].Message.ID)
	}
	if got[0].Score < got[1].Score {
		t.Fatalf("TopK not sorted descending: %#v", got)
	}
	if got[1].Message.ID != 3 {
		t.Fatalf("second message ID = %d, want 3", got[1].Message.ID)
	}
}

func TestMergeHitsCombinesFlagsAndSorts(t *testing.T) {
	now := time.Now()
	old := &data.Message{ID: 1, Subject: "old", Date: now.Add(-time.Hour)}
	newer := &data.Message{ID: 2, Subject: "newer", Date: now}
	semanticOnly := &data.Message{ID: 3, Subject: "semantic", Date: now.Add(-2 * time.Hour)}

	got := MergeHits(
		[]TopKItem{
			{Message: old, Score: 0.7},
			{Message: semanticOnly, Score: 0.9},
		},
		[]TopKItem{
			{Message: old, Score: 0.8},
			{Message: newer, Score: 0.8},
		},
	)

	if len(got) != 3 {
		t.Fatalf("MergeHits len = %d, want 3", len(got))
	}
	if got[0].Message.ID != 3 || !got[0].Semantic || got[0].Similar {
		t.Fatalf("first hit = %+v, want semantic-only highest score", got[0])
	}
	if got[1].Message.ID != 2 {
		t.Fatalf("tie should sort newer message first, got ID %d", got[1].Message.ID)
	}
	if got[2].Message.ID != 1 || !got[2].Semantic || !got[2].Similar || got[2].Score != 0.8 {
		t.Fatalf("merged duplicate hit = %+v, want combined flags and max score", got[2])
	}
}

func TestTopKHeap(t *testing.T) {
	h := NewTopKHeap(2)
	h.Add(&data.Message{ID: 1}, 0.1)
	h.Add(&data.Message{ID: 2}, 0.9)
	h.Add(&data.Message{ID: 3}, 0.5)

	got := h.ItemsSorted()
	if len(got) != 2 {
		t.Fatalf("heap len = %d, want 2", len(got))
	}
	if got[0].Message.ID != 2 || got[1].Message.ID != 3 {
		t.Fatalf("heap items = %#v, want IDs [2, 3]", got)
	}
}
