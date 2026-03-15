package composer

import (
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/charmbracelet/lipgloss"
)

type mdSpan struct {
	text string
	kind string // "text", "strong", "bold", "italic", "code"
}

// parseInlineMarkdown tokenises a plain-text line into styled spans.
// Precedence (highest first): `code`, **strong**, *bold*, _italic_.
// An unclosed delimiter is treated as a literal character.
func parseInlineMarkdown(text string) []mdSpan {
	runes := []rune(text)
	var spans []mdSpan
	var buf strings.Builder
	i := 0

	flush := func() {
		if buf.Len() > 0 {
			spans = append(spans, mdSpan{text: buf.String(), kind: "text"})
			buf.Reset()
		}
	}

	for i < len(runes) {
		// ── backtick code span ────────────────────────────────────────────────
		if runes[i] == '`' {
			flush()
			j := i + 1
			for j < len(runes) && runes[j] != '`' {
				j++
			}
			if j < len(runes) {
				spans = append(spans, mdSpan{text: string(runes[i+1 : j]), kind: "code"})
				i = j + 1
				continue
			}
			buf.WriteRune(runes[i])
			i++
			continue
		}

		// ── **strong** ────────────────────────────────────────────────────────
		if runes[i] == '*' && i+1 < len(runes) && runes[i+1] == '*' {
			flush()
			j := i + 2
			for j+1 < len(runes) && !(runes[j] == '*' && runes[j+1] == '*') {
				j++
			}
			if j+1 < len(runes) {
				spans = append(spans, mdSpan{text: string(runes[i+2 : j]), kind: "strong"})
				i = j + 2
				continue
			}
			buf.WriteRune(runes[i])
			i++
			continue
		}

		// ── *bold* ────────────────────────────────────────────────────────────
		if runes[i] == '*' {
			flush()
			j := i + 1
			for j < len(runes) && runes[j] != '*' {
				j++
			}
			if j < len(runes) {
				spans = append(spans, mdSpan{text: string(runes[i+1 : j]), kind: "bold"})
				i = j + 1
				continue
			}
			buf.WriteRune(runes[i])
			i++
			continue
		}

		// ── _italic_ ──────────────────────────────────────────────────────────
		if runes[i] == '_' {
			flush()
			j := i + 1
			for j < len(runes) && runes[j] != '_' {
				j++
			}
			if j < len(runes) {
				spans = append(spans, mdSpan{text: string(runes[i+1 : j]), kind: "italic"})
				i = j + 1
				continue
			}
			buf.WriteRune(runes[i])
			i++
			continue
		}

		buf.WriteRune(runes[i])
		i++
	}
	flush()
	return spans
}

// renderMarkdownLine parses inline markdown from plain and returns a styled
// string suitable for display. bg is the background colour for all spans.
// The caller is responsible for width-padding the result.
func renderMarkdownLine(plain string, theme *config.Theme, bg lipgloss.Color) string {
	spans := parseInlineMarkdown(plain)
	var sb strings.Builder
	for _, s := range spans {
		var st lipgloss.Style
		switch s.kind {
		case "strong": // **text** — accent + bold
			st = lipgloss.NewStyle().
				Foreground(theme.Accent).
				Background(bg).
				Bold(true)
		case "bold": // *text* — bold
			st = lipgloss.NewStyle().
				Foreground(theme.Text).
				Background(bg).
				Bold(true)
		case "italic": // _text_ — italic
			st = lipgloss.NewStyle().
				Foreground(theme.Text).
				Background(bg).
				Italic(true)
		case "code": // `text` — highlighted
			st = lipgloss.NewStyle().
				Foreground(theme.Starred).
				Background(bg)
		default:
			st = lipgloss.NewStyle().
				Foreground(theme.Text).
				Background(bg)
		}
		sb.WriteString(st.Render(s.text))
	}
	return sb.String()
}

// isCodeFence reports whether a sanitised line is a markdown code fence (```).
func isCodeFence(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}
