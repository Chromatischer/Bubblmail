package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
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

// NewFolderView creates a new folder view.
func NewFolderView(theme *config.Theme) *FolderView {
	return &FolderView{theme: theme}
}

// SetSize sets the view dimensions.
func (v *FolderView) SetSize(w, h int) {
	v.width = w
	v.height = h
}

// SetFolders updates the folder list.
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
		if v.cursor >= v.offset+v.height {
			v.offset++
		}
	}
}

// PageUp moves cursor up by a page.
func (v *FolderView) PageUp() {
	v.cursor -= v.height
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.offset = v.cursor
}

// PageDown moves cursor down by a page.
func (v *FolderView) PageDown() {
	v.cursor += v.height
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
	v.cursor = len(v.folders) - 1
	v.offset = v.cursor - v.height + 1
	if v.offset < 0 {
		v.offset = 0
	}
}

// View renders the folder browser.
func (v *FolderView) View() string {
	theme := v.theme

	if len(v.folders) == 0 {
		msg := lipgloss.NewStyle().
			Foreground(theme.TextMuted).
			Render("No folders")
		return lipgloss.Place(v.width, v.height, lipgloss.Center, lipgloss.Center, msg)
	}

	title := lipgloss.NewStyle().
		Foreground(theme.Accent).
		Bold(true).
		Padding(0, 1).
		Render("Folders — " + v.account)

	divider := lipgloss.NewStyle().
		Foreground(theme.Border).
		Render(strings.Repeat("─", v.width))

	headerRows := 2
	listHeight := v.height - headerRows
	if listHeight < 1 {
		listHeight = 1
	}

	end := v.offset + listHeight
	if end > len(v.folders) {
		end = len(v.folders)
	}

	var rows []string
	for i := v.offset; i < end; i++ {
		f := v.folders[i]
		isSelected := i == v.cursor
		rows = append(rows, v.renderFolder(f, isSelected))
	}

	for len(rows) < listHeight {
		rows = append(rows, lipgloss.NewStyle().Width(v.width).Render(""))
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		divider,
		strings.Join(rows, "\n"),
	)
}

func (v *FolderView) renderFolder(f *data.Folder, selected bool) string {
	theme := v.theme
	indent := strings.Repeat("  ", f.Depth)

	name := f.DisplayName
	if name == "" {
		name = f.Name
	}

	var lineStyle lipgloss.Style
	if selected {
		lineStyle = lipgloss.NewStyle().
			Background(theme.Selected).
			Foreground(theme.Background).
			Bold(true)
	} else {
		lineStyle = lipgloss.NewStyle().
			Foreground(theme.Text)
	}

	unreadStr := ""
	if f.Unread > 0 && !selected {
		unreadStr = lipgloss.NewStyle().
			Foreground(theme.Unread).
			Render(fmt.Sprintf(" %d", f.Unread))
	}

	nameW := v.width - len(indent) - 4 - lipgloss.Width(unreadStr)
	if nameW < 1 {
		nameW = 1
	}
	if len([]rune(name)) > nameW {
		runes := []rune(name)
		name = string(runes[:nameW-1]) + "…"
	}

	line := lineStyle.Render(" " + indent + name)
	lineW := lipgloss.Width(line)
	gap := v.width - lineW - lipgloss.Width(unreadStr)
	if gap < 0 {
		gap = 0
	}
	return line + strings.Repeat(" ", gap) + unreadStr
}
