package ui

import "github.com/bubblmail/bubblmail/ui/icons"

// Key constants for all actions.
const (
	KeyUp           = "up"
	KeyDown         = "down"
	KeyLeft         = "left"
	KeyRight        = "right"
	KeyJ            = "j"
	KeyK            = "k"
	KeyH            = "h"
	KeyPageDown     = "ctrl+d"
	KeyPageUp       = "ctrl+u"
	KeyTop          = "g"
	KeyBottom       = "G"
	KeyEnter        = "enter"
	KeyEsc          = "esc"
	KeyBack         = "h"
	KeyCompose      = "c"
	KeyReply        = "r"
	KeyReplyAll     = "R"
	KeyForward      = "f"
	KeyDelete       = "d"
	KeyMove         = "v"
	KeyArchive      = "e"
	KeyStar         = "s"
	KeyMarkRead     = "m"
	KeyTag          = "t"
	KeySearch       = "/"
	KeyIMAPSearch   = "ctrl+f"
	KeyInbox        = "i"
	KeySidebar      = "b"
	KeySidebarFocus = "\\"
	KeyNextAcct     = "tab"
	KeyPrevAcct     = "shift+tab"
	KeySync         = "ctrl+r"
	KeyHelp         = "?"
	KeyQuit         = "q"
	KeyCtrlC        = "ctrl+c"
)

// HelpLine describes a single key binding for the help overlay.
type HelpLine struct {
	Key  string
	Desc string
}

// AllHelpLines returns all key bindings for the help overlay.
func AllHelpLines() []HelpLine {
	return []HelpLine{
		{"j/k, " + icons.ArrowUp + "/" + icons.ArrowDown, "Navigate list"},
		{"ctrl+d/u", "Page down/up"},
		{"g/G", "Top/bottom"},
		{"", ""},
		{"Enter", "Open thread/message"},
		{"Esc/q/h/" + icons.ArrowLeft, "Back to inbox"},
		{"", ""},
		{icons.Reply + "/" + icons.ReplyAll + " r/R", "Reply / Reply All"},
		{icons.Forward + " f", "Forward"},
		{icons.Compose + " c", "Compose new"},
		{icons.Trash + " d", "Delete (move to Trash)"},
		{icons.FolderOpen + " v", "Move to folder"},
		{icons.Archive + " e", "Archive"},
		{icons.Star + " s", "Toggle starred"},
		{icons.Read + " m", "Toggle read/unread"},
		{icons.Tag + " t", "Tag picker"},
		{"", ""},
		{icons.Search + " /", "Local search"},
		{"ctrl+f", "IMAP server search"},
		{icons.Inbox + " i", "Jump to INBOX"},
		{icons.FolderTree + " b", "Toggle sidebar"},
		{"\\", "Focus sidebar"},
		{"Tab/Shift+Tab", "Next/prev account"},
		{icons.Refresh + " ctrl+r", "Force sync"},
		{icons.Help + " ?", "Help"},
		{icons.Quit + " q q", "Quit"},
	}
}
