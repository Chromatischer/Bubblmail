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

CREATE TABLE IF NOT EXISTS message_embeddings (
    message_id   INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    model        TEXT    NOT NULL,
    vector       BLOB    NOT NULL,
    norm         REAL    NOT NULL,
    content_hash TEXT    NOT NULL,
    updated_at   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (message_id, model)
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

CREATE TABLE IF NOT EXISTS folder_embeddings (
    account_name TEXT    NOT NULL,
    folder_name  TEXT    NOT NULL,
    model        TEXT    NOT NULL,
    vector       BLOB    NOT NULL,
    norm         REAL    NOT NULL,
    sample_count INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (account_name, folder_name, model)
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

CREATE INDEX IF NOT EXISTS idx_messages_account_folder ON messages(account_name, folder_name);
CREATE INDEX IF NOT EXISTS idx_messages_thread_id      ON messages(thread_id);
CREATE INDEX IF NOT EXISTS idx_messages_date           ON messages(date DESC);
CREATE INDEX IF NOT EXISTS idx_embeddings_updated_at   ON message_embeddings(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_folder_embeddings_updated_at ON folder_embeddings(updated_at DESC);

-- Smart folder classification results (local-only, never synced to IMAP)
CREATE TABLE IF NOT EXISTS message_categories (
    message_id     INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    category       TEXT    NOT NULL,
    confidence     REAL    NOT NULL DEFAULT 1.0,
    model          TEXT    NOT NULL DEFAULT '',
    classified_at  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (message_id)
);
CREATE INDEX IF NOT EXISTS idx_message_categories_category ON message_categories(category);

CREATE TABLE IF NOT EXISTS suggested_events (
    message_id     INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    has_event      INTEGER NOT NULL DEFAULT 0,
    summary        TEXT    NOT NULL DEFAULT '',
    date           TEXT    NOT NULL DEFAULT '',
    start_time     TEXT    NOT NULL DEFAULT '',
    end_time       TEXT    NOT NULL DEFAULT '',
    location       TEXT    NOT NULL DEFAULT '',
    calendar       TEXT    NOT NULL DEFAULT '',
    status         TEXT    NOT NULL DEFAULT '',
    all_day        INTEGER NOT NULL DEFAULT 0,
    recurring      INTEGER NOT NULL DEFAULT 0,
    description    TEXT    NOT NULL DEFAULT '',
    model          TEXT    NOT NULL DEFAULT '',
    generated_at   INTEGER NOT NULL DEFAULT 0,
    source_hash    TEXT    NOT NULL DEFAULT '',
    plain_text     TEXT    NOT NULL DEFAULT '',
    json_text      TEXT    NOT NULL DEFAULT '',
    parse_error    TEXT    NOT NULL DEFAULT '',
    generation_ok  INTEGER NOT NULL DEFAULT 0,
    rejected       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (message_id)
);
CREATE INDEX IF NOT EXISTS idx_suggested_events_model ON suggested_events(model);
