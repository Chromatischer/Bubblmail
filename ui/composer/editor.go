// Package composer provides the email composition overlay.
package composer

import (
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/charmbracelet/lipgloss"
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
	cursor int // rune position within Value
}

const (
	labelWidth = 10
	fieldSep   = " │ "
)

// insert inserts s at cursor position.
func (f *Field) insert(s string) {
	byteIdx := f.cursorByteIndex()
	f.Value = f.Value[:byteIdx] + s + f.Value[byteIdx:]
	f.cursor += len([]rune(s))
}

// backspace deletes the character before cursor.
func (f *Field) backspace() {
	if f.cursor <= 0 {
		return
	}
	runes := []rune(f.Value)
	if f.cursor > len(runes) {
		f.cursor = len(runes)
	}
	newRunes := append(runes[:f.cursor-1], runes[f.cursor:]...)
	f.Value = string(newRunes)
	f.cursor--
}

func (f *Field) cursorByteIndex() int {
	if f.cursor <= 0 {
		return 0
	}
	runes := []rune(f.Value)
	if f.cursor >= len(runes) {
		return len(f.Value)
	}
	return len(string(runes[:f.cursor]))
}

func (f *Field) runeIndexFromByte(byteIdx int) int {
	if byteIdx <= 0 {
		return 0
	}
	if byteIdx >= len(f.Value) {
		return len([]rune(f.Value))
	}
	return len([]rune(f.Value[:byteIdx]))
}

func (f *Field) lineAndColumn() (int, int, []string) {
	lines := strings.Split(f.Value, "\n")
	byteIdx := f.cursorByteIndex()
	before := f.Value[:byteIdx]
	line := strings.Count(before, "\n")
	lastIdx := strings.LastIndex(before, "\n")
	col := 0
	if lastIdx < 0 {
		col = len([]rune(before))
	} else {
		col = len([]rune(before[lastIdx+1:]))
	}
	return line, col, lines
}

func (f *Field) cursorLeft() {
	runes := []rune(f.Value)
	if f.cursor > len(runes) {
		f.cursor = len(runes)
	}
	if f.cursor > 0 {
		f.cursor--
	}
}

func (f *Field) cursorRight() {
	runes := []rune(f.Value)
	if f.cursor > len(runes) {
		f.cursor = len(runes)
	}
	if f.cursor < len(runes) {
		f.cursor++
	}
}

func (f *Field) cursorHome() {
	if f.Kind == FieldTextArea {
		// Move to start of current line
		byteIdx := f.cursorByteIndex()
		idx := strings.LastIndex(f.Value[:byteIdx], "\n")
		if idx < 0 {
			f.cursor = 0
		} else {
			f.cursor = f.runeIndexFromByte(idx + 1)
		}
	} else {
		f.cursor = 0
	}
}

func (f *Field) cursorEnd() {
	if f.Kind == FieldTextArea {
		byteIdx := f.cursorByteIndex()
		idx := strings.Index(f.Value[byteIdx:], "\n")
		if idx < 0 {
			f.cursor = len([]rune(f.Value))
		} else {
			f.cursor = f.runeIndexFromByte(byteIdx + idx)
		}
	} else {
		f.cursor = len([]rune(f.Value))
	}
}

func (f *Field) cursorMoveLines(delta int) {
	if f.Kind != FieldTextArea || delta == 0 {
		return
	}
	line, col, lines := f.lineAndColumn()
	target := line + delta
	if target < 0 {
		target = 0
	}
	if target >= len(lines) {
		target = len(lines) - 1
	}
	if target < 0 {
		f.cursor = 0
		return
	}
	lineRunes := []rune(lines[target])
	if col > len(lineRunes) {
		col = len(lineRunes)
	}
	before := 0
	for i := 0; i < target; i++ {
		before += len([]rune(lines[i])) + 1
	}
	f.cursor = before + col
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

	// Label and separator change colour when the field is focused.
	var labelFg lipgloss.Color
	var sepStr string
	var sepFg lipgloss.Color
	if ef.active {
		labelFg = theme.Accent
		sepStr = " ▸ "
		sepFg = theme.Accent
	} else {
		labelFg = theme.TextMuted
		sepStr = " │ "
		sepFg = theme.Border
	}

	labelStyle := lipgloss.NewStyle().
		Foreground(labelFg).
		Background(theme.Surface).
		Width(labelWidth).
		Align(lipgloss.Right)

	sep := lipgloss.NewStyle().Foreground(sepFg).Background(theme.Surface).Render(sepStr)

	// Use plain char counts — never measure ANSI strings for layout.
	// sepStr is always exactly 3 chars (" ▸ " or " │ ").
	valueW := ef.width - labelWidth - 3
	if valueW < 1 {
		valueW = 1
	}

	label := labelStyle.Render(ef.field.Label + ":")

	value := ef.field.Value
	if ef.field.Kind == FieldTextArea {
		parts := strings.Split(value, "\n")
		if len(parts) > 0 {
			value = parts[0]
		} else {
			value = ""
		}
	}

	var display string
	if ef.active {
		runes := []rune(value)
		cursor := ef.field.cursor
		if cursor > len(runes) {
			cursor = len(runes)
		}
		if cursor < 0 {
			cursor = 0
		}
		display = lipgloss.NewStyle().
			Foreground(theme.Text).
			Background(theme.SurfaceAlt).
			Width(valueW).
			Render(string(runes[:cursor]) + "▌" + string(runes[cursor:]))
	} else if value == "" {
		display = lipgloss.NewStyle().
			Foreground(theme.TextFaint).
			Background(theme.Surface).
			Width(valueW).
			Render(fieldPlaceholder(ef.field.Label))
	} else {
		display = lipgloss.NewStyle().
			Foreground(theme.Text).
			Background(theme.Surface).
			Width(valueW).
			Render(value)
	}

	return label + sep + display
}

// fieldPlaceholder returns a subtle hint shown in empty unfocused fields.
func fieldPlaceholder(label string) string {
	switch label {
	case "To":
		return "recipient address..."
	case "CC":
		return "optional..."
	case "Subject":
		return "subject line..."
	default:
		return ""
	}
}
