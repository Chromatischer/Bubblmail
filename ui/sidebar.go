package ui

import (
	"github.com/bubblmail/bubblmail/config"
	"strings"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// sidebarHitZone is a clickable row in the sidebar.
type sidebarHitZone struct {
	y           int
	folder      string
	accountName string
	depth       int
	fold        int
}

// Sidebar renders the left chrome panel: one section per account, then the
// smart folders, with unread counts right-aligned in a fixed column.
//
// Every row is composed at an exact column budget rather than padded after the
// fact, because the panel's right edge is a border the content area is placed
// against — one stray column here shifts the entire mail list.
type Sidebar struct {
	theme         *config.Theme
	width         int
	height        int
	accounts      []*data.Account
	folders       map[string][]*data.Folder // account → folders
	activeAccount string
	activeFolder  string
	hitZones      []sidebarHitZone
	rowOffset     int // scroll offset within sidebar
	totalRows     int // rows built on the last render, for scrollbar sizing

	// Smart folders
	smartCounts     map[string]int
	smartCategories []string

	// Keyboard focus navigation
	focused       bool
	cursorAccount string
	cursorFolder  string

	// Folded folders, keyed by config.FolderKey. Folding hides a mailbox's
	// descendants; the mailbox itself stays selectable, because a parent in
	// IMAP is usually a real mailbox too.
	collapsed map[string]bool
	// onFoldChange is called after a fold toggles so the app can persist it.
	onFoldChange func()
}

// SetCollapsed restores the folded set, normally from persisted state.
func (sb *Sidebar) SetCollapsed(keys []string) {
	sb.collapsed = make(map[string]bool, len(keys))
	for _, k := range keys {
		sb.collapsed[k] = true
	}
}

// CollapsedKeys returns the folded set for persisting.
func (sb *Sidebar) CollapsedKeys() []string {
	keys := make([]string, 0, len(sb.collapsed))
	for k, v := range sb.collapsed {
		if v {
			keys = append(keys, k)
		}
	}
	return keys
}

// OnFoldChange registers a callback invoked whenever a fold toggles.
func (sb *Sidebar) OnFoldChange(fn func()) { sb.onFoldChange = fn }

// hasChildren reports whether the folder at index i in folders has any
// descendants. The list is depth-ordered, so a child can only ever be the very
// next entry.
func hasChildren(folders []*data.Folder, i int) bool {
	return i+1 < len(folders) && folders[i+1].Depth > folders[i].Depth
}

// subtreeUnread totals the unread of a folded folder's hidden descendants, so
// a shut folder can still say how much is inside it. Returns 0 for anything
// that is not folded — the folder's own count is shown then.
func (sb *Sidebar) subtreeUnread(folders []*data.Folder, i, fold int) int {
	if fold != foldClosed {
		return 0
	}
	total := 0
	for j := i + 1; j < len(folders) && folders[j].Depth > folders[i].Depth; j++ {
		total += folders[j].Unread
	}
	return total
}

// IsCollapsed reports whether the given folder is folded shut.
func (sb *Sidebar) IsCollapsed(account, folder string) bool {
	return sb.collapsed[config.FolderKey(account, folder)]
}

// ToggleFold folds or unfolds the cursor folder. It reports false when the
// cursor is not on a folder that has children, so the caller can fall back to
// another meaning for the key.
func (sb *Sidebar) ToggleFold() bool {
	return sb.setFoldAt(sb.cursorAccount, sb.cursorFolder, !sb.IsCollapsed(sb.cursorAccount, sb.cursorFolder))
}

// ToggleFoldAt folds a named folder without moving the cursor there. The mouse
// uses it: clicking a chevron should fold that row and leave the selection
// where the user put it.
func (sb *Sidebar) ToggleFoldAt(account, folder string) bool {
	return sb.setFoldAt(account, folder, !sb.IsCollapsed(account, folder))
}

// Collapse folds the cursor folder shut. If it is already shut, or is a leaf,
// the cursor moves to its parent instead — the behaviour a file tree has, and
// the reason h is worth binding here.
func (sb *Sidebar) Collapse() bool {
	if sb.setFold(true) {
		return true
	}
	folders := sb.folders[sb.cursorAccount]
	for i, f := range folders {
		if f.Name != sb.cursorFolder {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if folders[j].Depth < f.Depth {
				sb.cursorFolder = folders[j].Name
				sb.scrollToCursor()
				return true
			}
		}
		return false
	}
	return false
}

// Expand unfolds the cursor folder.
func (sb *Sidebar) Expand() bool { return sb.setFold(false) }

func (sb *Sidebar) setFold(collapse bool) bool {
	return sb.setFoldAt(sb.cursorAccount, sb.cursorFolder, collapse)
}

func (sb *Sidebar) setFoldAt(account, folder string, collapse bool) bool {
	folders := sb.folders[account]
	for i, f := range folders {
		if f.Name == folder {
			if !hasChildren(folders, i) {
				return false
			}
			key := config.FolderKey(account, f.Name)
			if sb.collapsed == nil {
				sb.collapsed = map[string]bool{}
			}
			if sb.collapsed[key] == collapse {
				return false
			}
			sb.collapsed[key] = collapse
			if !collapse {
				delete(sb.collapsed, key)
			}
			if sb.onFoldChange != nil {
				sb.onFoldChange()
			}
			return true
		}
	}
	return false
}

// NewSidebar creates a new sidebar.
func NewSidebar(theme *config.Theme) *Sidebar {
	return &Sidebar{
		theme:   theme,
		folders: make(map[string][]*data.Folder),
	}
}

// SetSize sets the sidebar dimensions. w is the content width, excluding the
// right border drawn by View.
func (sb *Sidebar) SetSize(w, h int) {
	sb.width = w
	sb.height = h
}

// SetAccounts updates the account list.
func (sb *Sidebar) SetAccounts(accounts []*data.Account) { sb.accounts = accounts }

// SetFolders updates the folder list for an account.
func (sb *Sidebar) SetFolders(account string, folders []*data.Folder) {
	sb.folders[account] = folders
}

// SetActive sets the active account and folder, unfolding whatever was hiding
// it. Reading a folder that is not visible in the tree is disorienting: the
// selection highlight is simply absent, with nothing to say where it went.
func (sb *Sidebar) SetActive(account, folder string) {
	sb.activeAccount = account
	sb.activeFolder = folder
	sb.reveal(account, folder)
}

// reveal unfolds every ancestor of a folder. The folder list is depth-ordered,
// so walking backwards and taking each first-shallower entry yields the path.
func (sb *Sidebar) reveal(account, folder string) {
	folders := sb.folders[account]
	changed := false
	for i, f := range folders {
		if f.Name != folder {
			continue
		}
		depth := f.Depth
		for j := i - 1; j >= 0 && depth > 0; j-- {
			if folders[j].Depth >= depth {
				continue
			}
			depth = folders[j].Depth
			key := config.FolderKey(account, folders[j].Name)
			if sb.collapsed[key] {
				delete(sb.collapsed, key)
				changed = true
			}
		}
		break
	}
	if changed && sb.onFoldChange != nil {
		sb.onFoldChange()
	}
}

// FoldAll folds or unfolds every folder with children in one account. A mail
// account with forty mailboxes is unusable until it can be shut in one key.
func (sb *Sidebar) FoldAll(account string, collapse bool) {
	folders := sb.folders[account]
	if sb.collapsed == nil {
		sb.collapsed = map[string]bool{}
	}
	changed := false
	for i, f := range folders {
		if !hasChildren(folders, i) {
			continue
		}
		key := config.FolderKey(account, f.Name)
		if sb.collapsed[key] == collapse {
			continue
		}
		if collapse {
			sb.collapsed[key] = true
		} else {
			delete(sb.collapsed, key)
		}
		changed = true
	}
	if !changed {
		return
	}
	// Folding everything can bury the cursor. Move it to the outermost visible
	// ancestor rather than leaving it on a row that is no longer drawn.
	if collapse && account == sb.cursorAccount {
		for i, f := range folders {
			if f.Name != sb.cursorFolder || f.Depth == 0 {
				continue
			}
			for j := i - 1; j >= 0; j-- {
				if folders[j].Depth == 0 {
					sb.cursorFolder = folders[j].Name
					break
				}
			}
			break
		}
	}
	if !collapse {
		sb.reveal(sb.activeAccount, sb.activeFolder)
	}
	sb.scrollToCursor()
	if sb.onFoldChange != nil {
		sb.onFoldChange()
	}
}

// SetSmartCounts updates the category counts and category list for the smart
// folders section. Pass nil counts to hide the section.
func (sb *Sidebar) SetSmartCounts(counts map[string]int, categories []string) {
	sb.smartCounts = counts
	sb.smartCategories = categories
}

// SetFocused enables or disables keyboard navigation mode.
func (sb *Sidebar) SetFocused(focused bool) { sb.focused = focused }

// FocusAt positions the cursor at the given account+folder when entering focus mode.
func (sb *Sidebar) FocusAt(account, folder string) {
	sb.cursorAccount = account
	sb.cursorFolder = folder
	sb.scrollToCursor()
}

// MoveUp moves the cursor to the previous folder.
func (sb *Sidebar) MoveUp() { sb.step(-1) }

// MoveDown moves the cursor to the next folder.
func (sb *Sidebar) MoveDown() { sb.step(+1) }

// step moves the cursor by delta positions through the selectable rows.
func (sb *Sidebar) step(delta int) {
	for i, z := range sb.hitZones {
		if z.accountName == sb.cursorAccount && z.folder == sb.cursorFolder {
			next := i + delta
			if next >= 0 && next < len(sb.hitZones) {
				sb.cursorAccount = sb.hitZones[next].accountName
				sb.cursorFolder = sb.hitZones[next].folder
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

// Fold markers. A folder that can fold always shows a chevron, and one that
// cannot always shows a blank of the same width, so names stay on one column
// whatever the shape of the tree.
const (
	foldLeaf   = iota // nothing under it — the marker column stays blank
	foldOpen          // has children, showing them
	foldClosed        // has children, hidden
)

const (
	chevronOpen   = "▾"
	chevronClosed = "▸"
)

// sidebarEdge is the single vertical rule that separates the sidebar from the
// content. A thin bar rather than a box-drawing │, so it reads as an edge of
// the panel and not as one side of a table.
const sidebarEdge = "▏"

// HitTestFold reports whether a click landed on a foldable row's chevron, and
// on which folder. Clicking the marker itself folds; clicking anywhere else on
// the row selects, which is what a file tree does.
func (sb *Sidebar) HitTestFold(x, contentY int) (account, folder string, ok bool) {
	row := contentY + sb.rowOffset
	for _, z := range sb.hitZones {
		if z.y != row || (z.fold != foldOpen && z.fold != foldClosed) {
			continue
		}
		// gutter + leading space + one indent step per depth level.
		if x == 2+2*z.depth {
			return z.accountName, z.folder, true
		}
	}
	return "", "", false
}

// rowState classifies a row for the shared gutter/background treatment.
const (
	rowIdle = iota
	rowActive
	rowCursor
)

// sidebarRow lays out one folder line at exactly sb.width columns:
//
//	▌ ␣␣ 󰇮 Inbox                        ●6 ␣
//	│ │  │  │                            │  └ right pad / scrollbar
//	│ │  │  │                            └──── count, right-aligned
//	│ │  │  └───────────────────────────────── name, truncated to fit
//	│ │  └──────────────────────────────────── folder glyph
//	│ └─────────────────────────────────────── depth indent, 2 cols per level
//	└───────────────────────────────────────── shared selection gutter
func (sb *Sidebar) sidebarRow(width int, base, iconFg lipgloss.Color, icon, name string, depth, count, state, fold, hiddenUnread int) string {
	theme := sb.theme

	bg := base
	var fg lipgloss.Color
	bold := false
	gutter := components.GutterNone

	switch state {
	case rowActive:
		bg, fg, bold, gutter = theme.ChromeAlt, theme.Accent, true, components.GutterActive
	case rowCursor:
		bg, fg, gutter = theme.ChromeAlt, theme.Text, components.GutterActive
	default:
		fg = theme.TextMuted
		if count > 0 {
			fg = theme.Text
		}
	}

	// Reserve: gutter(1) + space(1) + chevron(1) + … + rightPad(1)
	const gutterW, leadW, rightPadW = 1, 1, 1
	indent := strings.Repeat("  ", depth)

	// The chevron occupies two columns — marker plus a space — so it never sits
	// flush against the folder glyph. Rows that cannot fold pay the same two
	// columns, which is what keeps every name in the tree on one column.
	// The marker occupies two columns — glyph plus a space — on every row,
	// foldable or not, so that one icon column runs down the whole sidebar.
	const chevW = 2
	glyph, glyphFg := " ", theme.TextFaint
	switch fold {
	case foldOpen:
		glyph = chevronOpen
	case foldClosed:
		glyph, glyphFg = chevronClosed, theme.Accent
	}
	cs := lipgloss.NewStyle().Foreground(glyphFg)
	if bg != "" {
		cs = cs.Background(bg)
	}
	chevron := cs.Render(glyph + " ")
	iconCell := ""
	iconW := 0
	if icon != "" {
		iconCell = icon + " "
		iconW = util.VisibleWidth(iconCell)
	}

	// A folded folder speaks for its children: its badge is their total.
	if hiddenUnread > 0 {
		count += hiddenUnread
	}

	countStr := ""
	countW := 0
	if count > 0 {
		countFg := theme.Unread
		if state == rowActive {
			countFg = theme.Accent
		}
		countStr = components.Count(theme, count, bg, countFg)
		countW = components.CountWidth(count) + 1 // +1 breathing room before it
	}

	nameW := width - gutterW - leadW - chevW - rightPadW - len([]rune(indent)) - iconW - countW
	if nameW < 1 {
		nameW = 1
	}

	nameSt := lipgloss.NewStyle().Foreground(fg).Bold(bold).Width(nameW)
	pad := lipgloss.NewStyle()
	if bg != "" {
		nameSt = nameSt.Background(bg)
		pad = pad.Background(bg)
	}

	return components.GutterCell(theme, gutter, bg) +
		pad.Render(" "+indent) +
		chevron +
		lipgloss.NewStyle().Foreground(iconFg).Background(bg).Render(iconCell) +
		nameSt.Render(util.TruncateText(util.SingleLine(name), nameW)) +
		pad.Render(strings.Repeat(" ", boolToInt(countW > 0))) +
		countStr
	// The trailing right-pad column is supplied by View, which may replace it
	// with a scrollbar cell.
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// View renders the sidebar.
// accountLabel renders the heading above an account's folders. It keeps the
// address in the case the user typed it: SectionLabel upper-cases, which is
// right for a word like SMART and wrong for demo@example.com.
func (sb *Sidebar) accountLabel(name string, width int, bg lipgloss.Color) string {
	theme := sb.theme
	st := lipgloss.NewStyle().Foreground(theme.TextFaint)
	if bg != "" {
		st = st.Background(bg)
	}
	text := " " + icons.User + " " + util.SingleLine(name)
	return st.Width(width).Render(util.TruncateText(text, width))
}

func (sb *Sidebar) View() string {
	theme := sb.theme
	// The panel carries no fill. Without one it needs an edge, so it ends in a
	// single dim rule — the only structural line left in the app — and that
	// rule is also the focus indicator: it takes the accent when the sidebar
	// has the keyboard.
	const bg = lipgloss.Color("")
	sb.hitZones = sb.hitZones[:0]

	// Rows are built one column short; View supplies the final column itself
	// so the scrollbar can claim it without every row knowing about it.
	rowW := sb.width - 1
	if rowW < 1 {
		rowW = 1
	}

	var lines []string
	row := 0
	blank := components.Fill(rowW, bg)

	for ai, acct := range sb.accounts {
		if ai > 0 {
			lines = append(lines, blank)
			row++
		}
		lines = append(lines, sb.accountLabel(acct.Name, rowW, bg))
		row++

		folders := sb.folders[acct.Name]
		// hideBelow is the depth at which rows are currently being skipped:
		// everything deeper than a folded parent, until the tree comes back up
		// to that parent's own level.
		hideBelow := -1
		for i, f := range folders {
			if hideBelow >= 0 {
				if f.Depth > hideBelow {
					continue
				}
				hideBelow = -1
			}

			name := f.DisplayName
			if name == "" {
				name = f.Name
			}
			state := rowIdle
			if acct.Name == sb.activeAccount && f.Name == sb.activeFolder {
				state = rowActive
			} else if sb.focused && acct.Name == sb.cursorAccount && f.Name == sb.cursorFolder {
				state = rowCursor
			}

			fold := foldLeaf
			if hasChildren(folders, i) {
				if sb.IsCollapsed(acct.Name, f.Name) {
					fold = foldClosed
					hideBelow = f.Depth
				} else {
					fold = foldOpen
				}
			}

			lines = append(lines, sb.sidebarRow(sb.width, bg, folderTint(theme, f.Name),
				folderIcon(f.Name, state != rowIdle), name, f.Depth, f.Unread, state, fold,
				sb.subtreeUnread(folders, i, fold)))
			sb.hitZones = append(sb.hitZones, sidebarHitZone{y: row, folder: f.Name, accountName: acct.Name, depth: f.Depth, fold: fold})
			row++
		}
	}

	if len(sb.smartCategories) > 0 {
		if len(lines) > 0 {
			lines = append(lines, blank)
			row++
		}
		lines = append(lines, components.SectionLabel(theme, icons.Sparkle+" Smart", rowW, bg))
		row++

		for _, cat := range sb.smartCategories {
			state := rowIdle
			if sb.activeAccount == "" && sb.activeFolder == cat {
				state = rowActive
			} else if sb.focused && sb.cursorAccount == "" && sb.cursorFolder == cat {
				state = rowCursor
			}
			lines = append(lines,
				sb.sidebarRow(sb.width, bg, smartTint(theme, cat), smartFolderIcon(cat), titleCase(cat), 0, sb.smartCounts[cat], state, foldLeaf, 0))
			sb.hitZones = append(sb.hitZones, sidebarHitZone{y: row, folder: cat, accountName: ""})
			row++
		}
	}

	sb.totalRows = len(lines)

	if len(lines) == 0 {
		lines = append(lines, lipgloss.NewStyle().
			Foreground(theme.TextFaint).Background(bg).Width(rowW).
			Render(" "+icons.Users+" No accounts"))
	}

	// Clamp the scroll offset before slicing — a folder list can shrink under
	// a stale offset after a sync.
	maxOffset := len(lines) - sb.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if sb.rowOffset > maxOffset {
		sb.rowOffset = maxOffset
	}
	if sb.rowOffset < 0 {
		sb.rowOffset = 0
	}

	end := sb.rowOffset + sb.height
	if end > len(lines) {
		end = len(lines)
	}
	visible := append([]string{}, lines[sb.rowOffset:end]...)
	for len(visible) < sb.height {
		visible = append(visible, blank)
	}

	// Last column: scrollbar when the list overflows, plain fill otherwise.
	track := components.Scrollbar(theme, len(lines), sb.height, sb.rowOffset, sb.height, bg)
	for i := range visible {
		if i < len(track) {
			visible[i] += track[i]
		} else {
			visible[i] += components.Fill(1, bg)
		}
	}

	edgeFg := theme.Border
	if sb.focused {
		edgeFg = theme.Accent
	}
	edge := lipgloss.NewStyle().Foreground(edgeFg).Render(sidebarEdge)
	for i := range visible {
		visible[i] += edge
	}

	style := lipgloss.NewStyle().
		Width(sb.width + sidebarBorderWidth).
		MaxWidth(sb.width + sidebarBorderWidth).
		Height(sb.height)
	return style.Render(strings.Join(visible, "\n"))
}

func folderIcon(name string, selected bool) string {
	switch strings.ToUpper(name) {
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

// Ring slots used by the standard mailboxes. Named so the meanings are
// readable at the call site and stay put if the ring is ever reordered.
const (
	hueBlue = iota
	hueGreen
	huePeach
	hueMauve
	hueTeal
	huePink
	hueYellow
	hueSapphire
)

// folderTint returns the identity colour for a mailbox icon. The standard
// mailboxes get fixed meanings; everything else takes a stable hue derived from
// its name, so a user's own tree is colour-coded too instead of a wall of grey.
// Only the glyph is tinted — the name keeps the row's own text colour, so the
// row still reads as one line rather than as a rainbow.
func folderTint(theme *config.Theme, name string) lipgloss.Color {
	switch strings.ToUpper(name) {
	case "INBOX":
		return theme.PaletteAt(hueBlue)
	case "SENT", "SENT ITEMS", "SENT MAIL":
		return theme.PaletteAt(hueGreen)
	case "DRAFTS":
		return theme.PaletteAt(hueYellow)
	case "ARCHIVE", "ARCHIVES":
		return theme.PaletteAt(hueTeal)
	case "OUTBOX":
		return theme.PaletteAt(hueSapphire)
	case "TRASH", "DELETED", "BIN":
		return theme.Error
	case "JUNK", "SPAM":
		return theme.Warning
	}
	return theme.Hue(name)
}

// smartTint colours a smart category. Important and Spam are states as much as
// categories, so they borrow the status colours; the rest are hashed, which
// means a category the user invents gets a colour without any code change.
func smartTint(theme *config.Theme, category string) lipgloss.Color {
	switch strings.ToUpper(category) {
	case "IMPORTANT":
		return theme.Error
	case "SPAM", "JUNK":
		return theme.Warning
	}
	return theme.Hue(category)
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
		return icons.Bookmark
	case "RECEIPTS":
		return icons.Edit
	case "SPAM":
		return icons.Junk
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
