package ui

import (
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/thread"
	tea "github.com/charmbracelet/bubbletea"
)

// --- navigation helpers ---

func (a *App) moveUp() {
	switch a.viewID {
	case ViewInbox, ViewSmartFolder:
		a.inboxView.MoveUp()
	case ViewReader:
		a.readerView.MoveUp()
	case ViewFolder:
		a.folderView.MoveUp()
	}
}

func (a *App) moveDown() {
	switch a.viewID {
	case ViewInbox, ViewSmartFolder:
		a.inboxView.MoveDown()
	case ViewReader:
		a.readerView.MoveDown()
	case ViewFolder:
		a.folderView.MoveDown()
	}
}

func (a *App) pageUp() {
	switch a.viewID {
	case ViewInbox, ViewSmartFolder:
		a.inboxView.PageUp()
	case ViewReader:
		a.readerView.PageUp()
	case ViewFolder:
		a.folderView.PageUp()
	}
}

func (a *App) pageDown() {
	switch a.viewID {
	case ViewInbox, ViewSmartFolder:
		a.inboxView.PageDown()
	case ViewReader:
		a.readerView.PageDown()
	case ViewFolder:
		a.folderView.PageDown()
	}
}

func (a *App) goToTop() {
	switch a.viewID {
	case ViewInbox, ViewSmartFolder:
		a.inboxView.GoToTop()
	case ViewReader:
		a.readerView.GoToTop()
	case ViewFolder:
		a.folderView.GoToTop()
	}
}

func (a *App) goToBottom() {
	switch a.viewID {
	case ViewInbox, ViewSmartFolder:
		a.inboxView.GoToBottom()
	case ViewReader:
		a.readerView.GoToBottom()
	case ViewFolder:
		a.folderView.GoToBottom()
	}
}

func (a *App) handleEnter() tea.Cmd {
	switch a.viewID {
	case ViewInbox, ViewSmartFolder:
		t := a.inboxView.SelectedThread()
		if t != nil {
			return a.openThread(t)
		}
	case ViewFolder:
		f := a.folderView.SelectedFolder()
		if f != nil {
			a.activeFolder = f.Name
			a.sidebar.SetActive(a.activeAccount, f.Name)
			a.header.SetFolder(f.Name)
			a.viewID = ViewInbox
			return a.fetchMessages()
		}
	}
	return nil
}

// --- message/folder actions ---

func (a *App) openMessage(msg *data.Message) tea.Cmd {
	t := &data.Thread{
		ID:       msg.ThreadID,
		Subject:  msg.Subject,
		Messages: []*data.Message{msg},
		LastDate: msg.Date,
	}
	return a.openThread(t)
}

// openThread opens a full thread in the reader, fetching bodies for any
// messages that haven't been loaded yet.
func (a *App) openThread(t *data.Thread) tea.Cmd {
	a.prevViewID = a.viewID
	a.readerView.SetThread(t)
	a.viewID = ViewReader

	var cmds []tea.Cmd

	// Mark all messages in this thread as read
	for _, msg := range t.Messages {
		if !msg.IsRead() {
			client, ok := a.imapClients[a.activeAccount]
			if ok {
				cmds = append(cmds, client.SetFlag(a.activeFolder, msg.UID, data.FlagSeen, true))
			}
			_ = a.store.SetFlags(a.activeAccount, a.activeFolder, msg.UID,
				append(msg.Flags, data.FlagSeen))
			msg.Flags = append(msg.Flags, data.FlagSeen)
		}
	}
	t.HasUnread = false

	// Fetch body for every message in the thread that doesn't have one yet.
	// Use each message's own account and folder — threads can span folders (e.g. INBOX + Sent).
	for _, msg := range t.Messages {
		if msg.Body == "" && msg.HTMLBody == "" {
			acctName := msg.AccountName
			if acctName == "" {
				acctName = a.activeAccount
			}
			client, ok := a.imapClients[acctName]
			if !ok {
				continue
			}
			folder := msg.FolderName
			if folder == "" {
				folder = a.activeFolder
			}
			cmds = append(cmds, client.FetchBody(folder, msg.UID, msg.ID))
		}
	}

	// Trigger suggested event extraction for every message in the thread
	for _, msg := range t.Messages {
		a.debugLog("loadSuggestedEvent for thread %s: msg.ID=%d", t.ID, msg.ID)
		if cmd := a.loadSuggestedEvent(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

func (a *App) loadSuggestedEvent(msg *data.Message) tea.Cmd {
	if msg == nil {
		return nil
	}
	a.debugLog("loadSuggestedEvent: msg.ID=%d", msg.ID)
	if cached, err := a.store.GetSuggestedEvent(msg.ID); err == nil && cached != nil {
		a.debugLog("loadSuggestedEvent: found cached GenerationOK=%v HasEvent=%v Rejected=%v", cached.GenerationOK, cached.HasEvent, cached.Rejected)
		// If the user previously rejected this suggestion, don't show it again.
		if cached.Rejected {
			return nil
		}
		if a.readerView != nil {
			a.readerView.SetSuggestedEvent(cached)
		}
		if cached.GenerationOK || cached.ParseError != "" {
			return nil
		}
	}
	if a.suggestQueue == nil {
		// No extraction client configured — show a clear unavailable state
		// instead of leaving the view stuck on "Loading suggestion...".
		if a.readerView != nil {
			a.readerView.SetSuggestedEvent(&data.SuggestedEvent{
				MessageID:    msg.ID,
				GenerationOK: false,
			})
		}
		return nil
	}
	if a.readerView != nil {
		a.readerView.SetSuggestedEvent(nil)
	}
	a.suggestQueue.Submit(msg)
	return nil
}

// visibleMessages returns loadedMessages filtered to unread-only when the
// unread filter is active, otherwise returns the full slice.
func (a *App) visibleMessages() []*data.Message {
	if !a.unreadOnly {
		return a.loadedMessages
	}
	out := make([]*data.Message, 0, len(a.loadedMessages))
	for _, m := range a.loadedMessages {
		if !m.HasFlag(data.FlagSeen) {
			out = append(out, m)
		}
	}
	return out
}

func (a *App) fetchMessages() tea.Cmd {
	a.loadedMessages = nil
	a.fetchedCount = 0
	a.loadingMore = false
	a.allLoaded = false
	a.unreadOnly = false
	a.header.SetUnreadFilter(false, 0)
	a.inboxView.SetFiltered(false)

	client, ok := a.imapClients[a.activeAccount]
	if !ok {
		// Try loading from cache
		threads, err := a.store.GetThreads(a.activeAccount, a.activeFolder)
		if err == nil {
			for _, t := range threads {
				a.loadedMessages = append(a.loadedMessages, t.Messages...)
			}
			built := thread.BuildThreads(a.visibleMessages())
			a.inboxView.SetThreads(built)
		}
		return nil
	}
	a.statusbar.SetLoading(true)
	a.inboxView.SetLoading() // shown only while the list is empty
	return tea.Batch(
		client.FetchMessages(a.activeFolder, a.cfg.General.PageSize),
		spinnerTick(),
	)
}

func (a *App) prefetchBodies(limit int) []tea.Cmd {
	if limit <= 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, acct := range a.accounts {
		client, ok := a.imapClients[acct.Name]
		if !ok {
			continue
		}
		msgs, err := a.store.ListBodyPrefetchCandidates(acct.Name, limit)
		if err != nil {
			continue
		}
		for _, msg := range msgs {
			if msg == nil {
				continue
			}
			if msg.FolderName == "" {
				continue
			}
			if a.prefetchSkip[msg.ID] {
				continue
			}
			cmds = append(cmds, client.FetchBody(msg.FolderName, msg.UID, msg.ID))
		}
	}
	return cmds
}

func (a *App) currentMessage() *data.Message {
	switch a.viewID {
	case ViewInbox, ViewSmartFolder:
		t := a.inboxView.SelectedThread()
		if t != nil {
			return t.Latest()
		}
	case ViewReader:
		if t := a.readerView.CurrentThread(); t != nil {
			return t.Latest()
		}
		return a.readerView.CurrentMessage()
	}
	return nil
}
