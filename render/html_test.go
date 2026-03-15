package render

import (
	"strings"
	"testing"

	"github.com/bubblmail/bubblmail/config"
)

func getTestTheme() *config.Theme {
	return &config.Theme{
		Accent:    "#1E66F5",
		TextMuted: "#6C6F85",
	}
}

func TestRenderHTML_Paragraphs(t *testing.T) {
	theme := getTestTheme()
	width := 80

	html := `<p>First paragraph</p><p>Second paragraph</p>`

	lines := RenderBody("", html, width, theme)

	if len(lines) == 0 {
		t.Fatal("expected at least one line, got none")
	}

	contains := func(s string) bool {
		for _, line := range lines {
			if s == line {
				return true
			}
		}
		return false
	}

	if !contains("First paragraph") {
		t.Errorf("expected to find 'First paragraph' in output, got: %v", lines)
	}
	if !contains("Second paragraph") {
		t.Errorf("expected to find 'Second paragraph' in output, got: %v", lines)
	}

	emptyCount := 0
	for _, line := range lines {
		if line == "" {
			emptyCount++
		}
	}

	if emptyCount == 0 {
		t.Errorf("expected at least one empty line between paragraphs, got none")
	}
}

func TestRenderHTML_ListRendering(t *testing.T) {
	theme := getTestTheme()
	width := 80

	tests := []struct {
		name       string
		html       string
		wantPrefix string
	}{
		{
			name:       "unordered list",
			html:       `<ul><li>Item 1</li><li>Item 2</li></ul>`,
			wantPrefix: "• ",
		},
		{
			name:       "ordered list",
			html:       `<ol><li>First</li><li>Second</li></ol>`,
			wantPrefix: "1. ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := renderHTML(tt.html, width, theme)

			if len(lines) == 0 {
				t.Fatal("expected at least one line, got none")
			}

			found := false
			for _, line := range lines {
				runes := []rune(line)
				if len(runes) > 0 && runes[0] == '•' || (len(runes) > 2 && runes[1] == '.' && runes[2] == ' ') {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("expected to find list prefix '%s' in output, got: %v", tt.wantPrefix, lines)
			}
		})
	}
}

func TestRenderHTML_Blockquote(t *testing.T) {
	theme := getTestTheme()
	width := 80

	html := `<blockquote>Quoted text here</blockquote>`

	lines := RenderBody("", html, width, theme)

	if len(lines) == 0 {
		t.Fatal("expected at least one line, got none")
	}

	containsText := false
	for _, line := range lines {
		if strings.Contains(line, "Quoted text here") {
			containsText = true
			break
		}
	}

	if !containsText {
		t.Errorf("expected to find quoted text in output, got: %v", lines)
	}
}

func TestRenderHTML_CodeBlock(t *testing.T) {
	theme := getTestTheme()
	width := 80

	html := `<pre>  code line 1
  code line 2
  code line 3</pre>`

	lines := RenderBody("", html, width, theme)

	if len(lines) == 0 {
		t.Fatal("expected at least one line, got none")
	}

	combined := ""
	for _, line := range lines {
		combined += line + " "
	}

	containsCode := false
	if len(combined) > 0 {
		if strings.Contains(combined, "code line 1") && strings.Contains(combined, "code line 2") && strings.Contains(combined, "code line 3") {
			containsCode = true
		}
	}

	if !containsCode {
		t.Errorf("expected to find code lines in output, got: %v", lines)
	}
}

func TestRenderHTML_Links(t *testing.T) {
	theme := getTestTheme()
	width := 80

	html := `<a href="http://example.com">Example Link</a>`

	lines := RenderBody("", html, width, theme)

	if len(lines) == 0 {
		t.Fatal("expected at least one line, got none")
	}

	containsLinkText := false
	for _, line := range lines {
		if containsText(line, "Example Link") {
			containsLinkText = true
			break
		}
	}

	if !containsLinkText {
		t.Errorf("expected to find 'Example Link' in output, got: %v", lines)
	}

	containsLink := false
	for _, line := range lines {
		if strings.Contains(line, "http://example.com") {
			containsLink = true
			break
		}
	}

	if !containsLink {
		t.Errorf("expected to find hyperlink with URL 'http://example.com' in output, got: %v", lines)
	}
}

func containsText(line, text string) bool {
	stripped := line
	stripped = stripped[:0]
	for i := 0; i < len(line); {
		if i+4 < len(line) && line[i] == '\x1b' && line[i+1] == ']' && line[i+2] == '8' && line[i+3] == ';' && line[i+4] == ';' {
			j := i + 5
			for j < len(line) && line[j] != '\x07' {
				j++
			}
			if j < len(line) {
				j++
				k := j
				for k < len(line) && !(k+4 < len(line) && line[k] == '\x1b' && line[k+1] == ']' && line[k+2] == '8' && line[k+3] == ';' && line[k+4] == ';') {
					k++
				}
				if k < len(line) {
					stripped += line[j:k]
					i = k + 5
					for i < len(line) && line[i] != '\x07' {
						i++
					}
					i++
					continue
				}
			}
			i = j
			continue
		}
		if line[i] == '\x1b' {
			if i+1 < len(line) && (line[i+1] == '[' || line[i+1] == ']') {
				j := i + 2
				for j < len(line) && !(line[j] >= 0x40 && line[j] <= 0x7E) && line[j] != '\x07' {
					j++
				}
				if j < len(line) {
					i = j + 1
					continue
				}
			}
			i++
			continue
		}
		stripped += string(line[i])
		i++
	}
	return stripped == text
}

func containsHyperlink(line, url string) bool {
	return len(line) > len(url)+10 &&
		line[:4] == "\x1b]8;" &&
		line[5:5] == ";" &&
		line[6:6+len(url)] == url
}

func TestRenderHTML_Headings(t *testing.T) {
	theme := getTestTheme()
	width := 80

	html := `<h1>Heading 1</h1><h2>Heading 2</h2>`

	lines := RenderBody("", html, width, theme)

	if len(lines) == 0 {
		t.Fatal("expected at least one line, got none")
	}

	containsH1 := false
	containsH2 := false
	for _, line := range lines {
		if containsText(line, "Heading 1") {
			containsH1 = true
		}
		if containsText(line, "Heading 2") {
			containsH2 = true
		}
	}

	if !containsH1 {
		t.Errorf("expected to find 'Heading 1' in output, got: %v", lines)
	}
	if !containsH2 {
		t.Errorf("expected to find 'Heading 2' in output, got: %v", lines)
	}
}

func TestRenderHTML_BoldAndItalic(t *testing.T) {
	theme := getTestTheme()
	width := 80

	html := `<p><b>Bold text</b> and <i>italic text</i></p>`

	lines := RenderBody("", html, width, theme)

	if len(lines) == 0 {
		t.Fatal("expected at least one line, got none")
	}

	combined := ""
	for _, line := range lines {
		combined += line + " "
	}

	if !strings.Contains(combined, "Bold text") {
		t.Errorf("expected to find 'Bold text' in output, got: %v", lines)
	}
	if !strings.Contains(combined, "italic text") {
		t.Errorf("expected to find 'italic text' in output, got: %v", lines)
	}
}

func TestRenderHTML_Divider(t *testing.T) {
	theme := getTestTheme()
	width := 80

	html := `<hr>`

	lines := RenderBody("", html, width, theme)

	if len(lines) == 0 {
		t.Fatal("expected at least one line, got none")
	}

	containsDivider := false
	for _, line := range lines {
		runes := []rune(line)
		if len(runes) == width && len(runes) > 0 && runes[0] == '─' {
			containsDivider = true
			break
		}
	}

	if !containsDivider {
		t.Errorf("expected to find divider line in output, got: %v", lines)
	}
}

func allRune(s string, r rune) bool {
	for _, c := range s {
		if c != r {
			return false
		}
	}
	return true
}
