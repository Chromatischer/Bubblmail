package components

import (
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// Divider renders a horizontal rule on the surface fill. It belongs inside a
// modal card, which is opaque; the panes do not use rules at all.
func Divider(theme *config.Theme, width int) string {
	return lipgloss.NewStyle().
		Foreground(theme.Border).
		Background(theme.Surface).
		Width(width).
		Render(strings.Repeat("─", width))
}

// ModalBox wraps content in a rounded-border surface box and centres it over a
// (fullW × fullH) area. Pass boxW=0 or boxH=0 to let lipgloss size that
// dimension from the content.
func ModalBox(theme *config.Theme, content string, boxW, boxH, fullW, fullH int) string {
	style := lipgloss.NewStyle().
		Background(theme.Surface).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		Padding(1, 2)
	if boxW > 0 {
		style = style.Width(boxW)
	}
	if boxH > 0 {
		style = style.Height(boxH)
	}
	box := style.Render(content)
	// The box is filled and the area around it is not: the card floats, and
	// whatever the terminal shows through stays visible around it.
	return lipgloss.Place(fullW, fullH, lipgloss.Center, lipgloss.Center, box)
}

// ScrollList tracks cursor and scroll-offset state for a virtualized list.
type ScrollList struct {
	Cursor int
	Offset int
}

// MoveUp moves the cursor up one row, scrolling the viewport if needed.
func (sl *ScrollList) MoveUp() {
	if sl.Cursor > 0 {
		sl.Cursor--
		if sl.Cursor < sl.Offset {
			sl.Offset--
		}
	}
}

// MoveDown moves the cursor down one row, scrolling the viewport if needed.
// count is the total number of items; visible is the number of visible rows.
func (sl *ScrollList) MoveDown(count, visible int) {
	if sl.Cursor < count-1 {
		sl.Cursor++
		if sl.Cursor >= sl.Offset+visible {
			sl.Offset++
		}
	}
}

// Reset moves cursor and offset back to the top.
func (sl *ScrollList) Reset() {
	sl.Cursor = 0
	sl.Offset = 0
}

// Clamp ensures cursor and offset are valid for a list of the given length.
func (sl *ScrollList) Clamp(count int) {
	if count == 0 {
		sl.Cursor = 0
		sl.Offset = 0
		return
	}
	if sl.Cursor >= count {
		sl.Cursor = count - 1
	}
	if sl.Cursor < 0 {
		sl.Cursor = 0
	}
}

// RenderButton renders a standardized button with uniform padding.
func RenderButton(theme *config.Theme, label string, active bool, danger bool) string {
	style := lipgloss.NewStyle().Padding(0, 1)

	if active {
		style = style.Bold(true).Foreground(theme.Background)
		if danger {
			style = style.Background(theme.Error)
		} else {
			style = style.Background(theme.Accent)
		}
	} else {
		style = style.Foreground(theme.Text).Background(theme.Surface)
	}

	return style.Render(label)
}

// TextInput is a reusable single-line text input component.
type TextInput struct {
	Theme       *config.Theme
	Placeholder string
	Value       string
	Icon        string   // Optional icon prefix
	Chips       []string // Committed filter chips, rendered as pills before Value
	Active      bool     // If true, shows a cursor block
}

// NewTextInput creates a new standardized TextInput.
func NewTextInput(theme *config.Theme) *TextInput {
	return &TextInput{
		Theme: theme,
	}
}

// Render draws the input restricted to the given width.
// Note: width is the total width, including the optional icon.
func (t *TextInput) Render(width int) string {
	theme := t.Theme
	surf := theme.Surface

	var iconCell string
	iconCellW := 0

	if t.Icon != "" {
		iconCellW = util.VisibleWidth(t.Icon) + 2 // " " + icon + padding
		iconCell = lipgloss.NewStyle().
			Background(surf).
			Foreground(theme.Accent).
			Width(iconCellW).
			Render(" " + t.Icon)
	}

	// Committed chips are objects, not text: rendering them as pills is what
	// tells the user backspace will remove a whole filter rather than a letter.
	var chipCell string
	chipCellW := 0
	for _, c := range t.Chips {
		room := width - iconCellW - chipCellW - 8 // keep room for the caret
		if room < 5 {
			break
		}
		label := util.TruncateText(util.SingleLine(c), room-2)
		pill := Pill(theme, label, ToneAccent, surf)
		chipCell += pill + lipgloss.NewStyle().Background(surf).Render(" ")
		chipCellW += util.VisibleWidth(label) + 3
	}

	inputAreaW := width - iconCellW - chipCellW
	if inputAreaW < 1 {
		inputAreaW = 1
	}

	var textCell string
	if t.Value == "" && chipCellW == 0 {
		textCell = lipgloss.NewStyle().
			Background(surf).
			Foreground(theme.TextFaint).
			Width(inputAreaW).
			Render(util.TruncateText(t.Placeholder, inputAreaW))
	} else {
		displayVal := util.SingleLine(t.Value)
		if t.Active {
			// Leave 1 col for the cursor glyph
			displayVal = util.TruncateText(displayVal, inputAreaW-1) + "▌"
			textCell = lipgloss.NewStyle().
				Background(surf).
				Foreground(theme.Text).
				Bold(true).
				Width(inputAreaW).
				Render(displayVal)
		} else {
			displayVal = util.TruncateText(displayVal, inputAreaW)
			textCell = lipgloss.NewStyle().
				Background(surf).
				Foreground(theme.Text).
				Width(inputAreaW).
				Render(displayVal)
		}
	}

	return iconCell + chipCell + textCell
}
