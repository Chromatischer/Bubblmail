package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/render"
	"github.com/bubblmail/bubblmail/util"
)

// ReaderView displays a single email message.
type ReaderView struct {
	theme   *config.Theme
	width   int
	height  int
	message *data.Message
	scrollY int
	lines   []string // cached rendered body lines
}

// NewReaderView creates a new reader view.
func NewReaderView(theme *config.Theme) *ReaderView {
	return &ReaderView{theme: theme}
}

// SetSize sets the view dimensions.
func (v *ReaderView) SetSize(w, h int) {
	v.width = w
	v.height = h
	if v.message != nil {
		v.buildLines()
	}
}

// SetMessage sets the message to display and resets scroll.
func (v *ReaderView) SetMessage(msg *data.Message) {
	v.message = msg
	v.scrollY = 0
	v.buildLines()
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

// GoToTop scrolls to the top.
func (v *ReaderView) GoToTop() {
	v.scrollY = 0
}

// GoToBottom scrolls to the bottom.
func (v *ReaderView) GoToBottom() {
	max := len(v.lines) - v.bodyHeight()
	if max < 0 {
		max = 0
	}
	v.scrollY = max
}

// headerHeight returns the number of rows the header block occupies.
func (v *ReaderView) headerHeight() int {
	return 6 // From, To, Date, Subject + 2 dividers
}

// bodyHeight returns usable rows for body.
func (v *ReaderView) bodyHeight() int {
	h := v.height - v.headerHeight()
	if h < 1 {
		h = 1
	}
	return h
}

// buildLines renders the body into display lines using the rich renderer.
func (v *ReaderView) buildLines() {
	if v.message == nil {
		v.lines = nil
		return
	}
	plainBody := v.message.Body
	htmlBody := v.message.HTMLBody
	if plainBody == "" && htmlBody == "" {
		v.lines = []string{"(No body loaded — press Enter to fetch)"}
		return
	}
	textWidth := v.width - 2
	if textWidth < 20 {
		textWidth = 20
	}
	v.lines = render.RenderBody(plainBody, htmlBody, textWidth, v.theme)
}

// View renders the message reader.
func (v *ReaderView) View() string {
	theme := v.theme

	if v.message == nil {
		msg := lipgloss.NewStyle().Foreground(theme.TextMuted).Render("No message selected")
		return lipgloss.Place(v.width, v.height, lipgloss.Center, lipgloss.Center, msg)
	}

	divider := lipgloss.NewStyle().
		Foreground(theme.Border).
		Render(strings.Repeat("─", v.width))

	// Header block
	labelStyle := lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Width(10).
		Align(lipgloss.Right)
	valueStyle := lipgloss.NewStyle().
		Foreground(theme.Text)
	boldStyle := lipgloss.NewStyle().
		Foreground(theme.Text).
		Bold(true)

	// maxValW: visible columns available for header values.
	// Label = 10, separator = 2, so value must be ≤ v.width-12.
	// Subject also reserves 2 cols for the " ★" star indicator.
	maxValW := v.width - 12
	if maxValW < 10 {
		maxValW = 10
	}
	maxSubjectW := maxValW - 2
	if maxSubjectW < 5 {
		maxSubjectW = 5
	}

	fromStr := util.TruncateText(addressListStr(v.message.From), maxValW)
	toStr := util.TruncateText(addressListStr(v.message.To), maxValW)
	dateStr := util.TruncateText(util.FormatDateLong(v.message.Date)+"  "+v.message.Date.Format("15:04"), maxValW)
	subjectStr := util.TruncateText(v.message.Subject, maxSubjectW)

	stars := ""
	if v.message.IsStarred() {
		stars = lipgloss.NewStyle().Foreground(theme.Starred).Render(" ★")
	}

	headerLines := []string{
		labelStyle.Render("From") + "  " + valueStyle.Render(fromStr),
		labelStyle.Render("To") + "  " + valueStyle.Render(toStr),
		labelStyle.Render("Date") + "  " + valueStyle.Render(dateStr),
		labelStyle.Render("Subject") + "  " + boldStyle.Render(subjectStr) + stars,
		divider,
	}

	headerStr := strings.Join(headerLines, "\n")

	// Body
	bodyLines := v.lines
	bh := v.bodyHeight()
	start := v.scrollY
	if start > len(bodyLines) {
		start = len(bodyLines)
	}
	end := start + bh
	if end > len(bodyLines) {
		end = len(bodyLines)
	}
	visibleBody := bodyLines[start:end]

	// Pad body
	for len(visibleBody) < bh {
		visibleBody = append(visibleBody, "")
	}

	// Use default foreground so per-line ANSI colours from the renderer are preserved.
	bodyStr := lipgloss.NewStyle().
		Width(v.width).
		Render(strings.Join(visibleBody, "\n"))

	// Scroll indicator
	if len(bodyLines) > bh {
		pct := 0
		if len(bodyLines) > 0 {
			pct = (v.scrollY + bh) * 100 / len(bodyLines)
			if pct > 100 {
				pct = 100
			}
		}
		scrollIndicator := lipgloss.NewStyle().
			Foreground(theme.TextFaint).
			Render(fmt.Sprintf(" %d%%", pct))
		dividerWithPct := divider[:len(divider)-len(scrollIndicator)] + scrollIndicator
		_ = dividerWithPct // use simple divider for now
	}

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
