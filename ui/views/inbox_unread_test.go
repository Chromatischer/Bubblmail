package views

import (
	"strings"
	"testing"
	"time"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestUnreadSubjectBrighter verifies unread threads render their subject in the
// bright Text color while read threads stay muted, so the inbox is scannable by
// subject. Color output is forced on since tests run without a TTY.
func TestUnreadSubjectBrighter(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	th := &config.Theme{
		Text: "#FFFFFF", TextMuted: "#888888", Surface: "#222233",
		Unread: "#88AAFF", Starred: "#FFCC44", Selected: "#4444AA",
		Background: "#111111", Accent: "#5577FF",
	}
	v := NewInboxView(th)
	v.SetSize(60, 8)

	mk := func(from, subj string, unread bool) *data.Thread {
		m := &data.Message{Subject: subj, Date: time.Now(), From: []data.Address{{Name: from}}}
		if !unread {
			m.Flags = []data.Flag{data.FlagSeen}
		}
		return &data.Thread{Subject: subj, Messages: []*data.Message{m}, LastDate: time.Now(), HasUnread: unread}
	}

	// The cursor sits on index 0 (rendered selected/inverted), so put the
	// threads under test at index 1 where the read-state styling applies.
	v.SetThreads([]*data.Thread{mk("Cursor", "selected row", false), mk("Alice", "Unread subject here", true)})
	unreadOut := v.View()
	v.SetThreads([]*data.Thread{mk("Cursor", "selected row", false), mk("Bob", "Read subject here", false)})
	readOut := v.View()

	if !strings.Contains(unreadOut, "255;255;255") {
		t.Error("unread subject should render in the bright Text color (#FFFFFF)")
	}
	if !strings.Contains(readOut, "136;136;136") {
		t.Error("read subject should render in the muted color (#888888)")
	}
}
