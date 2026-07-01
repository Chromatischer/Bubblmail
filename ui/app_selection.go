package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// updateLineBuffers splits the rendered output into per-line buffers used for
// text selection: ansiLines keeps ANSI codes (for markdown copy), plainLines
// strips them (for plain-text copy). Returns the split lines slice so callers
// can reuse it without re-splitting.
func (a *App) updateLineBuffers(output string) []string {
	lines := strings.Split(output, "\n")
	a.ansiLines = lines
	a.plainLines = make([]string, len(lines))
	for i, l := range lines {
		a.plainLines[i] = ansi.Strip(l)
	}
	return lines
}

// ansiToMarkdown converts an ANSI-escaped string to Markdown, mapping bold
// (SGR 1/22) to **…** and italic (SGR 3/23) to _…_. All other escape
// sequences (colors, underline, etc.) are stripped.
func ansiToMarkdown(s string) string {
	var b strings.Builder
	inBold, inItalic := false, false
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Scan to the final byte of the escape sequence (a letter @–~).
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			if j < len(s) && s[j] == 'm' { // SGR only
				for _, param := range strings.Split(s[i+2:j], ";") {
					switch param {
					case "0", "": // reset
						if inItalic {
							b.WriteByte('_')
							inItalic = false
						}
						if inBold {
							b.WriteString("**")
							inBold = false
						}
					case "1": // bold on
						if !inBold {
							b.WriteString("**")
							inBold = true
						}
					case "22": // bold off
						if inBold {
							b.WriteString("**")
							inBold = false
						}
					case "3": // italic on
						if !inItalic {
							b.WriteByte('_')
							inItalic = true
						}
					case "23": // italic off
						if inItalic {
							b.WriteByte('_')
							inItalic = false
						}
					}
				}
			}
			if j < len(s) {
				i = j + 1
			} else {
				i++
			}
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	if inItalic {
		b.WriteByte('_')
	}
	if inBold {
		b.WriteString("**")
	}
	return b.String()
}

// normalizeSelection returns (startRow, startCol, endRow, endCol) with the start
// guaranteed to come before the end in reading order (top-left → bottom-right).
func normalizeSelection(startX, startY, endX, endY int) (sr, sc, er, ec int) {
	if startY < endY || (startY == endY && startX <= endX) {
		return startY, startX, endY, endX
	}
	return endY, endX, startY, startX
}

// extractSelectedText returns the text covered by the drag from (startX, startY)
// to (endX, endY), clamped to the drag's zone bounds.
// asMarkdown=true uses the ANSI-rich line buffer and converts bold/italic to
// Markdown syntax; asMarkdown=false returns raw stripped plain text.
func (a *App) extractSelectedText(startX, startY, endX, endY int, asMarkdown bool) string {
	if a.drag == nil {
		return ""
	}
	if asMarkdown && len(a.ansiLines) == 0 {
		return ""
	}
	if !asMarkdown && len(a.plainLines) == 0 {
		return ""
	}

	zx0 := a.drag.zoneX0
	zx1 := a.drag.zoneX1
	zy0 := a.drag.zoneY0
	zy1 := a.drag.zoneY1

	sr, sc, er, ec := normalizeSelection(startX, startY, endX, endY)
	if sr == er && sc == ec {
		return ""
	}

	// Clamp row range to zone.
	if sr < zy0 {
		sr, sc = zy0, zx0
	}
	if er > zy1 {
		er, ec = zy1, zx1+1
	}

	nLines := len(a.plainLines)
	if asMarkdown {
		nLines = len(a.ansiLines)
	}

	var parts []string
	for r := sr; r <= er && r < nLines; r++ {
		// Column range for this row, clamped to zone x bounds.
		c0 := zx0
		c1 := zx1 + 1
		if r == sr && sc > c0 {
			c0 = sc
		}
		if r == er && ec < c1 {
			c1 = ec
		}

		var chunk string
		if asMarkdown {
			chunk = ansiToMarkdown(ansi.Cut(a.ansiLines[r], c0, c1))
		} else {
			runes := []rune(a.plainLines[r])
			if c0 > len(runes) {
				c0 = len(runes)
			}
			if c1 > len(runes) {
				c1 = len(runes)
			}
			if c0 < c1 {
				chunk = string(runes[c0:c1])
			}
		}
		parts = append(parts, strings.TrimRight(chunk, " \t"))
	}

	return strings.TrimRight(strings.Join(parts, "\n"), "\n")
}

// applySelectionHighlight overlays a reverse-video highlight on the selected
// cells of the already-rendered (ANSI-escaped) lines slice, then re-joins them.
// The selection is clamped to [zx0,zx1] × [zy0,zy1] so it cannot bleed into
// adjacent UI zones.
func applySelectionHighlight(lines []string, startX, startY, endX, endY int,
	zx0, zy0, zx1, zy1 int) string {
	sr, sc, er, ec := normalizeSelection(startX, startY, endX, endY)
	selStyle := lipgloss.NewStyle().Reverse(true)

	// Clamp row range to zone.
	if sr < zy0 {
		sr, sc = zy0, zx0
	}
	if er > zy1 {
		er, ec = zy1, zx1+1
	}

	result := make([]string, len(lines))
	copy(result, lines)

	for r := sr; r <= er && r < len(lines); r++ {
		// Column range for this row, clamped to zone x bounds.
		c0 := zx0
		c1 := zx1 + 1
		if r == sr && sc > c0 {
			c0 = sc
		}
		if r == er && ec < c1 {
			c1 = ec
		}
		if c0 >= c1 {
			continue
		}
		result[r] = selectionHighlightLine(lines[r], c0, c1, selStyle)
	}

	return strings.Join(result, "\n")
}

// selectionHighlightLine applies highlight to columns [c0, c1) of a single
// ANSI-escaped line, preserving original styling outside the selection.
func selectionHighlightLine(line string, c0, c1 int, style lipgloss.Style) string {
	// Clamp c1 to the visual width of actual content (no trailing-space highlight).
	plain := ansi.Strip(line)
	contentEnd := lipgloss.Width(strings.TrimRight(plain, " "))
	if c1 > contentEnd {
		c1 = contentEnd
	}
	if c0 >= c1 {
		return line
	}
	prefix := ansi.Truncate(line, c0, "")
	middle := ansi.Strip(ansi.Cut(line, c0, c1))
	suffix := ansi.TruncateLeft(line, c1, "")
	return prefix + style.Render(middle) + suffix
}
