// Package render converts email body content (plain text or HTML) into
// styled terminal display lines ready for use in ReaderView.
package render

import (
	"github.com/bubblmail/bubblmail/config"
)

// RenderBody returns a slice of display lines for an email body.
// If htmlBody is non-empty it is parsed and rendered; otherwise plainBody is used.
// Each line may contain ANSI escape codes for colour and style.
func RenderBody(plainBody, htmlBody string, width int, theme *config.Theme) []string {
	if htmlBody != "" {
		return renderHTML(htmlBody, width, theme)
	}
	return renderPlain(plainBody, width, theme)
}
