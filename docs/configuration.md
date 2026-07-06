# Configuration

Config lives at `~/.config/bubblmail/config.toml` and is written with user-only
permissions (`0600`).

## Minimal example

```toml
[general]
sync_interval_minutes = 5

[cache]
dir = "~/.cache/bubblmail"

[[account]]
name = "work"
imap_host = "imap.example.com"
imap_port = 993
smtp_host = "smtp.example.com"
smtp_port = 587
username = "user@example.com"
password_cmd = "pass email/work"
```

## Sections

### `[general]`

- `sync_interval_minutes` - background sync interval
- `page_size` - initial message page size
- `default_account` - preferred account on startup

### `[theme]`

- `mode` - `auto`, `dark`, or `light`
- `accent` - accent color hex value

### `[cache]`

- `dir` - cache directory; default is under `~/.cache/bubblmail`

### `[[account]]`

- `name`
- `imap_host`, `imap_port`
- `smtp_host`, `smtp_port`
- `username`
- `password` or `password_cmd`

Prefer `password_cmd` over storing a plaintext password.

### `[embeddings]`

Used for semantic search and smart move.

- `model`
- `api_key` or `api_key_cmd`
- `base_url`
- `batch_size`
- `stream_batch`
- `top_semantic`
- `top_similar`
- `max_candidates`
- `max_content_chars`
- `folder_sample_limit`
- `auto_move_threshold`
- `prefetch_bodies`
- `prefetch_batch`
- `prefetch_interval_seconds`

If no key is set, `OPENROUTER_API_KEY` is also checked.

### `[classification]`

Used for smart folders and related features.

- `enabled`
- `model`
- `api_key` or `api_key_cmd`
- `base_url`
- `categories`

If no classification key is set, the embeddings key resolution path is reused.

## Notes

- The default API base URL is OpenRouter-compatible.
- Cached bodies, embeddings, and suggestion/classification results are stored locally.
- Enabling AI-backed features may send message content to the configured endpoint.
