# Unread Filter (`u` keybind) — Design Spec

**Date:** 2026-05-13

## Overview

Add a `u` keybind in the Mailbox (inbox) view that toggles a filter showing only unread threads. While active, the header row 2 displays a centered `"Only Showing Unread (N)"` banner alongside the existing breadcrumb.

---

## Architecture

### Filtering location: App layer (`app.go`)

The filter lives in `App` as `unreadOnly bool`. A helper `visibleMessages()` returns `loadedMessages` filtered to messages that belong to threads with unread mail when the flag is set. Every existing callsite that calls `thread.BuildThreads(a.loadedMessages)` is replaced with `thread.BuildThreads(a.visibleMessages())`.

This approach requires no changes to `InboxView` or the thread-building logic, and automatically applies to live syncs and incremental fetches.

**Unread message detection:** A message is considered unread when its `Flags` slice does not contain `data.FlagSeen`. `visibleMessages()` filters by this directly (no thread pre-build needed).

---

## Components

### 1. `keybindings.go`

- Add constant: `KeyUnreadFilter = "u"`
- Add help entry: `{"u", "Toggle unread-only filter"}`

### 2. `App` (`ui/app.go`)

- New field: `unreadOnly bool`
- New helper:
  ```go
  func (a *App) visibleMessages() []*data.Message {
      if !a.unreadOnly {
          return a.loadedMessages
      }
      out := make([]*data.Message, 0, len(a.loadedMessages))
      for _, m := range a.loadedMessages {
          if !m.HasFlag(data.FlagSeen) {
              out = append(out, m)
          }
      }
      return out
  }
  ```
- Replace all `thread.BuildThreads(a.loadedMessages)` with `thread.BuildThreads(a.visibleMessages())`.
- On `u` keypress (when `viewID == ViewInbox || viewID == ViewSmartFolder`, and no overlay is active):
  1. Toggle `a.unreadOnly`
  2. Rebuild threads from `visibleMessages()`
  3. Call `a.inboxView.SetThreads(threads)` (reset cursor/scroll)
  4. Compute `count = len(threads)` (number of unread threads shown)
  5. Call `a.header.SetUnreadFilter(a.unreadOnly, count)`

### 3. `Header` (`ui/header.go`)

- New fields: `unreadFilter bool`, `unreadCount int`
- New method: `SetUnreadFilter(on bool, count int)`
- Row 2 layout when `unreadFilter` is true:
  - Breadcrumb rendered at left as today (no change)
  - Banner: `"Only Showing Unread (N)"` styled with `theme.Accent` foreground, bold
  - Banner is **true-centered** in the full row width: padding between breadcrumb and banner is `(h.width - breadcrumbW - bannerW) / 2`. If there is insufficient space (breadcrumb too wide), the banner is placed immediately after a single space.
  - The row is still rendered as a single `Width(h.width)` string.

---

## Behaviour Details

| Scenario | Expected behaviour |
|---|---|
| Press `u` in inbox | Filter on; thread list resets to top; banner appears |
| Press `u` again | Filter off; full thread list restored; banner disappears |
| New messages arrive while filter is on | `visibleMessages()` applied automatically; count stays accurate |
| Navigate to another folder | `unreadOnly` resets to `false`; banner hidden |
| Filter active, all messages read | List shows empty state (existing `emptyState()` rendering) |
| Overlays active (search, compose…) | `u` ignored (overlay consumes input) |

Folder navigation resets the filter so the user always sees a full inbox when switching folders, matching the principle of least surprise.

---

## Non-goals

- No persistence of filter state across sessions.
- No filter on SmartFolder views beyond what `visibleMessages()` naturally provides.
- No changes to the sidebar unread counts (those remain message-level counts).
