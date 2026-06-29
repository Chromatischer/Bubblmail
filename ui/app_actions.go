package ui

import (
	"fmt"
	"strings"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/icons"
	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) handleQuickMenuEnter() tea.Cmd {
	if a.quickMenu == nil || a.quickMenu.side == quickMenuNone {
		return nil
	}
	side := a.quickMenu.side
	step := a.quickMenu.step
	a.closeQuickMenu()
	if side == quickMenuLeft {
		switch step {
		case 0:
			return a.toggleRead()
		case 1:
			return a.quickMoveMessage()
		}
	}
	if side == quickMenuRight {
		switch step {
		case 0:
			return a.toggleStar()
		case 1:
			return a.deleteMessage()
		}
	}
	return nil
}

func (a *App) deleteMessage() tea.Cmd {
	if a.viewID != ViewInbox && a.viewID != ViewSmartFolder {
		// Outside inbox — single-message mode (e.g. reader).
		msg := a.currentMessage()
		if msg == nil {
			return nil
		}
		client, ok := a.imapClients[a.activeAccount]
		if !ok {
			return nil
		}
		trash := a.findTrashFolder(a.activeAccount)
		if trash == "" {
			a.flash("No Trash folder found", "err")
			return nil
		}
		a.flash(fmt.Sprintf("%s Moving to Trash…", icons.Trash), "info")
		return client.MoveToTrash(a.activeFolder, msg.UID, trash)
	}
	threads := a.inboxView.SelectedThreads()
	if len(threads) == 0 {
		return nil
	}
	client, ok := a.imapClients[a.activeAccount]
	if !ok {
		return nil
	}
	trash := a.findTrashFolder(a.activeAccount)
	if trash == "" {
		a.flash("No Trash folder found", "err")
		return nil
	}
	var cmds []tea.Cmd
	for _, t := range threads {
		msg := t.Latest()
		if msg == nil {
			continue
		}
		cmds = append(cmds, client.MoveToTrash(msg.FolderName, msg.UID, trash))
	}
	if len(cmds) == 1 {
		a.flash(fmt.Sprintf("%s Moving to Trash…", icons.Trash), "info")
	} else {
		a.flash(fmt.Sprintf("%s Moving %d to Trash…", icons.Trash, len(cmds)), "info")
	}
	a.inboxView.ClearSelection()
	return tea.Batch(cmds...)
}

// undoMove records the last reversible move so Ctrl+Z can put the message back.
type undoMove struct {
	account string
	src     string // folder the message should return to
	dest    string // folder it currently lives in
	destUID uint32 // its UID in dest
	label   string // human label of where it went, for the flash
}

// undoResultMsg is returned when an undo move completes.
type undoResultMsg struct {
	account string
	folder  string // folder the message was restored to
	err     error
}

// registerUndo stores a reversible move and returns the flash suffix advertising
// it. Returns "" (and clears any pending undo) when the server gave no
// destination UID, since we cannot then move the message back.
func (a *App) registerUndo(account, src, dest string, destUID uint32, label string) string {
	if destUID == 0 || dest == "" {
		a.lastUndo = nil
		return ""
	}
	a.lastUndo = &undoMove{account: account, src: src, dest: dest, destUID: destUID, label: label}
	return " · ctrl+z to undo"
}

// performUndo reverses the last move, restoring the message to its origin.
func (a *App) performUndo() tea.Cmd {
	u := a.lastUndo
	if u == nil {
		return a.flash("Nothing to undo", "info")
	}
	a.lastUndo = nil
	client, ok := a.imapClients[u.account]
	if !ok {
		return a.flash(fmt.Sprintf("%s Cannot undo: not connected", icons.Error), "err")
	}
	return tea.Batch(
		a.flash(fmt.Sprintf("%s Undoing…", icons.Refresh), "info"),
		func() tea.Msg {
			_, err := client.MoveMessageSync(u.dest, u.destUID, u.src)
			return undoResultMsg{account: u.account, folder: u.src, err: err}
		},
	)
}

func (a *App) moveMessageToFolder(destFolder string) tea.Cmd {
	if a.viewID != ViewInbox && a.viewID != ViewSmartFolder {
		// Outside inbox — single-message mode (e.g. reader).
		msg := a.currentMessage()
		if msg == nil {
			return nil
		}
		client, ok := a.imapClients[a.activeAccount]
		if !ok {
			return nil
		}
		a.flash(fmt.Sprintf("%s Moving…", icons.FolderOpen), "info")
		return client.MoveMessage(a.activeFolder, msg.UID, destFolder)
	}
	threads := a.inboxView.SelectedThreads()
	if len(threads) == 0 {
		return nil
	}
	client, ok := a.imapClients[a.activeAccount]
	if !ok {
		return nil
	}
	var cmds []tea.Cmd
	for _, t := range threads {
		msg := t.Latest()
		if msg == nil {
			continue
		}
		cmds = append(cmds, client.MoveMessage(msg.FolderName, msg.UID, destFolder))
	}
	if len(cmds) == 0 {
		return nil
	}
	a.flash(fmt.Sprintf("%s Moving…", icons.FolderOpen), "info")
	a.inboxView.ClearSelection()
	return tea.Batch(cmds...)
}

// findTrashFolder returns the IMAP name of the Trash folder for the given account.
// It looks for a folder with the \Trash attribute, then falls back to common names.
func (a *App) findTrashFolder(account string) string {
	for _, acct := range a.accounts {
		if acct.Name != account {
			continue
		}
		for _, f := range acct.Folders {
			if hasFolderAttribute(f, `\Trash`) {
				return f.Name
			}
		}
		for _, f := range acct.Folders {
			switch f.DisplayName {
			case "Trash", "Deleted", "Deleted Items", "Deleted Messages", "Bin":
				return f.Name
			}
		}
	}
	return ""
}

// findArchiveFolder returns the IMAP name of the Archive folder for the account.
// It prefers the \Archive special-use folder and falls back to common names.
func (a *App) findArchiveFolder(account string) string {
	for _, acct := range a.accounts {
		if acct.Name != account {
			continue
		}
		for _, f := range acct.Folders {
			if hasFolderAttribute(f, `\Archive`) {
				return f.Name
			}
		}
		for _, f := range acct.Folders {
			switch strings.ToLower(strings.TrimSpace(f.DisplayName)) {
			case "archive", "archives", "archived":
				return f.Name
			}
		}
		for _, f := range acct.Folders {
			if hasFolderAttribute(f, `\All`) {
				return f.Name
			}
			switch strings.ToLower(strings.TrimSpace(f.DisplayName)) {
			case "all mail", "all messages":
				return f.Name
			}
		}
	}
	return ""
}

// findSentFolder returns the IMAP name of the Sent folder for the given account.
// It checks for the \Sent attribute first, then falls back to common names.
func (a *App) findSentFolder(account string) string {
	for _, acct := range a.accounts {
		if acct.Name != account {
			continue
		}
		for _, f := range acct.Folders {
			if hasFolderAttribute(f, `\Sent`) {
				return f.Name
			}
		}
		for _, f := range acct.Folders {
			switch f.DisplayName {
			case "Sent", "Sent Items", "Sent Mail", "Sent Messages":
				return f.Name
			}
		}
	}
	return ""
}

// findDraftsFolder returns the IMAP name of the Drafts folder for the given account.
// It checks for the \Drafts attribute first, then falls back to common names.
func (a *App) findDraftsFolder(account string) string {
	for _, acct := range a.accounts {
		if acct.Name != account {
			continue
		}
		for _, f := range acct.Folders {
			if hasFolderAttribute(f, `\Drafts`) || hasFolderAttribute(f, `\Draft`) {
				return f.Name
			}
		}
		for _, f := range acct.Folders {
			switch f.DisplayName {
			case "Drafts", "Draft":
				return f.Name
			}
		}
	}
	return ""
}

func hasFolderAttribute(folder *data.Folder, attr string) bool {
	if folder == nil {
		return false
	}
	for _, candidate := range folder.Attributes {
		if candidate == attr {
			return true
		}
	}
	return false
}

func (a *App) archiveMessage() tea.Cmd {
	archive := a.findArchiveFolder(a.activeAccount)
	if archive == "" {
		return a.flash("No Archive folder found", "err")
	}
	if archive == a.activeFolder {
		return a.flash("Already in Archive", "info")
	}
	return tea.Batch(
		a.flash(fmt.Sprintf("%s Archiving…", icons.Archive), "info"),
		a.moveMessageToFolder(archive),
	)
}
