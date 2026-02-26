PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS messages (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    account_name TEXT    NOT NULL,
    folder_name  TEXT    NOT NULL,
    uid          INTEGER NOT NULL,
    message_id   TEXT    NOT NULL DEFAULT '',
    in_reply_to  TEXT    NOT NULL DEFAULT '',
    refs         TEXT    NOT NULL DEFAULT '', -- space-separated Message-IDs
    subject      TEXT    NOT NULL DEFAULT '',
    from_addr    TEXT    NOT NULL DEFAULT '', -- JSON encoded []Address
    to_addr      TEXT    NOT NULL DEFAULT '', -- JSON encoded []Address
    cc_addr      TEXT    NOT NULL DEFAULT '', -- JSON encoded []Address
    date         INTEGER NOT NULL DEFAULT 0,  -- Unix timestamp
    flags        TEXT    NOT NULL DEFAULT '', -- space-separated
    size         INTEGER NOT NULL DEFAULT 0,
    snippet      TEXT    NOT NULL DEFAULT '',
    thread_id    TEXT    NOT NULL DEFAULT '',
    UNIQUE(account_name, folder_name, uid)
);

CREATE TABLE IF NOT EXISTS bodies (
    message_id   INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    body_text    TEXT NOT NULL DEFAULT '',
    body_html    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS tags (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    name  TEXT NOT NULL UNIQUE,
    color TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS message_tags (
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tag_id     INTEGER NOT NULL REFERENCES tags(id)     ON DELETE CASCADE,
    PRIMARY KEY (message_id, tag_id)
);

CREATE TABLE IF NOT EXISTS folders (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    account_name TEXT    NOT NULL,
    name         TEXT    NOT NULL,
    display_name TEXT    NOT NULL DEFAULT '',
    delimiter    TEXT    NOT NULL DEFAULT '/',
    attributes   TEXT    NOT NULL DEFAULT '',
    depth        INTEGER NOT NULL DEFAULT 0,
    unread       INTEGER NOT NULL DEFAULT 0,
    total        INTEGER NOT NULL DEFAULT 0,
    UNIQUE(account_name, name)
);

-- FTS4 virtual table for full-text search (enabled by default in go-sqlite3)
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts4(
    subject,
    snippet,
    body_text,
    content='messages'
);

-- Triggers to keep FTS index in sync
CREATE TRIGGER IF NOT EXISTS messages_fts_insert AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(docid, subject, snippet, body_text)
    VALUES (new.id, new.subject, new.snippet, '');
END;

CREATE TRIGGER IF NOT EXISTS messages_fts_delete AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, docid, subject, snippet, body_text)
    VALUES ('delete', old.id, old.subject, old.snippet, '');
END;

CREATE TRIGGER IF NOT EXISTS messages_fts_update AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, docid, subject, snippet, body_text)
    VALUES ('delete', old.id, old.subject, old.snippet, '');
    INSERT INTO messages_fts(docid, subject, snippet, body_text)
    VALUES (new.id, new.subject, new.snippet, '');
END;

CREATE INDEX IF NOT EXISTS idx_messages_account_folder ON messages(account_name, folder_name);
CREATE INDEX IF NOT EXISTS idx_messages_thread_id      ON messages(thread_id);
CREATE INDEX IF NOT EXISTS idx_messages_date           ON messages(date DESC);
