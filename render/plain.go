package render

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/util"
)

// renderPlain converts plain text email body to styled terminal lines.
// Lines starting with '>' are treated as quoted reply chains and styled
// with a muted color and │ bar prefix per quote depth.
func renderPlain(body string, width int, theme *config.Theme) []string {
	quoteStyle := lipgloss.NewStyle().Foreground(theme.TextMuted)

	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			lines = append(lines, "")
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
