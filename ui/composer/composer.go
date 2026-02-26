package composer

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	outsmtp "github.com/bubblmail/bubblmail/smtp"
)

// Result holds the outcome of a composer interaction.
type Result struct {
	Action  string // "send", "cancel"
	Draft   *outsmtp.ComposedMessage
}

// Composer is the full-screen email composition overlay.
type Composer struct {
	theme   *config.Theme
	width   int
	height  int
	active  bool
	result  *Result
	focused int // 0=To, 1=CC, 2=Subject, 3=Body

	fields []*Field
	from   data.Address
}

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

	to := ""
	if len(orig.From) > 0 {
		to = orig.From[0].Address
	}
	cc := ""
	if replyAll && len(orig.To) > 0 {
		var ccParts []string
		for _, a := range orig.To {
			if a.Address != from.Address {
				ccParts = append(ccParts, a.Address)
			}
		}
		cc = strings.Join(ccParts, ", ")
	}
	subject := orig.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	body := quoteBody(orig)

	c.fields = []*Field{
		{Label: "To", Kind: FieldText, Value: to, cursor: len(to)},
		{Label: "CC", Kind: FieldText, Value: cc, cursor: len(cc)},
		{Label: "Subject", Kind: FieldText, Value: subject, cursor: len(subject)},
		{Label: "Body", Kind: FieldTextArea, Value: body, cursor: 0},
	}
}

// OpenForward opens the composer pre-filled for a forwarded message.
func (c *Composer) OpenForward(from data.Address, orig *data.Message) {
	c.from = from
	c.active = true
	c.result = nil
	c.focused = 0

	subject := orig.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "fwd:") {
		subject = "Fwd: " + subject
	}
	body := forwardBody(orig)

	c.fields = []*Field{
		{Label: "To", Kind: FieldText},
		{Label: "CC", Kind: FieldText},
		{Label: "Subject", Kind: FieldText, Value: subject, cursor: len(subject)},
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
}

// HandleKey processes a key press.
func (c *Composer) HandleKey(key string) {
	if !c.active {
		return
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

	titleStyle := lipgloss.NewStyle().
		Foreground(theme.Accent).
		Background(theme.Surface).
		Bold(true).
		Align(lipgloss.Center).
		Width(boxWidth - 4)

	title := titleStyle.Render("New Message")

	divider := lipgloss.NewStyle().
		Foreground(theme.Border).
		Render(strings.Repeat("─", boxWidth-4))

	var rows []string
	rows = append(rows, title, divider)

	for i, f := range c.fields {
		ef := NewEditorField(theme, f)
		ef.SetActive(i == c.focused)
		ef.SetWidth(boxWidth - 4)

		if f.Kind == FieldTextArea {
			// Render multi-line body
			rows = append(rows, ef.View())
			// Show additional body lines
			bodyLines := strings.Split(f.Value, "\n")
			bodyH := c.height - len(rows) - 6
			if bodyH < 3 {
				bodyH = 3
			}
			// Show visible portion of body
			start := 0
			if len(bodyLines) > bodyH {
				// Find current line from cursor
				before := f.Value[:f.cursor]
				currentLine := strings.Count(before, "\n")
				start = currentLine - bodyH + 1
				if start < 0 {
					start = 0
				}
			}
			end := start + bodyH
			if end > len(bodyLines) {
				end = len(bodyLines)
			}
			visible := bodyLines[start:end]
			for _, line := range visible {
				lineStyle := lipgloss.NewStyle().
					Foreground(theme.Text).
					Background(theme.SurfaceAlt).
					Width(boxWidth - 4)
				rows = append(rows, "           "+lineStyle.Render(line))
			}
		} else {
			rows = append(rows, ef.View())
		}
	}

	rows = append(rows, "")

	hintStyle := lipgloss.NewStyle().
		Foreground(theme.TextFaint).
		Background(theme.Surface).
		Align(lipgloss.Center).
		Width(boxWidth - 4)
	rows = append(rows, hintStyle.Render("ctrl+enter send  ·  tab next field  ·  esc cancel"))

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
