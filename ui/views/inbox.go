// Package views contains the swappable main-pane views.
package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// rowsPerThread is the fixed height of one thread entry.
const rowsPerThread = 2

// Row kinds in the render plan.
const (
	rowKindThread = iota
	rowKindHeader
)

// planRow is one terminal line of the list. The view keeps an explicit plan
// rather than deriving line numbers from the thread index, because date group
// headers make the mapping non-uniform — and every piece of geometry in this
// file (scrolling, paging, mouse hit-testing) then reads from the same table
// instead of each re-deriving it.
type planRow struct {
	kind   int
	thread int    // index into threads, for rowKindThread
	sub    int    // 0 = top line, 1 = detail line
	label  string // for rowKindHeader
}

// InboxView displays a scrollable list of email threads grouped by date.
//
//	 TODAY ──────────────────────────────────────────────────────
//	▌ ● Sarah Chen                          ‹work›  󰁦     09:41
//	    Q1 Planning Meeting · Let's align on the roadmap…      3
type InboxView struct {
	theme           *config.Theme
	width           int
	height          int
	threads         []*data.Thread
	cursor          int // focused thread index
	offset          int // first visible plan row
	quick           *QuickMenuRender
	selectionAnchor int // -1 = no multi-selection; ≥0 = anchor index

	plan      []planRow
	threadRow map[int]int // thread index → plan row of its first line
	grouped   bool        // date group headers enabled
}

// QuickMenuRender controls the inline quick action hint rendering.
type QuickMenuRender struct {
	Side     string
	Step     int
	MoveHint string // "" = computing, "?" = picker, else = AI-chosen folder
}

// NewInboxView creates a new inbox view.
func NewInboxView(theme *config.Theme) *InboxView {
	return &InboxView{theme: theme, selectionAnchor: -1, grouped: true}
}

// SetGrouped turns date group headers on or off. Off is appropriate where the
// list order is relevance rather than time, such as search results.
func (v *InboxView) SetGrouped(on bool) {
	if v.grouped != on {
		v.grouped = on
		v.rebuildPlan()
	}
}

// SetQuickMenu updates the inline quick action state.
// moveHint is only relevant for left/step-1 (the move action):
// "" = still computing, "?" = will open folder picker, else = AI-chosen folder.
func (v *InboxView) SetQuickMenu(side string, step int, moveHint string) {
	if side == "" {
		v.quick = nil
		return
	}
	v.quick = &QuickMenuRender{Side: side, Step: step, MoveHint: moveHint}
}

// SetSize sets the view dimensions.
func (v *InboxView) SetSize(w, h int) {
	v.width = w
	v.height = h
	v.scrollToCursor()
}

// SetThreads updates the thread list and resets scroll.
func (v *InboxView) SetThreads(threads []*data.Thread) {
	v.threads = threads
	v.cursor = 0
	v.offset = 0
	v.selectionAnchor = -1
	v.rebuildPlan()
}

// AppendThreads replaces the thread list while preserving cursor position.
func (v *InboxView) AppendThreads(threads []*data.Thread) {
	// Remember which thread is currently selected so we can find it after reorder.
	var selectedID string
	if v.cursor >= 0 && v.cursor < len(v.threads) {
		selectedID = v.threads[v.cursor].ID
	}

	v.threads = threads
	v.selectionAnchor = -1
	v.rebuildPlan()

	// Try to find the previously selected thread in the new (possibly reordered) list.
	if selectedID != "" {
		for i, t := range v.threads {
			if t.ID == selectedID {
				v.cursor = i
				v.scrollToCursor()
				return
			}
		}
	}
	// Fallback: clamp cursor.
	if v.cursor >= len(v.threads) {
		v.cursor = len(v.threads) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.scrollToCursor()
}

// rebuildPlan recomputes the line plan from the current thread list.
func (v *InboxView) rebuildPlan() {
	v.plan = v.plan[:0]
	v.threadRow = make(map[int]int, len(v.threads))

	lastBucket := ""
	for i, t := range v.threads {
		if v.grouped {
			if b := dateBucket(t.LastDate); b != lastBucket {
				// A blank row opens every group but the first. With no rules
				// anywhere in this pane, the space above a heading is what
				// makes it read as a heading rather than as another mail row.
				if lastBucket != "" {
					v.plan = append(v.plan, planRow{kind: rowKindHeader})
				}
				v.plan = append(v.plan, planRow{kind: rowKindHeader, label: b})
				lastBucket = b
			}
		}
		v.threadRow[i] = len(v.plan)
		for sub := 0; sub < rowsPerThread; sub++ {
			v.plan = append(v.plan, planRow{kind: rowKindThread, thread: i, sub: sub})
		}
	}
}

// Len returns the number of threads loaded.
func (v *InboxView) Len() int { return len(v.threads) }

// Counts returns how many loaded threads have unread mail, and how many there
// are in total. The header reports these so the count always describes what is
// actually on screen rather than a separately-tracked figure that can drift.
func (v *InboxView) Counts() (unread, total int) {
	for _, t := range v.threads {
		if t.HasUnread {
			unread++
		}
	}
	return unread, len(v.threads)
}

// HitTestThread returns the thread index for a click at contentY (rows from the
// top of the inbox area), or -1 if the click doesn't land on a thread row.
func (v *InboxView) HitTestThread(contentY int) int {
	if contentY < 0 {
		return -1
	}
	i := v.offset + contentY
	if i < 0 || i >= len(v.plan) {
		return -1
	}
	if v.plan[i].kind != rowKindThread {
		return -1
	}
	return v.plan[i].thread
}

// CursorPos returns the current cursor index.
func (v *InboxView) CursorPos() int { return v.cursor }

// SetCursor moves the cursor to the given index, clamped to valid bounds.
func (v *InboxView) SetCursor(i int) {
	if i >= len(v.threads) {
		i = len(v.threads) - 1
	}
	if i < 0 {
		i = 0
	}
	v.cursor = i
	v.scrollToCursor()
}

// SelectedThread returns the currently focused thread, or nil.
func (v *InboxView) SelectedThread() *data.Thread {
	if v.cursor < 0 || v.cursor >= len(v.threads) {
		return nil
	}
	return v.threads[v.cursor]
}

// scrollToCursor pulls the viewport so both lines of the focused thread — and
// the group header immediately above it, if any — are on screen.
func (v *InboxView) scrollToCursor() {
	if v.height <= 0 || len(v.plan) == 0 {
		return
	}
	first, ok := v.threadRow[v.cursor]
	if !ok {
		return
	}
	last := first + rowsPerThread - 1

	top := first
	if top > 0 && v.plan[top-1].kind == rowKindHeader {
		top--
	}
	if top < v.offset {
		v.offset = top
	}
	if last >= v.offset+v.height {
		v.offset = last - v.height + 1
	}
	v.clampOffset()
}

func (v *InboxView) clampOffset() {
	maxOffset := len(v.plan) - v.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.offset > maxOffset {
		v.offset = maxOffset
	}
	if v.offset < 0 {
		v.offset = 0
	}
}

// pageThreads is how many threads a page key moves by.
func (v *InboxView) pageThreads() int {
	n := v.height / rowsPerThread
	if n < 1 {
		n = 1
	}
	return n
}

// MoveUp moves cursor up.
func (v *InboxView) MoveUp() {
	if v.cursor > 0 {
		v.cursor--
		v.scrollToCursor()
	}
}

// MoveDown moves cursor down.
func (v *InboxView) MoveDown() {
	if v.cursor < len(v.threads)-1 {
		v.cursor++
		v.scrollToCursor()
	}
}

// PageUp moves cursor up by a page.
func (v *InboxView) PageUp() {
	v.cursor -= v.pageThreads()
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.scrollToCursor()
}

// PageDown moves cursor down by a page.
func (v *InboxView) PageDown() {
	v.cursor += v.pageThreads()
	if v.cursor >= len(v.threads) {
		v.cursor = len(v.threads) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
	v.scrollToCursor()
}

// GoToTop jumps to the first thread.
func (v *InboxView) GoToTop() {
	v.cursor = 0
	v.offset = 0
}

// GoToBottom jumps to the last thread.
func (v *InboxView) GoToBottom() {
	if len(v.threads) == 0 {
		return
	}
	v.cursor = len(v.threads) - 1
	v.offset = len(v.plan) // scrollToCursor clamps this back to the last page
	v.scrollToCursor()
}

// selRange returns the inclusive [lo, hi] index range of the current selection.
func (v *InboxView) selRange() (lo, hi int) {
	if v.selectionAnchor < 0 {
		return v.cursor, v.cursor
	}
	if v.cursor < v.selectionAnchor {
		return v.cursor, v.selectionAnchor
	}
	return v.selectionAnchor, v.cursor
}

// HasMultiSelection returns true when more than one thread is selected.
func (v *InboxView) HasMultiSelection() bool {
	return v.selectionAnchor >= 0 && v.selectionAnchor != v.cursor
}

// SelectedThreads returns all threads in the current selection range.
func (v *InboxView) SelectedThreads() []*data.Thread {
	lo, hi := v.selRange()
	out := make([]*data.Thread, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		if i >= 0 && i < len(v.threads) {
			out = append(out, v.threads[i])
		}
	}
	return out
}

// ClearSelection collapses multi-selection back to just the cursor.
func (v *InboxView) ClearSelection() { v.selectionAnchor = -1 }

// ShiftMoveUp extends or starts a shift-selection one step upward.
func (v *InboxView) ShiftMoveUp() {
	if v.selectionAnchor < 0 {
		v.selectionAnchor = v.cursor
	}
	v.MoveUp()
}

// ShiftMoveDown extends or starts a shift-selection one step downward.
func (v *InboxView) ShiftMoveDown() {
	if v.selectionAnchor < 0 {
		v.selectionAnchor = v.cursor
	}
	v.MoveDown()
}

// gutterW and barW are the two fixed columns framing every list row: the
// shared selection gutter on the left and the scroll position on the right.
const (
	gutterW = 1
	barW    = 1
)

// View renders the inbox thread list.
func (v *InboxView) View() string {
	if len(v.threads) == 0 {
		return components.EmptyState(v.theme, v.width, v.height,
			icons.Inbox, "Nothing in this folder",
			"ctrl+r to sync  ·  / to search  ·  c to compose")
	}

	contentW := v.width - gutterW - barW
	if contentW < 10 {
		contentW = 10
	}
	// The pane itself is transparent — only the cursor row and the marked rows
	// carry a fill, and those are small deliberate patches rather than a plane.
	const pane = lipgloss.Color("")

	v.clampOffset()
	lo, hi := v.selRange()
	center := (lo + hi) / 2

	// Rendered thread pairs are memoised because the plan visits each thread
	// twice and renderThread is not cheap.
	pairs := make(map[int][]string, v.height)

	rows := make([]string, 0, v.height)
	for i := v.offset; i < len(v.plan) && len(rows) < v.height; i++ {
		p := v.plan[i]
		if p.kind == rowKindHeader {
			if p.label == "" {
				rows = append(rows, "")
				continue
			}
			rows = append(rows,
				components.Fill(gutterW, pane)+
					components.SectionLabel(v.theme, p.label, contentW+barW, pane))
			continue
		}

		ti := p.thread
		pair, ok := pairs[ti]
		if !ok {
			isCursor := ti == v.cursor
			inMulti := v.selectionAnchor >= 0 && ti >= lo && ti <= hi && !isCursor
			pair = v.renderThread(v.threads[ti], isCursor, ti, inMulti, ti == center, contentW)
			pairs[ti] = pair
		}
		if p.sub >= len(pair) {
			continue
		}

		gutter := components.GutterNone
		switch {
		case ti == v.cursor:
			gutter = components.GutterActive
		case v.selectionAnchor >= 0 && ti >= lo && ti <= hi:
			gutter = components.GutterMarked
		}
		rows = append(rows, components.GutterCell(v.theme, gutter, pane)+pair[p.sub])
	}

	// Scroll position: one column down the right edge.
	track := components.Scrollbar(v.theme, len(v.plan), v.height, v.offset, len(rows), pane)
	for i := range rows {
		if i < len(track) {
			rows[i] += track[i]
		}
	}

	return strings.Join(components.Pad(rows, v.height, v.width, pane), "\n")
}

// dateBucket names the group a thread belongs to. Buckets get coarser as they
// recede, which is how people actually remember when mail arrived.
func dateBucket(t time.Time) string {
	now := time.Now()
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())

	switch {
	case t.IsZero():
		return "No date"
	case !t.Before(today.AddDate(0, 0, 1)):
		return "Today" // includes clock-skewed future mail
	case !t.Before(today):
		return "Today"
	case !t.Before(today.AddDate(0, 0, -1)):
		return "Yesterday"
	case !t.Before(today.AddDate(0, 0, -7)):
		return "Earlier this week"
	case !t.Before(today.AddDate(0, 0, -30)):
		return "Earlier this month"
	case t.Year() == y:
		return t.Format("January")
	default:
		return t.Format("January 2006")
	}
}

// listDate is the right-aligned timestamp. Inside the Today and Yesterday
// groups the date is already known from the header, so the clock time is the
// only new information and the column shows that instead.
func listDate(t time.Time, grouped bool) string {
	if t.IsZero() {
		return ""
	}
	if grouped {
		switch dateBucket(t) {
		case "Today", "Yesterday":
			return t.Format("15:04")
		}
	}
	return util.FormatDate(t)
}

func (v *InboxView) renderThread(t *data.Thread, selected bool, index int, inMultiSel bool, showQuickIcon bool, fullW int) []string {
	theme := v.theme
	// Quick menu renders for the cursor row normally; for other selected rows it
	// renders a colour strip (icon only shown on the centre of the selection).
	quickActive := (selected || inMultiSel) && v.quick != nil

	// We always render mail content at fullW.
	// When a quick menu is active we build a wider virtual card:
	//
	//   [ leftBadge(6) | content(fullW) | rightBadge(6) ]
	//       total = fullW + 12
	//
	// Both side panels are always exactly 6 cols wide so the content never
	// shifts position. At step 1 the active panel splits its 6 cols into two
	// tiles (2-col icon + 4-col icon/label) without changing the total width.
	//
	// Then we crop a fullW-wide window from it:
	//   - resting:    crop [6 .. 6+fullW]  → only content visible
	//   - left slide: crop [0 .. fullW]    → leftBadge + content[:fullW-6]
	//   - right slide:crop [12 .. 12+fullW]→ content[6:] + rightBadge
	contentW := fullW

	// Resting rows carry no fill at all, so the terminal shows through them.
	// The list used to zebra-stripe onto theme.Surface — a two-step lift on a
	// list with two rows per thread, which banded the whole pane. Threads are
	// told apart by their date group and their own second line.
	rowBg := lipgloss.Color("")

	// The cursor is a one-step lift plus the accent gutter — the same signal
	// the sidebar uses. It used to be a full-width fill in theme.Selected, a
	// saturated blue that both shouted across a 160-column pane and introduced
	// a second highlight colour into an app that has one accent.
	rowFill := rowBg
	switch {
	case selected:
		rowFill = theme.SurfaceAlt
	case inMultiSel:
		rowFill = theme.Surface
	}

	sel := func(s lipgloss.Style) lipgloss.Style {
		if rowFill != "" {
			return s.Background(rowFill)
		}
		return s
	}
	plain := func() lipgloss.Style { return sel(lipgloss.NewStyle()) }

	fgMain := theme.Text
	fgMuted := theme.TextMuted
	if selected {
		fgMain = theme.Text
		fgMuted = theme.TextMuted
	}

	latest := t.Latest()

	// ── Prefix: star, then unread dot ────────────────────────────────────────
	flagCh := " "
	if t.Starred {
		flagCh = icons.Flag
	}
	flag := sel(lipgloss.NewStyle().Foreground(theme.Starred)).Render(flagCh)

	// ── From ─────────────────────────────────────────────────────────────────
	fromSrc := t.Messages[0]
	if t.HasUnread && latest != nil {
		fromSrc = latest
	}
	fromStr := util.SingleLine(fromSrc.FromString())

	// The unread dot carries the sender's colour. Presence of the dot is still
	// the only thing that means unread; its hue is identity, so a column of
	// dots also reads as "these three are the same sender" at a glance. Read
	// threads stay colourless, which keeps the list from turning into confetti.
	dot := plain().Render(" ")
	if t.HasUnread {
		dot = sel(lipgloss.NewStyle().Foreground(theme.Hue(fromSrc.FromKey()))).Render(icons.Unread)
	}

	// prefixW: leading space + star + space + dot + space
	const prefixW = 5

	// ── Right cluster on line 1: tags, attachment marker, date ───────────────
	dateStr := listDate(t.LastDate, v.grouped)
	dateW := util.VisibleWidth(dateStr)

	attachStr := ""
	attachW := 0
	if threadHasAttachment(t) {
		attachStr = sel(lipgloss.NewStyle().Foreground(fgMuted)).Render(icons.Attachment)
		attachW = util.VisibleWidth(icons.Attachment) + 1
	}

	tagStr, tagW := v.renderTags(t, contentW-prefixW-dateW-attachW-8, sel)

	spaceBeforeTags := 0
	if tagW > 0 {
		spaceBeforeTags = 1
	}
	fromW := contentW - prefixW - tagW - attachW - dateW - spaceBeforeTags - 1
	if fromW < 5 {
		fromW = 5
	}

	fromStyle := lipgloss.NewStyle().Width(fromW)
	if t.HasUnread && !selected {
		fromStyle = sel(fromStyle.Foreground(theme.Text).Bold(true))
	} else {
		fromStyle = sel(fromStyle.Foreground(fgMain))
	}

	tagCell := ""
	if tagStr != "" {
		tagCell = plain().Render(" ") + tagStr
	}
	attachCell := ""
	if attachStr != "" {
		attachCell = attachStr + plain().Render(" ")
	}

	row1Content := sel(lipgloss.NewStyle().Width(contentW)).Render(
		plain().Render(" ") + flag + plain().Render(" ") + dot + plain().Render(" ") +
			fromStyle.Render(util.TruncateText(fromStr, fromW)) +
			tagCell +
			plain().Render(" ") + attachCell +
			sel(lipgloss.NewStyle().Foreground(fgMuted)).Render(dateStr) +
			plain().Render(" "),
	)

	// ── Line 2: subject, then the snippet in the space left over ─────────────
	subject := util.SingleLine(t.Subject)
	if subject == "" && latest != nil {
		subject = util.SingleLine(latest.Subject)
	}

	countStr := ""
	countW := 0
	if len(t.Messages) > 1 {
		plainCount := fmt.Sprintf("%d msgs", len(t.Messages))
		countStr = sel(lipgloss.NewStyle().Foreground(fgMuted)).Render(plainCount)
		countW = util.VisibleWidth(plainCount) + 1
	}

	textW := contentW - prefixW - countW - 1
	if textW < 1 {
		textW = 1
	}

	subjFg := fgMuted
	subjBold := false
	if t.HasUnread {
		subjFg = fgMain
		subjBold = true
	}
	subjectTrunc := util.TruncateText(subject, textW)
	line2 := sel(lipgloss.NewStyle().Foreground(subjFg).Bold(subjBold)).Render(subjectTrunc)
	usedW := util.VisibleWidth(subjectTrunc)

	// The snippet only earns its place when there is real room for it, and it
	// is always the first thing to go.
	if snip := threadSnippet(t); snip != "" && textW-usedW > 14 {
		sepStr := "  ·  "
		room := textW - usedW - util.VisibleWidth(sepStr)
		snipTrunc := util.TruncateText(snip, room)
		if util.VisibleWidth(snipTrunc) > 3 {
			line2 += sel(lipgloss.NewStyle().Foreground(theme.TextFaint)).Render(sepStr + snipTrunc)
			usedW += util.VisibleWidth(sepStr) + util.VisibleWidth(snipTrunc)
		}
	}

	row2Content := sel(lipgloss.NewStyle().Width(contentW)).Render(
		plain().Render(strings.Repeat(" ", prefixW)) +
			sel(lipgloss.NewStyle().Width(textW)).Render(line2) +
			plain().Render(" ") + countStr,
	)

	if !quickActive {
		return []string{row1Content, row2Content}
	}

	// Badge widths:
	//   badgeW  = width of the step-0 tile (READ / STAR) — stays the same at step 1
	//   moveW   = width of the step-1 tile (MOVE / DEL)  — wider than badgeW
	//
	// Left panel layout:
	//   step 0: [ READ(badgeW) ]
	//   step 1: [ MOVE(moveW) | READ(badgeW) ]   ← MOVE grows to the left of READ
	//
	// Right panel layout:
	//   step 0: [ STAR(badgeW) ]
	//   step 1: [ STAR(badgeW) | DEL(moveW) ]    ← DEL grows to the right of STAR
	//
	// cropStart keeps READ / STAR anchored at the same screen edge:
	//   left  → cropStart always 0 (left edge of left panel is the viewport left edge)
	//   right → cropStart = leftW + rightW so the window ends flush with the card right edge
	const (
		badgeW = 6
		moveW  = 9
	)

	// panelWidth returns the total column width for one side's panel.
	panelWidth := func(isActiveSide bool, step int) int {
		if isActiveSide && step == 1 {
			return badgeW + moveW
		}
		return badgeW
	}

	activeSide := v.quick.Side
	leftW := panelWidth(activeSide == "left", v.quick.Step)
	rightW := panelWidth(activeSide == "right", v.quick.Step)

	// Build each side panel as a single-row string (we split badges by "\n"
	// and handle row0/row1 separately).
	// When showQuickIcon is false (non-centre multi-select rows) we render a
	// plain colour strip instead of the icon/label tiles.
	buildPanel := func(side string, totalW int) (row0, row1 string) {
		isActiveSide := side == activeSide
		step := 0
		if isActiveSide {
			step = v.quick.Step
		}

		// For non-centre selection rows render a solid colour strip.
		if !showQuickIcon {
			var bg lipgloss.Color
			if !isActiveSide {
				bg = v.theme.Surface
			} else if side == "left" {
				if step == 0 {
					bg = v.theme.Unread
				} else if v.quick != nil && v.quick.MoveHint == "?" {
					bg = v.theme.Border
				} else {
					bg = v.theme.Accent
				}
			} else {
				if step == 0 {
					bg = v.theme.Starred
				} else {
					bg = v.theme.Error
				}
			}
			strip := lipgloss.NewStyle().Width(totalW).Background(bg).Render("")
			return strip, strip
		}

		type actionSpec struct {
			icon  string
			label string
			bg    lipgloss.Color
		}

		actions := func() []actionSpec {
			if side == "left" {
				a0 := func() actionSpec {
					if t.HasUnread {
						return actionSpec{icons.Read, "READ", v.theme.Unread}
					}
					return actionSpec{icons.Unread, "UNRD", v.theme.Unread}
				}()
				a1 := func() actionSpec {
					hint := ""
					if v.quick != nil {
						hint = v.quick.MoveHint
					}
					switch hint {
					case "?":
						return actionSpec{icons.FolderTree, "PICK", v.theme.Border}
					case "":
						return actionSpec{icons.FolderOpen, "MOVE", v.theme.Accent}
					default:
						label := folderShortName(hint, moveW-1)
						return actionSpec{icons.FolderOpen, label, v.theme.Accent}
					}
				}()
				return []actionSpec{a0, a1}
			}
			return []actionSpec{
				{icons.Star, "STAR", v.theme.Starred},
				{icons.Trash, "DEL", v.theme.Error},
			}
		}()

		renderTile := func(a actionSpec, w int, active bool) (string, string) {
			bg := a.bg
			fg := v.theme.Background
			if !active {
				bg = v.theme.Surface
				fg = v.theme.TextFaint
			}
			r0 := lipgloss.NewStyle().
				Width(w).Align(lipgloss.Center, lipgloss.Center).
				Background(bg).Foreground(fg).Bold(active).Height(1).
				Render(a.icon)
			r1 := lipgloss.NewStyle().
				Width(w).Align(lipgloss.Center, lipgloss.Center).
				Background(bg).Foreground(fg).
				Render(a.label)
			return r0, r1
		}

		if !isActiveSide || step == 0 {
			// Single tile — active side at step 0, or inactive side (always step 0 appearance).
			active := isActiveSide // inactive side is dimmed
			t0, t1 := renderTile(actions[0], totalW, active)
			return t0, t1
		}

		// Step 1: two tiles.
		// Left:  [ MOVE(moveW, active) | READ(badgeW, dimmed) ]
		// Right: [ STAR(badgeW, dimmed) | DEL(moveW, active) ]
		if side == "left" {
			t0r0, t0r1 := renderTile(actions[1], moveW, true)   // MOVE — active, wider, left
			t1r0, t1r1 := renderTile(actions[0], badgeW, false) // READ — dimmed, same width, right
			return t0r0 + t1r0, t0r1 + t1r1
		}
		// right side
		t0r0, t0r1 := renderTile(actions[0], badgeW, false) // STAR — dimmed, same width, left
		t1r0, t1r1 := renderTile(actions[1], moveW, true)   // DEL  — active, wider, right
		return t0r0 + t1r0, t0r1 + t1r1
	}

	lb0, lb1 := buildPanel("left", leftW)
	rb0, rb1 := buildPanel("right", rightW)

	card1 := lipgloss.JoinHorizontal(lipgloss.Top, lb0, row1Content, rb0)
	card2 := lipgloss.JoinHorizontal(lipgloss.Top, lb1, row2Content, rb1)

	// Crop a fullW-wide window from the virtual card:
	//   [ leftPanel(leftW) | content(fullW) | rightPanel(rightW) ]
	//
	// Left active  → cropStart=0: left panel is already flush at the left edge.
	// Right active → cropStart = leftW + rightW: window ends flush at the card's
	//                right edge, keeping STAR / DEL anchored at the right edge.
	cropStart := 0
	if activeSide == "right" {
		cropStart = leftW + rightW
	}

	cropEnd := cropStart + fullW

	row1 := ansi.Cut(card1, cropStart, cropEnd)
	row2 := ansi.Cut(card2, cropStart, cropEnd)

	return []string{row1, row2}
}

// renderTags renders up to two tag chips within budget, returning the styled
// string and its plain column width.
func (v *InboxView) renderTags(t *data.Thread, budget int, sel func(lipgloss.Style) lipgloss.Style) (string, int) {
	if len(t.Tags) == 0 || budget <= 4 {
		return "", 0
	}
	// A tag's colour is derived from its own name, so the same label is the
	// same colour in every folder and across restarts without any stored state.
	tagBase := sel(lipgloss.NewStyle().Background(v.theme.SurfaceAlt))

	var parts []string
	used := 0
	for _, tag := range t.Tags {
		if len(parts) >= 2 {
			break
		}
		gap := 0
		if len(parts) > 0 {
			gap = 1
		}
		room := budget - used - gap - 2 // 2 = the chip's own side padding
		if room < 2 {
			break
		}
		label := util.TruncateText(util.SingleLine(tag), room)
		parts = append(parts, tagBase.Foreground(v.theme.Hue(strings.ToLower(tag))).Render(" "+label+" "))
		used += gap + util.VisibleWidth(label) + 2
	}
	if len(parts) == 0 {
		return "", 0
	}
	return strings.Join(parts, sel(lipgloss.NewStyle()).Render(" ")), used
}

// threadHasAttachment reports whether any message in the thread carries one.
func threadHasAttachment(t *data.Thread) bool {
	for _, m := range t.Messages {
		if len(m.Attachments) > 0 {
			return true
		}
	}
	return false
}

// threadSnippet returns the preview text of the newest message in the thread.
func threadSnippet(t *data.Thread) string {
	m := t.Latest()
	if m == nil {
		return ""
	}
	return strings.TrimSpace(util.SingleLine(m.Snippet))
}

// folderShortName returns the last path segment of a folder name, truncated to
// maxCols terminal columns.
func folderShortName(name string, maxCols int) string {
	seg := name
	if i := strings.LastIndexAny(name, "/."); i >= 0 {
		seg = name[i+1:]
	}
	if seg == "" {
		seg = name
	}
	return util.TruncateText(seg, maxCols)
}
