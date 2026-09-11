package components

import (
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// Hint is one "press this, get that" pair. It is the single unit of key
// affordance in the app: the status bar, the help overlay, the folder browser
// footer and the reader action row all render Hints, so a binding looks
// identical wherever the user meets it.
type Hint struct {
	Icon string // optional glyph, rendered in Accent ahead of the key
	Key  string // display form of the key, e.g. "j/k" or "ctrl+r"
	Desc string // what it does, lowercase, one or two words
	// Action is the tea.KeyMsg string to synthesize when the hint is clicked.
	// Leave empty for informational hints that are not actionable.
	Action string
	// Priority orders graceful degradation: when the bar does not fit, the
	// highest Priority hints are dropped first. 0 = never drop.
	Priority int
}

// HintZone is the clickable screen span of one rendered hint.
type HintZone struct {
	X0, X1 int
	Action string
}

// hintSep is the visual break between hints. Two spaces alone let long bars
// blur together; a faint middot gives the eye a rhythm to scan by.
const hintSep = " · "

// HintBar renders hints left-to-right within a column budget.
//
// If the full set does not fit, hints are dropped by descending Priority until
// it does, and a faint "+N" marker records what was hidden — the bar never
// wraps and never overflows, because either behaviour corrupts the layout row
// count that the parent already committed to.
//
// startX is the absolute screen column of the first rendered cell, used to
// place the returned hit zones. bg may be "" for the terminal default.
func HintBar(theme *config.Theme, hints []Hint, budget, startX int, bg lipgloss.Color) (rendered string, width int, zones []HintZone) {
	if budget <= 0 || len(hints) == 0 {
		return "", 0, nil
	}

	styled := func(st lipgloss.Style) lipgloss.Style {
		if bg != "" {
			return st.Background(bg)
		}
		return st
	}
	iconSt := styled(lipgloss.NewStyle().Foreground(theme.Accent))
	keySt := styled(lipgloss.NewStyle().Foreground(theme.Accent).Bold(true))
	descSt := styled(lipgloss.NewStyle().Foreground(theme.TextMuted))
	sepSt := styled(lipgloss.NewStyle().Foreground(theme.TextFaint))
	moreSt := styled(lipgloss.NewStyle().Foreground(theme.TextFaint))

	// plainWidth is what one hint costs in columns, measured from plain text
	// only — never from the styled string, whose length shifts with the theme.
	plainWidth := func(h Hint) int {
		w := util.VisibleWidth(h.Key) + 1 + util.VisibleWidth(h.Desc)
		if h.Icon != "" {
			w += util.VisibleWidth(h.Icon) + 1
		}
		return w
	}

	sepW := util.VisibleWidth(hintSep)

	// Decide which hints survive the budget. Work on an index set so the
	// surviving hints keep their authored order.
	keep := make([]bool, len(hints))
	total := 0
	for i, h := range hints {
		keep[i] = true
		if i > 0 {
			total += sepW
		}
		total += plainWidth(h)
	}

	// Once anything is dropped the bar owes the reader a "+N" marker, so the
	// budget it must fit inside shrinks by the marker's width.
	const markerReserve = 5 // " · +N"
	dropped := 0
	for total > budget-boolInt(dropped > 0)*markerReserve {
		// Find the surviving hint with the highest Priority (ties: rightmost).
		worst, worstPri := -1, 0
		for i := range hints {
			if !keep[i] || hints[i].Priority == 0 {
				continue
			}
			if hints[i].Priority >= worstPri {
				worst, worstPri = i, hints[i].Priority
			}
		}
		if worst < 0 {
			break // nothing left that may be dropped
		}
		keep[worst] = false
		total -= plainWidth(hints[worst]) + sepW
		dropped++
	}

	var b strings.Builder
	x := startX
	first := true
	for i, h := range hints {
		if !keep[i] {
			continue
		}
		if !first {
			b.WriteString(sepSt.Render(hintSep))
			x += sepW
		}
		first = false

		w := plainWidth(h)
		if h.Icon != "" {
			b.WriteString(iconSt.Render(h.Icon) + styled(lipgloss.NewStyle()).Render(" "))
		}
		b.WriteString(keySt.Render(h.Key))
		b.WriteString(styled(lipgloss.NewStyle()).Render(" "))
		b.WriteString(descSt.Render(h.Desc))

		if h.Action != "" {
			zones = append(zones, HintZone{X0: x, X1: x + w - 1, Action: h.Action})
		}
		x += w
	}

	width = x - startX
	if dropped > 0 {
		marker := hintSep + "+" + itoa(dropped)
		if width+util.VisibleWidth(marker) <= budget {
			b.WriteString(moreSt.Render(marker))
			width += util.VisibleWidth(marker)
		}
	}
	return b.String(), width, zones
}

// boolInt is the missing ternary.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// HintColumns renders hints as an aligned two-column key/description block for
// use inside overlays, where vertical space is cheaper than horizontal.
// keyW is the fixed width of the key column.
func HintColumns(theme *config.Theme, hints []Hint, keyW, descW int, bg lipgloss.Color) []string {
	styled := func(st lipgloss.Style) lipgloss.Style {
		if bg != "" {
			return st.Background(bg)
		}
		return st
	}
	keySt := styled(lipgloss.NewStyle().Foreground(theme.Accent).Bold(true).Width(keyW).Align(lipgloss.Right))
	descSt := styled(lipgloss.NewStyle().Foreground(theme.Text).Width(descW))
	gap := styled(lipgloss.NewStyle()).Render("  ")

	out := make([]string, 0, len(hints))
	for _, h := range hints {
		if h.Key == "" && h.Desc == "" {
			out = append(out, Fill(keyW+2+descW, bg))
			continue
		}
		if h.Key == "" { // section heading row
			out = append(out, SectionLabel(theme, h.Desc, keyW+2+descW, bg))
			continue
		}
		out = append(out,
			keySt.Render(util.TruncateText(h.Key, keyW))+
				gap+
				descSt.Render(util.TruncateText(h.Desc, descW)))
	}
	return out
}

// itoa avoids pulling strconv into this file for a single digit-or-two.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
