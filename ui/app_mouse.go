package ui

import (
	"github.com/bubblmail/bubblmail/util"
	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Mouse wheel scrolling for content views — several steps per notch so the
	// wheel moves faster than single-step keyboard navigation. In the inbox the
	// wheel scrolls without a selection highlight (pointer-driven, no keyboard
	// focus); other views just step their cursor/scroll.
	if msg.Button == tea.MouseButtonWheelDown {
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.inboxView.WheelScroll(wheelScrollStep)
		} else {
			for i := 0; i < wheelScrollStep; i++ {
				a.moveDown()
			}
		}
		return a, a.maybeLoadMore()
	}
	if msg.Button == tea.MouseButtonWheelUp {
		if a.viewID == ViewInbox || a.viewID == ViewSmartFolder {
			a.inboxView.WheelScroll(-wheelScrollStep)
		} else {
			for i := 0; i < wheelScrollStep; i++ {
				a.moveUp()
			}
		}
		return a, nil
	}

	if msg.Button != tea.MouseButtonLeft {
		return a, nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
		// Start drag tracking so motion events can build a selection.
		a.drag = &dragState{startX: msg.X, startY: msg.Y, endX: msg.X, endY: msg.Y}
		// Also fire the immediate click action.
		return a.handleLeftPress(msg.X, msg.Y)

	case tea.MouseActionMotion:
		if a.drag != nil {
			a.drag.endX = msg.X
			a.drag.endY = msg.Y
			dx := msg.X - a.drag.startX
			if dx < 0 {
				dx = -dx
			}
			dy := msg.Y - a.drag.startY
			if dy < 0 {
				dy = -dy
			}
			if dx >= 1 || dy >= 1 {
				a.drag.isDrag = true
			}
		}

	case tea.MouseActionRelease:
		if a.drag != nil && a.drag.isDrag && a.drag.canCopy {
			asMarkdown := !msg.Ctrl
			text := a.extractSelectedText(a.drag.startX, a.drag.startY, msg.X, msg.Y, asMarkdown)
			a.drag = nil
			if text != "" {
				if err := util.CopyToClipboard(text); err != nil {
					return a, a.flash("Copy failed: "+err.Error(), "err")
				}
				label := "Copied as Markdown"
				if !asMarkdown {
					label = "Copied as plain text"
				}
				return a, a.flash(label, "ok")
			}
			return a, nil
		}
		a.drag = nil
	}

	return a, nil
}

// handleLeftPress fires the immediate-action for a left-button press at (x, y).
func (a *App) handleLeftPress(x, y int) (tea.Model, tea.Cmd) {
	headerH := a.headerHeight()
	statusH := a.statusHeight()
	statusY := a.height - statusH

	// Record the zone where this drag started so the selection cannot bleed
	// into adjacent UI elements (e.g. drag in content ≠ selects sidebar).
	if a.drag != nil {
		sidebarW := 0
		if a.showSidebar {
			sidebarW = sidebarRenderedWidth()
		}
		switch {
		case a.showSidebar && x < sidebarW:
			// Sidebar zone
			a.drag.zoneX0, a.drag.zoneX1 = 0, sidebarW-1
			a.drag.zoneY0, a.drag.zoneY1 = headerH, statusY-1
		case y < headerH:
			// Header zone
			a.drag.zoneX0, a.drag.zoneX1 = 0, a.width-1
			a.drag.zoneY0, a.drag.zoneY1 = 0, headerH-1
		case y >= statusY:
			// Status bar zone
			a.drag.zoneX0, a.drag.zoneX1 = 0, a.width-1
			a.drag.zoneY0, a.drag.zoneY1 = statusY, a.height-1
		default:
			if a.comp.IsActive() {
				// Restrict copy zone to the body text area only: excludes
				// box borders, padding, field labels, title, dividers, and hints.
				x0, y0, x1, y1 := a.comp.BodyDragZone(headerH)
				a.drag.zoneX0, a.drag.zoneX1 = x0, x1
				a.drag.zoneY0, a.drag.zoneY1 = y0, y1
				a.drag.canCopy = true
			} else {
				// Content zone — copyable in the reader.
				a.drag.zoneX0, a.drag.zoneX1 = sidebarW, a.width-1
				a.drag.zoneY0, a.drag.zoneY1 = headerH, statusY-1
				a.drag.canCopy = a.viewID == ViewReader
			}
		}
	}

	// Sidebar click — skip when any overlay is active (sidebar is not rendered then).
	overlayActive := a.comp.IsActive() || a.searchOverlay.IsActive() || a.folderPicker.IsActive() || a.newFolder.IsActive() || a.commandPalette.IsActive()
	if !overlayActive && a.showSidebar && x < sidebarRenderedWidth() {
		contentY := y - headerH
		acct, folder, ok := a.sidebar.HitTest(x, contentY)
		if ok && folder != "" {
			if acct == "" {
				a.smartFolder = folder
				a.viewID = ViewSmartFolder
				a.sidebar.SetActive("", folder)
				a.header.SetFolder(folder)
				return a, a.fetchSmartFolder(folder)
			}
			a.activeAccount = acct
			a.activeFolder = folder
			a.sidebar.SetActive(acct, folder)
			a.header.SetAccount(acct)
			a.header.SetFolder(folder)
			a.viewID = ViewInbox
			return a, a.fetchMessages()
		}
		return a, nil
	}

	// Status bar hint click
	if y >= statusY {
		if key := a.statusbar.HitTest(x, y-statusY); key != "" {
			return a.handleKey(syntheticKeyMsg(key))
		}
		return a, nil
	}

	// Content area click
	if y >= headerH && y < statusY {
		contentY := y - headerH

		// Overlay hit testing — overlays cover the full content area and consume
		// all clicks so the underlying views are not accidentally activated.
		if a.comp.IsActive() {
			if a.comp.IsPrompting() {
				if key := a.comp.HitTestDraftPrompt(x, contentY); key != "" {
					a.comp.HandleKey(syntheticKeyMsg(key))
				}
			} else if field := a.comp.HitTestField(contentY); field >= 0 {
				a.comp.SetFocus(field)
			} else if key := a.comp.HitTestFooter(x, contentY); key != "" {
				a.comp.HandleKey(syntheticKeyMsg(key))
				if r := a.comp.Result(); r != nil {
					cmd := a.handleComposerResult(r)
					a.comp.ClearResult()
					return a, cmd
				}
			}
			return a, nil
		}

		if a.searchOverlay.IsActive() {
			idx := a.searchOverlay.HitTestResult(contentY)
			if idx >= 0 {
				if idx == a.searchOverlay.Cursor() {
					// Second click on the focused result: open it.
					return a.handleKey(syntheticKeyMsg("enter"))
				}
				a.searchOverlay.SetCursor(idx)
			}
			return a, nil
		}

		if a.folderPicker.IsActive() {
			idx := a.folderPicker.HitTestFolder(contentY)
			if idx >= 0 {
				a.folderPicker.SetCursor(idx)
				closed := a.folderPicker.HandleKey("enter")
				if closed {
					folder := a.folderPicker.Result()
					a.folderPicker.Close()
					if folder != nil {
						return a, a.moveMessageToFolder(folder.Name)
					}
				}
			}
			return a, nil
		}

		if a.newFolder.IsActive() {
			// New-folder overlay has no clickable rows; consume the click.
			return a, nil
		}

		sidebarOff := 0
		if a.showSidebar {
			sidebarOff = sidebarRenderedWidth()
		}
		cx := x - sidebarOff

		switch a.viewID {
		case ViewInbox, ViewSmartFolder:
			idx := a.inboxView.HitTestThread(contentY)
			if idx >= 0 {
				if idx == a.inboxView.CursorPos() {
					return a, a.handleEnter()
				}
				a.closeQuickMenu()
				a.inboxView.SetCursor(idx)
				return a, nil
			}

		case ViewFolder:
			idx := a.folderView.HitTestFolder(contentY)
			if idx >= 0 {
				if idx == a.folderView.CursorPos() {
					return a, a.handleEnter()
				}
				a.folderView.SetCursor(idx)
			}

		case ViewReader:
			action := a.readerView.HitTestContent(contentY, cx)
			switch action {
			case "attach:open":
				a.readerView.SetAttachActionFocus(0)
				return a, a.executeAttachmentAction()
			case "attach:download":
				a.readerView.SetAttachActionFocus(1)
				return a, a.executeAttachmentAction()
			case "attach:editor":
				a.readerView.SetAttachActionFocus(2)
				return a, a.executeAttachmentAction()
			case "event:plain":
				a.readerView.SetEventFocus(0)
				return a, a.copySuggestedEvent()
			case "event:json":
				a.readerView.SetEventFocus(1)
				return a, a.copySuggestedEvent()
			case "event:reject":
				a.readerView.SetEventFocus(2)
				return a, a.copySuggestedEvent()
			}
		}
	}
	return a, nil
}

// syntheticKeyMsg constructs a tea.KeyMsg for the given key string so that
// status-bar hint clicks can be routed through handleKey unchanged.
func syntheticKeyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "ctrl+f":
		return tea.KeyMsg{Type: tea.KeyCtrlF}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+enter":
		return tea.KeyMsg{Type: tea.KeyCtrlJ}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	}
	if len([]rune(key)) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
