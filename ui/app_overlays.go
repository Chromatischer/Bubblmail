package ui

import (
	"fmt"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/icons"
	tea "github.com/charmbracelet/bubbletea"
)

// persistUI writes the current UI state (collapsed folders + flags) to disk,
// preserving all fields rather than overwriting the file with a partial struct.
func (a *App) persistUI() {
	if a.persisted == nil {
		a.persisted = &uiState{}
	}
	a.persisted.CollapsedFolders = a.sidebar.CollapsedKeys()
	_ = saveUIState(a.persisted)
}

// firstRunTipMsg triggers the one-time onboarding hint.
type firstRunTipMsg struct{}

// focusSidebar moves keyboard focus into the sidebar (bound to both Tab and \).
func (a *App) focusSidebar() {
	if !a.showSidebar {
		return
	}
	a.sidebarFocused = true
	a.sidebar.SetFocused(true)
	if a.viewID == ViewSmartFolder {
		a.sidebar.FocusAt("", a.smartFolder)
	} else {
		a.sidebar.FocusAt(a.activeAccount, a.activeFolder)
	}
}

func (a *App) openCommandPalette() {
	a.commandPalette.SetSize(a.width, a.height-a.headerHeight()-a.statusHeight())
	a.commandPalette.Open()
}

func (a *App) openFolderPicker() {
	var folders []*data.Folder
	for _, acct := range a.accounts {
		if acct.Name == a.activeAccount {
			folders = acct.Folders
			break
		}
	}
	if len(folders) == 0 {
		a.flash(fmt.Sprintf("%s No folders available", icons.FolderEmpty), "err")
		return
	}
	a.folderPicker.SetSize(a.width, a.height-a.headerHeight()-a.statusHeight())
	a.folderPicker.Open(folders, a.activeFolder, a.sidebar.CollapsedFolders(a.activeAccount))
}

func (a *App) openNewFolderDialog() {
	contentH := a.height - a.headerHeight() - a.statusHeight()
	if contentH < 1 {
		contentH = 1
	}
	a.newFolder.SetSize(a.width, contentH)
	a.newFolder.Open()
}

func (a *App) createFolder(name string) tea.Cmd {
	client, ok := a.imapClients[a.activeAccount]
	if !ok {
		return a.flash(fmt.Sprintf("%s Not connected", icons.Error), "err")
	}
	a.flash(fmt.Sprintf("%s Creating folder '%s'…", icons.FolderNew, name), "info")
	return client.CreateFolder(name)
}

func (a *App) findMessageByID(id int64) *data.Message {
	for _, msg := range a.loadedMessages {
		if msg.ID == id {
			return msg
		}
	}
	return nil
}

func (a *App) embedMessage(msg *data.Message, body string) tea.Cmd {
	return func() tea.Msg {
		if a.embQueue == nil {
			return nil
		}
		a.embQueue.Enqueue(msg, body)
		return embeddingStartedMsg{stats: a.embQueue.Stats()}
	}
}
