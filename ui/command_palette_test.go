package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dispatchGlobalKey must have a case for every action the palette can run. This
// reads the handler source so it catches a palette command pointing at a key the
// handler does not actually handle.
func TestPaletteKeysAreDispatchable(t *testing.T) {
	body := dispatchGlobalKeyBody(t)
	// Action keys that are written as named constants in the switch.
	constCase := map[string]string{
		KeyAttachments:  "KeyAttachments",
		KeyUnreadFilter: "KeyUnreadFilter",
		KeySidebarFocus: "KeySidebarFocus",
	}
	for _, c := range commandPaletteItems() {
		handled := strings.Contains(body, `case "`+c.Key+`"`) ||
			strings.Contains(body, `, "`+c.Key+`"`) // grouped case "x", "y"
		if name, ok := constCase[c.Key]; ok {
			handled = handled || strings.Contains(body, "case "+name) || strings.Contains(body, ", "+name)
		}
		if !handled {
			t.Errorf("palette command %q uses key %q which dispatchGlobalKey does not handle", c.Label, c.Key)
		}
	}
}

// dispatchGlobalKeyBody returns the source text of the dispatchGlobalKey method.
func dispatchGlobalKeyBody(t *testing.T) string {
	t.Helper()
	files, err := filepath.Glob("app*.go")
	if err != nil {
		t.Fatalf("listing app files: %v", err)
	}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		s := string(src)
		start := strings.Index(s, "func (a *App) dispatchGlobalKey(")
		if start < 0 {
			continue
		}
		next := strings.Index(s[start+1:], "\nfunc ")
		if next < 0 {
			return s[start:]
		}
		return s[start : start+1+next]
	}
	t.Fatal("could not find dispatchGlobalKey in app*.go")
	return ""
}

func TestPaletteFilterAndSelect(t *testing.T) {
	p := NewCommandPaletteOverlay(NewStyles(testTheme()))
	p.SetSize(50, 20)
	p.Open()

	// "jump" must be typeable as filter text (j is not a navigation key here).
	for _, r := range "jump" {
		p.HandleKey(string(r))
	}
	if len(p.filtered) != 1 || p.filtered[0].Key != "i" {
		t.Fatalf("filtering 'jump' should yield only Jump-to-inbox, got %+v", p.filtered)
	}

	closed := p.HandleKey("enter")
	if !closed {
		t.Fatal("enter should close the palette")
	}
	if p.Result() == nil || p.Result().Key != "i" {
		t.Fatalf("expected Jump-to-inbox result, got %+v", p.Result())
	}
}

func TestPaletteEscClearsThenCloses(t *testing.T) {
	p := NewCommandPaletteOverlay(NewStyles(testTheme()))
	p.SetSize(50, 20)
	p.Open()
	p.HandleKey("r")
	if closed := p.HandleKey("esc"); closed {
		t.Error("first esc with a query should clear, not close")
	}
	if p.query != "" {
		t.Error("esc should have cleared the query")
	}
	if closed := p.HandleKey("esc"); !closed {
		t.Error("esc with empty query should close")
	}
}
