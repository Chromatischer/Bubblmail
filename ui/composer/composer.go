package composer

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	outsmtp "github.com/bubblmail/bubblmail/smtp"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Result holds the outcome of a composer interaction.
type Result struct {
	Action string // "send", "cancel", "save-draft"
	Draft  *outsmtp.ComposedMessage
}

// savedDraftState holds the stashed composer state for the continue/discard prompt.
type savedDraftState struct {
	fields      []*Field
	from        data.Address
	mode        string
	attachments []Attachment
	anonymize   bool
	focused     int
	bodyTop     int
}

// Composer is the full-screen email composition overlay.
type Composer struct {
	theme   *config.Theme
	width   int
	height  int
	active  bool
	result  *Result
	focused int // 0=To, 1=CC, 2=Subject, 3=Body, 4=Attachments
	mode    string
	bodyTop int

	fields []*Field
	from   data.Address

	// File attachments
	attachments  []Attachment
	filePicker   *filePicker
	anonymize    bool
	attachCursor int // selected index in attachment list when focused==4

	// Draft persistence: stash when closing with content, prompt on reopen.
	savedDraft *savedDraftState
	prompting  bool // showing the continue/discard prompt
}

const (
	composerLabelWidth = 10
	composerFieldSep   = " │ "
)

// NewComposer creates a new composer overlay.
func NewComposer(theme *config.Theme) *Composer {
	return &Composer{
		theme:      theme,
		filePicker: newFilePicker(theme),
	}
}

// openWith is the shared opener that sets active, resets state, and then
// shows the continue/discard prompt when a saved draft exists.
func (c *Composer) openWith(from data.Address, fields []*Field, mode string, focusedField int) {
	c.from = from
	c.active = true
	c.result = nil
	c.bodyTop = 0
	c.attachments = nil
	c.anonymize = false
	c.attachCursor = 0

	if c.savedDraft != nil {
		// Stash the intended fresh fields so "Discard" can load them.
		// We show the prompt immediately; restoreDraft / discard will swap fields.
		c.fields = fields
		c.focused = focusedField
		c.mode = mode
		c.prompting = true
		return
	}

	c.fields = fields
	c.focused = focusedField
	c.mode = mode
	c.prompting = false
}

// OpenNew opens the composer for a new email.
func (c *Composer) OpenNew(from data.Address) {
	fields := []*Field{
		{Label: "To", Kind: FieldText},
		{Label: "CC", Kind: FieldText},
		{Label: "Subject", Kind: FieldText},
		{Label: "Body", Kind: FieldTextArea},
	}
	c.openWith(from, fields, "New Message", 0)
}

// OpenReply opens the composer pre-filled for a reply.
func (c *Composer) OpenReply(from data.Address, orig *data.Message, replyAll bool) {
	mode := "Reply"
	if replyAll {
		mode = "Reply All"
	}

	replyTo := primaryReplyAddress(orig)
	to := replyTo.Address
	cc := ""
	if replyAll {
		cc = addressListString(filterSelf(orig.To, from.Address))
	}
	subject := orig.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	body := quoteBody(orig)

	fields := []*Field{
		{Label: "To", Kind: FieldText, Value: to, cursor: len([]rune(to))},
		{Label: "CC", Kind: FieldText, Value: cc, cursor: len([]rune(cc))},
		{Label: "Subject", Kind: FieldText, Value: subject, cursor: len([]rune(subject))},
		{Label: "Body", Kind: FieldTextArea, Value: body, cursor: 0},
	}
	c.openWith(from, fields, mode, 3)
}

// OpenForward opens the composer pre-filled for a forwarded message.
func (c *Composer) OpenForward(from data.Address, orig *data.Message) {
	subject := orig.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "fwd:") {
		subject = "Fwd: " + subject
	}
	body := forwardBody(orig)

	fields := []*Field{
		{Label: "To", Kind: FieldText},
		{Label: "CC", Kind: FieldText},
		{Label: "Subject", Kind: FieldText, Value: subject, cursor: len([]rune(subject))},
		{Label: "Body", Kind: FieldTextArea, Value: body, cursor: 0},
	}
	c.openWith(from, fields, "Forward", 0)
}

// IsActive returns true if the composer is open.
func (c *Composer) IsActive() bool {
	return c.active
}

// Result returns the composer result (non-nil when done).
func (c *Composer) Result() *Result {
	return c.result
}

// ClearResult clears the result.
func (c *Composer) ClearResult() {
	c.result = nil
}

// SetSize sets the overlay dimensions.
func (c *Composer) SetSize(w, h int) {
	c.width = w
	c.height = h
	c.ensureBodyVisible()
}

// HandleKey processes a key press.
func (c *Composer) HandleKey(key string) {
	if !c.active {
		return
	}

	// Continue/Discard prompt intercepts all keys.
	if c.prompting {
		switch key {
		case "enter", "c", "C":
			// Continue — restore the saved draft.
			c.restoreDraft()
		case "d", "D", "n", "N", "esc":
			// Discard — clear the saved draft and open fresh.
			c.savedDraft = nil
			c.prompting = false
		}
		return
	}

	// If file picker is open, route all keys to it.
	if c.filePicker.isActive() {
		path, isDir, accepted, _ := c.filePicker.handleKey(key)
		if accepted {
			c.addAttachment(path, isDir)
		}
		return
	}

	if c.focused < 0 || c.focused > len(c.fields) {
		c.focused = 0
	}

	// ── Attachment list focus ─────────────────────────────────────────────
	if c.focused == len(c.fields) { // attachment list
		switch key {
		case "esc":
			c.cancelOrSaveDraft()
			return
		case "ctrl+s", "ctrl+enter":
			c.submit()
			return
		case "tab":
			c.focused = 0
			return
		case "shift+tab":
			c.focused = len(c.fields) - 1
			return
		case "up":
			if c.attachCursor > 0 {
				c.attachCursor--
			}
			return
		case "down":
			if c.attachCursor < len(c.attachments)-1 {
				c.attachCursor++
			}
			return
		case "backspace", "delete":
			c.removeAttachment(c.attachCursor)
			return
		case "ctrl+r":
			c.anonymize = !c.anonymize
			return
		}
		return
	}

	f := c.fields[c.focused]

	switch key {
	case "ctrl+s", "ctrl+enter":
		c.submit()
		return
	case "esc":
		c.cancelOrSaveDraft()
		return
	case "tab":
		next := c.focused + 1
		if next == len(c.fields) && len(c.attachments) == 0 {
			next = 0
		}
		c.focused = next % (len(c.fields) + 1)
		return
	case "shift+tab":
		prev := c.focused - 1
		if prev < 0 {
			if len(c.attachments) > 0 {
				c.focused = len(c.fields)
			} else {
				c.focused = len(c.fields) - 1
			}
			return
		}
		c.focused = prev
		return
	case "ctrl+r":
		c.anonymize = !c.anonymize
		return
	case "up":
		if f.Kind == FieldTextArea {
			f.cursorMoveLines(-1)
		}
	case "down":
		if f.Kind == FieldTextArea {
			f.cursorMoveLines(1)
		}
	case "pgup":
		c.scrollBody(-1)
		return
	case "pgdn":
		c.scrollBody(1)
		return
	case "enter":
		if f.Kind == FieldTextArea {
			f.insert("\n")
		} else {
			c.focused = (c.focused + 1) % len(c.fields)
		}
	case "backspace", "ctrl+h":
		f.backspace()
	case "left":
		f.cursorLeft()
	case "right":
		f.cursorRight()
	case "home", "ctrl+a":
		f.cursorHome()
	case "end", "ctrl+e":
		f.cursorEnd()
	default:
		if len(key) == 1 && key[0] >= 32 {
			// Trigger file picker when @ is typed in the body field.
			if key == "@" && c.focused == 3 {
				c.filePicker.activate()
				return
			}
			f.insert(key)
		}
	}
	c.ensureBodyVisible()
}

// addAttachment attaches a path. If isDir is true, zip it first.
func (c *Composer) addAttachment(path string, isDir bool) {
	if isDir {
		zipPath, err := zipDir(path)
		if err != nil {
			return // silently ignore; could surface as status msg
		}
		c.attachments = append(c.attachments, Attachment{
			Path:        zipPath,
			DisplayName: filepath.Base(path) + ".zip",
			IsDir:       true,
			Compressed:  true,
			TempFile:    true,
		})
	} else {
		c.attachments = append(c.attachments, Attachment{
			Path:        path,
			DisplayName: filepath.Base(path),
		})
	}
}

// removeAttachment removes the attachment at idx, clamping the cursor.
func (c *Composer) removeAttachment(idx int) {
	if idx < 0 || idx >= len(c.attachments) {
		return
	}
	a := c.attachments[idx]
	if a.TempFile {
		_ = os.Remove(a.Path)
	}
	c.attachments = append(c.attachments[:idx], c.attachments[idx+1:]...)
	if c.attachCursor >= len(c.attachments) && c.attachCursor > 0 {
		c.attachCursor--
	}
	if len(c.attachments) == 0 && c.focused == len(c.fields) {
		c.focused = len(c.fields) - 1
	}
}

// IsPrompting returns true while the continue/discard draft prompt is showing.
func (c *Composer) IsPrompting() bool { return c.prompting }

// BodyDragZone returns the screen-coordinate bounding box of the body text
// content — the only area that makes sense to drag-select for copying.
// headerH is the height of the app header so the caller can convert from
// content-area to screen coordinates.
//
// Geometry (all in screen coords, y=0 is terminal top):
//   x: skip left-border(1) + left-pad(2) + label(10) + sep(3) = 16 cols from box left
//   y: border(1)+pad(1)+title(1)+from(1)+divider(1)+3fields(3)+sectionDiv(1) = 9 rows down
func (c *Composer) BodyDragZone(headerH int) (x0, y0, x1, y1 int) {
	if !c.active || c.prompting {
		return
	}
	boxWidth := c.width - 16
	if boxWidth > 110 {
		boxWidth = 110
	}
	if boxWidth < 60 {
		boxWidth = 60
	}
	boxX0 := (c.width - boxWidth - 2) / 2
	// x: past left-border(1) + left-pad(2) + label(composerLabelWidth) + sep(" │ "=3)
	x0 = boxX0 + 1 + 2 + composerLabelWidth + 3
	// x: last content col, before right-pad(2) + right-border(1)
	x1 = boxX0 + boxWidth - 2
	// y: 9 rows of overhead (see comment above), then bodyHeight rows of text
	y0 = headerH + 9
	y1 = y0 + c.bodyHeight() - 1
	return
}

// SetFocus moves keyboard focus to the given field index (0=To,1=CC,2=Subject,3=Body).
func (c *Composer) SetFocus(field int) {
	if field < 0 || field > len(c.fields) {
		return
	}
	c.focused = field
	c.ensureBodyVisible()
}

// HitTestField returns the field index for a click at the given content-area y,
// or -1 if no field was hit.
//
// Geometry: composer box is Top-aligned (boxY0=0). Inside the box:
// border(1)+padding(1)=2 rows overhead, then title(1)+from(1)+divider(1)=3 more.
// Fields start at content-area y=5: To(5), CC(6), Subject(7), sectionDiv(8), Body(9+).
func (c *Composer) HitTestField(contentY int) int {
	if !c.active || c.prompting {
		return -1
	}
	switch contentY {
	case 5:
		return 0 // To
	case 6:
		return 1 // CC
	case 7:
		return 2 // Subject
	}
	// Body occupies rows 9 through 8+bodyHeight.
	bodyY0 := 9
	if contentY >= bodyY0 && contentY < bodyY0+c.bodyHeight() {
		return 3
	}
	return -1
}

// HitTestDraftPrompt returns "c" (Continue) or "d" (Discard) for a click on
// the hint row of the draft prompt dialog, or "" if not on that row.
//
// Geometry: prompt box has 5 content rows + 2 padding + 2 border = 9 rows total;
// boxY0 = (height-9)/2. Hint row is at boxY0+6. Left half = Continue, right = Discard.
func (c *Composer) HitTestDraftPrompt(x, contentY int) string {
	boxY0 := (c.height - 9) / 2
	if contentY != boxY0+6 {
		return ""
	}
	if x < c.width/2 {
		return "c"
	}
	return "d"
}

// HitTestFooter returns the key string for a click on the in-box footer hint
// row, or "" if the click doesn't land on an actionable hint.
//
// Geometry: footer hint row is at content-area y = 10 + bodyHeight + attachRows.
// Hint x positions are computed the same way View() does it.
func (c *Composer) HitTestFooter(x, contentY int) string {
	if !c.active || c.prompting {
		return ""
	}
	expectedY := 10 + c.bodyHeight() + c.attachSectionRows()
	if contentY != expectedY {
		return ""
	}

	boxWidth := c.width - 16
	if boxWidth > 110 {
		boxWidth = 110
	}
	if boxWidth < 60 {
		boxWidth = 60
	}
	boxX0 := (c.width - boxWidth - 2) / 2
	innerW := boxWidth - 4

	type hintItem struct{ icon, key, desc string }
	hints := []hintItem{
		{icons.Send, "ctrl+s", "send"},
		{icons.ChevronRight, "tab", "next field"},
		{icons.ArrowUpDown, "", "scroll"}, // pgup/pgdn not a single key
		{icons.Attachment, "@", "attach"},
	}
	if len(c.attachments) > 0 {
		anonDesc := "anonymize"
		if c.anonymize {
			anonDesc = "anonymize ON"
		}
		hints = append(hints, hintItem{icons.Label, "ctrl+r", anonDesc})
	}
	hints = append(hints, hintItem{icons.Close, "esc", "cancel"})

	// Reproduce the plain-width calculation from View() to get the same padLeft.
	var plainParts []string
	for _, h := range hints {
		plainParts = append(plainParts, h.icon+" "+h.desc+" ("+h.key+")")
	}
	plainW := len([]rune(strings.Join(plainParts, "  ")))
	padLeft := (innerW - plainW) / 2
	if padLeft < 0 {
		padLeft = 0
	}

	// Hit-test each hint: content starts at boxX0+border(1)+pad(2) = boxX0+3.
	curX := boxX0 + 3 + padLeft
	for i, h := range hints {
		if i > 0 {
			curX += 2 // "  " gap between hints
		}
		partW := len([]rune(h.icon + " " + h.desc + " (" + h.key + ")"))
		if x >= curX && x < curX+partW {
			return h.key // "" for the scroll hint = no action
		}
		curX += partW
	}
	return ""
}

// HandleMouse processes mouse input for composer scrolling.
func (c *Composer) HandleMouse(msg tea.MouseMsg) bool {
	if !c.active {
		return false
	}
	if msg.Action != tea.MouseActionPress {
		return false
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		c.scrollBody(-1)
		return true
	case tea.MouseButtonWheelDown:
		c.scrollBody(1)
		return true
	}
	return false
}

// submit builds the draft and sets the result.
func (c *Composer) submit() {
	toStr := c.fields[0].Value
	ccStr := c.fields[1].Value
	subject := c.fields[2].Value
	body := c.fields[3].Value

	to := parseAddresses(toStr)
	cc := parseAddresses(ccStr)

	var smtpAttachments []outsmtp.Attachment
	for i, a := range c.attachments {
		smtpAttachments = append(smtpAttachments, outsmtp.Attachment{
			Path:     a.Path,
			Filename: a.sendName(i, c.anonymize),
		})
	}

	draft := &outsmtp.ComposedMessage{
		From:        c.from,
		To:          to,
		CC:          cc,
		Subject:     subject,
		Body:        body,
		Attachments: smtpAttachments,
	}

	c.result = &Result{Action: "send", Draft: draft}
	c.active = false
}

// ── Layout helpers ────────────────────────────────────────────────────────────

// attachSectionRows returns the number of rows the attachment section occupies.
func (c *Composer) attachSectionRows() int {
	if len(c.attachments) == 0 {
		return 0
	}
	return 1 + len(c.attachments) // header + one row per attachment
}

// bodyHeight computes available rows for the body / file picker area.
func (c *Composer) bodyHeight() int {
	// Fixed rows consumed: title(1) + from(1) + topDivider(1) + 3 header fields(3) + sectionDiv(1) = 8
	// After body: attachSection + divider(1) + hints(1)
	// Box overhead: padding top(1) + padding bot(1) + border top(1) + border bot(1) = 4
	fixed := 8 + 1 + 1 + 4 + c.attachSectionRows()
	h := c.height - fixed
	if h < 3 {
		h = 3
	}
	return h
}

// View renders the composer overlay.
func (c *Composer) View() string {
	if !c.active {
		return ""
	}
	theme := c.theme

	// ── Continue / Discard prompt ─────────────────────────────────────────────
	if c.prompting {
		return c.viewDraftPrompt(theme)
	}

	boxWidth := c.width - 16
	if boxWidth > 110 {
		boxWidth = 110
	}
	if boxWidth < 60 {
		boxWidth = 60
	}
	// innerW is the usable content width inside the box's Padding(1,2).
	innerW := boxWidth - 4

	// ── Title ────────────────────────────────────────────────────────────────
	mode := c.mode
	if mode == "" {
		mode = "New Message"
	}
	title := lipgloss.NewStyle().
		Foreground(theme.Accent).
		Background(theme.Surface).
		Bold(true).
		Align(lipgloss.Center).
		Width(innerW).
		Render(mode)

	// ── From line ────────────────────────────────────────────────────────────
	fromStr := c.from.Address
	if c.from.Name != "" {
		fromStr = c.from.Name + " <" + c.from.Address + ">"
	}
	fromLine := lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Background(theme.Surface).
		Align(lipgloss.Center).
		Width(innerW).
		Render(fromStr)

	// ── Dividers ─────────────────────────────────────────────────────────────
	divider := lipgloss.NewStyle().
		Foreground(theme.Border).
		Background(theme.Surface).
		Render(strings.Repeat("─", innerW))

	sectionDiv := lipgloss.NewStyle().
		Foreground(theme.Overlay).
		Background(theme.Surface).
		Render(strings.Repeat("╌", innerW))

	// ── Header fields (To / CC / Subject) ────────────────────────────────────
	var rows []string
	rows = append(rows, title, fromLine, divider)

	for i, f := range c.fields[:3] {
		ef := NewEditorField(theme, f)
		ef.SetActive(i == c.focused)
		ef.SetWidth(innerW)
		rows = append(rows, ef.View())
	}

	rows = append(rows, sectionDiv)

	// ── Body / File Picker area ───────────────────────────────────────────────
	bodyF := c.fields[3]
	bodyFocused := c.focused == 3

	var bodyLabelFg, bodySepFg lipgloss.Color
	var bodySepStr string
	if bodyFocused {
		bodyLabelFg = theme.Accent
		bodySepFg = theme.Accent
		bodySepStr = " ▸ "
	} else {
		bodyLabelFg = theme.TextMuted
		bodySepFg = theme.Border
		bodySepStr = " │ "
	}

	bodyLabel := lipgloss.NewStyle().
		Foreground(bodyLabelFg).
		Background(theme.Surface).
		Width(composerLabelWidth).
		Align(lipgloss.Right).
		Render("Body:")

	bodySep := lipgloss.NewStyle().Foreground(bodySepFg).Background(theme.Surface).Render(bodySepStr)

	const bodyPrefixWidth = composerLabelWidth + 3
	indent := lipgloss.NewStyle().Background(theme.Surface).Render(strings.Repeat(" ", bodyPrefixWidth))
	textW := innerW - bodyPrefixWidth
	if textW < 1 {
		textW = 1
	}

	bodyH := c.bodyHeight()

	if c.filePicker.isActive() {
		// Replace body rows with the file picker.
		pickerLines := strings.Split(c.filePicker.view(textW, bodyH), "\n")
		for idx, line := range pickerLines {
			prefix := indent
			if idx == 0 {
				prefix = bodyLabel + bodySep
			}
			rows = append(rows, prefix+line)
		}
		// Pad remaining rows so the box height stays stable.
		for i := len(pickerLines); i < bodyH; i++ {
			rows = append(rows, indent+lipgloss.NewStyle().
				Background(theme.Surface).Width(textW).Render(""))
		}
	} else {
		bodyLines := strings.Split(bodyF.Value, "\n")
		start, end := c.bodyWindow(bodyF, bodyH)
		visible := bodyLines[start:end]

		cursorLine := -1
		cursorCol := 0
		if bodyFocused {
			cursorLine = c.cursorLine(bodyF)
			cursorCol = c.cursorColumn(bodyF)
		}

		// Purple palette for quoted reply text. Two shades — one for the │ gutter
		// (brighter) and one for the body text (softer) — on both dark and light themes.
		var quotePurple, quoteTextPurple lipgloss.Color
		if theme.IsDark {
			quotePurple = lipgloss.Color("#B094F0")     // bright lavender gutter bar
			quoteTextPurple = lipgloss.Color("#9B82D4") // muted purple body text
		} else {
			quotePurple = lipgloss.Color("#7C5CBF")     // deep violet gutter bar
			quoteTextPurple = lipgloss.Color("#8B6DC8") // medium violet body text
		}

		plainLineStyle := lipgloss.NewStyle().
			Foreground(theme.Text).
			Background(theme.SurfaceAlt).
			Width(textW)

		visualRows := 0 // number of visual rows emitted so far
		isFirst := true // whether the very first visual row has been emitted

		for logIdx, line := range visible {
			logicalLineIdx := start + logIdx

			// Detect quoted lines ("> …") and strip depth markers.
			quoteDepth, quoteText := bodyQuoteDepth(line)
			isQuoted := quoteDepth > 0

			// The gutter occupies 2 cols per depth level ("│ ").
			// Non-quoted lines use the full textW.
			gutterW := quoteDepth * 2
			wrapW := textW - gutterW
			if wrapW < 1 {
				wrapW = 1
			}

			// Soft-wrap the logical line into visual sub-lines.
			var subLines []string
			if isQuoted {
				subLines = util.WrapANSI(quoteText, wrapW)
			} else {
				subLines = util.WrapANSI(line, textW)
			}
			if len(subLines) == 0 {
				subLines = []string{""}
			}

			// Determine where the cursor sits within this logical line's sub-lines.
			// Cursor tracking works on the raw logical line (with > markers intact),
			// so we map cursorCol into sub-lines using the raw line's rune positions.
			cursorSubLine := -1
			cursorSubCol := 0
			if bodyFocused && logicalLineIdx == cursorLine {
				// For quoted lines the cursor column is measured against the raw
				// "> …" content, but we display only the stripped text. Shift the
				// cursor left by the number of stripped prefix runes so it lands in
				// the right sub-line position.
				effectiveCol := cursorCol
				if isQuoted {
					prefixRunes := len([]rune(line)) - len([]rune(quoteText))
					effectiveCol = cursorCol - prefixRunes
					if effectiveCol < 0 {
						effectiveCol = 0
					}
				}
				runesConsumed := 0
				for si, sl := range subLines {
					slRunes := []rune(sl)
					slLen := len(slRunes)
					isLastSub := si == len(subLines)-1
					if effectiveCol <= runesConsumed+slLen || isLastSub {
						cursorSubLine = si
						cursorSubCol = effectiveCol - runesConsumed
						if cursorSubCol < 0 {
							cursorSubCol = 0
						}
						if cursorSubCol > slLen {
							cursorSubCol = slLen
						}
						break
					}
					runesConsumed += slLen + 1
				}
			}

			for si, sl := range subLines {
				if visualRows >= bodyH {
					break
				}

				var renderedLine string
				if isQuoted {
					// Build the purple gutter: one "│ " per depth level.
					gutterStr := strings.Repeat("│ ", quoteDepth)
					gutter := lipgloss.NewStyle().
						Foreground(quotePurple).
						Background(theme.SurfaceAlt).
						Render(gutterStr)

					// Insert cursor into stripped text if needed.
					display := sl
					if bodyFocused && logicalLineIdx == cursorLine && si == cursorSubLine {
						slRunes := []rune(sl)
						col := cursorSubCol
						if col > len(slRunes) {
							col = len(slRunes)
						}
						display = string(slRunes[:col]) + "▌" + string(slRunes[col:])
					}

					// Text cell: remaining width after gutter.
					textCell := lipgloss.NewStyle().
						Foreground(quoteTextPurple).
						Background(theme.SurfaceAlt).
						Width(wrapW).
						Render(display)

					renderedLine = gutter + textCell
				} else {
					display := sl
					if bodyFocused && logicalLineIdx == cursorLine && si == cursorSubLine {
						slRunes := []rune(sl)
						col := cursorSubCol
						if col > len(slRunes) {
							col = len(slRunes)
						}
						display = string(slRunes[:col]) + "▌" + string(slRunes[col:])
					}
					renderedLine = plainLineStyle.Render(display)
				}

				prefix := indent
				if isFirst {
					prefix = bodyLabel + bodySep
					isFirst = false
				}
				rows = append(rows, prefix+renderedLine)
				visualRows++
			}
		}

		// Pad remaining visual rows to keep the body area height stable.
		for visualRows < bodyH {
			prefix := indent
			if isFirst {
				prefix = bodyLabel + bodySep
				isFirst = false
			}
			rows = append(rows, prefix+lipgloss.NewStyle().
				Background(theme.SurfaceAlt).Width(textW).Render(""))
			visualRows++
		}
	}

	// ── Attachments section ───────────────────────────────────────────────────
	if len(c.attachments) > 0 {
		attachFocused := c.focused == len(c.fields)

		var attachLabelFg lipgloss.Color
		var attachSepStr string
		var attachSepFg lipgloss.Color
		if attachFocused {
			attachLabelFg = theme.Accent
			attachSepFg = theme.Accent
			attachSepStr = " ▸ "
		} else {
			attachLabelFg = theme.TextMuted
			attachSepFg = theme.Border
			attachSepStr = " │ "
		}

		attachLabelSt := lipgloss.NewStyle().
			Foreground(attachLabelFg).
			Background(theme.Surface).
			Width(composerLabelWidth).
			Align(lipgloss.Right)

		attachSepSt := lipgloss.NewStyle().Foreground(attachSepFg).Background(theme.Surface)

		fileW := innerW - composerLabelWidth - 3
		if fileW < 1 {
			fileW = 1
		}

		// Build the label for the first row: "Files:" right-aligned.
		// Subsequent rows get an empty label + separator-width indent.
		blankIndent := attachLabelSt.Render("") +
			attachSepSt.Render(strings.Repeat(" ", len(attachSepStr)))

		for i, a := range c.attachments {
			displayName := a.DisplayName
			if c.anonymize {
				displayName = a.sendName(i, true)
			}
			selected := attachFocused && i == c.attachCursor
			var entrySt lipgloss.Style
			if selected {
				entrySt = lipgloss.NewStyle().
					Foreground(theme.Accent).
					Background(theme.SurfaceAlt).
					Bold(true).
					Width(fileW)
			} else {
				entrySt = lipgloss.NewStyle().
					Foreground(theme.Text).
					Background(theme.Surface).
					Width(fileW)
			}

			icon := icons.Attachment + " "
			if a.Compressed {
				icon = icons.Package + " "
			}
			entry := icon + displayName
			if selected {
				entry += "  ← del: remove"
			}

			if i == 0 {
				label := attachLabelSt.Render("Files:")
				rows = append(rows, label+attachSepSt.Render(attachSepStr)+entrySt.Render(entry))
			} else {
				rows = append(rows, blankIndent+entrySt.Render(entry))
			}
		}
	}

	// ── Footer ───────────────────────────────────────────────────────────────
	rows = append(rows, divider)

	hintIconSt := lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.Surface)
	hintDescSt := lipgloss.NewStyle().Foreground(theme.TextMuted).Background(theme.Surface)
	hintKeySt := lipgloss.NewStyle().Foreground(theme.TextFaint).Background(theme.Surface)
	hintSepSt := lipgloss.NewStyle().Foreground(theme.TextFaint).Background(theme.Surface)

	type hintItem struct{ icon, key, desc string }
	composerHints := []hintItem{
		{icons.Send, "ctrl+s", "send"},
		{icons.ChevronRight, "tab", "next field"},
		{icons.ArrowUpDown, "pgup/pgdn", "scroll"},
		{icons.Attachment, "@", "attach"},
	}
	if len(c.attachments) > 0 {
		anonDesc := "anonymize"
		if c.anonymize {
			anonDesc = "anonymize ON"
		}
		composerHints = append(composerHints, hintItem{icons.Label, "ctrl+r", anonDesc})
	}
	composerHints = append(composerHints, hintItem{icons.Close, "esc", "cancel"})

	var plainParts []string
	for _, h := range composerHints {
		plainParts = append(plainParts, h.icon+" "+h.desc+" ("+h.key+")")
	}
	plainHint := strings.Join(plainParts, "  ")
	plainW := len([]rune(plainHint))
	padLeft := (innerW - plainW) / 2
	if padLeft < 0 {
		padLeft = 0
	}
	padRight := innerW - plainW - padLeft
	if padRight < 0 {
		padRight = 0
	}

	var hintParts []string
	for _, h := range composerHints {
		hintParts = append(hintParts,
			hintIconSt.Render(h.icon+" ")+
				hintDescSt.Render(h.desc+" ")+
				hintKeySt.Render("("+h.key+")"),
		)
	}
	styledHint := hintSepSt.Render(strings.Repeat(" ", padLeft)) +
		strings.Join(hintParts, hintSepSt.Render("  ")) +
		hintSepSt.Render(strings.Repeat(" ", padRight))
	rows = append(rows, styledHint)

	content := strings.Join(rows, "\n")

	box := lipgloss.NewStyle().
		Background(theme.Surface).
		Foreground(theme.Text).
		Padding(1, 2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		Width(boxWidth).
		Render(content)

	return lipgloss.Place(c.width, c.height, lipgloss.Center, lipgloss.Top, box)
}

// --- helpers ---

func parseAddresses(s string) []data.Address {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var addrs []data.Address
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			addrs = append(addrs, data.Address{Address: p})
		}
	}
	return addrs
}

func quoteBody(msg *data.Message) string {
	var sb strings.Builder
	sb.WriteString("\n\n--- ")
	sb.WriteString(msg.FromString())
	sb.WriteString(" wrote:\n")
	for _, line := range strings.Split(msg.Body, "\n") {
		sb.WriteString("> ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

func forwardBody(msg *data.Message) string {
	var sb strings.Builder
	sb.WriteString("\n\n--- Forwarded message ---\n")
	sb.WriteString("From: ")
	sb.WriteString(msg.FromString())
	sb.WriteString("\nSubject: ")
	sb.WriteString(msg.Subject)
	sb.WriteString("\n\n")
	sb.WriteString(msg.Body)
	return sb.String()
}

func primaryReplyAddress(msg *data.Message) data.Address {
	if len(msg.From) > 0 {
		return msg.From[0]
	}
	return data.Address{}
}

func filterSelf(addrs []data.Address, self string) []data.Address {
	if self == "" {
		return addrs
	}
	var out []data.Address
	for _, a := range addrs {
		if strings.EqualFold(a.Address, self) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func addressListString(addrs []data.Address) string {
	if len(addrs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Address == "" {
			continue
		}
		parts = append(parts, a.Address)
	}
	return strings.Join(parts, ", ")
}

func (c *Composer) bodyWindow(f *Field, bodyH int) (int, int) {
	lines := strings.Split(f.Value, "\n")
	if len(lines) == 0 {
		return 0, 0
	}
	maxTop := len(lines) - bodyH
	if maxTop < 0 {
		maxTop = 0
	}
	if c.bodyTop > maxTop {
		c.bodyTop = maxTop
	}
	if c.bodyTop < 0 {
		c.bodyTop = 0
	}
	end := c.bodyTop + bodyH
	if end > len(lines) {
		end = len(lines)
	}
	return c.bodyTop, end
}

func (c *Composer) cursorLine(f *Field) int {
	byteIdx := f.cursorByteIndex()
	return strings.Count(f.Value[:byteIdx], "\n")
}

func (c *Composer) cursorColumn(f *Field) int {
	byteIdx := f.cursorByteIndex()
	before := f.Value[:byteIdx]
	lastIdx := strings.LastIndex(before, "\n")
	if lastIdx < 0 {
		return len([]rune(before))
	}
	return len([]rune(before[lastIdx+1:]))
}

func (c *Composer) ensureBodyVisible() {
	if c.focused >= len(c.fields) {
		return
	}
	f := c.fields[c.focused]
	if f.Kind != FieldTextArea {
		return
	}
	bodyH := c.bodyHeight()
	line := c.cursorLine(f)
	if line < c.bodyTop {
		c.bodyTop = line
	}
	if line >= c.bodyTop+bodyH {
		c.bodyTop = line - bodyH + 1
	}
	maxTop := strings.Count(f.Value, "\n") - bodyH + 1
	if maxTop < 0 {
		maxTop = 0
	}
	if c.bodyTop > maxTop {
		c.bodyTop = maxTop
	}
}

func (c *Composer) scrollBody(delta int) {
	if c.focused >= len(c.fields) {
		return
	}
	f := c.fields[c.focused]
	if f.Kind != FieldTextArea {
		return
	}
	bodyH := c.bodyHeight()
	lines := strings.Split(f.Value, "\n")
	maxTop := len(lines) - bodyH
	if maxTop < 0 {
		maxTop = 0
	}
	c.bodyTop += delta * (bodyH / 2)
	if c.bodyTop < 0 {
		c.bodyTop = 0
	}
	if c.bodyTop > maxTop {
		c.bodyTop = maxTop
	}
}

// viewDraftPrompt renders the "Continue draft or Discard?" dialog.
func (c *Composer) viewDraftPrompt(theme *config.Theme) string {
	boxWidth := c.width - 4
	if boxWidth < 60 {
		boxWidth = 60
	}
	innerW := boxWidth - 4

	titleSt := lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.Surface).Bold(true).Align(lipgloss.Center).Width(innerW)
	textSt := lipgloss.NewStyle().Foreground(theme.Text).Background(theme.Surface).Align(lipgloss.Center).Width(innerW)
	hintSt := lipgloss.NewStyle().Foreground(theme.TextMuted).Background(theme.Surface).Align(lipgloss.Center).Width(innerW)
	divSt := lipgloss.NewStyle().Foreground(theme.Border).Background(theme.Surface)

	divider := divSt.Render(strings.Repeat("─", innerW))

	rows := []string{
		titleSt.Render("Unsaved Draft"),
		divider,
		textSt.Render("You have an unsaved draft."),
		textSt.Render(""),
		hintSt.Render("  [enter / c]  Continue editing    [d / esc]  Discard  "),
	}

	content := strings.Join(rows, "\n")
	box := lipgloss.NewStyle().
		Background(theme.Surface).
		Foreground(theme.Text).
		Padding(1, 2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		Width(boxWidth).
		Render(content)

	return lipgloss.Place(c.width, c.height, lipgloss.Center, lipgloss.Center, box)
}

// hasDraftContent returns true if any field has been filled in or any
// attachment added — i.e. the composer has meaningful content worth saving.
func (c *Composer) hasDraftContent() bool {
	for _, f := range c.fields {
		if strings.TrimSpace(f.Value) != "" {
			return true
		}
	}
	return len(c.attachments) > 0
}

// cancelOrSaveDraft closes the composer. If there is content, it stashes the
// current state as a saved draft and emits "save-draft"; otherwise "cancel".
func (c *Composer) cancelOrSaveDraft() {
	if c.hasDraftContent() {
		c.savedDraft = &savedDraftState{
			fields:      c.fields,
			from:        c.from,
			mode:        c.mode,
			attachments: c.attachments,
			anonymize:   c.anonymize,
			focused:     c.focused,
			bodyTop:     c.bodyTop,
		}
		// Build a minimal ComposedMessage so app.go can append it to Drafts.
		draft := &outsmtp.ComposedMessage{
			From:    c.from,
			Subject: c.fields[2].Value,
			Body:    c.fields[3].Value,
		}
		c.result = &Result{Action: "save-draft", Draft: draft}
	} else {
		c.result = &Result{Action: "cancel"}
	}
	c.active = false
}

// restoreDraft swaps in the saved draft fields and clears the prompt.
func (c *Composer) restoreDraft() {
	if c.savedDraft == nil {
		c.prompting = false
		return
	}
	c.fields = c.savedDraft.fields
	c.from = c.savedDraft.from
	c.mode = c.savedDraft.mode
	c.attachments = c.savedDraft.attachments
	c.anonymize = c.savedDraft.anonymize
	c.focused = c.savedDraft.focused
	c.bodyTop = c.savedDraft.bodyTop
	c.savedDraft = nil
	c.prompting = false
}

// bodyQuoteDepth counts leading '>' characters in a body line and returns the
// quote depth and the stripped text content (without the '>' markers or their
// trailing spaces). Returns depth=0 and the original line if not a quote.
func bodyQuoteDepth(line string) (depth int, text string) {
	s := line
	for len(s) > 0 && s[0] == '>' {
		depth++
		s = s[1:]
		if len(s) > 0 && s[0] == ' ' {
			s = s[1:]
		}
	}
	if depth == 0 {
		return 0, line
	}
	return depth, strings.TrimSpace(s)
}
