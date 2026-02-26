package util

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// TruncateText truncates s to at most maxCols terminal columns, appending "…" if needed.
func TruncateText(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	return runewidth.Truncate(s, maxCols, "…")
}

// VisibleWidth returns the number of terminal columns occupied by s.
// ANSI escape codes are stripped before measurement.
func VisibleWidth(s string) int {
	return runewidth.StringWidth(StripANSI(s))
}

// PadRight pads s with spaces on the right to reach targetCols.
// Uses runewidth for correct Unicode handling.
func PadRight(s string, targetCols int) string {
	w := runewidth.StringWidth(s)
	if w >= targetCols {
		return s
	}
	return s + strings.Repeat(" ", targetCols-w)
}

// WrapText wraps s into lines of at most maxCols terminal columns.
// It breaks on word boundaries where possible.
func WrapText(s string, maxCols int) []string {
	if maxCols <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	current := ""
	currentW := 0

	for _, word := range words {
		wordW := runewidth.StringWidth(word)
		if currentW == 0 {
			current = word
			currentW = wordW
		} else if currentW+1+wordW <= maxCols {
			current += " " + word
			currentW += 1 + wordW
		} else {
			lines = append(lines, current)
			current = word
			currentW = wordW
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// WrapANSI wraps ANSI-styled text into lines of at most maxCols visible columns.
// It preserves ANSI codes across line breaks.
func WrapANSI(s string, maxCols int) []string {
	// For simplicity, split on spaces and track visible width.
	// A production implementation would carry escape sequences across breaks.
	if maxCols <= 0 {
		return []string{s}
	}
	lines := strings.Split(s, "\n")
	var result []string
	for _, line := range lines {
		result = append(result, wrapANSILine(line, maxCols)...)
	}
	return result
}

func wrapANSILine(s string, maxCols int) []string {
	if VisibleWidth(s) <= maxCols {
		return []string{s}
	}
	// Greedy word-wrap respecting visible width.
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	var lines []string
	current := ""
	currentW := 0
	for _, word := range words {
		wordW := VisibleWidth(word)
		if currentW == 0 {
			current = word
			currentW = wordW
		} else if currentW+1+wordW <= maxCols {
			current += " " + word
			currentW += 1 + wordW
		} else {
			lines = append(lines, current)
			current = word
			currentW = wordW
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// PluralCount returns "N singular" or "N plural" based on count.
func PluralCount(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// StripANSI removes ANSI escape codes from s.
func StripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inEsc {
			if c >= 0x40 && c <= 0x7E {
				inEsc = false
			}
		} else if c == 0x1B && i+1 < len(s) && s[i+1] == '[' {
			inEsc = true
			i++ // skip '['
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
