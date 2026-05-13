package cache

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/embeddings"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

// Store is the local SQLite cache for messages, folders, and tags.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at dir/cache.db.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating cache dir: %w", err)
	}
	dbPath := filepath.Join(dir, "cache.db")
	if err := removeStaleFTS5DB(dbPath); err != nil {
		return nil, fmt.Errorf("migrating cache: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	s := &Store{db: db}
	if err := s.applySchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	return s, nil
}

// removeStaleFTS5DB removes the cache database if it was built with the fts5
// module, which go-sqlite3 does not compile by default. The cache is not a
// source of truth (mail lives on the IMAP server), so deletion is safe.
func removeStaleFTS5DB(dbPath string) error {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil // file doesn't exist yet
	}
	var ftsDDL string
	err = db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='messages_fts'`).Scan(&ftsDDL)
	db.Close()
	if err != nil || !strings.Contains(strings.ToLower(ftsDDL), "fts5") {
		return nil // no stale fts5 table
	}
	return os.Remove(dbPath)
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) applySchema() error {
	// Migration: drop stale FTS4 triggers if they exist. The delete/update
	// triggers can reject normal message deletes with "SQL logic error" on
	// FTS4 external-content tables. Search joins FTS rows back to messages, so
	// orphaned FTS rows from cache deletion are not returned.
	if _, err := s.db.Exec(`DROP TRIGGER IF EXISTS messages_fts_delete`); err != nil {
		return fmt.Errorf("dropping stale fts_delete trigger: %w", err)
	}
	// Migration: drop the FTS4 update trigger if it exists. It re-inserted with
	// the same docid after a 'delete', which FTS4 external-content tables reject
	// with "sql logic error". Email content (subject/snippet/body) never changes
	// after initial sync, so the trigger was unnecessary to begin with.
	if _, err := s.db.Exec(`DROP TRIGGER IF EXISTS messages_fts_update`); err != nil {
		return fmt.Errorf("dropping stale fts_update trigger: %w", err)
	}
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return err
	}
	// Migration: add rejected column to suggested_events if it doesn't exist yet.
	if _, err := s.db.Exec(`ALTER TABLE suggested_events ADD COLUMN rejected INTEGER NOT NULL DEFAULT 0`); err != nil {
		// Ignore "duplicate column" errors — column already exists.
		if !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migrating suggested_events.rejected: %w", err)
		}
	}
	return nil
}

// UpsertMessages inserts or updates messages in the cache.
func (s *Store) UpsertMessages(msgs []*data.Message) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO messages
			(account_name, folder_name, uid, message_id, in_reply_to, refs,
			 subject, from_addr, to_addr, cc_addr, date, flags, size, snippet, thread_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(account_name, folder_name, uid) DO UPDATE SET
			message_id   = excluded.message_id,
			in_reply_to  = excluded.in_reply_to,
			refs         = excluded.refs,
			subject      = excluded.subject,
			from_addr    = excluded.from_addr,
			to_addr      = excluded.to_addr,
			cc_addr      = excluded.cc_addr,
			date         = excluded.date,
			flags        = excluded.flags,
			size         = excluded.size,
			snippet      = excluded.snippet,
			thread_id    = excluded.thread_id
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, m := range msgs {
		fromJSON, _ := json.Marshal(m.From)
		toJSON, _ := json.Marshal(m.To)
		ccJSON, _ := json.Marshal(m.CC)
		flags := encodeFlags(m.Flags)
		refs := strings.Join(m.References, " ")

		res, err := stmt.Exec(
			m.AccountName, m.FolderName, m.UID, m.MessageID, m.InReplyTo, refs,
			m.Subject, string(fromJSON), string(toJSON), string(ccJSON),
			m.Date.Unix(), flags, m.Size, m.Snippet, m.ThreadID,
		)
		if err != nil {
			return err
		}
		if id, err := res.LastInsertId(); err == nil && id > 0 {
			m.ID = id
		} else {
			// ON CONFLICT DO UPDATE: SQLite returns 0 for last_insert_rowid on
			// a pure update. Look up the existing row ID explicitly.
			_ = tx.QueryRow(
				`SELECT id FROM messages WHERE account_name=? AND folder_name=? AND uid=?`,
				m.AccountName, m.FolderName, m.UID,
			).Scan(&m.ID)
		}
	}
	return tx.Commit()
}

// UpsertBody stores the body text/HTML for a message identified by DB id.
func (s *Store) UpsertBody(msgID int64, bodyText, bodyHTML string) error {
	_, err := s.db.Exec(`
		INSERT INTO bodies (message_id, body_text, body_html)
		VALUES (?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			body_text = excluded.body_text,
			body_html = excluded.body_html
	`, msgID, bodyText, bodyHTML)
	if err != nil {
		return err
	}
	// Refresh FTS index entry with updated body_text
	_, _ = s.db.Exec(`
		INSERT INTO messages_fts(messages_fts, docid, subject, snippet, body_text)
		SELECT 'delete', id, subject, snippet, '' FROM messages WHERE id = ?
	`, msgID)
	_, err = s.db.Exec(`
		INSERT INTO messages_fts(docid, subject, snippet, body_text)
		SELECT id, subject, snippet, ? FROM messages WHERE id = ?
	`, bodyText, msgID)
	return err
}

// SaveEmbedding stores an embedding vector for a message.
func (s *Store) SaveEmbedding(msgID int64, model string, vector []float32, norm float32, contentHash string) error {
	encoded, err := embeddings.EncodeVector(vector)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO message_embeddings (message_id, model, vector, norm, content_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id, model) DO UPDATE SET
			vector = excluded.vector,
			norm = excluded.norm,
			content_hash = excluded.content_hash,
			updated_at = excluded.updated_at
	`, msgID, model, encoded, norm, contentHash, time.Now().Unix())
	return err
}

// GetEmbedding returns the stored embedding for a message, or nil if missing.
func (s *Store) GetEmbedding(msgID int64, model string) ([]float32, float32, error) {
	var vecBlob []byte
	var norm float32
	err := s.db.QueryRow(`
		SELECT vector, norm FROM message_embeddings WHERE message_id = ? AND model = ?
	`, msgID, model).Scan(&vecBlob, &norm)
	if err == sql.ErrNoRows {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	vec, err := embeddings.DecodeVector(vecBlob)
	if err != nil {
		return nil, 0, err
	}
	return vec, norm, nil
}

// UpsertFolderEmbedding stores a centroid embedding for a folder.
func (s *Store) UpsertFolderEmbedding(account, folder, model string, vector []float32, norm float32, sampleCount int) error {
	encoded, err := embeddings.EncodeVector(vector)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO folder_embeddings (account_name, folder_name, model, vector, norm, sample_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_name, folder_name, model) DO UPDATE SET
			vector = excluded.vector,
			norm = excluded.norm,
			sample_count = excluded.sample_count,
			updated_at = excluded.updated_at
	`, account, folder, model, encoded, norm, sampleCount, time.Now().Unix())
	return err
}

// ListFolderEmbeddings returns all folder embeddings for an account.
func (s *Store) ListFolderEmbeddings(account, model string) ([]*data.Folder, [][]float32, []float32, []int, error) {
	rows, err := s.db.Query(`
		SELECT f.name, f.display_name, f.delimiter, f.attributes, f.depth,
		       e.vector, e.norm, e.sample_count
		FROM folder_embeddings e
		JOIN folders f ON f.account_name = e.account_name AND f.name = e.folder_name
		WHERE e.account_name = ? AND e.model = ?
	`, account, model)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer rows.Close()

	var folders []*data.Folder
	var vectors [][]float32
	var norms []float32
	var counts []int
	for rows.Next() {
		f := &data.Folder{AccountName: account}
		var attrs string
		var vecBlob []byte
		var norm float32
		var count int
		if err := rows.Scan(&f.Name, &f.DisplayName, &f.Delimiter, &attrs, &f.Depth, &vecBlob, &norm, &count); err != nil {
			return nil, nil, nil, nil, err
		}
		if attrs != "" {
			f.Attributes = strings.Split(attrs, " ")
		}
		vec, err := embeddings.DecodeVector(vecBlob)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		folders = append(folders, f)
		vectors = append(vectors, vec)
		norms = append(norms, norm)
		counts = append(counts, count)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	return folders, vectors, norms, counts, nil
}

// BuildFolderEmbeddings aggregates message embeddings per folder and stores centroids.
func (s *Store) BuildFolderEmbeddings(account, model string, perFolderLimit int) error {
	rows, err := s.db.Query(`
		SELECT m.folder_name, e.vector, e.norm
		FROM messages m
		JOIN message_embeddings e ON e.message_id = m.id
		WHERE m.account_name = ? AND e.model = ?
		ORDER BY m.date DESC
	`, account, model)
	if err != nil {
		return err
	}
	defer rows.Close()

	type agg struct {
		sum   []float64
		count int
	}
	aggs := make(map[string]*agg)

	for rows.Next() {
		var folder string
		var vecBlob []byte
		var norm float32
		if err := rows.Scan(&folder, &vecBlob, &norm); err != nil {
			return err
		}
		if perFolderLimit > 0 {
			if a := aggs[folder]; a != nil && a.count >= perFolderLimit {
				continue
			}
		}
		vec, err := embeddings.DecodeVector(vecBlob)
		if err != nil {
			return err
		}
		a := aggs[folder]
		if a == nil {
			a = &agg{sum: make([]float64, len(vec))}
			aggs[folder] = a
		}
		if len(a.sum) != len(vec) {
			continue
		}
		for i, v := range vec {
			a.sum[i] += float64(v)
		}
		a.count++
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for folder, a := range aggs {
		if a.count == 0 {
			continue
		}
		centroid := make([]float32, len(a.sum))
		inv := 1.0 / float64(a.count)
		for i, v := range a.sum {
			centroid[i] = float32(v * inv)
		}
		norm := embeddings.VectorNorm(centroid)
		if norm == 0 {
			continue
		}
		if err := s.UpsertFolderEmbedding(account, folder, model, centroid, norm, a.count); err != nil {
			return err
		}
	}
	return nil
}

// EmbeddingUpToDate returns true if an embedding exists for the message with the same content hash.
func (s *Store) EmbeddingUpToDate(msgID int64, model, contentHash string) (bool, error) {
	var existing string
	err := s.db.QueryRow(`
		SELECT content_hash FROM message_embeddings WHERE message_id = ? AND model = ?
	`, msgID, model).Scan(&existing)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return existing == contentHash, nil
}

// ListEmbeddingCandidates returns message embeddings for an account.
func (s *Store) ListEmbeddingCandidates(account, model string, limit int) ([]*data.Message, [][]float32, []float32, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.uid, 0, m.message_id, m.in_reply_to, m.refs,
		       m.subject, m.from_addr, m.to_addr, m.cc_addr, m.date,
		       m.flags, m.size, m.snippet, m.thread_id,
		       m.account_name, m.folder_name,
		       e.vector, e.norm
		FROM messages m
		JOIN message_embeddings e ON e.message_id = m.id
		WHERE m.account_name = ? AND e.model = ?
		ORDER BY m.date DESC
		LIMIT ?
	`, account, model, limit)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	var msgs []*data.Message
	var vectors [][]float32
	var norms []float32
	for rows.Next() {
		m := &data.Message{}
		var fromStr, toStr, ccStr, flagsStr, refs string
		var dateUnix int64
		var vecBlob []byte
		var norm float32
		if err := rows.Scan(
			&m.ID, &m.UID, &m.SeqNum, &m.MessageID, &m.InReplyTo, &refs,
			&m.Subject, &fromStr, &toStr, &ccStr, &dateUnix, &flagsStr,
			&m.Size, &m.Snippet, &m.ThreadID, &m.AccountName, &m.FolderName,
			&vecBlob, &norm,
		); err != nil {
			return nil, nil, nil, err
		}
		m.Date = time.Unix(dateUnix, 0)
		m.Flags = decodeFlags(flagsStr)
		m.From = decodeAddresses(fromStr)
		m.To = decodeAddresses(toStr)
		m.CC = decodeAddresses(ccStr)
		if refs != "" {
			m.References = strings.Fields(refs)
		}
		vec, err := embeddings.DecodeVector(vecBlob)
		if err != nil {
			return nil, nil, nil, err
		}
		msgs = append(msgs, m)
		vectors = append(vectors, vec)
		norms = append(norms, norm)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	return msgs, vectors, norms, nil
}

// GetMessages returns messages for an account/folder, newest first.
func (s *Store) GetMessages(account, folder string, limit int) ([]*data.Message, error) {
	rows, err := s.db.Query(`
		SELECT id, uid, 0, message_id, in_reply_to, refs,
		       subject, from_addr, to_addr, cc_addr, date, flags, size, snippet, thread_id
		FROM messages
		WHERE account_name = ? AND folder_name = ?
		ORDER BY date DESC
		LIMIT ?
	`, account, folder, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows, account, folder)
}

// GetMessagesFiltered returns messages filtered by account/folder/unread, newest first.
// Empty account or folder means "all".
func (s *Store) GetMessagesFiltered(account, folder string, unreadOnly bool, limit int) ([]*data.Message, error) {
	query := strings.Builder{}
	query.WriteString(`
		SELECT id, uid, 0, message_id, in_reply_to, refs,
		       subject, from_addr, to_addr, cc_addr, date, flags, size, snippet, thread_id,
		       account_name, folder_name
		FROM messages
		WHERE 1=1
	`)
	args := make([]any, 0, 4)
	if account != "" {
		query.WriteString(" AND account_name = ?")
		args = append(args, account)
	}
	if folder != "" {
		query.WriteString(" AND folder_name = ?")
		args = append(args, folder)
	}
	if unreadOnly {
		query.WriteString(" AND flags NOT LIKE ?")
		args = append(args, "%\\Seen%")
	}
	query.WriteString(" ORDER BY date DESC LIMIT ?")
	args = append(args, limit)

	rows, err := s.db.Query(query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessagesWithAccount(rows)
}

// GetMessageByUID returns a single message by account/folder/uid.
func (s *Store) GetMessageByUID(account, folder string, uid uint32) (*data.Message, error) {
	row := s.db.QueryRow(`
		SELECT id, uid, 0, message_id, in_reply_to, refs,
		       subject, from_addr, to_addr, cc_addr, date, flags, size, snippet, thread_id
		FROM messages
		WHERE account_name = ? AND folder_name = ? AND uid = ?
	`, account, folder, uid)
	msgs, err := scanMessageRow(row, account, folder)
	return msgs, err
}

// GetBody returns the stored body for a message.
func (s *Store) GetBody(msgID int64) (string, string, error) {
	var bodyText, bodyHTML string
	err := s.db.QueryRow(`
		SELECT body_text, body_html FROM bodies WHERE message_id = ?
	`, msgID).Scan(&bodyText, &bodyHTML)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return bodyText, bodyHTML, err
}

// ListBodyPrefetchCandidates returns messages without cached bodies for an account.
func (s *Store) ListBodyPrefetchCandidates(account string, limit int) ([]*data.Message, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.uid, 0, m.message_id, m.in_reply_to, m.refs,
		       m.subject, m.from_addr, m.to_addr, m.cc_addr, m.date,
		       m.flags, m.size, m.snippet, m.thread_id,
		       m.account_name, m.folder_name
		FROM messages m
		LEFT JOIN bodies b ON b.message_id = m.id
		WHERE m.account_name = ? AND b.message_id IS NULL
		ORDER BY m.date DESC
		LIMIT ?
	`, account, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessagesWithAccount(rows)
}

// ListBodiesForEmbedding returns cached bodies for an account.
// When force is false, it only returns messages without an embedding for the model.
func (s *Store) ListBodiesForEmbedding(account, model string, force bool) ([]*data.Message, []string, error) {
	var rows *sql.Rows
	var err error
	if force {
		rows, err = s.db.Query(`
			SELECT m.id, m.uid, 0, m.message_id, m.in_reply_to, m.refs,
			       m.subject, m.from_addr, m.to_addr, m.cc_addr, m.date,
			       m.flags, m.size, m.snippet, m.thread_id,
			       m.account_name, m.folder_name,
			       b.body_text
			FROM messages m
			JOIN bodies b ON b.message_id = m.id
			WHERE m.account_name = ?
			ORDER BY m.date DESC
		`, account)
	} else {
		rows, err = s.db.Query(`
			SELECT m.id, m.uid, 0, m.message_id, m.in_reply_to, m.refs,
			       m.subject, m.from_addr, m.to_addr, m.cc_addr, m.date,
			       m.flags, m.size, m.snippet, m.thread_id,
			       m.account_name, m.folder_name,
			       b.body_text
			FROM messages m
			JOIN bodies b ON b.message_id = m.id
			LEFT JOIN message_embeddings e ON e.message_id = m.id AND e.model = ?
			WHERE m.account_name = ? AND e.message_id IS NULL
			ORDER BY m.date DESC
		`, model, account)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var msgs []*data.Message
	var bodies []string
	for rows.Next() {
		m := &data.Message{}
		var fromStr, toStr, ccStr, flagsStr, refs string
		var dateUnix int64
		var bodyText string
		if err := rows.Scan(
			&m.ID, &m.UID, &m.SeqNum, &m.MessageID, &m.InReplyTo, &refs,
			&m.Subject, &fromStr, &toStr, &ccStr, &dateUnix, &flagsStr,
			&m.Size, &m.Snippet, &m.ThreadID, &m.AccountName, &m.FolderName,
			&bodyText,
		); err != nil {
			return nil, nil, err
		}
		m.Date = time.Unix(dateUnix, 0)
		m.Flags = decodeFlags(flagsStr)
		m.From = decodeAddresses(fromStr)
		m.To = decodeAddresses(toStr)
		m.CC = decodeAddresses(ccStr)
		if refs != "" {
			m.References = strings.Fields(refs)
		}
		msgs = append(msgs, m)
		bodies = append(bodies, bodyText)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return msgs, bodies, nil
}

// GetThreads returns thread groups for an account/folder, newest first.
func (s *Store) GetThreads(account, folder string) ([]*data.Thread, error) {
	msgs, err := s.GetMessages(account, folder, 500)
	if err != nil {
		return nil, err
	}
	// Group by thread_id
	threadMap := make(map[string]*data.Thread)
	var threadOrder []string
	for _, m := range msgs {
		tid := m.ThreadID
		if tid == "" {
			tid = m.MessageID
		}
		t, ok := threadMap[tid]
		if !ok {
			t = &data.Thread{
				ID:      tid,
				Subject: normalizeSubject(m.Subject),
			}
			threadMap[tid] = t
			threadOrder = append(threadOrder, tid)
		}
		t.Messages = append(t.Messages, m)
		if m.Date.After(t.LastDate) {
			t.LastDate = m.Date
		}
		if !m.IsRead() {
			t.HasUnread = true
		}
		if m.IsStarred() {
			t.Starred = true
		}
	}
	threads := make([]*data.Thread, 0, len(threadOrder))
	for _, tid := range threadOrder {
		threads = append(threads, threadMap[tid])
	}
	sort.Slice(threads, func(i, j int) bool {
		if !threads[i].LastDate.Equal(threads[j].LastDate) {
			return threads[i].LastDate.After(threads[j].LastDate)
		}
		return threads[i].ID < threads[j].ID
	})
	return threads, nil
}

// SearchLocal performs a full-text search using FTS5.
func (s *Store) SearchLocal(query string) ([]*data.Message, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.uid, 0, m.message_id, m.in_reply_to, m.refs,
		       m.subject, m.from_addr, m.to_addr, m.cc_addr, m.date,
		       m.flags, m.size, m.snippet, m.thread_id,
		       m.account_name, m.folder_name
		FROM messages m
		JOIN messages_fts ON messages_fts.docid = m.id
		WHERE messages_fts MATCH ?
		ORDER BY m.date DESC
		LIMIT 100
	`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessagesWithAccount(rows)
}

// SearchLocalFiltered performs a full-text search with optional account/folder filters.
func (s *Store) SearchLocalFiltered(query, account, folder string, limit int) ([]*data.Message, error) {
	sql := strings.Builder{}
	sql.WriteString(`
		SELECT m.id, m.uid, 0, m.message_id, m.in_reply_to, m.refs,
		       m.subject, m.from_addr, m.to_addr, m.cc_addr, m.date,
		       m.flags, m.size, m.snippet, m.thread_id,
		       m.account_name, m.folder_name
		FROM messages m
		JOIN messages_fts ON messages_fts.docid = m.id
		WHERE messages_fts MATCH ?
	`)
	args := make([]any, 0, 4)
	args = append(args, query)
	if account != "" {
		sql.WriteString(" AND m.account_name = ?")
		args = append(args, account)
	}
	if folder != "" {
		sql.WriteString(" AND m.folder_name = ?")
		args = append(args, folder)
	}
	sql.WriteString(" ORDER BY m.date DESC LIMIT ?")
	args = append(args, limit)

	rows, err := s.db.Query(sql.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessagesWithAccount(rows)
}

// KnownAddresses returns a sorted, deduplicated list of all email addresses
// seen in From, To, and CC fields across all cached messages.
func (s *Store) KnownAddresses() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT from_addr FROM messages WHERE from_addr != '' AND from_addr != '[]'
		UNION ALL
		SELECT to_addr   FROM messages WHERE to_addr   != '' AND to_addr   != '[]'
		UNION ALL
		SELECT cc_addr   FROM messages WHERE cc_addr   != '' AND cc_addr   != '[]'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	var result []string
	for rows.Next() {
		var jsonStr string
		if err := rows.Scan(&jsonStr); err != nil {
			continue
		}
		var addrs []data.Address
		if err := json.Unmarshal([]byte(jsonStr), &addrs); err != nil {
			continue
		}
		for _, addr := range addrs {
			if addr.Address != "" && !seen[addr.Address] {
				seen[addr.Address] = true
				result = append(result, addr.Address)
			}
		}
	}
	sort.Strings(result)
	return result, nil
}

// SearchLocalWithFilters performs a full-text search with optional chip filters
// parsed from a compiled query string like "[from:alice@x.com] invoice".
func (s *Store) SearchLocalWithFilters(compiledQuery string) ([]*data.Message, error) {
	pq := data.ParseSearchQuery(compiledQuery)

	if pq.FreeText == "" && len(pq.From) == 0 && len(pq.To) == 0 && pq.Subject == "" {
		return nil, nil
	}

	var b strings.Builder
	b.WriteString(`SELECT m.id, m.uid, 0, m.message_id, m.in_reply_to, m.refs,
		       m.subject, m.from_addr, m.to_addr, m.cc_addr, m.date,
		       m.flags, m.size, m.snippet, m.thread_id,
		       m.account_name, m.folder_name
		FROM messages m`)

	var args []any
	var conditions []string

	if pq.FreeText != "" {
		b.WriteString("\n\tJOIN messages_fts ON messages_fts.docid = m.id")
		conditions = append(conditions, "messages_fts MATCH ?")
		args = append(args, pq.FreeText)
	}
	for _, addr := range pq.From {
		conditions = append(conditions, "m.from_addr LIKE ?")
		args = append(args, "%"+addr+"%")
	}
	for _, addr := range pq.To {
		conditions = append(conditions, "(m.to_addr LIKE ? OR m.cc_addr LIKE ?)")
		args = append(args, "%"+addr+"%", "%"+addr+"%")
	}
	if pq.Subject != "" {
		conditions = append(conditions, "m.subject LIKE ?")
		args = append(args, "%"+pq.Subject+"%")
	}

	if len(conditions) > 0 {
		b.WriteString("\n\tWHERE " + strings.Join(conditions, " AND "))
	}
	b.WriteString("\n\tORDER BY m.date DESC LIMIT 100")

	rows, err := s.db.Query(b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessagesWithAccount(rows)
}

// SetFlags updates flags for a message by UID.
func (s *Store) SetFlags(account, folder string, uid uint32, flags []data.Flag) error {
	_, err := s.db.Exec(`
		UPDATE messages SET flags = ? WHERE account_name = ? AND folder_name = ? AND uid = ?
	`, encodeFlags(flags), account, folder, uid)
	return err
}

// AddTag adds a tag to a message (creates tag if not exists).
func (s *Store) AddTag(msgID int64, tagName string) error {
	var tagID int64
	err := s.db.QueryRow(`INSERT OR IGNORE INTO tags (name) VALUES (?); SELECT id FROM tags WHERE name = ?`, tagName, tagName).Scan(&tagID)
	if err != nil {
		// Try separately
		if _, err2 := s.db.Exec(`INSERT OR IGNORE INTO tags (name) VALUES (?)`, tagName); err2 != nil {
			return err2
		}
		if err2 := s.db.QueryRow(`SELECT id FROM tags WHERE name = ?`, tagName).Scan(&tagID); err2 != nil {
			return err2
		}
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO message_tags (message_id, tag_id) VALUES (?, ?)`, msgID, tagID)
	return err
}

// RemoveTag removes a tag from a message.
func (s *Store) RemoveTag(msgID int64, tagName string) error {
	_, err := s.db.Exec(`
		DELETE FROM message_tags
		WHERE message_id = ? AND tag_id = (SELECT id FROM tags WHERE name = ?)
	`, msgID, tagName)
	return err
}

// UpsertFolders stores folder metadata for an account.
func (s *Store) UpsertFolders(account string, folders []*data.Folder) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, f := range folders {
		attrs := strings.Join(f.Attributes, " ")
		_, err := tx.Exec(`
			INSERT INTO folders (account_name, name, display_name, delimiter, attributes, depth, unread, total)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_name, name) DO UPDATE SET
				display_name = excluded.display_name,
				delimiter    = excluded.delimiter,
				attributes   = excluded.attributes,
				depth        = excluded.depth,
				unread       = CASE WHEN excluded.unread > 0 THEN excluded.unread ELSE folders.unread END,
				total        = CASE WHEN excluded.total  > 0 THEN excluded.total  ELSE folders.total  END
		`, account, f.Name, f.DisplayName, f.Delimiter, attrs, f.Depth, f.Unread, f.Total)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MoveMessage updates cached metadata for a moved message. If destUID is zero,
// the destination UID is unknown and the source row is removed instead.
func (s *Store) MoveMessage(account, sourceFolder string, uid uint32, destFolder string, destUID uint32) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if destUID == 0 {
		if _, err := tx.Exec(`
			DELETE FROM messages
			WHERE account_name = ? AND folder_name = ? AND uid = ?
		`, account, sourceFolder, uid); err != nil {
			return err
		}
		return tx.Commit()
	}

	if _, err := tx.Exec(`
		DELETE FROM messages
		WHERE account_name = ? AND folder_name = ? AND uid = ?
	`, account, destFolder, destUID); err != nil {
		return err
	}
	_, err = tx.Exec(`
		UPDATE messages
		SET folder_name = ?, uid = ?
		WHERE account_name = ? AND folder_name = ? AND uid = ?
	`, destFolder, destUID, account, sourceFolder, uid)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RenameFolder updates cached folder metadata and messages for a renamed folder.
func (s *Store) RenameFolder(account, oldName, newName string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldPrefix := oldName + "/"
	rows, err := tx.Query(`
		SELECT name FROM folders
		WHERE account_name = ? AND (name = ? OR substr(name, 1, ?) = ?)
	`, account, oldName, len(oldPrefix), oldPrefix)
	if err != nil {
		return err
	}
	var folderNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		folderNames = append(folderNames, name)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, oldFolderName := range folderNames {
		newFolderName := renamedFolderName(oldFolderName, oldName, newName)
		displayName, depth := folderDisplayNameAndDepth(newFolderName, "/")
		if _, err := tx.Exec(`
			UPDATE folders
			SET name = ?, display_name = ?, delimiter = '/', depth = ?
			WHERE account_name = ? AND name = ?
		`, newFolderName, displayName, depth, account, oldFolderName); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`
		UPDATE messages
		SET folder_name = ? || substr(folder_name, ?)
		WHERE account_name = ? AND (folder_name = ? OR substr(folder_name, 1, ?) = ?)
	`, newName, len(oldName)+1, account, oldName, len(oldPrefix), oldPrefix); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE folder_embeddings
		SET folder_name = ? || substr(folder_name, ?)
		WHERE account_name = ? AND (folder_name = ? OR substr(folder_name, 1, ?) = ?)
	`, newName, len(oldName)+1, account, oldName, len(oldPrefix), oldPrefix); err != nil {
		return err
	}
	return tx.Commit()
}

func renamedFolderName(name, oldName, newName string) string {
	if name == oldName {
		return newName
	}
	return newName + strings.TrimPrefix(name, oldName)
}

// DeleteFolder removes a folder and cached messages it contains.
func (s *Store) DeleteFolder(account, folder string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		DELETE FROM messages
		WHERE account_name = ? AND folder_name = ?
	`, account, folder); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		DELETE FROM folder_embeddings
		WHERE account_name = ? AND folder_name = ?
	`, account, folder); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		DELETE FROM folders
		WHERE account_name = ? AND name = ?
	`, account, folder); err != nil {
		return err
	}
	return tx.Commit()
}

func folderDisplayNameAndDepth(name, delimiter string) (string, int) {
	if delimiter == "" {
		delimiter = "/"
	}
	parts := strings.Split(name, delimiter)
	return parts[len(parts)-1], len(parts) - 1
}

// GetFolders returns all folders for an account.
func (s *Store) GetFolders(account string) ([]*data.Folder, error) {
	rows, err := s.db.Query(`
		SELECT name, display_name, delimiter, attributes, depth, unread, total
		FROM folders WHERE account_name = ? ORDER BY name
	`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []*data.Folder
	for rows.Next() {
		f := &data.Folder{AccountName: account}
		var attrs string
		if err := rows.Scan(&f.Name, &f.DisplayName, &f.Delimiter, &attrs, &f.Depth, &f.Unread, &f.Total); err != nil {
			return nil, err
		}
		if attrs != "" {
			f.Attributes = strings.Split(attrs, " ")
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// GetAllFolders returns folders across all accounts with counts.
func (s *Store) GetAllFolders() ([]*data.Folder, error) {
	rows, err := s.db.Query(`
		SELECT account_name, name, display_name, delimiter, attributes, depth, unread, total
		FROM folders ORDER BY account_name, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []*data.Folder
	for rows.Next() {
		f := &data.Folder{}
		var attrs string
		if err := rows.Scan(&f.AccountName, &f.Name, &f.DisplayName, &f.Delimiter, &attrs, &f.Depth, &f.Unread, &f.Total); err != nil {
			return nil, err
		}
		if attrs != "" {
			f.Attributes = strings.Split(attrs, " ")
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// UpdateFolderCounts updates unread/total counts for a folder.
func (s *Store) UpdateFolderCounts(account, folder string, unread, total int) error {
	_, err := s.db.Exec(`
		UPDATE folders SET unread = ?, total = ? WHERE account_name = ? AND name = ?
	`, unread, total, account, folder)
	return err
}

// CountMessages returns total messages for an account.
func (s *Store) CountMessages(account string) (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM messages WHERE account_name = ?
	`, account).Scan(&count)
	return count, err
}

// CountEmbeddings returns total embeddings for an account and model.
func (s *Store) CountEmbeddings(account, model string) (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM message_embeddings e
		JOIN messages m ON m.id = e.message_id
		WHERE m.account_name = ? AND e.model = ?
	`, account, model).Scan(&count)
	return count, err
}

// --- helpers ---

func encodeFlags(flags []data.Flag) string {
	parts := make([]string, len(flags))
	for i, f := range flags {
		parts[i] = string(f)
	}
	return strings.Join(parts, " ")
}

func decodeFlags(s string) []data.Flag {
	if s == "" {
		return nil
	}
	parts := strings.Fields(s)
	flags := make([]data.Flag, len(parts))
	for i, p := range parts {
		flags[i] = data.Flag(p)
	}
	return flags
}

func decodeAddresses(s string) []data.Address {
	if s == "" || s == "null" {
		return nil
	}
	var addrs []data.Address
	_ = json.Unmarshal([]byte(s), &addrs)
	return addrs
}

func normalizeSubject(s string) string {
	s = strings.TrimSpace(s)
	for {
		lower := strings.ToLower(s)
		if strings.HasPrefix(lower, "re:") {
			s = strings.TrimSpace(s[3:])
		} else if strings.HasPrefix(lower, "fwd:") {
			s = strings.TrimSpace(s[4:])
		} else if strings.HasPrefix(lower, "fw:") {
			s = strings.TrimSpace(s[3:])
		} else {
			break
		}
	}
	return s
}

func scanMessages(rows *sql.Rows, account, folder string) ([]*data.Message, error) {
	var msgs []*data.Message
	for rows.Next() {
		m := &data.Message{AccountName: account, FolderName: folder}
		var fromStr, toStr, ccStr, flagsStr, refs string
		var dateUnix int64
		if err := rows.Scan(
			&m.ID, &m.UID, &m.SeqNum, &m.MessageID, &m.InReplyTo, &refs,
			&m.Subject, &fromStr, &toStr, &ccStr, &dateUnix, &flagsStr,
			&m.Size, &m.Snippet, &m.ThreadID,
		); err != nil {
			return nil, err
		}
		m.Date = time.Unix(dateUnix, 0)
		m.Flags = decodeFlags(flagsStr)
		m.From = decodeAddresses(fromStr)
		m.To = decodeAddresses(toStr)
		m.CC = decodeAddresses(ccStr)
		if refs != "" {
			m.References = strings.Fields(refs)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func scanMessageRow(row *sql.Row, account, folder string) (*data.Message, error) {
	m := &data.Message{AccountName: account, FolderName: folder}
	var fromStr, toStr, ccStr, flagsStr, refs string
	var dateUnix int64
	err := row.Scan(
		&m.ID, &m.UID, &m.SeqNum, &m.MessageID, &m.InReplyTo, &refs,
		&m.Subject, &fromStr, &toStr, &ccStr, &dateUnix, &flagsStr,
		&m.Size, &m.Snippet, &m.ThreadID,
	)
	if err != nil {
		return nil, err
	}
	m.Date = time.Unix(dateUnix, 0)
	m.Flags = decodeFlags(flagsStr)
	m.From = decodeAddresses(fromStr)
	m.To = decodeAddresses(toStr)
	m.CC = decodeAddresses(ccStr)
	if refs != "" {
		m.References = strings.Fields(refs)
	}
	return m, nil
}

func scanMessagesWithAccount(rows *sql.Rows) ([]*data.Message, error) {
	var msgs []*data.Message
	for rows.Next() {
		m := &data.Message{}
		var fromStr, toStr, ccStr, flagsStr, refs string
		var dateUnix int64
		if err := rows.Scan(
			&m.ID, &m.UID, &m.SeqNum, &m.MessageID, &m.InReplyTo, &refs,
			&m.Subject, &fromStr, &toStr, &ccStr, &dateUnix, &flagsStr,
			&m.Size, &m.Snippet, &m.ThreadID, &m.AccountName, &m.FolderName,
		); err != nil {
			return nil, err
		}
		m.Date = time.Unix(dateUnix, 0)
		m.Flags = decodeFlags(flagsStr)
		m.From = decodeAddresses(fromStr)
		m.To = decodeAddresses(toStr)
		m.CC = decodeAddresses(ccStr)
		if refs != "" {
			m.References = strings.Fields(refs)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// UpsertCategory stores or updates the classification result for a message.
func (s *Store) UpsertCategory(messageID int64, category, model string, confidence float32) error {
	_, err := s.db.Exec(`
		INSERT INTO message_categories (message_id, category, confidence, model, classified_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			category      = excluded.category,
			confidence    = excluded.confidence,
			model         = excluded.model,
			classified_at = excluded.classified_at
	`, messageID, category, confidence, model, time.Now().Unix())
	return err
}

// GetCategory returns the stored category for a message, or ("", nil) if none.
func (s *Store) GetCategory(messageID int64) (string, error) {
	var category string
	err := s.db.QueryRow(
		`SELECT category FROM message_categories WHERE message_id = ?`, messageID,
	).Scan(&category)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return category, err
}

// GetMessagesByCategory returns all messages across all folders for an account
// that have been classified into the given category, ordered newest first.
func (s *Store) GetMessagesByCategory(accountName, category string) ([]*data.Message, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.uid, 0, m.message_id, m.in_reply_to, m.refs,
		       m.subject, m.from_addr, m.to_addr, m.cc_addr,
		       m.date, m.flags, m.size, m.snippet, m.thread_id,
		       m.account_name, m.folder_name
		FROM messages m
		JOIN message_categories mc ON mc.message_id = m.id
		WHERE m.account_name = ? AND mc.category = ?
		ORDER BY m.date DESC
	`, accountName, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessagesWithAccount(rows)
}

// GetCategoryCounts returns a map of category → unread message count for an account.
func (s *Store) GetCategoryCounts(accountName string) (map[string]int, error) {
	rows, err := s.db.Query(`
		SELECT mc.category, COUNT(*) as cnt
		FROM message_categories mc
		JOIN messages m ON m.id = mc.message_id
		WHERE m.account_name = ?
		  AND m.flags NOT LIKE '%\Seen%'
		GROUP BY mc.category
	`, accountName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var cat string
		var cnt int
		if err := rows.Scan(&cat, &cnt); err != nil {
			return nil, err
		}
		counts[cat] = cnt
	}
	return counts, rows.Err()
}

// UpsertSuggestedEvent stores or updates the extracted event for a message.
func (s *Store) UpsertSuggestedEvent(ev *data.SuggestedEvent) error {
	if ev == nil {
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO suggested_events (
			message_id, has_event, summary, date, start_time, end_time,
			location, calendar, status, all_day, recurring, description,
			model, generated_at, source_hash, plain_text, json_text,
			parse_error, generation_ok, rejected
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			has_event = excluded.has_event,
			summary = excluded.summary,
			date = excluded.date,
			start_time = excluded.start_time,
			end_time = excluded.end_time,
			location = excluded.location,
			calendar = excluded.calendar,
			status = excluded.status,
			all_day = excluded.all_day,
			recurring = excluded.recurring,
			description = excluded.description,
			model = excluded.model,
			generated_at = excluded.generated_at,
			source_hash = excluded.source_hash,
			plain_text = excluded.plain_text,
			json_text = excluded.json_text,
			parse_error = excluded.parse_error,
			generation_ok = excluded.generation_ok,
			rejected = excluded.rejected
	`,
		ev.MessageID,
		boolToInt(ev.HasEvent),
		ev.Summary,
		ev.Date,
		ev.Start,
		ev.End,
		ev.Location,
		ev.Calendar,
		ev.Status,
		boolToInt(ev.AllDay),
		boolToInt(ev.Recurring),
		ev.Description,
		ev.Model,
		ev.GeneratedAt,
		ev.SourceHash,
		ev.PlainText,
		ev.JSONText,
		ev.ParseError,
		boolToInt(ev.GenerationOK),
		boolToInt(ev.Rejected),
	)
	return err
}

// GetSuggestedEvent returns the cached suggested event for a message.
func (s *Store) GetSuggestedEvent(messageID int64) (*data.SuggestedEvent, error) {
	ev := &data.SuggestedEvent{}
	var hasEvent, allDay, recurring, generationOK, rejected int
	err := s.db.QueryRow(`
		SELECT message_id, has_event, summary, date, start_time, end_time,
		       location, calendar, status, all_day, recurring, description,
		       model, generated_at, source_hash, plain_text, json_text,
		       parse_error, generation_ok,
		       COALESCE(rejected, 0)
		FROM suggested_events
		WHERE message_id = ?
	`, messageID).Scan(
		&ev.MessageID,
		&hasEvent,
		&ev.Summary,
		&ev.Date,
		&ev.Start,
		&ev.End,
		&ev.Location,
		&ev.Calendar,
		&ev.Status,
		&allDay,
		&recurring,
		&ev.Description,
		&ev.Model,
		&ev.GeneratedAt,
		&ev.SourceHash,
		&ev.PlainText,
		&ev.JSONText,
		&ev.ParseError,
		&generationOK,
		&rejected,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ev.HasEvent = hasEvent != 0
	ev.AllDay = allDay != 0
	ev.Recurring = recurring != 0
	ev.GenerationOK = generationOK != 0
	ev.Rejected = rejected != 0
	return ev, nil
}

// RejectSuggestedEvent marks a suggestion as rejected so it is never shown again.
func (s *Store) RejectSuggestedEvent(messageID int64) error {
	_, err := s.db.Exec(`
		INSERT INTO suggested_events (message_id, rejected)
		VALUES (?, 1)
		ON CONFLICT(message_id) DO UPDATE SET rejected = 1
	`, messageID)
	return err
}

// GetUnclassifiedInboxMessages returns messages from INBOX for an account that
// have not yet been classified. Limit controls the maximum number returned.
func (s *Store) GetUnclassifiedInboxMessages(accountName string, limit int) ([]*data.Message, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.uid, 0, m.message_id, m.in_reply_to, m.refs,
		       m.subject, m.from_addr, m.to_addr, m.cc_addr,
		       m.date, m.flags, m.size, m.snippet, m.thread_id,
		       m.account_name, m.folder_name
		FROM messages m
		LEFT JOIN message_categories mc ON mc.message_id = m.id
		WHERE m.account_name = ? AND UPPER(m.folder_name) = 'INBOX'
		  AND mc.message_id IS NULL
		ORDER BY m.date DESC
		LIMIT ?
	`, accountName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessagesWithAccount(rows)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
