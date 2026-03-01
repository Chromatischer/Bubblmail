package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/net/html"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/util"
)

// block represents a logical content block extracted from an HTML document.
type block struct {
	text       string
	quoteDepth int
	isBold     bool
	isCode     bool
	listPrefix string // "• " or "N. "
	isDivider  bool
}

// renderHTML converts an HTML email body to styled terminal lines.
func renderHTML(htmlBody string, width int, theme *config.Theme) []string {
	doc, err := html.Parse(strings.NewReader(htmlBody))
	if err != nil {
		// Fallback: treat as plain text
		return renderPlain(htmlBody, width, theme)
	}

	var blocks []block
	var walker func(n *html.Node, quoteDepth int, inCode bool, listCounters []int)

	walker = func(n *html.Node, quoteDepth int, inCode bool, listCounters []int) {
		switch n.Type {
		case html.ElementNode:
			tag := strings.ToLower(n.Data)
			switch tag {
			case "br":
				// Line break — emit an empty block as paragraph separator
				blocks = append(blocks, block{text: ""})

			case "hr":
				blocks = append(blocks, block{isDivider: true})

			case "p", "div":
				// Ensure paragraph separation before content
				if len(blocks) > 0 && blocks[len(blocks)-1].text != "" {
					blocks = append(blocks, block{text: ""})
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walker(c, quoteDepth, inCode, listCounters)
				}
				// Ensure paragraph separation after content
				if len(blocks) > 0 && blocks[len(blocks)-1].text != "" {
					blocks = append(blocks, block{text: ""})
				}
				return

			case "h1", "h2", "h3", "h4", "h5", "h6":
				// Collect heading text with isBold=true
				if len(blocks) > 0 && blocks[len(blocks)-1].text != "" {
					blocks = append(blocks, block{text: ""})
				}
				var sb strings.Builder
				collectText(n, &sb)
				if t := strings.TrimSpace(sb.String()); t != "" {
					blocks = append(blocks, block{
						text:       t,
						isBold:     true,
						quoteDepth: quoteDepth,
					})
					blocks = append(blocks, block{text: ""})
				}
				return

			case "blockquote":
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walker(c, quoteDepth+1, inCode, listCounters)
				}
				return

			case "pre":
				// Preserve whitespace — emit code block
				var sb strings.Builder
				collectText(n, &sb)
				raw := sb.String()
				for _, l := range strings.Split(raw, "\n") {
					blocks = append(blocks, block{
						text:       l,
						isCode:     true,
						quoteDepth: quoteDepth,
					})
				}
				return

			case "code":
				if inCode {
					// Already inside <pre>, handled above
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walker(c, quoteDepth, true, listCounters)
					}
					return
				}
				// Inline code — treat as normal text with backtick wrapping
				var sb strings.Builder
				collectText(n, &sb)
				if t := sb.String(); t != "" {
					appendText(&blocks, "`"+t+"`", quoteDepth)
				}
				return

			case "ul":
				newCounters := append(listCounters, 0) // 0 = unordered
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && strings.ToLower(c.Data) == "li" {
						emitListItem(c, quoteDepth, inCode, newCounters, -1, &blocks, walker)
					}
				}
				return

			case "ol":
				newCounters := append(listCounters, 1) // start at 1
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && strings.ToLower(c.Data) == "li" {
						n := newCounters[len(newCounters)-1]
						emitListItem(c, quoteDepth, inCode, newCounters, n, &blocks, walker)
						newCounters[len(newCounters)-1]++
					}
				}
				return

			case "b", "strong":
				// Collect inline bold text
				var sb strings.Builder
				collectText(n, &sb)
				if t := strings.TrimSpace(sb.String()); t != "" {
					styled := lipgloss.NewStyle().Bold(true).Render(t)
					appendText(&blocks, styled, quoteDepth)
				}
				return

			case "i", "em":
				// Collect inline italic text
				var sb strings.Builder
				collectText(n, &sb)
				if t := strings.TrimSpace(sb.String()); t != "" {
					styled := lipgloss.NewStyle().Italic(true).Render(t)
					appendText(&blocks, styled, quoteDepth)
				}
				return

			case "a":
				href := attrVal(n, "href")
				var sb strings.Builder
				collectText(n, &sb)
				linkText := strings.TrimSpace(sb.String())
				if linkText == "" {
					// Fall back to alt text from any child <img>.
					linkText = collectImgAlt(n)
				}
				if linkText == "" {
					// Still no text — walk children generically so nested content isn't lost.
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walker(c, quoteDepth, inCode, listCounters)
					}
					return
				}
				if href != "" {
					appendText(&blocks, hyperlink(href, linkText, theme.Accent), quoteDepth)
				} else {
					appendText(&blocks, linkText, quoteDepth)
				}
				return

			case "td", "th":
				// Treat each cell as a block: inject paragraph separation before and after.
				if len(blocks) > 0 && blocks[len(blocks)-1].text != "" {
					blocks = append(blocks, block{text: ""})
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walker(c, quoteDepth, inCode, listCounters)
				}
				if len(blocks) > 0 && blocks[len(blocks)-1].text != "" {
					blocks = append(blocks, block{text: ""})
				}
				return

			case "style", "script", "head":
				// Skip entirely
				return

			default:
				// Walk children generically
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walker(c, quoteDepth, inCode, listCounters)
				}
				return
			}

		case html.TextNode:
			t := cleanText(n.Data)
			if t != "" {
				appendText(&blocks, t, quoteDepth)
			}
			return
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walker(c, quoteDepth, inCode, listCounters)
		}
	}

	walker(doc, 0, false, nil)

	return renderBlocks(blocks, width, theme)
}

// emitListItem processes a single <li> element.
func emitListItem(
	n *html.Node,
	quoteDepth int,
	inCode bool,
	listCounters []int,
	orderedNum int,
	blocks *[]block,
	walker func(*html.Node, int, bool, []int),
) {
	var prefix string
	if orderedNum < 0 {
		prefix = "• "
	} else {
		prefix = fmt.Sprintf("%d. ", orderedNum)
	}
	var sb strings.Builder
	collectText(n, &sb)
	if t := strings.TrimSpace(sb.String()); t != "" {
		*blocks = append(*blocks, block{
			text:       t,
			listPrefix: prefix,
			quoteDepth: quoteDepth,
		})
	}
}

// appendText appends text to the last block if it has the same quoteDepth and
// is not special; otherwise creates a new block.
func appendText(blocks *[]block, text string, quoteDepth int) {
	if len(*blocks) > 0 {
		last := &(*blocks)[len(*blocks)-1]
		if !last.isDivider && !last.isBold && !last.isCode && last.quoteDepth == quoteDepth && last.listPrefix == "" {
			if last.text == "" {
				last.text = text
			} else {
				last.text += " " + text
			}
			return
		}
	}
	*blocks = append(*blocks, block{text: text, quoteDepth: quoteDepth})
}

// renderBlocks converts extracted blocks into terminal display lines.
func renderBlocks(blocks []block, width int, theme *config.Theme) []string {
	quoteStyle := lipgloss.NewStyle().Foreground(theme.TextMuted)
	divider := strings.Repeat("─", width)

	// Collapse consecutive empty blocks to at most one (marketing emails use
	// many <br> tags and empty table cells that generate long runs of blanks).
	deduped := blocks[:0:0]
	lastEmpty := false
	for _, b := range blocks {
		isEmpty := !b.isDivider && b.text == "" && b.listPrefix == ""
		if isEmpty {
			if lastEmpty {
				continue
			}
			lastEmpty = true
		} else {
			lastEmpty = false
		}
		deduped = append(deduped, b)
	}
	blocks = deduped

	var lines []string
	for _, b := range blocks {
		if b.isDivider {
			lines = append(lines, divider)
			continue
		}

		if b.isCode {
			// No wrapping; cap to width using rune-aware truncation.
			line := b.text
			if util.VisibleWidth(line) > width {
				line = util.TruncateText(line, width)
			}
			lines = append(lines, line)
			continue
		}

		prefix := strings.Repeat("│ ", b.quoteDepth)
		prefixWidth := b.quoteDepth * 2

		listPrefix := b.listPrefix
		listPrefixWidth := util.VisibleWidth(listPrefix)

		wrapWidth := width - prefixWidth - listPrefixWidth
		if wrapWidth < 10 {
			wrapWidth = 10
		}

		text := b.text
		if text == "" {
			lines = append(lines, "")
			continue
		}

		var wrapped []string
		if b.isBold {
			styled := lipgloss.NewStyle().Bold(true).Render(text)
			wrapped = util.WrapANSI(styled, wrapWidth)
		} else {
			wrapped = util.WrapANSI(text, wrapWidth)
		}

		for i, wl := range wrapped {
			linePrefix := prefix
			if i == 0 {
				linePrefix += listPrefix
			} else if listPrefix != "" {
				// Indent continuation lines to align with list text
				linePrefix += strings.Repeat(" ", listPrefixWidth)
			}

			if b.quoteDepth > 0 {
				lines = append(lines, quoteStyle.Render(linePrefix+wl))
			} else {
				lines = append(lines, linePrefix+wl)
			}
		}
	}
	return lines
}

// collectText recursively collects all text node content under n, normalising
// whitespace so that newlines and tabs never appear in the result. Words from
// adjacent elements are separated by a single space.
func collectText(n *html.Node, sb *strings.Builder) {
	if n.Type == html.TextNode {
		// Collapse all whitespace runs (including \n and \t) to single spaces.
		t := strings.Join(strings.Fields(n.Data), " ")
		if t == "" {
			return
		}
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(t)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		// Skip style/script
		if c.Type == html.ElementNode {
			tag := strings.ToLower(c.Data)
			if tag == "style" || tag == "script" {
				continue
			}
		}
		collectText(c, sb)
	}
}

// collectImgAlt finds the first <img> descendant of n and returns its alt
// attribute value, trimmed of whitespace. Returns "" if none found.
func collectImgAlt(n *html.Node) string {
	if n.Type == html.ElementNode && strings.ToLower(n.Data) == "img" {
		return strings.TrimSpace(attrVal(n, "alt"))
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if alt := collectImgAlt(c); alt != "" {
			return alt
		}
	}
	return ""
}

// cleanText normalises whitespace in text nodes.
func cleanText(s string) string {
	// Replace runs of whitespace (including newlines) with a single space
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !inSpace {
				b.WriteRune(' ')
				inSpace = true
			}
		} else {
			b.WriteRune(r)
			inSpace = false
		}
	}
	result := b.String()
	// Only return entirely-whitespace strings as empty
	if strings.TrimSpace(result) == "" {
		return ""
	}
	return result
}

// attrVal returns the value of an attribute on an element node.
func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
