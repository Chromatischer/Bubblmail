package ui

import (
	"github.com/bubblmail/bubblmail/config"
	"strings"

	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/charmbracelet/lipgloss"
)

// HelpOverlay renders the keyboard reference as a two-column card.
type HelpOverlay struct {
	theme *config.Theme
}

// NewHelpOverlay creates a new help overlay.
func NewHelpOverlay(theme *config.Theme) *HelpOverlay {
	return &HelpOverlay{theme: theme}
}

// Column geometry. Two columns keep the card inside a 24-row terminal, which a
// single flat list of every binding does not.
const (
	helpKeyW    = 15
	helpDescW   = 24
	helpColW    = helpKeyW + 2 + helpDescW
	helpColGap  = 4
	helpMinRows = 24 // terminal height below which the card falls back to one column
)

// View renders the help overlay centered in a container of (w, h).
func (h *HelpOverlay) View(w, height int) string {
	theme := h.theme
	surf := theme.Surface

	cols := 2
	if w < helpColW*2+helpColGap+8 {
		cols = 1
	}

	groups := HelpGroups()
	blocks := make([][]string, 0, len(groups))
	for _, g := range groups {
		block := []string{components.SectionLabel(theme, g.Title, helpColW, surf)}
		block = append(block, components.HintColumns(theme, g.Keys, helpKeyW, helpDescW, surf)...)
		block = append(block, components.Fill(helpColW, surf))
		blocks = append(blocks, block)
	}

	columns := distributeBlocks(blocks, cols)

	// Pad every column to the tallest so the joined rows stay rectangular.
	tallest := 0
	for _, c := range columns {
		if len(c) > tallest {
			tallest = len(c)
		}
	}
	rendered := make([]string, len(columns))
	for i, c := range columns {
		rendered[i] = strings.Join(components.Pad(c, tallest, helpColW, surf), "\n")
	}

	gap := lipgloss.NewStyle().Background(surf).Width(helpColGap).Height(tallest).Render("")
	body := rendered[0]
	for i := 1; i < len(rendered); i++ {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, gap, rendered[i])
	}

	totalW := helpColW*len(columns) + helpColGap*(len(columns)-1)
	title := lipgloss.NewStyle().
		Foreground(theme.Accent).Background(surf).Bold(true).
		Width(totalW).Align(lipgloss.Center).
		Render(icons.Help + "  Keyboard Shortcuts")

	content := strings.Join([]string{title, components.Fill(totalW, surf), body}, "\n")
	return components.ModalBox(theme, content, 0, 0, w, height)
}

// distributeBlocks packs blocks into n columns, keeping each block whole and
// balancing total height. Splitting a group across a column break would put a
// heading in one column and half its keys in the next.
func distributeBlocks(blocks [][]string, n int) [][]string {
	if n < 1 {
		n = 1
	}
	total := 0
	for _, b := range blocks {
		total += len(b)
	}
	target := (total + n - 1) / n

	columns := make([][]string, 0, n)
	cur := []string{}
	for _, b := range blocks {
		// Start a new column once this one has met its share, unless we are
		// already on the last column — everything left has to land somewhere.
		if len(cur) > 0 && len(cur) >= target && len(columns) < n-1 {
			columns = append(columns, cur)
			cur = nil
		}
		cur = append(cur, b...)
	}
	columns = append(columns, cur)
	return columns
}
