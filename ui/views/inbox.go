// Package views contains the swappable main-pane views.
package views

import (
	"fmt"
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// InboxView displays a scrollable list of email threads.
// Each thread occupies two rows:
//
//	● From Name                    tag1  tag2     Jun 12
//	  Re: Subject truncated…                   (3 msgs)
type InboxView struct {
	theme   *config.Theme
	width   int
	height  int
	threads []*data.Thread
	cursor  int // focused thread index
	offset  int // first visible thread
}

// NewInboxView creates a new inbox view.
func NewInboxView(theme *config.Theme) *InboxView {
	return &InboxView{theme: theme}
}

// SetSize sets the view dimensions.
func (v *InboxView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

// SetThreads updates the thread list and resets scroll.
func (v *InboxView) SetThreads(threads []*data.Thread) {
	v.threads = threads
	v.cursor = 0
	v.offset = 0
}

// AppendThreads replaces the thread list while preserving cursor position.
func (v *InboxView) AppendThreads(threads []*data.Thread) {
	v.threads = threads
	if v.cursor >= len(v.threads) {
		v.cursor = len(v.threads) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

// Len returns the number of threads loaded.
func (v *InboxView) Len() int { return len(v.threads) }

// CursorPos returns the current cursor index.
func (v *InboxView) CursorPos() int { return v.cursor }

// SelectedThread returns the currently focused thread, or nil.
func (v *InboxView) SelectedThread() *data.Thread {
	if v.cursor < 0 || v.cursor >= len(v.threads) {
		return nil
	}
	return v.threads[v.cursor]
}

// MoveUp moves cursor up.
func (v *InboxView) MoveUp() {
	if v.cursor > 0 {
		v.cursor--
		if v.cursor < v.offset {
			v.offset--
		}
	}
}

// MoveDown moves cursor down.
func (v *InboxView) MoveDown() {
	if v.cursor < len(v.threads)-1 {
		v.cursor++
		rowsPerThread := 2
		visibleThreads := v.height / rowsPerThread
		if visibleThreads < 1 {
			visibleThreads = 1
		}
		if v.cursor >= v.offset+visibleThreads {
			v.offset++
		}
	}
}

// PageUp moves cursor up by a page.
func (v *InboxView) PageUp() {
	rowsPerThread := 2
	page := v.height / rowsPerThread
	if page < 1 {
		page = 1
	}
	v.cursor -= page
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.offset = v.cursor
}

// PageDown moves cursor down by a page.
func (v *InboxView) PageDown() {
	rowsPerThread := 2
	page := v.height / rowsPerThread
	if page < 1 {
		page = 1
	}
	v.cursor += page
	if v.cursor >= len(v.threads) {
		v.cursor = len(v.threads) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.offset = v.cursor
}

// GoToTop jumps to the first thread.
func (v *InboxView) GoToTop() {
	v.cursor = 0
	v.offset = 0
}

// GoToBottom jumps to the last thread.
func (v *InboxView) GoToBottom() {
	if len(v.threads) == 0 {
		return
	}
	v.cursor = len(v.threads) - 1
	rowsPerThread := 2
	visibleThreads := v.height / rowsPerThread
	if visibleThreads < 1 {
		visibleThreads = 1
	}
	v.offset = v.cursor - visibleThreads + 1
	if v.offset < 0 {
		v.offset = 0
	}
}

// View renders the inbox thread list.
func (v *InboxView) View() string {
	if len(v.threads) == 0 {
		return v.emptyState()
	}

	rowsPerThread := 2
	visibleThreads := v.height / rowsPerThread
	if visibleThreads < 1 {
		visibleThreads = 1
	}

	end := v.offset + visibleThreads
	if end > len(v.threads) {
		end = len(v.threads)
	}

	var rows []string
	for i := v.offset; i < end; i++ {
		t := v.threads[i]
		isSelected := i == v.cursor
		rows = append(rows, v.renderThread(t, isSelected, i)...)
	}

	// Pad to full height
	for len(rows) < v.height {
		rows = append(rows, lipgloss.NewStyle().Width(v.width).Render(""))
	}

	return strings.Join(rows[:v.height], "\n")
}

func (v *InboxView) renderThread(t *data.Thread, selected bool, index int) []string {
	w := v.width
	theme := v.theme

	rowBg := lipgloss.Color("")
	if !selected && index%2 == 1 {
		rowBg = theme.Surface
	}

	// sel applies the selection background to any style when this row is selected.
	// Every pre-rendered span must go through sel() so that ANSI resets inside
	// them don't leave gaps in the selection highlight.
	sel := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(theme.Selected)
		}
		if rowBg != "" {
			return s.Background(rowBg)
		}
		return s
	}

	fgMain := theme.Text
	fgMuted := theme.TextMuted
	if selected {
		fgMain = theme.Background
		fgMuted = theme.Background
	}

	// Flag column: accent ▌ bar for starred/flagged messages, blank otherwise
	flagCh := " "
	if t.Starred {
		flagCh = icons.Flag
	}
	flag := sel(lipgloss.NewStyle().Foreground(theme.Starred)).Render(flagCh)

	// Status dot: ● unread, blank for read
	var dot string
	if t.HasUnread {
		dot = sel(lipgloss.NewStyle().Foreground(theme.Unread)).Render(icons.Unread)
	} else {
		dot = sel(lipgloss.NewStyle().Foreground(fgMuted)).Render(" ")
	}

	// From field
	fromStr := util.SingleLine(t.Messages[0].FromString())
	if t.HasUnread {
		if latest := t.Latest(); latest != nil {
			fromStr = util.SingleLine(latest.FromString())
		}
	}

	// Tags
	var tagStr string
	if len(t.Tags) > 0 {
		tagStyle := lipgloss.NewStyle().
			Foreground(theme.Background).
			Background(theme.Accent).
			Padding(0, 1)
		minFromW := 5
		maxTagW := w - 3 - lipgloss.Width(util.FormatDate(t.LastDate)) - 2 - minFromW
		if maxTagW > 0 {
			var tagParts []string
			usedW := 0
			for _, tag := range t.Tags {
				if len(tagParts) >= 2 { // show at most 2 tags
					break
				}
				remaining := maxTagW - usedW
				if len(tagParts) > 0 {
					remaining--
				}
				if remaining <= 2 {
					break
				}
				labelMax := remaining - 2
				if labelMax < 1 {
					break
				}
				label := util.TruncateText(util.SingleLine(tag), labelMax)
				part := tagStyle.Render(label)
				partW := util.VisibleWidth(part)
				if partW > remaining {
					labelMax = remaining - 2
					if labelMax < 1 {
						break
					}
					label = util.TruncateText(label, labelMax)
					part = tagStyle.Render(label)
					partW = util.VisibleWidth(part)
				}
				tagParts = append(tagParts, part)
				usedW += partW
				if len(tagParts) > 0 {
					usedW++
				}
			}
			tagStr = strings.Join(tagParts, " ")
		}
	}

	// Date — right side of row 1. dateW is the plain visible width (no ANSI).
	dateStr := util.FormatDate(t.LastDate)
	dateW := util.VisibleWidth(dateStr)
	tagW := util.VisibleWidth(tagStr)

	// Prefix: flag(1) + space(1) + dot(1) + space(1) = 4 cols, always fixed.
	const prefixW = 4
	spaceBeforeTags := 0
	if tagStr != "" {
		spaceBeforeTags = 1
	}
	fromW := w - prefixW - tagW - dateW - spaceBeforeTags - 1 // -1 for rightPad
	if fromW < 5 {
		fromW = 5
	}
	fromTrunc := util.TruncateText(fromStr, fromW)

	fromStyle := lipgloss.NewStyle().Width(fromW)
	if t.HasUnread && !selected {
		fromStyle = sel(fromStyle.Foreground(theme.Text).Bold(true))
	} else {
		fromStyle = sel(fromStyle.Foreground(fgMain))
	}
	fromRendered := fromStyle.Render(fromTrunc)

	// Row 1: each segment has a fixed known width; no gap measurement needed.
	// prefix(4) + from(fromW) [+ space(1) + tags(tagW)] + date(dateW) + pad(1) = w
	row1 := sel(lipgloss.NewStyle().Width(w)).Render(
		flag + sel(lipgloss.NewStyle()).Render(" ") +
			dot + sel(lipgloss.NewStyle()).Render(" ") +
			fromRendered +
			func() string {
				if tagStr != "" {
					return " " + tagStr
				}
				return ""
			}() +
			sel(lipgloss.NewStyle().Foreground(fgMuted)).Render(dateStr) +
			sel(lipgloss.NewStyle()).Render(" "),
	)

	// Row 2: indent + subject + gap + count
	subject := util.SingleLine(t.Subject)
	if subject == "" {
		if latest := t.Latest(); latest != nil {
			subject = util.SingleLine(latest.Subject)
		}
	}

	countStr := ""
	if len(t.Messages) > 1 {
		countStr = sel(lipgloss.NewStyle().Foreground(fgMuted)).Render(fmt.Sprintf("(%d)", len(t.Messages)))
	}
	countW := util.VisibleWidth(countStr)

	// Row 2: [indent(prefixW) + subject(textW)] + count(countW) + pad(1) = w
	// The subject cell is Width(prefixW+textW) so lipgloss self-pads it exactly.
	textW := w - prefixW - countW - 1 // visible cols available for subject text
	if textW < 1 {
		textW = 1
	}
	subjectTrunc := util.TruncateText(subject, textW)
	indent := strings.Repeat(" ", prefixW)
	subjectRendered := sel(lipgloss.NewStyle().Foreground(fgMuted).Width(prefixW + textW)).Render(indent + subjectTrunc)

	row2 := sel(lipgloss.NewStyle().Width(w)).Render(subjectRendered + countStr + sel(lipgloss.NewStyle()).Render(" "))

	return []string{row1, row2}
}

func (v *InboxView) emptyState() string {
	theme := v.theme
	msg := lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Render(icons.Inbox + " No messages")
	hint := lipgloss.NewStyle().
		Foreground(theme.TextFaint).
		Render(fmt.Sprintf("%s Press ctrl+r to sync", icons.Refresh))
	body := lipgloss.JoinVertical(lipgloss.Center, msg, hint)
	return lipgloss.Place(v.width, v.height, lipgloss.Center, lipgloss.Center, body)
}
