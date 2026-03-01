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
// ANSI escape codes (CSI and OSC) are stripped before measurement.
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
// OSC 8 hyperlinks are treated as atomic units and never split across lines.
func WrapANSI(s string, maxCols int) []string {
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

// wrapWord is a single unit for word-wrapping: either a plain word or an
// entire OSC 8 hyperlink (treated atomically to preserve the escape sequences).
type wrapWord struct {
	raw  string // raw bytes including ANSI/OSC sequences
	visW int    // visible terminal width
}

// splitToWrapWords tokenises s into wrapWords. Plain-text spans are split on
// whitespace; OSC 8 hyperlinks (ESC]8;;url BEL content ESC]8;;BEL) are kept
// as single atomic units so they are never broken across line boundaries.
func splitToWrapWords(s string) []wrapWord {
	var words []wrapWord

	// flushPlain splits a plain-text segment by whitespace and appends each
	// piece as a wrapWord.
	flushPlain := func(plain string) {
		for _, w := range strings.Fields(plain) {
			words = append(words, wrapWord{raw: w, visW: VisibleWidth(w)})
		}
	}

	segStart := 0
	i := 0
	for i < len(s) {
		// Detect OSC 8 hyperlink start: ESC ] 8 ; ; …
		if i+4 < len(s) && s[i] == '\x1b' && s[i+1] == ']' && s[i+2] == '8' && s[i+3] == ';' && s[i+4] == ';' {
			// Flush any preceding plain text.
			if i > segStart {
				flushPlain(s[segStart:i])
			}

			// Advance past "ESC]8;;" to find the BEL that ends the URL.
			j := i + 5
			for j < len(s) && s[j] != '\x07' {
				j++
			}
			if j >= len(s) {
				// Malformed: no BEL — treat the rest as plain text.
				flushPlain(s[i:])
				segStart = len(s)
				break
			}
			j++ // past BEL

			// Find the closing "ESC]8;;BEL".
			const closeSeq = "\x1b]8;;\x07"
			k := strings.Index(s[j:], closeSeq)
			if k < 0 {
				// Malformed: no closing sequence — treat the rest as plain text.
				flushPlain(s[i:])
				segStart = len(s)
				break
			}
			k += j
			linkEnd := k + len(closeSeq)

			// The visible width is the display text between the two BELs.
			displayText := s[j:k]
			visW := runewidth.StringWidth(StripANSI(displayText))

			words = append(words, wrapWord{raw: s[i:linkEnd], visW: visW})
			segStart = linkEnd
			i = linkEnd
			continue
		}
		i++
	}

	// Flush any trailing plain text.
	if segStart < len(s) {
		flushPlain(s[segStart:])
	}

	return words
}

func wrapANSILine(s string, maxCols int) []string {
	if VisibleWidth(s) <= maxCols {
		return []string{s}
	}

	words := splitToWrapWords(s)
	if len(words) == 0 {
		return []string{s}
	}

	var lines []string
	current := ""
	currentW := 0

	for _, w := range words {
		if currentW == 0 {
			current = w.raw
			currentW = w.visW
		} else if currentW+1+w.visW <= maxCols {
			current += " " + w.raw
			currentW += 1 + w.visW
		} else {
			lines = append(lines, current)
			current = w.raw
			currentW = w.visW
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
// It handles both CSI sequences (ESC [ ... final-byte) and
// OSC sequences (ESC ] ... BEL  or  ESC ] ... ESC \).
func StripANSI(s string) string {
	var b strings.Builder
	inCSI := false
	inOSC := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inOSC:
			// OSC ends at BEL or at ESC \  (ST = String Terminator)
			if c == '\x07' {
				inOSC = false
			} else if c == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
				i++ // skip '\'
				inOSC = false
			}
			// else: skip OSC content byte
		case inCSI:
			// CSI ends at any byte in 0x40–0x7E
			if c >= 0x40 && c <= 0x7E {
				inCSI = false
			}
			// else: skip CSI parameter byte
		case c == '\x1b' && i+1 < len(s):
			next := s[i+1]
			if next == '[' {
				inCSI = true
				i++ // skip '['
			} else if next == ']' {
				inOSC = true
				i++ // skip ']'
			}
			// other ESC sequences: skip the introducer, let the byte after be processed normally
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
