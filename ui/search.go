package ui

import (
	"fmt"
	"strings"

	"github.com/bubblmail/bubblmail/config"
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
	styles   *Styles
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
func NewSearchOverlay(styles *Styles) *SearchOverlay {
	return &SearchOverlay{styles: styles, minChars: 3}
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
// or -1 if no result row was hit.
func (s *SearchOverlay) HitTestResult(contentY int) int {
	// Geometry: box border(1)+padding(1)=2 rows overhead, then inputRow(1)+divider(1)=2,
	// plus autocomplete rows (if shown), then results.
	resultY0 := 5 + s.acRowCount()
	i := contentY - resultY0
	if i < 0 || i >= s.visibleRowCount() || s.list.Offset+i >= len(s.results) {
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

// visibleRowCount returns how many result rows fit inside the overlay.
func (s *SearchOverlay) visibleRowCount() int {
	boxH := s.height - 4
	if boxH < 10 {
		boxH = 10
	}
	// Padding(1,2) = 2 rows; input(1)+divider(1)+footer(1) = 3; ac rows on top.
	vr := boxH - 5 - s.acRowCount()
	if vr < 1 {
		vr = 1
	}
	return vr
}

// View renders the search overlay centered over the terminal.
func (s *SearchOverlay) View() string {
	theme := s.styles.Theme

	boxW := s.width - 4
	if boxW < 44 {
		boxW = 44
	}
	boxH := s.height - 4
	if boxH < 10 {
		boxH = 10
	}

	// innerW: content area inside Padding(1, 2) = boxW − 4
	innerW := boxW - 4
	if innerW < 10 {
		innerW = 10
	}

	vr := s.visibleRowCount()

	surf := theme.Surface

	// bg-colored single-space helper used between cells so resets don't expose
	// the terminal default background between styled blocks.
	sp := func(bg lipgloss.Color) string {
		return lipgloss.NewStyle().Background(bg).Render(" ")
	}

	// ── Input row ────────────────────────────────────────────────────────────
	ti := components.NewTextInput(theme)
	ti.Placeholder = "Search mail… (type from: to: subject: has:)"
	ti.Value = s.inputDisplayValue()
	ti.Icon = icons.Search
	ti.Active = s.chipKey == ""

	inputRow := ti.Render(innerW)

	// ── Divider ──────────────────────────────────────────────────────────────
	divider := components.Divider(theme, innerW)

	// ── Autocomplete rows (chip mode only) ───────────────────────────────────
	var acLines []string
	if s.chipKey != "" && len(s.autocomplete) > 0 {
		maxAC := len(s.autocomplete)
		if maxAC > 4 {
			maxAC = 4
		}
		for i := 0; i < maxAC; i++ {
			addr := s.autocomplete[i]
			selected := i == s.acCursor
			bg := surf
			fg := theme.Text
			if selected {
				bg = theme.Selected
				fg = theme.Background
			}
			row := lipgloss.NewStyle().
				Width(innerW).
				Background(bg).
				Foreground(fg).
				Render(" " + util.TruncateText(addr, innerW-2))
			acLines = append(acLines, row)
		}
		// Separator below AC list
		acLines = append(acLines, components.Divider(theme, innerW))
	}

	// ── Result rows ──────────────────────────────────────────────────────────
	blankLine := lipgloss.NewStyle().Background(surf).Width(innerW).Render("")
	var resultLines []string

	if len(s.results) == 0 {
		var stateMsg string
		var stateColor lipgloss.Color
		switch {
		case !s.CanSearch():
			if s.query == "" && len(s.chips) == 0 {
				stateMsg = "Type to search, or use from: to: subject: has:"
			} else {
				stateMsg = "Keep typing…"
			}
			stateColor = theme.TextFaint
		case s.loading:
			stateMsg = searchSpinnerFrames[s.spinnerFrame] + " Searching…"
			stateColor = theme.TextMuted
		default:
			stateMsg = "No results"
			stateColor = theme.TextMuted
		}
		topPad := (vr - 1) / 2
		for i := 0; i < topPad; i++ {
			resultLines = append(resultLines, blankLine)
		}
		resultLines = append(resultLines,
			lipgloss.NewStyle().
				Width(innerW).
				Background(surf).
				Foreground(stateColor).
				Align(lipgloss.Center).
				Render(stateMsg),
		)
		for len(resultLines) < vr {
			resultLines = append(resultLines, blankLine)
		}
	} else {
		// Reserve a right-edge scrollbar column when results overflow the viewport.
		scrolling := len(s.results) > vr
		contentW := innerW
		if scrolling {
			contentW = innerW - 1
		}
		thumbStart, thumbEnd := 0, 0
		if scrolling {
			total := len(s.results)
			thumb := vr * vr / total
			if thumb < 1 {
				thumb = 1
			}
			pos := 0
			if maxOff := total - vr; maxOff > 0 {
				pos = s.list.Offset * (vr - thumb) / maxOff
			}
			thumbStart, thumbEnd = pos, pos+thumb
		}
		for i := s.list.Offset; i < len(s.results) && i < s.list.Offset+vr; i++ {
			row := s.renderRow(s.results[i], i == s.list.Cursor, contentW, sp)
			if scrolling {
				r := i - s.list.Offset
				row += scrollbarCell(theme, r >= thumbStart && r < thumbEnd)
			}
			resultLines = append(resultLines, row)
		}
		for len(resultLines) < vr {
			resultLines = append(resultLines, blankLine)
		}
	}

	// ── Footer ───────────────────────────────────────────────────────────────
	var footerStr string
	if len(s.results) > 0 {
		countTxt := util.PluralCount(len(s.results), "result", "results")
		countCell := lipgloss.NewStyle().
			Background(surf).
			Foreground(theme.TextFaint).
			Render(countTxt)
		if s.loading {
			spinTxt := searchSpinnerFrames[s.spinnerFrame] + " streaming…"
			spinCell := lipgloss.NewStyle().
				Background(surf).
				Foreground(theme.TextMuted).
				Render(spinTxt)
			gap := innerW - util.VisibleWidth(countTxt) - util.VisibleWidth(spinTxt)
			if gap < 1 {
				gap = 1
			}
			gapCell := lipgloss.NewStyle().Background(surf).Render(strings.Repeat(" ", gap))
			footerStr = countCell + gapCell + spinCell
		} else {
			footerStr = countCell
		}
	} else {
		footerStr = blankLine
	}

	// ── Assemble ─────────────────────────────────────────────────────────────
	lines := make([]string, 0, 3+len(acLines)+vr)
	lines = append(lines, inputRow, divider)
	lines = append(lines, acLines...)
	lines = append(lines, resultLines...)
	lines = append(lines, footerStr)

	return components.ModalBox(theme, strings.Join(lines, "\n"), boxW, boxH, s.width, s.height)
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
// All widths are computed from plain strings; each cell uses Width(N) to self-pad.
// sp is a pre-built bg-colored single space injected between cells so that
// resets from inner styles don't expose the terminal's default background.
func (s *SearchOverlay) renderRow(res *SearchResult, selected bool, innerW int, sp func(lipgloss.Color) string) string {
	theme := s.styles.Theme
	msg := res.Message

	bg := theme.Surface
	if selected {
		bg = theme.Selected
	}

	var rowFg, metaFg lipgloss.Color
	if selected {
		rowFg = theme.Background
		metaFg = theme.Background
	} else {
		rowFg = theme.Text
		metaFg = theme.TextMuted
	}

	cell := func(w int, fg lipgloss.Color, plain string) string {
		return lipgloss.NewStyle().
			Width(w).
			Background(bg).
			Foreground(fg).
			Render(plain)
	}

	// Unread indicator (1 col)
	var unreadChar string
	var unreadFg lipgloss.Color
	if !msg.IsRead() {
		unreadChar = icons.Unread
		unreadFg = theme.Unread
	} else {
		unreadChar = " "
		unreadFg = bg
	}
	unreadCell := cell(1, unreadFg, unreadChar)

	// Date — fixed width so the indicator column stays vertically aligned.
	const dateW = 6
	dateFmt := util.FormatDate(msg.Date)
	if dateFmt == "Yesterday" {
		dateFmt = "Yest."
	}
	dateRaw := util.TruncateText(util.SingleLine(dateFmt), dateW)
	dateCell := cell(dateW, metaFg, dateRaw)

	// Match-type indicator: 1-col character right before the date.
	const indCellW = 2
	var indChar string
	var indFg lipgloss.Color
	switch {
	case res.Semantic:
		indChar = "*"
		indFg = theme.Accent
	case res.Similar:
		indChar = "~"
		indFg = theme.TextMuted
	default:
		indChar = " "
		indFg = bg
	}
	indCell := cell(indCellW, indFg, indChar)

	suffixW := 1 + indCellW + 1 + dateW

	// From (fixed 18 cols)
	const fromW = 18
	fromCell := cell(fromW, rowFg, util.TruncateText(util.SingleLine(msg.FromString()), fromW))

	// Subject fills remaining space, with the free-text query highlighted.
	subjectW := innerW - 1 - 1 - fromW - 1 - suffixW
	if subjectW < 1 {
		subjectW = 1
	}
	subjectFg := rowFg
	if !selected && msg.IsRead() {
		subjectFg = theme.TextMuted
	}
	subjectCell := highlightedCell(theme, bg, subjectFg, msg.Subject, s.query, subjectW, selected)

	spc := sp(bg)
	suffix := spc + indCell + spc + dateCell

	return unreadCell + spc + fromCell + spc + subjectCell + suffix
}

// highlightedCell renders plain into an exact w-wide cell on background bg,
// accenting the first case-insensitive occurrence of query. Width is measured
// from plain strings only; segments each carry bg so it fills uniformly.
func highlightedCell(theme *config.Theme, bg, fg lipgloss.Color, plain, query string, w int, selected bool) string {
	plain = util.TruncateText(util.SingleLine(plain), w)
	base := lipgloss.NewStyle().Background(bg).Foreground(fg)

	ms, me := -1, -1
	if query != "" {
		ms, me = findMatchRange(plain, query)
	}
	if ms < 0 {
		return base.Width(w).Render(plain)
	}

	match := lipgloss.NewStyle().Background(bg).Bold(true)
	if selected {
		match = match.Foreground(fg).Underline(true)
	} else {
		match = match.Foreground(theme.Accent)
	}

	var b strings.Builder
	b.WriteString(base.Render(plain[:ms]))
	b.WriteString(match.Render(plain[ms:me]))
	b.WriteString(base.Render(plain[me:]))
	if pad := w - util.VisibleWidth(plain); pad > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}
