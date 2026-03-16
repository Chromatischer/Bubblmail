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

// FolderView renders the full folder/label browser pane.
type FolderView struct {
	theme   *config.Theme
	width   int
	height  int
	folders []*data.Folder
	account string
	cursor  int
	offset  int
}

// Layout constants — mirrors the composer's fixed overhead accounting.
const (
	folderHeaderRows = 3 // title + account + divider
	folderFooterRows = 2 // section-divider + hints
)

// NewFolderView creates a new folder view.
func NewFolderView(theme *config.Theme) *FolderView {
	return &FolderView{theme: theme}
}

// SetSize sets the view dimensions.
func (v *FolderView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

// SetFolders updates the folder list and resets navigation.
func (v *FolderView) SetFolders(account string, folders []*data.Folder) {
	v.account = account
	v.folders = folders
	v.cursor = 0
	v.offset = 0
}

// SelectedFolder returns the currently focused folder, or nil.
func (v *FolderView) SelectedFolder() *data.Folder {
	if v.cursor < 0 || v.cursor >= len(v.folders) {
		return nil
	}
	return v.folders[v.cursor]
}

// listHeight returns the number of folder rows visible within the layout.
func (v *FolderView) listHeight() int {
	h := v.height - folderHeaderRows - folderFooterRows
	if h < 1 {
		h = 1
	}
	return h
}

// MoveUp moves cursor up.
func (v *FolderView) MoveUp() {
	if v.cursor > 0 {
		v.cursor--
		if v.cursor < v.offset {
			v.offset--
		}
	}
}

// MoveDown moves cursor down.
func (v *FolderView) MoveDown() {
	if v.cursor < len(v.folders)-1 {
		v.cursor++
		lh := v.listHeight()
		if v.cursor >= v.offset+lh {
			v.offset++
		}
	}
}

// PageUp moves cursor up by a page.
func (v *FolderView) PageUp() {
	lh := v.listHeight()
	v.cursor -= lh
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.offset = v.cursor
}

// PageDown moves cursor down by a page.
func (v *FolderView) PageDown() {
	lh := v.listHeight()
	v.cursor += lh
	if v.cursor >= len(v.folders) {
		v.cursor = len(v.folders) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.offset = v.cursor
}

// GoToTop jumps to the first folder.
func (v *FolderView) GoToTop() {
	v.cursor = 0
	v.offset = 0
}

// GoToBottom jumps to the last folder.
func (v *FolderView) GoToBottom() {
	if len(v.folders) == 0 {
		return
	}
	lh := v.listHeight()
	v.cursor = len(v.folders) - 1
	v.offset = v.cursor - lh + 1
	if v.offset < 0 {
		v.offset = 0
	}
}

// View renders the folder browser.
func (v *FolderView) View() string {
	theme := v.theme

	if len(v.folders) == 0 {
		msg := lipgloss.NewStyle().
			Background(theme.Surface).
			Foreground(theme.TextMuted).
			Render(icons.FolderTree + " No folders")
		return lipgloss.Place(v.width, v.height, lipgloss.Center, lipgloss.Center, msg)
	}

	// ── Title block ───────────────────────────────────────────────────────────
	// Centered accent title + muted account subtitle, mirroring the composer's
	// title/from-line layout.
	title := lipgloss.NewStyle().
		Foreground(theme.Accent).
		Background(theme.Surface).
		Bold(true).
		Align(lipgloss.Center).
		Width(v.width).
		Render(icons.FolderTree + " Folders")

	fromLine := lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Background(theme.Surface).
		Align(lipgloss.Center).
		Width(v.width).
		Render(v.account)

	divider := lipgloss.NewStyle().
		Foreground(theme.Border).
		Background(theme.Surface).
		Render(strings.Repeat("─", v.width))

	// ── Folder list ───────────────────────────────────────────────────────────
	lh := v.listHeight()
	end := v.offset + lh
	if end > len(v.folders) {
		end = len(v.folders)
	}

	var rows []string
	for i := v.offset; i < end; i++ {
		rows = append(rows, v.renderFolder(v.folders[i], i == v.cursor))
	}

	// Pad remaining rows to keep the view height stable.
	blank := lipgloss.NewStyle().Background(theme.Surface).Width(v.width).Render("")
	for len(rows) < lh {
		rows = append(rows, blank)
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	// Dashed section divider + centered hint bar — mirrors composer footer.
	sectionDiv := lipgloss.NewStyle().
		Foreground(theme.Overlay).
		Background(theme.Surface).
		Render(strings.Repeat("╌", v.width))

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		fromLine,
		divider,
		strings.Join(rows, "\n"),
		sectionDiv,
		v.renderHints(),
	)
}

// renderFolder renders a single folder row as three explicit-background cells:
//
//	[iconCell: margin + indent + icon + space]  [nameCell: folder name]  [unreadCell: badge]
//
// All cells carry explicit Background(bg) — no stray terminal resets between segments.
// Widths are computed from plain strings only; no ANSI string is ever measured.
func (v *FolderView) renderFolder(f *data.Folder, selected bool) string {
	theme := v.theme

	var bg, nameFg, iconFg lipgloss.Color
	if selected {
		bg = theme.Selected
		nameFg = theme.Background
		iconFg = theme.Background
	} else {
		bg = theme.Surface
		nameFg = theme.Text
		iconFg = theme.Accent
	}

	const leftPad = 1 // single leading space for breathing room
	const unreadW = 5 // right-side badge: right-aligned count + trailing space

	// Icon cell: left margin + depth indent + icon glyph + trailing space.
	depthIndent := 2 * f.Depth
	iconCellW := leftPad + depthIndent + 2 // +2 for icon(1) + space(1)
	iconPrefix := strings.Repeat(" ", leftPad+depthIndent) + folderIcon(f) + " "
	iconCell := lipgloss.NewStyle().
		Background(bg).
		Foreground(iconFg).
		Width(iconCellW).
		Render(iconPrefix)

	// Name cell: fills remaining width between icon and unread badge.
	nameW := v.width - iconCellW - unreadW
	if nameW < 1 {
		nameW = 1
	}
	name := util.SingleLine(f.DisplayName)
	if name == "" {
		name = util.SingleLine(f.Name)
	}
	name = util.TruncateText(name, nameW)

	nameSt := lipgloss.NewStyle().
		Background(bg).
		Foreground(nameFg).
		Width(nameW)
	if selected {
		nameSt = nameSt.Bold(true)
	}
	nameCell := nameSt.Render(name)

	// Unread badge: right-aligned count in Unread color; blank when zero or selected.
	var unreadStr string
	var unreadFg lipgloss.Color
	if f.Unread > 0 && !selected {
		count := f.Unread
		if count > 9999 {
			count = 9999
		}
		// "%4d " → right-aligned count in 4 cols + 1 trailing space = 5 total.
		unreadStr = fmt.Sprintf("%4d ", count)
		unreadFg = theme.Unread
	} else {
		unreadStr = strings.Repeat(" ", unreadW)
		unreadFg = bg
	}
	unreadCell := lipgloss.NewStyle().
		Background(bg).
		Foreground(unreadFg).
		Width(unreadW).
		Render(unreadStr)

	return iconCell + nameCell + unreadCell
}

// renderHints builds the centered footer hint bar.
// Structure and styling mirror the composer's footer hints exactly.
func (v *FolderView) renderHints() string {
	theme := v.theme

	hintIconSt := lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.Surface)
	hintDescSt := lipgloss.NewStyle().Foreground(theme.TextMuted).Background(theme.Surface)
	hintKeySt := lipgloss.NewStyle().Foreground(theme.TextFaint).Background(theme.Surface)
	hintSepSt := lipgloss.NewStyle().Foreground(theme.TextFaint).Background(theme.Surface)

	type hintItem struct{ icon, key, desc string }
	hints := []hintItem{
		{icons.ArrowUpDown, "j/k", "navigate"},
		{icons.ChevronRight, "enter", "open"},
		{icons.FolderNew, "n", "new folder"},
		{icons.Close, "esc", "back"},
	}

	// Compute plain-text width for centering using rune count (same as composer).
	var plainParts []string
	for _, h := range hints {
		plainParts = append(plainParts, h.icon+" "+h.desc+" ("+h.key+")")
	}
	plainHint := strings.Join(plainParts, "  ")
	plainW := len([]rune(plainHint))
	padLeft := (v.width - plainW) / 2
	if padLeft < 0 {
		padLeft = 0
	}
	padRight := v.width - plainW - padLeft
	if padRight < 0 {
		padRight = 0
	}

	var styledParts []string
	for _, h := range hints {
		styledParts = append(styledParts,
			hintIconSt.Render(h.icon+" ")+
				hintDescSt.Render(h.desc+" ")+
				hintKeySt.Render("("+h.key+")"),
		)
	}
	return hintSepSt.Render(strings.Repeat(" ", padLeft)) +
		strings.Join(styledParts, hintSepSt.Render("  ")) +
		hintSepSt.Render(strings.Repeat(" ", padRight))
}

// folderIcon returns a contextual icon for the given folder, checking both
// IMAP special-use attributes and well-known name patterns (same logic as
// pickerFolderIcon in folder_picker.go).
func folderIcon(f *data.Folder) string {
	for _, attr := range f.Attributes {
		switch strings.ToLower(attr) {
		case `\sent`:
			return icons.Sent
		case `\drafts`:
			return icons.Drafts
		case `\trash`:
			return icons.Trash
		case `\junk`, `\spam`:
			return icons.Junk
		case `\archive`:
			return icons.Archive
		case `\important`, `\flagged`:
			return icons.Important
		}
	}

	name := f.DisplayName
	if name == "" {
		name = f.Name
	}
	upper := strings.ToUpper(name)
	switch upper {
	case "INBOX":
		return icons.Inbox
	case "SENT", "SENT ITEMS", "SENT MAIL":
		return icons.Sent
	case "DRAFTS":
		return icons.Drafts
	case "TRASH", "DELETED", "BIN":
		return icons.Trash
	case "ARCHIVE", "ARCHIVES":
		return icons.Archive
	case "JUNK", "SPAM":
		return icons.Junk
	case "OUTBOX":
		return icons.Outbox
	}

	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "sent"):
		return icons.Sent
	case strings.Contains(lower, "draft"):
		return icons.Drafts
	case strings.Contains(lower, "trash") || strings.Contains(lower, "deleted"):
		return icons.Trash
	case strings.Contains(lower, "junk") || strings.Contains(lower, "spam"):
		return icons.Junk
	case strings.Contains(lower, "archive"):
		return icons.Archive
	case strings.Contains(lower, "important") || strings.Contains(lower, "starred"):
		return icons.Important
	}
	return icons.Folder
}
