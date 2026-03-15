# Architecture

## Shape of the codebase

- `cmd/` wires CLI commands and starts the TUI
- `ui/` owns Bubble Tea models, views, overlays, and interaction state
- `cache/` owns the SQLite schema and local persistence
- `imap/` and `smtp/` talk to mail servers
- `render/` converts plain text and HTML bodies into terminal-friendly output
- `thread/` groups messages into threads
- `embeddings/`, `classify/`, and `suggest/` talk to external AI services

## Runtime model

The app is local-first.

- IMAP fetches message metadata and bodies
- SQLite stores cached messages, bodies, folder metadata, FTS data, embeddings, and local-only suggestion/classification results
- the TUI reads mostly from the cache and updates it as work completes

The root Bubble Tea model in `ui/app.go` currently coordinates most of the app. It owns view switching, background jobs, search, sync, and action handling.

## Search

There are three search paths:

- local FTS search against SQLite
- IMAP server search
- semantic search using stored embeddings

Local search is backed by `messages_fts` in `cache/schema.sql`. Body updates refresh the FTS row in `cache/store.go`.

## Background work

There are separate queues for:

- embeddings
- smart-folder classification
- suggested event extraction

These run outside the main UI loop and feed results back into the app.

## SQLite cache

The cache is not the source of truth; the server is. Deleting or rebuilding the cache is acceptable when schema or FTS compatibility changes.

Important tables:

- `messages`
- `bodies`
- `folders`
- `messages_fts`
- `message_embeddings`
- `folder_embeddings`
- `message_categories`
- `suggested_events`

## Known pressure points

- `ui/app.go` carries too much orchestration
- large rendering files are easy to regress with width/layout changes
- terminal width correctness depends on sanitizing strings before measurement

If you are adding a feature, prefer keeping protocol code in `imap/` or `smtp/`, persistence in `cache/`, and rendering in `ui/`/`render/` rather than expanding `ui/app.go` further.
