package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/bubblmail/bubblmail/data"
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
	cursor   int
	offset   int
	result   *data.Folder
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
	f.cursor = 0
	f.offset = 0
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

// resetCursor resets cursor and offset to the top of the list.
func (f *FolderPickerOverlay) resetCursor() {
	f.cursor = 0
	f.offset = 0
}

// HandleKey processes a key. Returns closed=true when done; check Result() for selection.
func (f *FolderPickerOverlay) HandleKey(key string) (closed bool) {
	switch key {
	case "esc":
		if f.query != "" {
			f.query = ""
			f.applyFilter()
			f.resetCursor()
			return false
		}
		return true

	case "enter":
		if len(f.filtered) > 0 && f.cursor < len(f.filtered) {
			f.result = f.filtered[f.cursor]
		}
		return true

	case "up", "k":
		if f.cursor > 0 {
			f.cursor--
			if f.cursor < f.offset {
				f.offset--
			}
		}

	case "down", "j":
		if f.cursor < len(f.filtered)-1 {
			f.cursor++
			if f.cursor >= f.offset+f.listHeight() {
				f.offset++
			}
		}

	case "ctrl+u":
		if f.query != "" {
			f.query = ""
			f.applyFilter()
			f.resetCursor()
		}

	case "backspace", "ctrl+h":
		if f.query != "" {
			_, size := utf8.DecodeLastRuneInString(f.query)
			f.query = f.query[:len(f.query)-size]
			f.applyFilter()
			f.resetCursor()
		}

	default:
		// Any single printable character appends to the filter query.
		if len(key) == 1 && key >= " " {
			f.query += key
			f.applyFilter()
			f.resetCursor()
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
	const prefixW = 3     // " " + icon(1) + " "
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
	theme := f.styles.Theme

	cw := f.innerWidth()
	listH := f.listHeight()

	// Title row — plain string, single style, no resets mid-row.
	titleLine := lipgloss.NewStyle().
		Background(theme.Surface).
		Foreground(theme.Accent).
		Bold(true).
		Width(cw).
		Render(icons.FolderOpen + " Move to folder")

	// Input row: split into two Width(N) plain cells so every character has
	// an explicit Background(Surface) — no stray resets between segments.
	// Cell 1: " " + search icon (3 cols). Cell 2: query/placeholder (cw-3 cols).
	const iconCellW = 3 // " " + icon(1) + implicit pad(1)
	iconCell := lipgloss.NewStyle().
		Background(theme.Surface).
		Foreground(theme.Accent).
		Width(iconCellW).
		Render(" " + icons.Search)

	var textCell string
	if f.query == "" {
		textCell = lipgloss.NewStyle().
			Background(theme.Surface).
			Foreground(theme.TextFaint).
			Width(cw - iconCellW).
			Render("type to filter…")
	} else {
		textCell = lipgloss.NewStyle().
			Background(theme.Surface).
			Foreground(theme.Text).
			Width(cw - iconCellW).
			Render(util.TruncateText(util.SingleLine(f.query)+"▌", cw-iconCellW))
	}
	inputLine := iconCell + textCell

	// Folder rows
	isFiltering := f.query != ""
	nameAvail := cw - 3 // " " + icon(1) + " "

	var rows []string
	if len(f.filtered) == 0 {
		emptyMsg := "  " + icons.FolderEmpty + "  No matches"
		if len(f.all) == 0 {
			emptyMsg = "  " + icons.FolderEmpty + "  No folders available"
		}
		rows = append(rows, lipgloss.NewStyle().
			Background(theme.Surface).
			Foreground(theme.TextFaint).
			Width(cw).
			Render(emptyMsg))
	}

	for i := f.offset; i < len(f.filtered) && i < f.offset+listH; i++ {
		folder := f.filtered[i]
		isSelected := i == f.cursor
		icon := pickerFolderIcon(folder)

		// Build the display name as a plain string.
		var nameStr string
		if isFiltering {
			// Flat mode: show full IMAP path so context is clear while filtering.
			nameStr = util.SingleLine(folder.Name)
		} else {
			// Tree mode: indented display name reflects mailbox hierarchy.
			indent := strings.Repeat("  ", folder.Depth)
			nameStr = indent + util.SingleLine(folder.DisplayName)
		}

		// plainRow is pure plain text — safe for Width(cw).
		plainRow := " " + icon + " " + util.TruncateText(nameStr, nameAvail)

		if isSelected {
			rows = append(rows, lipgloss.NewStyle().
				Background(theme.Selected).
				Foreground(theme.Background).
				Bold(true).
				Width(cw).
				Render(plainRow))
		} else {
			rows = append(rows, lipgloss.NewStyle().
				Background(theme.Surface).
				Foreground(theme.Text).
				Width(cw).
				Render(plainRow))
		}
	}

	// Pad remaining rows to keep the box height stable.
	blank := lipgloss.NewStyle().Background(theme.Surface).Width(cw).Render("")
	for len(rows) < listH {
		rows = append(rows, blank)
	}

	sep := lipgloss.NewStyle().
		Foreground(theme.Border).
		Background(theme.Surface).
		Width(cw).
		Render(strings.Repeat("─", cw))

	// Content: title + blank + input + separator + folder rows
	contentParts := []string{titleLine, "", inputLine, sep}
	contentParts = append(contentParts, rows...)
	content := strings.Join(contentParts, "\n")

	boxH := listH + 4 // title(1) + blank(1) + input(1) + sep(1) + listH rows

	box := lipgloss.NewStyle().
		Width(cw).
		Height(boxH).
		Background(theme.Surface).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(f.width, f.height, lipgloss.Center, lipgloss.Center, box)
}
