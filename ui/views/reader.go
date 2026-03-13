package views

import (
	"encoding/json"
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
	theme      *config.Theme
	width      int
	height     int
	thread     *data.Thread  // thread mode
	message    *data.Message // single-message mode
	event      *data.SuggestedEvent
	eventFocus int
	// attachment focus state (single-message mode only)
	attachFocus       int // -1 = none, 0+ = focused attachment index
	attachActionFocus int // 0=Open, 1=Download, 2=Editor
	scrollY           int
	lines             []string // all rendered lines
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
	v.event = nil
	v.eventFocus = 0
	v.attachFocus = -1
	v.attachActionFocus = 0
	v.scrollY = 0
	v.buildLines()
}

// SetThread switches to thread mode and scrolls to the latest (bottom) message.
func (v *ReaderView) SetThread(t *data.Thread) {
	v.message = nil
	v.thread = t
	v.event = nil
	v.eventFocus = 0
	v.attachFocus = -1
	v.attachActionFocus = 0
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

// SetSuggestedEvent attaches a cached/generated suggested event to the current single-message view.
func (v *ReaderView) SetSuggestedEvent(ev *data.SuggestedEvent) {
	v.event = ev
	v.rebuildLines()
}

// HasFocusableEvent reports whether the reader has a suggested event with copy actions available.
func (v *ReaderView) HasFocusableEvent() bool {
	return v.event != nil && v.event.HasEvent
}

// FocusNextEventAction advances the action button focus (0=Copy Plain, 1=Copy JSON, 2=Reject).
func (v *ReaderView) FocusNextEventAction(delta int) {
	if v.event == nil || !v.event.HasEvent {
		return
	}
	v.eventFocus += delta
	if v.eventFocus < 0 {
		v.eventFocus = 2
	}
	if v.eventFocus > 2 {
		v.eventFocus = 0
	}
	v.rebuildLines()
}

// FocusedEventAction returns the current action identifier: "plain", "json", or "reject".
func (v *ReaderView) FocusedEventAction() string {
	if v.event == nil || !v.event.HasEvent {
		return ""
	}
	switch v.eventFocus {
	case 1:
		return "json"
	case 2:
		return "reject"
	default:
		return "plain"
	}
}

// SuggestedEvent returns the current event suggestion.
func (v *ReaderView) SuggestedEvent() *data.SuggestedEvent { return v.event }

// UpdateMessageBody updates the body of the message with the given UID in the
// current thread (or single message) and rebuilds lines without resetting scroll.
func (v *ReaderView) UpdateMessageBody(uid uint32, text, html string, attachments []data.Attachment) {
	if v.thread != nil {
		for _, m := range v.thread.Messages {
			if m.UID == uid {
				m.Body = text
				m.HTMLBody = html
				m.Attachments = attachments
				v.rebuildLines()
				return
			}
		}
	}
	if v.message != nil && v.message.UID == uid {
		v.message.Body = text
		v.message.HTMLBody = html
		v.message.Attachments = attachments
		v.rebuildLines()
	}
}

// HasAttachments reports whether the current message has any attachments.
func (v *ReaderView) HasAttachments() bool {
	return len(v.messageAttachments()) > 0
}

// AttachFocusActive reports whether an attachment is currently focused.
func (v *ReaderView) AttachFocusActive() bool {
	return v.attachFocus >= 0 && v.attachFocus < len(v.messageAttachments())
}

// FocusNextAttachment cycles attachment focus by delta (+1 or -1).
// -1 means no attachment focused. Cycles: -1 → 0 → … → n-1 → -1.
func (v *ReaderView) FocusNextAttachment(delta int) {
	atts := v.messageAttachments()
	if len(atts) == 0 {
		return
	}
	n := len(atts)
	wasUnfocused := v.attachFocus < 0
	if delta > 0 {
		if v.attachFocus >= n-1 {
			v.attachFocus = -1
		} else {
			v.attachFocus++
		}
	} else {
		if v.attachFocus < 0 {
			v.attachFocus = n - 1
		} else if v.attachFocus == 0 {
			v.attachFocus = -1
		} else {
			v.attachFocus--
		}
	}
	// Auto-scroll to bottom when entering the attachment section.
	if wasUnfocused && v.attachFocus >= 0 {
		v.GoToBottom()
	}
	v.rebuildLines()
}

// FocusNextAttachmentAction cycles the action (Open/Download/Editor) for the focused attachment.
func (v *ReaderView) FocusNextAttachmentAction(delta int) {
	if !v.AttachFocusActive() {
		return
	}
	v.attachActionFocus = (v.attachActionFocus + delta + 3) % 3
	v.rebuildLines()
}

// FocusedAttachmentIndex returns the index of the focused attachment, or -1.
func (v *ReaderView) FocusedAttachmentIndex() int { return v.attachFocus }

// FocusedAttachmentAction returns "open", "download", or "editor" for the focused action button.
func (v *ReaderView) FocusedAttachmentAction() string {
	switch v.attachActionFocus {
	case 1:
		return "download"
	case 2:
		return "editor"
	default:
		return "open"
	}
}

// messageAttachments returns attachments for the current single message or the
// latest message in a thread (which gets the interactive attachment panel).
func (v *ReaderView) messageAttachments() []data.Attachment {
	if v.message != nil {
		return v.message.Attachments
	}
	if v.thread != nil && len(v.thread.Messages) > 0 {
		return v.thread.Messages[len(v.thread.Messages)-1].Attachments
	}
	return nil
}

// AttachmentSource returns the message whose attachments the focus state applies to.
func (v *ReaderView) AttachmentSource() *data.Message {
	if v.message != nil {
		return v.message
	}
	if v.thread != nil && len(v.thread.Messages) > 0 {
		return v.thread.Messages[len(v.thread.Messages)-1]
	}
	return nil
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
	return len(v.singleHeaderLines())
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
	bodyLoaded := v.message.Body != "" || v.message.HTMLBody != ""
	hasAttachments := len(v.message.Attachments) > 0
	if !bodyLoaded && !hasAttachments {
		v.lines = []string{fmt.Sprintf("(%s No body loaded — press Enter to fetch)", icons.Download)}
		return
	}
	textWidth := v.width - 2
	if textWidth < 20 {
		textWidth = 20
	}
	if bodyLoaded {
		v.lines = render.RenderBody(v.message.Body, v.message.HTMLBody, textWidth, v.theme)
	} else {
		v.lines = nil
	}
	v.lines = append(v.lines, v.renderAttachmentLines()...)
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

	divider := lipgloss.NewStyle().Foreground(v.theme.Border).Render(strings.Repeat("─", v.width))
	for i, msg := range msgs {
		v.msgLineOffsets[i] = len(all)
		isLatest := i == len(msgs)-1

		if i == 0 {
			all = append(all, v.fullMsgHeader(msg)...)
		} else {
			all = append(all, "") // blank spacer before separator
			all = append(all, v.compactMsgHeader(msg))
		}

		// Inject suggested event section after the latest message's header.
		if isLatest {
			if extra := v.renderSuggestedEventSection(); len(extra) > 0 {
				if i > 0 {
					// compactMsgHeader has no trailing divider — add one before the event.
					all = append(all, divider)
				}
				all = append(all, extra...)
				all = append(all, divider)
			}
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
		// The latest message's attachments are handled by the interactive
		// section appended below; render all others non-interactively.
		if i < len(msgs)-1 {
			all = append(all, v.renderAttachmentLinesSimple(msg.Attachments)...)
		}
	}
	// Interactive attachment panel for the latest message (supports tab focus).
	all = append(all, v.renderAttachmentLines()...)
	v.lines = all
}

// renderAttachmentLines renders the interactive attachment section for the current
// single-message. Returns nil if there are no attachments.
func (v *ReaderView) renderAttachmentLines() []string {
	atts := v.messageAttachments()
	if len(atts) == 0 {
		return nil
	}
	theme := v.theme
	divider := lipgloss.NewStyle().Foreground(theme.Border).Render(strings.Repeat("─", v.width))
	labelStyle := lipgloss.NewStyle().Foreground(theme.TextMuted).Width(10).Align(lipgloss.Right)
	normalStyle := lipgloss.NewStyle().Foreground(theme.Text)
	focusedStyle := lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)

	maxNameW := v.width - 28
	if maxNameW < 10 {
		maxNameW = 10
	}

	lines := []string{divider}
	for i, att := range atts {
		label := "        "
		if i == 0 {
			label = labelStyle.Render("Attach")
		}
		focused := v.attachFocus == i
		name := util.TruncateText(util.SingleLine(att.Filename), maxNameW)
		size := formatAttachmentSize(len(att.Data))

		rowStyle := normalStyle
		if focused {
			rowStyle = focusedStyle
		}
		row := label + "  " + rowStyle.Render(icons.Attachment+" "+name+"  "+size)
		lines = append(lines, row)
		if focused {
			lines = append(lines, "            "+v.renderAttachmentButtons())
		}
	}
	return lines
}

// renderAttachmentLinesSimple renders attachment names for thread mode (no interactivity).
func (v *ReaderView) renderAttachmentLinesSimple(atts []data.Attachment) []string {
	if len(atts) == 0 {
		return nil
	}
	theme := v.theme
	divider := lipgloss.NewStyle().Foreground(theme.Border).Render(strings.Repeat("─", v.width))
	labelStyle := lipgloss.NewStyle().Foreground(theme.TextMuted).Width(10).Align(lipgloss.Right)
	valueStyle := lipgloss.NewStyle().Foreground(theme.Text)

	maxNameW := v.width - 28
	if maxNameW < 10 {
		maxNameW = 10
	}

	lines := []string{divider}
	for i, att := range atts {
		label := "        "
		if i == 0 {
			label = labelStyle.Render("Attach")
		}
		name := util.TruncateText(util.SingleLine(att.Filename), maxNameW)
		size := formatAttachmentSize(len(att.Data))
		lines = append(lines, label+"  "+valueStyle.Render(icons.Attachment+" "+name+"  "+size))
	}
	return lines
}

func (v *ReaderView) renderAttachmentButtons() string {
	normal := lipgloss.NewStyle().Foreground(v.theme.Text).Background(v.theme.Surface).Padding(0, 1)
	active := lipgloss.NewStyle().Foreground(v.theme.Background).Background(v.theme.Accent).Bold(true).Padding(0, 1)
	open := normal
	download := normal
	editor := normal
	switch v.attachActionFocus {
	case 0:
		open = active
	case 1:
		download = active
	case 2:
		editor = active
	}
	return open.Render("Open") + " " + download.Render("Download") + " " + editor.Render("Editor")
}

func formatAttachmentSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
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
	headerStr := strings.Join(v.singleHeaderLines(), "\n")

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

func (v *ReaderView) singleHeaderLines() []string {
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

	lines := []string{
		labelStyle.Render("From") + "  " + valueStyle.Render(fromStr),
		labelStyle.Render("To") + "  " + valueStyle.Render(toStr),
		labelStyle.Render("Date") + "  " + valueStyle.Render(dateStr),
		labelStyle.Render("Subject") + "  " + boldStyle.Render(subjectStr) + stars,
	}
	lines = append(lines, divider)
	if extra := v.renderSuggestedEventSection(); len(extra) > 0 {
		lines = append(lines, extra...)
		lines = append(lines, divider)
	}
	return lines
}

func (v *ReaderView) renderSuggestedEventSection() []string {
	labelStyle := lipgloss.NewStyle().Foreground(v.theme.TextMuted).Width(10).Align(lipgloss.Right)
	header := labelStyle.Render("Event") + "  " + lipgloss.NewStyle().Foreground(v.theme.Accent).Bold(true).Render(icons.Calendar+" Suggested Event")

	if v.event == nil {
		return []string{
			header,
			"            " + lipgloss.NewStyle().Foreground(v.theme.TextMuted).Render("Loading suggestion..."),
		}
	}
	if !v.event.GenerationOK {
		msg := "No suggested event available"
		if v.event.ParseError != "" {
			msg = util.TruncateText(util.SingleLine(v.event.ParseError), max(20, v.width-14))
		}
		return []string{
			header,
			"            " + lipgloss.NewStyle().Foreground(v.theme.TextMuted).Render(msg),
		}
	}
	if !v.event.HasEvent {
		return []string{
			header,
			"            " + lipgloss.NewStyle().Foreground(v.theme.TextMuted).Render("No event found in this message"),
		}
	}

	// Render the event fields as highlighted rows.
	fieldLabel := lipgloss.NewStyle().Foreground(v.theme.TextMuted)
	fieldValue := lipgloss.NewStyle().Foreground(v.theme.Text)
	titleStyle := lipgloss.NewStyle().Foreground(v.theme.Text).Bold(true)
	indent := "            "

	var out []string
	out = append(out, header)

	// Title row
	summary := util.SingleLine(v.event.Summary)
	if summary != "" {
		out = append(out, indent+titleStyle.Render(summary))
	}

	// Date / time row
	if v.event.AllDay && v.event.Date != "" {
		out = append(out, indent+fieldLabel.Render("Date    ")+fieldValue.Render(util.SingleLine(v.event.Date))+" "+fieldLabel.Render("(all day)"))
	} else if v.event.Date != "" || v.event.Start != "" || v.event.End != "" {
		datePart := util.SingleLine(v.event.Date)
		timePart := strings.TrimSpace(util.SingleLine(v.event.Start) + " – " + util.SingleLine(v.event.End))
		timePart = strings.TrimPrefix(timePart, " – ")
		timePart = strings.TrimSuffix(timePart, " – ")
		row := indent + fieldLabel.Render("Date    ")
		if datePart != "" {
			row += fieldValue.Render(datePart)
		}
		if timePart != "" {
			if datePart != "" {
				row += "  " + fieldLabel.Render("Time    ") + fieldValue.Render(timePart)
			} else {
				row += fieldValue.Render(timePart)
			}
		}
		out = append(out, row)
	}

	// Location row
	if v.event.Location != "" {
		out = append(out, indent+fieldLabel.Render("Location")+fieldValue.Render("  "+util.SingleLine(v.event.Location)))
	}

	// Recurring
	if v.event.Recurring {
		out = append(out, indent+fieldLabel.Render("Repeats ")+fieldValue.Render("  Yes"))
	}

	// Description (wrapped, preserving paragraph breaks)
	if v.event.Description != "" {
		textWidth := v.width - 14
		if textWidth < 20 {
			textWidth = 20
		}
		out = append(out, "")
		for _, line := range strings.Split(v.event.Description, "\n") {
			sanitised := util.SingleLine(line)
			if strings.TrimSpace(sanitised) == "" {
				out = append(out, "")
				continue
			}
			wrapped := util.WrapText(sanitised, textWidth)
			for _, w := range wrapped {
				out = append(out, indent+fieldValue.Render(w))
			}
		}
	}

	// Buttons below the event
	out = append(out, "")
	out = append(out, indent+v.renderEventButtons())
	return out
}

func (v *ReaderView) renderEventButtons() string {
	normal := lipgloss.NewStyle().Foreground(v.theme.Text).Background(v.theme.Surface).Padding(0, 1)
	active := lipgloss.NewStyle().Foreground(v.theme.Background).Background(v.theme.Accent).Bold(true).Padding(0, 1)
	danger := lipgloss.NewStyle().Foreground(v.theme.Background).Background(v.theme.Error).Bold(true).Padding(0, 1)
	copyPlain := normal
	copyJSON := normal
	reject := normal
	switch v.eventFocus {
	case 0:
		copyPlain = active
	case 1:
		copyJSON = active
	case 2:
		reject = danger
	}
	return copyPlain.Render("Copy Plain") + " " + copyJSON.Render("Copy JSON") + " " + reject.Render("Reject")
}

func formatSuggestedEventPlain(ev *data.SuggestedEvent) string {
	if ev == nil {
		return ""
	}
	if !ev.HasEvent {
		return "No suggested event found"
	}
	lines := []string{ev.Summary}
	if ev.Date != "" {
		lines = append(lines, "Date: "+ev.Date)
	}
	if ev.AllDay {
		lines = append(lines, "Time: All Day")
	} else if ev.Start != "" || ev.End != "" {
		lines = append(lines, "Time: "+strings.TrimSpace(ev.Start+" – "+ev.End))
	}
	if ev.Location != "" {
		lines = append(lines, "Location: "+ev.Location)
	}
	if ev.Recurring {
		lines = append(lines, "Repeating: Yes")
	}
	if ev.Description != "" {
		lines = append(lines, "", ev.Description)
	}
	return strings.Join(lines, "\n")
}

func (v *ReaderView) SuggestedEventJSON() string {
	if v.event == nil {
		return ""
	}
	if v.event.JSONText != "" {
		return v.event.JSONText
	}
	b, _ := json.Marshal(map[string]any{
		"hasEvent":    v.event.HasEvent,
		"summary":     v.event.Summary,
		"date":        v.event.Date,
		"start":       v.event.Start,
		"end":         v.event.End,
		"location":    v.event.Location,
		"calendar":    v.event.Calendar,
		"status":      v.event.Status,
		"allDay":      v.event.AllDay,
		"recurring":   v.event.Recurring,
		"description": v.event.Description,
	})
	return string(b)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func addressListStr(addrs []data.Address) string {
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = a.String()
	}
	return strings.Join(parts, ", ")
}
