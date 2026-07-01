package ui

import (
	"fmt"

	"github.com/bubblmail/bubblmail/ui/icons"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- layout ---

const sidebarWidth = 26 // content width
const sidebarBorderWidth = 1

// wheelScrollStep is how many navigation steps one mouse-wheel notch moves, so
// the wheel scrolls faster than single-step keyboard navigation.
const wheelScrollStep = 3

func sidebarRenderedWidth() int {
	return sidebarWidth + sidebarBorderWidth
}

func (a *App) headerHeight() int {
	return 3 // row1 + row2 + divider
}

func (a *App) currentSbContext() string {
	ctx := "inbox"
	switch a.viewID {
	case ViewReader:
		ctx = "reader"
		a.statusbar.SetReaderState(
			a.readerView.HasAttachments(),
			a.readerView.AttachFocusActive(),
			a.readerView.HasFocusableEvent(),
			a.readerView.CanJumpMessages(),
		)
	case ViewFolder:
		ctx = "folder"
	case ViewSmartFolder:
		ctx = "inbox"
	}
	if a.comp.IsActive() {
		ctx = "composer"
	}
	if a.searchOverlay.IsActive() {
		ctx = "search"
	}
	if a.folderPicker.IsActive() {
		ctx = "move"
	}
	if a.newFolder.IsActive() {
		ctx = "new-folder"
	}
	if a.commandPalette.IsActive() {
		ctx = "palette"
	}
	if a.sidebarFocused {
		ctx = "sidebar"
	}
	if (a.viewID == ViewInbox || a.viewID == ViewSmartFolder) && a.quickMenu != nil && a.quickMenu.side != quickMenuNone {
		ctx = "quick"
	}
	return ctx
}

func (a *App) statusHeight() int {
	return a.statusbar.Height(a.currentSbContext())
}

func (a *App) updateLayout() {
	const minSidebarTotalWidth = sidebarWidth + sidebarBorderWidth + 60
	if a.width < minSidebarTotalWidth {
		a.showSidebar = false
	} else {
		a.showSidebar = a.wantSidebar
	}

	sidebarW := 0
	if a.showSidebar {
		sidebarW = sidebarRenderedWidth()
	}

	contentW := a.width - sidebarW

	// Set widths before computing heights so statusHeight() renders correctly.
	a.header.SetWidth(a.width)
	a.statusbar.SetWidth(a.width)

	contentH := a.height - a.headerHeight() - a.statusHeight()
	if contentH < 1 {
		contentH = 1
	}

	a.header.SetAccount(a.activeAccount)
	a.header.SetFolder(a.activeFolder)
	a.sidebar.SetSize(sidebarWidth, contentH)

	a.inboxView.SetSize(contentW, contentH)
	a.readerView.SetSize(contentW, contentH)
	a.folderView.SetSize(contentW, contentH)
	a.comp.SetSize(a.width, contentH)
	a.searchOverlay.SetSize(a.width, contentH)
	a.newFolder.SetSize(a.width, contentH)
}

// View implements tea.Model.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return icons.Syncing + " Loading…"
	}

	const minW, minH = 80, 24
	if a.width < minW || a.height < minH {
		msg := fmt.Sprintf("Terminal too small (%dx%d)\nMinimum: %dx%d",
			a.width, a.height, minW, minH)
		return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(a.theme.TextMuted).Render(msg))
	}

	// Header
	header := a.header.View()

	// Content
	contentH := a.height - a.headerHeight() - a.statusHeight()
	if contentH < 1 {
		contentH = 1
	}

	var mainContent string

	// Composer overlay takes full screen.
	if a.comp.IsActive() {
		mainContent = a.comp.View()
		output := lipgloss.JoinVertical(lipgloss.Left, header, mainContent, a.statusbar.View("composer"))
		// Line buffers are only consumed by drag-to-copy, so build them only while
		// a drag is in progress instead of ANSI-stripping the whole screen every frame.
		if a.drag != nil {
			lines := a.updateLineBuffers(output)
			if a.drag.isDrag && a.drag.canCopy {
				output = applySelectionHighlight(lines,
					a.drag.startX, a.drag.startY, a.drag.endX, a.drag.endY,
					a.drag.zoneX0, a.drag.zoneY0, a.drag.zoneX1, a.drag.zoneY1)
			}
		}
		return output
	}

	// Active view
	var viewContent string
	switch a.viewID {
	case ViewInbox:
		viewContent = a.inboxView.View()
	case ViewReader:
		viewContent = a.readerView.View()
	case ViewFolder:
		viewContent = a.folderView.View()
	case ViewSmartFolder:
		viewContent = a.inboxView.View()
	}

	if a.showSidebar {
		sb := a.sidebar.View()
		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, sb, viewContent)
	} else {
		mainContent = lipgloss.NewStyle().
			Width(a.width).
			Height(contentH).
			Render(viewContent)
	}

	// Overlays
	if a.searchOverlay.IsActive() {
		mainContent = a.searchOverlay.View()
	}
	if a.folderPicker.IsActive() {
		mainContent = a.folderPicker.View()
	}
	if a.newFolder.IsActive() {
		mainContent = a.newFolder.View()
	}
	if a.commandPalette.IsActive() {
		mainContent = a.commandPalette.View()
	}
	if a.showHelp {
		mainContent = a.helpOverlay.View(a.width, contentH)
	}

	if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
		a.statusbar.SetSelectionCount(a.inboxView.SelectionCount())
	} else {
		a.statusbar.SetSelectionCount(0)
	}
	statusbar := a.statusbar.View(a.currentSbContext())

	output := lipgloss.JoinVertical(lipgloss.Left, header, mainContent, statusbar)

	// Line buffers feed drag-selection text extraction, which can only run while a
	// drag is active. Building them every frame would ANSI-strip the entire screen
	// on each keystroke, scroll notch, and idle timer tick for nothing — so only
	// refresh them when a drag is in progress.
	if a.drag != nil {
		lines := a.updateLineBuffers(output)
		// Apply visual selection highlight during an active drag (reader only).
		if a.drag.isDrag && a.drag.canCopy {
			output = applySelectionHighlight(lines,
				a.drag.startX, a.drag.startY, a.drag.endX, a.drag.endY,
				a.drag.zoneX0, a.drag.zoneY0, a.drag.zoneX1, a.drag.zoneY1)
		}
	}

	return output
}

func (a *App) maybeLoadMore() tea.Cmd {
	if a.viewID != ViewInbox || a.loadingMore || a.allLoaded {
		return nil
	}
	client, ok := a.imapClients[a.activeAccount]
	if !ok {
		return nil
	}
	// Trigger off the bottom of the viewport rather than the cursor, so wheel
	// scrolling (offset-driven, cursor parked mid-view) prefetches just like
	// keyboard navigation does as the cursor nears the end.
	n := a.inboxView.Len()
	if n == 0 || a.inboxView.LastVisibleIndex() < n-10 {
		return nil
	}
	a.loadingMore = true
	a.statusbar.SetLoading(true)
	return tea.Batch(
		client.FetchMoreMessages(a.activeFolder, a.fetchedCount, a.cfg.General.PageSize),
		spinnerTick(),
	)
}

// --- utility ---

// flash sets a temporary status message. The auto-clear timer is scheduled
// centrally in Update, so callers may use or discard the (nil) return freely.
func (a *App) flash(msg, kind string) tea.Cmd {
	a.statusbar.SetMessage(msg, kind)
	a.updateLayout() // flash message adds a line; recalculate content height
	return nil
}
