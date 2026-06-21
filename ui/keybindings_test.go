package ui

import (
	"strings"
	"testing"
)

func TestFocusSidebar(t *testing.T) {
	styles := NewStyles(testTheme())
	a := &App{
		showSidebar:   true,
		sidebar:       NewSidebar(styles),
		viewID:        ViewInbox,
		activeAccount: "acct",
		activeFolder:  "INBOX",
	}
	a.focusSidebar()
	if !a.sidebarFocused {
		t.Fatal("focusSidebar should set sidebarFocused when the sidebar is shown")
	}

	// No-op when the sidebar is hidden.
	b := &App{showSidebar: false, sidebar: NewSidebar(styles), viewID: ViewInbox}
	b.focusSidebar()
	if b.sidebarFocused {
		t.Fatal("focusSidebar should be a no-op when the sidebar is hidden")
	}
}

// TestHelpHasNoPhantomBindings guards against help drift: the help overlay must
// not advertise actions the app no longer implements.
func TestHelpHasNoPhantomBindings(t *testing.T) {
	for _, line := range AllHelpLines() {
		if strings.Contains(strings.ToLower(line.Desc), "account") {
			t.Errorf("help still references account switching, which is not implemented: %q", line.Desc)
		}
	}
}
