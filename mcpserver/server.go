// Package mcpserver exposes the Bubblmail mailbox over the Model Context
// Protocol so that LLM agents can search, read, and triage mail through the
// same cache and IMAP layers the TUI uses.
//
// The server speaks JSON-RPC over stdio; stdout is reserved for the protocol,
// so nothing here may print to stdout. Read tools are served entirely from the
// local SQLite cache and are cheap; write tools connect to IMAP on demand and
// mirror the change back into the cache, matching the CLI's behaviour.
package mcpserver

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
	imaplib "github.com/bubblmail/bubblmail/imap"
)

const (
	serverName    = "bubblmail"
	serverVersion = "0.1.0"
	defaultLimit  = 50
)

// Server wires the mailbox cache and account config into MCP tool handlers.
type Server struct {
	cfg      *config.Config
	store    *cache.Store
	readOnly bool
}

// New creates a Server backed by the given config and cache store. The store is
// owned by the caller and must outlive the server. When readOnly is true the
// mutating tools are not registered, so the server can only read the mailbox.
func New(cfg *config.Config, store *cache.Store, readOnly bool) *Server {
	return &Server{cfg: cfg, store: store, readOnly: readOnly}
}

// Serve builds the MCP server and serves it over stdio until stdin closes. Pass
// readOnly to expose the read tools only.
func Serve(cfg *config.Config, store *cache.Store, readOnly bool) error {
	return server.ServeStdio(New(cfg, store, readOnly).MCPServer())
}

// MCPServer constructs the MCP server with all tools registered. In read-only
// mode the mutating tools are omitted.
func (s *Server) MCPServer() *server.MCPServer {
	instructions := "Bubblmail mailbox access. Read tools serve from a local cache and are " +
		"safe to call freely. Write tools (move_message, move_messages, " +
		"set_read, set_starred, add_tag, remove_tag) change the real mailbox over IMAP; confirm intent " +
		"with the user before calling them."
	if s.readOnly {
		instructions = "Bubblmail mailbox access, read-only. All tools serve from a local " +
			"cache and are safe to call freely; no tool can change the mailbox."
	}
	srv := server.NewMCPServer(
		serverName, serverVersion,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions(instructions),
	)

	s.registerReadTools(srv)
	if !s.readOnly {
		s.registerWriteTools(srv)
	}
	return srv
}

func (s *Server) registerReadTools(srv *server.MCPServer) {
	srv.AddTool(mcp.NewTool("list_accounts",
		mcp.WithDescription("List configured mail accounts by name."),
	), s.handleListAccounts)

	srv.AddTool(mcp.NewTool("list_folders",
		mcp.WithDescription("List mailbox folders with unread/total counts."),
		mcp.WithString("account", mcp.Description("Account name; omit to list folders for all accounts.")),
	), s.handleListFolders)

	srv.AddTool(mcp.NewTool("list_messages",
		mcp.WithDescription("List cached messages in a folder, newest first."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Description("Folder name (default INBOX).")),
		mcp.WithBoolean("unread_only", mcp.Description("Only return unread messages.")),
		mcp.WithNumber("limit", mcp.Description("Max messages to return (default 50).")),
	), s.handleListMessages)

	srv.AddTool(mcp.NewTool("search_messages",
		mcp.WithDescription("Full-text search of cached messages. The query may include "+
			"structured filter chips like [from:alice] [to:me] [since:2026-01-01] "+
			"[before:2026-06-01] [folder:INBOX] alongside free text."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query, optionally with [key:value] filter chips.")),
		mcp.WithString("account", mcp.Description("Restrict results to this account.")),
		mcp.WithString("folder", mcp.Description("Restrict results to this folder.")),
		mcp.WithNumber("limit", mcp.Description("Max results to return (default 50).")),
	), s.handleSearchMessages)

	srv.AddTool(mcp.NewTool("semantic_search",
		mcp.WithDescription("Embedding-based semantic search over an account's messages. "+
			"Requires embeddings to be configured and built; falls back to an error otherwise."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural-language query.")),
		mcp.WithString("account", mcp.Description("Account to search (default: first configured account).")),
		mcp.WithNumber("top", mcp.Description("Number of top matches to return (default 10).")),
		mcp.WithNumber("candidates", mcp.Description("Max embedded messages to scan (default 200).")),
	), s.handleSemanticSearch)

	srv.AddTool(mcp.NewTool("read_message",
		mcp.WithDescription("Read a single message including its body. Fetches the body "+
			"from IMAP and caches it if not already cached."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Required(), mcp.Description("Folder name.")),
		mcp.WithNumber("uid", mcp.Required(), mcp.Description("Message UID.")),
	), s.handleReadMessage)

	srv.AddTool(mcp.NewTool("list_categories",
		mcp.WithDescription("Show smart-folder category counts for an account."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
	), s.handleListCategories)

	srv.AddTool(mcp.NewTool("list_by_category",
		mcp.WithDescription("List messages classified under a smart-folder category."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("category", mcp.Required(), mcp.Description("Category name, e.g. IMPORTANT.")),
	), s.handleListByCategory)
}

func (s *Server) registerWriteTools(srv *server.MCPServer) {
	srv.AddTool(mcp.NewTool("move_message",
		mcp.WithDescription("Move a message to another folder over IMAP and update the cache."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("source_folder", mcp.Required(), mcp.Description("Current folder of the message.")),
		mcp.WithNumber("uid", mcp.Required(), mcp.Description("Message UID.")),
		mcp.WithString("dest_folder", mcp.Required(), mcp.Description("Destination folder.")),
	), s.handleMoveMessage)

	srv.AddTool(mcp.NewTool("move_messages",
		mcp.WithDescription("Move multiple messages to another folder over one IMAP connection and update the cache."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("source_folder", mcp.Required(), mcp.Description("Current folder of the messages.")),
		mcp.WithArray("uids", mcp.Required(), mcp.MinItems(1), mcp.WithIntegerItems(mcp.Min(1)), mcp.Description("Message UIDs.")),
		mcp.WithString("dest_folder", mcp.Required(), mcp.Description("Destination folder.")),
	), s.handleMoveMessages)

	srv.AddTool(mcp.NewTool("set_read",
		mcp.WithDescription("Mark a message read or unread over IMAP and update the cache."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Required(), mcp.Description("Folder name.")),
		mcp.WithNumber("uid", mcp.Required(), mcp.Description("Message UID.")),
		mcp.WithBoolean("read", mcp.Required(), mcp.Description("true to mark read, false to mark unread.")),
	), s.handleSetRead)

	srv.AddTool(mcp.NewTool("set_starred",
		mcp.WithDescription("Star or unstar (flag) a message over IMAP and update the cache."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Required(), mcp.Description("Folder name.")),
		mcp.WithNumber("uid", mcp.Required(), mcp.Description("Message UID.")),
		mcp.WithBoolean("starred", mcp.Required(), mcp.Description("true to star, false to unstar.")),
	), s.handleSetStarred)

	srv.AddTool(mcp.NewTool("add_tag",
		mcp.WithDescription("Add a local tag to a message (stored in the cache, not on the server)."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Required(), mcp.Description("Folder name.")),
		mcp.WithNumber("uid", mcp.Required(), mcp.Description("Message UID.")),
		mcp.WithString("tag", mcp.Required(), mcp.Description("Tag name.")),
	), s.handleAddTag)

	srv.AddTool(mcp.NewTool("remove_tag",
		mcp.WithDescription("Remove a local tag from a message."),
		mcp.WithString("account", mcp.Required(), mcp.Description("Account name.")),
		mcp.WithString("folder", mcp.Required(), mcp.Description("Folder name.")),
		mcp.WithNumber("uid", mcp.Required(), mcp.Description("Message UID.")),
		mcp.WithString("tag", mcp.Required(), mcp.Description("Tag name.")),
	), s.handleRemoveTag)
}

// account resolves an account by name. When name is empty and exactly one
// account is configured, that account is used.
func (s *Server) account(name string) (*config.AccountConfig, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		if len(s.cfg.Accounts) == 1 {
			return &s.cfg.Accounts[0], nil
		}
		return nil, fmt.Errorf("account is required (configured: %s)", strings.Join(s.accountNames(), ", "))
	}
	for i := range s.cfg.Accounts {
		if s.cfg.Accounts[i].Name == name {
			return &s.cfg.Accounts[i], nil
		}
	}
	return nil, fmt.Errorf("unknown account %q (configured: %s)", name, strings.Join(s.accountNames(), ", "))
}

func (s *Server) accountNames() []string {
	names := make([]string, 0, len(s.cfg.Accounts))
	for i := range s.cfg.Accounts {
		names = append(names, s.cfg.Accounts[i].Name)
	}
	return names
}

// connect opens an IMAP connection for the named account. Callers must Close.
func (s *Server) connect(name string) (*imaplib.Client, error) {
	acct, err := s.account(name)
	if err != nil {
		return nil, err
	}
	return imaplib.Connect(acct)
}
