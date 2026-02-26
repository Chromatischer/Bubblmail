// Package views contains the swappable main-pane views.
package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/util"
)

// InboxView displays a scrollable list of email threads.
// Each thread occupies two rows:
//
//	  ● From Name                    tag1  tag2     Jun 12
//	    Re: Subject truncated…                   (3 msgs)
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
		rows = append(rows, v.renderThread(t, isSelected)...)
	}

	// Pad to full height
	for len(rows) < v.height {
		rows = append(rows, lipgloss.NewStyle().Width(v.width).Render(""))
	}

	return strings.Join(rows[:v.height], "\n")
}

func (v *InboxView) renderThread(t *data.Thread, selected bool) []string {
	w := v.width
	theme := v.theme

	fgMain := theme.Text
	fgMuted := theme.TextMuted
	if selected {
		fgMain = theme.Background
		fgMuted = theme.Background
	}

	// Status indicator: ★ for starred, ● unread, ○ read
	var indicator string
	if t.Starred {
		indicator = lipgloss.NewStyle().Foreground(theme.Starred).Render("★")
	} else if t.HasUnread {
		indicator = lipgloss.NewStyle().Foreground(theme.Unread).Render("●")
	} else {
		indicator = lipgloss.NewStyle().Foreground(fgMuted).Render("○")
	}

	// From field
	fromStr := t.Messages[0].FromString()
	if t.HasUnread {
		if latest := t.Latest(); latest != nil {
			fromStr = latest.FromString()
		}
	}

	// Tags
	tagStr := ""
	if len(t.Tags) > 0 {
		tagStyle := lipgloss.NewStyle().
			Foreground(theme.Background).
			Background(theme.Accent).
			Padding(0, 1)
		var tagParts []string
		for _, tag := range t.Tags {
			if len(tagParts) >= 2 { // show at most 2 tags
				break
			}
			tagParts = append(tagParts, tagStyle.Render(tag))
		}
		tagStr = strings.Join(tagParts, " ")
	}

	// Date
	dateRendered := lipgloss.NewStyle().Foreground(fgMuted).Render(util.FormatDate(t.LastDate))
	dateW := lipgloss.Width(dateRendered)
	tagW := lipgloss.Width(tagStr)
	indW := 2 // "● "

	fromW := w - indW - tagW - dateW - 4
	if fromW < 5 {
		fromW = 5
	}
	fromTrunc := util.TruncateText(fromStr, fromW)

	fromStyle := lipgloss.NewStyle()
	if t.HasUnread && !selected {
		fromStyle = fromStyle.Foreground(theme.Text).Bold(true)
	} else {
		fromStyle = fromStyle.Foreground(fgMain)
	}
	fromRendered := fromStyle.Render(util.PadRight(fromTrunc, fromW))

	// Assemble row 1
	row1Parts := " " + indicator + " " + fromRendered
	if tagStr != "" {
		row1Parts += " " + tagStr
	}
	gap1 := w - lipgloss.Width(row1Parts) - dateW
	if gap1 < 1 {
		gap1 = 1
	}
	row1Style := lipgloss.NewStyle().Width(w)
	if selected {
		row1Style = row1Style.Background(theme.Selected)
	}
	row1 := row1Style.Render(row1Parts + strings.Repeat(" ", gap1) + dateRendered)

	// Row 2: subject + message count
	subject := t.Subject
	if subject == "" {
		if latest := t.Latest(); latest != nil {
			subject = latest.Subject
		}
	}

	countStr := ""
	if len(t.Messages) > 1 {
		countStr = lipgloss.NewStyle().Foreground(fgMuted).Render(fmt.Sprintf("(%d)", len(t.Messages)))
	}
	countW := lipgloss.Width(countStr)

	subjectW := w - 4 - countW - 2
	if subjectW < 5 {
		subjectW = 5
	}
	subjectTrunc := util.TruncateText(subject, subjectW)

	subjectRendered := lipgloss.NewStyle().Foreground(fgMuted).Render("   " + subjectTrunc)

	gap2 := w - lipgloss.Width(subjectRendered) - countW
	if gap2 < 1 {
		gap2 = 1
	}
	row2Style := lipgloss.NewStyle().Width(w)
	if selected {
		row2Style = row2Style.Background(theme.Selected)
	}
	row2 := row2Style.Render(subjectRendered + strings.Repeat(" ", gap2) + countStr)

	return []string{row1, row2}
}

func (v *InboxView) emptyState() string {
	theme := v.theme
	msg := lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Render("No messages")
	hint := lipgloss.NewStyle().
		Foreground(theme.TextFaint).
		Render("Press ctrl+r to sync")
	body := lipgloss.JoinVertical(lipgloss.Center, msg, hint)
	return lipgloss.Place(v.width, v.height, lipgloss.Center, lipgloss.Center, body)
}
