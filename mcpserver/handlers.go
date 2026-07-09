package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/embeddings"
	imaplib "github.com/bubblmail/bubblmail/imap"
	smtplib "github.com/bubblmail/bubblmail/smtp"
	"github.com/bubblmail/bubblmail/util"
)

type bodyFetcher func(account, folder string, uid uint32) (string, string, []data.Attachment, error)

type attachmentJSON struct {
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	Cached      bool   `json:"cached"`
	LocalPath   string `json:"local_path,omitempty"`
}

type attachmentListResult struct {
	Attachments []attachmentJSON `json:"attachments"`
	Count       int              `json:"count"`
}

type cachedAttachmentMeta struct {
	Account     string `json:"account"`
	Folder      string `json:"folder"`
	UID         uint32 `json:"uid"`
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	LocalPath   string `json:"local_path"`
}

// messageJSON is the wire shape for a message summary returned by tools.
type messageJSON struct {
	Account     string           `json:"account"`
	Folder      string           `json:"folder"`
	UID         uint32           `json:"uid"`
	ID          int64            `json:"id"`
	Subject     string           `json:"subject"`
	From        []string         `json:"from"`
	To          []string         `json:"to,omitempty"`
	Date        string           `json:"date"`
	Snippet     string           `json:"snippet,omitempty"`
	Read        bool             `json:"read"`
	Starred     bool             `json:"starred"`
	Tags        []string         `json:"tags,omitempty"`
	ThreadID    string           `json:"thread_id,omitempty"`
	Score       float32          `json:"score,omitempty"`
	Body        string           `json:"body,omitempty"`
	Attachments []attachmentJSON `json:"attachments"`
}

func toMessageJSON(m *data.Message) messageJSON {
	return messageJSON{
		Account:     m.AccountName,
		Folder:      m.FolderName,
		UID:         m.UID,
		ID:          m.ID,
		Subject:     m.Subject,
		From:        addressStrings(m.From),
		To:          addressStrings(m.To),
		Date:        m.Date.Format("2006-01-02 15:04"),
		Snippet:     m.Snippet,
		Read:        m.IsRead(),
		Starred:     m.IsStarred(),
		Tags:        m.Tags,
		ThreadID:    m.ThreadID,
		Attachments: []attachmentJSON{},
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
	var attachments []data.Attachment
	fetched := false
	if bodyText == "" && bodyHTML == "" {
		bodyText, bodyHTML, attachments, err = s.fetchMessageBody(account, folder, uid)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("fetching body", err), nil
		}
		if err := s.store.UpsertBody(msg.ID, bodyText, bodyHTML); err != nil {
			return mcp.NewToolResultErrorFromErr("caching body", err), nil
		}
		fetched = true
	}
	body := bodyText
	if strings.TrimSpace(body) == "" {
		body = bodyHTML
	}
	mj := toMessageJSON(msg)
	mj.Body = body
	if fetched {
		mj.Attachments = s.attachmentsJSON(account, folder, uid, attachments)
	} else {
		// Attachment metadata/blobs are not persisted in SQLite. On the cached
		// body fast path, avoid an IMAP fetch just to prove there are no
		// attachments; only report attachments already materialized in the MCP
		// attachment cache by read_attachment.
		cached, err := s.cachedAttachmentsJSON(account, folder, uid)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("reading cached attachments", err), nil
		}
		mj.Attachments = cached
	}
	return mcp.NewToolResultJSON(mj)
}

func (s *Server) handleListAttachments(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, folder, uid, errRes := s.messageTarget(req)
	if errRes != nil {
		return errRes, nil
	}
	if _, err := s.store.GetMessageByUID(account, folder, uid); err != nil {
		return mcp.NewToolResultErrorFromErr("finding message", err), nil
	}
	// IMAP FetchBodySync is currently the only cheap-enough source of
	// attachment metadata; SQLite deliberately stores only body text/HTML.
	// This tool discards the body so clients can enumerate attachments without
	// receiving message body content in the MCP response.
	_, _, attachments, err := s.fetchMessageBody(account, folder, uid)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("fetching attachments", err), nil
	}
	out := s.attachmentsJSON(account, folder, uid, attachments)
	return mcp.NewToolResultJSON(attachmentListResult{Attachments: out, Count: len(out)})
}

func (s *Server) handleReadAttachment(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, folder, uid, errRes := s.messageTarget(req)
	if errRes != nil {
		return errRes, nil
	}
	if _, err := s.store.GetMessageByUID(account, folder, uid); err != nil {
		return mcp.NewToolResultErrorFromErr("finding message", err), nil
	}
	index := req.GetInt("index", -1)
	filename := strings.TrimSpace(req.GetString("filename", ""))
	if index >= 0 && filename != "" {
		return mcp.NewToolResultError("provide either index or filename, not both"), nil
	}
	if index < 0 && filename == "" {
		return mcp.NewToolResultError("index or filename is required"), nil
	}

	_, _, attachments, err := s.fetchMessageBody(account, folder, uid)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("fetching attachment", err), nil
	}
	if filename != "" {
		index, err = attachmentIndexByFilename(attachments, filename)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("selecting attachment", err), nil
		}
	}
	if index < 0 || index >= len(attachments) {
		return mcp.NewToolResultError(fmt.Sprintf("attachment index %d out of range (count %d)", index, len(attachments))), nil
	}

	att := attachments[index]
	path, err := s.cacheAttachment(account, folder, uid, index, att)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("caching attachment", err), nil
	}
	out := attachmentJSON{
		Index:       index,
		Filename:    att.Filename,
		ContentType: att.ContentType,
		Size:        len(att.Data),
		Cached:      true,
		LocalPath:   path,
	}
	return mcp.NewToolResultJSON(out)
}

func (s *Server) fetchMessageBody(account, folder string, uid uint32) (string, string, []data.Attachment, error) {
	if s.fetchBody != nil {
		return s.fetchBody(account, folder, uid)
	}
	client, err := s.connect(account)
	if err != nil {
		return "", "", nil, fmt.Errorf("connecting: %w", err)
	}
	bodyText, bodyHTML, attachments, err := client.FetchBodySync(folder, uid)
	closeErr := client.Close()
	if err != nil {
		return "", "", nil, err
	}
	if closeErr != nil {
		return "", "", nil, fmt.Errorf("closing connection: %w", closeErr)
	}
	return bodyText, bodyHTML, attachments, nil
}

func (s *Server) attachmentsJSON(account, folder string, uid uint32, attachments []data.Attachment) []attachmentJSON {
	out := make([]attachmentJSON, 0, len(attachments))
	for i, att := range attachments {
		item := attachmentJSON{
			Index:       i,
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Size:        len(att.Data),
		}
		if path, err := s.attachmentPath(account, folder, uid, i, att.Filename); err == nil {
			if _, err := os.Stat(path); err == nil {
				item.Cached = true
				item.LocalPath = path
			}
		}
		out = append(out, item)
	}
	return out
}

func (s *Server) cachedAttachmentsJSON(account, folder string, uid uint32) ([]attachmentJSON, error) {
	dir, err := s.attachmentDir(account, folder)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []attachmentJSON{}, nil
	}
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("%d-", uid)
	out := []attachmentJSON{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".meta.json") {
			continue
		}
		meta, err := readCachedAttachmentMeta(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if meta.Account != account || meta.Folder != folder || meta.UID != uid {
			continue
		}
		out = append(out, attachmentJSON{
			Index:       meta.Index,
			Filename:    meta.Filename,
			ContentType: meta.ContentType,
			Size:        meta.Size,
			Cached:      true,
			LocalPath:   meta.LocalPath,
		})
	}
	return out, nil
}

func readCachedAttachmentMeta(path string) (cachedAttachmentMeta, error) {
	var meta cachedAttachmentMeta
	b, err := os.ReadFile(path)
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return meta, fmt.Errorf("decoding %s: %w", path, err)
	}
	return meta, nil
}

func (s *Server) cacheAttachment(account, folder string, uid uint32, index int, att data.Attachment) (string, error) {
	dir, err := s.attachmentDir(account, folder)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path, err := s.attachmentPath(account, folder, uid, index, att.Filename)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, att.Data, 0600); err != nil {
		return "", err
	}
	meta := cachedAttachmentMeta{
		Account:     account,
		Folder:      folder,
		UID:         uid,
		Index:       index,
		Filename:    att.Filename,
		ContentType: att.ContentType,
		Size:        len(att.Data),
		LocalPath:   path,
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path+".meta.json", metaBytes, 0600); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Server) attachmentPath(account, folder string, uid uint32, index int, filename string) (string, error) {
	dir, err := s.attachmentDir(account, folder)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%d-%d-%s", uid, index, util.SafeAttachmentFilename(filename))
	return filepath.Join(dir, name), nil
}

func (s *Server) attachmentDir(account, folder string) (string, error) {
	cacheDir, err := s.cacheDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "attachments", safePathSegment(account), safePathSegment(folder)), nil
}

func safePathSegment(s string) string {
	s = strings.NewReplacer("/", "_", "\\", "_").Replace(s)
	return util.SafeAttachmentFilename(s)
}

func attachmentIndexByFilename(attachments []data.Attachment, filename string) (int, error) {
	matches := []int{}
	safeFilename := util.SafeAttachmentFilename(filename)
	for i, att := range attachments {
		if att.Filename == filename || util.SafeAttachmentFilename(att.Filename) == safeFilename {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return -1, fmt.Errorf("no attachment named %q", filename)
	case 1:
		return matches[0], nil
	default:
		return -1, fmt.Errorf("multiple attachments named %q; use index", filename)
	}
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

func (s *Server) handleSendMessage(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	account, err := req.RequireString("account")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("account", err), nil
	}
	acct, err := s.account(account)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("account", err), nil
	}
	toValues, err := req.RequireStringSlice("to")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("to", err), nil
	}
	to, err := smtplib.ParseAddresses(toValues)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("to", err), nil
	}
	if len(to) == 0 {
		return mcp.NewToolResultError("to must contain at least one recipient"), nil
	}
	cc, err := smtplib.ParseAddresses(req.GetStringSlice("cc", nil))
	if err != nil {
		return mcp.NewToolResultErrorFromErr("cc", err), nil
	}
	subject, err := req.RequireString("subject")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("subject", err), nil
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return mcp.NewToolResultError("subject is required"), nil
	}
	body, err := req.RequireString("body")
	if err != nil {
		return mcp.NewToolResultErrorFromErr("body", err), nil
	}

	attachmentPaths := req.GetStringSlice("attachments", nil)
	attachments := make([]smtplib.Attachment, 0, len(attachmentPaths))
	for _, path := range attachmentPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		attachments = append(attachments, smtplib.Attachment{
			Path:     path,
			Filename: filepath.Base(path),
		})
	}

	draft := &smtplib.ComposedMessage{
		From:        data.Address{Address: acct.Username},
		To:          to,
		CC:          cc,
		Subject:     subject,
		Body:        body,
		Attachments: attachments,
	}
	if err := smtplib.Send(acct, draft); err != nil {
		return mcp.NewToolResultErrorFromErr("sending message", err), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Sent to %s (subject: %s).", smtplib.FormatRecipientList(sendRecipients(draft)), subject)), nil
}

func sendRecipients(draft *smtplib.ComposedMessage) []data.Address {
	if draft == nil {
		return nil
	}
	out := make([]data.Address, 0, len(draft.To)+len(draft.CC))
	out = append(out, draft.To...)
	out = append(out, draft.CC...)
	return out
}

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
