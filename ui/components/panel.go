package components

import (
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// EmptyState renders the centred "there is nothing here, and that is fine"
// block. A pane that renders blank reads as a bug; this reads as a state.
func EmptyState(theme *config.Theme, w, h int, icon, msg, hint string) string {
	var rows []string
	if icon != "" {
		rows = append(rows, lipgloss.NewStyle().
			Foreground(theme.Border).
			Render(icon))
		rows = append(rows, "")
	}
	rows = append(rows, lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Render(util.SingleLine(msg)))
	if hint != "" {
		rows = append(rows, lipgloss.NewStyle().
			Foreground(theme.TextFaint).
			Render(util.SingleLine(hint)))
	}
	body := lipgloss.JoinVertical(lipgloss.Center, rows...)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, body)
}

// FieldRow renders one `label  value` line with a right-aligned label column.
// The reader header, the suggested-event card and the composer all describe
// records this way, so they share the alignment.
//
// Set bold to emphasise the value (used for the one field that is the record's
// headline). suffix is appended unstyled after the value — pass "" for none.
func FieldRow(theme *config.Theme, label string, labelW int, value string, valueW int, bold bool, suffix string, bg lipgloss.Color) string {
	labelSt := lipgloss.NewStyle().
		Foreground(theme.TextFaint).
		Width(labelW).
		Align(lipgloss.Right)
	valueSt := lipgloss.NewStyle().Foreground(theme.Text)
	gapSt := lipgloss.NewStyle()
	if bg != "" {
		labelSt = labelSt.Background(bg)
		valueSt = valueSt.Background(bg)
		gapSt = gapSt.Background(bg)
	}
	if bold {
		valueSt = valueSt.Bold(true)
	}
	if valueW > 0 {
		value = util.TruncateText(util.SingleLine(value), valueW)
	} else {
		value = util.SingleLine(value)
	}
	return labelSt.Render(util.TruncateText(label, labelW)) +
		gapSt.Render("  ") +
		valueSt.Render(value) +
		suffix
}

// Card wraps content in a left accent bar plus indent. It marks a block as a
// distinct object inside a flowing document (a suggested event inside a mail
// body, a quoted block, an attachment list) without spending two rows and two
// columns on a full border.
//
//	▎ Suggested event
//	▎ Brunch at The Brunch Club
func Card(theme *config.Theme, lines []string, width int, accent lipgloss.Color) []string {
	if accent == "" {
		accent = theme.Accent
	}
	bar := lipgloss.NewStyle().Foreground(accent).Render(gutterBar + " ")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, bar+l)
	}
	return out
}

// Pad returns rows padded out to exactly h entries with blank lines of the
// given width, or truncated if there are too many. Callers that hand a
// variable-length body to a fixed-height pane must go through this, or the
// row arithmetic in the parent layout silently drifts.
func Pad(rows []string, h, w int, bg lipgloss.Color) []string {
	if h <= 0 {
		return nil
	}
	if len(rows) > h {
		return rows[:h]
	}
	blank := Fill(w, bg)
	out := make([]string, h)
	copy(out, rows)
	for i := len(rows); i < h; i++ {
		out[i] = blank
	}
	return out
}

// JoinRows joins rendered rows with newlines. Trivial, but it keeps view code
// free of strings import churn as blocks move between files.
func JoinRows(rows []string) string { return strings.Join(rows, "\n") }
