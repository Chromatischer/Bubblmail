package util

import (
	"strings"
	"testing"
)

func TestSingleLine_RemovesNewlinesCarriageReturnsAndZWJ(t *testing.T) {
	in := "hello\nworld\rchef \U0001F9D1\u200D\U0001F373"
	got := SingleLine(in)

	if strings.ContainsRune(got, '\n') || strings.ContainsRune(got, '\r') {
		t.Fatalf("SingleLine should remove line breaks, got %q", got)
	}
	if strings.ContainsRune(got, '\u200d') {
		t.Fatalf("SingleLine should strip ZWJ, got %q", got)
	}
	if got != "hello world chef \U0001F9D1\U0001F373" {
		t.Fatalf("SingleLine = %q", got)
	}
}

func TestStripANSI_RemovesCSIAndOSC8Sequences(t *testing.T) {
	styled := "\x1b[31mred\x1b[0m and \x1b]8;;https://example.com\x07link\x1b]8;;\x07"
	got := StripANSI(styled)
	if got != "red and link" {
		t.Fatalf("StripANSI = %q, want %q", got, "red and link")
	}
}

func TestWrapANSI_PreservesOSC8LinkAsAtomicUnit(t *testing.T) {
	link := "\x1b]8;;https://example.com\x07hello\x1b]8;;\x07"
	input := "start " + link + " end"

	lines := WrapANSI(input, 7)
	if len(lines) != 3 {
		t.Fatalf("expected 3 wrapped lines, got %d: %+v", len(lines), lines)
	}
	if StripANSI(lines[1]) != "hello" {
		t.Fatalf("expected link line to preserve display text, got %q", StripANSI(lines[1]))
	}
	if !strings.Contains(lines[1], "\x1b]8;;https://example.com\x07") {
		t.Fatalf("expected OSC8 sequence to remain intact, got %q", lines[1])
	}
}

func TestWrapANSI_ClampsLongOSC8LinkText(t *testing.T) {
	link := "\x1b]8;;https://example.com\x07verylonglinktext\x1b]8;;\x07"
	lines := WrapANSI(link, 6)

	if len(lines) != 1 {
		t.Fatalf("expected one clamped line, got %d: %+v", len(lines), lines)
	}
	visible := StripANSI(lines[0])
	if VisibleWidth(visible) > 6 {
		t.Fatalf("expected visible width <= 6, got %d for %q", VisibleWidth(visible), visible)
	}
	if !strings.Contains(lines[0], "\x1b]8;;https://example.com\x07") {
		t.Fatalf("expected clamped OSC8 link to preserve URL, got %q", lines[0])
	}
}

func TestPadRight_UsesVisibleWidthWithANSI(t *testing.T) {
	styled := "\x1b[32mhi\x1b[0m"
	padded := PadRight(styled, 5)
	if VisibleWidth(padded) != 5 {
		t.Fatalf("VisibleWidth(PadRight(...)) = %d, want 5", VisibleWidth(padded))
	}
	if !strings.HasPrefix(padded, styled) {
		t.Fatalf("expected padding to keep original prefix, got %q", padded)
	}
}
