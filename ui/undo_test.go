package ui

import (
	"strings"
	"testing"
)

func newTestApp() *App {
	return &App{
		statusbar:     NewStatusBar(NewStyles(testTheme())),
		activeAccount: "acct",
		activeFolder:  "INBOX",
	}
}

func TestRegisterUndo(t *testing.T) {
	a := newTestApp()

	// A normal move with a real destination UID is undoable.
	suffix := a.registerUndo("acct", "INBOX", "Archive", 42, "Archive")
	if !strings.Contains(suffix, "ctrl+z") {
		t.Errorf("expected undo suffix, got %q", suffix)
	}
	if a.lastUndo == nil || a.lastUndo.dest != "Archive" || a.lastUndo.destUID != 42 || a.lastUndo.src != "INBOX" {
		t.Fatalf("lastUndo not recorded correctly: %+v", a.lastUndo)
	}

	// Without a destination UID (server lacks UIDPLUS) there is no undo.
	suffix = a.registerUndo("acct", "INBOX", "Archive", 0, "Archive")
	if suffix != "" {
		t.Errorf("expected no undo suffix when destUID is 0, got %q", suffix)
	}
	if a.lastUndo != nil {
		t.Error("lastUndo should be cleared when undo is unavailable")
	}
}
