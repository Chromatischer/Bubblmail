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

When adding a new overlay or list, reach for these before writing inline
lipgloss styles. When you notice a third copy of a pattern, extract it.

## Git commits

Never add a `Co-Authored-By` trailer or any Claude attribution to commit messages.
