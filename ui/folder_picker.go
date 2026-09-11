package ui

import (
	"github.com/bubblmail/bubblmail/config"
	"strings"
	"unicode/utf8"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// FolderPickerOverlay is a floating folder-selection menu with type-to-filter search.
type FolderPickerOverlay struct {
	theme    *config.Theme
	width    int
	height   int
	active   bool
	all      []*data.Folder // all selectable folders (excluding current)
	filtered []*data.Folder // folders matching the current query
	query    string
	list     components.ScrollList
	result   *data.Folder
}

// NewFolderPickerOverlay creates a new folder picker overlay.
func NewFolderPickerOverlay(theme *config.Theme) *FolderPickerOverlay {
	return &FolderPickerOverlay{theme: theme}
}

// SetSize sets the overlay dimensions.
func (f *FolderPickerOverlay) SetSize(w, h int) {
	f.width = w
	f.height = h
}

// Open opens the picker with selectable folders, excluding currentFolder.
func (f *FolderPickerOverlay) Open(folders []*data.Folder, currentFolder string) {
	var sel []*data.Folder
	for _, folder := range folders {
		if folder.IsSelectable() && folder.Name != currentFolder {
			sel = append(sel, folder)
		}
	}
	f.all = sel
	f.filtered = sel
	f.query = ""
	f.list.Reset()
	f.result = nil
	f.active = true
}

// Close hides the picker.
func (f *FolderPickerOverlay) Close() {
	f.active = false
	f.all = nil
	f.filtered = nil
	f.result = nil
	f.query = ""
}

// IsActive returns true if the picker is open.
func (f *FolderPickerOverlay) IsActive() bool {
	return f.active
}

// Result returns the selected folder, or nil if cancelled.
func (f *FolderPickerOverlay) Result() *data.Folder {
	return f.result
}

// ClearResult resets the result after it has been consumed.
func (f *FolderPickerOverlay) ClearResult() {
	f.result = nil
}

// SetCursor moves the cursor to the given filtered-list index, adjusting scroll offset.
func (f *FolderPickerOverlay) SetCursor(filteredIdx int) {
	if filteredIdx < 0 || filteredIdx >= len(f.filtered) {
		return
	}
	f.list.Cursor = filteredIdx
	lh := f.listHeight()
	if f.list.Cursor < f.list.Offset {
		f.list.Offset = f.list.Cursor
	} else if f.list.Cursor >= f.list.Offset+lh {
		f.list.Offset = f.list.Cursor - lh + 1
	}
}

// HitTestFolder returns the filtered-list index for a click at the given
// content-area y, or -1 if no folder row was hit.
//
// Geometry: rendered box height = (listH+4)+2; boxY0 = (height-renderedH)/2.
// Inside the box: border(1)+padding(1)+title(1)+blank(1)+input(1)+sep(1) = 6
// rows of overhead before folder rows.
func (f *FolderPickerOverlay) HitTestFolder(contentY int) int {
	listH := f.listHeight()
	boxH := listH + 4
	boxY0 := (f.height - (boxH + 2)) / 2
	rowY0 := boxY0 + 6
	i := contentY - rowY0
	if i < 0 || i >= listH || f.list.Offset+i >= len(f.filtered) {
		return -1
	}
	return f.list.Offset + i
}

// applyFilter recomputes filtered from all using the current query.
func (f *FolderPickerOverlay) applyFilter() {
	if f.query == "" {
		f.filtered = f.all
		return
	}
	q := strings.ToLower(f.query)
	var out []*data.Folder
	for _, folder := range f.all {
		if strings.Contains(strings.ToLower(folder.Name), q) ||
			strings.Contains(strings.ToLower(folder.DisplayName), q) {
			out = append(out, folder)
		}
	}
	f.filtered = out
}

// HandleKey processes a key. Returns closed=true when done; check Result() for selection.
func (f *FolderPickerOverlay) HandleKey(key string) (closed bool) {
	switch key {
	case "esc":
		if f.query != "" {
			f.query = ""
			f.applyFilter()
			f.list.Reset()
			return false
		}
		return true

	case "enter":
		if len(f.filtered) > 0 && f.list.Cursor < len(f.filtered) {
			f.result = f.filtered[f.list.Cursor]
		}
		return true

	case "up", "k":
		f.list.MoveUp()

	case "down", "j":
		f.list.MoveDown(len(f.filtered), f.listHeight())

	case "ctrl+u":
		if f.query != "" {
			f.query = ""
			f.applyFilter()
			f.list.Reset()
		}

	case "backspace", "ctrl+h":
		if f.query != "" {
			_, size := utf8.DecodeLastRuneInString(f.query)
			f.query = f.query[:len(f.query)-size]
			f.applyFilter()
			f.list.Reset()
		}

	default:
		// Any single printable character appends to the filter query.
		if len(key) == 1 && key >= " " {
			f.query += key
			f.applyFilter()
			f.list.Reset()
		}
	}
	return false
}

// listHeight returns the number of folder rows the list shows.
//
// It follows the result count rather than always claiming the maximum, so a
// five-folder account gets a five-row box instead of a mostly-empty one — but
// it has a floor, so filtering down to one match does not make the box jump
// around under the cursor.
func (f *FolderPickerOverlay) listHeight() int {
	// Overhead: border(2) + padding_v(2) + title(1) + blank(1) + input(1) + sep(1) = 8
	maxH := f.height - 8
	if maxH > 15 {
		maxH = 15
	}
	if maxH < 3 {
		maxH = 3
	}

	n := len(f.filtered)
	if n < 5 {
		n = 5
	}
	if n > maxH {
		n = maxH
	}
	return n
}

// innerWidth computes the content area width that fits all folder names.
// Row layout: " " + icon(1) + " " + name → prefixW=3 + name width.
func (f *FolderPickerOverlay) innerWidth() int {
	const prefixW = 3 // " " + icon(1) + " "
	const titleStr = "Move to folder"
	w := util.VisibleWidth(icons.FolderOpen+" "+titleStr) + 1
	for _, folder := range f.all {
		// Tree-mode width: indented display name
		treeW := prefixW + folder.Depth*2 + util.VisibleWidth(folder.DisplayName)
		if treeW > w {
			w = treeW
		}
		// Flat-mode width: full path shown when filtering
		flatW := prefixW + util.VisibleWidth(folder.Name)
		if flatW > w {
			w = flatW
		}
	}
	// Right-side breathing room
	w += 4
	if max := f.width - 10; w > max {
		w = max
	}
	if w < 26 {
		w = 26
	}
	return w
}

// pickerFolderIcon returns a contextual icon for the folder based on special-use
// attributes or, failing that, well-known name patterns.
func pickerFolderIcon(folder *data.Folder) string {
	for _, attr := range folder.Attributes {
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
	name := strings.ToLower(folder.Name)
	switch {
	case name == "inbox":
		return icons.Inbox
	case strings.Contains(name, "sent"):
		return icons.Sent
	case strings.Contains(name, "draft"):
		return icons.Drafts
	case strings.Contains(name, "trash") || strings.Contains(name, "deleted"):
		return icons.Trash
	case strings.Contains(name, "junk") || strings.Contains(name, "spam"):
		return icons.Junk
	case strings.Contains(name, "archive"):
		return icons.Archive
	case strings.Contains(name, "important") || strings.Contains(name, "starred"):
		return icons.Important
	default:
		return icons.Folder
	}
}

// View renders the folder picker overlay.
func (f *FolderPickerOverlay) View() string {
	theme := f.theme

	cw := f.innerWidth()
	listH := f.listHeight()

	// innerW: actual content area inside Padding(1, 2) = cw − 4.
	// All cells and the separator must use innerW, not cw.
	const hPad = 2
	innerW := cw - 2*hPad

	// Title row — plain string, single style, no resets mid-row.
	titleLine := components.PaneTitle(theme, icons.FolderOpen+" Move to folder", innerW, theme.Surface)

	// Input row
	ti := components.NewTextInput(theme)
	ti.Placeholder = "type to filter…"
	ti.Value = f.query
	ti.Icon = icons.Search
	ti.Active = true

	inputLine := ti.Render(innerW)

	// Folder rows. The cursor is drawn with the same gutter-plus-raised-fill
	// treatment the sidebar and the search results use.
	isFiltering := f.query != ""
	const gutW = 1
	rowW := innerW - gutW - 1 // gutter on the left, scroll indicator on the right
	nameAvail := rowW - 3     // " " + icon(1) + " "

	var rows []string
	if len(f.filtered) == 0 {
		emptyMsg := icons.FolderEmpty + "  No matches"
		if len(f.all) == 0 {
			emptyMsg = icons.FolderEmpty + "  No folders available"
		}
		rows = append(rows, lipgloss.NewStyle().
			Background(theme.Surface).
			Foreground(theme.TextFaint).
			Width(innerW).
			Align(lipgloss.Center).
			Render(emptyMsg))
	}

	track := components.Scrollbar(theme, len(f.filtered), listH, f.list.Offset, listH, theme.Surface)
	for i := f.list.Offset; i < len(f.filtered) && i < f.list.Offset+listH; i++ {
		folder := f.filtered[i]
		isSelected := i == f.list.Cursor
		icon := pickerFolderIcon(folder)

		// Build the display name as a plain string.
		var nameStr string
		if isFiltering {
			// Flat mode: show full IMAP path so context is clear while filtering.
			nameStr = util.SingleLine(folder.Name)
		} else {
			// Tree mode: indented display name reflects mailbox hierarchy.
			nameStr = strings.Repeat("  ", folder.Depth) + util.SingleLine(folder.DisplayName)
		}

		bg, fg, gut := theme.Surface, theme.Text, components.GutterNone
		bold := false
		if isSelected {
			bg, fg, gut, bold = theme.SurfaceAlt, theme.Text, components.GutterActive, true
		}

		// plainRow is pure plain text — safe for Width(rowW).
		plainRow := " " + icon + " " + util.TruncateText(nameStr, nameAvail)
		row := components.GutterCell(theme, gut, bg) +
			lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(bold).Width(rowW).Render(plainRow)
		if idx := i - f.list.Offset; idx < len(track) {
			row += track[idx]
		}
		rows = append(rows, row)
	}

	// Pad remaining rows to keep the box height stable.
	blank := components.Fill(innerW, theme.Surface)
	for len(rows) < listH {
		rows = append(rows, blank)
	}

	sep := components.Divider(theme, innerW)

	// Content: title + blank + input + separator + folder rows
	contentParts := []string{titleLine, "", inputLine, sep}
	contentParts = append(contentParts, rows...)
	content := strings.Join(contentParts, "\n")

	boxH := listH + 4 // title(1) + blank(1) + input(1) + sep(1) + listH rows

	return components.ModalBox(theme, content, cw, boxH, f.width, f.height)
}
