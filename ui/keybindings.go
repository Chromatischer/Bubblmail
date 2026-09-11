package ui

import "github.com/bubblmail/bubblmail/ui/components"

// Key bindings live as string literals in the App.Update switch. A parallel
// table of Key* constants used to sit here, referenced by nothing, and it drifted:
// it claimed `\` focused the sidebar and `t` opened a tag picker, neither of
// which the app has ever handled. HelpGroups below is now the only description
// of the key map, and it is the one the user actually sees.

// HelpGroup is a titled block of bindings in the help overlay.
//
// Bindings are grouped by what the user is trying to do rather than listed in
// one flat column: a thirty-row list is a list you scan for a key you already
// know, and no help for the key you do not.
type HelpGroup struct {
	Title string
	Keys  []components.Hint
}

// HelpGroups returns every binding, grouped for display.
func HelpGroups() []HelpGroup {
	return []HelpGroup{
		{"Move", []components.Hint{
			{Key: "j / k", Desc: "Up / down"},
			{Key: "ctrl+d / u", Desc: "Page down / up"},
			{Key: "g / G", Desc: "Top / bottom"},
			{Key: "shift+↑/↓", Desc: "Extend selection"},
			{Key: "↵", Desc: "Open thread"},
			{Key: "esc / h", Desc: "Back"},
		}},
		{"Reply", []components.Hint{
			{Key: "r", Desc: "Reply"},
			{Key: "R", Desc: "Reply all"},
			{Key: "f", Desc: "Forward"},
			{Key: "c", Desc: "Compose new"},
		}},
		{"Organise", []components.Hint{
			{Key: "s", Desc: "Toggle star"},
			{Key: "m", Desc: "Toggle read"},
			{Key: "e", Desc: "Archive"},
			{Key: "v", Desc: "Move to folder"},
			{Key: "d", Desc: "Delete to trash"},
			{Key: "← / →", Desc: "Quick actions"},
		}},
		{"Find", []components.Hint{
			{Key: "/", Desc: "Search this account"},
			{Key: "ctrl+f", Desc: "Search on the server"},
			{Key: "i", Desc: "Jump to inbox"},
		}},
		{"Layout", []components.Hint{
			{Key: "b", Desc: "Toggle sidebar"},
			{Key: "tab", Desc: "Focus sidebar"},
			{Key: "n", Desc: "New folder (in sidebar)"},
			{Key: "z", Desc: "Fold quoted text"},
			{Key: "tab / shift+tab", Desc: "Cycle attachments"},
		}},
		{"Folder tree", []components.Hint{
			{Key: "space", Desc: "Fold / unfold"},
			{Key: "l / →", Desc: "Unfold"},
			{Key: "h / ←", Desc: "Fold, then go up"},
			{Key: "H / L", Desc: "Fold / unfold all"},
			{Key: "click ▸", Desc: "Fold / unfold"},
		}},
		{"App", []components.Hint{
			{Key: "ctrl+r", Desc: "Force sync"},
			{Key: "?", Desc: "Close this help"},
			{Key: "q q", Desc: "Quit"},
		}},
	}
}
