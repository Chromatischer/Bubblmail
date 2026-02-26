package cache

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/bubblmail/bubblmail/data"
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
	_, err := s.db.Exec(schemaSQL)
	return err
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

		if _, err := stmt.Exec(
			m.AccountName, m.FolderName, m.UID, m.MessageID, m.InReplyTo, refs,
			m.Subject, string(fromJSON), string(toJSON), string(ccJSON),
			m.Date.Unix(), flags, m.Size, m.Snippet, m.ThreadID,
		); err != nil {
			return err
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
			INSERT INTO folders (account_name, name, display_name, delimiter, attributes, depth)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_name, name) DO UPDATE SET
				display_name = excluded.display_name,
				delimiter    = excluded.delimiter,
				attributes   = excluded.attributes,
				depth        = excluded.depth
		`, account, f.Name, f.DisplayName, f.Delimiter, attrs, f.Depth)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
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

// UpdateFolderCounts updates unread/total counts for a folder.
func (s *Store) UpdateFolderCounts(account, folder string, unread, total int) error {
	_, err := s.db.Exec(`
		UPDATE folders SET unread = ?, total = ? WHERE account_name = ? AND name = ?
	`, unread, total, account, folder)
	return err
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
