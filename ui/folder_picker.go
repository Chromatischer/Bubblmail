package ui

import (
	"strings"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// qwertyRow holds the top keyboard row keys for instant folder selection.
var qwertyRow = []string{"w", "e", "r", "t", "y", "u", "i", "o", "p"}

// FolderPickerOverlay is a floating folder-selection menu.
type FolderPickerOverlay struct {
	styles  *Styles
	width   int
	height  int
	active  bool
	folders []*data.Folder
	cursor  int
	offset  int
	result  *data.Folder
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
	f.folders = sel
	f.cursor = 0
	f.offset = 0
	f.result = nil
	f.active = true
}

// Close hides the picker.
func (f *FolderPickerOverlay) Close() {
	f.active = false
	f.folders = nil
	f.result = nil
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

// HandleKey processes a key. Returns closed=true when done; check Result() for selection.
func (f *FolderPickerOverlay) HandleKey(key string) (closed bool) {
	// QWERTY top-row quick-select (q w e r t y u i o p → indices 0-9)
	for i, k := range qwertyRow {
		if key == k && i < len(f.folders) {
			f.result = f.folders[i]
			return true
		}
	}

	switch key {
	case "esc":
		return true
	case "enter":
		if len(f.folders) > 0 && f.cursor < len(f.folders) {
			f.result = f.folders[f.cursor]
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
		if f.cursor < len(f.folders)-1 {
			f.cursor++
			if f.cursor >= f.offset+f.listHeight() {
				f.offset++
			}
		}
	case "g":
		f.cursor = 0
		f.offset = 0
	case "G":
		if len(f.folders) > 0 {
			f.cursor = len(f.folders) - 1
			lh := f.listHeight()
			if f.cursor >= lh {
				f.offset = f.cursor - lh + 1
			}
		}
	}
	return false
}

// listHeight returns the number of folder rows that fit in the current layout.
func (f *FolderPickerOverlay) listHeight() int {
	// Overhead: Padding(1,0)=2 rows, Border=2 rows, title=1, blank=1 → 6 rows
	h := f.height - 6
	if h < 3 {
		h = 3
	}
	if h > 15 {
		h = 15
	}
	return h
}

// innerWidth computes the content area width for all folder names plus breathing room.
// Row layout: " " + key(1) + "  " + indent + name  →  4 cols fixed prefix + name.
func (f *FolderPickerOverlay) innerWidth() int {
	const titleStr = "Move to folder" // 14 chars
	const prefixW = 4                 // " " + key(1) + "  "
	w := len(titleStr)
	for _, folder := range f.folders {
		rowW := prefixW + folder.Depth*2 + util.VisibleWidth(folder.DisplayName)
		if rowW > w {
			w = rowW
		}
	}
	// Add right-side breathing room so the box isn't flush against the names.
	w += 6
	// Cap to available screen minus border (2), padding (4), and margin (4)
	if max := f.width - 10; w > max {
		w = max
	}
	if w < 22 {
		w = 22
	}
	return w
}

// View renders the folder picker overlay.
func (f *FolderPickerOverlay) View() string {
	theme := f.styles.Theme

	cw := f.innerWidth()
	listH := f.listHeight()
	boxH := listH + 2 // title row + blank row

	// nameW: how many cols are available for "indent + displayName"
	// cw is the content area width (inside Padding(1,2), so box renders cw+4+2 wide)
	nameW := cw - 4 // subtract the 4-col prefix (" " + key + "  ")

	// Title
	titleLine := lipgloss.NewStyle().Foreground(theme.Accent).Bold(true).Render(icons.FolderOpen + " Move to folder")

	// Folder rows
	var rows []string
	if len(f.folders) == 0 {
		emptyLine := util.PadRight("  "+icons.FolderEmpty+" No folders available", cw)
		rows = append(rows, lipgloss.NewStyle().Foreground(theme.TextFaint).Render(emptyLine))
	}

	for i := f.offset; i < len(f.folders) && i < f.offset+listH; i++ {
		folder := f.folders[i]
		isSelected := i == f.cursor
		hasKey := i < len(qwertyRow)

		keyChar := " "
		if hasKey {
			keyChar = qwertyRow[i]
		}

		// Build the name: truncate displayName then pad indent+name to nameW
		indent := strings.Repeat("  ", folder.Depth)
		availNameW := nameW - folder.Depth*2
		if availNameW < 1 {
			availNameW = 1
		}
		truncated := util.TruncateText(folder.DisplayName, availNameW)
		namePart := util.PadRight(indent+truncated, nameW) // exactly nameW plain cols

		if isSelected {
			// Whole row rendered as one inverted block
			plainRow := " " + keyChar + "  " + namePart
			rows = append(rows, lipgloss.NewStyle().
				Background(theme.Selected).
				Foreground(theme.Background).
				Bold(true).
				Width(cw).
				Render(plainRow))
		} else {
			// Key in accent (or dimmed if no key), name in normal text
			var styledKey string
			if hasKey {
				styledKey = lipgloss.NewStyle().
					Foreground(theme.Accent).
					Bold(true).
					Render(keyChar)
			} else {
				styledKey = lipgloss.NewStyle().
					Foreground(theme.TextFaint).
					Render(keyChar)
			}
			styledName := lipgloss.NewStyle().Foreground(theme.Text).Render(namePart)
			rows = append(rows, " "+styledKey+"  "+styledName)
		}
	}

	// Pad remaining rows for a stable box height
	blank := lipgloss.NewStyle().Background(theme.Surface).Width(cw).Render("")
	for len(rows) < listH {
		rows = append(rows, blank)
	}

	content := strings.Join(append([]string{titleLine, ""}, rows...), "\n")

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
