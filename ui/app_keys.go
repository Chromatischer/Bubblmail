package ui

import (
	"fmt"
	"time"

	"github.com/bubblmail/bubblmail/thread"
	"github.com/bubblmail/bubblmail/ui/icons"
	tea "github.com/charmbracelet/bubbletea"
)

// dispatchGlobalKey handles top-level (non-overlay) key actions. It is also the
// entry point used by the command palette to run a chosen action by its key.
func (a *App) dispatchGlobalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return a, tea.Quit

	case "q":
		if a.quitPending {
			return a, tea.Quit
		}
		a.quitPending = true
		return a, tea.Batch(
			a.flash("press q again to quit", "info"),
			tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
				return quitTimeoutMsg{}
			}),
		)

	case "?":
		a.showHelp = true

	case "z":
		if a.viewID == ViewReader {
			a.readerView.ToggleQuoteFolds()
		}

	case "]":
		if a.viewID == ViewReader {
			a.readerView.JumpToMessage(1)
		}

	case "[":
		if a.viewID == ViewReader {
			a.readerView.JumpToMessage(-1)
		}

	case "b":
		a.wantSidebar = !a.wantSidebar
		a.updateLayout()

	case KeyAttachments:
		// Enter/leave the attachment section in the reader. Dedicated key so Tab
		// stays free to focus the sidebar.
		if a.viewID == ViewReader && a.readerView.HasAttachments() {
			if a.readerView.AttachFocusActive() {
				a.readerView.ExitAttachments()
			} else {
				a.readerView.EnterAttachments()
			}
			return a, nil
		}

	case "tab":
		// Within the attachment section, Tab cycles attachments; otherwise it
		// focuses the sidebar (consistent across views).
		if a.viewID == ViewReader && a.readerView.AttachFocusActive() {
			a.readerView.FocusNextAttachment(1)
			return a, nil
		}
		a.focusSidebar()

	case KeySidebarFocus: // "\" — dedicated, never-overloaded sidebar focus
		a.focusSidebar()

	case "i":
		a.activeFolder = "INBOX"
		a.viewID = ViewInbox
		a.sidebar.SetActive(a.activeAccount, a.activeFolder)
		a.header.SetFolder("INBOX")
		return a, a.fetchMessages()

	case "ctrl+r":
		a.header.SetSyncState("syncing")
		a.statusbar.SetLoading(true)
		return a, tea.Batch(a.fetchMessages(), spinnerTick())

	case "ctrl+z":
		return a, a.performUndo()

	case ".":
		a.openCommandPalette()

	case "shift+tab":
		if a.viewID == ViewReader && a.readerView.AttachFocusActive() {
			a.readerView.FocusNextAttachment(-1)
			return a, nil
		}

	case "/":
		contentH := a.height - a.headerHeight() - a.statusHeight()
		if contentH < 1 {
			contentH = 1
		}
		a.searchOverlay.SetSize(a.width, contentH)
		if addrs, err := a.store.KnownAddresses(); err == nil {
			a.searchOverlay.SetKnownAddresses(addrs)
		}
		a.searchOverlay.Open()
		a.searchState = nil

	case "ctrl+f":
		// IMAP server search
		client, ok := a.imapClients[a.activeAccount]
		if ok {
			a.flash(fmt.Sprintf("%s Searching server…", icons.Search), "info")
			q := a.searchOverlay.Query()
			return a, client.SearchIMAP(a.activeFolder, q)
		}

	// View-specific navigation
	case "shift+down":
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.closeQuickMenu()
			a.inboxView.ShiftMoveDown()
			return a, a.maybeLoadMore()
		}

	case "shift+up":
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.closeQuickMenu()
			a.inboxView.ShiftMoveUp()
		}

	case "j", "down":
		a.closeQuickMenu()
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.inboxView.ClearSelection()
		}
		a.moveDown()
		return a, a.maybeLoadMore()

	case "k", "up":
		a.closeQuickMenu()
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.inboxView.ClearSelection()
		}
		a.moveUp()
		return a, nil

	case "ctrl+down":
		// Fast keyboard scroll, matching the mouse-wheel step (selection stays).
		a.closeQuickMenu()
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.inboxView.ClearSelection()
		}
		for i := 0; i < wheelScrollStep; i++ {
			a.moveDown()
		}
		return a, a.maybeLoadMore()

	case "ctrl+up":
		a.closeQuickMenu()
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.inboxView.ClearSelection()
		}
		for i := 0; i < wheelScrollStep; i++ {
			a.moveUp()
		}
		return a, nil

	case "ctrl+d":
		a.closeQuickMenu()
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.inboxView.ClearSelection()
		}
		a.pageDown()
		return a, a.maybeLoadMore()

	case "ctrl+u":
		a.closeQuickMenu()
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.inboxView.ClearSelection()
		}
		a.pageUp()
		return a, nil

	case "g":
		a.closeQuickMenu()
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.inboxView.ClearSelection()
		}
		a.goToTop()
		return a, nil

	case "G":
		a.closeQuickMenu()
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.inboxView.ClearSelection()
		}
		a.goToBottom()
		return a, a.maybeLoadMore()

	case "enter":
		if a.viewID == ViewReader && a.readerView.AttachFocusActive() {
			return a, a.executeAttachmentAction()
		}
		if a.viewID == ViewReader && a.readerView.HasFocusableEvent() {
			if cmd := a.copySuggestedEvent(); cmd != nil {
				return a, cmd
			}
		}
		if (a.viewID == ViewInbox || a.viewID == ViewSmartFolder) && a.quickMenu != nil && a.quickMenu.side != quickMenuNone {
			cmd := a.handleQuickMenuEnter()
			return a, cmd
		}
		return a, a.handleEnter()

	case "left":
		if a.viewID == ViewReader && a.readerView.AttachFocusActive() {
			a.readerView.FocusNextAttachmentAction(-1)
			return a, nil
		}
		if a.viewID == ViewReader && a.readerView.HasFocusableEvent() {
			a.readerView.FocusNextEventAction(-1)
			return a, nil
		}
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			if a.quickMenu != nil && a.quickMenu.side == quickMenuLeft {
				// Same direction again — close
				a.closeQuickMenu()
				return a, nil
			}
			if a.quickMenu != nil && a.quickMenu.side == quickMenuRight {
				// Already on right, pressing left advances step
				cmd := a.openQuickMenu(quickMenuRight)
				return a, cmd
			}
			// Fresh open — left arrow reveals right panel (natural: swipe left → right panel)
			cmd := a.openQuickMenu(quickMenuRight)
			return a, cmd
		}
		fallthrough
	case "esc", "h":
		// In the attachment section, leave the section first rather than the reader.
		if a.viewID == ViewReader && a.readerView.AttachFocusActive() {
			a.readerView.ExitAttachments()
			return a, nil
		}
		if a.viewID == ViewReader {
			a.viewID = a.prevViewID
		} else if a.viewID == ViewFolder {
			a.viewID = ViewInbox
		}
		a.closeQuickMenu()

	case "right":
		if a.viewID == ViewReader && a.readerView.AttachFocusActive() {
			a.readerView.FocusNextAttachmentAction(1)
			return a, nil
		}
		if a.viewID == ViewReader && a.readerView.HasFocusableEvent() {
			a.readerView.FocusNextEventAction(1)
			return a, nil
		}
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			if a.quickMenu != nil && a.quickMenu.side == quickMenuRight {
				// Same direction again — close
				a.closeQuickMenu()
				return a, nil
			}
			if a.quickMenu != nil && a.quickMenu.side == quickMenuLeft {
				// Already on left, pressing right advances step
				cmd := a.openQuickMenu(quickMenuLeft)
				return a, cmd
			}
			// Fresh open — right arrow reveals left panel
			cmd := a.openQuickMenu(quickMenuLeft)
			return a, cmd
		}

	case "c":
		return a, a.openCompose()

	case "r":
		if msg := a.currentMessage(); msg != nil {
			return a, a.openReply(msg, false)
		}

	case "R":
		if msg := a.currentMessage(); msg != nil {
			return a, a.openReply(msg, true)
		}

	case "f":
		if msg := a.currentMessage(); msg != nil {
			return a, a.openForward(msg)
		}

	case "s":
		return a, a.toggleStar()

	case "m":
		return a, a.toggleRead()

	case "d":
		return a, a.deleteMessage()

	case "v":
		a.openFolderPicker()

	case "e":
		return a, a.archiveMessage()

	case KeyUnreadFilter:
		a.unreadOnly = !a.unreadOnly
		threads := thread.BuildThreads(a.visibleMessages())
		a.inboxView.SetThreads(threads)
		a.inboxView.SetFiltered(a.unreadOnly)
		a.header.SetUnreadFilter(a.unreadOnly, len(threads))
	}

	return a, nil
}
