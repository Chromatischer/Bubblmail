package ui

import (
	"os"
	"time"

	"github.com/bubblmail/bubblmail/cache"
	classifylib "github.com/bubblmail/bubblmail/classify"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/embeddings"
	imaplib "github.com/bubblmail/bubblmail/imap"
	"github.com/bubblmail/bubblmail/suggest"
	"github.com/bubblmail/bubblmail/ui/composer"
	"github.com/bubblmail/bubblmail/ui/views"
	tea "github.com/charmbracelet/bubbletea"
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

// clearStatusMsg clears the flash message. seq identifies which flash generation
// this timer belongs to, so a stale timer cannot clear a newer message.
type clearStatusMsg struct{ seq int }

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
	styles    *Styles
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
	imapClients map[string]mailClient

	// UI components
	header         *Header
	sidebar        *Sidebar
	statusbar      *StatusBar
	helpOverlay    *HelpOverlay
	searchOverlay  *SearchOverlay
	folderPicker   *FolderPickerOverlay
	newFolder      *NewFolderOverlay
	commandPalette *CommandPaletteOverlay

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
	showHelp       bool
	sidebarFocused bool
	activeAccount  string
	activeFolder   string

	// Pagination
	loadedMessages []*data.Message
	fetchedCount   int
	loadingMore    bool
	allLoaded      bool
	unreadOnly     bool
	quitPending    bool
	searchSeq      int
	searchState    *searchState
	prefetchSkip   map[int64]bool
	quickMenu      *quickMenuState
	lastUndo       *undoMove // last reversible move/delete/archive (nil if none)
	persisted      *uiState  // on-disk UI state (collapsed folders, first-run tip)

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
		!a.commandPalette.IsActive() &&
		!a.showHelp &&
		!a.comp.IsActive()
}

// NewApp creates the root application model.
func NewApp(cfg *config.Config, store *cache.Store) *App {
	theme := config.NewTheme(cfg)
	styles := NewStyles(theme)
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
		styles:          styles,
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
		imapClients:     make(map[string]mailClient),
		header:          NewHeader(styles),
		sidebar:         NewSidebar(styles),
		statusbar:       NewStatusBar(styles),
		helpOverlay:     NewHelpOverlay(styles),
		showSidebar:     true,
		wantSidebar:     true,
		viewID:          ViewInbox,
		prefetchSkip:    make(map[int64]bool),
		quickMenu:       &quickMenuState{side: quickMenuNone},
	}

	app.searchOverlay = NewSearchOverlay(styles)
	app.folderPicker = NewFolderPickerOverlay(styles)
	app.newFolder = NewNewFolderOverlay(styles)
	app.commandPalette = NewCommandPaletteOverlay(styles)
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

	if state, err := loadUIState(); err == nil {
		app.persisted = state
	}
	if app.persisted == nil {
		app.persisted = &uiState{}
	}
	app.sidebar.SetCollapsed(app.persisted.CollapsedFolders)

	return app
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.SetWindowTitle("bubblmail"),
	}
	// One-time onboarding hint, shown a beat after the first render settles.
	if a.persisted != nil && !a.persisted.TipSeen {
		cmds = append(cmds, tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg {
			return firstRunTipMsg{}
		}))
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
		cmds = append(cmds, a.waitForClassifyResult())
	}
	if a.suggestQueue != nil {
		cmds = append(cmds, a.waitForSuggestResult())
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

	// Populate sidebar from cache immediately so it is visible before IMAP connects.
	for i := range a.cfg.Accounts {
		acfg := &a.cfg.Accounts[i]
		if cached, err := a.store.GetFolders(acfg.Name); err == nil && len(cached) > 0 {
			a.sidebar.SetFolders(acfg.Name, cached)
		}
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
	client  mailClient
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

// waitForClassifyResult blocks on the classification results channel and wakes
// the Update loop only when a result actually arrives — no idle polling.
func (a *App) waitForClassifyResult() tea.Cmd {
	return func() tea.Msg {
		r, ok := <-a.classifyResults
		return classifyResultReadyMsg{result: r, ok: ok}
	}
}

// waitForSuggestResult blocks on the suggested-event results channel, waking the
// Update loop only when a result arrives.
func (a *App) waitForSuggestResult() tea.Cmd {
	return func() tea.Msg {
		r, ok := <-a.suggestResults
		return suggestResultReadyMsg{result: r, ok: ok}
	}
}

// classifyResultReadyMsg delivers one classification result from the channel.
type classifyResultReadyMsg struct {
	result classifylib.ResultMsg
	ok     bool // false once the channel is closed
}

// suggestResultReadyMsg delivers one suggested-event result from the channel.
type suggestResultReadyMsg struct {
	result suggest.ResultMsg
	ok     bool
}

// applyClassifyResult folds one classification result into the smart-folder
// counts, reporting whether it changed anything.
func (a *App) applyClassifyResult(r classifylib.ResultMsg) bool {
	if r.Err == nil && r.Category != "" {
		a.smartCounts[r.Category]++
		return true
	}
	return false
}

// applySuggestResult delivers one suggested-event result to the reader if it is
// still showing the matching message.
func (a *App) applySuggestResult(r suggest.ResultMsg) {
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
