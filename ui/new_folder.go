package ui

import (
	"strings"
	"unicode/utf8"

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
	theme := o.styles.Theme

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
	titleLine := lipgloss.NewStyle().
		Background(theme.Surface).
		Foreground(theme.Accent).
		Bold(true).
		Width(innerW).
		Render(icons.FolderNew + " New Folder")

	// ── Input row — mirrors search.go ────────────────────────────────────────
	// iconCellW=2: icon(1) + 1 implicit pad from Width(2).
	const iconCellW = 2
	iconCell := lipgloss.NewStyle().
		Width(iconCellW).
		Background(theme.Surface).
		Foreground(theme.Accent).
		Render(icons.FolderNew)

	inputAreaW := innerW - iconCellW
	var inputCell string
	if o.input == "" {
		inputCell = lipgloss.NewStyle().
			Width(inputAreaW).
			Background(theme.Surface).
			Foreground(theme.TextFaint).
			Render("folder name…")
	} else {
		displayInput := util.TruncateText(util.SingleLine(o.input), inputAreaW-1) + "▌"
		inputCell = lipgloss.NewStyle().
			Width(inputAreaW).
			Background(theme.Surface).
			Foreground(theme.Text).
			Bold(true).
			Render(displayInput)
	}
	inputLine := iconCell + inputCell

	// ── Divider ──────────────────────────────────────────────────────────────
	sep := lipgloss.NewStyle().
		Foreground(theme.Border).
		Background(theme.Surface).
		Width(innerW).
		Render(strings.Repeat("─", innerW))

	// ── Footer: hints or error ────────────────────────────────────────────────
	// Error state: replace hints with icon + message.
	// Normal state: statusbar-style hint pills — icon desc (key).
	var footerLine string
	if o.errMsg != "" {
		footerLine = lipgloss.NewStyle().
			Background(theme.Surface).
			Foreground(theme.Error).
			Width(innerW).
			Render(icons.Error + " " + o.errMsg)
	} else {
		iconStyle := lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.Surface)
		descStyle := lipgloss.NewStyle().Foreground(theme.TextMuted).Background(theme.Surface)
		keyStyle := lipgloss.NewStyle().Foreground(theme.TextFaint).Background(theme.Surface)
		sp := lipgloss.NewStyle().Background(theme.Surface).Render(" ")
		gap := lipgloss.NewStyle().Background(theme.Surface).Render("  ")

		type hint struct{ icon, key, desc string }
		hints := []hint{
			{icons.Check, "enter", "create"},
			{icons.Close, "esc", "cancel"},
		}
		var parts []string
		for _, h := range hints {
			parts = append(parts,
				iconStyle.Render(h.icon)+sp+descStyle.Render(h.desc)+sp+keyStyle.Render("("+h.key+")"),
			)
		}
		hintStr := strings.Join(parts, gap)
		footerLine = lipgloss.NewStyle().
			Background(theme.Surface).
			Width(innerW).
			Render(hintStr)
	}

	blank := lipgloss.NewStyle().Background(theme.Surface).Width(innerW).Render("")

	content := strings.Join([]string{titleLine, blank, inputLine, sep, footerLine}, "\n")

	box := lipgloss.NewStyle().
		Width(cw).
		Background(theme.Surface).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(o.width, o.height, lipgloss.Center, lipgloss.Center, box)
}
