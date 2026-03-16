package components

import (
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

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
	Icon        string // Optional icon prefix
	Active      bool   // If true, shows a cursor block
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
	if t.Active {
		surf = theme.SurfaceAlt // Highlight background slightly when active if needed, but keeping Surface to match original behavior.
		// Actually, let's keep it Surface to match Bubblmail's original overlays exactly
		surf = theme.Surface
	}

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

	inputAreaW := width - iconCellW
	if inputAreaW < 1 {
		inputAreaW = 1
	}

	var textCell string
	if t.Value == "" {
		textCell = lipgloss.NewStyle().
			Background(surf).
			Foreground(theme.TextFaint).
			Width(inputAreaW).
			Render(t.Placeholder)
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

	return iconCell + textCell
}
