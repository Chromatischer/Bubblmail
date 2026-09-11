package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bubblmail/bubblmail/cache"
	classifylib "github.com/bubblmail/bubblmail/classify"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/embeddings"
	imaplib "github.com/bubblmail/bubblmail/imap"
	outsmtp "github.com/bubblmail/bubblmail/smtp"
	"github.com/bubblmail/bubblmail/suggest"
	"github.com/bubblmail/bubblmail/thread"
	"github.com/bubblmail/bubblmail/ui/composer"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/ui/views"
	"github.com/bubblmail/bubblmail/util"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ViewID identifies which main pane is active.
type ViewID int

const (
	ViewInbox       ViewID = iota
	ViewReader             // reading a single message
	ViewFolder             // folder browser
	ViewSmartFolder        // AI-categorized smart folder
)

// syncMsg triggers a background sync.
type syncMsg struct{ account string }

// spinnerTickMsg advances the loading spinner.
type spinnerTickMsg struct{}

// searchDebounceMsg fires after the debounce delay to trigger a search.
type searchDebounceMsg struct{ id int }

// embeddingTickMsg refreshes embedding status.
type embeddingTickMsg struct{}

// embeddingStartedMsg reports embedding queue activity.
type embeddingStartedMsg struct {
	stats embeddingStats
}

type prefetchTickMsg struct{}
type suggestPollMsg struct{}

type quickMoveResultMsg struct {
	msgID   int64
	account string
	folder  string
	uid     uint32
	dest    string
	score   float32
	err     error
}

// moveHintMsg carries a precomputed folder suggestion for the quick-move hint.
type moveHintMsg struct{ dest string }

// clearStatusMsg clears the flash message.
type clearStatusMsg struct{}

// quitTimeoutMsg clears the pending quit state.
type quitTimeoutMsg struct{}

// classifyResultMsg is sent when the classification queue produces results.
type classifyResultMsg struct {
	account  string
	category string
}

// classifyBackfillMsg reports the result of a backfill classification pass.
type classifyBackfillMsg struct {
	account string
	queued  int
}

// App is the root Bubble Tea model.
type App struct {
	cfg       *config.Config
	theme     *config.Theme
	store     *cache.Store
	embClient *embeddings.Client
	embQueue  *embeddingQueue
	debugFile *os.File

	// Demo mode: skip IMAP connects and inject pre-canned data.
	demoMode bool
	demoData *demoInitData

	// Classification (smart folders)
	classifyClient  *classifylib.Client
	classifyQueue   *classifylib.Queue
	classifyResults chan classifylib.ResultMsg
	suggestClient   *suggest.Client
	suggestQueue    *suggest.Queue
	suggestResults  chan suggest.ResultMsg
	smartFolder     string // active smart folder name (when viewID == ViewSmartFolder)
	smartCounts     map[string]int
	prevViewID      ViewID // view to return to when closing reader

	// IMAP clients, one per account
	imapClients map[string]*imaplib.Client

	// UI components
	header        *Header
	sidebar       *Sidebar
	statusbar     *StatusBar
	helpOverlay   *HelpOverlay
	searchOverlay *SearchOverlay
	folderPicker  *FolderPickerOverlay
	newFolder     *NewFolderOverlay

	// Main views
	viewID     ViewID
	inboxView  *views.InboxView
	readerView *views.ReaderView
	folderView *views.FolderView

	// Overlay
	comp *composer.Composer

	// State
	width          int
	height         int
	showSidebar    bool
	wantSidebar    bool
	uiState        *config.UIState
	showHelp       bool
	sidebarFocused bool
	activeAccount  string
	activeFolder   string

	// Pagination
	loadedMessages []*data.Message
	fetchedCount   int
	loadingMore    bool
	allLoaded      bool
	quitPending    bool
	searchSeq      int
	searchState    *searchState
	prefetchSkip   map[int64]bool
	quickMenu      *quickMenuState

	accounts []*data.Account

	// Text selection via mouse drag
	drag       *dragState
	plainLines []string // ANSI-stripped lines of last render, used for plain-text copy
	ansiLines  []string // ANSI-rich lines of last render, used for markdown copy
}

// dragState tracks an in-progress mouse drag for text selection.
type dragState struct {
	startX, startY int
	endX, endY     int
	isDrag         bool // true once the pointer moved ≥1 cell from start
	canCopy        bool // true only when drag started in a copyable zone (reader body)
	// Zone bounds (inclusive) constrain the selection to the UI element where
	// the drag started, so a drag begun in the email body cannot bleed into
	// the sidebar, header, or status bar.
	zoneX0, zoneY0, zoneX1, zoneY1 int
}

type quickMenuSide int

const (
	quickMenuNone quickMenuSide = iota
	quickMenuLeft
	quickMenuRight
)

type quickMenuState struct {
	side quickMenuSide
	step int
}

type searchState struct {
	seq       int
	queryVec  []float32
	queryNorm float32
	msgs      []*data.Message
	vectors   [][]float32
	norms     []float32
	nextIndex int
	results   []*embeddings.SearchHit
	semItems  []embeddings.TopKItem
	simHeap   *embeddings.TopKHeap
}

func (a *App) canQuitNow() bool {
	return (a.viewID == ViewInbox || a.viewID == ViewSmartFolder) &&
		!a.sidebarFocused &&
		!a.searchOverlay.IsActive() &&
		!a.folderPicker.IsActive() &&
		!a.newFolder.IsActive() &&
		!a.showHelp &&
		!a.comp.IsActive()
}

// NewApp creates the root application model.
func NewApp(cfg *config.Config, store *cache.Store) *App {
	theme := config.NewTheme(cfg)
	embClient, _ := embeddings.NewClient(cfg.Embeddings)
	var embQueue *embeddingQueue
	if embClient != nil {
		embQueue = newEmbeddingQueue(cfg.Embeddings, embClient, store)
	}

	classifyClient, _ := classifylib.NewClient(cfg.Classification, cfg.Embeddings)
	classifyResults := make(chan classifylib.ResultMsg, 256)
	var classifyQueue *classifylib.Queue
	if classifyClient != nil {
		classifyQueue = classifylib.NewQueue(classifyClient, store, classifyResults)
	}
	suggestClient, _ := suggest.NewClient(cfg.Classification, cfg.Embeddings)
	suggestResults := make(chan suggest.ResultMsg, 128)
	var suggestQueue *suggest.Queue
	if suggestClient != nil {
		suggestQueue = suggest.NewQueue(suggestClient, store, suggestResults)
	}

	app := &App{
		cfg:             cfg,
		theme:           theme,
		store:           store,
		embClient:       embClient,
		embQueue:        embQueue,
		classifyClient:  classifyClient,
		classifyQueue:   classifyQueue,
		classifyResults: classifyResults,
		suggestClient:   suggestClient,
		suggestQueue:    suggestQueue,
		suggestResults:  suggestResults,
		smartCounts:     make(map[string]int),
		imapClients:     make(map[string]*imaplib.Client),
		header:          NewHeader(theme),
		sidebar:         NewSidebar(theme),
		statusbar:       NewStatusBar(theme),
		helpOverlay:     NewHelpOverlay(theme),
		showSidebar:     true,
		wantSidebar:     true,
		viewID:          ViewInbox,
		prefetchSkip:    make(map[int64]bool),
		quickMenu:       &quickMenuState{side: quickMenuNone},
	}

	app.searchOverlay = NewSearchOverlay(theme)
	app.folderPicker = NewFolderPickerOverlay(theme)
	app.newFolder = NewNewFolderOverlay(theme)
	app.inboxView = views.NewInboxView(theme)
	app.readerView = views.NewReaderView(theme)
	app.folderView = views.NewFolderView(theme)
	app.comp = composer.NewComposer(theme)

	// Set up accounts from config
	for _, acfg := range cfg.Accounts {
		acct := &data.Account{
			Name:         acfg.Name,
			Username:     acfg.Username,
			IMAPHost:     acfg.IMAPHost,
			IMAPPort:     acfg.IMAPPort,
			SMTPHost:     acfg.SMTPHost,
			SMTPPort:     acfg.SMTPPort,
			ActiveFolder: "INBOX",
		}
		app.accounts = append(app.accounts, acct)
	}
	if len(app.accounts) > 0 {
		app.activeAccount = app.accounts[0].Name
		app.activeFolder = "INBOX"
	}

	app.sidebar.SetAccounts(app.accounts)
	app.sidebar.SetActive(app.activeAccount, app.activeFolder)

	// Restore the parts of the layout the user last left in place. Fold state
	// is worth persisting because an account with thirty mailboxes is only
	// usable folded, and refolding it at every start is busywork.
	app.uiState = config.LoadState()
	app.sidebar.SetCollapsed(app.uiState.CollapsedFolders)
	app.sidebar.OnFoldChange(app.saveUIState)
	if app.uiState.SidebarHidden {
		app.showSidebar = false
		app.wantSidebar = false
	}

	return app
}

// saveUIState persists the layout state. Failures are ignored on purpose:
// losing a fold is not worth an error banner over the user's mail.
func (a *App) saveUIState() {
	if a.uiState == nil {
		a.uiState = &config.UIState{}
	}
	a.uiState.CollapsedFolders = a.sidebar.CollapsedKeys()
	a.uiState.SidebarHidden = !a.wantSidebar
	_ = a.uiState.Save()
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.SetWindowTitle("bubblmail"),
	}
	if a.embQueue != nil {
		a.statusbar.SetEmbeddingStats(a.embQueue.Stats())
		cmds = append(cmds, embeddingTick())
	}
	if a.cfg.Embeddings.PrefetchBodies {
		interval := time.Duration(a.cfg.Embeddings.PrefetchIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 2 * time.Second
		}
		cmds = append(cmds, prefetchTick(interval))
	}
	if a.classifyQueue != nil {
		cmds = append(cmds, classifyResultsTick())
	}
	if a.suggestQueue != nil {
		cmds = append(cmds, suggestResultsTick())
	}
	// Load any previously cached classification counts immediately so the
	// sidebar is populated on first render without waiting for new messages.
	if len(a.cfg.Classification.Categories) > 0 || a.classifyQueue != nil {
		a.refreshSmartCounts()
	}

	if len(a.cfg.Accounts) == 0 {
		return tea.Batch(cmds...)
	}

	// In demo mode, inject pre-canned data instead of connecting to IMAP.
	if a.demoMode && a.demoData != nil {
		dd := a.demoData
		cmds = append(cmds,
			func() tea.Msg {
				return imaplib.FolderListMsg{Account: dd.account, Folders: dd.folders}
			},
			func() tea.Msg {
				return imaplib.MessageListMsg{Account: dd.account, Folder: "INBOX", Messages: dd.messages}
			},
		)
		return tea.Batch(cmds...)
	}

	// Connect to all accounts and fetch folders/messages
	for i := range a.cfg.Accounts {
		acfg := &a.cfg.Accounts[i]
		cmds = append(cmds, a.connectAccount(acfg))
	}

	return tea.Batch(cmds...)
}

// connectAccount returns a cmd that connects to an account and fetches folders.
func (a *App) connectAccount(acfg *config.AccountConfig) tea.Cmd {
	return func() tea.Msg {
		client, err := imaplib.Connect(acfg)
		if err != nil {
			return imaplib.FolderListMsg{Account: acfg.Name, Err: err}
		}
		// Store client
		// Note: this is safe since we're in a tea.Cmd goroutine before Update runs
		// The client is stored in the next Update via FolderListMsg
		_ = client // returned via the stored reference in the closure
		return connectResultMsg{account: acfg.Name, client: client}
	}
}

// connectResultMsg delivers a successfully connected IMAP client.
type connectResultMsg struct {
	account string
	client  *imaplib.Client
}

type embeddingBackfillMsg struct {
	account string
	queued  int
	err     error
}

// scheduleSyncTick returns a cmd that fires a sync tick.
func (a *App) scheduleSyncTick(account string) tea.Cmd {
	interval := time.Duration(a.cfg.General.SyncIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return syncMsg{account: account}
	})
}

// spinnerTick returns a spinner tick cmd.
func spinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

func (a *App) startEmbeddingBackfill(account string) tea.Cmd {
	return func() tea.Msg {
		if a.embQueue == nil {
			return embeddingBackfillMsg{account: account}
		}
		queued, err := a.embQueue.BackfillAccount(account, false)
		return embeddingBackfillMsg{account: account, queued: queued, err: err}
	}
}

// startClassifyBackfill queues unclassified inbox messages for an account.
func (a *App) startClassifyBackfill(account string) tea.Cmd {
	return func() tea.Msg {
		if a.classifyQueue == nil {
			return classifyBackfillMsg{account: account}
		}
		msgs, err := a.store.GetUnclassifiedInboxMessages(account, 200)
		if err != nil || len(msgs) == 0 {
			return classifyBackfillMsg{account: account}
		}
		a.classifyQueue.Submit(msgs)
		return classifyBackfillMsg{account: account, queued: len(msgs)}
	}
}

func prefetchTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return prefetchTickMsg{}
	})
}

// embeddingTick returns a tick cmd for embedding status updates.
func embeddingTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return embeddingTickMsg{}
	})
}

// classifyResultsTick polls the classification results channel.
func classifyResultsTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return classifyPollMsg{}
	})
}

func suggestResultsTick() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return suggestPollMsg{}
	})
}

// classifyPollMsg is the tick that drains the classify results channel.
type classifyPollMsg struct{}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			msg.client.FetchMessages("INBOX", a.cfg.General.PageSize),
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
			if msg.Account == a.activeAccount && msg.Folder == a.activeFolder {
				a.header.SetSyncState("error")
				a.statusbar.SetLoading(false)
			}
			return a, a.scheduleSyncTick(msg.Account)
		}
		if err := a.store.UpsertMessages(msg.Messages); err != nil {
			a.flash("Cache write error: "+err.Error(), "err")
		}
		// Submit new inbox messages for classification
		if a.classifyQueue != nil {
			a.classifyQueue.Submit(msg.Messages)
		}
		// A background response for another account or a folder that the user
		// has left must update the cache without replacing the visible inbox.
		if msg.Account != a.activeAccount || msg.Folder != a.activeFolder {
			return a, a.scheduleSyncTick(msg.Account)
		}
		a.loadedMessages = msg.Messages
		a.fetchedCount = len(msg.Messages)
		a.loadingMore = false
		a.allLoaded = false
		threads := thread.BuildThreads(a.loadedMessages)
		a.inboxView.SetThreads(threads)
		a.applyQuickMenuState()
		a.header.SetSyncState("synced")
		a.statusbar.SetLoading(false)
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
		a.fetchedCount += len(msg.Messages)
		a.loadedMessages = append(a.loadedMessages, msg.Messages...)
		if err := a.store.UpsertMessages(msg.Messages); err != nil {
			a.flash("Cache write error: "+err.Error(), "err")
		}
		threads := thread.BuildThreads(a.loadedMessages)
		a.inboxView.AppendThreads(threads)
		a.applyQuickMenuState()
		// Submit newly loaded messages for classification
		if a.classifyQueue != nil {
			a.classifyQueue.Submit(msg.Messages)
		}
		return a, nil

	case imaplib.MessageBodyMsg:
		if msg.Err != nil {
			details := fmt.Sprintf("Body fetch error (%s %s uid=%d id=%d): %s", msg.Account, msg.Folder, msg.UID, msg.MsgID, msg.Err.Error())
			a.flash(details, "err")
			if msg.MsgID > 0 {
				a.prefetchSkip[msg.MsgID] = true
			}
			return a, nil
		}
		// Store body in cache
		if msg.MsgID > 0 {
			if err := a.store.UpsertBody(msg.MsgID, msg.Text, msg.HTML); err != nil {
				a.flash("Cache error: "+err.Error(), "err")
			}
		}
		// Store attachments and update the reader — must happen before any early return.
		if msg.MsgID > 0 {
			if m := a.findMessageByID(msg.MsgID); m != nil {
				m.Attachments = msg.Attachments
			}
		}
		if a.viewID == ViewReader {
			a.readerView.UpdateMessageBody(msg.Folder, msg.UID, msg.Text, msg.HTML, msg.Attachments)
		}
		if msg.MsgID > 0 && a.embQueue != nil && msg.Text != "" {
			m := a.findMessageByID(msg.MsgID)
			if m != nil {
				cmd := a.embedMessage(m, msg.Text)
				if a.cfg.Embeddings.FolderSampleLimit > 0 {
					return a, tea.Batch(cmd, func() tea.Msg {
						_ = a.store.BuildFolderEmbeddings(m.AccountName, a.cfg.Embeddings.Model, a.cfg.Embeddings.FolderSampleLimit)
						return nil
					})
				}
				return a, cmd
			}
		}
		if msg.MsgID > 0 {
			m := a.findMessageByID(msg.MsgID)
			if m != nil && a.viewID == ViewReader {
				// Single-message mode: trigger when the open message body arrives.
				if a.readerView.CurrentMessage() != nil && a.readerView.CurrentMessage().ID == msg.MsgID {
					return a, a.loadSuggestedEvent(m)
				}
				// Thread mode: trigger when the latest message's body arrives.
				if t := a.readerView.CurrentThread(); t != nil {
					if latest := t.Latest(); latest != nil && latest.ID == msg.MsgID {
						return a, a.loadSuggestedEvent(latest)
					}
				}
			}
		}
		return a, nil

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
		a.flash("Moved to Trash", "ok")
		// Remove the message from local state and refresh the inbox view.
		// Preserve cursor: keep it at the same index so the next thread is selected.
		cursor := a.inboxView.CursorPos()
		a.loadedMessages = removeByUID(a.loadedMessages, msg.UID)
		a.fetchedCount = len(a.loadedMessages)
		threads := thread.BuildThreads(a.loadedMessages)
		a.inboxView.AppendThreads(threads)
		if cursor < len(threads) {
			a.inboxView.SetCursor(cursor)
		}
		if a.viewID == ViewReader {
			a.viewID = ViewInbox
		}
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
		a.flash("Moved to "+msg.Dest, "ok")
		cursor := a.inboxView.CursorPos()
		a.loadedMessages = removeByUID(a.loadedMessages, msg.UID)
		a.fetchedCount = len(a.loadedMessages)
		threads := thread.BuildThreads(a.loadedMessages)
		a.inboxView.AppendThreads(threads)
		if cursor < len(threads) {
			a.inboxView.SetCursor(cursor)
		}
		if a.viewID == ViewReader {
			a.viewID = ViewInbox
		}
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
			return a, nil
		}
		a.flash("Message sent!", "ok")
		// Append the exact submitted message after SMTP accepts it. This keeps
		// the Sent copy's Message-ID identical and avoids saving failed sends.
		if sent := a.findSentFolder(msg.Account); sent != "" && len(msg.Raw) > 0 {
			if client, ok := a.imapClients[msg.Account]; ok {
				return a, client.AppendMessage(sent, msg.Raw, []data.Flag{data.FlagSeen})
			}
		}
		return a, nil

	case syncMsg:
		client, ok := a.imapClients[msg.account]
		if !ok {
			return a, nil
		}
		folder := "INBOX"
		if msg.account == a.activeAccount {
			folder = a.activeFolder
			a.header.SetSyncState("syncing")
			a.statusbar.SetLoading(true)
		}
		return a, tea.Batch(
			client.FetchMessages(folder, a.cfg.General.PageSize),
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

	case classifyPollMsg:
		if a.classifyQueue == nil {
			return a, nil
		}
		// Drain all pending results without blocking
		changed := false
		for {
			select {
			case r := <-a.classifyResults:
				if r.Err == nil && r.Category != "" {
					a.smartCounts[r.Category]++
					changed = true
				}
			default:
				goto drained
			}
		}
	drained:
		if changed {
			a.refreshSmartCounts()
			// If viewing a smart folder, refresh its thread list
			if a.viewID == ViewSmartFolder && a.smartFolder != "" {
				return a, tea.Batch(classifyResultsTick(), a.fetchSmartFolder(a.smartFolder))
			}
		}
		return a, classifyResultsTick()

	case suggestPollMsg:
		if a.suggestQueue == nil {
			return a, nil
		}
		for {
			select {
			case r := <-a.suggestResults:
				if r.Err != nil && r.Event == nil {
					a.flash("Suggested event error: "+r.Err.Error(), "err")
					r.Event = &data.SuggestedEvent{
						MessageID:    r.MessageID,
						GenerationOK: false,
						ParseError:   r.Err.Error(),
					}
				}
				if cur := a.readerView.CurrentMessage(); cur != nil && cur.ID == r.MessageID {
					a.readerView.SetSuggestedEvent(r.Event)
				} else if t := a.readerView.CurrentThread(); t != nil {
					if latest := t.Latest(); latest != nil && latest.ID == r.MessageID {
						a.readerView.SetSuggestedEvent(r.Event)
					}
				}
				if r.Event != nil && !r.Event.GenerationOK && r.Err != nil {
					a.flash("Suggested event parse error: "+r.Err.Error(), "err")
				}
			default:
				return a, suggestResultsTick()
			}
		}

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
			interval := time.Duration(a.cfg.Embeddings.PrefetchIntervalSeconds) * time.Second
			if interval <= 0 {
				interval = 2 * time.Second
			}
			return a, prefetchTick(interval)
		}
		interval := time.Duration(a.cfg.Embeddings.PrefetchIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 2 * time.Second
		}
		return a, tea.Batch(append(cmds, prefetchTick(interval))...)

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
		a.statusbar.ClearMessage()
		a.updateLayout() // message gone; reclaim the extra line
		return a, nil

	case quitTimeoutMsg:
		a.quitPending = false
		return a, nil

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

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// 1. Composer captures all keys when active
	if a.comp.IsActive() {
		a.comp.HandleKey(key)
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
		case " ", "space":
			a.sidebar.ToggleFold()
		case "l", "right":
			a.sidebar.Expand()
		case "H":
			if acct, _, ok := a.sidebar.Selected(); ok && acct != "" {
				a.sidebar.FoldAll(acct, true)
			}
		case "L":
			if acct, _, ok := a.sidebar.Selected(); ok && acct != "" {
				a.sidebar.FoldAll(acct, false)
			}
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
		case "h", "left":
			// h folds the tree up one level at a time and only leaves the
			// sidebar once there is nothing left to fold.
			if !a.sidebar.Collapse() {
				a.sidebarFocused = false
				a.sidebar.SetFocused(false)
			}
		case "esc", "tab", "q":
			a.sidebarFocused = false
			a.sidebar.SetFocused(false)
		case "ctrl+c":
			return a, tea.Quit
		}
		return a, nil
	}

	// 7. Global keys
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

	case "b":
		a.wantSidebar = !a.wantSidebar
		a.saveUIState()
		a.updateLayout()

	case "tab":
		if a.viewID == ViewReader && a.readerView.HasAttachments() {
			a.readerView.FocusNextAttachment(1)
			return a, nil
		}
		if a.showSidebar {
			a.sidebarFocused = true
			a.sidebar.SetFocused(true)
			if a.viewID == ViewSmartFolder {
				a.sidebar.FocusAt("", a.smartFolder)
			} else {
				a.sidebar.FocusAt(a.activeAccount, a.activeFolder)
			}
		}

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

	case "shift+tab":
		if a.viewID == ViewReader && a.readerView.HasAttachments() {
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
	}

	return a, nil
}

func (a *App) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Mouse wheel scrolling for content views.
	if msg.Button == tea.MouseButtonWheelDown {
		a.moveDown()
		return a, a.maybeLoadMore()
	}
	if msg.Button == tea.MouseButtonWheelUp {
		a.moveUp()
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
	overlayActive := a.comp.IsActive() || a.searchOverlay.IsActive() || a.folderPicker.IsActive() || a.newFolder.IsActive()
	if !overlayActive && a.showSidebar && x < sidebarRenderedWidth() {
		contentY := y - headerH
		if acct, folder, ok := a.sidebar.HitTestFold(x, contentY); ok {
			a.sidebar.ToggleFoldAt(acct, folder)
			return a, nil
		}
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
					a.comp.HandleKey(key)
				}
			} else if field := a.comp.HitTestField(contentY); field >= 0 {
				a.comp.SetFocus(field)
			} else if key := a.comp.HitTestFooter(x, contentY); key != "" {
				a.comp.HandleKey(key)
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
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+enter":
		// Terminals deliver ctrl+enter as ctrl+j, and the composer does not
		// bind ctrl+j — synthesizing it would click through to nothing. The
		// send hint uses ctrl+s for that reason.
		return tea.KeyMsg{Type: tea.KeyCtrlJ}
	}
	if len([]rune(key)) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

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

func (a *App) applyQuickMenuState() {
	if a.inboxView == nil || a.quickMenu == nil {
		return
	}
	if a.quickMenu.side == quickMenuNone || (a.viewID != ViewInbox && a.viewID != ViewSmartFolder) {
		a.inboxView.SetQuickMenu("", 0, "")
		return
	}
	side := ""
	if a.quickMenu.side == quickMenuLeft {
		side = "left"
	} else if a.quickMenu.side == quickMenuRight {
		side = "right"
	}
	a.inboxView.SetQuickMenu(side, a.quickMenu.step, a.statusbar.MoveHint())
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

func (a *App) fetchMessages() tea.Cmd {
	a.loadedMessages = nil
	a.fetchedCount = 0
	a.loadingMore = false
	a.allLoaded = false

	client, ok := a.imapClients[a.activeAccount]
	if !ok {
		// Try loading from cache
		threads, err := a.store.GetThreads(a.activeAccount, a.activeFolder)
		if err == nil {
			// Flatten to messages for BuildThreads
			var msgs []*data.Message
			for _, t := range threads {
				msgs = append(msgs, t.Messages...)
			}
			built := thread.BuildThreads(msgs)
			a.inboxView.SetThreads(built)
		}
		return nil
	}
	a.statusbar.SetLoading(true)
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

func (a *App) openCompose() tea.Cmd {
	from := a.senderAddress()
	a.comp.OpenNew(from)
	a.comp.SetSize(a.width, a.height-a.headerHeight()-a.statusHeight())
	return nil
}

func (a *App) openReply(msg *data.Message, replyAll bool) tea.Cmd {
	from := a.senderAddress()
	a.comp.OpenReply(from, msg, replyAll)
	a.comp.SetSize(a.width, a.height-a.headerHeight()-a.statusHeight())
	return nil
}

func (a *App) openForward(msg *data.Message) tea.Cmd {
	from := a.senderAddress()
	a.comp.OpenForward(from, msg)
	a.comp.SetSize(a.width, a.height-a.headerHeight()-a.statusHeight())
	return nil
}

func (a *App) senderAddress() data.Address {
	for _, acfg := range a.cfg.Accounts {
		if acfg.Name == a.activeAccount {
			return data.Address{Address: acfg.Username}
		}
	}
	return data.Address{}
}

func (a *App) toggleStar() tea.Cmd {
	threads := a.inboxView.SelectedThreads()
	if len(threads) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, t := range threads {
		msg := t.Latest()
		if msg == nil {
			continue
		}
		starred := msg.IsStarred()
		newFlags := toggleFlag(msg.Flags, data.FlagFlagged, !starred)
		_ = a.store.SetFlags(msg.AccountName, msg.FolderName, msg.UID, newFlags)
		msg.Flags = newFlags
		// Recompute thread aggregate so the inbox re-renders immediately.
		t.Starred = false
		for _, m := range t.Messages {
			if m.IsStarred() {
				t.Starred = true
				break
			}
		}
		client, ok := a.imapClients[msg.AccountName]
		if ok {
			cmds = append(cmds, client.SetFlag(msg.FolderName, msg.UID, data.FlagFlagged, !starred))
		}
	}
	a.inboxView.ClearSelection()
	return tea.Batch(cmds...)
}

func (a *App) toggleRead() tea.Cmd {
	threads := a.inboxView.SelectedThreads()
	if len(threads) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, t := range threads {
		msg := t.Latest()
		if msg == nil {
			continue
		}
		read := msg.IsRead()
		newFlags := toggleFlag(msg.Flags, data.FlagSeen, !read)
		_ = a.store.SetFlags(msg.AccountName, msg.FolderName, msg.UID, newFlags)
		msg.Flags = newFlags
		// Recompute thread aggregate so the inbox re-renders immediately.
		t.HasUnread = false
		for _, m := range t.Messages {
			if !m.IsRead() {
				t.HasUnread = true
				break
			}
		}
		client, ok := a.imapClients[msg.AccountName]
		if ok {
			cmds = append(cmds, client.SetFlag(msg.FolderName, msg.UID, data.FlagSeen, !read))
		}
	}
	a.inboxView.ClearSelection()
	return tea.Batch(cmds...)
}

func (a *App) quickMoveMessage() tea.Cmd {
	if a.viewID != ViewInbox && a.viewID != ViewSmartFolder {
		return nil
	}
	threads := a.inboxView.SelectedThreads()
	if len(threads) == 0 {
		return nil
	}
	if a.embClient == nil {
		// No embeddings — open folder picker; selection stays active so
		// moveMessageToFolder will act on all selected threads when confirmed.
		a.openFolderPicker()
		return nil
	}
	var cmds []tea.Cmd
	for _, t := range threads {
		msg := t.Latest()
		if msg == nil {
			continue
		}
		if cmd := a.quickMoveSingleMessage(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	a.inboxView.ClearSelection()
	return tea.Batch(cmds...)
}

func (a *App) quickMoveSingleMessage(msg *data.Message) tea.Cmd {
	account := a.activeAccount
	folder := a.activeFolder
	uid := msg.UID
	msgID := msg.ID

	return func() tea.Msg {
		model := a.cfg.Embeddings.Model
		if model == "" {
			model = "openai/text-embedding-3-small"
		}
		vec, norm, err := a.store.GetEmbedding(msgID, model)
		if err != nil {
			return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, err: err}
		}
		if vec == nil {
			bodyText, _, err := a.store.GetBody(msgID)
			if err != nil {
				return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, err: err}
			}
			content := strings.TrimSpace(msg.Subject + "\n" + bodyText)
			if bodyText == "" && msg.Snippet != "" {
				content = strings.TrimSpace(msg.Subject + "\n" + msg.Snippet)
			}
			maxChars := a.cfg.Embeddings.MaxContentChars
			if maxChars <= 0 {
				maxChars = 8000
			}
			if len(content) > maxChars {
				content = content[:maxChars]
			}
			if content != "" {
				vecs, err := a.embClient.EmbedTexts(context.Background(), []string{content})
				if err == nil && len(vecs) > 0 {
					vec = vecs[0]
					norm = embeddings.VectorNorm(vec)
					hash := embeddings.HashContent(content)
					_ = a.store.SaveEmbedding(msgID, model, vec, norm, hash)
					_ = a.store.BuildFolderEmbeddings(account, model, a.cfg.Embeddings.FolderSampleLimit)
				}
			}
		}
		if vec == nil || norm == 0 {
			return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, dest: ""}
		}
		folders, vectors, norms, _, err := a.store.ListFolderEmbeddings(account, model)
		if err != nil {
			return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, err: err}
		}
		bestScore := float32(0)
		bestFolder := ""
		for i, f := range folders {
			if f == nil {
				continue
			}
			if !f.IsSelectable() {
				continue
			}
			if f.Name == folder {
				continue
			}
			if isSystemFolder(f.DisplayName, f.Name) {
				continue
			}
			score := embeddings.CosineSimilarity(vec, norm, vectors[i], norms[i])
			if score > bestScore {
				bestScore = score
				bestFolder = f.Name
			}
		}
		threshold := float32(a.cfg.Embeddings.AutoMoveThreshold)
		if threshold <= 0 {
			threshold = 0.27
		}
		if bestFolder == "" || bestScore < threshold {
			return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, dest: "", score: bestScore}
		}
		return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, dest: bestFolder, score: bestScore}
	}
}

// prefetchMoveHint computes the AI folder suggestion for the currently selected
// thread and returns a moveHintMsg so the status bar can show the destination
// before the user confirms the move.
func (a *App) prefetchMoveHint() tea.Cmd {
	if a.embClient == nil {
		return nil
	}
	threads := a.inboxView.SelectedThreads()
	if len(threads) == 0 {
		t := a.inboxView.SelectedThread()
		if t == nil {
			return nil
		}
		threads = []*data.Thread{t}
	}
	msg := threads[0].Latest()
	if msg == nil {
		return nil
	}
	account := a.activeAccount
	folder := a.activeFolder
	msgID := msg.ID
	uid := msg.UID

	return func() tea.Msg {
		model := a.cfg.Embeddings.Model
		if model == "" {
			model = "openai/text-embedding-3-small"
		}
		vec, norm, err := a.store.GetEmbedding(msgID, model)
		if err != nil || vec == nil || norm == 0 {
			return moveHintMsg{}
		}
		_ = uid
		folders, vectors, norms, _, err := a.store.ListFolderEmbeddings(account, model)
		if err != nil {
			return moveHintMsg{}
		}
		bestScore := float32(0)
		bestFolder := ""
		for i, f := range folders {
			if f == nil {
				continue
			}
			if !f.IsSelectable() {
				continue
			}
			if f.Name == folder {
				continue
			}
			if isSystemFolder(f.DisplayName, f.Name) {
				continue
			}
			score := embeddings.CosineSimilarity(vec, norm, vectors[i], norms[i])
			if score > bestScore {
				bestScore = score
				bestFolder = f.Name
			}
		}
		threshold := float32(a.cfg.Embeddings.AutoMoveThreshold)
		if threshold <= 0 {
			threshold = 0.27
		}
		if bestFolder == "" || bestScore < threshold {
			return moveHintMsg{}
		}
		return moveHintMsg{dest: bestFolder}
	}
}

func isSystemFolder(displayName, name string) bool {
	label := strings.ToLower(strings.TrimSpace(displayName))
	full := strings.ToLower(strings.TrimSpace(name))
	if full == "inbox" {
		return true
	}
	system := []string{"inbox", "trash", "deleted", "deleted items", "deleted messages", "bin", "sent", "sent items", "sent mail", "drafts", "archive", "all mail", "junk", "spam"}
	for _, s := range system {
		if label == s || full == s {
			return true
		}
	}
	return false
}

func (a *App) closeQuickMenu() {
	if a.quickMenu == nil {
		return
	}
	a.quickMenu.side = quickMenuNone
	a.quickMenu.step = 0
	a.statusbar.SetMoveHint("")
	a.applyQuickMenuState()
}

func (a *App) openQuickMenu(side quickMenuSide) tea.Cmd {
	if a.quickMenu == nil {
		a.quickMenu = &quickMenuState{}
	}
	if a.quickMenu.side != side {
		// Switching sides — reset to step 0
		a.quickMenu.side = side
		a.quickMenu.step = 0
		a.applyQuickMenuState()
		return nil
	}
	// Same side — advance step. If already on the last step, execute it.
	const maxSteps = 2
	if a.quickMenu.step >= maxSteps-1 {
		return a.handleQuickMenuEnter()
	}
	a.quickMenu.step++
	a.applyQuickMenuState()
	// Prefetch the move suggestion when landing on the move step (left side, step 1).
	if a.quickMenu.side == quickMenuLeft && a.quickMenu.step == 1 {
		return a.prefetchMoveHint()
	}
	return nil
}

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
	a.folderPicker.Open(folders, a.activeFolder)
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
		// First pass: look for \Trash attribute
		for _, f := range acct.Folders {
			for _, attr := range f.Attributes {
				if attr == `\Trash` {
					return f.Name
				}
			}
		}
		// Second pass: common names
		for _, f := range acct.Folders {
			switch f.DisplayName {
			case "Trash", "Deleted", "Deleted Items", "Deleted Messages", "Bin":
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
			for _, attr := range f.Attributes {
				if attr == `\Sent` {
					return f.Name
				}
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
			for _, attr := range f.Attributes {
				if attr == `\Drafts` || attr == `\Draft` {
					return f.Name
				}
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

func (a *App) archiveMessage() tea.Cmd {
	a.flash(fmt.Sprintf("%s Archive not yet implemented", icons.Archive), "info")
	return nil
}

func (a *App) doLocalSearch(q string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := a.store.SearchLocalWithFilters(q)
		if err != nil {
			return imaplib.SearchResultMsg{Err: err}
		}
		items := make([]embeddings.TopKItem, 0, len(msgs))
		for i, msg := range msgs {
			if msg == nil {
				continue
			}
			var score float32
			if len(msgs) > 1 {
				score = 1 - float32(i)/float32(len(msgs)-1)
			} else {
				score = 1
			}
			items = append(items, embeddings.TopKItem{Message: msg, Score: score})
		}
		if a.searchState != nil {
			a.searchState.semItems = items
		}
		return imaplib.SearchResultMsg{Messages: msgs}
	}
}

func (a *App) doStreamingSearch(q string, seq int) tea.Cmd {
	return func() tea.Msg {
		query := strings.TrimSpace(q)
		if query == "" {
			return embeddings.StreamSearchMsg{Seq: seq, Loading: false}
		}

		semItems := make([]embeddings.TopKItem, 0)
		semMsgs, semErr := a.store.SearchLocalWithFilters(query)
		if semErr == nil {
			semItems = make([]embeddings.TopKItem, 0, len(semMsgs))
			for i, msg := range semMsgs {
				if msg == nil {
					continue
				}
				var score float32
				if len(semMsgs) > 1 {
					score = 1 - float32(i)/float32(len(semMsgs)-1)
				} else {
					score = 1
				}
				semItems = append(semItems, embeddings.TopKItem{Message: msg, Score: score})
			}
		}

		if a.embClient == nil {
			if semErr != nil {
				return embeddings.StreamSearchMsg{Seq: seq, Err: semErr}
			}
			return embeddings.StreamSearchMsg{Seq: seq, Results: mergeSearchHits(semItems, nil), Loading: false}
		}
		ctx := context.Background()
		vecs, err := a.embClient.EmbedTexts(ctx, []string{query})
		if err != nil || len(vecs) == 0 {
			if semErr != nil {
				return embeddings.StreamSearchMsg{Seq: seq, Err: err}
			}
			return embeddings.StreamSearchMsg{Seq: seq, Results: mergeSearchHits(semItems, nil), Loading: false}
		}
		queryVec := vecs[0]
		queryNorm := embeddings.VectorNorm(queryVec)

		maxCandidates := a.cfg.Embeddings.MaxCandidates
		if maxCandidates <= 0 {
			maxCandidates = 5000
		}
		msgs, vectors, norms, err := a.store.ListEmbeddingCandidates(a.activeAccount, a.cfg.Embeddings.Model, maxCandidates)
		if err != nil || len(msgs) == 0 {
			if semErr != nil {
				return embeddings.StreamSearchMsg{Seq: seq, Err: err}
			}
			return embeddings.StreamSearchMsg{Seq: seq, Results: mergeSearchHits(semItems, nil), Loading: false}
		}
		kSim := a.cfg.Embeddings.TopSimilar
		if kSim <= 0 {
			kSim = 30
		}
		simItems := embeddings.TopK(msgs, vectors, norms, queryVec, queryNorm, kSim)
		results := mergeSearchHits(semItems, simItems)
		return embeddings.StreamSearchMsg{Seq: seq, Results: results, Loading: false}
	}
}

func (a *App) searchStreamTick(seq int) tea.Cmd {
	return func() tea.Msg {
		if a.searchState == nil || a.searchState.seq != seq {
			return nil
		}
		return a.searchStreamNext()()
	}
}

func (a *App) searchStreamNext() tea.Cmd {
	return func() tea.Msg {
		state := a.searchState
		if state == nil {
			return nil
		}
		batch := a.cfg.Embeddings.StreamBatch
		if batch <= 0 {
			batch = 128
		}
		total := len(state.msgs)
		start := state.nextIndex
		if start >= total {
			return embeddings.StreamSearchMsg{Seq: state.seq, Results: state.results, Loading: false}
		}
		end := start + batch
		if end > total {
			end = total
		}
		for i := start; i < end; i++ {
			score := embeddings.CosineSimilarity(state.queryVec, state.queryNorm, state.vectors[i], state.norms[i])
			state.simHeap.Add(state.msgs[i], score)
		}
		sem := state.semItems
		sim := state.simHeap.ItemsSorted()
		state.results = mergeSearchHits(sem, sim)
		state.nextIndex = end
		loading := end < total
		return embeddings.StreamSearchMsg{Seq: state.seq, Results: state.results, Loading: loading}
	}
}

func mergeSearchHits(sem []embeddings.TopKItem, sim []embeddings.TopKItem) []*embeddings.SearchHit {
	denomSem := float32(0)
	if len(sem) > 1 {
		denomSem = float32(len(sem) - 1)
	}
	denomSim := float32(0)
	if len(sim) > 1 {
		denomSim = float32(len(sim) - 1)
	}

	byID := make(map[int64]*embeddings.SearchHit)
	for i, item := range sem {
		if item.Message == nil {
			continue
		}
		score := float32(1)
		if denomSem > 0 {
			score = 1 - float32(i)/denomSem
		}
		entry, ok := byID[item.Message.ID]
		if !ok {
			entry = &embeddings.SearchHit{Message: item.Message}
			byID[item.Message.ID] = entry
		}
		entry.Semantic = true
		entry.Score += score
	}
	for i, item := range sim {
		if item.Message == nil {
			continue
		}
		score := float32(1)
		if denomSim > 0 {
			score = 1 - float32(i)/denomSim
		}
		entry, ok := byID[item.Message.ID]
		if !ok {
			entry = &embeddings.SearchHit{Message: item.Message}
			byID[item.Message.ID] = entry
		}
		entry.Similar = true
		entry.Score += score
	}

	merged := make([]*embeddings.SearchHit, 0, len(byID))
	for _, entry := range byID {
		merged = append(merged, entry)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Score == merged[j].Score {
			return merged[i].Message.Date.After(merged[j].Message.Date)
		}
		return merged[i].Score > merged[j].Score
	})

	withBoth := merged[:0]
	onlySem := make([]*embeddings.SearchHit, 0, len(merged))
	onlySim := make([]*embeddings.SearchHit, 0, len(merged))
	for _, entry := range merged {
		if entry.Semantic && entry.Similar {
			withBoth = append(withBoth, entry)
		} else if entry.Semantic {
			onlySem = append(onlySem, entry)
		} else if entry.Similar {
			onlySim = append(onlySim, entry)
		}
	}
	return append(append(withBoth, onlySem...), onlySim...)
}

func convertSearchResults(results []*embeddings.SearchHit) []*SearchResult {
	converted := make([]*SearchResult, 0, len(results))
	for _, res := range results {
		converted = append(converted, &SearchResult{
			Message:  res.Message,
			Score:    res.Score,
			Semantic: res.Semantic,
			Similar:  res.Similar,
		})
	}
	return converted
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

func (a *App) handleComposerResult(r *composer.Result) tea.Cmd {
	switch r.Action {
	case "send":
		if r.Draft == nil {
			return nil
		}
		for i := range a.cfg.Accounts {
			if a.cfg.Accounts[i].Name == a.activeAccount {
				acfg := &a.cfg.Accounts[i]
				a.flash(fmt.Sprintf("%s Sending…", icons.Send), "info")
				return outsmtp.SendMessage(acfg, r.Draft)
			}
		}
		a.flash("No account configured for sending", "err")
	case "save-draft":
		if r.Draft == nil {
			return nil
		}
		if drafts := a.findDraftsFolder(a.activeAccount); drafts != "" {
			if client, ok := a.imapClients[a.activeAccount]; ok {
				if raw, err := outsmtp.BuildRawMessage(r.Draft); err == nil {
					return client.AppendMessage(drafts, raw, []data.Flag{data.FlagDraft, data.FlagSeen})
				}
			}
		}
	case "cancel":
		// nothing
	}
	return nil
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

func (a *App) copySuggestedEvent() tea.Cmd {
	ev := a.readerView.SuggestedEvent()
	if ev == nil || !ev.HasEvent {
		return nil
	}
	action := a.readerView.FocusedEventAction()
	if action == "reject" {
		return a.rejectSuggestedEvent()
	}
	content := ev.PlainText
	label := "Copied suggested event"
	if action == "json" {
		content = a.readerView.SuggestedEventJSON()
		label = "Copied suggested event JSON"
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if err := util.CopyToClipboard(content); err != nil {
		return a.flash("Copy failed: "+err.Error(), "err")
	}
	return a.flash(label, "ok")
}

func (a *App) rejectSuggestedEvent() tea.Cmd {
	ev := a.readerView.SuggestedEvent()
	if ev == nil {
		return nil
	}
	if err := a.store.RejectSuggestedEvent(ev.MessageID); err != nil {
		return a.flash("Reject failed: "+err.Error(), "err")
	}
	// Hide the suggestion in the current view.
	rejected := *ev
	rejected.Rejected = true
	rejected.HasEvent = false
	a.readerView.SetSuggestedEvent(&rejected)
	return a.flash("Event suggestion rejected", "ok")
}

func (a *App) executeAttachmentAction() tea.Cmd {
	msg := a.readerView.AttachmentSource()
	if msg == nil {
		return nil
	}
	idx := a.readerView.FocusedAttachmentIndex()
	if idx < 0 || idx >= len(msg.Attachments) {
		return nil
	}
	att := msg.Attachments[idx]
	switch a.readerView.FocusedAttachmentAction() {
	case "open":
		return openAttachmentDefaultCmd(att)
	case "download":
		return downloadAttachmentCmd(att)
	case "editor":
		return openAttachmentEditorCmd(att)
	}
	return nil
}

// attachActionResultMsg is returned when an attachment action completes.
type attachActionResultMsg struct {
	action string
	path   string
	err    error
}

func openAttachmentDefaultCmd(att data.Attachment) tea.Cmd {
	return func() tea.Msg {
		path, err := writeAttachmentTemp(att)
		if err != nil {
			return attachActionResultMsg{action: "open", err: err}
		}
		if err := exec.Command("xdg-open", path).Start(); err != nil {
			return attachActionResultMsg{action: "open", err: err}
		}
		return attachActionResultMsg{action: "open", path: path}
	}
}

func downloadAttachmentCmd(att data.Attachment) tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		dest := filepath.Join(home, "Downloads", att.Filename)
		if err := os.WriteFile(dest, att.Data, 0644); err != nil {
			return attachActionResultMsg{action: "download", err: err}
		}
		return attachActionResultMsg{action: "download", path: dest}
	}
}

func openAttachmentEditorCmd(att data.Attachment) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	path, err := writeAttachmentTemp(att)
	if err != nil {
		return func() tea.Msg { return attachActionResultMsg{action: "editor", err: err} }
	}
	return tea.ExecProcess(exec.Command(editor, path), func(err error) tea.Msg {
		return attachActionResultMsg{action: "editor", path: path, err: err}
	})
}

func writeAttachmentTemp(att data.Attachment) (string, error) {
	ext := filepath.Ext(att.Filename)
	f, err := os.CreateTemp("", "bubblmail-*"+ext)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(att.Data); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// --- layout ---

const sidebarWidth = 26 // content width
const sidebarBorderWidth = 1

func sidebarRenderedWidth() int {
	return sidebarWidth + sidebarBorderWidth
}

func (a *App) headerHeight() int {
	return a.header.Height()
}

func (a *App) currentSbContext() string {
	ctx := "inbox"
	switch a.viewID {
	case ViewReader:
		ctx = "reader"
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

	// Header. Counts are pushed here rather than in updateLayout because they
	// change with every sync, not with every resize.
	unread, total := a.inboxView.Counts()
	a.header.SetCounts(unread, total)
	if a.viewID == ViewSmartFolder {
		a.header.SetFilterLabel(a.activeFolder)
	} else {
		a.header.SetFilterLabel("")
	}
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
		lines := a.updateLineBuffers(output)
		if a.drag != nil && a.drag.isDrag && a.drag.canCopy {
			output = applySelectionHighlight(lines,
				a.drag.startX, a.drag.startY, a.drag.endX, a.drag.endY,
				a.drag.zoneX0, a.drag.zoneY0, a.drag.zoneX1, a.drag.zoneY1)
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
	if a.showHelp {
		mainContent = a.helpOverlay.View(a.width, contentH)
	}

	statusbar := a.statusbar.View(a.currentSbContext())

	output := lipgloss.JoinVertical(lipgloss.Left, header, mainContent, statusbar)

	// Update line buffers for drag-selection text extraction.
	lines := a.updateLineBuffers(output)

	// Apply visual selection highlight during an active drag (reader only).
	if a.drag != nil && a.drag.isDrag && a.drag.canCopy {
		output = applySelectionHighlight(lines,
			a.drag.startX, a.drag.startY, a.drag.endX, a.drag.endY,
			a.drag.zoneX0, a.drag.zoneY0, a.drag.zoneX1, a.drag.zoneY1)
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
	n := a.inboxView.Len()
	cursor := a.inboxView.CursorPos()
	if n == 0 || cursor < n-10 {
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

func (a *App) flash(msg, kind string) tea.Cmd {
	a.statusbar.SetMessage(msg, kind)
	a.updateLayout() // flash message adds a line; recalculate content height
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

// updateLineBuffers splits the rendered output into per-line buffers used for
// text selection: ansiLines keeps ANSI codes (for markdown copy), plainLines
// strips them (for plain-text copy). Returns the split lines slice so callers
// can reuse it without re-splitting.
func (a *App) updateLineBuffers(output string) []string {
	lines := strings.Split(output, "\n")
	a.ansiLines = lines
	a.plainLines = make([]string, len(lines))
	for i, l := range lines {
		a.plainLines[i] = ansi.Strip(l)
	}
	return lines
}

// ansiToMarkdown converts an ANSI-escaped string to Markdown, mapping bold
// (SGR 1/22) to **…** and italic (SGR 3/23) to _…_. All other escape
// sequences (colors, underline, etc.) are stripped.
func ansiToMarkdown(s string) string {
	var b strings.Builder
	inBold, inItalic := false, false
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Scan to the final byte of the escape sequence (a letter @–~).
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			if j < len(s) && s[j] == 'm' { // SGR only
				for _, param := range strings.Split(s[i+2:j], ";") {
					switch param {
					case "0", "": // reset
						if inItalic {
							b.WriteByte('_')
							inItalic = false
						}
						if inBold {
							b.WriteString("**")
							inBold = false
						}
					case "1": // bold on
						if !inBold {
							b.WriteString("**")
							inBold = true
						}
					case "22": // bold off
						if inBold {
							b.WriteString("**")
							inBold = false
						}
					case "3": // italic on
						if !inItalic {
							b.WriteByte('_')
							inItalic = true
						}
					case "23": // italic off
						if inItalic {
							b.WriteByte('_')
							inItalic = false
						}
					}
				}
			}
			if j < len(s) {
				i = j + 1
			} else {
				i++
			}
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	if inItalic {
		b.WriteByte('_')
	}
	if inBold {
		b.WriteString("**")
	}
	return b.String()
}

// normalizeSelection returns (startRow, startCol, endRow, endCol) with the start
// guaranteed to come before the end in reading order (top-left → bottom-right).
func normalizeSelection(startX, startY, endX, endY int) (sr, sc, er, ec int) {
	if startY < endY || (startY == endY && startX <= endX) {
		return startY, startX, endY, endX
	}
	return endY, endX, startY, startX
}

// extractSelectedText returns the text covered by the drag from (startX, startY)
// to (endX, endY), clamped to the drag's zone bounds.
// asMarkdown=true uses the ANSI-rich line buffer and converts bold/italic to
// Markdown syntax; asMarkdown=false returns raw stripped plain text.
func (a *App) extractSelectedText(startX, startY, endX, endY int, asMarkdown bool) string {
	if a.drag == nil {
		return ""
	}
	if asMarkdown && len(a.ansiLines) == 0 {
		return ""
	}
	if !asMarkdown && len(a.plainLines) == 0 {
		return ""
	}

	zx0 := a.drag.zoneX0
	zx1 := a.drag.zoneX1
	zy0 := a.drag.zoneY0
	zy1 := a.drag.zoneY1

	sr, sc, er, ec := normalizeSelection(startX, startY, endX, endY)
	if sr == er && sc == ec {
		return ""
	}

	// Clamp row range to zone.
	if sr < zy0 {
		sr, sc = zy0, zx0
	}
	if er > zy1 {
		er, ec = zy1, zx1+1
	}

	nLines := len(a.plainLines)
	if asMarkdown {
		nLines = len(a.ansiLines)
	}

	var parts []string
	for r := sr; r <= er && r < nLines; r++ {
		// Column range for this row, clamped to zone x bounds.
		c0 := zx0
		c1 := zx1 + 1
		if r == sr && sc > c0 {
			c0 = sc
		}
		if r == er && ec < c1 {
			c1 = ec
		}

		var chunk string
		if asMarkdown {
			chunk = ansiToMarkdown(ansi.Cut(a.ansiLines[r], c0, c1))
		} else {
			runes := []rune(a.plainLines[r])
			if c0 > len(runes) {
				c0 = len(runes)
			}
			if c1 > len(runes) {
				c1 = len(runes)
			}
			if c0 < c1 {
				chunk = string(runes[c0:c1])
			}
		}
		parts = append(parts, strings.TrimRight(chunk, " \t"))
	}

	return strings.TrimRight(strings.Join(parts, "\n"), "\n")
}

// applySelectionHighlight overlays a reverse-video highlight on the selected
// cells of the already-rendered (ANSI-escaped) lines slice, then re-joins them.
// The selection is clamped to [zx0,zx1] × [zy0,zy1] so it cannot bleed into
// adjacent UI zones.
func applySelectionHighlight(lines []string, startX, startY, endX, endY int,
	zx0, zy0, zx1, zy1 int) string {
	sr, sc, er, ec := normalizeSelection(startX, startY, endX, endY)
	selStyle := lipgloss.NewStyle().Reverse(true)

	// Clamp row range to zone.
	if sr < zy0 {
		sr, sc = zy0, zx0
	}
	if er > zy1 {
		er, ec = zy1, zx1+1
	}

	result := make([]string, len(lines))
	copy(result, lines)

	for r := sr; r <= er && r < len(lines); r++ {
		// Column range for this row, clamped to zone x bounds.
		c0 := zx0
		c1 := zx1 + 1
		if r == sr && sc > c0 {
			c0 = sc
		}
		if r == er && ec < c1 {
			c1 = ec
		}
		if c0 >= c1 {
			continue
		}
		result[r] = selectionHighlightLine(lines[r], c0, c1, selStyle)
	}

	return strings.Join(result, "\n")
}

// selectionHighlightLine applies highlight to columns [c0, c1) of a single
// ANSI-escaped line, preserving original styling outside the selection.
func selectionHighlightLine(line string, c0, c1 int, style lipgloss.Style) string {
	// Clamp c1 to the visual width of actual content (no trailing-space highlight).
	plain := ansi.Strip(line)
	contentEnd := lipgloss.Width(strings.TrimRight(plain, " "))
	if c1 > contentEnd {
		c1 = contentEnd
	}
	if c0 >= c1 {
		return line
	}
	prefix := ansi.Truncate(line, c0, "")
	middle := ansi.Strip(ansi.Cut(line, c0, c1))
	suffix := ansi.TruncateLeft(line, c1, "")
	return prefix + style.Render(middle) + suffix
}

func removeByUID(msgs []*data.Message, uid uint32) []*data.Message {
	result := msgs[:0]
	for _, m := range msgs {
		if m.UID != uid {
			result = append(result, m)
		}
	}
	return result
}

func toggleFlag(flags []data.Flag, f data.Flag, add bool) []data.Flag {
	var result []data.Flag
	for _, existing := range flags {
		if existing != f {
			result = append(result, existing)
		}
	}
	if add {
		result = append(result, f)
	}
	return result
}

// fetchSmartFolder loads messages for a smart folder category into the inbox view.
func (a *App) fetchSmartFolder(category string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := a.store.GetMessagesByCategory(a.activeAccount, category)
		if err != nil {
			return smartFolderMsg{category: category, err: err}
		}
		return smartFolderMsg{category: category, messages: msgs}
	}
}

// smartFolderMsg delivers messages for a smart folder.
type smartFolderMsg struct {
	category string
	messages []*data.Message
	err      error
}

// refreshSmartCounts reloads category counts from the database for the active account.
func (a *App) refreshSmartCounts() {
	counts, err := a.store.GetCategoryCounts(a.activeAccount)
	if err == nil {
		a.smartCounts = counts
		a.sidebar.SetSmartCounts(a.smartCounts, a.cfg.Classification.Categories)
	}
}

func (a *App) debugLog(format string, args ...any) {
	if a.debugFile == nil {
		f, err := os.OpenFile("/tmp/bubblmail-debug.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		a.debugFile = f
	}
	fmt.Fprintf(a.debugFile, format+"\n", args...)
}
