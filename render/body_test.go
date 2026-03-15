package render

import (
	"testing"
)

func TestRenderBody_PrefersHTMLWhenPresent(t *testing.T) {
	theme := getTestTheme()
	width := 80

	plain := "This is plain text body."
	html := "<html><body>This is HTML body.</body></html>"

	lines := RenderBody(plain, html, width, theme)

	if len(lines) == 0 {
		t.Fatal("expected at least one line, got none")
	}

	containsHTML := false
	for _, line := range lines {
		if line == "This is HTML body." {
			containsHTML = true
			break
		}
	}

	if !containsHTML {
		t.Errorf("expected to find HTML text in output when both are present, got: %v", lines)
	}

	shouldNotContain := false
	for _, line := range lines {
		if line == "This is plain text body." {
			shouldNotContain = true
			break
		}
	}

	if shouldNotContain {
		t.Errorf("expected not to find plain text when HTML is present, got: %v", lines)
	}
}

func TestRenderBody_UsesHTMLFallback(t *testing.T) {
	theme := getTestTheme()
	width := 80

	plain := ""
	html := "<html><body>This is HTML body.</body></html>"

	lines := RenderBody(plain, html, width, theme)

	if len(lines) == 0 {
		t.Fatal("expected at least one line, got none")
	}

	containsHTML := false
	for _, line := range lines {
		if line == "This is HTML body." {
			containsHTML = true
			break
		}
	}

	if !containsHTML {
		t.Errorf("expected to find HTML text in output when plain text is empty, got: %v", lines)
	}
}

func TestRenderBody_EmptyBoth(t *testing.T) {
	theme := getTestTheme()
	width := 80

	plain := ""
	html := ""

	lines := RenderBody(plain, html, width, theme)

	if lines == nil {
		t.Errorf("expected non-nil slice, got nil")
	}
	if len(lines) != 1 {
		t.Errorf("expected 1 line when both plain and HTML are empty, got: %d lines: %v", len(lines), lines)
	}
	if len(lines) > 0 && lines[0] != "" {
		t.Errorf("expected empty string in result, got: %q", lines[0])
	}
}

func TestRenderBody_OnlyPlain(t *testing.T) {
	theme := getTestTheme()
	width := 80

	plain := "Line 1\nLine 2\nLine 3"
	html := ""

	lines := RenderBody(plain, html, width, theme)

	if len(lines) == 0 {
		t.Fatal("expected at least one line, got none")
	}

	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}
