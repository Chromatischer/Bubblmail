package ui

import (
	"strings"

	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// NewFolderOverlay is a floating dialog for creating a new IMAP folder.
type NewFolderOverlay struct {
	styles    *Styles
	width     int
	height    int
	active    bool
	input     string // current text typed by the user
	result    string // non-empty when user confirmed; "" if cancelled
	submitted bool   // true once result has been set (even if empty string)
	errMsg    string // inline validation error
}

// NewNewFolderOverlay creates a new folder creation overlay.
func NewNewFolderOverlay(styles *Styles) *NewFolderOverlay {
	return &NewFolderOverlay{styles: styles}
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
			runes := []rune(o.input)
			o.input = string(runes[:len(runes)-1])
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
	theme := o.styles.Theme

	const minBoxW = 40
	const labelW = 7 // "Name  │"

	// Box content width: enough for the input field and hints.
	boxW := minBoxW
	if o.width > minBoxW+10 {
		boxW = minBoxW + (o.width-minBoxW-10)/2
	}
	if boxW > 60 {
		boxW = 60
	}

	// Input display: cursor appended to current text, truncated to fit.
	inputAvail := boxW - labelW - 4 // subtract label + padding
	if inputAvail < 8 {
		inputAvail = 8
	}
	displayInput := o.input
	// Show only the tail of the input if it's too long.
	runes := []rune(displayInput)
	if util.VisibleWidth(displayInput) > inputAvail-1 {
		for util.VisibleWidth(string(runes)) > inputAvail-1 && len(runes) > 0 {
			runes = runes[1:]
		}
		displayInput = string(runes)
	}
	cursor := lipgloss.NewStyle().
		Foreground(theme.Background).
		Background(theme.Accent).
		Render(" ")
	inputStr := displayInput + cursor
	inputField := lipgloss.NewStyle().
		Foreground(theme.Text).
		Background(theme.SurfaceAlt).
		Width(inputAvail).
		Padding(0, 1).
		Render(inputStr)

	labelStyle := lipgloss.NewStyle().Foreground(theme.TextMuted)
	sepStyle := lipgloss.NewStyle().Foreground(theme.Border)

	nameRow := labelStyle.Render("Name  ") +
		sepStyle.Render("│") + " " +
		inputField

	// Hint / error row
	var hintLine string
	if o.errMsg != "" {
		hintLine = lipgloss.NewStyle().
			Foreground(theme.Error).
			Render(icons.Error + " " + o.errMsg)
	} else {
		hintLine = lipgloss.NewStyle().
			Foreground(theme.TextFaint).
			Render("enter to create  ·  esc to cancel")
	}

	// Title
	titleLine := lipgloss.NewStyle().
		Foreground(theme.Accent).
		Bold(true).
		Render(icons.FolderNew + " New Folder")

	content := strings.Join([]string{
		titleLine,
		"",
		nameRow,
		"",
		hintLine,
	}, "\n")

	box := lipgloss.NewStyle().
		Width(boxW).
		Background(theme.Surface).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(o.width, o.height, lipgloss.Center, lipgloss.Center, box)
}
