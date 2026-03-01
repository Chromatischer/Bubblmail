package render

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// urlRegex matches http/https URLs — same pattern as tipical's textutil.go.
var urlRegex = regexp.MustCompile(`https?://[^\s<>"{}|\\^` + "`" + `\[\]]+`)

// hyperlink wraps displayText in an OSC 8 terminal hyperlink pointing to url,
// with the display text coloured using accentColor.
func hyperlink(url, displayText string, accentColor lipgloss.Color) string {
	styled := lipgloss.NewStyle().Foreground(accentColor).Render(displayText)
	return "\x1b]8;;" + url + "\x07" + styled + "\x1b]8;;\x07"
}

// linkifyText scans text for bare http/https URLs and wraps each one in an
// OSC 8 hyperlink with accent colouring. Non-URL spans are returned as-is.
func linkifyText(text string, accentColor lipgloss.Color) string {
	matches := urlRegex.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(text[last:m[0]])
		url := text[m[0]:m[1]]
		b.WriteString(hyperlink(url, url, accentColor))
		last = m[1]
	}
	b.WriteString(text[last:])
	return b.String()
}
