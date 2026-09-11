# Bubblmail — Notes for Claude

## Terminal rendering gotchas

### Strip ZWJ (U+200D) from user-supplied strings before display

Terminals like Alacritty do not compose ZWJ sequences — they render each
component glyph independently, so `🧑‍🍳` occupies 4 cols even though every
width library reports 2. Strip U+200D before truncation and layout.
`util.SingleLine` already does this; always pass display strings through it.

Do not try to fix this with a smarter width function. Stripping is the right fix.

### `Width(N).Render(s)` only works when library width == terminal width

lipgloss pads based on library measurement. If the terminal renders `s` wider
than the library thinks, the cell overflows. Only use `Width(N)` on content that
has already been sanitised.

### Never measure a pre-rendered ANSI string to compute layout widths

ANSI escape sequences change length when style changes (e.g. selected vs
unselected), making the measured width unstable. Compute all widths from plain
strings only, then use `Width(N)` on each cell so it self-pads. No gap
arithmetic against ANSI strings.

## UI componentisation

Extract repeated rendering logic into `ui/components/components.go` when the
same pattern appears in two or more places. Prefer simple functions or small
structs — don't over-abstract.

Examples of what lives there already and why:

- **`Divider(theme, width)`** — `─`-filled horizontal rule; was copy-pasted
  verbatim into `new_folder.go`, `folder_picker.go`, and `search.go`.
- **`ModalBox(theme, content, boxW, boxH, fullW, fullH)`** — rounded-border
  surface box centred with `lipgloss.Place`; identical in all four overlay
  files. Pass 0 for boxW/boxH to let content size that dimension.
- **`ScrollList`** — cursor + scroll-offset state with `MoveUp`, `MoveDown`,
  `Reset`, and `Clamp`; the navigation logic was duplicated between
  `FolderPickerOverlay` and `SearchOverlay`.
- **`TextInput`** — styled single-line input with icon prefix; used by Search,
  FolderPicker, and NewFolder overlays.
- **`RenderButton`** — accent/danger button; used in Composer.

The shared visual vocabulary lives alongside them, one concern per file:

- **`chrome.go`** — `GutterCell` (the 1-col `▌` cursor/selection marker used by
  the sidebar, inbox, folder view and folder picker), `SectionLabel` (a quiet
  uppercase heading for a group of list rows — no trailing rule), `PaneTitle`
  (accent, mixed case, for the name of an overlay or pane), `Fill` (closes a
  partially drawn row), `ChromeStyle` and `PaneStyle`.
- **`hints.go`** — `Hint` plus `HintBar`, the single unit of key affordance.
  The bar never wraps and never overflows: it drops hints by descending
  `Priority` and appends a faint `+N`. It returns `HintZone`s — cache them and
  hit-test against them instead of re-deriving the layout in the click handler.
  `HintColumns` renders the same hints as an aligned two-column block (help).
- **`badge.go`** — `Pill` (filter chips, tags) and `Count` (`●12` unread badge).
- **`scrollbar.go`** — `Scrollbar` returns `height` single-column rows, blank
  when everything fits, so the gutter disappears rather than lying.
- **`panel.go`** — `EmptyState`, `FieldRow`, `Card`, `Pad`, `JoinRows`.

`SectionLabel` is `TintedLabel` with `TextFaint`; call `TintedLabel` directly
when a heading should carry an identity hue.

### The panes are transparent — do not paint them

The user runs a translucent terminal. The content area, the sidebar rows and the
reader all emit **no background**, so whatever is behind the terminal shows
through the app. An earlier revision filled every region with `theme.Chrome` and
`theme.Background` to define the regions by colour; that looked fine on an
opaque terminal and destroyed a transparent one. Do not reintroduce it.

Rules that follow from this:

- **Pass `""` as the background colour** to `Fill`, `SectionLabel`, `GutterCell`,
  `Pad` and friends from any pane. They only set `Background(...)` when the
  colour is non-empty.
- **Fills are small deliberate patches, never planes.** The only opaque things
  on screen are the cursor row (`SurfaceAlt`), marked rows (`Surface`), modal
  cards, pills and the focused button. Audit with
  `tmux capture-pane -pe | grep -o '48;2;[0-9;]*'` — a handful of hits is
  correct, a screenful is a regression.
- **Regions are separated by one thin edge, not by rules.** `ui/sidebar.go` draws
  a single `▏` (`sidebarEdge`) down its right side, tinted `Border` normally and
  `Accent` when the sidebar has focus. There is no rule under the header, none
  above the status bar, and none between date groups — a blank row does that job
  in the mail list.
- **Do not set `BorderBackground` or `WithWhitespaceBackground`.** A floating
  card is supposed to float; the gutters around a centred overlay stay
  transparent on purpose.

### Colour is either state or identity, never decoration

There are two colour systems and they must not be confused.

**State** is `Accent`, `Unread`, `Starred`, `Error`, `Success`, `Warning`. One
accent, one highlight (see below). These say what the app is doing.

**Identity** is `theme.Palette`, an eight-hue ring reached through
`theme.Hue(key)` (stable hash) or `theme.PaletteAt(i)` (fixed meaning). These say
*which thing this is*, and the same thing must get the same hue everywhere:

- a sender colours its unread dot in the list, its name and its card bar in the
  reader, and its `TintedLabel` heading inside a thread — all from
  `theme.Hue(msg.FromKey())`;
- a folder colours its glyph in the sidebar and the same glyph in the header
  breadcrumb, via `folderTint`; the standard mailboxes have fixed hues (sent is
  green, trash is `Error`, junk is `Warning`) and the rest are hashed, so a
  user's own tree is colour-coded without any code change;
- a tag colours its pill from its own name, so a label looks the same in every
  folder without any stored state.

Identity hue lands on **glyphs and single words**, never on body text or a whole
row. A row still gets at most two foreground colours.

### One accent, one highlight

`theme.Accent` is the only interactive colour. There is no second highlight
token: the cursor row is a one-step lift in fill (`SurfaceAlt`) plus the accent
gutter, in every list. The mail list used to fill the cursor row edge-to-edge
with a saturated blue `Selected` colour, which both shouted and gave the app two
competing accents; that token has been removed from the theme. Do not add it
back. Alternating row fills are out for the same reason — with two rows per
thread they band the whole pane.

Two more rules that the components exist to enforce:

1. **One source of truth for geometry.** If `View()` draws it, `View()` records
   where it drew it. Search, the composer footer and the draft prompt each used
   to recompute their layout inside the hit-test, and each pair had drifted.
2. **Key hints are data, not strings.** Bindings live as literals in
   `App.Update`; `HelpGroups` in `ui/keybindings.go` is the only description of
   them. A parallel table of `Key*` constants referenced by nothing used to sit
   there and had gone stale.

When adding a new overlay or list, reach for these before writing inline
lipgloss styles. When you notice a third copy of a pattern, extract it.

## Persisted UI state

`config/state.go` writes `~/.config/bubblmail/state.json` — folded folders and
whether the sidebar is hidden. It is deliberately **not** `config.toml`: writing
that file on every fold would reformat the user's comments away. Load failures
are ignored (state is a convenience) and `Save` goes through a temp file plus
rename. Folders are keyed by `config.FolderKey(account, mailbox)`, which joins
the two with a tab because no IMAP mailbox name may contain one.

The sidebar owns the fold state and calls `OnFoldChange` after every change; the
app persists from there. Parenthood is derived from the depth-ordered folder
list (`hasChildren`), not stored — a child can only ever be the next entry.
`SetActive` unfolds a folder's ancestors, so the folder being read is never
hidden.

## Git commits

Never add a `Co-Authored-By` trailer or any Claude attribution to commit messages.
