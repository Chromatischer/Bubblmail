// Package thread implements JWZ-style email threading using Union-Find.
package thread

import (
	"sort"
	"strings"

	"github.com/bubblmail/bubblmail/data"
)

// BuildThreads groups messages into threads using References and In-Reply-To headers.
// Returns threads sorted by LastDate descending (most recent first).
func BuildThreads(msgs []*data.Message) []*data.Thread {
	if len(msgs) == 0 {
		return nil
	}

	// Build a map from MessageID to message for quick lookup.
	byID := make(map[string]*data.Message, len(msgs))
	for _, m := range msgs {
		if m.MessageID != "" {
			byID[m.MessageID] = m
		}
	}

	// Union-Find: map MessageID → root MessageID.
	parent := make(map[string]string)

	var find func(string) string
	find = func(id string) string {
		if parent[id] == "" || parent[id] == id {
			return id
		}
		root := find(parent[id])
		parent[id] = root // path compression
		return root
	}

	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			// Make the older message the root (canonical thread parent).
			// If we can't determine, keep ra as root.
			parent[rb] = ra
		}
	}

	// Seed every known MessageID into the forest.
	for _, m := range msgs {
		if m.MessageID != "" && parent[m.MessageID] == "" {
			parent[m.MessageID] = m.MessageID
		}
	}

	// Union messages with their references.
	for _, m := range msgs {
		if m.MessageID == "" {
			continue
		}
		// In-Reply-To links this message to its direct parent.
		if m.InReplyTo != "" {
			if parent[m.InReplyTo] == "" {
				parent[m.InReplyTo] = m.InReplyTo // ghost node
			}
			union(m.InReplyTo, m.MessageID)
		}
		// References chain: each ref is an ancestor.
		for _, ref := range m.References {
			if parent[ref] == "" {
				parent[ref] = ref // ghost node
			}
			union(ref, m.MessageID)
		}
	}

	// Group messages by root node.
	threadMap := make(map[string][]*data.Message)
	for _, m := range msgs {
		root := m.MessageID
		if root == "" {
			root = m.Subject // fallback for messages without MessageID
		} else {
			root = find(root)
		}
		threadMap[root] = append(threadMap[root], m)
	}

	// Build Thread objects.
	threads := make([]*data.Thread, 0, len(threadMap))
	for root, messages := range threadMap {
		// Sort messages within thread chronologically.
		sort.Slice(messages, func(i, j int) bool {
			return messages[i].Date.Before(messages[j].Date)
		})

		t := &data.Thread{
			ID:       root,
			Subject:  normalizeSubject(messages[len(messages)-1].Subject),
			Messages: messages,
		}

		// Compute aggregate state.
		for _, m := range messages {
			if m.Date.After(t.LastDate) {
				t.LastDate = m.Date
			}
			if !m.IsRead() {
				t.HasUnread = true
			}
			if m.IsStarred() {
				t.Starred = true
			}
		}

		// Collect unique tags.
		tagSet := make(map[string]struct{})
		for _, m := range messages {
			for _, tag := range m.Tags {
				tagSet[tag] = struct{}{}
			}
		}
		for tag := range tagSet {
			t.Tags = append(t.Tags, tag)
		}
		sort.Strings(t.Tags)

		threads = append(threads, t)
	}

	// Sort threads by LastDate descending.
	sort.Slice(threads, func(i, j int) bool {
		return threads[i].LastDate.After(threads[j].LastDate)
	})

	// Assign thread IDs back to messages.
	for _, t := range threads {
		for _, m := range t.Messages {
			m.ThreadID = t.ID
		}
	}

	return threads
}

// normalizeSubject strips common reply/forward prefixes.
func normalizeSubject(s string) string {
	s = strings.TrimSpace(s)
	for {
		lower := strings.ToLower(s)
		switch {
		case strings.HasPrefix(lower, "re:"):
			s = strings.TrimSpace(s[3:])
		case strings.HasPrefix(lower, "fwd:"):
			s = strings.TrimSpace(s[4:])
		case strings.HasPrefix(lower, "fw:"):
			s = strings.TrimSpace(s[3:])
		case strings.HasPrefix(lower, "re[") || strings.HasPrefix(lower, "fw["):
			// Re[2]: style
			if i := strings.Index(s, "]:"); i != -1 {
				s = strings.TrimSpace(s[i+2:])
			} else {
				return s
			}
		default:
			return s
		}
	}
}
