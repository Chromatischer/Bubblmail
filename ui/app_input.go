package ui

import (
	"fmt"
	"time"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/icons"
	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// 1. Composer captures all keys when active
	if a.comp.IsActive() {
		a.comp.HandleKey(msg)
		if r := a.comp.Result(); r != nil {
			cmd := a.handleComposerResult(r)
			a.comp.ClearResult()
			return a, cmd
		}
		return a, nil
	}

	if key != "q" {
		a.quitPending = false
	}

	if key == "q" && !a.canQuitNow() {
		if !a.searchOverlay.IsActive() {
			a.quitPending = false
			key = "esc"
		}
	}

	const minW, minH = 80, 24
	if a.width < minW || a.height < minH {
		if key == "ctrl+c" {
			return a, tea.Quit
		}
		if key == "q" && a.canQuitNow() {
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
		}
		return a, nil
	}

	// 2. Search overlay
	if a.searchOverlay.IsActive() {
		// ctrl+f triggers an IMAP server search using the current query
		if key == "ctrl+f" {
			if client, ok := a.imapClients[a.activeAccount]; ok {
				q := a.searchOverlay.CompiledQuery()
				a.searchOverlay.SetSearchResults(nil, true)
				a.flash(fmt.Sprintf("%s Searching server…", icons.Search), "info")
				cmds := []tea.Cmd{client.SearchIMAP(a.activeFolder, q)}
				if !a.statusbar.loading {
					cmds = append(cmds, spinnerTick())
				}
				return a, tea.Batch(cmds...)
			}
			return a, nil
		}
		qChanged, closed, selected := a.searchOverlay.HandleKey(key)
		if closed {
			a.searchState = nil
			var selectedMsg *data.Message
			if selected {
				selectedMsg = a.searchOverlay.SelectedMessage()
			}
			a.searchOverlay.Close()
			if selectedMsg != nil {
				return a, a.openMessage(selectedMsg)
			}
		} else if qChanged {
			id := a.searchOverlay.BumpDebounce()
			if !a.searchOverlay.CanSearch() {
				a.searchOverlay.SetSearchResults(nil, false)
			}
			return a, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
				return searchDebounceMsg{id: id}
			})
		}
		return a, nil
	}

	// 3. Folder picker
	if a.folderPicker.IsActive() {
		closed := a.folderPicker.HandleKey(key)
		if closed {
			folder := a.folderPicker.Result()
			a.folderPicker.Close()
			if folder != nil {
				return a, a.moveMessageToFolder(folder.Name)
			}
		}
		return a, nil
	}

	// 4. New folder dialog
	if a.newFolder.IsActive() {
		closed := a.newFolder.HandleKey(key)
		if closed {
			submitted := a.newFolder.WasSubmitted()
			name := a.newFolder.Result()
			a.newFolder.Close()
			a.newFolder.ClearResult()
			if submitted && name != "" {
				return a, a.createFolder(name)
			}
		}
		return a, nil
	}

	// 4b. Command palette
	if a.commandPalette.IsActive() {
		closed := a.commandPalette.HandleKey(key)
		if closed {
			result := a.commandPalette.Result()
			a.commandPalette.Close()
			if result != nil {
				return a.dispatchGlobalKey(result.Key)
			}
		}
		return a, nil
	}

	// 5. Help overlay — any key closes it
	if a.showHelp {
		a.showHelp = false
		return a, nil
	}

	// 6. Sidebar focus mode — routes navigation to sidebar
	if a.sidebarFocused {
		switch key {
		case "j", "down":
			a.sidebar.MoveDown()
		case "k", "up":
			a.sidebar.MoveUp()
		case "g":
			a.sidebar.GoToTop()
		case "G":
			a.sidebar.GoToBottom()
		case "n":
			a.openNewFolderDialog()
		case "enter":
			if acct, folder, ok := a.sidebar.Selected(); ok {
				a.sidebarFocused = false
				a.sidebar.SetFocused(false)
				// Check if the selected item is a smart folder
				if acct == "" {
					// Smart folder selected (no account)
					a.smartFolder = folder
					a.viewID = ViewSmartFolder
					a.header.SetFolder(folder)
					a.sidebar.SetActive("", folder)
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
		case "z":
			if acct, folder, ok := a.sidebar.Selected(); ok {
				a.sidebar.Toggle(acct, folder)
				a.persistUI()
			}
		case "esc", "h", "left", "tab", "q", KeySidebarFocus:
			a.sidebarFocused = false
			a.sidebar.SetFocused(false)
		case "ctrl+c":
			return a, tea.Quit
		}
		return a, nil
	}

	// 7. Global keys
	return a.dispatchGlobalKey(key)
}
