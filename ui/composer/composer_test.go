package composer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	tea "github.com/charmbracelet/bubbletea"
)

func typeRunes(c *Composer, s string) {
	for _, r := range s {
		if r == ' ' {
			c.HandleKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{r}})
			continue
		}
		c.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// Subject field is index 2 in the default New layout.
func TestComposerTypesMultiByteRunes(t *testing.T) {
	c := NewComposer(&config.Theme{})
	c.OpenNew(data.Address{})
	c.SetFocus(2) // Subject

	typeRunes(c, "Grüße €5 — café")

	if got := c.fields[2].Value; got != "Grüße €5 — café" {
		t.Fatalf("subject = %q, want %q", got, "Grüße €5 — café")
	}
}

// A bracketed paste arrives as a single KeyRunes event with many runes.
func TestComposerPasteInsertsFullText(t *testing.T) {
	c := NewComposer(&config.Theme{})
	c.OpenNew(data.Address{})
	c.SetFocus(2) // Subject

	paste := "hello wörld 漢字"
	c.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(paste), Paste: true})

	if got := c.fields[2].Value; got != paste {
		t.Fatalf("subject = %q, want %q", got, paste)
	}
}

// Pasting multi-line text into a single-line field flattens newlines to spaces.
func TestComposerPasteFlattensNewlinesInSingleLineField(t *testing.T) {
	c := NewComposer(&config.Theme{})
	c.OpenNew(data.Address{})
	c.SetFocus(0) // To (single line)

	c.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a@x.com\r\nb@y.com"), Paste: true})

	if got := c.fields[0].Value; got != "a@x.com b@y.com" {
		t.Fatalf("to = %q, want %q", got, "a@x.com b@y.com")
	}
}

// Pasting multi-line text into the body keeps newlines and tabs intact.
func TestComposerPastePreservesBodyWhitespace(t *testing.T) {
	c := NewComposer(&config.Theme{})
	c.OpenNew(data.Address{})
	c.SetFocus(3) // Body

	paste := "hello\n\twörld\r\n漢字"
	c.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(paste), Paste: true})

	if got := c.fields[3].Value; got != "hello\n\twörld\n漢字" {
		t.Fatalf("body = %q, want %q", got, "hello\n\twörld\n漢字")
	}
}

func TestComposerDeleteRemovesRuneAtCursor(t *testing.T) {
	c := NewComposer(&config.Theme{})
	c.OpenNew(data.Address{})
	c.SetFocus(2)
	typeRunes(c, "café")

	c.HandleKey(tea.KeyMsg{Type: tea.KeyLeft})
	c.HandleKey(tea.KeyMsg{Type: tea.KeyDelete})

	if got := c.fields[2].Value; got != "caf" {
		t.Fatalf("subject = %q, want %q", got, "caf")
	}
}

func TestComposerBackspaceRemovesPreviousRune(t *testing.T) {
	c := NewComposer(&config.Theme{})
	c.OpenNew(data.Address{})
	c.SetFocus(2)
	typeRunes(c, "café")

	c.HandleKey(tea.KeyMsg{Type: tea.KeyBackspace})

	if got := c.fields[2].Value; got != "caf" {
		t.Fatalf("subject = %q, want %q", got, "caf")
	}
}

func TestComposerIgnoresAltRuneCommands(t *testing.T) {
	c := NewComposer(&config.Theme{})
	c.OpenNew(data.Address{})
	c.SetFocus(2)

	c.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true})

	if got := c.fields[2].Value; got != "" {
		t.Fatalf("subject = %q, want empty", got)
	}
}

// Named special keys must not be inserted as literal text.
func TestComposerIgnoresNamedKeys(t *testing.T) {
	c := NewComposer(&config.Theme{})
	c.OpenNew(data.Address{})
	c.SetFocus(2)

	c.HandleKey(tea.KeyMsg{Type: tea.KeyF1})

	if got := c.fields[2].Value; got != "" {
		t.Fatalf("subject = %q, want empty", got)
	}
}

func TestComposerShowsAttachmentFailure(t *testing.T) {
	c := NewComposer(&config.Theme{})
	c.OpenNew(data.Address{})
	c.SetSize(100, 30)

	c.addAttachment(filepath.Join(t.TempDir(), "missing"), true)

	if c.errMessage == "" {
		t.Fatal("errMessage is empty, want attachment failure")
	}
	if !strings.Contains(c.View(), "Attachment failed:") {
		t.Fatal("composer view does not show attachment failure")
	}
}

func TestComposerClearsAttachmentFailureOnSuccess(t *testing.T) {
	c := NewComposer(&config.Theme{})
	c.OpenNew(data.Address{})
	c.addAttachment(filepath.Join(t.TempDir(), "missing"), true)
	if c.errMessage == "" {
		t.Fatal("errMessage is empty after failed attach")
	}

	path := filepath.Join(t.TempDir(), "ok.txt")
	c.addAttachment(path, false)

	if c.errMessage != "" {
		t.Fatalf("errMessage = %q, want empty after successful attach", c.errMessage)
	}
}
