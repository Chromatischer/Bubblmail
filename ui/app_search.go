package ui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/bubblmail/bubblmail/embeddings"
	imaplib "github.com/bubblmail/bubblmail/imap"
	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) doLocalSearch(q string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := a.store.SearchLocalWithFilters(q)
		if err != nil {
			return imaplib.SearchResultMsg{Err: err}
		}
		items := make([]embeddings.TopKItem, 0, len(msgs))
		for i, msg := range msgs {
			if msg == nil {
				continue
			}
			var score float32
			if len(msgs) > 1 {
				score = 1 - float32(i)/float32(len(msgs)-1)
			} else {
				score = 1
			}
			items = append(items, embeddings.TopKItem{Message: msg, Score: score})
		}
		if a.searchState != nil {
			a.searchState.semItems = items
		}
		return imaplib.SearchResultMsg{Messages: msgs}
	}
}

func (a *App) doStreamingSearch(q string, seq int) tea.Cmd {
	return func() tea.Msg {
		query := strings.TrimSpace(q)
		if query == "" {
			return embeddings.StreamSearchMsg{Seq: seq, Loading: false}
		}

		semItems := make([]embeddings.TopKItem, 0)
		semMsgs, semErr := a.store.SearchLocalWithFilters(query)
		if semErr == nil {
			semItems = make([]embeddings.TopKItem, 0, len(semMsgs))
			for i, msg := range semMsgs {
				if msg == nil {
					continue
				}
				var score float32
				if len(semMsgs) > 1 {
					score = 1 - float32(i)/float32(len(semMsgs)-1)
				} else {
					score = 1
				}
				semItems = append(semItems, embeddings.TopKItem{Message: msg, Score: score})
			}
		}

		if a.embClient == nil {
			if semErr != nil {
				return embeddings.StreamSearchMsg{Seq: seq, Err: semErr}
			}
			return embeddings.StreamSearchMsg{Seq: seq, Results: mergeSearchHits(semItems, nil), Loading: false}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		vecs, err := a.embClient.EmbedTexts(ctx, []string{query})
		if err != nil || len(vecs) == 0 {
			if semErr != nil {
				return embeddings.StreamSearchMsg{Seq: seq, Err: err}
			}
			return embeddings.StreamSearchMsg{Seq: seq, Results: mergeSearchHits(semItems, nil), Loading: false}
		}
		queryVec := vecs[0]
		queryNorm := embeddings.VectorNorm(queryVec)

		maxCandidates := a.cfg.Embeddings.MaxCandidates
		if maxCandidates <= 0 {
			maxCandidates = 5000
		}
		msgs, vectors, norms, err := a.store.ListEmbeddingCandidates(a.activeAccount, a.cfg.Embeddings.Model, maxCandidates)
		if err != nil || len(msgs) == 0 {
			if semErr != nil {
				return embeddings.StreamSearchMsg{Seq: seq, Err: err}
			}
			return embeddings.StreamSearchMsg{Seq: seq, Results: mergeSearchHits(semItems, nil), Loading: false}
		}
		kSim := a.cfg.Embeddings.TopSimilar
		if kSim <= 0 {
			kSim = 30
		}
		simItems := embeddings.TopK(msgs, vectors, norms, queryVec, queryNorm, kSim)
		results := mergeSearchHits(semItems, simItems)
		return embeddings.StreamSearchMsg{Seq: seq, Results: results, Loading: false}
	}
}

func (a *App) searchStreamTick(seq int) tea.Cmd {
	return func() tea.Msg {
		if a.searchState == nil || a.searchState.seq != seq {
			return nil
		}
		return a.searchStreamNext()()
	}
}

func (a *App) searchStreamNext() tea.Cmd {
	return func() tea.Msg {
		state := a.searchState
		if state == nil {
			return nil
		}
		batch := a.cfg.Embeddings.StreamBatch
		if batch <= 0 {
			batch = 128
		}
		total := len(state.msgs)
		start := state.nextIndex
		if start >= total {
			return embeddings.StreamSearchMsg{Seq: state.seq, Results: state.results, Loading: false}
		}
		end := start + batch
		if end > total {
			end = total
		}
		for i := start; i < end; i++ {
			score := embeddings.CosineSimilarity(state.queryVec, state.queryNorm, state.vectors[i], state.norms[i])
			state.simHeap.Add(state.msgs[i], score)
		}
		sem := state.semItems
		sim := state.simHeap.ItemsSorted()
		state.results = mergeSearchHits(sem, sim)
		state.nextIndex = end
		loading := end < total
		return embeddings.StreamSearchMsg{Seq: state.seq, Results: state.results, Loading: loading}
	}
}

func mergeSearchHits(sem []embeddings.TopKItem, sim []embeddings.TopKItem) []*embeddings.SearchHit {
	denomSem := float32(0)
	if len(sem) > 1 {
		denomSem = float32(len(sem) - 1)
	}
	denomSim := float32(0)
	if len(sim) > 1 {
		denomSim = float32(len(sim) - 1)
	}

	byID := make(map[int64]*embeddings.SearchHit)
	for i, item := range sem {
		if item.Message == nil {
			continue
		}
		score := float32(1)
		if denomSem > 0 {
			score = 1 - float32(i)/denomSem
		}
		entry, ok := byID[item.Message.ID]
		if !ok {
			entry = &embeddings.SearchHit{Message: item.Message}
			byID[item.Message.ID] = entry
		}
		entry.Semantic = true
		entry.Score += score
	}
	for i, item := range sim {
		if item.Message == nil {
			continue
		}
		score := float32(1)
		if denomSim > 0 {
			score = 1 - float32(i)/denomSim
		}
		entry, ok := byID[item.Message.ID]
		if !ok {
			entry = &embeddings.SearchHit{Message: item.Message}
			byID[item.Message.ID] = entry
		}
		entry.Similar = true
		entry.Score += score
	}

	merged := make([]*embeddings.SearchHit, 0, len(byID))
	for _, entry := range byID {
		merged = append(merged, entry)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Score == merged[j].Score {
			return merged[i].Message.Date.After(merged[j].Message.Date)
		}
		return merged[i].Score > merged[j].Score
	})

	withBoth := merged[:0]
	onlySem := make([]*embeddings.SearchHit, 0, len(merged))
	onlySim := make([]*embeddings.SearchHit, 0, len(merged))
	for _, entry := range merged {
		if entry.Semantic && entry.Similar {
			withBoth = append(withBoth, entry)
		} else if entry.Semantic {
			onlySem = append(onlySem, entry)
		} else if entry.Similar {
			onlySim = append(onlySim, entry)
		}
	}
	return append(append(withBoth, onlySem...), onlySim...)
}

func convertSearchResults(results []*embeddings.SearchHit) []*SearchResult {
	converted := make([]*SearchResult, 0, len(results))
	for _, res := range results {
		converted = append(converted, &SearchResult{
			Message:  res.Message,
			Score:    res.Score,
			Semantic: res.Semantic,
			Similar:  res.Similar,
		})
	}
	return converted
}
