# bubblmail

A keyboard-driven terminal email client built for people who live in the shell. Fast local cache, vim-style navigation, full-text search, and a clean TUI that stays out of your way.

> **Early alpha.** Expect rough edges, missing features, and occasional breakage. Use at your own risk with non-critical accounts.

---

## Screenshot

```
┌─────────────────────────────────────────────────────────────────────────┐
│ bubblmail                                              ● synced          │
│  work › INBOX                                                            │
├─────────────────────────────────────────────────────────────────────────┤
│  ACCOUNTS    │  ▌ Alice Nguyen          invoice  Jun 12                  │
│  ─────────── │    Re: Q2 invoices…              (3)                      │
│  work        │  ● Bob Martinez                  Mon                     │
│   INBOX  12  │    Deploy checklist for Friday                           │
│   Sent       │  ○ Carol Kim            work      Jun 9                  │
│   Drafts     │    Offsite logistics                                      │
│   Archive    │  ○ Dave Okafor                   Jun 8                   │
│  personal    │    Weekend plans                                          │
│   INBOX   3  │                                                           │
│   Sent       │                                                           │
├─────────────────────────────────────────────────────────────────────────┤
│ j/k nav  enter open  c compose  r reply  / search  ? help               │
└─────────────────────────────────────────────────────────────────────────┘
```

*Screenshot placeholder — real UI screenshots coming soon.*

---

## Features

- **Multiple accounts** — IMAP/SMTP with per-account credentials; password command support (`pass`, `1password-cli`, etc.)
- **Local SQLite cache** — all mail synced locally for fast browsing and offline access
- **Full-text search** — FTS4 keyword search across subject, snippet, and body; live results as you type
- **Email threading** — JWZ-style threading groups conversations correctly across folders
- **Compose, reply, reply-all, forward** — full composer with To/CC/Subject/Body fields
- **Semantic search** — embed mail bodies via OpenRouter and rank results by vector similarity alongside keyword search; background backfill keeps the index fresh
- **Quick actions** — press `←`/`→` in the inbox to reveal action panels that slide in from the edges of the selected row; press again to expose a second action, press a third time to execute; `Enter` executes the highlighted action at any step
- **Smart move** — the MOVE quick action uses OpenRouter embeddings and cosine similarity to suggest the best destination folder automatically; falls back to the folder picker if no confident match is found
- **Move to folder** — folder picker with QWERTY quick-select for fast filing
- **Star and mark read/unread** — local flag changes synced back to IMAP
- **Delete** — moves to Trash on the server
- **Collapsible sidebar** — account and folder tree with live unread counts
- **IMAP server search** — send search queries directly to the server when local cache isn't enough
- **Auto dark/light theme** — detects terminal background; `NO_COLOR` respected; configurable accent color
- **Background sync** — configurable sync interval keeps cache fresh without blocking the UI
- **CLI interface** — scriptable `list` subcommands for unread counts, search, and mailbox inspection

---

## Planned Features

- **Tag picker UI** — local labels with color support; backend already implemented, UI overlay pending
- **Archive action** — `e` key is wired; server-side archive move not yet implemented
- **HTML rendering improvements** — richer conversion of HTML email to terminal output

---

## Requirements

- Go 1.25+
- A terminal at least 80×24
- An IMAP/SMTP email account

---

## Install

**From source:**

```sh
git clone https://github.com/bubblmail/bubblmail
cd bubblmail
go build -o bubblmail .
sudo mv bubblmail /usr/local/bin/
```

---

## Setup

Run the interactive setup wizard to configure your first account:

```sh
bubblmail auth add
```

The wizard will prompt for:
- Account name (e.g. `work`, `personal`)
- IMAP host and port (default 993)
- SMTP host and port (default 587)
- Username and password (or a shell command to retrieve it)

Config is written to `~/.config/bubblmail/config.toml`.

The local cache is stored in `~/.cache/bubblmail` by default.

**Example config:**

```toml
[general]
sync_interval_minutes = 5
page_size = 50
default_account = "work"

[theme]
mode = "auto"       # "auto", "dark", "light"
accent = "#7C3AED"

[cache]
dir = "~/.cache/bubblmail"

[[account]]
name = "work"
imap_host = "imap.example.com"
imap_port = 993
smtp_host = "smtp.example.com"
smtp_port = 587
username = "user@example.com"
password_cmd = "pass email/work"  # or: password = "plaintext"

[[account]]
name = "personal"
imap_host = "imap.fastmail.com"
imap_port = 993
smtp_host = "smtp.fastmail.com"
smtp_port = 587
username = "you@fastmail.com"
password_cmd = "pass email/personal"
```

---

## CLI Commands

```
bubblmail                         Launch the TUI

bubblmail auth add                Add a new account (interactive wizard)
bubblmail auth list               List configured accounts
bubblmail auth status             Test IMAP connections
bubblmail auth remove <name>      Remove an account

bubblmail embeddings status       Show embedding coverage per account
bubblmail embeddings embedd       Backfill embeddings for cached bodies
bubblmail embeddings embedd force Re-embed all cached bodies

bubblmail list unread             List unread messages
bubblmail list mailbox <name>     List messages in a mailbox
bubblmail list account <name>     List messages for an account
bubblmail list search <query>     Full-text search cached messages
bubblmail list counts             Show unread/total counts per folder

bubblmail search diag <query>     Show search diagnostics for a query
```

**List flags:**

| Flag | Description |
|------|-------------|
| `--account` | Filter by account name |
| `--mailbox` | Filter by mailbox name |
| `--format compact\|detailed` | Output format |
| `--limit N` | Max results |
| `--refresh` | Sync before listing |
| `--unread` | Unread messages only |

---

## Keybindings

### Navigation

| Key | Action |
|-----|--------|
| `j` / `k` or `↑` / `↓` | Move up/down |
| `ctrl+d` / `ctrl+u` | Page down/up |
| `g` / `G` | Jump to top/bottom |
| `Enter` | Open thread or message |
| `←` / `→` | Quick actions (inbox); `←` / `Esc` / `q` / `h` go back elsewhere |
| `i` | Jump to INBOX |
| `b` | Toggle sidebar |
| `\` | Focus sidebar |
| `Tab` / `Shift+Tab` | Next/prev account |

### Quick Actions (Inbox)

Press `←` or `→` on a selected thread to slide in an action panel from that edge. Press the same direction again to reveal the second action. Press once more to execute it, or press `Enter` at any step.

| Key | Step 0 | Step 1 |
|-----|--------|--------|
| `→` | Mark read/unread | Move (smart move) |
| `←` | Star/unstar | Delete |

Pressing the opposite direction, or any navigation key (`j`/`k` etc.), closes the menu.

### Mail Actions

| Key | Action |
|-----|--------|
| `c` | Compose new message |
| `r` | Reply |
| `R` | Reply all |
| `f` | Forward |
| `d` | Delete (move to Trash) |
| `v` | Move to folder |
| `s` | Toggle starred |
| `m` | Toggle read/unread |

### Search & Sync

| Key | Action |
|-----|--------|
| `/` | Local full-text search |
| `ctrl+f` | IMAP server search |
| `ctrl+r` | Force sync |

### General

| Key | Action |
|-----|--------|
| `?` | Help overlay |
| `q q` | Quit |
| `ctrl+c` | Force quit |

### Mouse

| Action | Result |
|--------|--------|
| Click sidebar item | Switch account or folder |
| Click thread row | Move cursor; click again to open |
| Click folder row | Move cursor; click again to open |
| Click status bar hint | Trigger that action |
| Click reader button (Open / Download / Editor / Copy Plain / Copy JSON / Reject) | Execute that button |
| Drag to select text | Highlight selection; release to copy as **Markdown** (bold → `**…**`, italic → `_…_`) |
| Ctrl + release drag | Copy selection as plain text instead |
| Scroll wheel | Scroll current view |

Selection is scoped to the element where the drag begins — dragging inside the email body will not capture sidebar or header text.

### Composer

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Next/prev field |
| `ctrl+Enter` | Send |
| `Esc` | Cancel |

### Folder Picker

| Key | Action |
|-----|--------|
| `w e r t y u i o p` | Quick-select folders 1–9 |

---

## License

MIT

---

## Notes For Contributors

- Read `docs/architecture.md` for the package/runtime overview.
- Read `docs/configuration.md` for the full config surface.
