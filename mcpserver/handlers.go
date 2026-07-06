package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/embeddings"
	imaplib "github.com/bubblmail/bubblmail/imap"
)

// messageJSON is the wire shape for a message summary returned by tools.
type messageJSON struct {
	Account  string   `json:"account"`
	Folder   string   `json:"folder"`
	UID      uint32   `json:"uid"`
	ID       int64    `json:"id"`
	Subject  string   `json:"subject"`
	From     []string `json:"from"`
	To       []string `json:"to,omitempty"`
	Date     string   `json:"date"`
	Snippet  string   `json:"snippet,omitempty"`
	Read     bool     `json:"read"`
	Starred  bool     `json:"starred"`
	Tags     []string `json:"tags,omitempty"`
	ThreadID string   `json:"thread_id,omitempty"`
	Score    float32  `json:"score,omitempty"`
	Body     string   `json:"body,omitempty"`
}

func toMessageJSON(m *data.Message) messageJSON {
	return messageJSON{
		Account:  m.AccountName,
		Folder:   m.FolderName,
		UID:      m.UID,
		ID:       m.ID,
		Subject:  m.Subject,
		From:     addressStrings(m.From),
		To:       addressStrings(m.To),
		Date:     m.Date.Format("2006-01-02 15:04"),
		Snippet:  m.Snippet,
		Read:     m.IsRead(),
		Starred:  m.IsStarred(),
		Tags:     m.Tags,
		ThreadID: m.ThreadID,
	}
}

func toMessageJSONList(msgs []*data.Message) []messageJSON {
	out := make([]messageJSON, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		out = append(out, toMessageJSON(m))
	}
	return out
}

// messageListResult wraps a message slice in an object. The MCP spec requires a
// tool's structuredContent to be a JSON object, so list-style handlers must not
// return a bare array (clients reject it with "expected record, received array").
type messageListResult struct {
	Messages []messageJSON `json:"messages"`
	Count    int           `json:"count"`
}

func toMessageListResult(msgs []messageJSON) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultJSON(messageListResult{Messages: msgs, Count: len(msgs)})
}

func addressStrings(addrs []data.Address) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}

func limitArg(req mcp.CallToolRequest, def int) int {
	n := req.GetInt("limit", def)
	if n <= 0 {
		return def
	}
	return n
}

// --- read handlers ---

func (s *Server) handleListAccounts(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	type acctJSON struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		IMAPHost string `json:"imap_host"`
	}
	out := make([]acctJSON, 0, len(s.cfg.Accounts))
	for i := range s.cfg.Accounts {
		a := &s.cfg.Accounts[i]
		out = append(out, acctJSON{Name: a.Name, Username: a.Username, IMAPHost: a.IMAPHost})
	}
	return mcp.NewToolResultJSON(struct {
		Accounts []acctJSON `json:"accounts"`
		Count    int        `json:"count"`
	}{Accounts: out, Count: len(out)})
}

func (s *Server) handleListFolders(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account := strings.TrimSpace(req.GetString("account", ""))
	var (
		folders []*data.Folder
		err     error
	)
	if account != "" {
		folders, err = s.store.GetFolders(account)
	} else {
		folders, err = s.store.GetAllFolders()
	}
	if err != nil {
		return mcp.NewToolResultErrorFromErr("listing folders", err), nil
	}
	type folderJSON struct {
		Account string `json:"account"`
		Name    string `json:"name"`
		Display string `json:"display"`
		Unread  int    `json:"unread"`
		Total   int    `json:"total"`
	}
	out := make([]folderJSON, 0, len(folders))
	for _, f := range folders {
		out = append(out, folderJSON{
			Account: f.AccountName,
			Name:    f.Name,
			Display: f.DisplayName,
			Unread:  f.Unread,
			Total:   f.Total,
		})
	}
	return mcp.NewToolResultJSON(struct {
		Folders []folderJSON `json:"folders"`
		Count   int          `json:"count"`
	}{Folders: out, Count: len(out)})
}

func (s *Server) handleListMessages(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, err := req.RequireString("account")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("account", err), nil
	}
	folder := strings.TrimSpace(req.GetString("folder", "INBOX"))
	if folder == "" {
		folder = "INBOX"
	}
	unreadOnly := req.GetBool("unread_only", false)
	msgs, err := s.store.GetMessagesFiltered(account, folder, unreadOnly, limitArg(req, defaultLimit))
	if err != nil {
		return mcp.NewToolResultErrorFromErr("listing messages", err), nil
	}
	return toMessageListResult(toMessageJSONList(msgs))
}

func (s *Server) handleSearchMessages(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("query", err), nil
	}
	account := strings.TrimSpace(req.GetString("account", ""))
	folder := strings.TrimSpace(req.GetString("folder", ""))
	limit := limitArg(req, defaultLimit)

	msgs, err := s.store.SearchLocalWithFilters(query)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("searching", err), nil
	}
	filtered := make([]*data.Message, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if account != "" && m.AccountName != account {
			continue
		}
		if folder != "" && m.FolderName != folder {
			continue
		}
		filtered = append(filtered, m)
		if len(filtered) >= limit {
			break
		}
	}
	return toMessageListResult(toMessageJSONList(filtered))
}

func (s *Server) handleSemanticSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("query", err), nil
	}
	acct, err := s.account(req.GetString("account", ""))
	if err != nil {
		return mcp.NewToolResultErrorFromErr("account", err), nil
	}
	topN := req.GetInt("top", 10)
	if topN <= 0 {
		topN = 10
	}
	candidates := req.GetInt("candidates", 200)
	if candidates <= 0 {
		candidates = 200
	}

	client, err := embeddings.NewClient(s.cfg.Embeddings)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("semantic search unavailable: %v", err)), nil
	}
	model := strings.TrimSpace(s.cfg.Embeddings.Model)
	if model == "" {
		model = "openai/text-embedding-3-small"
	}
	msgs, vectors, norms, err := s.store.ListEmbeddingCandidates(acct.Name, model, candidates)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("listing embedding candidates", err), nil
	}
	if len(msgs) == 0 {
		return mcp.NewToolResultError("no embeddings built for this account/model; run `bubblmail embeddings embed` first"), nil
	}
	vecs, err := client.EmbedTexts(ctx, []string{query})
	if err != nil {
		return mcp.NewToolResultErrorFromErr("embedding query", err), nil
	}
	if len(vecs) == 0 {
		return mcp.NewToolResultError("embedding query returned no vector"), nil
	}
	queryVec := vecs[0]
	hits := embeddings.TopK(msgs, vectors, norms, queryVec, embeddings.VectorNorm(queryVec), topN)
	out := make([]messageJSON, 0, len(hits))
	for _, hit := range hits {
		if hit.Message == nil {
			continue
		}
		mj := toMessageJSON(hit.Message)
		mj.Score = hit.Score
		out = append(out, mj)
	}
	return toMessageListResult(out)
}

func (s *Server) handleReadMessage(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, folder, uid, errRes := s.messageTarget(req)
	if errRes != nil {
		return errRes, nil
	}
	msg, err := s.store.GetMessageByUID(account, folder, uid)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("finding message", err), nil
	}
	bodyText, bodyHTML, err := s.store.GetBody(msg.ID)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("reading cached body", err), nil
	}
	if bodyText == "" && bodyHTML == "" {
		client, err := s.connect(account)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("connecting", err), nil
		}
		bodyText, bodyHTML, _, err = client.FetchBodySync(folder, uid)
		closeErr := client.Close()
		if err != nil {
			return mcp.NewToolResultErrorFromErr("fetching body", err), nil
		}
		if closeErr != nil {
			return mcp.NewToolResultErrorFromErr("closing connection", closeErr), nil
		}
		if err := s.store.UpsertBody(msg.ID, bodyText, bodyHTML); err != nil {
			return mcp.NewToolResultErrorFromErr("caching body", err), nil
		}
	}
	body := bodyText
	if strings.TrimSpace(body) == "" {
		body = bodyHTML
	}
	mj := toMessageJSON(msg)
	mj.Body = body
	return mcp.NewToolResultJSON(mj)
}

func (s *Server) handleListCategories(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, err := req.RequireString("account")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("account", err), nil
	}
	counts, err := s.store.GetCategoryCounts(account)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("category counts", err), nil
	}
	return mcp.NewToolResultJSON(counts)
}

func (s *Server) handleListByCategory(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, err := req.RequireString("account")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("account", err), nil
	}
	category, err := req.RequireString("category")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("category", err), nil
	}
	msgs, err := s.store.GetMessagesByCategory(account, category)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("listing by category", err), nil
	}
	return toMessageListResult(toMessageJSONList(msgs))
}

// --- write handlers ---

func (s *Server) handleMoveMessage(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, err := req.RequireString("account")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("account", err), nil
	}
	source, err := req.RequireString("source_folder")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("source_folder", err), nil
	}
	dest, err := req.RequireString("dest_folder")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("dest_folder", err), nil
	}
	uid, errRes := uidArg(req)
	if errRes != nil {
		return errRes, nil
	}

	client, err := s.connect(account)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("connecting", err), nil
	}
	destUID, err := client.MoveMessageSync(source, uid, dest)
	if err != nil {
		client.Close()
		return mcp.NewToolResultErrorFromErr("moving message", err), nil
	}
	if err := client.Close(); err != nil {
		return mcp.NewToolResultErrorFromErr("closing connection", err), nil
	}
	if err := s.store.MoveMessage(account, source, uid, dest, destUID); err != nil {
		return mcp.NewToolResultErrorFromErr("updating cache", err), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Moved %s/%d to %s (new uid %d).", source, uid, dest, destUID)), nil
}

func (s *Server) handleMoveMessages(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, err := req.RequireString("account")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("account", err), nil
	}
	source, err := req.RequireString("source_folder")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("source_folder", err), nil
	}
	dest, err := req.RequireString("dest_folder")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("dest_folder", err), nil
	}
	uids, errRes := uidsArg(req)
	if errRes != nil {
		return errRes, nil
	}

	client, err := s.connect(account)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("connecting", err), nil
	}

	succeeded := 0
	failedUIDs := make([]uint32, 0)
	newUIDs := make([]string, 0, len(uids))
	var firstErr error
	for _, uid := range uids {
		destUID, err := client.MoveMessageSync(source, uid, dest)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("uid %d: %w", uid, err)
			}
			failedUIDs = append(failedUIDs, uid)
			continue
		}
		if err := s.store.MoveMessage(account, source, uid, dest, destUID); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("uid %d: updating cache: %w", uid, err)
			}
			failedUIDs = append(failedUIDs, uid)
			continue
		}
		succeeded++
		newUIDs = append(newUIDs, fmt.Sprintf("%d->%d", uid, destUID))
	}
	closeErr := client.Close()
	if firstErr == nil && closeErr != nil {
		return mcp.NewToolResultErrorFromErr("closing connection", closeErr), nil
	}

	if len(failedUIDs) > 0 {
		return mcp.NewToolResultText(fmt.Sprintf(
			"Moved %d message(s) from %s to %s. (%d failed; failed uids: %s; first error: %v)",
			succeeded, source, dest, len(failedUIDs), formatUIDs(failedUIDs), firstErr,
		)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Moved %d message(s) from %s to %s (new uids: %s).", succeeded, source, dest, strings.Join(newUIDs, ", "))), nil
}

func (s *Server) handleSetRead(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, folder, uid, errRes := s.messageTarget(req)
	if errRes != nil {
		return errRes, nil
	}
	read, err := req.RequireBool("read")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("read", err), nil
	}
	if err := s.setFlag(account, folder, uid, data.FlagSeen, read); err != nil {
		return mcp.NewToolResultErrorFromErr("setting read flag", err), nil
	}
	state := "unread"
	if read {
		state = "read"
	}
	return mcp.NewToolResultText(fmt.Sprintf("Marked %s/%d as %s.", folder, uid, state)), nil
}

func (s *Server) handleSetStarred(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, folder, uid, errRes := s.messageTarget(req)
	if errRes != nil {
		return errRes, nil
	}
	starred, err := req.RequireBool("starred")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("starred", err), nil
	}
	if err := s.setFlag(account, folder, uid, data.FlagFlagged, starred); err != nil {
		return mcp.NewToolResultErrorFromErr("setting star flag", err), nil
	}
	state := "Unstarred"
	if starred {
		state = "Starred"
	}
	return mcp.NewToolResultText(fmt.Sprintf("%s %s/%d.", state, folder, uid)), nil
}

func (s *Server) handleAddTag(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, folder, uid, errRes := s.messageTarget(req)
	if errRes != nil {
		return errRes, nil
	}
	tag, err := req.RequireString("tag")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("tag", err), nil
	}
	msg, err := s.store.GetMessageByUID(account, folder, uid)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("finding message", err), nil
	}
	if err := s.store.AddTag(msg.ID, tag); err != nil {
		return mcp.NewToolResultErrorFromErr("adding tag", err), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Tagged %s/%d with %q.", folder, uid, tag)), nil
}

func (s *Server) handleRemoveTag(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, folder, uid, errRes := s.messageTarget(req)
	if errRes != nil {
		return errRes, nil
	}
	tag, err := req.RequireString("tag")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("tag", err), nil
	}
	msg, err := s.store.GetMessageByUID(account, folder, uid)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("finding message", err), nil
	}
	if err := s.store.RemoveTag(msg.ID, tag); err != nil {
		return mcp.NewToolResultErrorFromErr("removing tag", err), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Removed tag %q from %s/%d.", tag, folder, uid)), nil
}

// setFlag sets or clears an IMAP flag on the server and mirrors the change into
// the cache by rewriting the message's full flag set.
func (s *Server) setFlag(account, folder string, uid uint32, flag data.Flag, set bool) error {
	client, err := s.connect(account)
	if err != nil {
		return err
	}
	msg := client.SetFlag(folder, uid, flag, set)()
	if res, ok := msg.(imaplib.SetFlagResultMsg); ok && res.Err != nil {
		client.Close()
		return res.Err
	}
	if err := client.Close(); err != nil {
		return err
	}
	cached, err := s.store.GetMessageByUID(account, folder, uid)
	if err != nil {
		return err
	}
	return s.store.SetFlags(account, folder, uid, withFlag(cached.Flags, flag, set))
}

func withFlag(flags []data.Flag, flag data.Flag, set bool) []data.Flag {
	out := make([]data.Flag, 0, len(flags)+1)
	for _, f := range flags {
		if f == flag {
			continue
		}
		out = append(out, f)
	}
	if set {
		out = append(out, flag)
	}
	return out
}

// messageTarget reads the common (account, folder, uid) tuple.
func (s *Server) messageTarget(req mcp.CallToolRequest) (string, string, uint32, *mcp.CallToolResult) {
	account, err := req.RequireString("account")
	if err != nil {
		return "", "", 0, mcp.NewToolResultErrorFromErr("account", err)
	}
	folder, err := req.RequireString("folder")
	if err != nil {
		return "", "", 0, mcp.NewToolResultErrorFromErr("folder", err)
	}
	uid, errRes := uidArg(req)
	if errRes != nil {
		return "", "", 0, errRes
	}
	return account, folder, uid, nil
}

func uidArg(req mcp.CallToolRequest) (uint32, *mcp.CallToolResult) {
	n, err := req.RequireInt("uid")
	if err != nil {
		return 0, mcp.NewToolResultErrorFromErr("uid", err)
	}
	if n <= 0 {
		return 0, mcp.NewToolResultError("uid must be a positive integer")
	}
	return uint32(n), nil
}

func uidsArg(req mcp.CallToolRequest) ([]uint32, *mcp.CallToolResult) {
	nums, err := req.RequireIntSlice("uids")
	if err != nil {
		return nil, mcp.NewToolResultErrorFromErr("uids", err)
	}
	if len(nums) == 0 {
		return nil, mcp.NewToolResultError("uids must contain at least one UID")
	}
	uids := make([]uint32, 0, len(nums))
	for _, n := range nums {
		if n <= 0 {
			return nil, mcp.NewToolResultError("uids must contain only positive integers")
		}
		uids = append(uids, uint32(n))
	}
	return uids, nil
}

func formatUIDs(uids []uint32) string {
	parts := make([]string, 0, len(uids))
	for _, uid := range uids {
		parts = append(parts, fmt.Sprint(uid))
	}
	return strings.Join(parts, ", ")
}
