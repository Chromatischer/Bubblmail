package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	imaplib "github.com/bubblmail/bubblmail/imap"
	outsmtp "github.com/bubblmail/bubblmail/smtp"
	"github.com/bubblmail/bubblmail/thread"
	"github.com/bubblmail/bubblmail/ui/composer"
	"github.com/bubblmail/bubblmail/ui/views"
)

// ViewID identifies which main pane is active.
type ViewID int

const (
	ViewInbox  ViewID = iota
	ViewReader         // reading a single message
	ViewFolder         // folder browser
)

// syncMsg triggers a background sync.
type syncMsg struct{ account string }

// spinnerTickMsg advances the loading spinner.
type spinnerTickMsg struct{}

// clearStatusMsg clears the flash message.
type clearStatusMsg struct{}

// App is the root Bubble Tea model.
type App struct {
	cfg    *config.Config
	theme  *config.Theme
	styles *Styles
	store  *cache.Store

	// IMAP clients, one per account
	imapClients map[string]*imaplib.Client

	// UI components
	header        *Header
	sidebar       *Sidebar
	statusbar     *StatusBar
	helpOverlay   *HelpOverlay
	searchOverlay *SearchOverlay

	// Main views
	viewID     ViewID
	inboxView  *views.InboxView
	readerView *views.ReaderView
	folderView *views.FolderView

	// Overlay
	comp *composer.Composer

	// State
	width         int
	height        int
	showSidebar   bool
	wantSidebar   bool
	showHelp      bool
	activeAccount string
	activeFolder  string

	// Pagination
	loadedMessages []*data.Message
	fetchedCount   int
	loadingMore    bool
	allLoaded      bool

	accounts []*data.Account
}

// NewApp creates the root application model.
func NewApp(cfg *config.Config, store *cache.Store) *App {
	theme := config.NewTheme(cfg)
	styles := NewStyles(theme)

	app := &App{
		cfg:         cfg,
		theme:       theme,
		styles:      styles,
		store:       store,
		imapClients: make(map[string]*imaplib.Client),
		header:      NewHeader(styles),
		sidebar:     NewSidebar(styles),
		statusbar:   NewStatusBar(styles),
		helpOverlay: NewHelpOverlay(styles),
		showSidebar: true,
		wantSidebar: true,
		viewID:      ViewInbox,
	}

	app.searchOverlay = NewSearchOverlay(styles)
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

	return app
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.SetWindowTitle("bubblmail"),
	}

	if len(a.cfg.Accounts) == 0 {
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
			msg.client.FetchMessages(a.activeFolder, a.cfg.General.PageSize),
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
		a.header.SetSyncState("synced")
		return a, nil

	case imaplib.MessageListMsg:
		if msg.Err != nil {
			a.flash("Fetch error: "+msg.Err.Error(), "err")
			a.statusbar.SetLoading(false)
			return a, nil
		}
		a.loadedMessages = msg.Messages
		a.fetchedCount = len(msg.Messages)
		a.loadingMore = false
		a.allLoaded = false
		if err := a.store.UpsertMessages(msg.Messages); err != nil {
			a.flash("Cache write error: "+err.Error(), "err")
		}
		threads := thread.BuildThreads(a.loadedMessages)
		a.inboxView.SetThreads(threads)
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
		return a, nil

	case imaplib.MessageBodyMsg:
		if msg.Err != nil {
			a.flash("Body fetch error: "+msg.Err.Error(), "err")
			return a, nil
		}
		// Store body in cache
		if msg.MsgID > 0 {
			if err := a.store.UpsertBody(msg.MsgID, msg.Text, msg.HTML); err != nil {
				a.flash("Cache error: "+err.Error(), "err")
			}
		}
		// Update reader view if still showing same message
		if v := a.readerView; v != nil {
			if cur := a.currentMessage(); cur != nil && cur.UID == msg.UID {
				cur.Body = msg.Text
				cur.HTMLBody = msg.HTML
				v.SetMessage(cur)
			}
		}
		return a, nil

	case imaplib.SetFlagResultMsg:
		if msg.Err != nil {
			a.flash("Flag error: "+msg.Err.Error(), "err")
		}
		return a, nil

	case imaplib.SearchResultMsg:
		if msg.Err != nil {
			a.flash("Search error: "+msg.Err.Error(), "err")
			return a, nil
		}
		a.searchOverlay.SetResults(msg.Messages)
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
		if a.statusbar.loading {
			return a, spinnerTick()
		}
		return a, nil

	case clearStatusMsg:
		a.statusbar.ClearMessage()
		return a, nil

	case tea.MouseMsg:
		return a.handleMouse(msg)

	case tea.KeyMsg:
		return a.handleKey(msg)
	}

	return a, nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	const minW, minH = 80, 24
	if a.width < minW || a.height < minH {
		if key == "q" || key == "ctrl+c" {
			return a, tea.Quit
		}
		return a, nil
	}

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

	// 2. Search overlay
	if a.searchOverlay.IsActive() {
		qChanged, closed, selected := a.searchOverlay.HandleKey(key)
		if closed {
			if selected {
				if msg := a.searchOverlay.SelectedMessage(); msg != nil {
					return a, a.openMessage(msg)
				}
			}
			a.searchOverlay.Close()
		} else if qChanged {
			q := a.searchOverlay.Query()
			if q != "" {
				return a, a.doLocalSearch(q)
			}
		}
		return a, nil
	}

	// 3. Help overlay — any key closes it
	if a.showHelp {
		a.showHelp = false
		return a, nil
	}

	// 4. Global keys
	switch key {
	case "q", "ctrl+c":
		return a, tea.Quit

	case "?":
		a.showHelp = true

	case "b":
		a.wantSidebar = !a.wantSidebar
		a.updateLayout()

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

	case "tab":
		a.cycleAccount(1)

	case "shift+tab":
		a.cycleAccount(-1)

	case "/":
		a.searchOverlay.SetSize(a.width, a.height-3)
		a.searchOverlay.Open()

	case "ctrl+f":
		// IMAP server search
		client, ok := a.imapClients[a.activeAccount]
		if ok {
			a.flash("Searching server…", "info")
			q := a.searchOverlay.Query()
			return a, client.SearchIMAP(a.activeFolder, q)
		}

	// View-specific navigation
	case "j", "down":
		a.moveDown()
		return a, a.maybeLoadMore()

	case "k", "up":
		a.moveUp()

	case "ctrl+d":
		a.pageDown()
		return a, a.maybeLoadMore()

	case "ctrl+u":
		a.pageUp()

	case "g":
		a.goToTop()

	case "G":
		a.goToBottom()
		return a, a.maybeLoadMore()

	case "enter":
		return a, a.handleEnter()

	case "esc", "h", "left":
		if a.viewID == ViewReader || a.viewID == ViewFolder {
			a.viewID = ViewInbox
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

	case "e":
		return a, a.archiveMessage()
	}

	return a, nil
}

func (a *App) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
		// Sidebar click
		if a.showSidebar && msg.X < sidebarRenderedWidth() {
			contentY := msg.Y - a.headerHeight()
			acct, folder, ok := a.sidebar.HitTest(msg.X, contentY)
			if ok && folder != "" {
				a.activeAccount = acct
				a.activeFolder = folder
				a.sidebar.SetActive(acct, folder)
				a.header.SetAccount(acct)
				a.header.SetFolder(folder)
				a.viewID = ViewInbox
				return a, a.fetchMessages()
			}
		}
	}
	return a, nil
}

// --- navigation helpers ---

func (a *App) moveUp() {
	switch a.viewID {
	case ViewInbox:
		a.inboxView.MoveUp()
	case ViewReader:
		a.readerView.MoveUp()
	case ViewFolder:
		a.folderView.MoveUp()
	}
}

func (a *App) moveDown() {
	switch a.viewID {
	case ViewInbox:
		a.inboxView.MoveDown()
	case ViewReader:
		a.readerView.MoveDown()
	case ViewFolder:
		a.folderView.MoveDown()
	}
}

func (a *App) pageUp() {
	switch a.viewID {
	case ViewInbox:
		a.inboxView.PageUp()
	case ViewReader:
		a.readerView.PageUp()
	case ViewFolder:
		a.folderView.PageUp()
	}
}

func (a *App) pageDown() {
	switch a.viewID {
	case ViewInbox:
		a.inboxView.PageDown()
	case ViewReader:
		a.readerView.PageDown()
	case ViewFolder:
		a.folderView.PageDown()
	}
}

func (a *App) goToTop() {
	switch a.viewID {
	case ViewInbox:
		a.inboxView.GoToTop()
	case ViewReader:
		a.readerView.GoToTop()
	case ViewFolder:
		a.folderView.GoToTop()
	}
}

func (a *App) goToBottom() {
	switch a.viewID {
	case ViewInbox:
		a.inboxView.GoToBottom()
	case ViewReader:
		a.readerView.GoToBottom()
	case ViewFolder:
		a.folderView.GoToBottom()
	}
}

func (a *App) handleEnter() tea.Cmd {
	switch a.viewID {
	case ViewInbox:
		t := a.inboxView.SelectedThread()
		if t != nil && t.Latest() != nil {
			return a.openMessage(t.Latest())
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
	a.readerView.SetMessage(msg)
	a.viewID = ViewReader

	// Mark as read
	var markCmd tea.Cmd
	if !msg.IsRead() {
		client, ok := a.imapClients[a.activeAccount]
		if ok {
			markCmd = client.SetFlag(a.activeFolder, msg.UID, data.FlagSeen, true)
		}
		_ = a.store.SetFlags(a.activeAccount, a.activeFolder, msg.UID,
			append(msg.Flags, data.FlagSeen))
		msg.Flags = append(msg.Flags, data.FlagSeen)
	}

	// Fetch body if not yet loaded
	var fetchCmd tea.Cmd
	if msg.Body == "" {
		client, ok := a.imapClients[a.activeAccount]
		if ok {
			fetchCmd = client.FetchBody(a.activeFolder, msg.UID, msg.ID)
		}
	}

	return tea.Batch(markCmd, fetchCmd)
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

func (a *App) openCompose() tea.Cmd {
	from := a.senderAddress()
	a.comp.OpenNew(from)
	a.comp.SetSize(a.width, a.height)
	return nil
}

func (a *App) openReply(msg *data.Message, replyAll bool) tea.Cmd {
	from := a.senderAddress()
	a.comp.OpenReply(from, msg, replyAll)
	a.comp.SetSize(a.width, a.height)
	return nil
}

func (a *App) openForward(msg *data.Message) tea.Cmd {
	from := a.senderAddress()
	a.comp.OpenForward(from, msg)
	a.comp.SetSize(a.width, a.height)
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
	msg := a.currentMessage()
	if msg == nil {
		return nil
	}
	starred := msg.IsStarred()
	_ = a.store.SetFlags(a.activeAccount, a.activeFolder, msg.UID, toggleFlag(msg.Flags, data.FlagFlagged, !starred))
	client, ok := a.imapClients[a.activeAccount]
	if ok {
		return client.SetFlag(a.activeFolder, msg.UID, data.FlagFlagged, !starred)
	}
	return nil
}

func (a *App) toggleRead() tea.Cmd {
	msg := a.currentMessage()
	if msg == nil {
		return nil
	}
	read := msg.IsRead()
	newFlags := toggleFlag(msg.Flags, data.FlagSeen, !read)
	_ = a.store.SetFlags(a.activeAccount, a.activeFolder, msg.UID, newFlags)
	msg.Flags = newFlags
	client, ok := a.imapClients[a.activeAccount]
	if ok {
		return client.SetFlag(a.activeFolder, msg.UID, data.FlagSeen, !read)
	}
	return nil
}

func (a *App) deleteMessage() tea.Cmd {
	msg := a.currentMessage()
	if msg == nil {
		return nil
	}
	client, ok := a.imapClients[a.activeAccount]
	if ok {
		a.flash("Moving to Trash…", "info")
		return client.SetFlag(a.activeFolder, msg.UID, data.FlagDeleted, true)
	}
	return nil
}

func (a *App) archiveMessage() tea.Cmd {
	a.flash("Archive not yet implemented", "info")
	return nil
}

func (a *App) doLocalSearch(q string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := a.store.SearchLocal(q)
		if err != nil {
			return imaplib.SearchResultMsg{Err: err}
		}
		return imaplib.SearchResultMsg{Messages: msgs}
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
				a.flash("Sending…", "info")
				return outsmtp.SendMessage(acfg, r.Draft)
			}
		}
		a.flash("No account configured for sending", "err")
	case "cancel":
		// nothing
	}
	return nil
}

func (a *App) currentMessage() *data.Message {
	switch a.viewID {
	case ViewInbox:
		t := a.inboxView.SelectedThread()
		if t != nil {
			return t.Latest()
		}
	case ViewReader:
		// readerView stores its own message; return via the inbox thread
		t := a.inboxView.SelectedThread()
		if t != nil {
			return t.Latest()
		}
	}
	return nil
}

func (a *App) cycleAccount(dir int) {
	if len(a.accounts) == 0 {
		return
	}
	idx := 0
	for i, acct := range a.accounts {
		if acct.Name == a.activeAccount {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(a.accounts)) % len(a.accounts)
	a.activeAccount = a.accounts[idx].Name
	a.activeFolder = "INBOX"
	a.sidebar.SetActive(a.activeAccount, a.activeFolder)
	a.header.SetAccount(a.activeAccount)
	a.header.SetFolder(a.activeFolder)
}

// --- layout ---

const sidebarWidth = 26 // content width
const sidebarBorderWidth = 1

func sidebarRenderedWidth() int {
	return sidebarWidth + sidebarBorderWidth
}

func (a *App) headerHeight() int {
	return 3 // row1 + row2 + divider
}

func (a *App) statusHeight() int {
	return 2 // divider + row
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
	contentH := a.height - a.headerHeight() - a.statusHeight()
	if contentH < 1 {
		contentH = 1
	}

	a.header.SetWidth(a.width)
	a.header.SetAccount(a.activeAccount)
	a.header.SetFolder(a.activeFolder)
	a.statusbar.SetWidth(a.width)
	a.sidebar.SetSize(sidebarWidth, contentH)

	a.inboxView.SetSize(contentW, contentH)
	a.readerView.SetSize(contentW, contentH)
	a.folderView.SetSize(contentW, contentH)
	a.comp.SetSize(a.width, a.height)
	a.searchOverlay.SetSize(a.width, contentH)
}

// View implements tea.Model.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Loading…"
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

	// Composer overlay takes full screen
	if a.comp.IsActive() {
		mainContent = a.comp.View()
		return lipgloss.JoinVertical(lipgloss.Left, header, mainContent, a.statusbar.View("composer"))
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
	if a.showHelp {
		mainContent = a.helpOverlay.View(a.width, contentH)
	}

	// Status bar context
	sbContext := "inbox"
	switch a.viewID {
	case ViewReader:
		sbContext = "reader"
	case ViewFolder:
		sbContext = "folder"
	}
	if a.searchOverlay.IsActive() {
		sbContext = "search"
	}

	statusbar := a.statusbar.View(sbContext)

	return lipgloss.JoinVertical(lipgloss.Left, header, mainContent, statusbar)
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
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearStatusMsg{}
	})
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
