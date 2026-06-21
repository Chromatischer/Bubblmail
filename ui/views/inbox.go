// Package views contains the swappable main-pane views.
package views

import (
	"fmt"
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// InboxView displays a scrollable list of email threads.
// Each thread occupies two rows:
//
//	● From Name                    tag1  tag2     Jun 12
//	  Re: Subject truncated…                   (3 msgs)
type InboxView struct {
	theme           *config.Theme
	width           int
	height          int
	threads         []*data.Thread
	cursor          int // focused thread index
	offset          int // first visible thread
	quick           *QuickMenuRender
	selectionAnchor int  // -1 = no multi-selection; ≥0 = anchor index
	selectionHidden bool // true while scrolling by mouse wheel: suppress the cursor highlight

	// Empty-state context: distinguishes loading / error / inbox-zero /
	// filtered-empty so the placeholder can say something useful.
	state    inboxState
	errMsg   string
	filtered bool // unread-only filter is active
}

type inboxState int

const (
	inboxReady inboxState = iota
	inboxLoading
	inboxError
)

// QuickMenuRender controls the inline quick action hint rendering.
type QuickMenuRender struct {
	Side     string
	Step     int
	MoveHint string // "" = computing, "?" = picker, else = AI-chosen folder
}

// NewInboxView creates a new inbox view.
func NewInboxView(theme *config.Theme) *InboxView {
	return &InboxView{theme: theme, selectionAnchor: -1}
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
}

// SetThreads updates the thread list and resets scroll.
func (v *InboxView) SetThreads(threads []*data.Thread) {
	v.threads = threads
	v.cursor = 0
	v.offset = 0
	v.selectionAnchor = -1
	v.selectionHidden = false
	v.state = inboxReady
	v.errMsg = ""
}

// SetLoading marks the inbox as syncing, shown only when the list is empty.
func (v *InboxView) SetLoading() { v.state = inboxLoading }

// SetError marks a load failure with a message, shown only when the list is empty.
func (v *InboxView) SetError(msg string) {
	v.state = inboxError
	v.errMsg = msg
}

// SetFiltered records whether the unread-only filter is active, so the
// empty-state copy can distinguish "no unread" from a truly empty inbox.
func (v *InboxView) SetFiltered(filtered bool) { v.filtered = filtered }

// AppendThreads replaces the thread list while preserving cursor position.
func (v *InboxView) AppendThreads(threads []*data.Thread) {
	// Remember which thread is currently selected so we can find it after reorder.
	var selectedID string
	if v.cursor >= 0 && v.cursor < len(v.threads) {
		selectedID = v.threads[v.cursor].ID
	}

	v.threads = threads
	v.selectionAnchor = -1

	// Try to find the previously selected thread in the new (possibly reordered) list.
	if selectedID != "" {
		for i, t := range v.threads {
			if t.ID == selectedID {
				v.cursor = i
				break
			}
		}
	}
	// Re-clamp the cursor and scroll offset to the new list so the selection
	// never ends up outside the rendered window.
	v.clampScroll()
}

// Len returns the number of threads loaded.
func (v *InboxView) Len() int { return len(v.threads) }

// HitTestThread returns the thread index for a click at contentY (rows from the
// top of the inbox area), or -1 if the click doesn't land on a thread row.
func (v *InboxView) HitTestThread(contentY int) int {
	if contentY < 0 || len(v.threads) == 0 {
		return -1
	}
	idx := v.offset + contentY/2 // each thread occupies 2 rows
	if idx >= 0 && idx < len(v.threads) {
		return idx
	}
	return -1
}

// CursorPos returns the current cursor index.
func (v *InboxView) CursorPos() int { return v.cursor }

// SetCursor moves the cursor to the given index, clamped to valid bounds, and
// keeps it inside the scrolled window.
func (v *InboxView) SetCursor(i int) {
	if i >= len(v.threads) {
		i = len(v.threads) - 1
	}
	if i < 0 {
		i = 0
	}
	v.cursor = i
	v.selectionHidden = false
	v.clampScroll()
}

// clampScroll keeps the cursor within bounds and repositions the scroll offset
// so the cursor is always inside the rendered window. It is the single guard
// against the cursor falling outside the visible list when threads are added,
// removed, reordered, or the viewport is resized under it.
func (v *InboxView) clampScroll() {
	n := len(v.threads)
	if n == 0 {
		v.cursor = 0
		v.offset = 0
		return
	}
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}

	visible := v.height / 2
	if visible < 1 {
		visible = 1
	}
	maxOffset := n - visible
	if maxOffset < 0 {
		maxOffset = 0
	}

	if v.selectionHidden {
		// Wheel mode: the offset leads (the viewport moves on every notch). Keep
		// it in range, and park the hidden cursor near the middle of the viewport
		// so keyboard navigation later resumes from a sensible, in-view row
		// without snapping the list back.
		if v.offset > maxOffset {
			v.offset = maxOffset
		}
		if v.offset < 0 {
			v.offset = 0
		}
		v.cursor = v.offset + visible/2
		if v.cursor > n-1 {
			v.cursor = n - 1
		}
		return
	}

	// Keyboard mode: the cursor leads. Pull the offset so the cursor keeps at
	// least `margin` threads of context from each edge (vim's "scrolloff") — a
	// smoother feel than pinning to the top/bottom row. The margin is naturally
	// not enforced past the list's top/bottom.
	margin := scrollMargin
	if m := (visible - 1) / 2; margin > m {
		margin = m
	}
	if v.cursor-margin < v.offset {
		v.offset = v.cursor - margin
	} else if v.cursor+margin >= v.offset+visible {
		v.offset = v.cursor + margin - visible + 1
	}
	if v.offset > maxOffset {
		v.offset = maxOffset
	}
	if v.offset < 0 {
		v.offset = 0
	}
}

// scrollMargin is the number of threads kept visible above and below the cursor
// while scrolling, so the selection never sits flush against the viewport edge.
const scrollMargin = 2

// LastVisibleIndex returns the index of the last thread currently in the
// viewport. Used to drive "load more" off what's on screen, so it works the
// same for wheel scrolling (offset-driven) and keyboard navigation.
func (v *InboxView) LastVisibleIndex() int {
	n := len(v.threads)
	if n == 0 {
		return -1
	}
	visible := v.height / 2
	if visible < 1 {
		visible = 1
	}
	last := v.offset + visible - 1
	if last > n-1 {
		last = n - 1
	}
	return last
}

// SelectedThread returns the currently focused thread, or nil.
func (v *InboxView) SelectedThread() *data.Thread {
	if v.cursor < 0 || v.cursor >= len(v.threads) {
		return nil
	}
	return v.threads[v.cursor]
}

// MoveUp moves cursor up.
func (v *InboxView) MoveUp() {
	v.cursor--
	v.selectionHidden = false
	v.clampScroll()
}

// MoveDown moves cursor down.
func (v *InboxView) MoveDown() {
	v.cursor++
	v.selectionHidden = false
	v.clampScroll()
}

// WheelScroll scrolls the viewport by delta threads in response to the mouse
// wheel. It moves the scroll offset directly so the list moves on every notch
// (not only once a hidden cursor reaches an edge), and keeps the selection
// highlight hidden — a permanent selection makes no sense while driving with a
// pointer.
func (v *InboxView) WheelScroll(delta int) {
	v.selectionHidden = true
	v.offset += delta
	v.clampScroll()
}

// pageSize is the number of threads a ctrl+d/ctrl+u jump moves. It is half the
// visible page (vim-style) so the jump is gentle rather than skipping a whole
// screenful at once.
func (v *InboxView) pageSize() int {
	visibleThreads := v.height / 2
	page := visibleThreads / 2
	if page < 1 {
		page = 1
	}
	return page
}

// PageUp moves cursor up by a page.
func (v *InboxView) PageUp() {
	v.cursor -= v.pageSize()
	v.selectionHidden = false
	v.clampScroll()
}

// PageDown moves cursor down by a page.
func (v *InboxView) PageDown() {
	v.cursor += v.pageSize()
	v.selectionHidden = false
	v.clampScroll()
}

// GoToTop jumps to the first thread.
func (v *InboxView) GoToTop() {
	v.cursor = 0
	v.selectionHidden = false
	v.clampScroll()
}

// GoToBottom jumps to the last thread.
func (v *InboxView) GoToBottom() {
	if len(v.threads) == 0 {
		return
	}
	v.cursor = len(v.threads) - 1
	v.selectionHidden = false
	v.clampScroll()
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

// SelectionCount returns the number of threads in the active shift-selection,
// or 0 when there is no multi-selection.
func (v *InboxView) SelectionCount() int {
	if v.selectionAnchor < 0 {
		return 0
	}
	lo, hi := v.selRange()
	return hi - lo + 1
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
func (v *InboxView) ClearSelection() {
	v.selectionAnchor = -1
}

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

// View renders the inbox thread list.
func (v *InboxView) View() string {
	if len(v.threads) == 0 {
		return v.emptyState()
	}

	// Always render with the cursor inside the window, regardless of how the
	// thread list or viewport changed since the last interaction.
	v.clampScroll()

	rowsPerThread := 2
	visibleThreads := v.height / rowsPerThread
	if visibleThreads < 1 {
		visibleThreads = 1
	}

	end := v.offset + visibleThreads
	if end > len(v.threads) {
		end = len(v.threads)
	}

	lo, hi := v.selRange()
	center := (lo + hi) / 2

	var rows []string
	for i := v.offset; i < end; i++ {
		t := v.threads[i]
		isSelected := !v.selectionHidden && i == v.cursor
		inMultiSel := v.selectionAnchor >= 0 && i >= lo && i <= hi && !isSelected
		showQuickIcon := i == center
		rows = append(rows, v.renderThread(t, isSelected, i, inMultiSel, showQuickIcon)...)
	}

	// Pad to full height
	for len(rows) < v.height {
		rows = append(rows, lipgloss.NewStyle().Width(v.width).Render(""))
	}

	return strings.Join(rows[:v.height], "\n")
}

func (v *InboxView) renderThread(t *data.Thread, selected bool, index int, inMultiSel bool, showQuickIcon bool) []string {
	fullW := v.width
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

	rowBg := lipgloss.Color("")
	if !selected && !inMultiSel && index%2 == 1 {
		rowBg = theme.Surface
	}

	sel := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(theme.Selected)
		}
		if inMultiSel {
			return s.Background(theme.SurfaceAlt)
		}
		if rowBg != "" {
			return s.Background(rowBg)
		}
		return s
	}

	fgMain := theme.Text
	fgMuted := theme.TextMuted
	if selected {
		fgMain = theme.Background
		fgMuted = theme.Background
	}

	// Flag column
	flagCh := " "
	if t.Starred {
		flagCh = icons.Flag
	}
	flag := sel(lipgloss.NewStyle().Foreground(theme.Starred)).Render(flagCh)

	// Status dot
	var dot string
	if t.HasUnread {
		dot = sel(lipgloss.NewStyle().Foreground(theme.Unread)).Render(icons.Unread)
	} else {
		dot = sel(lipgloss.NewStyle().Foreground(fgMuted)).Render(" ")
	}

	// From field
	fromStr := util.SingleLine(t.Messages[0].FromString())
	if t.HasUnread {
		if latest := t.Latest(); latest != nil {
			fromStr = util.SingleLine(latest.FromString())
		}
	}

	// Tags
	var tagStr string
	if len(t.Tags) > 0 {
		tagStyle := lipgloss.NewStyle().
			Foreground(theme.Background).
			Background(theme.Accent).
			Padding(0, 1)
		minFromW := 5
		maxTagW := contentW - 3 - lipgloss.Width(util.FormatDate(t.LastDate)) - 2 - minFromW
		if maxTagW > 0 {
			var tagParts []string
			usedW := 0
			for _, tag := range t.Tags {
				if len(tagParts) >= 2 {
					break
				}
				remaining := maxTagW - usedW
				if len(tagParts) > 0 {
					remaining--
				}
				if remaining <= 2 {
					break
				}
				labelMax := remaining - 2
				if labelMax < 1 {
					break
				}
				label := util.TruncateText(util.SingleLine(tag), labelMax)
				part := tagStyle.Render(label)
				partW := util.VisibleWidth(part)
				if partW > remaining {
					labelMax = remaining - 2
					if labelMax < 1 {
						break
					}
					label = util.TruncateText(label, labelMax)
					part = tagStyle.Render(label)
					partW = util.VisibleWidth(part)
				}
				tagParts = append(tagParts, part)
				usedW += partW
				if len(tagParts) > 0 {
					usedW++
				}
			}
			tagStr = strings.Join(tagParts, " ")
		}
	}

	dateStr := util.FormatDate(t.LastDate)
	dateW := util.VisibleWidth(dateStr)
	tagW := util.VisibleWidth(tagStr)

	const prefixW = 4
	spaceBeforeTags := 0
	if tagStr != "" {
		spaceBeforeTags = 1
	}
	fromW := contentW - prefixW - tagW - dateW - spaceBeforeTags - 1
	if fromW < 5 {
		fromW = 5
	}
	fromTrunc := util.TruncateText(fromStr, fromW)

	fromStyle := lipgloss.NewStyle().Width(fromW)
	if t.HasUnread && !selected {
		fromStyle = sel(fromStyle.Foreground(theme.Text).Bold(true))
	} else {
		fromStyle = sel(fromStyle.Foreground(fgMain))
	}
	fromRendered := fromStyle.Render(fromTrunc)

	row1Content := sel(lipgloss.NewStyle().Width(contentW)).Render(
		flag + sel(lipgloss.NewStyle()).Render(" ") +
			dot + sel(lipgloss.NewStyle()).Render(" ") +
			fromRendered +
			func() string {
				if tagStr != "" {
					return " " + tagStr
				}
				return ""
			}() +
			sel(lipgloss.NewStyle().Foreground(fgMuted)).Render(dateStr) +
			sel(lipgloss.NewStyle()).Render(" "),
	)

	subject := util.SingleLine(t.Subject)
	if subject == "" {
		if latest := t.Latest(); latest != nil {
			subject = util.SingleLine(latest.Subject)
		}
	}

	countStr := ""
	if len(t.Messages) > 1 {
		countStr = sel(lipgloss.NewStyle().Foreground(fgMuted)).Render(fmt.Sprintf("(%d)", len(t.Messages)))
	}
	countW := util.VisibleWidth(countStr)

	textW := contentW - prefixW - countW - 1
	if textW < 1 {
		textW = 1
	}
	subjectTrunc := util.TruncateText(subject, textW)
	indent := strings.Repeat(" ", prefixW)
	// Brighten the subject for unread threads so the eye can scan unread mail by
	// subject, not just by the sender/dot. Read threads stay muted.
	subjectFg := fgMuted
	if t.HasUnread && !selected {
		subjectFg = theme.Text
	}
	subjectRendered := sel(lipgloss.NewStyle().Foreground(subjectFg).Width(prefixW + textW)).Render(indent + subjectTrunc)

	row2Content := sel(lipgloss.NewStyle().Width(contentW)).Render(
		subjectRendered + countStr + sel(lipgloss.NewStyle()).Render(" "),
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

func (v *InboxView) emptyState() string {
	theme := v.theme

	var icon, title string
	var hints []string
	titleColor := theme.TextMuted

	switch v.state {
	case inboxLoading:
		icon, title = icons.Syncing, "Syncing your mail"
		hints = []string{"This only takes a moment…"}
	case inboxError:
		icon, title, titleColor = icons.Error, "Couldn't load mail", theme.Error
		if v.errMsg != "" {
			hints = append(hints, v.errMsg)
		}
		hints = append(hints, "Press ctrl+r to retry")
	default:
		if v.filtered {
			icon, title = icons.Check, "No unread messages"
			hints = []string{"Press u to show all mail"}
		} else {
			icon, title = icons.Inbox, "Inbox zero"
			hints = []string{"You're all caught up"}
		}
	}

	lines := []string{
		lipgloss.NewStyle().Foreground(titleColor).Bold(true).Render(icon + "  " + title),
	}
	for _, h := range hints {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.TextFaint).Render(h))
	}
	body := lipgloss.JoinVertical(lipgloss.Center, lines...)
	return lipgloss.Place(v.width, v.height, lipgloss.Center, lipgloss.Center, body)
}
