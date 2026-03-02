package views

import (
	"fmt"
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/render"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// ReaderView displays a single email or a full thread as one scrollable document.
//
// Thread mode: thread != nil — all messages rendered oldest→newest, scrolled to latest.
// Single mode: message != nil — classic static-header + scrollable-body layout.
type ReaderView struct {
	theme   *config.Theme
	width   int
	height  int
	thread  *data.Thread  // thread mode
	message *data.Message // single-message mode
	scrollY int
	lines   []string // all rendered lines
	// msgLineOffsets[i] is the first line index of thread.Messages[i].
	msgLineOffsets []int
}

// NewReaderView creates a new reader view.
func NewReaderView(theme *config.Theme) *ReaderView {
	return &ReaderView{theme: theme}
}

// SetSize sets the view dimensions and re-renders cached lines.
func (v *ReaderView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.thread != nil || v.message != nil {
		v.rebuildLines()
	}
}

// SetMessage switches to single-message mode.
func (v *ReaderView) SetMessage(msg *data.Message) {
	v.thread = nil
	v.message = msg
	v.scrollY = 0
	v.buildLines()
}

// SetThread switches to thread mode and scrolls to the latest (bottom) message.
func (v *ReaderView) SetThread(t *data.Thread) {
	v.message = nil
	v.thread = t
	v.scrollY = 0
	v.buildLines()
	// Default scroll position: start of the latest message.
	if len(v.msgLineOffsets) > 1 {
		target := v.msgLineOffsets[len(v.msgLineOffsets)-1]
		max := len(v.lines) - v.bodyHeight()
		if max < 0 {
			max = 0
		}
		if target > max {
			target = max
		}
		v.scrollY = target
	}
}

// UpdateMessageBody updates the body of the message with the given UID in the
// current thread (or single message) and rebuilds lines without resetting scroll.
func (v *ReaderView) UpdateMessageBody(uid uint32, text, html string) {
	if v.thread != nil {
		for _, m := range v.thread.Messages {
			if m.UID == uid {
				m.Body = text
				m.HTMLBody = html
				v.rebuildLines()
				return
			}
		}
	}
	if v.message != nil && v.message.UID == uid {
		v.message.Body = text
		v.message.HTMLBody = html
		v.rebuildLines()
	}
}

// CurrentThread returns the thread being displayed, or nil.
func (v *ReaderView) CurrentThread() *data.Thread { return v.thread }

// CurrentMessage returns the single message being displayed, or nil.
func (v *ReaderView) CurrentMessage() *data.Message { return v.message }

// rebuildLines refreshes the line cache while clamping the current scroll position.
func (v *ReaderView) rebuildLines() {
	old := v.scrollY
	v.buildLines()
	max := len(v.lines) - v.bodyHeight()
	if max < 0 {
		max = 0
	}
	if old > max {
		old = max
	}
	v.scrollY = old
}

// MoveUp scrolls up one line.
func (v *ReaderView) MoveUp() {
	if v.scrollY > 0 {
		v.scrollY--
	}
}

// MoveDown scrolls down one line.
func (v *ReaderView) MoveDown() {
	max := len(v.lines) - v.bodyHeight()
	if max < 0 {
		max = 0
	}
	if v.scrollY < max {
		v.scrollY++
	}
}

// PageUp scrolls up by half a page.
func (v *ReaderView) PageUp() {
	v.scrollY -= v.bodyHeight() / 2
	if v.scrollY < 0 {
		v.scrollY = 0
	}
}

// PageDown scrolls down by half a page.
func (v *ReaderView) PageDown() {
	v.scrollY += v.bodyHeight() / 2
	max := len(v.lines) - v.bodyHeight()
	if max < 0 {
		max = 0
	}
	if v.scrollY > max {
		v.scrollY = max
	}
}

// GoToTop scrolls to the very first line.
func (v *ReaderView) GoToTop() { v.scrollY = 0 }

// GoToBottom scrolls to the last line.
func (v *ReaderView) GoToBottom() {
	max := len(v.lines) - v.bodyHeight()
	if max < 0 {
		max = 0
	}
	v.scrollY = max
}

// headerHeight is the number of rows occupied by the static header above the
// scrollable area. In thread mode headers are embedded in v.lines, so it is 0.
func (v *ReaderView) headerHeight() int {
	if v.thread != nil {
		return 0
	}
	return 5 // From + To + Date + Subject + divider
}

// bodyHeight returns the number of rows available for scrollable content.
func (v *ReaderView) bodyHeight() int {
	h := v.height - v.headerHeight()
	if h < 1 {
		h = 1
	}
	return h
}

// buildLines dispatches to the thread or single-message line builder.
func (v *ReaderView) buildLines() {
	v.msgLineOffsets = nil
	if v.thread != nil {
		v.buildThreadLines()
		return
	}
	v.buildSingleLines()
}

// buildSingleLines renders the body of v.message into v.lines.
func (v *ReaderView) buildSingleLines() {
	if v.message == nil {
		v.lines = nil
		return
	}
	if v.message.Body == "" && v.message.HTMLBody == "" {
		v.lines = []string{fmt.Sprintf("(%s No body loaded — press Enter to fetch)", icons.Download)}
		return
	}
	textWidth := v.width - 2
	if textWidth < 20 {
		textWidth = 20
	}
	v.lines = render.RenderBody(v.message.Body, v.message.HTMLBody, textWidth, v.theme)
}

// buildThreadLines renders all messages in the thread as a single scrollable
// document (oldest first, latest last) and records per-message line offsets.
func (v *ReaderView) buildThreadLines() {
	msgs := v.thread.Messages
	if len(msgs) == 0 {
		v.lines = nil
		return
	}
	v.msgLineOffsets = make([]int, len(msgs))
	var all []string

	for i, msg := range msgs {
		v.msgLineOffsets[i] = len(all)

		if i == 0 {
			all = append(all, v.fullMsgHeader(msg)...)
		} else {
			all = append(all, "") // blank spacer before separator
			all = append(all, v.compactMsgHeader(msg))
		}

		textWidth := v.width - 2
		if textWidth < 20 {
			textWidth = 20
		}
		if msg.Body == "" && msg.HTMLBody == "" {
			all = append(all, fmt.Sprintf("  (%s Loading…)", icons.Syncing))
		} else {
			all = append(all, render.RenderBody(msg.Body, msg.HTMLBody, textWidth, v.theme)...)
		}
	}
	v.lines = all
}

// fullMsgHeader returns the 5-line header: From / To / Date / Subject / divider.
func (v *ReaderView) fullMsgHeader(msg *data.Message) []string {
	theme := v.theme
	divider := lipgloss.NewStyle().Foreground(theme.Border).Render(strings.Repeat("─", v.width))

	labelStyle := lipgloss.NewStyle().Foreground(theme.TextMuted).Width(10).Align(lipgloss.Right)
	valueStyle := lipgloss.NewStyle().Foreground(theme.Text)
	boldStyle := lipgloss.NewStyle().Foreground(theme.Text).Bold(true)

	maxValW := v.width - 12
	if maxValW < 10 {
		maxValW = 10
	}
	maxSubjectW := maxValW - 2
	if maxSubjectW < 5 {
		maxSubjectW = 5
	}

	fromStr := util.TruncateText(util.SingleLine(addressListStr(msg.From)), maxValW)
	toStr := util.TruncateText(util.SingleLine(addressListStr(msg.To)), maxValW)
	dateStr := util.TruncateText(util.FormatDateLong(msg.Date)+"  "+msg.Date.Format("15:04"), maxValW)
	subjectStr := util.TruncateText(util.SingleLine(msg.Subject), maxSubjectW)

	stars := ""
	if msg.IsStarred() {
		stars = lipgloss.NewStyle().Foreground(theme.Starred).Render(" " + icons.Star)
	}

	return []string{
		labelStyle.Render("From") + "  " + valueStyle.Render(fromStr),
		labelStyle.Render("To") + "  " + valueStyle.Render(toStr),
		labelStyle.Render("Date") + "  " + valueStyle.Render(dateStr),
		labelStyle.Render("Subject") + "  " + boldStyle.Render(subjectStr) + stars,
		divider,
	}
}

// compactMsgHeader renders a single decorative separator line used between
// messages in a thread:   ──── Sender Name ──────────────────── Date ────
func (v *ReaderView) compactMsgHeader(msg *data.Message) string {
	theme := v.theme
	dash := lipgloss.NewStyle().Foreground(theme.Border)
	fromStyle := lipgloss.NewStyle().Foreground(theme.Text).Bold(true)
	dateStyle := lipgloss.NewStyle().Foreground(theme.TextMuted)

	maxFromW := v.width / 2
	if maxFromW < 10 {
		maxFromW = 10
	}
	fromStr := util.TruncateText(util.SingleLine(addressListStr(msg.From)), maxFromW)
	dateStr := util.FormatDate(msg.Date)

	fromPart := fromStyle.Render(fromStr)
	datePart := dateStyle.Render(dateStr)

	// layout: "──── " + fromPart + " " + fill + " " + datePart + " ────"
	//          5 cols    fromW     1      fillW   1     dateW     5 cols
	fromW := lipgloss.Width(fromPart)
	dateW := lipgloss.Width(datePart)
	fillW := v.width - 5 - fromW - 1 - 1 - dateW - 5
	if fillW < 1 {
		fillW = 1
	}
	fill := dash.Render(strings.Repeat("─", fillW))

	return dash.Render("──── ") + fromPart + " " + fill + " " + datePart + dash.Render(" ────")
}

// View renders the reader pane.
func (v *ReaderView) View() string {
	theme := v.theme
	if v.thread == nil && v.message == nil {
		empty := lipgloss.NewStyle().Foreground(theme.TextMuted).Render(icons.MailOpen + " No message selected")
		return lipgloss.Place(v.width, v.height, lipgloss.Center, lipgloss.Center, empty)
	}
	if v.thread != nil {
		return v.viewThread()
	}
	return v.viewSingle()
}

// viewThread renders the thread as a scrollable full-height block.
func (v *ReaderView) viewThread() string {
	bh := v.bodyHeight()
	start := v.scrollY
	if start > len(v.lines) {
		start = len(v.lines)
	}
	end := start + bh
	if end > len(v.lines) {
		end = len(v.lines)
	}

	padded := make([]string, bh)
	copy(padded, v.lines[start:end])
	// empty strings already in padded from make

	// Scroll percentage in bottom-right of the last row (only when there is room).
	if len(v.lines) > bh && bh > 0 {
		pct := (v.scrollY + bh) * 100 / len(v.lines)
		if pct > 100 {
			pct = 100
		}
		indicator := lipgloss.NewStyle().
			Foreground(v.theme.TextFaint).
			Render(fmt.Sprintf(" %d%%", pct))
		indW := lipgloss.Width(indicator)
		last := padded[bh-1]
		lastW := lipgloss.Width(last)
		gap := v.width - lastW - indW
		if gap >= 0 {
			// There is room: right-align indicator without overflowing the line.
			padded[bh-1] = last + strings.Repeat(" ", gap) + indicator
		}
		// If gap < 0 the line is already wide enough to overflow — skip the
		// indicator rather than wrapping the line and pushing the header off screen.
	}

	return lipgloss.NewStyle().Width(v.width).Render(strings.Join(padded, "\n"))
}

// viewSingle renders the classic static-header + scrollable-body layout.
func (v *ReaderView) viewSingle() string {
	theme := v.theme

	divider := lipgloss.NewStyle().Foreground(theme.Border).Render(strings.Repeat("─", v.width))
	labelStyle := lipgloss.NewStyle().Foreground(theme.TextMuted).Width(10).Align(lipgloss.Right)
	valueStyle := lipgloss.NewStyle().Foreground(theme.Text)
	boldStyle := lipgloss.NewStyle().Foreground(theme.Text).Bold(true)

	maxValW := v.width - 12
	if maxValW < 10 {
		maxValW = 10
	}
	maxSubjectW := maxValW - 2
	if maxSubjectW < 5 {
		maxSubjectW = 5
	}

	fromStr := util.TruncateText(util.SingleLine(addressListStr(v.message.From)), maxValW)
	toStr := util.TruncateText(util.SingleLine(addressListStr(v.message.To)), maxValW)
	dateStr := util.TruncateText(util.FormatDateLong(v.message.Date)+"  "+v.message.Date.Format("15:04"), maxValW)
	subjectStr := util.TruncateText(util.SingleLine(v.message.Subject), maxSubjectW)

	stars := ""
	if v.message.IsStarred() {
		stars = lipgloss.NewStyle().Foreground(theme.Starred).Render(" " + icons.Star)
	}

	headerStr := strings.Join([]string{
		labelStyle.Render("From") + "  " + valueStyle.Render(fromStr),
		labelStyle.Render("To") + "  " + valueStyle.Render(toStr),
		labelStyle.Render("Date") + "  " + valueStyle.Render(dateStr),
		labelStyle.Render("Subject") + "  " + boldStyle.Render(subjectStr) + stars,
		divider,
	}, "\n")

	bh := v.bodyHeight()
	start := v.scrollY
	if start > len(v.lines) {
		start = len(v.lines)
	}
	end := start + bh
	if end > len(v.lines) {
		end = len(v.lines)
	}

	padded := make([]string, bh)
	copy(padded, v.lines[start:end])

	if len(v.lines) > bh {
		pct := 0
		if len(v.lines) > 0 {
			pct = (v.scrollY + bh) * 100 / len(v.lines)
			if pct > 100 {
				pct = 100
			}
		}
		scrollIndicator := lipgloss.NewStyle().
			Foreground(theme.TextFaint).
			Render(fmt.Sprintf(" %d%%", pct))
		_ = scrollIndicator // retained for future: show in divider row
	}

	bodyStr := lipgloss.NewStyle().
		Width(v.width).
		Render(strings.Join(padded, "\n"))

	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Width(v.width).Render(headerStr),
		bodyStr,
	)
}

func addressListStr(addrs []data.Address) string {
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = a.String()
	}
	return strings.Join(parts, ", ")
}
