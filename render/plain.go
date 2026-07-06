package render

import (
	"fmt"
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// foldMarkerPrefix is the prefix used to identify collapsed quote fold lines.
const foldMarkerPrefix = "▶ "

// FoldQuoteBlocks replaces contiguous runs of `>` quoted lines in a plain text
// body with a single summary line. The fold marker is plain text and will be
// styled by renderPlain when encountered.
func FoldQuoteBlocks(body string) string {
	lines := strings.Split(body, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		depth, _ := countAndStripQuote(lines[i])
		if depth > 0 {
			j := i
			for j < len(lines) {
				d, _ := countAndStripQuote(lines[j])
				if d == 0 {
					break
				}
				j++
			}
			n := j - i
			if n == 1 {
				out = append(out, foldMarkerPrefix+"1 quoted line")
			} else {
				out = append(out, fmt.Sprintf("%s%d quoted lines", foldMarkerPrefix, n))
			}
			i = j
		} else {
			out = append(out, lines[i])
			i++
		}
	}
	return strings.Join(out, "\n")
}

// renderPlain converts plain text email body to styled terminal lines.
// Lines starting with '>' are treated as quoted reply chains and styled
// with a muted color and │ bar prefix per quote depth.
// Lines starting with foldMarkerPrefix are fold summary lines, styled
// with accent color.
func renderPlain(body string, width int, theme *config.Theme) []string {
	quoteStyle := lipgloss.NewStyle().Foreground(theme.TextMuted)
	foldStyle := lipgloss.NewStyle().Foreground(theme.Accent)

	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			lines = append(lines, "")
			continue
		}

		// Fold marker line (collapsed quote block): render summary + inline keybind hint.
		if strings.HasPrefix(line, foldMarkerPrefix) {
			keyHintStyle := lipgloss.NewStyle().Foreground(theme.TextFaint)
			descHintStyle := lipgloss.NewStyle().Foreground(theme.TextMuted)
			hint := "  " + keyHintStyle.Render("z") + descHintStyle.Render(" expand")
			lines = append(lines, foldStyle.Render(line)+hint)
			continue
		}

		depth, stripped := countAndStripQuote(line)
		if depth == 0 {
			linked := linkifyText(line, theme.Accent)
			wrapped := util.WrapANSI(linked, width)
			lines = append(lines, wrapped...)
			continue
		}

		// Quote prefix: "│ " per depth level
		prefix := strings.Repeat("│ ", depth)
		prefixWidth := depth * 2
		wrapWidth := width - prefixWidth
		if wrapWidth < 10 {
			wrapWidth = 10
		}

		linked := linkifyText(stripped, theme.Accent)
		wrapped := util.WrapANSI(linked, wrapWidth)
		for _, wl := range wrapped {
			rendered := quoteStyle.Render(prefix + wl)
			lines = append(lines, rendered)
		}
	}
	return lines
}

// countAndStripQuote counts leading '>' characters and returns the depth
// and the stripped text content.
func countAndStripQuote(line string) (depth int, text string) {
	s := line
	for len(s) > 0 && s[0] == '>' {
		depth++
		s = s[1:]
		// Strip optional space after >
		if len(s) > 0 && s[0] == ' ' {
			s = s[1:]
		}
	}
	return depth, strings.TrimSpace(s)
}
