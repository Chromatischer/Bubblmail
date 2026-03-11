package composer

import (
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	outsmtp "github.com/bubblmail/bubblmail/smtp"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Result holds the outcome of a composer interaction.
type Result struct {
	Action string // "send", "cancel"
	Draft  *outsmtp.ComposedMessage
}

// Composer is the full-screen email composition overlay.
type Composer struct {
	theme   *config.Theme
	width   int
	height  int
	active  bool
	result  *Result
	focused int // 0=To, 1=CC, 2=Subject, 3=Body
	mode    string
	bodyTop int

	fields []*Field
	from   data.Address
}

const (
	composerLabelWidth = 10
	composerFieldSep   = " │ "
)

// NewComposer creates a new composer overlay.
func NewComposer(theme *config.Theme) *Composer {
	return &Composer{theme: theme}
}

// OpenNew opens the composer for a new email.
func (c *Composer) OpenNew(from data.Address) {
	c.from = from
	c.active = true
	c.result = nil
	c.focused = 0
	c.mode = "New Message"
	c.bodyTop = 0
	c.fields = []*Field{
		{Label: "To", Kind: FieldText},
		{Label: "CC", Kind: FieldText},
		{Label: "Subject", Kind: FieldText},
		{Label: "Body", Kind: FieldTextArea},
	}
}

// OpenReply opens the composer pre-filled for a reply.
func (c *Composer) OpenReply(from data.Address, orig *data.Message, replyAll bool) {
	c.from = from
	c.active = true
	c.result = nil
	c.focused = 3 // jump to body
	c.bodyTop = 0
	if replyAll {
		c.mode = "Reply All"
	} else {
		c.mode = "Reply"
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

	c.fields = []*Field{
		{Label: "To", Kind: FieldText, Value: to, cursor: len([]rune(to))},
		{Label: "CC", Kind: FieldText, Value: cc, cursor: len([]rune(cc))},
		{Label: "Subject", Kind: FieldText, Value: subject, cursor: len([]rune(subject))},
		{Label: "Body", Kind: FieldTextArea, Value: body, cursor: 0},
	}
}

// OpenForward opens the composer pre-filled for a forwarded message.
func (c *Composer) OpenForward(from data.Address, orig *data.Message) {
	c.from = from
	c.active = true
	c.result = nil
	c.focused = 0
	c.mode = "Forward"
	c.bodyTop = 0

	subject := orig.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "fwd:") {
		subject = "Fwd: " + subject
	}
	body := forwardBody(orig)

	c.fields = []*Field{
		{Label: "To", Kind: FieldText},
		{Label: "CC", Kind: FieldText},
		{Label: "Subject", Kind: FieldText, Value: subject, cursor: len([]rune(subject))},
		{Label: "Body", Kind: FieldTextArea, Value: body, cursor: 0},
	}
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
	if c.focused < 0 || c.focused >= len(c.fields) {
		c.focused = 0
	}
	f := c.fields[c.focused]

	switch key {
	case "ctrl+enter":
		c.submit()
		return
	case "esc":
		c.result = &Result{Action: "cancel"}
		c.active = false
		return
	case "tab":
		c.focused = (c.focused + 1) % len(c.fields)
		return
	case "shift+tab":
		c.focused = (c.focused - 1 + len(c.fields)) % len(c.fields)
		return
	case "up":
		if f.Kind == FieldTextArea {
			f.cursorMoveLines(-1)
			return
		}
	case "down":
		if f.Kind == FieldTextArea {
			f.cursorMoveLines(1)
			return
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
			// Move to next field
			c.focused = (c.focused + 1) % len(c.fields)
		}
		return
	case "backspace", "ctrl+h":
		f.backspace()
		return
	case "left":
		f.cursorLeft()
		return
	case "right":
		f.cursorRight()
		return
	case "home", "ctrl+a":
		f.cursorHome()
		return
	case "end", "ctrl+e":
		f.cursorEnd()
		return
	default:
		if len(key) == 1 && key[0] >= 32 {
			f.insert(key)
		}
	}
	c.ensureBodyVisible()
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

	draft := &outsmtp.ComposedMessage{
		From:    c.from,
		To:      to,
		CC:      cc,
		Subject: subject,
		Body:    body,
	}

	c.result = &Result{Action: "send", Draft: draft}
	c.active = false
}

// composerBodyOverhead is the total number of rows consumed outside the body
// area: title(1) + from(1) + divider(1) + To(1) + CC(1) + Subject(1) +
// sectionDiv(1) + divider(1) + hints(1) = 9 content rows, plus box border(2)
// + box padding top/bottom(2) = 13 total.
const composerBodyOverhead = 13

// View renders the composer overlay.
func (c *Composer) View() string {
	if !c.active {
		return ""
	}
	theme := c.theme

	boxWidth := c.width - 4
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

	// ── Body field ───────────────────────────────────────────────────────────
	bodyF := c.fields[3]
	bodyFocused := c.focused == 3

	// Label / separator colours follow focus state.
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

	// Use plain char counts — never measure pre-rendered ANSI strings.
	// composerLabelWidth(10) + sepStr(3) = 13 chars of prefix.
	const bodyPrefixWidth = composerLabelWidth + 3
	indent := lipgloss.NewStyle().Background(theme.Surface).Render(strings.Repeat(" ", bodyPrefixWidth))
	textW := innerW - bodyPrefixWidth
	if textW < 1 {
		textW = 1
	}

	bodyLines := strings.Split(bodyF.Value, "\n")
	bodyH := c.height - len(rows) - 6 // matches composerBodyOverhead
	if bodyH < 3 {
		bodyH = 3
	}
	start, end := c.bodyWindow(bodyF, bodyH)
	visible := bodyLines[start:end]

	for idx, line := range visible {
		lineStyle := lipgloss.NewStyle().
			Foreground(theme.Text).
			Background(theme.SurfaceAlt).
			Width(textW)

		display := line
		if bodyFocused {
			cursorLine := c.cursorLine(bodyF)
			if cursorLine == start+idx {
				col := c.cursorColumn(bodyF)
				lineRunes := []rune(line)
				if col > len(lineRunes) {
					col = len(lineRunes)
				}
				display = string(lineRunes[:col]) + "▌" + string(lineRunes[col:])
			}
		}

		// Always render the label on the first visible row, even when scrolled.
		prefix := indent
		if idx == 0 {
			prefix = bodyLabel + bodySep
		}
		rows = append(rows, prefix+lineStyle.Render(display))
	}

	// ── Footer ───────────────────────────────────────────────────────────────
	rows = append(rows, divider)

	// Build hint line matching the main statusbar format: icon desc (key).
	// Every span carries Background(theme.Surface) so ANSI resets leave no holes.
	// Spaces are absorbed into adjacent renders — never left bare between spans.
	hintIconSt := lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.Surface)
	hintDescSt := lipgloss.NewStyle().Foreground(theme.TextMuted).Background(theme.Surface)
	hintKeySt := lipgloss.NewStyle().Foreground(theme.TextFaint).Background(theme.Surface)
	hintSepSt := lipgloss.NewStyle().Foreground(theme.TextFaint).Background(theme.Surface)

	type hintItem struct{ icon, key, desc string }
	composerHints := []hintItem{
		{icons.Send, "ctrl+enter", "send"},
		{icons.ChevronRight, "tab", "next field"},
		{icons.ArrowUpDown, "pgup/pgdn", "scroll"},
		{icons.Close, "esc", "cancel"},
	}

	// Plain text for rune-count centering (icons render as 1 col each).
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

	return lipgloss.Place(c.width, c.height, lipgloss.Center, lipgloss.Center, box)
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
	bodyH := c.height - composerBodyOverhead
	if bodyH < 3 {
		bodyH = 3
	}
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
	bodyH := c.height - composerBodyOverhead
	if bodyH < 3 {
		bodyH = 3
	}
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
