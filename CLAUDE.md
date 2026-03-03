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
