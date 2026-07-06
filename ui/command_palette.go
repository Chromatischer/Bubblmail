package ui

import (
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// paletteCommand is one searchable action in the command palette. Key is the
// global action key dispatched (via dispatchGlobalKey) when the command runs.
type paletteCommand struct {
	Label string
	Key   string
	Icon  string
}

// commandPaletteItems lists every action reachable from the palette. Each Key
// must be handled by dispatchGlobalKey.
func commandPaletteItems() []paletteCommand {
	return []paletteCommand{
		{"Reply", "r", icons.Reply},
		{"Reply all", "R", icons.ReplyAll},
		{"Forward", "f", icons.Forward},
		{"Compose new", "c", icons.Compose},
		{"Move to folder", "v", icons.FolderOpen},
		{"Archive", "e", icons.Archive},
		{"Delete", "d", icons.Trash},
		{"Toggle star", "s", icons.Star},
		{"Toggle read / unread", "m", icons.Read},
		{"Attachments", KeyAttachments, icons.Attachment},
		{"Undo last move", "ctrl+z", icons.Refresh},
		{"Search", "/", icons.Search},
		{"Server search", "ctrl+f", icons.Search},
		{"Jump to inbox", "i", icons.Inbox},
		{"Unread-only filter", KeyUnreadFilter, icons.MailOpen},
		{"Toggle sidebar", "b", icons.FolderTree},
		{"Focus sidebar", KeySidebarFocus, icons.FolderTree},
		{"Force sync", "ctrl+r", icons.Refresh},
		{"Help", "?", icons.Help},
	}
}

// CommandPaletteOverlay is a searchable list of actions, opened with ".".
type CommandPaletteOverlay struct {
	styles   *Styles
	width    int
	height   int
	active   bool
	all      []paletteCommand
	filtered []paletteCommand
	query    string
	list     components.ScrollList
	result   *paletteCommand
}

// NewCommandPaletteOverlay creates a new command palette overlay.
func NewCommandPaletteOverlay(styles *Styles) *CommandPaletteOverlay {
	return &CommandPaletteOverlay{styles: styles}
}

// SetSize sets the overlay dimensions.
func (p *CommandPaletteOverlay) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// Open shows the palette with the full command list.
func (p *CommandPaletteOverlay) Open() {
	p.all = commandPaletteItems()
	p.query = ""
	p.applyFilter()
	p.list.Reset()
	p.result = nil
	p.active = true
}

// Close hides the palette.
func (p *CommandPaletteOverlay) Close() {
	p.active = false
	p.all = nil
	p.filtered = nil
	p.result = nil
	p.query = ""
}

// IsActive reports whether the palette is open.
func (p *CommandPaletteOverlay) IsActive() bool { return p.active }

// Result returns the chosen command, or nil if cancelled.
func (p *CommandPaletteOverlay) Result() *paletteCommand { return p.result }

func (p *CommandPaletteOverlay) applyFilter() {
	if p.query == "" {
		p.filtered = p.all
		return
	}
	q := strings.ToLower(p.query)
	var out []paletteCommand
	for _, c := range p.all {
		if strings.Contains(strings.ToLower(c.Label), q) {
			out = append(out, c)
		}
	}
	p.filtered = out
}

// HandleKey processes a key. Returns closed=true when done; check Result().
func (p *CommandPaletteOverlay) HandleKey(key string) (closed bool) {
	switch key {
	case "esc":
		if p.query != "" {
			p.query = ""
			p.applyFilter()
			p.list.Reset()
			return false
		}
		return true
	case "enter":
		if len(p.filtered) > 0 && p.list.Cursor < len(p.filtered) {
			c := p.filtered[p.list.Cursor]
			p.result = &c
		}
		return true
	case "up", "ctrl+k":
		p.list.MoveUp()
	case "down", "ctrl+j":
		p.list.MoveDown(len(p.filtered), p.listHeight())
	case "ctrl+u":
		if p.query != "" {
			p.query = ""
			p.applyFilter()
			p.list.Reset()
		}
	case "backspace", "ctrl+h":
		if p.query != "" {
			p.query = p.query[:len(p.query)-1]
			p.applyFilter()
			p.list.Reset()
		}
	default:
		if len(key) == 1 && key >= " " {
			p.query += key
			p.applyFilter()
			p.list.Reset()
		}
	}
	return false
}

func (p *CommandPaletteOverlay) listHeight() int {
	// Overhead matches the folder picker: border + padding + title + blank +
	// input + separator = 8 rows.
	h := p.height - 8
	if h < 3 {
		h = 3
	}
	if h > 15 {
		h = 15
	}
	return h
}

func (p *CommandPaletteOverlay) innerWidth() int {
	const titleStr = "Commands"
	w := util.VisibleWidth(icons.Sparkle+" "+titleStr) + 1
	for _, c := range p.all {
		// " " + icon + " " + label + gap(1) + key
		row := 3 + util.VisibleWidth(c.Label) + 1 + util.VisibleWidth(displayKey(c.Key))
		if row > w {
			w = row
		}
	}
	w += 4
	if max := p.width - 10; w > max {
		w = max
	}
	if w < 30 {
		w = 30
	}
	return w
}

// displayKey makes an action key string readable in the palette (e.g. "\" not
// the Go escape).
func displayKey(key string) string {
	switch key {
	case "\\":
		return `\`
	default:
		return key
	}
}

// View renders the command palette.
func (p *CommandPaletteOverlay) View() string {
	theme := p.styles.Theme

	cw := p.innerWidth()
	listH := p.listHeight()
	const hPad = 2
	innerW := cw - 2*hPad

	titleLine := lipgloss.NewStyle().
		Background(theme.Surface).
		Foreground(theme.Accent).
		Bold(true).
		Width(innerW).
		Render(icons.Sparkle + " Commands")

	ti := components.NewTextInput(theme)
	ti.Placeholder = "type a command…"
	ti.Value = p.query
	ti.Icon = icons.Search
	ti.Active = true
	inputLine := ti.Render(innerW)

	scrolling := len(p.filtered) > listH
	contentW := innerW
	if scrolling {
		contentW = innerW - 1
	}

	thumbStart, thumbEnd := 0, 0
	if scrolling {
		total := len(p.filtered)
		thumb := listH * listH / total
		if thumb < 1 {
			thumb = 1
		}
		pos := 0
		if maxOff := total - listH; maxOff > 0 {
			pos = p.list.Offset * (listH - thumb) / maxOff
		}
		thumbStart, thumbEnd = pos, pos+thumb
	}

	var rows []string
	if len(p.filtered) == 0 {
		rows = append(rows, lipgloss.NewStyle().
			Background(theme.Surface).
			Foreground(theme.TextFaint).
			Width(innerW).
			Render("  "+icons.FolderEmpty+"  No matching command"))
	}

	for i := p.list.Offset; i < len(p.filtered) && i < p.list.Offset+listH; i++ {
		c := p.filtered[i]
		isSelected := i == p.list.Cursor
		ms, me := findMatchRange(c.Label, p.query)
		cell := renderPaletteRow(theme, c.Icon, c.Label, displayKey(c.Key), ms, me, contentW, isSelected)
		if scrolling {
			r := i - p.list.Offset
			cell += scrollbarCell(theme, r >= thumbStart && r < thumbEnd)
		}
		rows = append(rows, cell)
	}

	blank := lipgloss.NewStyle().Background(theme.Surface).Width(innerW).Render("")
	for len(rows) < listH {
		rows = append(rows, blank)
	}

	sep := components.Divider(theme, innerW)
	parts := []string{titleLine, "", inputLine, sep}
	parts = append(parts, rows...)
	content := strings.Join(parts, "\n")

	boxH := listH + 4
	return components.ModalBox(theme, content, cw, boxH, p.width, p.height)
}

// renderPaletteRow renders one command to an exact contentW-wide cell with the
// label on the left (match-highlighted) and the key right-aligned.
func renderPaletteRow(theme *config.Theme, icon, label, keyHint string, matchStart, matchEnd, contentW int, selected bool) string {
	bg, fg := theme.Surface, theme.Text
	if selected {
		bg, fg = theme.Selected, theme.Background
	}
	base := lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(selected)
	match := lipgloss.NewStyle().Background(bg).Bold(true)
	if selected {
		match = match.Foreground(fg).Underline(true)
	} else {
		match = match.Foreground(theme.Accent)
	}
	keyStyle := lipgloss.NewStyle().Background(bg).Foreground(theme.TextFaint)
	if selected {
		keyStyle = keyStyle.Foreground(fg)
	}

	lead := " " + icon + " "
	leadW := util.VisibleWidth(lead)
	keyW := util.VisibleWidth(keyHint)
	avail := contentW - leadW - keyW - 1
	if avail < 1 {
		avail = 1
	}
	label = util.TruncateText(util.SingleLine(label), avail)

	var b strings.Builder
	b.WriteString(base.Render(lead))
	if matchStart >= 0 && matchEnd > matchStart && matchEnd <= len(label) {
		b.WriteString(base.Render(label[:matchStart]))
		b.WriteString(match.Render(label[matchStart:matchEnd]))
		b.WriteString(base.Render(label[matchEnd:]))
	} else {
		b.WriteString(base.Render(label))
	}
	pad := contentW - leadW - util.VisibleWidth(label) - keyW
	if pad < 1 {
		pad = 1
	}
	b.WriteString(base.Render(strings.Repeat(" ", pad)))
	b.WriteString(keyStyle.Render(keyHint))
	return b.String()
}
