package ui

import (
	"fmt"
	"time"

	"github.com/bubblmail/bubblmail/embeddings"
	imaplib "github.com/bubblmail/bubblmail/imap"
	outsmtp "github.com/bubblmail/bubblmail/smtp"
	"github.com/bubblmail/bubblmail/thread"
	"github.com/bubblmail/bubblmail/ui/icons"
	tea "github.com/charmbracelet/bubbletea"
)

// flashTimeout is how long a status flash stays visible before it auto-clears.
const flashTimeout = 3 * time.Second

// Update implements tea.Model. It wraps the real handler so that any flash
// raised during the update — including by background result handlers that
// discard flash()'s return value — is given an auto-clear timer exactly once.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := a.statusbar.MessageSeq()
	model, cmd := a.update(msg)
	if a.statusbar.MessageSeq() != before && a.statusbar.HasMessage() {
		seq := a.statusbar.MessageSeq()
		clear := tea.Tick(flashTimeout, func(time.Time) tea.Msg {
			return clearStatusMsg{seq: seq}
		})
		if cmd == nil {
			cmd = clear
		} else {
			cmd = tea.Batch(cmd, clear)
		}
	}
	return model, cmd
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.updateLayout()
		return a, nil

	case connectResultMsg:
		a.imapClients[msg.account] = msg.client
		// Start fetching folders
		return a, tea.Batch(
			msg.client.FetchFolders(),
			msg.client.FetchMessages(a.activeFolder, a.cfg.General.PageSize),
			a.startEmbeddingBackfill(msg.account),
			a.startClassifyBackfill(msg.account),
		)

	case imaplib.FolderListMsg:
		if msg.Err != nil {
			a.flash("IMAP error: "+msg.Err.Error(), "err")
			return a, nil
		}
		// Update cache and sidebar
		if err := a.store.UpsertFolders(msg.Account, msg.Folders); err != nil {
			a.flash("Cache error: "+err.Error(), "err")
		}
		a.sidebar.SetFolders(msg.Account, msg.Folders)
		// Update account struct
		for _, acct := range a.accounts {
			if acct.Name == msg.Account {
				acct.Folders = msg.Folders
			}
		}
		_ = a.store.BuildFolderEmbeddings(msg.Account, a.cfg.Embeddings.Model, a.cfg.Embeddings.FolderSampleLimit)
		a.header.SetSyncState("synced")
		return a, nil

	case embeddingBackfillMsg:
		if msg.err != nil {
			a.flash("Embeddings backfill error: "+msg.err.Error(), "err")
			return a, nil
		}
		if msg.queued > 0 && a.embQueue != nil {
			a.statusbar.SetEmbeddingStats(a.embQueue.Stats())
			return a, embeddingTick()
		}
		return a, nil

	case imaplib.MessageListMsg:
		if msg.Err != nil {
			a.flash("Fetch error: "+msg.Err.Error(), "err")
			a.statusbar.SetLoading(false)
			a.inboxView.SetError(msg.Err.Error())
			return a, nil
		}
		if err := a.store.UpsertMessages(msg.Messages); err != nil {
			a.flash("Cache write error: "+err.Error(), "err")
		}
		a.setInboxMessages(msg.Messages)
		a.statusbar.SetLoading(false)
		// Submit new inbox messages for classification
		if a.classifyQueue != nil {
			a.classifyQueue.Submit(msg.Messages)
		}
		return a, a.scheduleSyncTick(msg.Account)

	case imaplib.MoreMessageListMsg:
		a.loadingMore = false
		a.statusbar.SetLoading(false)
		if msg.Err != nil {
			a.flash("Load more error: "+msg.Err.Error(), "err")
			return a, nil
		}
		if len(msg.Messages) == 0 {
			a.allLoaded = true
			return a, nil
		}
		if err := a.store.UpsertMessages(msg.Messages); err != nil {
			a.flash("Cache write error: "+err.Error(), "err")
		}
		a.appendInboxMessages(msg.Messages)
		// Submit newly loaded messages for classification
		if a.classifyQueue != nil {
			a.classifyQueue.Submit(msg.Messages)
		}
		return a, nil

	case imaplib.MessageBodyMsg:
		return a, a.handleMessageBodyResult(msg)

	case attachActionResultMsg:
		switch msg.action {
		case "open":
			if msg.err != nil {
				a.flash("Open failed: "+msg.err.Error(), "err")
			}
		case "download":
			if msg.err != nil {
				a.flash("Download failed: "+msg.err.Error(), "err")
			} else {
				a.flash("Saved to "+msg.path, "ok")
			}
		case "editor":
			if msg.err != nil {
				a.flash("Editor error: "+msg.err.Error(), "err")
			}
		}
		return a, nil

	case imaplib.SetFlagResultMsg:
		if msg.Err != nil {
			a.flash("Flag error: "+msg.Err.Error(), "err")
		}
		return a, nil

	case imaplib.MoveToTrashResultMsg:
		if msg.Err != nil {
			a.flash("Move failed: "+msg.Err.Error(), "err")
			return a, nil
		}
		a.flash("Moved to Trash"+a.registerUndo(msg.Account, msg.Folder, msg.Dest, msg.DestUID, "Trash"), "ok")
		a.removeInboxMessageByUID(msg.UID)
		return a, nil

	case imaplib.AppendMessageResultMsg:
		// Silently ignore Sent/Drafts append errors — the send already succeeded.
		return a, nil

	case imaplib.CreateFolderResultMsg:
		if msg.Err != nil {
			a.flash(fmt.Sprintf("%s Failed to create folder: %s", icons.Error, msg.Err.Error()), "err")
			return a, nil
		}
		a.flash(fmt.Sprintf("%s Folder '%s' created", icons.FolderNew, msg.Name), "ok")
		// Refresh folder list for the active account.
		if client, ok := a.imapClients[a.activeAccount]; ok {
			return a, client.FetchFolders()
		}
		return a, nil

	case imaplib.MoveMessageResultMsg:
		if msg.Err != nil {
			a.flash("Move failed: "+msg.Err.Error(), "err")
			return a, nil
		}
		a.flash("Moved to "+msg.Dest+a.registerUndo(msg.Account, msg.Folder, msg.Dest, msg.DestUID, msg.Dest), "ok")
		a.removeInboxMessageByUID(msg.UID)
		return a, nil

	case imaplib.SearchResultMsg:
		if msg.Err != nil {
			a.searchOverlay.SetSearchResults(nil, false)
			a.flash("Search error: "+msg.Err.Error(), "err")
			return a, nil
		}
		a.searchOverlay.SetResults(msg.Messages)
		return a, nil

	case embeddings.StreamSearchMsg:
		if msg.Err != nil {
			a.searchOverlay.SetSearchResults(nil, false)
			a.flash("Search error: "+msg.Err.Error(), "err")
			return a, nil
		}
		if msg.Seq != a.searchSeq {
			return a, nil
		}
		results := convertSearchResults(msg.Results)
		a.searchOverlay.SetSearchResults(results, msg.Loading)
		if msg.Loading {
			return a, a.searchStreamTick(msg.Seq)
		}
		return a, nil

	case outsmtp.SendResultMsg:
		if msg.Err != nil {
			a.flash("Send failed: "+msg.Err.Error(), "err")
		} else {
			a.flash("Message sent!", "ok")
		}
		return a, nil

	case syncMsg:
		client, ok := a.imapClients[msg.account]
		if !ok {
			return a, nil
		}
		a.header.SetSyncState("syncing")
		a.statusbar.SetLoading(true)
		return a, tea.Batch(
			client.FetchMessages(a.activeFolder, a.cfg.General.PageSize),
			spinnerTick(),
		)

	case spinnerTickMsg:
		a.statusbar.AdvanceSpinner()
		a.searchOverlay.AdvanceSpinner()
		if a.statusbar.loading || a.searchOverlay.IsLoading() {
			return a, spinnerTick()
		}
		return a, nil

	case searchDebounceMsg:
		if !a.searchOverlay.IsActive() || msg.id != a.searchOverlay.DebounceID() {
			return a, nil
		}
		q := a.searchOverlay.CompiledQuery()
		if !a.searchOverlay.CanSearch() {
			return a, nil
		}
		a.searchOverlay.SetSearchResults(nil, true)
		a.searchSeq++
		cmds := []tea.Cmd{a.doStreamingSearch(q, a.searchSeq)}
		if !a.statusbar.loading {
			cmds = append(cmds, spinnerTick())
		}
		return a, tea.Batch(cmds...)

	case embeddingStartedMsg:
		a.statusbar.SetEmbeddingStats(msg.stats)
		if msg.stats.Queued > 0 || msg.stats.InFlight > 0 {
			return a, embeddingTick()
		}
		return a, nil

	case embeddingTickMsg:
		if a.embQueue != nil {
			a.statusbar.SetEmbeddingStats(a.embQueue.Stats())
			stats := a.embQueue.Stats()
			if stats.Queued > 0 || stats.InFlight > 0 {
				return a, embeddingTick()
			}
		}
		return a, nil

	case classifyBackfillMsg:
		// No-op for now; backfill is fire-and-forget
		return a, nil

	case smartFolderMsg:
		if msg.err != nil {
			a.flash("Smart folder error: "+msg.err.Error(), "err")
			return a, nil
		}
		threads := thread.BuildThreads(msg.messages)
		a.inboxView.SetThreads(threads)
		return a, nil

	case classifyResultReadyMsg:
		return a, a.handleClassifyReady(msg)

	case suggestResultReadyMsg:
		return a, a.handleSuggestReady(msg)

	case prefetchTickMsg:
		if !a.cfg.Embeddings.PrefetchBodies {
			return a, nil
		}
		batch := a.cfg.Embeddings.PrefetchBatch
		if batch <= 0 {
			batch = 10
		}
		cmds := a.prefetchBodies(batch)
		if len(cmds) == 0 {
			return a, prefetchTick(a.embeddingPrefetchInterval())
		}
		return a, tea.Batch(append(cmds, prefetchTick(a.embeddingPrefetchInterval()))...)

	case quickMoveResultMsg:
		if msg.err != nil {
			a.flash("Quick move failed: "+msg.err.Error(), "err")
			return a, nil
		}
		if msg.dest == "" {
			a.openFolderPicker()
			return a, nil
		}
		if client, ok := a.imapClients[msg.account]; ok {
			return a, client.MoveMessage(msg.folder, msg.uid, msg.dest)
		}
		return a, nil

	case moveHintMsg:
		if msg.dest == "" {
			a.statusbar.SetMoveHint("?") // no AI match — will open folder picker
		} else {
			a.statusbar.SetMoveHint(msg.dest)
		}
		a.applyQuickMenuState()
		return a, nil

	case clearStatusMsg:
		if msg.seq != a.statusbar.MessageSeq() {
			return a, nil // a newer flash replaced this one; its own timer will clear it
		}
		a.statusbar.ClearMessage()
		a.lastUndo = nil // undo is offered only while its flash is visible
		a.updateLayout() // message gone; reclaim the extra line
		return a, nil

	case undoResultMsg:
		if msg.err != nil {
			a.flash("Undo failed: "+msg.err.Error(), "err")
			return a, nil
		}
		// Re-sync the folder the message returned to so it reappears in the list.
		if msg.account == a.activeAccount && msg.folder == a.activeFolder {
			return a, tea.Batch(a.flash("Undone", "ok"), a.fetchMessages())
		}
		return a, a.flash("Undone", "ok")

	case quitTimeoutMsg:
		a.quitPending = false
		return a, nil

	case firstRunTipMsg:
		if a.persisted != nil && a.persisted.TipSeen {
			return a, nil
		}
		if a.persisted != nil {
			a.persisted.TipSeen = true
			a.persistUI()
		}
		return a, a.flash(fmt.Sprintf("%s Tip: press . for all commands · ? for keys · ctrl+z to undo", icons.Sparkle), "info")

	case tea.MouseMsg:
		if a.comp.IsActive() {
			if a.comp.HandleMouse(msg) {
				if r := a.comp.Result(); r != nil {
					cmd := a.handleComposerResult(r)
					a.comp.ClearResult()
					return a, cmd
				}
				return a, nil
			}
		}
		return a.handleMouse(msg)

	case tea.KeyMsg:
		return a.handleKey(msg)
	}

	return a, nil
}
