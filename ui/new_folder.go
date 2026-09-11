package ui

import (
	"github.com/bubblmail/bubblmail/config"
	"strings"
	"unicode/utf8"

	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// NewFolderOverlay is a floating dialog for creating a new IMAP folder.
type NewFolderOverlay struct {
	theme     *config.Theme
	width     int
	height    int
	active    bool
	input     string // current text typed by the user
	result    string // non-empty when user confirmed; "" if cancelled
	submitted bool   // true once result has been set (even if empty string)
	errMsg    string // inline validation error
}

// NewNewFolderOverlay creates a new folder creation overlay.
func NewNewFolderOverlay(theme *config.Theme) *NewFolderOverlay {
	return &NewFolderOverlay{theme: theme}
}

// SetSize sets the overlay dimensions (full content area, not the modal box).
func (o *NewFolderOverlay) SetSize(w, h int) {
	o.width = w
	o.height = h
}

// Open opens the overlay, resetting all state.
func (o *NewFolderOverlay) Open() {
	o.input = ""
	o.result = ""
	o.submitted = false
	o.errMsg = ""
	o.active = true
}

// Close hides the overlay.
func (o *NewFolderOverlay) Close() {
	o.active = false
	o.input = ""
	o.result = ""
	o.submitted = false
	o.errMsg = ""
}

// IsActive returns true when the overlay is open.
func (o *NewFolderOverlay) IsActive() bool {
	return o.active
}

// Result returns the folder name the user confirmed, or "" if cancelled.
// Only valid after IsActive() returns false.
func (o *NewFolderOverlay) Result() string {
	return o.result
}

// ClearResult resets the result after it has been consumed.
func (o *NewFolderOverlay) ClearResult() {
	o.result = ""
	o.submitted = false
}

// WasSubmitted reports whether the user confirmed (not cancelled).
func (o *NewFolderOverlay) WasSubmitted() bool {
	return o.submitted
}

// HandleKey processes a key press. Returns closed=true when the overlay
// should be dismissed; check WasSubmitted() + Result() for the outcome.
func (o *NewFolderOverlay) HandleKey(key string) (closed bool) {
	switch key {
	case "esc":
		o.submitted = false
		o.result = ""
		return true

	case "enter":
		name := strings.TrimSpace(o.input)
		if name == "" {
			o.errMsg = "Folder name cannot be empty"
			return false
		}
		o.result = name
		o.submitted = true
		return true

	case "backspace", "ctrl+h":
		if len(o.input) > 0 {
			_, size := utf8.DecodeLastRuneInString(o.input)
			o.input = o.input[:len(o.input)-size]
		}
		o.errMsg = ""

	case "ctrl+u":
		o.input = ""
		o.errMsg = ""

	default:
		// Accept printable characters (single rune keys)
		if len([]rune(key)) == 1 {
			r := []rune(key)[0]
			if r >= 32 { // skip control characters
				o.input += string(r)
				o.errMsg = ""
			}
		}
	}
	return false
}

// View renders the new-folder dialog centered on the content area.
func (o *NewFolderOverlay) View() string {
	theme := o.theme

	const minBoxW = 42
	cw := minBoxW
	if o.width > minBoxW+10 {
		cw = minBoxW + (o.width-minBoxW-10)/3
	}
	if cw > 60 {
		cw = 60
	}

	// innerW: content area inside Padding(1, 2) = cw − 4.
	const hPad = 2
	innerW := cw - 2*hPad

	// ── Title ────────────────────────────────────────────────────────────────
	titleLine := components.PaneTitle(theme, icons.FolderNew+" New folder", innerW, theme.Surface)

	// ── Input row — mirrors search.go ────────────────────────────────────────
	ti := components.NewTextInput(theme)
	ti.Placeholder = "folder name…"
	ti.Value = o.input
	ti.Icon = icons.FolderNew
	ti.Active = true

	inputLine := ti.Render(innerW)

	// ── Divider ──────────────────────────────────────────────────────────────
	sep := components.Divider(theme, innerW)

	// ── Footer: hints or error ────────────────────────────────────────────────
	// Error state: replace hints with icon + message.
	// Normal state: statusbar-style hint pills — icon desc (key).
	var footerLine string
	if o.errMsg != "" {
		footerLine = lipgloss.NewStyle().
			Background(theme.Surface).
			Foreground(theme.Error).
			Width(innerW).
			Render(util.TruncateText(icons.Error+" "+o.errMsg, innerW))
	} else {
		bar, w, _ := components.HintBar(theme, []components.Hint{
			{Icon: icons.Check, Key: "enter", Desc: "create"},
			{Icon: icons.Close, Key: "esc", Desc: "cancel"},
		}, innerW, 0, theme.Surface)
		footerLine = bar + components.Fill(innerW-w, theme.Surface)
	}

	blank := components.Fill(innerW, theme.Surface)

	content := strings.Join([]string{titleLine, blank, inputLine, sep, footerLine}, "\n")

	return components.ModalBox(theme, content, cw, 0, o.width, o.height)
}
