package cache

import (
	"database/sql"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/embeddings"
	_ "github.com/mattn/go-sqlite3"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func testMessage(account, folder string, uid uint32, subject, snippet string, date time.Time) *data.Message {
	return &data.Message{
		AccountName: account,
		FolderName:  folder,
		UID:         uid,
		MessageID:   subject + "-id",
		Subject:     subject,
		Snippet:     snippet,
		Date:        date,
		From:        []data.Address{{Name: "Sender", Address: "sender@example.com"}},
		To:          []data.Address{{Name: "Recipient", Address: "recipient@example.com"}},
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table','view') AND name = ?`, name).Scan(&count)
	if err != nil {
		t.Fatalf("checking table %s: %v", name, err)
	}
	return count == 1
}

func TestOpenAppliesSchema(t *testing.T) {
	store := openTestStore(t)

	tables := []string{
		"messages",
		"bodies",
		"folders",
		"message_embeddings",
		"folder_embeddings",
		"messages_fts",
		"message_categories",
		"suggested_events",
	}
	for _, name := range tables {
		if !tableExists(t, store.db, name) {
			t.Fatalf("expected table %q to exist", name)
		}
	}

	rows, err := store.db.Query(`PRAGMA table_info(suggested_events)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	foundRejected := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan pragma row: %v", err)
		}
		if name == "rejected" {
			foundRejected = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("pragma rows: %v", err)
	}
	if !foundRejected {
		t.Fatal("expected suggested_events.rejected column to exist")
	}
}

func TestOpenRemovesStaleFTS5Database(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cache.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE messages_fts (subject TEXT)`); err != nil {
		_ = db.Close()
		t.Fatalf("create fake messages_fts table: %v", err)
	}
	if _, err := db.Exec(`PRAGMA writable_schema=ON`); err != nil {
		_ = db.Close()
		t.Fatalf("enable writable_schema: %v", err)
	}
	if _, err := db.Exec(`UPDATE sqlite_master SET sql = 'CREATE VIRTUAL TABLE messages_fts USING fts5(subject, snippet, body_text)' WHERE name = 'messages_fts'`); err != nil {
		_ = db.Close()
		t.Fatalf("rewrite sqlite_master ddl: %v", err)
	}
	if _, err := db.Exec(`PRAGMA writable_schema=OFF`); err != nil {
		_ = db.Close()
		t.Fatalf("disable writable_schema: %v", err)
	}
	_ = db.Close()

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() after stale fts5 db: %v", err)
	}
	defer store.Close()

	var ddl string
	if err := store.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='messages_fts'`).Scan(&ddl); err != nil {
		t.Fatalf("query messages_fts ddl: %v", err)
	}
	if ddl == "" || !tableExists(t, store.db, "messages") {
		t.Fatal("expected schema to be recreated after stale fts5 removal")
	}
	if !containsLower(ddl, "fts4") {
		t.Fatalf("expected recreated messages_fts to use fts4, got %q", ddl)
	}
}

func TestSearchLocalIndexesSubjectSnippetAndBody(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0)

	msgA := testMessage("work", "INBOX", 1, "Quarterly invoice", "first note", now.Add(-time.Hour))
	msgB := testMessage("work", "INBOX", 2, "Roadmap", "contains needle snippet", now)
	if err := store.UpsertMessages([]*data.Message{msgA, msgB}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}

	results, err := store.SearchLocal("invoice")
	if err != nil {
		t.Fatalf("SearchLocal(invoice): %v", err)
	}
	if len(results) != 1 || results[0].UID != 1 {
		t.Fatalf("SearchLocal(invoice) = %+v, want uid 1", results)
	}

	results, err = store.SearchLocal("needle")
	if err != nil {
		t.Fatalf("SearchLocal(needle): %v", err)
	}
	if len(results) != 1 || results[0].UID != 2 {
		t.Fatalf("SearchLocal(needle) = %+v, want uid 2", results)
	}

	if err := store.UpsertBody(msgA.ID, "hidden body token", ""); err != nil {
		t.Fatalf("UpsertBody: %v", err)
	}
	results, err = store.SearchLocal("hidden")
	if err != nil {
		t.Fatalf("SearchLocal(hidden): %v", err)
	}
	if len(results) != 1 || results[0].UID != 1 {
		t.Fatalf("SearchLocal(hidden) = %+v, want uid 1", results)
	}
	if results[0].Date != msgA.Date {
		t.Fatalf("got date %v, want %v", results[0].Date, msgA.Date)
	}
}

func TestUpsertBodyRefreshesFTSBodyText(t *testing.T) {
	store := openTestStore(t)
	msg := testMessage("work", "INBOX", 1, "Subject", "Snippet", time.Unix(1_700_000_000, 0))
	if err := store.UpsertMessages([]*data.Message{msg}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}

	results, err := store.SearchLocal("bodyterm")
	if err != nil {
		t.Fatalf("SearchLocal before body upsert: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no bodyterm hits before body upsert, got %d", len(results))
	}

	if err := store.UpsertBody(msg.ID, "bodyterm appears here", ""); err != nil {
		t.Fatalf("first UpsertBody: %v", err)
	}
	results, err = store.SearchLocal("bodyterm")
	if err != nil {
		t.Fatalf("SearchLocal after first body upsert: %v", err)
	}
	if len(results) != 1 || results[0].UID != msg.UID {
		t.Fatalf("expected bodyterm to match uid %d, got %+v", msg.UID, results)
	}

	if err := store.UpsertBody(msg.ID, "replacement text only", ""); err != nil {
		t.Fatalf("second UpsertBody: %v", err)
	}
	results, err = store.SearchLocal("replacement")
	if err != nil {
		t.Fatalf("SearchLocal after body replacement: %v", err)
	}
	if len(results) != 1 || results[0].UID != msg.UID {
		t.Fatalf("expected replacement to match uid %d, got %+v", msg.UID, results)
	}
	bodyText, _, err := store.GetBody(msg.ID)
	if err != nil {
		t.Fatalf("GetBody after replacement: %v", err)
	}
	if bodyText != "replacement text only" {
		t.Fatalf("body text = %q, want replacement text only", bodyText)
	}
	var bodyTextRow string
	if err := store.db.QueryRow(`SELECT body_text FROM bodies WHERE message_id = ?`, msg.ID).Scan(&bodyTextRow); err != nil {
		t.Fatalf("query bodies body_text: %v", err)
	}
	if bodyTextRow != "replacement text only" {
		t.Fatalf("stored body_text = %q, want replacement text only", bodyTextRow)
	}
}

func TestSearchLocalFilteredRespectsAccountFolderAndLimit(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0)
	msgs := []*data.Message{
		testMessage("work", "INBOX", 1, "invoice one", "", now.Add(-2*time.Hour)),
		testMessage("work", "Archive", 2, "invoice two", "", now.Add(-time.Hour)),
		testMessage("personal", "INBOX", 3, "invoice three", "", now),
	}
	if err := store.UpsertMessages(msgs); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}

	results, err := store.SearchLocalFiltered("invoice", "work", "INBOX", 10)
	if err != nil {
		t.Fatalf("SearchLocalFiltered exact scope: %v", err)
	}
	if len(results) != 1 || results[0].AccountName != "work" || results[0].FolderName != "INBOX" {
		t.Fatalf("unexpected exact-scope results: %+v", results)
	}

	results, err = store.SearchLocalFiltered("invoice", "work", "", 1)
	if err != nil {
		t.Fatalf("SearchLocalFiltered account scope: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected limit=1 result, got %d", len(results))
	}
	if results[0].UID != 2 {
		t.Fatalf("expected newest work result uid 2, got uid %d", results[0].UID)
	}
}

func TestUpsertMessagesConflictUpdatePreservesIDAndUpdatesFields(t *testing.T) {
	store := openTestStore(t)
	original := testMessage("work", "INBOX", 9, "Initial subject", "first snippet", time.Unix(1_700_000_000, 0))
	original.Flags = []data.Flag{data.FlagSeen}
	original.ThreadID = "thread-a"
	if err := store.UpsertMessages([]*data.Message{original}); err != nil {
		t.Fatalf("UpsertMessages original: %v", err)
	}
	originalID := original.ID

	updated := testMessage("work", "INBOX", 9, "Updated subject", "updated snippet", time.Unix(1_700_000_100, 0))
	updated.Flags = []data.Flag{data.FlagFlagged}
	updated.ThreadID = "thread-b"
	if err := store.UpsertMessages([]*data.Message{updated}); err != nil {
		t.Fatalf("UpsertMessages updated: %v", err)
	}
	if updated.ID != originalID {
		t.Fatalf("updated message ID = %d, want %d", updated.ID, originalID)
	}

	got, err := store.GetMessageByUID("work", "INBOX", 9)
	if err != nil {
		t.Fatalf("GetMessageByUID: %v", err)
	}
	if got.ID != originalID || got.Subject != "Updated subject" || got.Snippet != "updated snippet" || got.ThreadID != "thread-b" {
		t.Fatalf("updated row mismatch: %+v", got)
	}
	if !got.IsStarred() || got.IsRead() {
		t.Fatalf("expected starred unread message after update, got flags %+v", got.Flags)
	}

	results, err := store.SearchLocal("thread-b")
	if err != nil {
		t.Fatalf("SearchLocal(updated subject): %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected thread ID changes to stay out of FTS, got %+v", results)
	}
	results, err = store.SearchLocal("first")
	if err != nil {
		t.Fatalf("SearchLocal(initial subject): %v", err)
	}
	if len(results) != 1 || results[0].ID != originalID {
		t.Fatalf("expected original FTS entry to remain until explicitly refreshed, got %+v", results)
	}
	results, err = store.SearchLocal("updated")
	if err != nil {
		t.Fatalf("SearchLocal(updated) after conflict update: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected subject/snippet update not to refresh FTS automatically, got %+v", results)
	}
}

func TestGetMessagesFilteredUnreadOnlyAndSetFlags(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0)
	msg1 := testMessage("work", "INBOX", 1, "Unread", "", now)
	msg2 := testMessage("work", "INBOX", 2, "Seen", "", now.Add(time.Minute))
	msg2.Flags = []data.Flag{data.FlagSeen}
	if err := store.UpsertMessages([]*data.Message{msg1, msg2}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}

	unread, err := store.GetMessagesFiltered("work", "INBOX", true, 10)
	if err != nil {
		t.Fatalf("GetMessagesFiltered unreadOnly: %v", err)
	}
	if len(unread) != 1 || unread[0].UID != 1 {
		t.Fatalf("unexpected unread set: %+v", unread)
	}

	if err := store.SetFlags("work", "INBOX", 1, []data.Flag{data.FlagSeen}); err != nil {
		t.Fatalf("SetFlags seen: %v", err)
	}
	unread, err = store.GetMessagesFiltered("work", "INBOX", true, 10)
	if err != nil {
		t.Fatalf("GetMessagesFiltered after SetFlags seen: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("expected no unread messages after marking seen, got %+v", unread)
	}

	if err := store.SetFlags("work", "INBOX", 2, []data.Flag{data.FlagFlagged}); err != nil {
		t.Fatalf("SetFlags flagged: %v", err)
	}
	unread, err = store.GetMessagesFiltered("work", "INBOX", true, 10)
	if err != nil {
		t.Fatalf("GetMessagesFiltered after clearing seen: %v", err)
	}
	if len(unread) != 1 || unread[0].UID != 2 || !unread[0].IsStarred() {
		t.Fatalf("expected uid 2 to become unread starred, got %+v", unread)
	}
}

func TestGetMessagesFilteredLimitZeroMeansUnlimited(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0)
	msg1 := testMessage("work", "INBOX", 1, "First", "", now)
	msg2 := testMessage("work", "INBOX", 2, "Second", "", now.Add(time.Minute))
	if err := store.UpsertMessages([]*data.Message{msg1, msg2}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}

	msgs, err := store.GetMessagesFiltered("work", "INBOX", false, 0)
	if err != nil {
		t.Fatalf("GetMessagesFiltered unlimited: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("GetMessagesFiltered unlimited returned %d messages, want 2", len(msgs))
	}
}

func TestListBodiesForEmbeddingAndPrefetchCandidates(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0)
	msg1 := testMessage("work", "INBOX", 1, "First", "", now)
	msg2 := testMessage("work", "INBOX", 2, "Second", "", now.Add(time.Minute))
	msg3 := testMessage("work", "Archive", 3, "Third", "", now.Add(2*time.Minute))
	if err := store.UpsertMessages([]*data.Message{msg1, msg2, msg3}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}
	if err := store.UpsertBody(msg1.ID, "body one", ""); err != nil {
		t.Fatalf("UpsertBody msg1: %v", err)
	}
	if err := store.UpsertBody(msg2.ID, "body two", ""); err != nil {
		t.Fatalf("UpsertBody msg2: %v", err)
	}

	prefetch, err := store.ListBodyPrefetchCandidates("work", 10)
	if err != nil {
		t.Fatalf("ListBodyPrefetchCandidates: %v", err)
	}
	if len(prefetch) != 1 || prefetch[0].UID != 3 {
		t.Fatalf("expected only uid 3 as prefetch candidate, got %+v", prefetch)
	}

	model := "test-model"
	msgs, bodies, err := store.ListBodiesForEmbedding("work", model, false)
	if err != nil {
		t.Fatalf("ListBodiesForEmbedding(force=false): %v", err)
	}
	if len(msgs) != 2 || len(bodies) != 2 {
		t.Fatalf("expected 2 embedding candidates, got %d msgs and %d bodies", len(msgs), len(bodies))
	}
	if msgs[0].UID != 2 || msgs[1].UID != 1 {
		t.Fatalf("expected descending date order [2 1], got [%d %d]", msgs[0].UID, msgs[1].UID)
	}

	vec := []float32{1, 2, 3}
	if err := store.SaveEmbedding(msg2.ID, model, vec, embeddings.VectorNorm(vec), "hash-2"); err != nil {
		t.Fatalf("SaveEmbedding: %v", err)
	}
	msgs, bodies, err = store.ListBodiesForEmbedding("work", model, false)
	if err != nil {
		t.Fatalf("ListBodiesForEmbedding after embedding save: %v", err)
	}
	if len(msgs) != 1 || msgs[0].UID != 1 || len(bodies) != 1 || bodies[0] != "body one" {
		t.Fatalf("expected only uid 1 remaining, got msgs=%+v bodies=%+v", msgs, bodies)
	}

	msgs, bodies, err = store.ListBodiesForEmbedding("work", model, true)
	if err != nil {
		t.Fatalf("ListBodiesForEmbedding(force=true): %v", err)
	}
	if len(msgs) != 2 || len(bodies) != 2 {
		t.Fatalf("expected both cached bodies with force=true, got %d msgs and %d bodies", len(msgs), len(bodies))
	}
}

func TestBuildFolderEmbeddingsStoresCentroids(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0)
	msgs := []*data.Message{
		testMessage("work", "INBOX", 1, "A", "", now),
		testMessage("work", "INBOX", 2, "B", "", now.Add(time.Minute)),
		testMessage("work", "Archive", 3, "C", "", now.Add(2*time.Minute)),
	}
	if err := store.UpsertMessages(msgs); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}
	if err := store.UpsertFolders("work", []*data.Folder{{AccountName: "work", Name: "INBOX", DisplayName: "INBOX"}, {AccountName: "work", Name: "Archive", DisplayName: "Archive"}}); err != nil {
		t.Fatalf("UpsertFolders: %v", err)
	}

	model := "test-model"
	vec1 := []float32{1, 0}
	vec2 := []float32{0, 1}
	vec3 := []float32{2, 0}
	if err := store.SaveEmbedding(msgs[0].ID, model, vec1, embeddings.VectorNorm(vec1), "h1"); err != nil {
		t.Fatalf("SaveEmbedding vec1: %v", err)
	}
	if err := store.SaveEmbedding(msgs[1].ID, model, vec2, embeddings.VectorNorm(vec2), "h2"); err != nil {
		t.Fatalf("SaveEmbedding vec2: %v", err)
	}
	if err := store.SaveEmbedding(msgs[2].ID, model, vec3, embeddings.VectorNorm(vec3), "h3"); err != nil {
		t.Fatalf("SaveEmbedding vec3: %v", err)
	}

	if err := store.BuildFolderEmbeddings("work", model, 10); err != nil {
		t.Fatalf("BuildFolderEmbeddings: %v", err)
	}

	folders, vectors, _, counts, err := store.ListFolderEmbeddings("work", model)
	if err != nil {
		t.Fatalf("ListFolderEmbeddings: %v", err)
	}
	if len(folders) != 2 || len(vectors) != 2 || len(counts) != 2 {
		t.Fatalf("expected 2 folder embeddings, got folders=%d vectors=%d counts=%d", len(folders), len(vectors), len(counts))
	}

	got := map[string]struct {
		vec   []float32
		count int
	}{
		folders[0].Name: {vec: vectors[0], count: counts[0]},
		folders[1].Name: {vec: vectors[1], count: counts[1]},
	}

	inbox := got["INBOX"]
	if inbox.count != 2 || len(inbox.vec) != 2 || math.Abs(float64(inbox.vec[0]-0.5)) > 1e-6 || math.Abs(float64(inbox.vec[1]-0.5)) > 1e-6 {
		t.Fatalf("unexpected INBOX centroid/count: %+v", inbox)
	}
	archive := got["Archive"]
	if archive.count != 1 || len(archive.vec) != 2 || math.Abs(float64(archive.vec[0]-2)) > 1e-6 || math.Abs(float64(archive.vec[1])) > 1e-6 {
		t.Fatalf("unexpected Archive centroid/count: %+v", archive)
	}
}

func containsLower(s, want string) bool {
	return strings.Contains(strings.ToLower(s), want)
}
