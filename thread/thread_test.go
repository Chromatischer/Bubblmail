package thread

import (
	"testing"
	"time"

	"github.com/bubblmail/bubblmail/data"
)

func TestBuildThreads_Empty(t *testing.T) {
	threads := BuildThreads(nil)
	if len(threads) != 0 {
		t.Errorf("expected 0 threads, got %d", len(threads))
	}
}

func TestBuildThreads_SingleMessage(t *testing.T) {
	msgs := []*data.Message{
		{
			MessageID: "<a@example.com>",
			Subject:   "Hello",
			Date:      time.Now(),
		},
	}
	threads := BuildThreads(msgs)
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	if threads[0].Subject != "Hello" {
		t.Errorf("expected subject 'Hello', got %q", threads[0].Subject)
	}
}

func TestBuildThreads_Reply(t *testing.T) {
	now := time.Now()
	msgs := []*data.Message{
		{
			MessageID: "<a@example.com>",
			Subject:   "Original",
			Date:      now.Add(-time.Hour),
		},
		{
			MessageID: "<b@example.com>",
			InReplyTo: "<a@example.com>",
			Subject:   "Re: Original",
			Date:      now,
		},
	}
	threads := BuildThreads(msgs)
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	if len(threads[0].Messages) != 2 {
		t.Errorf("expected 2 messages in thread, got %d", len(threads[0].Messages))
	}
}

func TestBuildThreads_MultipleUnrelated(t *testing.T) {
	now := time.Now()
	msgs := []*data.Message{
		{MessageID: "<a@example.com>", Subject: "First", Date: now.Add(-time.Hour)},
		{MessageID: "<b@example.com>", Subject: "Second", Date: now},
	}
	threads := BuildThreads(msgs)
	if len(threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(threads))
	}
}

func TestBuildThreads_ReferencesChain(t *testing.T) {
	now := time.Now()
	msgs := []*data.Message{
		{
			MessageID:  "<a@example.com>",
			Subject:    "Start",
			Date:       now.Add(-2 * time.Hour),
		},
		{
			MessageID:  "<b@example.com>",
			InReplyTo:  "<a@example.com>",
			References: []string{"<a@example.com>"},
			Subject:    "Re: Start",
			Date:       now.Add(-time.Hour),
		},
		{
			MessageID:  "<c@example.com>",
			InReplyTo:  "<b@example.com>",
			References: []string{"<a@example.com>", "<b@example.com>"},
			Subject:    "Re: Start",
			Date:       now,
		},
	}
	threads := BuildThreads(msgs)
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	if len(threads[0].Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(threads[0].Messages))
	}
	// Sorted chronologically
	if threads[0].Messages[0].MessageID != "<a@example.com>" {
		t.Errorf("first message should be <a@example.com>, got %s", threads[0].Messages[0].MessageID)
	}
}

func TestBuildThreads_SortedByLastDate(t *testing.T) {
	now := time.Now()
	msgs := []*data.Message{
		{MessageID: "<old@example.com>", Subject: "Old", Date: now.Add(-24 * time.Hour)},
		{MessageID: "<new@example.com>", Subject: "New", Date: now},
	}
	threads := BuildThreads(msgs)
	if len(threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(threads))
	}
	// Newest first
	if threads[0].Subject != "New" {
		t.Errorf("expected newest thread first, got %q", threads[0].Subject)
	}
}

func TestNormalizeSubject(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"Re: Hello", "Hello"},
		{"RE: Re: Hello", "Hello"},
		{"Fwd: Hello", "Hello"},
		{"FW: Hello", "Hello"},
		{"Hello", "Hello"},
		{"Re[2]: Hello", "Hello"},
	}
	for _, c := range cases {
		got := normalizeSubject(c.input)
		if got != c.expected {
			t.Errorf("normalizeSubject(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestBuildThreads_HasUnread(t *testing.T) {
	msgs := []*data.Message{
		{
			MessageID: "<a@example.com>",
			Subject:   "Test",
			Date:      time.Now(),
			// No Seen flag → unread
		},
	}
	threads := BuildThreads(msgs)
	if !threads[0].HasUnread {
		t.Error("expected HasUnread=true for message without \\Seen flag")
	}
}

func TestBuildThreads_AllRead(t *testing.T) {
	msgs := []*data.Message{
		{
			MessageID: "<a@example.com>",
			Subject:   "Test",
			Date:      time.Now(),
			Flags:     []data.Flag{data.FlagSeen},
		},
	}
	threads := BuildThreads(msgs)
	if threads[0].HasUnread {
		t.Error("expected HasUnread=false for message with \\Seen flag")
	}
}
