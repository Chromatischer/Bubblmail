package ui

import (
	"fmt"
	"github.com/bubblmail/bubblmail/config"
	"strings"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// SearchChip is a structured filter attached to a search query.
type SearchChip struct {
	Key   string // "from", "to", "subject", "has"
	Value string // e.g. "alice@example.com" or "attachment"
}

// String returns the bracketed chip representation included in compiled queries.
func (c SearchChip) String() string {
	return fmt.Sprintf("[%s:%s]", c.Key, c.Value)
}

// SearchOverlay is a floating search box with FTS + semantic results.
type SearchOverlay struct {
	theme    *config.Theme
	width    int
	height   int
	active   bool
	query    string // free-text portion
	results  []*SearchResult
	list     components.ScrollList
	loading  bool
	minChars int

	// debounce: bumped on each keystroke; app fires search only when ID matches
	debounceID int

	// spinner frame for loading indicator
	spinnerFrame int

	// filter chips
	chips          []SearchChip
	chipKey        string   // chip type being built ("from","to","subject","has"), "" = normal
	chipInput      string   // value being typed for the active chip
	autocomplete   []string // filtered suggestions shown during chip entry
	acCursor       int      // cursor within autocomplete list
	knownAddresses []string // address list mined from cache on open

	// Geometry cached by View for mouse hit-testing. Deriving it a second time
	// in HitTestResult is how the two used to drift apart.
	resultY0   int
	resultRows int
}

// SearchResult holds a search match and its source labels.
type SearchResult struct {
	Message  *data.Message
	Score    float32
	Semantic bool
	Similar  bool
}

var searchSpinnerFrames = []string{
	icons.Spinner1, icons.Spinner2, icons.Spinner3,
	icons.Spinner4, icons.Spinner5, icons.Spinner6,
}

// chipPrefixes maps typed prefixes to chip keys.
var chipPrefixes = map[string]string{
	"from:":    "from",
	"to:":      "to",
	"subject:": "subject",
	"has:":     "has",
}

// NewSearchOverlay creates a new search overlay.
func NewSearchOverlay(theme *config.Theme) *SearchOverlay {
	return &SearchOverlay{theme: theme, minChars: 3}
}

// SetSize sets the overlay dimensions.
func (s *SearchOverlay) SetSize(w, h int) { s.width = w; s.height = h }

// SetKnownAddresses provides the address list used for from:/to: autocomplete.
func (s *SearchOverlay) SetKnownAddresses(addrs []string) { s.knownAddresses = addrs }

// Open opens the overlay and resets all state.
func (s *SearchOverlay) Open() {
	s.active = true
	s.query = ""
	s.chips = nil
	s.chipKey = ""
	s.chipInput = ""
	s.autocomplete = nil
	s.acCursor = 0
	s.results = nil
	s.list.Reset()
	s.loading = false
	s.debounceID = 0
}

// Close closes the overlay and clears state.
func (s *SearchOverlay) Close() {
	s.active = false
	s.query = ""
	s.chips = nil
	s.chipKey = ""
	s.chipInput = ""
	s.results = nil
	s.loading = false
}

// IsActive returns true if the overlay is open.
func (s *SearchOverlay) IsActive() bool { return s.active }

// IsLoading returns true if a search is in progress.
func (s *SearchOverlay) IsLoading() bool { return s.loading }

// Query returns the free-text portion of the current query.
func (s *SearchOverlay) Query() string { return s.query }

// CompiledQuery returns the full search string including chip filters, e.g.
// "[from:alice@x.com] [has:attachment] invoice". This is what gets sent to
// both local FTS and IMAP search backends.
func (s *SearchOverlay) CompiledQuery() string {
	if len(s.chips) == 0 {
		return s.query
	}
	var b strings.Builder
	for _, c := range s.chips {
		b.WriteString(c.String())
		b.WriteString(" ")
	}
	b.WriteString(s.query)
	return strings.TrimSpace(b.String())
}

// CanSearch reports whether there is enough input to trigger a search.
func (s *SearchOverlay) CanSearch() bool {
	if len(s.chips) > 0 {
		return true
	}
	return len([]rune(strings.TrimSpace(s.query))) >= s.minChars
}

// BumpDebounce increments and returns the debounce counter.
func (s *SearchOverlay) BumpDebounce() int {
	s.debounceID++
	return s.debounceID
}

// DebounceID returns the current debounce counter.
func (s *SearchOverlay) DebounceID() int { return s.debounceID }

// AdvanceSpinner steps the loading indicator by one frame.
func (s *SearchOverlay) AdvanceSpinner() {
	s.spinnerFrame = (s.spinnerFrame + 1) % len(searchSpinnerFrames)
}

// SetResults updates results from a completed IMAP search.
func (s *SearchOverlay) SetResults(msgs []*data.Message) {
	results := make([]*SearchResult, 0, len(msgs))
	for _, msg := range msgs {
		results = append(results, &SearchResult{Message: msg})
	}
	s.results = results
	s.loading = false
	s.list.Reset()
}

// SetSearchResults updates results from a streaming semantic search.
func (s *SearchOverlay) SetSearchResults(results []*SearchResult, loading bool) {
	if results == nil {
		results = []*SearchResult{}
	}
	s.results = results
	s.loading = loading
	if len(s.results) == 0 {
		s.list.Reset()
	}
	s.list.Clamp(len(s.results))
}

// SelectedMessage returns the currently focused result, or nil.
func (s *SearchOverlay) SelectedMessage() *data.Message {
	if s.list.Cursor < 0 || s.list.Cursor >= len(s.results) {
		return nil
	}
	return s.results[s.list.Cursor].Message
}

// Cursor returns the current cursor position.
func (s *SearchOverlay) Cursor() int { return s.list.Cursor }

// SetCursor moves the cursor to the given result index, adjusting the scroll offset.
func (s *SearchOverlay) SetCursor(idx int) {
	if idx < 0 || idx >= len(s.results) {
		return
	}
	s.list.Cursor = idx
	vr := s.visibleRowCount()
	if s.list.Cursor < s.list.Offset {
		s.list.Offset = s.list.Cursor
	} else if s.list.Cursor >= s.list.Offset+vr {
		s.list.Offset = s.list.Cursor - vr + 1
	}
}

// HitTestResult returns the result index for a click at the given content-area y,
// or -1 if no result row was hit. It reads the geometry View recorded.
func (s *SearchOverlay) HitTestResult(contentY int) int {
	i := contentY - s.resultY0
	if i < 0 || i >= s.resultRows || s.list.Offset+i >= len(s.results) {
		return -1
	}
	return s.list.Offset + i
}

// HandleKey processes a key press. Returns (queryChanged, closed, selected).
func (s *SearchOverlay) HandleKey(key string) (queryChanged bool, closed bool, selected bool) {
	// ── Chip-entry mode ──────────────────────────────────────────────────────
	if s.chipKey != "" {
		switch key {
		case "esc":
			// Abort chip entry — restore chip prefix to free text
			s.query += s.chipKey + ":"
			s.chipKey = ""
			s.chipInput = ""
			s.autocomplete = nil
			return true, false, false
		case "enter":
			val := s.commitChipValue()
			if val != "" {
				s.chips = append(s.chips, SearchChip{Key: s.chipKey, Value: val})
			}
			s.chipKey = ""
			s.chipInput = ""
			s.autocomplete = nil
			s.acCursor = 0
			s.list.Reset()
			return true, false, false
		case "up", "k":
			if s.acCursor > 0 {
				s.acCursor--
			}
		case "down", "j":
			if s.acCursor < len(s.autocomplete)-1 {
				s.acCursor++
			}
		case "backspace", "ctrl+h":
			if len(s.chipInput) > 0 {
				runes := []rune(s.chipInput)
				s.chipInput = string(runes[:len(runes)-1])
				s.updateAutocomplete()
				s.acCursor = 0
			} else {
				// Empty chip input: exit chip mode entirely
				s.chipKey = ""
				s.autocomplete = nil
			}
			return true, false, false
		default:
			if len(key) == 1 && key[0] >= 32 {
				s.chipInput += key
				s.updateAutocomplete()
				s.acCursor = 0
				return true, false, false
			}
		}
		return false, false, false
	}

	// ── Normal mode ──────────────────────────────────────────────────────────
	switch key {
	case "esc":
		s.Close()
		return false, true, false
	case "enter":
		if len(s.results) > 0 {
			return false, true, true
		}
		return false, false, false
	case "up", "k":
		s.list.MoveUp()
	case "down", "j":
		s.list.MoveDown(len(s.results), s.visibleRowCount())
	case "left":
		// Move cursor into the last chip for editing (no deletion).
		if s.query == "" && len(s.chips) > 0 {
			last := s.chips[len(s.chips)-1]
			s.chips = s.chips[:len(s.chips)-1]
			s.chipKey = last.Key
			s.chipInput = last.Value
			s.updateAutocomplete()
			s.acCursor = 0
			return true, false, false
		}
	case "backspace", "ctrl+h":
		if len(s.query) > 0 {
			runes := []rune(s.query)
			s.query = string(runes[:len(runes)-1])
			s.list.Reset()
			return true, false, false
		}
		// Enter the last chip for editing, deleting its last character.
		if len(s.chips) > 0 {
			last := s.chips[len(s.chips)-1]
			s.chips = s.chips[:len(s.chips)-1]
			s.chipKey = last.Key
			runes := []rune(last.Value)
			if len(runes) > 0 {
				s.chipInput = string(runes[:len(runes)-1])
			} else {
				s.chipInput = ""
			}
			s.updateAutocomplete()
			s.acCursor = 0
			s.list.Reset()
			return true, false, false
		}
	default:
		if len(key) == 1 && key[0] >= 32 {
			s.query += key
			// Check if query ends with a chip prefix trigger
			if chipKey, triggered := s.detectChipTrigger(); triggered {
				s.chipKey = chipKey
				s.chipInput = ""
				s.updateAutocomplete()
				s.acCursor = 0
				return true, false, false
			}
			s.list.Reset()
			return true, false, false
		}
	}
	return false, false, false
}

// detectChipTrigger checks if the current query ends with a chip prefix like
// "from:". If so, it strips the prefix from query and returns the chip key.
func (s *SearchOverlay) detectChipTrigger() (string, bool) {
	for prefix, key := range chipPrefixes {
		if strings.HasSuffix(s.query, prefix) {
			s.query = strings.TrimSuffix(s.query, prefix)
			// Strip any trailing space that was before the prefix
			s.query = strings.TrimRight(s.query, " ")
			return key, true
		}
	}
	return "", false
}

// updateAutocomplete rebuilds the filtered autocomplete list for the current
// chip type and chipInput.
func (s *SearchOverlay) updateAutocomplete() {
	switch s.chipKey {
	case "from", "to":
		input := strings.ToLower(s.chipInput)
		s.autocomplete = s.autocomplete[:0]
		for _, addr := range s.knownAddresses {
			if input == "" || strings.Contains(strings.ToLower(addr), input) {
				s.autocomplete = append(s.autocomplete, addr)
				if len(s.autocomplete) >= 6 {
					break
				}
			}
		}
	case "has":
		if strings.HasPrefix("attachment", strings.ToLower(s.chipInput)) {
			s.autocomplete = []string{"attachment"}
		} else {
			s.autocomplete = nil
		}
	default:
		s.autocomplete = nil
	}
}

// commitChipValue returns the value to commit for the active chip. If an
// autocomplete item is selected it is used; otherwise the raw chipInput.
func (s *SearchOverlay) commitChipValue() string {
	if s.acCursor >= 0 && s.acCursor < len(s.autocomplete) {
		return s.autocomplete[s.acCursor]
	}
	return strings.TrimSpace(s.chipInput)
}

// boxWidth is the overlay's outer width.
//
// The overlay used to fill the terminal, which meant a search with no results
// yet was a full screen of empty box. It now takes only the width a mail row
// actually needs, capped so the lines stay scannable on a wide terminal.
func (s *SearchOverlay) boxWidth() int {
	w := s.width - 8
	if w > 108 {
		w = 108
	}
	if w < 48 {
		w = 48
	}
	if w > s.width {
		w = s.width
	}
	return w
}

// chrome rows inside the box: input, divider, footer divider, footer.
const searchChromeRows = 4

// maxResultRows is how many result rows the terminal can accommodate.
func (s *SearchOverlay) maxResultRows() int {
	// 2 border + 2 padding rows, plus a little breathing room top and bottom.
	n := s.height - 8 - searchChromeRows - s.acRowCount()
	if n < 1 {
		n = 1
	}
	return n
}

// acRowCount returns how many autocomplete rows are currently displayed.
func (s *SearchOverlay) acRowCount() int {
	if s.chipKey == "" || len(s.autocomplete) == 0 {
		return 0
	}
	n := len(s.autocomplete)
	if n > 4 {
		n = 4
	}
	return n + 1 // +1 for the AC divider line
}

// visibleRowCount returns how many result rows the overlay shows. The box
// grows with the result set and stops growing at the terminal's limit, so an
// empty search is a small box rather than an empty screen.
func (s *SearchOverlay) visibleRowCount() int {
	maxRows := s.maxResultRows()
	if len(s.results) == 0 {
		// Enough to centre a one-line state message without the box collapsing.
		if maxRows < 3 {
			return maxRows
		}
		return 3
	}
	if len(s.results) < maxRows {
		return len(s.results)
	}
	return maxRows
}

// View renders the search overlay centered over the terminal.
func (s *SearchOverlay) View() string {
	theme := s.theme
	surf := theme.Surface

	boxW := s.boxWidth()
	// innerW: content area inside Padding(1, 2) = boxW − 4
	innerW := boxW - 4
	if innerW < 20 {
		innerW = 20
	}
	vr := s.visibleRowCount()

	// ── Input row ────────────────────────────────────────────────────────────
	ti := components.NewTextInput(theme)
	ti.Placeholder = "Search mail…   from:  to:  subject:  has:"
	ti.Icon = icons.Search
	ti.Active = true
	for _, c := range s.chips {
		ti.Chips = append(ti.Chips, c.Key+":"+c.Value)
	}
	if s.chipKey != "" {
		ti.Value = s.chipKey + ":" + s.chipInput
	} else {
		ti.Value = s.query
	}
	inputRow := ti.Render(innerW)

	lines := []string{inputRow, components.Divider(theme, innerW)}

	// ── Autocomplete rows (chip mode only) ───────────────────────────────────
	if s.chipKey != "" && len(s.autocomplete) > 0 {
		maxAC := len(s.autocomplete)
		if maxAC > 4 {
			maxAC = 4
		}
		for i := 0; i < maxAC; i++ {
			bg, fg := surf, theme.Text
			gut := components.GutterNone
			if i == s.acCursor {
				bg, fg, gut = theme.SurfaceAlt, theme.Text, components.GutterActive
			}
			lines = append(lines, components.GutterCell(theme, gut, bg)+
				lipgloss.NewStyle().Width(innerW-1).Background(bg).Foreground(fg).
					Render(" "+util.TruncateText(s.autocomplete[i], innerW-3)))
		}
		lines = append(lines, components.Divider(theme, innerW))
	}

	// resultY0 is where the first result row lands relative to the content
	// area, which is what mouse hit-testing needs.
	boxTopMargin := (s.height - (vr + searchChromeRows + len(lines) - 2 + 2)) / 2
	if boxTopMargin < 0 {
		boxTopMargin = 0
	}
	s.resultY0 = boxTopMargin + 2 + len(lines) // border + top padding + chrome rows
	s.resultRows = vr

	// ── Result rows ──────────────────────────────────────────────────────────
	blankLine := components.Fill(innerW, surf)
	if len(s.results) == 0 {
		lines = append(lines, s.stateRows(vr, innerW, surf)...)
	} else {
		track := components.Scrollbar(theme, len(s.results), vr, s.list.Offset, vr, surf)
		for i := 0; i < vr; i++ {
			idx := s.list.Offset + i
			if idx >= len(s.results) {
				lines = append(lines, blankLine)
				continue
			}
			row := s.renderRow(s.results[idx], idx == s.list.Cursor, innerW-1)
			lines = append(lines, row+track[i])
		}
	}

	// ── Footer ───────────────────────────────────────────────────────────────
	lines = append(lines, components.Divider(theme, innerW), s.footer(innerW, surf))

	return components.ModalBox(theme, strings.Join(lines, "\n"), boxW, 0, s.width, s.height)
}

// stateRows renders the centred message shown when there are no results yet.
func (s *SearchOverlay) stateRows(vr, innerW int, surf lipgloss.Color) []string {
	theme := s.theme

	var msg string
	var color lipgloss.Color
	switch {
	case !s.CanSearch():
		msg, color = "Keep typing…", theme.TextFaint
		if s.query == "" && len(s.chips) == 0 {
			msg = "Type to search this account"
		}
	case s.loading:
		msg, color = searchSpinnerFrames[s.spinnerFrame]+" Searching…", theme.TextMuted
	default:
		msg, color = "No matches", theme.TextMuted
	}

	rows := make([]string, vr)
	for i := range rows {
		rows[i] = components.Fill(innerW, surf)
	}
	rows[vr/2] = lipgloss.NewStyle().
		Width(innerW).Background(surf).Foreground(color).Align(lipgloss.Center).
		Render(msg)
	return rows
}

// footer shows the result count on the left and streaming state on the right.
func (s *SearchOverlay) footer(innerW int, surf lipgloss.Color) string {
	theme := s.theme

	left := ""
	if len(s.results) > 0 {
		left = util.PluralCount(len(s.results), "result", "results")
	}
	right := ""
	if s.loading && len(s.results) > 0 {
		right = searchSpinnerFrames[s.spinnerFrame] + " streaming…"
	} else if len(s.results) > 0 {
		right = "* semantic   ~ similar"
	}

	gap := innerW - util.VisibleWidth(left) - util.VisibleWidth(right)
	if gap < 1 {
		gap = 1
		right = ""
	}
	return lipgloss.NewStyle().Background(surf).Foreground(theme.TextFaint).Render(left) +
		components.Fill(gap, surf) +
		lipgloss.NewStyle().Background(surf).Foreground(theme.TextFaint).Render(right)
}

// inputDisplayValue builds the text shown in the input box: committed chips
// followed by the current free-text query or chip-in-progress value.
func (s *SearchOverlay) inputDisplayValue() string {
	var b strings.Builder
	for _, c := range s.chips {
		b.WriteString(c.String())
		b.WriteString(" ")
	}
	if s.chipKey != "" {
		b.WriteString(s.chipKey + ":" + s.chipInput)
	} else {
		b.WriteString(s.query)
	}
	return b.String()
}

// renderRow renders a single search result at the given width.
// All widths are computed from plain strings; each cell uses Width(N) to
// self-pad, so the columns hold whatever the sender name contains.
func (s *SearchOverlay) renderRow(res *SearchResult, selected bool, innerW int) string {
	theme := s.theme
	msg := res.Message

	bg := theme.Surface
	rowFg, metaFg := theme.Text, theme.TextMuted
	gut := components.GutterNone
	if selected {
		bg, rowFg, metaFg, gut = theme.SurfaceAlt, theme.Text, theme.TextMuted, components.GutterActive
	}

	cell := func(w int, fg lipgloss.Color, bold bool, plain string) string {
		return lipgloss.NewStyle().
			Width(w).Background(bg).Foreground(fg).Bold(bold).
			Render(util.TruncateText(util.SingleLine(plain), w))
	}
	sp := components.Fill(1, bg)

	// Unread marker (1 col)
	unreadCell := sp
	if !msg.IsRead() {
		unreadCell = lipgloss.NewStyle().Width(1).Background(bg).Foreground(theme.Unread).
			Render(icons.Unread)
	}

	// Match kind: 1 col, immediately before the date.
	const indW = 1
	indChar, indFg := " ", bg
	switch {
	case res.Semantic:
		indChar, indFg = "*", theme.Accent
	case res.Similar:
		indChar, indFg = "~", theme.TextMuted
	}
	indCell := lipgloss.NewStyle().Width(indW).Background(bg).Foreground(indFg).Render(indChar)

	// Date — fixed width so the marker column stays vertically aligned.
	const dateW = 6
	dateFmt := util.FormatDate(msg.Date)
	if dateFmt == "Yesterday" {
		dateFmt = "Yest."
	}
	dateCell := cell(dateW, metaFg, false, dateFmt)

	// From gets a fixed share; the subject absorbs the rest.
	fromW := innerW / 4
	if fromW < 10 {
		fromW = 10
	}
	if fromW > 22 {
		fromW = 22
	}
	subjectW := innerW - 1 - 1 - fromW - 1 - indW - 1 - dateW
	if subjectW < 1 {
		subjectW = 1
	}

	subjFg, subjBold := metaFg, false
	if !msg.IsRead() {
		subjFg, subjBold = rowFg, true
	}

	return components.GutterCell(theme, gut, bg) +
		unreadCell +
		cell(fromW, rowFg, false, msg.FromString()) + sp +
		cell(subjectW, subjFg, subjBold, msg.Subject) + sp +
		indCell + sp +
		dateCell
}
