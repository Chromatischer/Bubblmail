package views

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/render"
	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// ReaderView displays a single email or a full thread as one scrollable document.
//
// Thread mode: thread != nil — all messages rendered oldest→newest, scrolled to latest.
// Single mode: message != nil — classic static-header + scrollable-body layout.
type ReaderView struct {
	theme        *config.Theme
	width        int
	height       int
	thread       *data.Thread  // thread mode
	message      *data.Message // single-message mode
	quotesFolded bool          // collapse quoted blocks into summary lines
	event        *data.SuggestedEvent
	eventFocus   int
	// attachment focus state (single-message mode only)
	attachFocus       int // -1 = none, 0+ = focused attachment index
	attachActionFocus int // 0=Open, 1=Download, 2=Editor
	scrollY           int
	lines             []string // all rendered lines
	// msgLineOffsets[i] is the first line index of thread.Messages[i].
	msgLineOffsets []int
	// Button-row tracking for mouse hit testing.
	outerWidth            int // full pane width; v.width is outerWidth minus the scrollbar
	attachButtonScrollRow int // index in v.lines where attach action buttons appear, or -1
	eventButtonScrollRow  int // index in v.lines where event action buttons appear (thread mode), or -1
	eventButtonHeaderRow  int // row within static single-mode header where event buttons appear, or -1
}

// NewReaderView creates a new reader view.
func NewReaderView(theme *config.Theme) *ReaderView {
	return &ReaderView{theme: theme, quotesFolded: true}
}

// ToggleQuoteFolds toggles quote block folding and rebuilds the line cache.
func (v *ReaderView) ToggleQuoteFolds() {
	v.quotesFolded = !v.quotesFolded
	v.rebuildLines()
}

// QuotesFolded reports whether quote blocks are currently collapsed.
func (v *ReaderView) QuotesFolded() bool { return v.quotesFolded }

// SetSize sets the view dimensions and re-renders cached lines.
//
// v.width is the *content* width: the rightmost column of the pane belongs to
// the scroll indicator. Keeping the reservation here means every line builder
// downstream wraps to the right width without knowing the scrollbar exists.
func (v *ReaderView) SetSize(w, h int) {
	v.outerWidth = w
	v.width = w - readerBarW
	if v.width < 1 {
		v.width = 1
	}
	v.height = h
	if v.thread != nil || v.message != nil {
		v.rebuildLines()
	}
}

// readerBarW is the column reserved on the right for the scroll indicator.
// readerBodyPad is the left margin given to message text: body copy set flush
// against the sidebar border is markedly harder to read than the same text
// with two columns of air, and the reader is the one pane that is all prose.
const (
	readerBarW    = 1
	readerBodyPad = 2
)

// readerActionIndent is the column where in-body action buttons start. Both
// the renderer and the mouse hit-tests read it, so the two cannot drift.
const readerActionIndent = 2

// bodyWidth is the column budget for wrapped message text.
func (v *ReaderView) bodyWidth() int {
	w := v.width - readerBodyPad*2
	if w < 20 {
		w = 20
	}
	return w
}

// indent shifts rendered body lines into the body gutter.
func indent(lines []string, n int) []string {
	pad := strings.Repeat(" ", n)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = pad + l
	}
	return out
}

// withScrollbar joins the scroll indicator column onto rendered body rows.
func (v *ReaderView) withScrollbar(rows []string, total, offset int) []string {
	track := components.Scrollbar(v.theme, total, len(rows), offset, len(rows), "")
	out := make([]string, len(rows))
	for i, r := range rows {
		cell := components.Fill(readerBarW, "")
		if i < len(track) {
			cell = track[i]
		}
		out[i] = v.pane().Width(v.width).MaxWidth(v.width).Render(r) + cell
	}
	return out
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
	v.attachButtonScrollRow = -1
	v.eventButtonScrollRow = -1
	v.eventButtonHeaderRow = -1
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
	v.attachButtonScrollRow = -1
	v.eventButtonScrollRow = -1
	v.eventButtonHeaderRow = -1
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

// UpdateMessageBody updates the body of the message with the given folder+UID in the
// current thread (or single message) and rebuilds lines without resetting scroll.
// Both folder and uid must match to avoid cross-folder UID collisions.
func (v *ReaderView) UpdateMessageBody(folder string, uid uint32, text, html string, attachments []data.Attachment) {
	if v.thread != nil {
		for _, m := range v.thread.Messages {
			if m.UID == uid && m.FolderName == folder {
				m.Body = text
				m.HTMLBody = html
				m.Attachments = attachments
				v.rebuildLines()
				return
			}
		}
	}
	if v.message != nil && v.message.UID == uid && v.message.FolderName == folder {
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

// HitTestContent tests a mouse click at (contentY, x), where contentY is
// relative to the top of the reader's allocated area. Returns one of:
// "attach:open", "attach:download", "attach:editor",
// "event:plain", "event:json", "event:reject", or "".
func (v *ReaderView) HitTestContent(contentY, x int) string {
	if v.thread != nil {
		absLine := v.scrollY + contentY
		if v.eventButtonScrollRow >= 0 && absLine == v.eventButtonScrollRow {
			return readerHitTestEventButtons(x)
		}
		if v.attachButtonScrollRow >= 0 && absLine == v.attachButtonScrollRow {
			return readerHitTestAttachButtons(x)
		}
	} else if v.message != nil {
		hh := v.headerHeight()
		if contentY < hh {
			// Click is in the static header — check event buttons.
			if v.eventButtonHeaderRow >= 0 && contentY == v.eventButtonHeaderRow {
				return readerHitTestEventButtons(x)
			}
			return ""
		}
		absLine := v.scrollY + (contentY - hh)
		if v.attachButtonScrollRow >= 0 && absLine == v.attachButtonScrollRow {
			return readerHitTestAttachButtons(x)
		}
	}
	return ""
}

// readerHitTestAttachButtons maps an x coordinate to an attachment button action.
// Buttons are rendered at readerActionIndent: Open(6) space Download(10) space Editor(8).
func readerHitTestAttachButtons(x int) string {
	const indent = readerActionIndent
	if x < indent {
		return ""
	}
	if x < indent+6 {
		return "attach:open"
	}
	if x < indent+7 {
		return ""
	}
	if x < indent+17 {
		return "attach:download"
	}
	if x < indent+18 {
		return ""
	}
	if x < indent+26 {
		return "attach:editor"
	}
	return ""
}

// readerHitTestEventButtons maps an x coordinate to an event button action.
// Buttons are rendered at readerActionIndent: CopyPlain(12) space CopyJSON(11) space Reject(8).
func readerHitTestEventButtons(x int) string {
	const indent = readerActionIndent
	if x < indent {
		return ""
	}
	if x < indent+12 {
		return "event:plain"
	}
	if x < indent+13 {
		return ""
	}
	if x < indent+24 {
		return "event:json"
	}
	if x < indent+25 {
		return ""
	}
	if x < indent+33 {
		return "event:reject"
	}
	return ""
}

// SetAttachActionFocus sets the action button focus directly (0=Open, 1=Download, 2=Editor).
func (v *ReaderView) SetAttachActionFocus(idx int) {
	v.attachActionFocus = idx
	v.rebuildLines()
}

// SetEventFocus sets the event action button focus directly (0=Copy Plain, 1=Copy JSON, 2=Reject).
func (v *ReaderView) SetEventFocus(idx int) {
	v.eventFocus = idx
	v.rebuildLines()
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
		v.lines = []string{v.pane().Foreground(v.theme.TextFaint).
			Render(fmt.Sprintf("  %s No body loaded — press ↵ to fetch", icons.Download))}
		return
	}
	if bodyLoaded {
		plainBody := v.message.Body
		if v.quotesFolded && v.message.HTMLBody == "" && plainBody != "" {
			plainBody = render.FoldQuoteBlocks(plainBody)
		}
		v.lines = indent(render.RenderBody(plainBody, v.message.HTMLBody, v.bodyWidth(), v.theme), readerBodyPad)
	} else {
		v.lines = nil
	}
	baseOffset := len(v.lines)
	attachLines := v.renderAttachmentLines()
	if v.attachButtonScrollRow >= 0 {
		v.attachButtonScrollRow += baseOffset
	}
	v.lines = append(v.lines, attachLines...)
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
	v.eventButtonScrollRow = -1
	var all []string

	// A blank row on the content plane, not a rule. Each block in this pane
	// already carries its own accent bar or section label; a full-width line
	// between them was a second separator doing the same job louder.
	gap := components.Fill(v.width, "")
	for i, msg := range msgs {
		v.msgLineOffsets[i] = len(all)
		isLatest := i == len(msgs)-1

		if i == 0 {
			all = append(all, v.messageHeaderLines(msg)...)
		} else {
			all = append(all, "") // blank spacer before separator
			all = append(all, v.compactMsgHeader(msg))
		}

		// Inject suggested event section after the latest message's header.
		if isLatest {
			if extra := v.renderSuggestedEventSection(); len(extra) > 0 {
				if i > 0 {
					all = append(all, gap)
				}
				all = append(all, extra...)
				// Event buttons are the last line only when the event has actions.
				if v.event != nil && v.event.HasEvent {
					v.eventButtonScrollRow = len(all) - 1
				}
				all = append(all, gap)
			}
		}

		if msg.Body == "" && msg.HTMLBody == "" {
			all = append(all, fmt.Sprintf("  (%s Loading…)", icons.Syncing))
		} else {
			plainBody := msg.Body
			if v.quotesFolded && msg.HTMLBody == "" && plainBody != "" {
				plainBody = render.FoldQuoteBlocks(plainBody)
			}
			all = append(all, indent(render.RenderBody(plainBody, msg.HTMLBody, v.bodyWidth(), v.theme), readerBodyPad)...)
		}
		// The latest message's attachments are handled by the interactive
		// section appended below; render all others non-interactively.
		if i < len(msgs)-1 {
			all = append(all, v.renderAttachmentLinesSimple(msg.Attachments)...)
		}
	}
	// Interactive attachment panel for the latest message (supports tab focus).
	attachBaseOffset := len(all)
	attachLines := v.renderAttachmentLines()
	if v.attachButtonScrollRow >= 0 {
		v.attachButtonScrollRow += attachBaseOffset
	}
	all = append(all, attachLines...)
	v.lines = all
}

// renderAttachmentLines renders the interactive attachment section for the current
// single-message. Returns nil if there are no attachments. Also sets
// v.attachButtonScrollRow to the index of the button row within the returned
// slice (caller must add its own base offset to get the absolute line index).
func (v *ReaderView) renderAttachmentLines() []string {
	atts := v.messageAttachments()
	if len(atts) == 0 {
		v.attachButtonScrollRow = -1
		return nil
	}
	v.attachButtonScrollRow = -1

	rows, buttonRow := v.attachmentRows(atts, v.attachFocus)
	out := []string{components.Fill(v.width, "")}
	out = append(out, components.Card(v.theme, rows, v.width, v.theme.Border)...)
	if buttonRow >= 0 {
		// +len(prefix rows) to convert from card-relative to slice-relative.
		v.attachButtonScrollRow = buttonRow + 2
	}
	return out
}

// renderAttachmentLinesSimple renders attachment names for thread mode (no interactivity).
func (v *ReaderView) renderAttachmentLinesSimple(atts []data.Attachment) []string {
	if len(atts) == 0 {
		return nil
	}
	rows, _ := v.attachmentRows(atts, -1)
	out := []string{components.Fill(v.width, "")}
	return append(out, components.Card(v.theme, rows, v.width, v.theme.Border)...)
}

// attachmentRows renders the shared attachment list body. focus is the index of
// the attachment whose action buttons should be shown, or -1 for none; the
// returned buttonRow is that row's index within rows, or -1.
func (v *ReaderView) attachmentRows(atts []data.Attachment, focus int) (rows []string, buttonRow int) {
	theme := v.theme
	buttonRow = -1

	inner := v.width - readerActionIndent
	if inner < 20 {
		inner = 20
	}

	rows = append(rows, v.pane().Foreground(theme.TextFaint).Bold(true).
		Render(icons.Attachment+" "+strings.ToUpper(util.PluralCount(len(atts), "attachment", "attachments"))))

	for i, att := range atts {
		size := formatAttachmentSize(len(att.Data))
		sizeW := util.VisibleWidth(size)
		nameW := inner - sizeW - 3 // 2 cols between name and size, 1 of trailing air
		if nameW < 8 {
			nameW = 8
		}

		nameSt := v.pane().Foreground(theme.Text).Width(nameW)
		if i == focus {
			nameSt = nameSt.Foreground(theme.Accent).Bold(true)
		}
		rows = append(rows,
			nameSt.Render(util.TruncateText(util.SingleLine(att.Filename), nameW))+
				v.pane().Foreground(theme.TextFaint).Render(size+" "))

		if i == focus {
			buttonRow = len(rows)
			rows = append(rows, v.renderAttachmentButtons())
		}
	}
	return rows, buttonRow
}

func (v *ReaderView) renderAttachmentButtons() string {
	openActive := v.attachActionFocus == 0
	downloadActive := v.attachActionFocus == 1
	editorActive := v.attachActionFocus == 2

	open := components.RenderButton(v.theme, "Open", openActive, false)
	download := components.RenderButton(v.theme, "Download", downloadActive, false)
	editor := components.RenderButton(v.theme, "Editor", editorActive, false)

	return open + " " + download + " " + editor
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

// messageHeaderLines renders the header block for one message:
//
//	▌ Q1 Planning Meeting                                             ★
//	▌ Sarah Chen <sarah@acme.com>  →  Alice Demo
//	▌ Thursday, August 20, 2026 · 09:00                     󰁦 2 files
//	────────────────────────────────────────────────────────────────────
//
// The subject leads because it is what the reader came for; the old layout put
// three routing fields above it and made the headline the fourth line down.
// The accent bar is the same Card idiom used by the suggested-event block, so
// the two read as siblings rather than as unrelated header styles.
func (v *ReaderView) messageHeaderLines(msg *data.Message) []string {
	theme := v.theme

	// Card prepends "▌ " — two columns off the content budget.
	const barW = 2
	inner := v.width - barW
	if inner < 20 {
		inner = 20
	}

	// ── Line 1: subject, with the star pinned right ──────────────────────────
	star, starW := "", 0
	if msg.IsStarred() {
		star = v.pane().Foreground(theme.Starred).Render(icons.Star)
		starW = util.VisibleWidth(icons.Star) + 1
	}
	subject := util.SingleLine(msg.Subject)
	if strings.TrimSpace(subject) == "" {
		subject = "(no subject)"
	}
	subject = util.TruncateText(subject, inner-starW)
	subjLine := v.pane().
		Foreground(theme.Text).Bold(true).
		Width(inner - starW).
		Render(subject)
	if star != "" {
		subjLine += " " + star
	}

	// ── Line 2: sender → recipients ──────────────────────────────────────────
	arrow := v.pane().Foreground(theme.TextFaint).Render("  " + icons.ArrowRight + "  ")
	arrowW := util.VisibleWidth("  " + icons.ArrowRight + "  ")

	from := util.SingleLine(addressListStr(msg.From))
	to := util.SingleLine(addressListStr(msg.To))

	fromW := inner
	toW := 0
	if to != "" {
		// The sender is the more useful half, so it gets the larger share and
		// the recipient list absorbs the truncation.
		fromW = (inner - arrowW) * 3 / 5
		if fromW < 10 {
			fromW = 10
		}
		toW = inner - arrowW - fromW
		if toW < 0 {
			toW = 0
		}
	}
	// The sender's hue is the same one the inbox put on this thread's unread
	// dot, so opening a message confirms what the list already said.
	peopleLine := v.pane().Foreground(theme.Hue(msg.FromKey())).Bold(true).
		Render(util.TruncateText(from, fromW))
	if to != "" && toW > 3 {
		peopleLine += arrow + v.pane().Foreground(theme.TextMuted).
			Render(util.TruncateText(to, toW))
	}

	// ── Line 3: date, with the attachment tally pinned right ─────────────────
	var attachStr string
	attachW := 0
	if n := len(msg.Attachments); n > 0 {
		plain := icons.Attachment + " " + util.PluralCount(n, "file", "files")
		attachStr = v.pane().Foreground(theme.TextMuted).Render(plain)
		attachW = util.VisibleWidth(plain)
	}
	dateStr := util.FormatDateLong(msg.Date) + " · " + msg.Date.Format("15:04")
	dateLine := v.pane().
		Foreground(theme.TextMuted).
		Width(inner - attachW).
		Render(util.TruncateText(dateStr, inner-attachW))
	if attachStr != "" {
		dateLine += attachStr
	}

	lines := components.Card(theme, []string{subjLine, peopleLine, dateLine}, v.width, theme.Hue(msg.FromKey()))
	// A blank row on the content plane closes the card. A full-width rule here
	// competed with the card's own accent bar: two separators for one break.
	return append(lines, components.Fill(v.width, ""))
}

// compactMsgHeader renders the separator between messages inside a thread:
//
//	SARAH CHEN ─────────────────────────────────────────   Mon 09:41
//
// It is the same small-caps-label-plus-rule idiom the inbox uses for its date
// groups, so "a new block starts here" looks the same in both panes.
func (v *ReaderView) compactMsgHeader(msg *data.Message) string {
	theme := v.theme

	dateStr := util.FormatDate(msg.Date) + " " + msg.Date.Format("15:04")
	dateW := util.VisibleWidth(dateStr) + 3 // 2 cols of lead-in, 1 of trailing air

	labelW := v.width - dateW
	if labelW < 8 {
		labelW = 8
		dateW = 0
		dateStr = ""
	}

	from := util.SingleLine(addressListStr(msg.From))
	line := components.TintedLabel(theme, from, labelW, "", theme.Hue(msg.FromKey()))
	if dateStr != "" {
		line += v.pane().
			Foreground(theme.TextMuted).
			Width(dateW).
			Align(lipgloss.Right).
			Render(dateStr + " ")
	}
	return line
}

// View renders the reader pane.
// pane returns the base style for a line of reader content. It sets no
// background: the content area is transparent, so a terminal with a background
// image or an alpha channel shows through the mail rather than behind it.
func (v *ReaderView) pane() lipgloss.Style {
	return lipgloss.NewStyle()
}

func (v *ReaderView) View() string {
	theme := v.theme
	if v.thread == nil && v.message == nil {
		return components.EmptyState(theme, v.outerWidth, v.height,
			icons.MailOpen, "No message selected", "pick a thread and press ↵")
	}
	if v.thread != nil {
		return v.pad(v.viewThread())
	}
	return v.pad(v.viewSingle())
}

// pad squares the reader off to the full pane height so the rows below the
// message are blank rather than absent. It adds no fill of its own.
func (v *ReaderView) pad(body string) string {
	rows := strings.Split(body, "\n")
	for len(rows) < v.height {
		rows = append(rows, "")
	}
	return strings.Join(rows, "\n")
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

	return strings.Join(v.withScrollbar(padded, len(v.lines), v.scrollY), "\n")
}

// viewSingle renders the classic static-header + scrollable-body layout.
func (v *ReaderView) viewSingle() string {
	headerLines := v.singleHeaderLines()

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

	// The header keeps the full pane width; only the scrolling body carries a
	// position indicator, because only the body scrolls.
	rows := make([]string, 0, len(headerLines)+bh)
	for _, h := range headerLines {
		rows = append(rows, v.pane().Width(v.outerWidth).MaxWidth(v.outerWidth).Render(h))
	}
	rows = append(rows, v.withScrollbar(padded, len(v.lines), v.scrollY)...)

	return strings.Join(rows, "\n")
}

func (v *ReaderView) singleHeaderLines() []string {
	lines := v.messageHeaderLines(v.message)

	v.eventButtonHeaderRow = -1
	if extra := v.renderSuggestedEventSection(); len(extra) > 0 {
		// Event buttons are the last line only when the event has actions.
		if v.event != nil && v.event.HasEvent {
			v.eventButtonHeaderRow = len(lines) + len(extra) - 1
		}
		lines = append(lines, extra...)
		lines = append(lines, components.Fill(v.width, ""))
	}
	return lines
}

func (v *ReaderView) renderSuggestedEventSection() []string {
	theme := v.theme
	inner := v.width - readerActionIndent
	if inner < 20 {
		inner = 20
	}

	title := v.pane().Foreground(theme.TextFaint).Bold(true).
		Render(icons.Calendar + " SUGGESTED EVENT")

	// Non-event states are a single explanatory line under the same bar, so the
	// block does not change shape as the suggestion resolves.
	note := func(msg string) []string {
		return components.Card(theme, []string{
			title,
			v.pane().Foreground(theme.TextMuted).
				Render(util.TruncateText(util.SingleLine(msg), inner)),
		}, v.width, theme.AccentSoft)
	}

	switch {
	case v.event == nil:
		return note(icons.Syncing + " Looking for an event…")
	case !v.event.GenerationOK:
		if v.event.ParseError != "" {
			return note(v.event.ParseError)
		}
		return note("No suggested event available")
	case !v.event.HasEvent:
		return note("No event found in this message")
	}

	ev := v.event
	muted := v.pane().Foreground(theme.TextMuted)

	body := []string{title}

	if summary := util.SingleLine(ev.Summary); summary != "" {
		body = append(body, v.pane().Foreground(theme.Text).Bold(true).
			Render(util.TruncateText(summary, inner)))
	}

	// When / how often, on one line — two short facts do not need two rows.
	when := ""
	switch {
	case ev.AllDay && ev.Date != "":
		when = util.SingleLine(ev.Date) + " · all day"
	default:
		when = util.SingleLine(ev.Date)
		times := strings.Trim(strings.TrimSpace(
			util.SingleLine(ev.Start)+" – "+util.SingleLine(ev.End)), "– ")
		if times != "" {
			if when != "" {
				when += " · "
			}
			when += times
		}
	}
	if ev.Recurring {
		when = strings.TrimSpace(when + " · repeats")
	}
	if when != "" {
		body = append(body, muted.Render(icons.Clock+" "+util.TruncateText(when, inner-2)))
	}
	if ev.Location != "" {
		body = append(body, muted.Render(icons.Pin+" "+
			util.TruncateText(util.SingleLine(ev.Location), inner-2)))
	}

	if ev.Description != "" {
		body = append(body, "")
		for _, line := range strings.Split(ev.Description, "\n") {
			sanitised := util.SingleLine(line)
			if strings.TrimSpace(sanitised) == "" {
				body = append(body, "")
				continue
			}
			for _, w := range util.WrapText(sanitised, inner) {
				body = append(body, v.pane().Foreground(theme.Text).Render(w))
			}
		}
	}

	body = append(body, "", v.renderEventButtons())
	return components.Card(theme, body, v.width, theme.AccentSoft)
}

func (v *ReaderView) renderEventButtons() string {
	gap := " "
	return components.RenderButton(v.theme, "Copy Plain", v.eventFocus == 0, false) + gap +
		components.RenderButton(v.theme, "Copy JSON", v.eventFocus == 1, false) + gap +
		components.RenderButton(v.theme, "Reject", v.eventFocus == 2, true)
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
