package ui

import (
	"strings"
	"testing"

	"github.com/bubblmail/bubblmail/data"
)

func newTestApp() *App {
	return &App{
		statusbar:     NewStatusBar(NewStyles(testTheme())),
		activeAccount: "acct",
		activeFolder:  "INBOX",
	}
}

func TestFindArchiveFolder(t *testing.T) {
	tests := []struct {
		name    string
		folders []*data.Folder
		want    string
	}{
		{
			name: "special use archive wins",
			folders: []*data.Folder{
				{Name: "Archives", DisplayName: "Archives"},
				{Name: "[Gmail]/Archive", DisplayName: "Archive", Attributes: []string{`\Archive`}},
			},
			want: "[Gmail]/Archive",
		},
		{
			name: "common archive name",
			folders: []*data.Folder{
				{Name: "Inbox", DisplayName: "Inbox"},
				{Name: "Archive", DisplayName: "Archive"},
			},
			want: "Archive",
		},
		{
			name: "gmail all mail fallback",
			folders: []*data.Folder{
				{Name: "[Gmail]/All Mail", DisplayName: "All Mail", Attributes: []string{`\All`}},
			},
			want: "[Gmail]/All Mail",
		},
		{
			name: "missing archive",
			folders: []*data.Folder{
				{Name: "INBOX", DisplayName: "INBOX"},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestApp()
			a.accounts = []*data.Account{{Name: "acct", Folders: tt.folders}}

			if got := a.findArchiveFolder("acct"); got != tt.want {
				t.Fatalf("findArchiveFolder() = %q, want %q", got, tt.want)
			}
		})
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
