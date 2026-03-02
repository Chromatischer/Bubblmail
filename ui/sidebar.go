package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/bubblmail/bubblmail/data"
)

// sidebarHitZone is a clickable row in the sidebar.
type sidebarHitZone struct {
	y           int
	folder      string
	accountName string
}

// Sidebar renders the left panel with folder tree and unread counts.
type Sidebar struct {
	styles        *Styles
	width         int
	height        int
	accounts      []*data.Account
	folders       map[string][]*data.Folder // account → folders
	activeAccount string
	activeFolder  string
	hitZones      []sidebarHitZone
	rowOffset     int // scroll offset within sidebar

	// Keyboard focus navigation
	focused       bool
	cursorAccount string
	cursorFolder  string
}

// NewSidebar creates a new sidebar.
func NewSidebar(styles *Styles) *Sidebar {
	return &Sidebar{
		styles:  styles,
		folders: make(map[string][]*data.Folder),
	}
}

// SetSize sets the sidebar dimensions.
func (sb *Sidebar) SetSize(w, h int) {
	sb.width = w
	sb.height = h
}

// SetAccounts updates the account list.
func (sb *Sidebar) SetAccounts(accounts []*data.Account) {
	sb.accounts = accounts
}

// SetFolders updates the folder list for an account.
func (sb *Sidebar) SetFolders(account string, folders []*data.Folder) {
	sb.folders[account] = folders
}

// SetActive sets the active account and folder.
func (sb *Sidebar) SetActive(account, folder string) {
	sb.activeAccount = account
	sb.activeFolder = folder
}

// SetFocused enables or disables keyboard navigation mode.
func (sb *Sidebar) SetFocused(focused bool) {
	sb.focused = focused
}

// FocusAt positions the cursor at the given account+folder when entering focus mode.
func (sb *Sidebar) FocusAt(account, folder string) {
	sb.cursorAccount = account
	sb.cursorFolder = folder
	sb.scrollToCursor()
}

// MoveUp moves the cursor to the previous folder.
func (sb *Sidebar) MoveUp() {
	for i, z := range sb.hitZones {
		if z.accountName == sb.cursorAccount && z.folder == sb.cursorFolder {
			if i > 0 {
				sb.cursorAccount = sb.hitZones[i-1].accountName
				sb.cursorFolder = sb.hitZones[i-1].folder
				sb.scrollToCursor()
			}
			return
		}
	}
	if len(sb.hitZones) > 0 {
		sb.cursorAccount = sb.hitZones[0].accountName
		sb.cursorFolder = sb.hitZones[0].folder
	}
}

// MoveDown moves the cursor to the next folder.
func (sb *Sidebar) MoveDown() {
	for i, z := range sb.hitZones {
		if z.accountName == sb.cursorAccount && z.folder == sb.cursorFolder {
			if i < len(sb.hitZones)-1 {
				sb.cursorAccount = sb.hitZones[i+1].accountName
				sb.cursorFolder = sb.hitZones[i+1].folder
				sb.scrollToCursor()
			}
			return
		}
	}
	if len(sb.hitZones) > 0 {
		sb.cursorAccount = sb.hitZones[0].accountName
		sb.cursorFolder = sb.hitZones[0].folder
	}
}

// GoToTop moves the cursor to the first folder.
func (sb *Sidebar) GoToTop() {
	if len(sb.hitZones) > 0 {
		sb.cursorAccount = sb.hitZones[0].accountName
		sb.cursorFolder = sb.hitZones[0].folder
		sb.scrollToCursor()
	}
}

// GoToBottom moves the cursor to the last folder.
func (sb *Sidebar) GoToBottom() {
	if len(sb.hitZones) > 0 {
		last := sb.hitZones[len(sb.hitZones)-1]
		sb.cursorAccount = last.accountName
		sb.cursorFolder = last.folder
		sb.scrollToCursor()
	}
}

// Selected returns the account and folder currently under the cursor.
func (sb *Sidebar) Selected() (account, folder string, ok bool) {
	if sb.cursorFolder == "" {
		return "", "", false
	}
	return sb.cursorAccount, sb.cursorFolder, true
}

// scrollToCursor adjusts rowOffset so the cursor line is visible.
func (sb *Sidebar) scrollToCursor() {
	for _, z := range sb.hitZones {
		if z.accountName == sb.cursorAccount && z.folder == sb.cursorFolder {
			if z.y < sb.rowOffset {
				sb.rowOffset = z.y
			} else if z.y >= sb.rowOffset+sb.height {
				sb.rowOffset = z.y - sb.height + 1
			}
			return
		}
	}
}

// HitTest returns the account/folder for a click at (x, contentY).
// contentY is 0-indexed from the top of the content area.
func (sb *Sidebar) HitTest(x, contentY int) (account, folder string, ok bool) {
	row := contentY + sb.rowOffset
	for _, z := range sb.hitZones {
		if z.y == row {
			return z.accountName, z.folder, true
		}
	}
	return "", "", false
}

// View renders the sidebar.
func (sb *Sidebar) View() string {
	theme := sb.styles.Theme
	sb.hitZones = sb.hitZones[:0]

	var lines []string
	row := 0

	accountHeaderStyle := lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Bold(true)

	for _, acct := range sb.accounts {
		// Account header
		acctLine := accountHeaderStyle.Render("  " + strings.ToUpper(acct.Name))
		lines = append(lines, acctLine)
		row++

		folders := sb.folders[acct.Name]
		for _, f := range folders {
			isActive := acct.Name == sb.activeAccount && f.Name == sb.activeFolder
			isCursor := sb.focused && acct.Name == sb.cursorAccount && f.Name == sb.cursorFolder

			var folderStyle lipgloss.Style
			var padStyle lipgloss.Style
			if isActive {
				folderStyle = lipgloss.NewStyle().
					Foreground(theme.Background).
					Background(theme.Accent).
					Bold(true)
				padStyle = lipgloss.NewStyle().Background(theme.Accent)
			} else if isCursor {
				folderStyle = lipgloss.NewStyle().
					Foreground(theme.Text).
					Background(theme.Surface)
				padStyle = lipgloss.NewStyle().Background(theme.Surface)
			} else {
				folderStyle = lipgloss.NewStyle().
					Foreground(theme.Text)
				padStyle = lipgloss.NewStyle()
			}

			name := f.DisplayName
			if name == "" {
				name = f.Name
			}

			// Build indent: depth spaces, then cursor indicator or space
			baseIndent := strings.Repeat("  ", f.Depth)
			var indent string
			if isCursor {
				marker := lipgloss.NewStyle().Foreground(theme.Accent)
				if isActive {
					marker = marker.Background(theme.Accent).Foreground(theme.Background)
				}
				indent = baseIndent + marker.Render(">") + " "
			} else {
				indent = baseIndent + "  "
			}

			var line string
			if f.Unread > 0 && !isActive {
				countStyle := lipgloss.NewStyle().
					Foreground(theme.Unread)
				unreadStr := countStyle.Render(fmt.Sprintf(" %d", f.Unread))
				available := sb.width - lipgloss.Width(indent) - 2 - lipgloss.Width(unreadStr)
				if available < 0 {
					available = 0
				}
				nameTrunc := truncateFolderName(name, available)
				line = folderStyle.Render(indent+nameTrunc) + unreadStr
			} else {
				line = folderStyle.Render(indent + name)
			}

			// Pad to full width
			lineW := lipgloss.Width(line)
			if lineW < sb.width {
				line += padStyle.Render(strings.Repeat(" ", sb.width-lineW))
			}

			sb.hitZones = append(sb.hitZones, sidebarHitZone{
				y: row, folder: f.Name, accountName: acct.Name,
			})
			lines = append(lines, line)
			row++
		}
		// Spacer between accounts
		lines = append(lines, "")
		row++
	}

	if len(lines) == 0 {
		emptyLine := lipgloss.NewStyle().
			Foreground(theme.TextFaint).
			Render("  No accounts")
		lines = append(lines, emptyLine)
	}

	// Apply scroll
	start := sb.rowOffset
	if start > len(lines) {
		start = len(lines)
	}
	end := start + sb.height
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[start:end]

	// Pad remaining height
	for len(visible) < sb.height {
		visible = append(visible, lipgloss.NewStyle().
			Width(sb.width).
			Render(""))
	}

	content := strings.Join(visible, "\n")

	borderColor := theme.Border
	if sb.focused {
		borderColor = theme.Accent
	}

	return lipgloss.NewStyle().
		Foreground(theme.Text).
		Padding(0, 1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderRight(true).
		BorderForeground(borderColor).
		Width(sb.width).
		Height(sb.height).
		Render(content)
}

func truncateFolderName(name string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	runes := []rune(name)
	if len(runes) <= maxCols {
		return name
	}
	if maxCols <= 1 {
		return "…"
	}
	return string(runes[:maxCols-1]) + "…"
}
