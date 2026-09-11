package components

import (
	"strconv"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// Badge tone selects the colour role a pill speaks in.
const (
	ToneAccent  = iota // the app's one interactive colour — tags, active filters
	ToneNeutral        // raised-surface pill — counts, passive metadata
	ToneUnread         // the unread colour — new-mail counts
	ToneDanger         // destructive or failed state
)

// Pill renders a small rounded-feeling label chip. Chips are how the app shows
// any short piece of categorical metadata — tags, filter chips, match kinds —
// so they must all come from here rather than from ad-hoc padded styles.
func Pill(theme *config.Theme, text string, tone int, bg lipgloss.Color) string {
	fg, fill := theme.Background, theme.Accent
	switch tone {
	case ToneNeutral:
		fg, fill = theme.TextMuted, theme.SurfaceAlt
	case ToneUnread:
		fg, fill = theme.Background, theme.Unread
	case ToneDanger:
		fg, fill = theme.Background, theme.Error
	}
	_ = bg // pills always paint their own fill; bg is accepted for symmetry
	return lipgloss.NewStyle().
		Foreground(fg).
		Background(fill).
		Padding(0, 1).
		Render(util.SingleLine(text))
}

// Count renders an unread-style count marker: a dot plus the number, in the
// unread colour. Unlike Pill it paints no fill, so it can sit on a selected
// row without fighting the selection background.
//
// bg is the row background it sits on; fg overrides the colour (pass "" to use
// the theme's unread colour).
func Count(theme *config.Theme, n int, bg, fg lipgloss.Color) string {
	if n <= 0 {
		return ""
	}
	if fg == "" {
		fg = theme.Unread
	}
	st := lipgloss.NewStyle().Foreground(fg).Bold(true)
	if bg != "" {
		st = st.Background(bg)
	}
	return st.Render("●" + strconv.Itoa(n))
}

// CountWidth returns the column cost of Count(n) without rendering it, for
// layouts that must reserve the slot before they know the row's colours.
func CountWidth(n int) int {
	if n <= 0 {
		return 0
	}
	return 1 + len(strconv.Itoa(n))
}
