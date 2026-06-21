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
