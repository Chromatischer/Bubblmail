package ui

import (
	"fmt"
	"strings"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/charmbracelet/lipgloss"
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

	// Smart folders
	smartCounts     map[string]int
	smartCategories []string

	// Keyboard focus navigation
	focused       bool
	cursorAccount string
	cursorFolder  string

	// Fold state: key is account+"\x00"+folderName
	collapsed map[string]bool
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

// SetSmartCounts updates the category counts and category list for the smart
// folders section. Pass nil counts to hide the section.
func (sb *Sidebar) SetSmartCounts(counts map[string]int, categories []string) {
	sb.smartCounts = counts
	sb.smartCategories = categories
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

// Toggle flips the collapsed/expanded state of the folder under the cursor.
// It is a no-op for leaf folders (those with no children).
func (sb *Sidebar) Toggle(account, folder string) {
	folders := sb.folders[account]
	hasKids := false
	for i, f := range folders {
		if f.Name == folder {
			hasKids = i+1 < len(folders) && folders[i+1].Depth > f.Depth
			break
		}
	}
	if !hasKids {
		return
	}
	if sb.collapsed == nil {
		sb.collapsed = make(map[string]bool)
	}
	key := account + "\x00" + folder
	sb.collapsed[key] = !sb.collapsed[key]
}

// isFolderHidden reports whether f should be skipped because an ancestor is collapsed.
func (sb *Sidebar) isFolderHidden(account string, f *data.Folder) bool {
	if f.Depth == 0 || f.Delimiter == "" || sb.collapsed == nil {
		return false
	}
	parts := strings.Split(f.Name, f.Delimiter)
	for d := 0; d < len(parts)-1; d++ {
		ancestor := strings.Join(parts[:d+1], f.Delimiter)
		if sb.collapsed[account+"\x00"+ancestor] {
			return true
		}
	}
	return false
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
		acctPrefix := ""
		if icons.User != "" {
			acctPrefix = icons.User + " "
		}
		acctLine := accountHeaderStyle.Render("  " + acctPrefix + strings.ToUpper(acct.Name))
		lines = append(lines, acctLine)
		row++

		allFolders := sb.folders[acct.Name]
		// Compute effective unread counts (own + all descendants) so parent
		// folders reflect their subtree totals even when children are collapsed.
		effCounts := effectiveCounts(allFolders)

		// Build a visible subset (skip children of collapsed parents).
		visibleFolders := make([]*data.Folder, 0, len(allFolders))
		for _, f := range allFolders {
			if !sb.isFolderHidden(acct.Name, f) {
				visibleFolders = append(visibleFolders, f)
			}
		}
		for fi, f := range visibleFolders {
			isActive := acct.Name == sb.activeAccount && f.Name == sb.activeFolder
			isCursor := sb.focused && acct.Name == sb.cursorAccount && f.Name == sb.cursorFolder
			isCollapsed := sb.collapsed != nil && sb.collapsed[acct.Name+"\x00"+f.Name]

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
			if icon := folderIcon(f.Name, isActive || isCursor); icon != "" {
				name = icon + " " + name
			}

			// Tree-line prefix (always shown) + leading space.
			// Pass isCollapsed so the branch bar becomes ▶ when folded.
			treePfx := " " + sidebarTreePrefix(visibleFolders, fi, isCollapsed)
			treePfxW := len([]rune(treePfx)) // ASCII-safe
			treeStyle := lipgloss.NewStyle().Foreground(theme.Border)
			if isActive {
				treeStyle = treeStyle.Background(theme.Accent)
			} else if isCursor {
				treeStyle = treeStyle.Background(theme.Surface)
			}
			indent := treeStyle.Render(treePfx)

			// innerW mirrors smart-folder layout: sb.width minus the 1-col left
			// padding and 1-col right padding from the sidebar lipgloss style.
			innerW := sb.width - 2
			nameW := innerW - treePfxW

			unread := effCounts[f.Name]
			var line string
			if unread > 0 {
				var countStyle lipgloss.Style
				if isActive {
					countStyle = lipgloss.NewStyle().Foreground(theme.Background).Background(theme.Accent).Bold(true)
				} else if isCursor {
					countStyle = lipgloss.NewStyle().Foreground(theme.Unread).Background(theme.Surface).Bold(true)
				} else {
					countStyle = lipgloss.NewStyle().Foreground(theme.Unread).Bold(true)
				}
				badge := fmt.Sprintf("●%d", unread)
				badgeStr := countStyle.Render(badge)
				// Width(cellW) pads the name so the badge lands at the right edge.
				cellW := nameW - len([]rune(badge))
				if cellW < 0 {
					cellW = 0
				}
				nameTrunc := truncateFolderName(name, cellW)
				nameCell := folderStyle.Width(cellW).Render(nameTrunc)
				line = indent + nameCell + badgeStr
			} else {
				nameTrunc := truncateFolderName(name, nameW)
				line = indent + folderStyle.Width(nameW).Render(nameTrunc)
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

	// Smart Folders section — only rendered when categories are configured
	if len(sb.smartCategories) > 0 {
		sectionStyle := lipgloss.NewStyle().
			Foreground(theme.TextMuted).
			Bold(true)
		sectionLine := sectionStyle.Render("  " + icons.Ghost + " SMART FOLDERS")
		lines = append(lines, sectionLine)
		row++

		for ci, cat := range sb.smartCategories {
			isActive := sb.activeAccount == "" && sb.activeFolder == cat
			isCursor := sb.focused && sb.cursorAccount == "" && sb.cursorFolder == cat

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

			catIcon := smartFolderIcon(cat)
			name := titleCase(cat)
			if catIcon != "" {
				name = catIcon + " " + name
			}

			// Tree-line prefix: same style as regular folders (flat list, depth 0)
			isLastCat := ci == len(sb.smartCategories)-1
			var branch string
			if isLastCat {
				branch = "└─ "
			} else {
				branch = "├─ "
			}
			treePfx := " " + branch
			treePfxW := len([]rune(treePfx)) // ASCII-safe
			treeStyle := lipgloss.NewStyle().Foreground(theme.Border)
			if isActive {
				treeStyle = treeStyle.Background(theme.Accent)
			} else if isCursor {
				treeStyle = treeStyle.Background(theme.Surface)
			}
			indent := treeStyle.Render(treePfx)

			// innerW: sidebar content width minus border/padding (1 left + 1 right).
			innerW := sb.width - 2
			nameW := innerW - treePfxW

			count := sb.smartCounts[cat]
			var line string
			if count > 0 {
				var countStyle lipgloss.Style
				if isActive {
					countStyle = lipgloss.NewStyle().Foreground(theme.Background).Background(theme.Accent).Bold(true)
				} else if isCursor {
					countStyle = lipgloss.NewStyle().Foreground(theme.Unread).Background(theme.Surface).Bold(true)
				} else {
					countStyle = lipgloss.NewStyle().Foreground(theme.Unread).Bold(true)
				}
				badge := fmt.Sprintf("●%d", count)
				badgeStr := countStyle.Render(badge)
				// nameW - badgeW leaves a fixed slot for the name; Width(N) pads
				// it so the badge always lands at the right edge.
				cellW := nameW - len([]rune(badge))
				if cellW < 0 {
					cellW = 0
				}
				nameTrunc := truncateFolderName(name, cellW)
				nameCell := folderStyle.Width(cellW).Render(nameTrunc)
				line = indent + nameCell + badgeStr
			} else {
				nameTrunc := truncateFolderName(name, nameW)
				line = indent + folderStyle.Width(nameW).Render(nameTrunc)
			}

			// Pad to full width
			lineW := lipgloss.Width(line)
			if lineW < sb.width {
				line += padStyle.Render(strings.Repeat(" ", sb.width-lineW))
			}

			sb.hitZones = append(sb.hitZones, sidebarHitZone{
				y: row, folder: cat, accountName: "",
			})
			lines = append(lines, line)
			row++
		}

		// Trailing spacer
		lines = append(lines, "")
		row++
	}

	if len(lines) == 0 {
		emptyLine := lipgloss.NewStyle().
			Foreground(theme.TextFaint).
			Render("  " + icons.Users + " No accounts")
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

// sidebarTreePrefix returns the tree-drawing prefix (e.g. "│  ├─ ") for
// folder at index idx in the visible folder slice. Each depth level occupies 3 columns.
// collapsed replaces the horizontal bar with ▶ to signal a folded subtree.
func sidebarTreePrefix(folders []*data.Folder, idx int, collapsed bool) string {
	d := folders[idx].Depth
	var b strings.Builder
	// Ancestor vertical lines: │  or spaces
	for a := 0; a < d; a++ {
		drawBar := false
		for j := idx + 1; j < len(folders); j++ {
			if folders[j].Depth <= a {
				drawBar = folders[j].Depth == a
				break
			}
		}
		if drawBar {
			b.WriteString("│  ")
		} else {
			b.WriteString("   ")
		}
	}
	// Branch connector for current node; ▶ signals a collapsed subtree.
	isLast := true
	for j := idx + 1; j < len(folders); j++ {
		if folders[j].Depth < d {
			break
		}
		if folders[j].Depth == d {
			isLast = false
			break
		}
	}
	bar := "─"
	if collapsed {
		bar = "▶"
	}
	if isLast {
		b.WriteString("└" + bar + " ")
	} else {
		b.WriteString("├" + bar + " ")
	}
	return b.String()
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

func folderIcon(name string, selected bool) string {
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
	if selected {
		return icons.FolderOpen
	}
	return icons.Folder
}

// smartFolderIcon returns the icon for a smart folder category.
func smartFolderIcon(category string) string {
	switch strings.ToUpper(category) {
	case "IMPORTANT":
		return icons.Shield
	case "GITHUB":
		return icons.Code
	case "DELIVERIES":
		return icons.Outbox
	case "NEWSLETTERS":
		return icons.Star
	case "RECEIPTS":
		return icons.Edit
	case "SPAM":
		return icons.Trash
	}
	return icons.Sparkle
}

// titleCase converts "GITHUB" → "Github", "IMPORTANT" → "Important".
func titleCase(s string) string {
	if s == "" {
		return s
	}
	lower := strings.ToLower(s)
	return strings.ToUpper(lower[:1]) + lower[1:]
}

// effectiveCounts returns a map of folder name → unread count where each
// folder's count includes its own messages plus all descendants. The input
// slice is assumed to be in tree order (parents before children), which is the
// typical IMAP LIST response ordering.
func effectiveCounts(folders []*data.Folder) map[string]int {
	counts := make(map[string]int, len(folders))
	for _, f := range folders {
		counts[f.Name] = f.Unread
	}
	// Walk backwards so children are always processed before their parents,
	// enabling a single-pass accumulation regardless of subtree depth.
	for i := len(folders) - 1; i >= 0; i-- {
		f := folders[i]
		if f.Depth == 0 || f.Delimiter == "" {
			continue
		}
		lastDelim := strings.LastIndex(f.Name, f.Delimiter)
		if lastDelim < 0 {
			continue
		}
		parentName := f.Name[:lastDelim]
		counts[parentName] += counts[f.Name]
	}
	return counts
}

// CollapsedKeys returns the set of "account\x00folder" keys that are currently
// collapsed. Used to persist sidebar state across sessions.
func (sb *Sidebar) CollapsedKeys() []string {
	keys := make([]string, 0, len(sb.collapsed))
	for k, v := range sb.collapsed {
		if v {
			keys = append(keys, k)
		}
	}
	return keys
}

// CollapsedFolders returns the set of folder names currently collapsed for the
// given account. Used to mirror the sidebar's fold state in the folder picker.
func (sb *Sidebar) CollapsedFolders(account string) map[string]bool {
	out := make(map[string]bool)
	prefix := account + "\x00"
	for k, v := range sb.collapsed {
		if v && strings.HasPrefix(k, prefix) {
			out[strings.TrimPrefix(k, prefix)] = true
		}
	}
	return out
}

// SetCollapsed initialises the collapsed map from a previously persisted key
// list (as returned by CollapsedKeys).
func (sb *Sidebar) SetCollapsed(keys []string) {
	sb.collapsed = make(map[string]bool, len(keys))
	for _, k := range keys {
		sb.collapsed[k] = true
	}
}
