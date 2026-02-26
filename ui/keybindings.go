package ui

// Key constants for all actions.
const (
	KeyUp        = "up"
	KeyDown      = "down"
	KeyLeft      = "left"
	KeyRight     = "right"
	KeyJ         = "j"
	KeyK         = "k"
	KeyH         = "h"
	KeyPageDown  = "ctrl+d"
	KeyPageUp    = "ctrl+u"
	KeyTop       = "g"
	KeyBottom    = "G"
	KeyEnter     = "enter"
	KeyEsc       = "esc"
	KeyBack      = "h"
	KeyCompose   = "c"
	KeyReply     = "r"
	KeyReplyAll  = "R"
	KeyForward   = "f"
	KeyDelete    = "d"
	KeyArchive   = "e"
	KeyStar      = "s"
	KeyMarkRead  = "m"
	KeyTag       = "t"
	KeySearch    = "/"
	KeyIMAPSearch = "ctrl+f"
	KeyInbox     = "i"
	KeySidebar   = "b"
	KeyNextAcct  = "tab"
	KeyPrevAcct  = "shift+tab"
	KeySync      = "ctrl+r"
	KeyHelp      = "?"
	KeyQuit      = "q"
	KeyCtrlC     = "ctrl+c"
)

// HelpLine describes a single key binding for the help overlay.
type HelpLine struct {
	Key  string
	Desc string
}

// AllHelpLines returns all key bindings for the help overlay.
func AllHelpLines() []HelpLine {
	return []HelpLine{
		{"j/k, ↑/↓", "Navigate list"},
		{"ctrl+d/u", "Page down/up"},
		{"g/G", "Top/bottom"},
		{"", ""},
		{"Enter", "Open thread/message"},
		{"Esc/h/←", "Back to inbox"},
		{"", ""},
		{"r/R", "Reply / Reply All"},
		{"f", "Forward"},
		{"c", "Compose new"},
		{"d", "Delete (move to Trash)"},
		{"e", "Archive"},
		{"s", "Toggle starred"},
		{"m", "Toggle read/unread"},
		{"t", "Tag picker"},
		{"", ""},
		{"/", "Local search"},
		{"ctrl+f", "IMAP server search"},
		{"i", "Jump to INBOX"},
		{"b", "Toggle sidebar"},
		{"Tab/Shift+Tab", "Next/prev account"},
		{"ctrl+r", "Force sync"},
		{"?", "Help"},
		{"q", "Quit"},
	}
}
