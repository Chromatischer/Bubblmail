package views

import (
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/components"
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

// Layout constants. The header is a section label plus one blank row; the
// footer is a rule plus the hint bar. Both counts are read by listHeight and by
// HitTestFolder, so a row added to either is accounted for in exactly one place.
const (
	folderHeaderRows = 2 // section label + blank
	folderFooterRows = 2 // rule + hints
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

// CursorPos returns the current cursor index.
func (v *FolderView) CursorPos() int { return v.cursor }

// SetCursor moves the cursor to the given index, clamped to valid bounds.
func (v *FolderView) SetCursor(i int) {
	if len(v.folders) == 0 {
		v.cursor = 0
		return
	}
	if i >= len(v.folders) {
		i = len(v.folders) - 1
	}
	if i < 0 {
		i = 0
	}
	v.cursor = i
}

// HitTestFolder returns the folder index for a click at contentY (rows from the
// top of the folder view area), or -1 if the click doesn't land on a folder row.
func (v *FolderView) HitTestFolder(contentY int) int {
	listStart := folderHeaderRows
	listEnd := v.height - folderFooterRows
	if contentY < listStart || contentY >= listEnd || len(v.folders) == 0 {
		return -1
	}
	idx := v.offset + (contentY - listStart)
	if idx >= 0 && idx < len(v.folders) {
		return idx
	}
	return -1
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
	const surf = lipgloss.Color("") // the pane is transparent

	if len(v.folders) == 0 {
		return components.EmptyState(theme, v.width, v.height,
			icons.FolderTree, "No folders", "Press n to create one")
	}

	label := "Folders"
	if v.account != "" {
		label = "Folders · " + util.SingleLine(v.account)
	}

	lh := v.listHeight()
	end := v.offset + lh
	if end > len(v.folders) {
		end = len(v.folders)
	}

	track := components.Scrollbar(theme, len(v.folders), lh, v.offset, lh, surf)
	rows := make([]string, 0, lh)
	for i := v.offset; i < end; i++ {
		row := v.renderFolder(v.folders[i], i == v.cursor)
		if idx := i - v.offset; idx < len(track) {
			row += track[idx]
		}
		rows = append(rows, row)
	}
	rows = components.Pad(rows, lh, v.width, surf)

	hints := []components.Hint{
		{Icon: icons.ArrowUpDown, Key: "j/k", Desc: "navigate"},
		{Icon: icons.ChevronRight, Key: "enter", Desc: "open", Priority: 2},
		{Icon: icons.FolderNew, Key: "n", Desc: "new folder", Priority: 1},
		{Icon: icons.Close, Key: "esc", Desc: "back", Priority: 3},
	}
	bar, barW, _ := components.HintBar(theme, hints, v.width-2, 1, surf)
	footer := components.Fill(1, surf) + bar + components.Fill(v.width-1-barW, surf)

	out := []string{
		components.PaneTitle(theme, label, v.width, surf),
		components.Fill(v.width, surf),
	}
	out = append(out, rows...)
	out = append(out, components.Divider(theme, v.width), footer)
	return components.JoinRows(out)
}

// renderFolder renders a single folder row from plain-string column budgets:
//
//	[gutter][ indent + icon + name ][unread count]
//
// Every cell carries an explicit background so no terminal default leaks
// between segments, and no ANSI string is ever measured.
func (v *FolderView) renderFolder(f *data.Folder, selected bool) string {
	theme := v.theme

	bg, nameFg, gut := lipgloss.Color(""), theme.Text, components.GutterNone
	if selected {
		bg, gut = theme.SurfaceAlt, components.GutterActive
	}

	const gutW = 1
	rowW := v.width - gutW - 1 // the last column belongs to the scroll indicator

	countW := 0
	var countCell string
	if f.Unread > 0 {
		countW = components.CountWidth(f.Unread) + 1
		countCell = components.Count(theme, f.Unread, bg, theme.Unread) +
			lipgloss.NewStyle().Background(bg).Render(" ")
	}

	depthIndent := 2 * f.Depth
	nameW := rowW - 3 - depthIndent - countW // " " + icon(1) + " "
	if nameW < 1 {
		nameW = 1
	}
	name := util.SingleLine(f.DisplayName)
	if name == "" {
		name = util.SingleLine(f.Name)
	}

	plainLeft := " " + strings.Repeat(" ", depthIndent) + folderIcon(f) + " " +
		util.TruncateText(name, nameW)
	leftSt := lipgloss.NewStyle().Background(bg).Foreground(nameFg).Width(rowW - countW)
	if selected {
		leftSt = leftSt.Bold(true)
	}

	return components.GutterCell(theme, gut, bg) + leftSt.Render(plainLeft) + countCell
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
