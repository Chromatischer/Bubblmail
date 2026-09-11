package components

import (
	"github.com/bubblmail/bubblmail/config"
	"github.com/charmbracelet/lipgloss"
)

const (
	scrollTrack = "│"
	scrollThumb = "┃"
)

// Scrollbar renders a 1-column position indicator as `height` separate rows,
// ready to be joined onto the right edge of a list or body pane.
//
// It answers the two questions a long list never otherwise answers: how much
// is there, and where am I. When everything fits, it returns blank columns
// rather than a full-height thumb, so the gutter disappears instead of lying.
//
//	total   — number of items (or lines) in the whole document
//	visible — number of them on screen
//	offset  — index of the first visible one
func Scrollbar(theme *config.Theme, total, visible, offset, height int, bg lipgloss.Color) []string {
	if height <= 0 {
		return nil
	}

	blank := lipgloss.NewStyle().Width(1)
	if bg != "" {
		blank = blank.Background(bg)
	}

	rows := make([]string, height)
	if total <= visible || total <= 0 || visible <= 0 {
		empty := blank.Render(" ")
		for i := range rows {
			rows[i] = empty
		}
		return rows
	}

	trackSt := lipgloss.NewStyle().Foreground(theme.Border).Width(1)
	thumbSt := lipgloss.NewStyle().Foreground(theme.AccentSoft).Width(1)
	if bg != "" {
		trackSt = trackSt.Background(bg)
		thumbSt = thumbSt.Background(bg)
	}

	// Thumb length is proportional to the visible fraction, floored at 1 so it
	// never vanishes in a very long document.
	thumbLen := visible * height / total
	if thumbLen < 1 {
		thumbLen = 1
	}
	if thumbLen > height {
		thumbLen = height
	}

	// Distribute the thumb over the scrollable travel rather than the full
	// height, so that offset==0 pins it to the top and the last page pins it
	// to the bottom exactly.
	maxOffset := total - visible
	travel := height - thumbLen
	start := 0
	if maxOffset > 0 && travel > 0 {
		start = offset * travel / maxOffset
	}
	if start < 0 {
		start = 0
	}
	if start > travel {
		start = travel
	}

	track := trackSt.Render(scrollTrack)
	thumb := thumbSt.Render(scrollThumb)
	for i := range rows {
		if i >= start && i < start+thumbLen {
			rows[i] = thumb
		} else {
			rows[i] = track
		}
	}
	return rows
}
