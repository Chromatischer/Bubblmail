package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
)

func TestSendMessageToolRegisteredOnlyWhenWritable(t *testing.T) {
	cfg := &config.Config{}

	writableTools := New(cfg, nil, false).MCPServer().ListTools()
	if _, ok := writableTools["list_attachments"]; !ok {
		t.Fatal("list_attachments tool not registered")
	}
	if _, ok := writableTools["read_attachment"]; !ok {
		t.Fatal("read_attachment tool not registered")
	}
	sendTool, ok := writableTools["send_message"]
	if !ok {
		t.Fatal("send_message tool not registered in writable mode")
	}
	if sendTool.Tool.Annotations.ReadOnlyHint == nil || *sendTool.Tool.Annotations.ReadOnlyHint {
		t.Fatalf("send_message readOnlyHint = %#v, want false", sendTool.Tool.Annotations.ReadOnlyHint)
	}
	if sendTool.Tool.Annotations.DestructiveHint == nil || !*sendTool.Tool.Annotations.DestructiveHint {
		t.Fatalf("send_message destructiveHint = %#v, want true", sendTool.Tool.Annotations.DestructiveHint)
	}
	if !strings.Contains(sendTool.Tool.Description, "Sending is irreversible") {
		t.Fatalf("send_message description missing irreversible caution: %q", sendTool.Tool.Description)
	}

	readOnlyTools := New(cfg, nil, true).MCPServer().ListTools()
	if _, ok := readOnlyTools["send_message"]; ok {
		t.Fatal("send_message tool registered in read-only mode")
	}
	if _, ok := readOnlyTools["list_attachments"]; !ok {
		t.Fatal("list_attachments tool not registered in read-only mode")
	}
	if _, ok := readOnlyTools["read_attachment"]; !ok {
		t.Fatal("read_attachment tool not registered in read-only mode")
	}
}

func TestReadMessageIncludesFetchedAttachments(t *testing.T) {
	store := openMCPTestStore(t)
	msg := mcpTestMessage("work", "INBOX", 42)
	if err := store.UpsertMessages([]*data.Message{msg}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}

	srv := New(&config.Config{Cache: config.CacheConfig{Dir: t.TempDir()}}, store, true)
	fetchCalls := 0
	srv.fetchBody = func(account, folder string, uid uint32) (string, string, []data.Attachment, error) {
		fetchCalls++
		if account != "work" || folder != "INBOX" || uid != 42 {
			t.Fatalf("fetch target = %s/%s/%d", account, folder, uid)
		}
		return "plain body", "", []data.Attachment{
			{Filename: "invoice.pdf", ContentType: "application/pdf", Data: []byte("pdf")},
			{Filename: "notes.txt", ContentType: "text/plain", Data: []byte("hello")},
		}, nil
	}

	res, err := srv.handleReadMessage(context.Background(), mcpRequest(map[string]any{
		"account": "work",
		"folder":  "INBOX",
		"uid":     42,
	}))
	if err != nil {
		t.Fatalf("handleReadMessage error: %v", err)
	}
	got, ok := res.StructuredContent.(messageJSON)
	if !ok {
		t.Fatalf("StructuredContent = %T, want messageJSON", res.StructuredContent)
	}
	if got.Body != "plain body" {
		t.Fatalf("Body = %q", got.Body)
	}
	if fetchCalls != 1 {
		t.Fatalf("fetchCalls = %d, want 1", fetchCalls)
	}
	if len(got.Attachments) != 2 {
		t.Fatalf("attachments len = %d, want 2: %+v", len(got.Attachments), got.Attachments)
	}
	if got.Attachments[0].Index != 0 || got.Attachments[0].Filename != "invoice.pdf" ||
		got.Attachments[0].ContentType != "application/pdf" || got.Attachments[0].Size != 3 ||
		got.Attachments[0].Cached || got.Attachments[0].LocalPath != "" {
		t.Fatalf("first attachment mismatch: %+v", got.Attachments[0])
	}
}

func TestReadAttachmentCachesStablePathAndReadMessageUsesCachedMetadata(t *testing.T) {
	store := openMCPTestStore(t)
	msg := mcpTestMessage("work", "INBOX", 42)
	if err := store.UpsertMessages([]*data.Message{msg}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}
	if err := store.UpsertBody(msg.ID, "cached body", ""); err != nil {
		t.Fatalf("UpsertBody: %v", err)
	}

	cacheDir := t.TempDir()
	srv := New(&config.Config{Cache: config.CacheConfig{Dir: cacheDir}}, store, true)
	srv.fetchBody = func(account, folder string, uid uint32) (string, string, []data.Attachment, error) {
		return "ignored", "", []data.Attachment{
			{Filename: "../invoice.pdf", ContentType: "application/pdf", Data: []byte("pdf bytes")},
		}, nil
	}

	res, err := srv.handleReadAttachment(context.Background(), mcpRequest(map[string]any{
		"account": "work",
		"folder":  "INBOX",
		"uid":     42,
		"index":   0,
	}))
	if err != nil {
		t.Fatalf("handleReadAttachment error: %v", err)
	}
	att, ok := res.StructuredContent.(attachmentJSON)
	if !ok {
		t.Fatalf("StructuredContent = %T, want attachmentJSON", res.StructuredContent)
	}
	if !att.Cached || att.LocalPath == "" || !filepath.IsAbs(att.LocalPath) {
		t.Fatalf("cached attachment path mismatch: %+v", att)
	}
	if filepath.Base(att.LocalPath) != "42-0-invoice.pdf" {
		t.Fatalf("cached filename = %q", filepath.Base(att.LocalPath))
	}
	if !strings.HasPrefix(att.LocalPath, filepath.Join(cacheDir, "attachments")+string(os.PathSeparator)) {
		t.Fatalf("cached path %q is not under cache dir %q", att.LocalPath, cacheDir)
	}
	b, err := os.ReadFile(att.LocalPath)
	if err != nil {
		t.Fatalf("ReadFile cached attachment: %v", err)
	}
	if string(b) != "pdf bytes" {
		t.Fatalf("cached bytes = %q", b)
	}

	res2, err := srv.handleReadAttachment(context.Background(), mcpRequest(map[string]any{
		"account":  "work",
		"folder":   "INBOX",
		"uid":      42,
		"filename": "invoice.pdf",
	}))
	if err != nil {
		t.Fatalf("handleReadAttachment by filename error: %v", err)
	}
	att2 := res2.StructuredContent.(attachmentJSON)
	if att2.LocalPath != att.LocalPath {
		t.Fatalf("second LocalPath = %q, want %q", att2.LocalPath, att.LocalPath)
	}

	srv.fetchBody = func(account, folder string, uid uint32) (string, string, []data.Attachment, error) {
		t.Fatalf("cached read_message should not fetch body for %s/%s/%d", account, folder, uid)
		return "", "", nil, nil
	}
	res3, err := srv.handleReadMessage(context.Background(), mcpRequest(map[string]any{
		"account": "work",
		"folder":  "INBOX",
		"uid":     42,
	}))
	if err != nil {
		t.Fatalf("handleReadMessage error: %v", err)
	}
	got := res3.StructuredContent.(messageJSON)
	if len(got.Attachments) != 1 || !got.Attachments[0].Cached || got.Attachments[0].LocalPath != att.LocalPath {
		t.Fatalf("cached read_message attachments mismatch: %+v", got.Attachments)
	}
}

func openMCPTestStore(t *testing.T) *cache.Store {
	t.Helper()
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func mcpTestMessage(account, folder string, uid uint32) *data.Message {
	return &data.Message{
		AccountName: account,
		FolderName:  folder,
		UID:         uid,
		MessageID:   "message-id",
		Subject:     "Subject",
		Date:        time.Unix(1_700_000_000, 0),
		From:        []data.Address{{Name: "Sender", Address: "sender@example.com"}},
		To:          []data.Address{{Name: "Recipient", Address: "recipient@example.com"}},
	}
}

func mcpRequest(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: args},
	}
}
