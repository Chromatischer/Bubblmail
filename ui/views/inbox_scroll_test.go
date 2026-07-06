package views

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func makeThreads(n int) []*data.Thread {
	ts := make([]*data.Thread, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("t%d", i)
		ts[i] = &data.Thread{
			ID:       id,
			Subject:  fmt.Sprintf("Subject %d", i),
			Messages: []*data.Message{{Subject: fmt.Sprintf("Subject %d", i), Date: time.Now()}},
			LastDate: time.Now(),
		}
	}
	return ts
}

// cursorVisible asserts the invariant the renderer relies on: the cursor is
// within bounds and inside the scrolled window.
func cursorVisible(t *testing.T, v *InboxView, ctx string) {
	t.Helper()
	n := len(v.threads)
	visible := v.height / 2
	if visible < 1 {
		visible = 1
	}
	if v.cursor < 0 || v.cursor >= n {
		t.Fatalf("%s: cursor %d out of bounds [0,%d)", ctx, v.cursor, n)
	}
	if v.offset < 0 || (n > 0 && v.offset >= n) {
		t.Fatalf("%s: offset %d out of bounds [0,%d)", ctx, v.offset, n)
	}
	if v.cursor < v.offset || v.cursor >= v.offset+visible {
		t.Fatalf("%s: cursor %d outside window [%d,%d)", ctx, v.cursor, v.offset, v.offset+visible)
	}
}

func TestInboxCursorStaysVisibleOnReorder(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 10) // visible = 5
	v.SetThreads(makeThreads(20))
	v.GoToBottom() // cursor=19, offset near bottom
	cursorVisible(t, v, "after GoToBottom")

	// A background load reorders the list: the selected thread (t19) is now at
	// the top. Offset previously pointed near the bottom — the cursor would be
	// off-window without a re-clamp.
	reordered := append([]*data.Thread{v.threads[19]}, makeThreads(19)...)
	v.AppendThreads(reordered)
	cursorVisible(t, v, "after reorder load")
	if v.SelectedThread() == nil || v.SelectedThread().ID != "t19" {
		t.Errorf("selection should still track t19, got %+v", v.SelectedThread())
	}
}

func TestInboxCursorStaysVisibleOnShrink(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 10)
	v.SetThreads(makeThreads(20))
	v.GoToBottom() // cursor=19, offset=15
	// The list shrinks under the cursor (e.g. filter / refresh returns fewer).
	v.AppendThreads(makeThreads(4))
	cursorVisible(t, v, "after shrink")
}

func TestInboxCursorStaysVisibleOnResize(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 30) // visible = 15
	v.SetThreads(makeThreads(20))
	v.GoToBottom() // cursor=19, offset=5
	// The window shrinks dramatically; the render must still show the cursor.
	v.SetSize(60, 6) // visible = 3
	_ = v.View()     // View() clamps
	cursorVisible(t, v, "after resize")
}

func TestInboxScrollMargin(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 12) // visible = 6
	v.SetThreads(makeThreads(50))

	visible := 6
	margin := scrollMargin
	if m := (visible - 1) / 2; margin > m {
		margin = m
	}

	// Scroll into the middle of the list one step at a time.
	for i := 0; i < 25; i++ {
		v.MoveDown()
	}

	// Mid-list, the cursor keeps `margin` threads of context above and below.
	if got := v.cursor - v.offset; got < margin {
		t.Errorf("cursor too close to top edge: %d rows above (want >= %d); cursor=%d offset=%d", got, margin, v.cursor, v.offset)
	}
	if got := (v.offset + visible - 1) - v.cursor; got < margin {
		t.Errorf("cursor too close to bottom edge: %d rows below (want >= %d); cursor=%d offset=%d", got, margin, v.cursor, v.offset)
	}

	// At the very top, the margin is not enforced past the list boundary.
	v.GoToTop()
	if v.offset != 0 {
		t.Errorf("at top, offset should be 0, got %d", v.offset)
	}
}

func TestInboxPageJumpIsHalfPage(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 20) // 10 visible threads → half-page jump = 5
	v.SetThreads(makeThreads(50))

	v.PageDown()
	if v.cursor != 5 {
		t.Errorf("ctrl+d should move a gentle half page (5 threads), cursor=%d", v.cursor)
	}
	v.PageDown()
	if v.cursor != 10 {
		t.Errorf("second ctrl+d should reach 10, cursor=%d", v.cursor)
	}
	v.PageUp()
	if v.cursor != 5 {
		t.Errorf("ctrl+u should step back a half page to 5, cursor=%d", v.cursor)
	}
}

func TestWheelScrollHidesSelection(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 12)
	v.SetThreads(makeThreads(50))

	v.WheelScroll(3)
	if !v.selectionHidden {
		t.Error("wheel scroll should hide the selection highlight")
	}
	cursorVisible(t, v, "after wheel scroll") // cursor still anchored in-window

	// Keyboard navigation re-reveals the selection.
	v.MoveDown()
	if v.selectionHidden {
		t.Error("keyboard navigation should reveal the selection again")
	}

	// Clicking (SetCursor) also reveals it.
	v.WheelScroll(3)
	v.SetCursor(v.cursor)
	if v.selectionHidden {
		t.Error("clicking a thread (SetCursor) should reveal the selection")
	}
}

func TestWheelScrollMovesViewportEveryNotch(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 12) // visible = 6, maxOffset = 50-6 = 44
	v.SetThreads(makeThreads(50))

	// Each notch moves the viewport immediately, starting from the very top.
	v.WheelScroll(3)
	if v.offset != 3 {
		t.Errorf("first notch should move offset to 3, got %d", v.offset)
	}
	v.WheelScroll(3)
	if v.offset != 6 {
		t.Errorf("second notch should move offset to 6, got %d", v.offset)
	}
	v.WheelScroll(-3)
	if v.offset != 3 {
		t.Errorf("up notch should move offset back to 3, got %d", v.offset)
	}
	// Clamps at both ends.
	v.WheelScroll(-100)
	if v.offset != 0 {
		t.Errorf("offset should clamp at 0, got %d", v.offset)
	}
	v.WheelScroll(1000)
	if v.offset != 44 {
		t.Errorf("offset should clamp at maxOffset 44, got %d", v.offset)
	}
}

func TestWheelScrollReachesEndForLoadMore(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 60) // visible = 30
	v.SetThreads(makeThreads(50))

	// Wheel all the way to the bottom.
	for i := 0; i < 100; i++ {
		v.WheelScroll(3)
	}
	n := v.Len()

	// The viewport bottom reaches the end, so load-more (driven by
	// LastVisibleIndex) fires.
	if v.LastVisibleIndex() < n-10 {
		t.Errorf("last visible index %d should be within 10 of end %d", v.LastVisibleIndex(), n)
	}
	// The centered hidden cursor does NOT reach the end — this is exactly why the
	// old cursor-based load-more trigger never fired for the wheel.
	if v.cursor >= n-10 {
		t.Errorf("cursor %d is near the end; test no longer demonstrates the wheel bug", v.cursor)
	}
}

func TestKeyboardResumeAfterWheelDoesNotSnapBack(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 12)
	v.SetThreads(makeThreads(50))

	v.WheelScroll(20) // scroll well down the list
	scrolled := v.offset
	v.MoveDown() // resume keyboard navigation
	if v.selectionHidden {
		t.Error("selection should be visible after keyboard navigation")
	}
	if v.offset < scrolled-1 || v.offset > scrolled+1 {
		t.Errorf("keyboard resume snapped the viewport: offset %d -> %d", scrolled, v.offset)
	}
}

func TestWheelScrollNoSelectedHighlightRendered(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	th := &config.Theme{
		Text: "#FFFFFF", TextMuted: "#888888", Surface: "#222233",
		Unread: "#88AAFF", Starred: "#FFCC44", Selected: "#4444AA",
		Background: "#111111", Accent: "#5577FF",
	}
	const selectedColor = "68;68;170" // #4444AA in truecolor

	v := NewInboxView(th)
	v.SetSize(60, 12)
	v.SetThreads(makeThreads(50))

	if !strings.Contains(v.View(), selectedColor) {
		t.Fatal("keyboard mode should render a selected row in the Selected color")
	}
	v.WheelScroll(3)
	if strings.Contains(v.View(), selectedColor) {
		t.Error("wheel scroll should render no selected-row highlight")
	}
	v.MoveDown()
	if !strings.Contains(v.View(), selectedColor) {
		t.Error("after keyboard nav the selected highlight should return")
	}
}

func TestInboxSetCursorAdjustsOffset(t *testing.T) {
	v := NewInboxView(&config.Theme{})
	v.SetSize(60, 10)
	v.SetThreads(makeThreads(20))
	v.GoToBottom() // offset near bottom
	v.SetCursor(0) // jump to top
	cursorVisible(t, v, "after SetCursor(0)")
	if v.offset != 0 {
		t.Errorf("offset should follow cursor to top, got %d", v.offset)
	}
}
