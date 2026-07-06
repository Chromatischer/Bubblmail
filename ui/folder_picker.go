package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// FolderPickerOverlay is a floating folder-selection menu with type-to-filter search.
type FolderPickerOverlay struct {
	styles   *Styles
	width    int
	height   int
	active   bool
	all      []*data.Folder // all selectable folders (excluding current)
	filtered []*data.Folder // folders matching the current query
	query    string
	list     components.ScrollList
	result   *data.Folder

	// collapsed mirrors the sidebar's fold state (folder name → collapsed).
	// It hides collapsed subtrees while browsing; filtering still searches all.
	collapsed map[string]bool
}

// NewFolderPickerOverlay creates a new folder picker overlay.
func NewFolderPickerOverlay(styles *Styles) *FolderPickerOverlay {
	return &FolderPickerOverlay{styles: styles}
}

// SetSize sets the overlay dimensions.
func (f *FolderPickerOverlay) SetSize(w, h int) {
	f.width = w
	f.height = h
}

// Open opens the picker with selectable folders, excluding currentFolder.
// collapsed mirrors the sidebar's fold state so collapsed subtrees stay hidden
// while browsing; pass nil to show the full tree.
func (f *FolderPickerOverlay) Open(folders []*data.Folder, currentFolder string, collapsed map[string]bool) {
	var sel []*data.Folder
	for _, folder := range folders {
		if folder.IsSelectable() && folder.Name != currentFolder {
			sel = append(sel, folder)
		}
	}
	f.all = sel
	f.collapsed = collapsed
	f.query = ""
	f.applyFilter()
	f.list.Reset()
	f.result = nil
	f.active = true
}

// Close hides the picker.
func (f *FolderPickerOverlay) Close() {
	f.active = false
	f.all = nil
	f.filtered = nil
	f.collapsed = nil
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

// folderHiddenByCollapse reports whether folder is hidden because one of its
// ancestors is collapsed. Mirrors Sidebar.isFolderHidden.
func folderHiddenByCollapse(folder *data.Folder, collapsed map[string]bool) bool {
	if folder.Depth == 0 || folder.Delimiter == "" || len(collapsed) == 0 {
		return false
	}
	parts := strings.Split(folder.Name, folder.Delimiter)
	for d := 0; d < len(parts)-1; d++ {
		if collapsed[strings.Join(parts[:d+1], folder.Delimiter)] {
			return true
		}
	}
	return false
}

// applyFilter recomputes filtered from all using the current query. With no
// query it shows the browse tree (collapsed subtrees hidden); while filtering it
// searches every folder so collapsed entries stay reachable by typing.
func (f *FolderPickerOverlay) applyFilter() {
	if f.query == "" {
		if len(f.collapsed) == 0 {
			f.filtered = f.all
			return
		}
		out := make([]*data.Folder, 0, len(f.all))
		for _, folder := range f.all {
			if !folderHiddenByCollapse(folder, f.collapsed) {
				out = append(out, folder)
			}
		}
		f.filtered = out
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

// listHeight returns the number of folder rows that fit in the current layout.
func (f *FolderPickerOverlay) listHeight() int {
	// Overhead: border(2) + padding_v(2) + title(1) + blank(1) + input(1) + sep(1) = 8
	h := f.height - 8
	if h < 3 {
		h = 3
	}
	if h > 15 {
		h = 15
	}
	return h
}

// innerWidth computes the content area width that fits all folder names.
// Row layout: " " + icon(1) + " " + name → prefixW=3 + name width.
func (f *FolderPickerOverlay) innerWidth() int {
	const prefixW = 3 // " " + icon(1) + " "
	// Tree mode reserves a 2-col chevron gutter when any subtree is collapsed.
	gutter := 0
	if len(f.collapsed) > 0 {
		gutter = 2
	}
	const titleStr = "Move to folder"
	w := util.VisibleWidth(icons.FolderOpen+" "+titleStr) + 1
	for _, folder := range f.all {
		// Tree-mode width: chevron gutter + indented display name
		treeW := prefixW + gutter + folder.Depth*2 + util.VisibleWidth(folder.DisplayName)
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

// folderTreePrefixes builds box-drawing connector prefixes for a depth-ordered
// folder list. Depth-0 folders get no connector; nested folders get ├/└ at their
// own level plus │/space continuation columns for each ancestor level. Each
// segment is two columns wide, matching the old two-space-per-depth indent.
func folderTreePrefixes(folders []*data.Folder) []string {
	n := len(folders)
	prefixes := make([]string, n)

	// last[i]: folder i is the final sibling at its depth within its parent.
	last := make([]bool, n)
	for i := 0; i < n; i++ {
		d := folders[i].Depth
		last[i] = true
		for j := i + 1; j < n; j++ {
			if folders[j].Depth < d {
				break
			}
			if folders[j].Depth == d {
				last[i] = false
				break
			}
		}
	}

	// cont[k]: the currently-open ancestor at depth k has a later sibling, so a
	// vertical guide should continue through deeper rows.
	var cont []bool
	for i := 0; i < n; i++ {
		d := folders[i].Depth
		if len(cont) < d+1 {
			cont = append(cont, make([]bool, d+1-len(cont))...)
		} else {
			cont = cont[:d+1]
		}
		cont[d] = !last[i]
		if d == 0 {
			continue // roots have no connector
		}
		var b strings.Builder
		for k := 1; k < d; k++ {
			if cont[k] {
				b.WriteString("│ ")
			} else {
				b.WriteString("  ")
			}
		}
		if last[i] {
			b.WriteString("└ ")
		} else {
			b.WriteString("├ ")
		}
		prefixes[i] = b.String()
	}
	return prefixes
}

// findMatchRange returns the byte [start,end) of the first case-insensitive
// occurrence of q in s, or (-1,-1) when q is empty or absent.
func findMatchRange(s, q string) (int, int) {
	if q == "" {
		return -1, -1
	}
	lq := strings.ToLower(q)
	var lower strings.Builder
	offsets := make([]int, 0, len(s)+1)
	for i, r := range s {
		offsets = append(offsets, i)
		lower.WriteRune(unicode.ToLower(r))
	}
	offsets = append(offsets, len(s))
	ls := lower.String()
	bi := strings.Index(ls, lq)
	if bi < 0 {
		return -1, -1
	}
	ri := utf8.RuneCountInString(ls[:bi])
	qr := utf8.RuneCountInString(lq)
	if ri+qr >= len(offsets) {
		return -1, -1
	}
	return offsets[ri], offsets[ri+qr]
}

// renderPickerCell renders a folder row to an exact contentW-wide styled cell.
// When matchStart >= 0, name[matchStart:matchEnd] is accented as a filter hit.
// Every segment carries the row background so it fills uniformly despite the
// mid-row style changes.
func renderPickerCell(theme *config.Theme, lead, name string, matchStart, matchEnd, contentW int, selected bool) string {
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

	var b strings.Builder
	b.WriteString(base.Render(lead))
	if matchStart >= 0 && matchEnd > matchStart && matchEnd <= len(name) {
		b.WriteString(base.Render(name[:matchStart]))
		b.WriteString(match.Render(name[matchStart:matchEnd]))
		b.WriteString(base.Render(name[matchEnd:]))
	} else {
		b.WriteString(base.Render(name))
	}
	if pad := contentW - util.VisibleWidth(lead) - util.VisibleWidth(name); pad > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}

// scrollbarCell renders one cell of the right-edge scrollbar.
func scrollbarCell(theme *config.Theme, thumb bool) string {
	ch, col := "░", theme.Overlay
	if thumb {
		ch, col = "█", theme.Accent
	}
	return lipgloss.NewStyle().Background(theme.Surface).Foreground(col).Render(ch)
}

// View renders the folder picker overlay.
func (f *FolderPickerOverlay) View() string {
	theme := f.styles.Theme

	cw := f.innerWidth()
	listH := f.listHeight()

	// innerW: actual content area inside Padding(1, 2) = cw − 4.
	// All cells and the separator must use innerW, not cw.
	const hPad = 2
	innerW := cw - 2*hPad

	// Title row — plain string, single style, no resets mid-row.
	titleLine := lipgloss.NewStyle().
		Background(theme.Surface).
		Foreground(theme.Accent).
		Bold(true).
		Width(innerW).
		Render(icons.FolderOpen + " Move to folder")

	// Input row
	ti := components.NewTextInput(theme)
	ti.Placeholder = "type to filter…"
	ti.Value = f.query
	ti.Icon = icons.Search
	ti.Active = true

	inputLine := ti.Render(innerW)

	// Folder rows
	isFiltering := f.query != ""

	// Reserve a right-edge column for the scrollbar when the list overflows.
	scrolling := len(f.filtered) > listH
	contentW := innerW
	if scrolling {
		contentW = innerW - 1
	}

	// Tree connector prefixes (tree mode only; filtering shows a flat list).
	var treePrefixes []string
	if !isFiltering {
		treePrefixes = folderTreePrefixes(f.filtered)
	}
	// In tree mode, reserve a left gutter for the ▶ collapsed marker when any
	// subtree is folded, so collapsed parents read like they do in the sidebar.
	markCollapse := !isFiltering && len(f.collapsed) > 0

	// Scrollbar thumb extent, in visible-row coordinates.
	thumbStart, thumbEnd := 0, 0
	if scrolling {
		total := len(f.filtered)
		thumb := listH * listH / total
		if thumb < 1 {
			thumb = 1
		}
		pos := 0
		if maxOff := total - listH; maxOff > 0 {
			pos = f.list.Offset * (listH - thumb) / maxOff
		}
		thumbStart, thumbEnd = pos, pos+thumb
	}

	var rows []string
	if len(f.filtered) == 0 {
		emptyMsg := "  " + icons.FolderEmpty + "  No matches"
		if len(f.all) == 0 {
			emptyMsg = "  " + icons.FolderEmpty + "  No folders available"
		}
		rows = append(rows, lipgloss.NewStyle().
			Background(theme.Surface).
			Foreground(theme.TextFaint).
			Width(innerW).
			Render(emptyMsg))
	}

	for i := f.list.Offset; i < len(f.filtered) && i < f.list.Offset+listH; i++ {
		folder := f.filtered[i]
		isSelected := i == f.list.Cursor
		icon := pickerFolderIcon(folder)

		// lead is the plain text before the name: leading space, collapse gutter,
		// tree connector, icon, gap. Widths are measured from this plain string.
		lead := " "
		if markCollapse {
			if f.collapsed[folder.Name] {
				lead += "▶ "
			} else {
				lead += "  "
			}
		}
		if !isFiltering {
			lead += treePrefixes[i]
		}
		lead += icon + " "
		nameAvail := contentW - util.VisibleWidth(lead)
		if nameAvail < 1 {
			nameAvail = 1
		}

		rawName := folder.DisplayName // tree mode: leaf name, hierarchy in connectors
		if isFiltering {
			rawName = folder.Name // flat mode: full IMAP path for context
		}
		name := util.TruncateText(util.SingleLine(rawName), nameAvail)

		matchStart, matchEnd := -1, -1
		if isFiltering {
			matchStart, matchEnd = findMatchRange(name, f.query)
		}

		cell := renderPickerCell(theme, lead, name, matchStart, matchEnd, contentW, isSelected)
		if scrolling {
			r := i - f.list.Offset
			cell += scrollbarCell(theme, r >= thumbStart && r < thumbEnd)
		}
		rows = append(rows, cell)
	}

	// Pad remaining rows to keep the box height stable.
	blank := lipgloss.NewStyle().Background(theme.Surface).Width(innerW).Render("")
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
