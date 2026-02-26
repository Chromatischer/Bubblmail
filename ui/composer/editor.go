// Package composer provides the email composition overlay.
package composer

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/bubblmail/bubblmail/config"
)

// Field types for the composer form.
type FieldKind int

const (
	FieldText     FieldKind = iota // single-line
	FieldTextArea                  // multi-line body
)

// Field is a single form input.
type Field struct {
	Label  string
	Kind   FieldKind
	Value  string
	cursor int // byte position within Value
}

// insert inserts s at cursor position.
func (f *Field) insert(s string) {
	f.Value = f.Value[:f.cursor] + s + f.Value[f.cursor:]
	f.cursor += len(s)
}

// backspace deletes the character before cursor.
func (f *Field) backspace() {
	if f.cursor == 0 {
		return
	}
	runes := []rune(f.Value)
	pos := f.runePos()
	if pos == 0 {
		return
	}
	newRunes := append(runes[:pos-1], runes[pos:]...)
	f.Value = string(newRunes)
	// recalculate cursor
	f.cursor = len(string(newRunes[:pos-1]))
}

func (f *Field) runePos() int {
	return len([]rune(f.Value[:f.cursor]))
}

func (f *Field) cursorLeft() {
	runes := []rune(f.Value)
	pos := f.runePos()
	if pos > 0 {
		f.cursor = len(string(runes[:pos-1]))
	}
}

func (f *Field) cursorRight() {
	runes := []rune(f.Value)
	pos := f.runePos()
	if pos < len(runes) {
		f.cursor = len(string(runes[:pos+1]))
	}
}

func (f *Field) cursorHome() {
	if f.Kind == FieldTextArea {
		// Move to start of current line
		idx := strings.LastIndex(f.Value[:f.cursor], "\n")
		if idx < 0 {
			f.cursor = 0
		} else {
			f.cursor = idx + 1
		}
	} else {
		f.cursor = 0
	}
}

func (f *Field) cursorEnd() {
	if f.Kind == FieldTextArea {
		idx := strings.Index(f.Value[f.cursor:], "\n")
		if idx < 0 {
			f.cursor = len(f.Value)
		} else {
			f.cursor = f.cursor + idx
		}
	} else {
		f.cursor = len(f.Value)
	}
}

// EditorField is the visual component for rendering a single field.
type EditorField struct {
	theme  *config.Theme
	field  *Field
	active bool
	width  int
}

// NewEditorField creates a field renderer.
func NewEditorField(theme *config.Theme, field *Field) *EditorField {
	return &EditorField{theme: theme, field: field}
}

// SetActive marks the field as focused.
func (ef *EditorField) SetActive(active bool) {
	ef.active = active
}

// SetWidth sets the field width.
func (ef *EditorField) SetWidth(w int) {
	ef.width = w
}

// View renders a single form field row.
func (ef *EditorField) View() string {
	theme := ef.theme
	labelW := 10

	labelStyle := lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Width(labelW).
		Align(lipgloss.Right)

	var valueStyle lipgloss.Style
	if ef.active {
		valueStyle = lipgloss.NewStyle().
			Foreground(theme.Text).
			Background(theme.SurfaceAlt).
			Width(ef.width - labelW - 3)
	} else {
		valueStyle = lipgloss.NewStyle().
			Foreground(theme.Text).
			Background(theme.Surface).
			Width(ef.width - labelW - 3)
	}

	label := labelStyle.Render(ef.field.Label + ":")
	sep := lipgloss.NewStyle().
		Foreground(theme.Border).
		Render(" │ ")

	var display string
	if ef.active {
		// Insert cursor marker
		v := ef.field.Value
		cursor := ef.field.cursor
		if cursor > len(v) {
			cursor = len(v)
		}
		display = valueStyle.Render(v[:cursor] + "▌" + v[cursor:])
	} else {
		if ef.field.Value == "" {
			display = lipgloss.NewStyle().
				Foreground(theme.TextFaint).
				Background(theme.Surface).
				Width(ef.width - labelW - 3).
				Render("(empty)")
		} else {
			display = valueStyle.Render(ef.field.Value)
		}
	}

	return label + sep + display
}
