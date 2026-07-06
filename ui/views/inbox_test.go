package views

import (
	"strings"
	"testing"

	"github.com/bubblmail/bubblmail/config"
)

func TestInboxEmptyStates(t *testing.T) {
	th := &config.Theme{}
	mk := func() *InboxView {
		v := NewInboxView(th)
		v.SetSize(60, 8)
		return v
	}

	cases := []struct {
		name    string
		setup   func(*InboxView)
		want    string
		notWant string
	}{
		{"loading", func(v *InboxView) { v.SetLoading() }, "Syncing", "Inbox zero"},
		{"error", func(v *InboxView) { v.SetError("connection refused") }, "connection refused", "Inbox zero"},
		{"error-retry-hint", func(v *InboxView) { v.SetError("boom") }, "ctrl+r", ""},
		{"inbox-zero", func(v *InboxView) {}, "Inbox zero", "Syncing"},
		{"filtered", func(v *InboxView) { v.SetFiltered(true) }, "No unread", "Inbox zero"},
	}
	for _, c := range cases {
		v := mk()
		c.setup(v)
		out := v.View()
		if !strings.Contains(out, c.want) {
			t.Errorf("%s: expected output to contain %q", c.name, c.want)
		}
		if c.notWant != "" && strings.Contains(out, c.notWant) {
			t.Errorf("%s: output should not contain %q", c.name, c.notWant)
		}
	}
}

func TestInboxSelectionCount(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 20)
	v.SetThreads(makeThreads(10))

	if v.SelectionCount() != 0 {
		t.Errorf("no multi-selection should report 0, got %d", v.SelectionCount())
	}
	v.ShiftMoveDown() // anchor at 0, cursor at 1 → 2 selected
	if v.SelectionCount() != 2 {
		t.Errorf("after one shift-move: count=%d, want 2", v.SelectionCount())
	}
	v.ShiftMoveDown()
	if v.SelectionCount() != 3 {
		t.Errorf("after two shift-moves: count=%d, want 3", v.SelectionCount())
	}
	v.ClearSelection()
	if v.SelectionCount() != 0 {
		t.Errorf("after ClearSelection: count=%d, want 0", v.SelectionCount())
	}
}

func TestInboxSetThreadsResetsState(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 8)
	v.SetError("boom")
	v.SetThreads(nil) // an empty successful load should clear the error
	if strings.Contains(v.View(), "boom") {
		t.Error("SetThreads should reset error state")
	}
	if !strings.Contains(v.View(), "Inbox zero") {
		t.Error("after SetThreads with no threads, expected inbox-zero copy")
	}
}
