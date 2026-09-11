package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/embeddings"
	imaplib "github.com/bubblmail/bubblmail/imap"
	outsmtp "github.com/bubblmail/bubblmail/smtp"
)

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.Flags().Bool("read-only", false, "Expose only read tools; disable mutating tools (flag/move/delete/send)")
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run a Model Context Protocol server over stdio",
	Long: strings.Join([]string{
		"Expose bubblmail's mail to MCP clients (e.g. Claude Desktop) as tools.",
		"",
		"By default the server exposes both read tools (backed by the local cache",
		"populated by `bubblmail refresh`) and write tools that act on the live IMAP/",
		"SMTP servers: marking messages read/starred, moving and deleting messages,",
		"and sending mail. Pass --read-only to expose the read tools only.",
		"",
		"It uses the same config and accounts as the other commands, communicates",
		"over stdio, and is meant to be launched by an MCP client, not interactively.",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		readOnly, _ := cmd.Flags().GetBool("read-only")
		return runMCP(cmd.Context(), readOnly)
	},
}

// runMCP loads config + cache once, registers the tools, and serves over stdio
// until the client disconnects. When readOnly is true, mutating tools are not
// registered.
func runMCP(ctx context.Context, readOnly bool) error {
	cfg, store, err := loadConfigAndStore()
	if err != nil {
		return err
	}
	defer store.Close()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "bubblmail",
		Title:   "Bubblmail",
		Version: "0.1.0",
	}, nil)

	registerMCPTools(server, cfg, store, readOnly)

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}

// --- Shared output shapes -------------------------------------------------

type mcpMessage struct {
	UID       uint32 `json:"uid" jsonschema:"IMAP UID of the message"`
	Account   string `json:"account" jsonschema:"account the message belongs to"`
	Mailbox   string `json:"mailbox" jsonschema:"mailbox/folder the message lives in"`
	Date      string `json:"date" jsonschema:"message date in RFC3339"`
	From      string `json:"from" jsonschema:"sender display name and address"`
	Subject   string `json:"subject" jsonschema:"message subject"`
	Snippet   string `json:"snippet,omitempty" jsonschema:"short plain-text preview"`
	Unread    bool   `json:"unread" jsonschema:"true if the message has not been read"`
	Starred   bool   `json:"starred" jsonschema:"true if the message is flagged/starred"`
	ThreadID  string `json:"thread_id,omitempty" jsonschema:"thread identifier"`
	MessageID string `json:"message_id,omitempty" jsonschema:"RFC822 Message-ID header"`
}

func toMCPMessage(m *data.Message) mcpMessage {
	date := ""
	if !m.Date.IsZero() {
		date = m.Date.Format("2006-01-02T15:04:05Z07:00")
	}
	return mcpMessage{
		UID:       m.UID,
		Account:   m.AccountName,
		Mailbox:   m.FolderName,
		Date:      date,
		From:      m.FromString(),
		Subject:   m.Subject,
		Snippet:   m.Snippet,
		Unread:    !m.IsRead(),
		Starred:   m.IsStarred(),
		ThreadID:  m.ThreadID,
		MessageID: m.MessageID,
	}
}

func toMCPMessages(msgs []*data.Message) []mcpMessage {
	out := make([]mcpMessage, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		out = append(out, toMCPMessage(m))
	}
	return out
}

func mcpLimit(n, def int) int {
	if n <= 0 {
		return def
	}
	if n > 200 {
		return 200
	}
	return n
}

// resolveAccount picks the account name to use: the requested one if set,
// otherwise the configured default, otherwise the first account.
func resolveAccount(cfg *config.Config, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		for i := range cfg.Accounts {
			if cfg.Accounts[i].Name == requested {
				return requested, nil
			}
		}
		return "", fmt.Errorf("account %q not found", requested)
	}
	if len(cfg.Accounts) == 0 {
		return "", errors.New("no accounts configured")
	}
	if cfg.General.DefaultAccount != "" {
		return cfg.General.DefaultAccount, nil
	}
	return cfg.Accounts[0].Name, nil
}

// --- Tool registration ----------------------------------------------------

func registerMCPTools(s *mcp.Server, cfg *config.Config, store *cache.Store, readOnly bool) {
	registerListMailboxes(s, store)
	registerListMessages(s, store)
	registerSearchMessages(s, store)
	registerSemanticSearch(s, cfg, store)
	registerReadMessage(s, store)
	registerGetClassification(s, store)
	registerGetSuggestedEvent(s, store)

	if readOnly {
		return
	}
	registerMarkRead(s, cfg, store)
	registerSetStarred(s, cfg, store)
	registerMoveMessage(s, cfg)
	registerDeleteMessage(s, cfg, store)
	registerSendMessage(s, cfg, store)
}

// list_mailboxes ------------------------------------------------------------

type listMailboxesInput struct {
	Account string `json:"account,omitempty" jsonschema:"account name; empty for all accounts"`
}

type mcpMailbox struct {
	Account string `json:"account"`
	Name    string `json:"name"`
	Unread  int    `json:"unread"`
	Total   int    `json:"total"`
}

type listMailboxesOutput struct {
	Mailboxes []mcpMailbox `json:"mailboxes"`
}

func registerListMailboxes(s *mcp.Server, store *cache.Store) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_mailboxes",
		Description: "List cached mailboxes/folders with unread and total message counts.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listMailboxesInput) (*mcp.CallToolResult, listMailboxesOutput, error) {
		var (
			folders []*data.Folder
			err     error
		)
		if acct := strings.TrimSpace(in.Account); acct != "" {
			folders, err = store.GetFolders(acct)
		} else {
			folders, err = store.GetAllFolders()
		}
		if err != nil {
			return nil, listMailboxesOutput{}, err
		}
		out := listMailboxesOutput{Mailboxes: make([]mcpMailbox, 0, len(folders))}
		for _, f := range folders {
			name := f.Name
			if f.DisplayName != "" {
				name = f.DisplayName
			}
			out.Mailboxes = append(out.Mailboxes, mcpMailbox{
				Account: f.AccountName, Name: name, Unread: f.Unread, Total: f.Total,
			})
		}
		return nil, out, nil
	})
}

// list_messages -------------------------------------------------------------

type listMessagesInput struct {
	Account    string `json:"account,omitempty" jsonschema:"account name; empty for all accounts"`
	Mailbox    string `json:"mailbox,omitempty" jsonschema:"mailbox/folder name; empty for all mailboxes"`
	UnreadOnly bool   `json:"unread_only,omitempty" jsonschema:"only return unread messages"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max messages to return (default 50, max 200)"`
}

type messagesOutput struct {
	Messages []mcpMessage `json:"messages"`
}

func registerListMessages(s *mcp.Server, store *cache.Store) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_messages",
		Description: "List cached messages, newest first, optionally filtered by account, mailbox, and unread state.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listMessagesInput) (*mcp.CallToolResult, messagesOutput, error) {
		msgs, err := store.GetMessagesFiltered(
			strings.TrimSpace(in.Account),
			strings.TrimSpace(in.Mailbox),
			in.UnreadOnly,
			mcpLimit(in.Limit, defaultListLimit),
		)
		if err != nil {
			return nil, messagesOutput{}, err
		}
		return nil, messagesOutput{Messages: toMCPMessages(msgs)}, nil
	})
}

// search_messages -----------------------------------------------------------

type searchMessagesInput struct {
	Query   string `json:"query" jsonschema:"full-text search query (FTS5 syntax)"`
	Account string `json:"account,omitempty" jsonschema:"account name; empty for all accounts"`
	Mailbox string `json:"mailbox,omitempty" jsonschema:"mailbox/folder name; empty for all mailboxes"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max messages to return (default 50, max 200)"`
}

func registerSearchMessages(s *mcp.Server, store *cache.Store) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_messages",
		Description: "Keyword (full-text) search over cached messages, newest first.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchMessagesInput) (*mcp.CallToolResult, messagesOutput, error) {
		query := strings.TrimSpace(in.Query)
		if query == "" {
			return nil, messagesOutput{}, errors.New("query is required")
		}
		msgs, err := store.SearchLocalFiltered(
			query,
			strings.TrimSpace(in.Account),
			strings.TrimSpace(in.Mailbox),
			mcpLimit(in.Limit, defaultListLimit),
		)
		if err != nil {
			return nil, messagesOutput{}, err
		}
		return nil, messagesOutput{Messages: toMCPMessages(msgs)}, nil
	})
}

// semantic_search -----------------------------------------------------------

type semanticSearchInput struct {
	Query   string `json:"query" jsonschema:"natural-language query to match semantically"`
	Account string `json:"account,omitempty" jsonschema:"account name; defaults to the configured default account"`
	Top     int    `json:"top,omitempty" jsonschema:"number of top results to return (default 5, max 50)"`
}

type semanticHit struct {
	Score   float32    `json:"score" jsonschema:"cosine similarity score"`
	Message mcpMessage `json:"message"`
}

type semanticSearchOutput struct {
	Hits []semanticHit `json:"hits"`
}

func registerSemanticSearch(s *mcp.Server, cfg *config.Config, store *cache.Store) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "semantic_search",
		Description: "Embedding-based semantic search over cached messages. Requires embeddings to be configured and backfilled (`bubblmail embeddings embedd`).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in semanticSearchInput) (*mcp.CallToolResult, semanticSearchOutput, error) {
		query := strings.TrimSpace(in.Query)
		if query == "" {
			return nil, semanticSearchOutput{}, errors.New("query is required")
		}
		account, err := resolveAccount(cfg, in.Account)
		if err != nil {
			return nil, semanticSearchOutput{}, err
		}
		client, err := embeddings.NewClient(cfg.Embeddings)
		if err != nil {
			return nil, semanticSearchOutput{}, fmt.Errorf("semantic search unavailable: %w", err)
		}
		model := strings.TrimSpace(cfg.Embeddings.Model)
		if model == "" {
			model = "openai/text-embedding-3-small"
		}
		topN := in.Top
		if topN <= 0 {
			topN = 5
		}
		if topN > 50 {
			topN = 50
		}

		msgs, vectors, norms, err := store.ListEmbeddingCandidates(account, model, 1000)
		if err != nil {
			return nil, semanticSearchOutput{}, fmt.Errorf("listing embedding candidates: %w", err)
		}
		if len(msgs) == 0 {
			return nil, semanticSearchOutput{Hits: []semanticHit{}}, nil
		}
		vecs, err := client.EmbedTexts(ctx, []string{query})
		if err != nil {
			return nil, semanticSearchOutput{}, fmt.Errorf("embedding query: %w", err)
		}
		if len(vecs) == 0 {
			return nil, semanticSearchOutput{}, errors.New("embedding query: empty response")
		}
		queryVec := vecs[0]
		queryNorm := embeddings.VectorNorm(queryVec)
		top := embeddings.TopK(msgs, vectors, norms, queryVec, queryNorm, topN)

		out := semanticSearchOutput{Hits: make([]semanticHit, 0, len(top))}
		for _, hit := range top {
			if hit.Message == nil {
				continue
			}
			out.Hits = append(out.Hits, semanticHit{
				Score:   hit.Score,
				Message: toMCPMessage(hit.Message),
			})
		}
		return nil, out, nil
	})
}

// read_message --------------------------------------------------------------

type readMessageInput struct {
	Account string `json:"account" jsonschema:"account the message belongs to"`
	Mailbox string `json:"mailbox" jsonschema:"mailbox/folder the message lives in"`
	UID     uint32 `json:"uid" jsonschema:"IMAP UID of the message"`
}

type readMessageOutput struct {
	Message  mcpMessage `json:"message"`
	BodyText string     `json:"body_text,omitempty" jsonschema:"plain-text body (from cache; may be empty if not fetched)"`
	BodyHTML string     `json:"body_html,omitempty" jsonschema:"HTML body (from cache; may be empty if not fetched)"`
}

func registerReadMessage(s *mcp.Server, store *cache.Store) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_message",
		Description: "Read a single cached message by account, mailbox, and UID, including its cached body if available.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in readMessageInput) (*mcp.CallToolResult, readMessageOutput, error) {
		account := strings.TrimSpace(in.Account)
		mailbox := strings.TrimSpace(in.Mailbox)
		if account == "" || mailbox == "" {
			return nil, readMessageOutput{}, errors.New("account and mailbox are required")
		}
		msg, err := store.GetMessageByUID(account, mailbox, in.UID)
		if err != nil {
			return nil, readMessageOutput{}, err
		}
		if msg == nil {
			return nil, readMessageOutput{}, fmt.Errorf("message uid %d not found in %s/%s", in.UID, account, mailbox)
		}
		bodyText, bodyHTML, err := store.GetBody(msg.ID)
		if err != nil {
			return nil, readMessageOutput{}, err
		}
		return nil, readMessageOutput{
			Message:  toMCPMessage(msg),
			BodyText: bodyText,
			BodyHTML: bodyHTML,
		}, nil
	})
}

// get_classification --------------------------------------------------------

type messageRefInput struct {
	Account string `json:"account" jsonschema:"account the message belongs to"`
	Mailbox string `json:"mailbox" jsonschema:"mailbox/folder the message lives in"`
	UID     uint32 `json:"uid" jsonschema:"IMAP UID of the message"`
}

type classificationOutput struct {
	Category string `json:"category" jsonschema:"stored smart-folder category, or empty if not classified"`
}

func registerGetClassification(s *mcp.Server, store *cache.Store) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_classification",
		Description: "Get the stored smart-folder category for a message (from prior AI classification). Does not run new classification.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in messageRefInput) (*mcp.CallToolResult, classificationOutput, error) {
		msg, err := lookupMessage(store, in.Account, in.Mailbox, in.UID)
		if err != nil {
			return nil, classificationOutput{}, err
		}
		category, err := store.GetCategory(msg.ID)
		if err != nil {
			return nil, classificationOutput{}, err
		}
		return nil, classificationOutput{Category: category}, nil
	})
}

// get_suggested_event -------------------------------------------------------

type suggestedEventOutput struct {
	HasEvent    bool   `json:"has_event" jsonschema:"true if a calendar event was detected"`
	Summary     string `json:"summary,omitempty"`
	Date        string `json:"date,omitempty"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
	Location    string `json:"location,omitempty"`
	AllDay      bool   `json:"all_day,omitempty"`
	Recurring   bool   `json:"recurring,omitempty"`
	Description string `json:"description,omitempty"`
	Found       bool   `json:"found" jsonschema:"true if a suggestion has been generated and stored for this message"`
}

func registerGetSuggestedEvent(s *mcp.Server, store *cache.Store) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_suggested_event",
		Description: "Get the stored calendar-event suggestion extracted from a message, if one has been generated.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in messageRefInput) (*mcp.CallToolResult, suggestedEventOutput, error) {
		msg, err := lookupMessage(store, in.Account, in.Mailbox, in.UID)
		if err != nil {
			return nil, suggestedEventOutput{}, err
		}
		ev, err := store.GetSuggestedEvent(msg.ID)
		if err != nil {
			return nil, suggestedEventOutput{}, err
		}
		if ev == nil {
			return nil, suggestedEventOutput{Found: false}, nil
		}
		return nil, suggestedEventOutput{
			Found:       true,
			HasEvent:    ev.HasEvent,
			Summary:     ev.Summary,
			Date:        ev.Date,
			Start:       ev.Start,
			End:         ev.End,
			Location:    ev.Location,
			AllDay:      ev.AllDay,
			Recurring:   ev.Recurring,
			Description: ev.Description,
		}, nil
	})
}

// --- Write tools ----------------------------------------------------------

// accountConfig resolves the *config.AccountConfig for a (possibly empty)
// requested account name, reusing the same defaulting rules as resolveAccount.
func accountConfig(cfg *config.Config, requested string) (*config.AccountConfig, error) {
	name, err := resolveAccount(cfg, requested)
	if err != nil {
		return nil, err
	}
	for i := range cfg.Accounts {
		if cfg.Accounts[i].Name == name {
			return &cfg.Accounts[i], nil
		}
	}
	return nil, fmt.Errorf("account %q not found", name)
}

// withClient connects to the given account's IMAP server, runs fn, and closes.
func withClient(cfg *config.Config, account string, fn func(*imaplib.Client) error) error {
	acct, err := accountConfig(cfg, account)
	if err != nil {
		return err
	}
	client, err := imaplib.Connect(acct)
	if err != nil {
		return err
	}
	defer client.Close()
	return fn(client)
}

// toggleFlag returns flags with f added (add) or removed (!add), no duplicates.
func toggleFlag(flags []data.Flag, f data.Flag, add bool) []data.Flag {
	out := make([]data.Flag, 0, len(flags)+1)
	for _, existing := range flags {
		if existing != f {
			out = append(out, existing)
		}
	}
	if add {
		out = append(out, f)
	}
	return out
}

// resolveSpecialFolder finds a folder by IMAP special-use attribute, falling
// back to common display names. Returns "" if none match.
func resolveSpecialFolder(store *cache.Store, account, attr string, names ...string) (string, error) {
	folders, err := store.GetFolders(account)
	if err != nil {
		return "", err
	}
	for _, f := range folders {
		for _, a := range f.Attributes {
			if a == attr {
				return f.Name, nil
			}
		}
	}
	for _, f := range folders {
		for _, n := range names {
			if f.DisplayName == n {
				return f.Name, nil
			}
		}
	}
	return "", nil
}

// setMessageFlag flips a single IMAP flag on a message and mirrors the change
// into the local cache so read tools stay consistent until the next refresh.
func setMessageFlag(cfg *config.Config, store *cache.Store, account, mailbox string, uid uint32, flag data.Flag, set bool) error {
	account = strings.TrimSpace(account)
	mailbox = strings.TrimSpace(mailbox)
	if account == "" || mailbox == "" {
		return errors.New("account and mailbox are required")
	}
	err := withClient(cfg, account, func(client *imaplib.Client) error {
		res := client.SetFlag(mailbox, uid, flag, set)()
		if r, ok := res.(imaplib.SetFlagResultMsg); ok {
			return r.Err
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Best-effort cache update; ignore lookup misses.
	if msg, _ := store.GetMessageByUID(account, mailbox, uid); msg != nil {
		_ = store.SetFlags(account, mailbox, uid, toggleFlag(msg.Flags, flag, set))
	}
	return nil
}

// mark_read -----------------------------------------------------------------

type markReadInput struct {
	Account string `json:"account" jsonschema:"account the message belongs to"`
	Mailbox string `json:"mailbox" jsonschema:"mailbox/folder the message lives in"`
	UID     uint32 `json:"uid" jsonschema:"IMAP UID of the message"`
	Read    bool   `json:"read" jsonschema:"true to mark read (set \\Seen), false to mark unread"`
}

type okOutput struct {
	OK bool `json:"ok" jsonschema:"true if the operation succeeded"`
}

func registerMarkRead(s *mcp.Server, cfg *config.Config, store *cache.Store) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "mark_read",
		Description: "Mark a message read or unread (sets/clears the IMAP \\Seen flag) on the live server.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in markReadInput) (*mcp.CallToolResult, okOutput, error) {
		if err := setMessageFlag(cfg, store, in.Account, in.Mailbox, in.UID, data.FlagSeen, in.Read); err != nil {
			return nil, okOutput{}, err
		}
		return nil, okOutput{OK: true}, nil
	})
}

// set_starred ---------------------------------------------------------------

type setStarredInput struct {
	Account string `json:"account" jsonschema:"account the message belongs to"`
	Mailbox string `json:"mailbox" jsonschema:"mailbox/folder the message lives in"`
	UID     uint32 `json:"uid" jsonschema:"IMAP UID of the message"`
	Starred bool   `json:"starred" jsonschema:"true to star (set \\Flagged), false to unstar"`
}

func registerSetStarred(s *mcp.Server, cfg *config.Config, store *cache.Store) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_starred",
		Description: "Star or unstar a message (sets/clears the IMAP \\Flagged flag) on the live server.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in setStarredInput) (*mcp.CallToolResult, okOutput, error) {
		if err := setMessageFlag(cfg, store, in.Account, in.Mailbox, in.UID, data.FlagFlagged, in.Starred); err != nil {
			return nil, okOutput{}, err
		}
		return nil, okOutput{OK: true}, nil
	})
}

// move_message --------------------------------------------------------------

type moveMessageInput struct {
	Account     string `json:"account" jsonschema:"account the message belongs to"`
	Mailbox     string `json:"mailbox" jsonschema:"source mailbox/folder the message lives in"`
	UID         uint32 `json:"uid" jsonschema:"IMAP UID of the message"`
	Destination string `json:"destination" jsonschema:"destination mailbox/folder name"`
}

func registerMoveMessage(s *mcp.Server, cfg *config.Config) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "move_message",
		Description: "Move a message from one mailbox to another on the live server. The cache is stale for the moved message until the next `bubblmail refresh`.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in moveMessageInput) (*mcp.CallToolResult, okOutput, error) {
		account := strings.TrimSpace(in.Account)
		mailbox := strings.TrimSpace(in.Mailbox)
		dest := strings.TrimSpace(in.Destination)
		if account == "" || mailbox == "" || dest == "" {
			return nil, okOutput{}, errors.New("account, mailbox, and destination are required")
		}
		err := withClient(cfg, account, func(client *imaplib.Client) error {
			res := client.MoveMessage(mailbox, in.UID, dest)()
			if r, ok := res.(imaplib.MoveMessageResultMsg); ok {
				return r.Err
			}
			return nil
		})
		if err != nil {
			return nil, okOutput{}, err
		}
		return nil, okOutput{OK: true}, nil
	})
}

// delete_message ------------------------------------------------------------

type deleteMessageInput struct {
	Account string `json:"account" jsonschema:"account the message belongs to"`
	Mailbox string `json:"mailbox" jsonschema:"mailbox/folder the message lives in"`
	UID     uint32 `json:"uid" jsonschema:"IMAP UID of the message"`
}

func registerDeleteMessage(s *mcp.Server, cfg *config.Config, store *cache.Store) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_message",
		Description: "Delete a message by moving it to the account's Trash folder on the live server. The cache is stale for the message until the next `bubblmail refresh`.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteMessageInput) (*mcp.CallToolResult, okOutput, error) {
		account := strings.TrimSpace(in.Account)
		mailbox := strings.TrimSpace(in.Mailbox)
		if account == "" || mailbox == "" {
			return nil, okOutput{}, errors.New("account and mailbox are required")
		}
		trash, err := resolveSpecialFolder(store, account, `\Trash`, "Trash", "Deleted", "Deleted Items", "Deleted Messages", "Bin")
		if err != nil {
			return nil, okOutput{}, err
		}
		if trash == "" {
			return nil, okOutput{}, errors.New("could not determine a Trash folder for this account")
		}
		if trash == mailbox {
			return nil, okOutput{}, errors.New("message is already in the Trash folder")
		}
		err = withClient(cfg, account, func(client *imaplib.Client) error {
			res := client.MoveToTrash(mailbox, in.UID, trash)()
			if r, ok := res.(imaplib.MoveToTrashResultMsg); ok {
				return r.Err
			}
			return nil
		})
		if err != nil {
			return nil, okOutput{}, err
		}
		return nil, okOutput{OK: true}, nil
	})
}

// send_message --------------------------------------------------------------

type sendMessageInput struct {
	Account string   `json:"account,omitempty" jsonschema:"sending account; defaults to the configured default account"`
	To      []string `json:"to" jsonschema:"recipient email addresses"`
	CC      []string `json:"cc,omitempty" jsonschema:"CC email addresses"`
	Subject string   `json:"subject" jsonschema:"message subject"`
	Body    string   `json:"body" jsonschema:"plain-text message body"`
}

func toAddresses(raw []string) []data.Address {
	out := make([]data.Address, 0, len(raw))
	for _, a := range raw {
		a = strings.TrimSpace(a)
		if a != "" {
			out = append(out, data.Address{Address: a})
		}
	}
	return out
}

func registerSendMessage(s *mcp.Server, cfg *config.Config, store *cache.Store) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "send_message",
		Description: "Send a plain-text email via SMTP and append a copy to the Sent folder. This sends real mail; only call it when the user clearly intends to send.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sendMessageInput) (*mcp.CallToolResult, okOutput, error) {
		acct, err := accountConfig(cfg, in.Account)
		if err != nil {
			return nil, okOutput{}, err
		}
		to := toAddresses(in.To)
		if len(to) == 0 {
			return nil, okOutput{}, errors.New("at least one recipient (to) is required")
		}
		if strings.TrimSpace(in.Subject) == "" {
			return nil, okOutput{}, errors.New("subject is required")
		}
		draft := &outsmtp.ComposedMessage{
			From:    data.Address{Address: acct.Username},
			To:      to,
			CC:      toAddresses(in.CC),
			Subject: in.Subject,
			Body:    in.Body,
		}
		res, ok := outsmtp.SendMessage(acct, draft)().(outsmtp.SendResultMsg)
		if !ok {
			return nil, okOutput{}, errors.New("unexpected SMTP response")
		}
		if res.Err != nil {
			return nil, okOutput{}, res.Err
		}
		// Best-effort: append a copy to the Sent folder. A send that succeeded
		// must not be reported as a failure if this step fails.
		if sent, _ := resolveSpecialFolder(store, acct.Name, `\Sent`, "Sent", "Sent Items", "Sent Mail", "Sent Messages"); sent != "" {
			_ = withClient(cfg, acct.Name, func(client *imaplib.Client) error {
				client.AppendMessage(sent, res.Raw, []data.Flag{data.FlagSeen})()
				return nil
			})
		}
		return nil, okOutput{OK: true}, nil
	})
}

// lookupMessage resolves a message by account/mailbox/uid, returning a clear error.
func lookupMessage(store *cache.Store, account, mailbox string, uid uint32) (*data.Message, error) {
	account = strings.TrimSpace(account)
	mailbox = strings.TrimSpace(mailbox)
	if account == "" || mailbox == "" {
		return nil, errors.New("account and mailbox are required")
	}
	msg, err := store.GetMessageByUID(account, mailbox, uid)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, fmt.Errorf("message uid %d not found in %s/%s", uid, account, mailbox)
	}
	return msg, nil
}
