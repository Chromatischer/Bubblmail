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
		Background(theme.Surface).
		Bold(true)

	for _, acct := range sb.accounts {
		// Account header
		acctLine := accountHeaderStyle.Render("  " + strings.ToUpper(acct.Name))
		lines = append(lines, acctLine)
		row++

		folders := sb.folders[acct.Name]
		for _, f := range folders {
			indent := strings.Repeat("  ", f.Depth+1)

			isActive := acct.Name == sb.activeAccount && f.Name == sb.activeFolder

			var folderStyle lipgloss.Style
			if isActive {
				folderStyle = lipgloss.NewStyle().
					Foreground(theme.Background).
					Background(theme.Accent).
					Bold(true)
			} else {
				folderStyle = lipgloss.NewStyle().
					Foreground(theme.Text).
					Background(theme.Surface)
			}

			name := f.DisplayName
			if name == "" {
				name = f.Name
			}

			var line string
			if f.Unread > 0 && !isActive {
				countStyle := lipgloss.NewStyle().
					Foreground(theme.Unread).
					Background(theme.Surface)
				unreadStr := countStyle.Render(fmt.Sprintf(" %d", f.Unread))
				// Build line with name and unread count
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
				bg := theme.Surface
				if isActive {
					bg = theme.Accent
				}
				line += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", sb.width-lineW))
			}

			sb.hitZones = append(sb.hitZones, sidebarHitZone{
				y: row, folder: f.Name, accountName: acct.Name,
			})
			lines = append(lines, line)
			row++
		}
		// Spacer between accounts
		lines = append(lines, lipgloss.NewStyle().Background(theme.Surface).Render(""))
		row++
	}

	if len(lines) == 0 {
		emptyLine := lipgloss.NewStyle().
			Foreground(theme.TextFaint).
			Background(theme.Surface).
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
			Background(theme.Surface).
			Width(sb.width).
			Render(""))
	}

	content := strings.Join(visible, "\n")
	return sb.styles.Sidebar.
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
