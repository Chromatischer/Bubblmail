package components

import (
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// Gutter state constants for GutterCell.
const (
	GutterNone   = 0 // no indicator
	GutterActive = 1 // current / focused row
	GutterMarked = 2 // part of a multi-selection, but not the cursor
	GutterQuiet  = 3 // structural marker (e.g. an unread run) — very dim
)

// Gutter glyphs. A left half-block reads as a bar rather than a character,
// which is what lets the same 1-column marker work in a sidebar, a mail list
// and a card header without looking like content.
const (
	gutterBar     = "▌"
	gutterThinBar = "▏"
)

// GutterCell renders the shared 1-column selection marker. Every list, tree
// and card in the app uses this so "where am I" always looks the same.
// bg is the row background it sits on (pass "" for the terminal default).
func GutterCell(theme *config.Theme, state int, bg lipgloss.Color) string {
	st := lipgloss.NewStyle().Width(1)
	if bg != "" {
		st = st.Background(bg)
	}
	switch state {
	case GutterActive:
		return st.Foreground(theme.Accent).Render(gutterBar)
	case GutterMarked:
		return st.Foreground(theme.AccentSoft).Render(gutterBar)
	case GutterQuiet:
		return st.Foreground(theme.Border).Render(gutterThinBar)
	default:
		return st.Render(" ")
	}
}

// ChromeStyle returns the base style for a chrome region — the header, the
// sidebar and the status bar all sit on the same fill so they read as one
// frame around the content area.
func ChromeStyle(theme *config.Theme) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(theme.Text)
	if theme.Chrome != "" {
		s = s.Background(theme.Chrome)
	}
	return s
}

// SectionLabel renders a quiet heading that groups the rows under it:
//
//	ACCOUNTS
//
// It carries no trailing rule on purpose. An earlier version filled the rest of
// the row with ─, which turned every heading in the app into a horizontal line;
// a screen with four of them read as a form, not as a list. Grouping is the
// job of the label plus the blank row above it, and nothing else.
func SectionLabel(theme *config.Theme, label string, width int, bg lipgloss.Color) string {
	return TintedLabel(theme, label, width, bg, theme.TextFaint)
}

// TintedLabel is SectionLabel in a caller-chosen colour. The reader uses it to
// give each message in a thread its sender's own hue, which is how a long
// thread becomes scannable: the eye follows the colour, not the name.
func TintedLabel(theme *config.Theme, label string, width int, bg, fg lipgloss.Color) string {
	base := lipgloss.NewStyle().Foreground(fg).Bold(true)
	if bg != "" {
		base = base.Background(bg)
	}
	text := " " + strings.ToUpper(util.SingleLine(label))
	if width <= 0 {
		return base.Render(text)
	}
	return base.Width(width).Render(util.TruncateText(text, width))
}

// PaneTitle renders the name of an overlay, dialog or pane: accent, bold, and
// in the case its author wrote it. A title names one thing, so it does not get
// the uppercase-and-rule treatment that a list heading gets — that distinction
// is what keeps a dialog from looking like a section of a list.
func PaneTitle(theme *config.Theme, label string, width int, bg lipgloss.Color) string {
	st := lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)
	if bg != "" {
		st = st.Background(bg)
	}
	text := " " + util.SingleLine(label)
	if width <= 0 {
		return st.Render(text)
	}
	return st.Width(width).Render(util.TruncateText(text, width))
}

// PaneStyle returns the base style for the content area — the plane the mail
// list, the reader and the folder browser are drawn on. It is painted
// explicitly so the boundary against a chrome region is a deliberate step in
// colour rather than whatever the user's terminal background happens to be.
func PaneStyle(theme *config.Theme) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(theme.Text)
	if theme.Background != "" {
		s = s.Background(theme.Background)
	}
	return s
}

// Rule renders a horizontal divider with an optional inline label:
//
//	──── Label ───────────────────────────────
//
// Pass an empty label for a plain rule. bg may be "" for the default fill.
func Rule(theme *config.Theme, label string, width int, bg lipgloss.Color) string {
	if width <= 0 {
		return ""
	}
	line := lipgloss.NewStyle().Foreground(theme.Border)
	if bg != "" {
		line = line.Background(bg)
	}
	if label == "" {
		return line.Render(strings.Repeat("─", width))
	}

	txt := lipgloss.NewStyle().Foreground(theme.TextMuted)
	if bg != "" {
		txt = txt.Background(bg)
	}
	const lead = 4
	labelStr := " " + util.SingleLine(label) + " "
	labelStr = util.TruncateText(labelStr, width-lead-1)
	tail := width - lead - util.VisibleWidth(labelStr)
	if tail < 0 {
		tail = 0
	}
	return line.Render(strings.Repeat("─", lead)) +
		txt.Render(labelStr) +
		line.Render(strings.Repeat("─", tail))
}

// Fill renders width columns of bg. Pass "" — as the list and reader panes do —
// to emit plain spaces that let the terminal show through. Pass a colour only
// inside something that is meant to be opaque, such as a modal card or the
// cursor row.
func Fill(width int, bg lipgloss.Color) string {
	if width <= 0 {
		return ""
	}
	s := lipgloss.NewStyle().Width(width)
	if bg != "" {
		s = s.Background(bg)
	}
	return s.Render("")
}
