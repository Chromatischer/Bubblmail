package ui

import (
	"strings"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// SearchOverlay is a floating search box with FTS + semantic results.
type SearchOverlay struct {
	styles   *Styles
	width    int
	height   int
	active   bool
	query    string
	results  []*SearchResult
	cursor   int
	offset   int
	loading  bool
	minChars int

	// debounce: bumped on each keystroke; app fires search only when ID matches
	debounceID int

	// spinner frame for loading indicator
	spinnerFrame int
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

// NewSearchOverlay creates a new search overlay.
func NewSearchOverlay(styles *Styles) *SearchOverlay {
	return &SearchOverlay{styles: styles, minChars: 3}
}

// SetSize sets the overlay dimensions.
func (s *SearchOverlay) SetSize(w, h int) { s.width = w; s.height = h }

// Open opens the overlay and resets all state.
func (s *SearchOverlay) Open() {
	s.active = true
	s.query = ""
	s.results = nil
	s.cursor = 0
	s.offset = 0
	s.loading = false
	s.debounceID = 0
}

// Close closes the overlay and clears state.
func (s *SearchOverlay) Close() {
	s.active = false
	s.query = ""
	s.results = nil
	s.loading = false
}

// IsActive returns true if the overlay is open.
func (s *SearchOverlay) IsActive() bool { return s.active }

// IsLoading returns true if a search is in progress.
func (s *SearchOverlay) IsLoading() bool { return s.loading }

// Query returns the current search query.
func (s *SearchOverlay) Query() string { return s.query }

// CanSearch reports whether the query is long enough to trigger a search.
func (s *SearchOverlay) CanSearch() bool {
	return len([]rune(strings.TrimSpace(s.query))) >= s.minChars
}

// BumpDebounce increments and returns the debounce counter.
// The caller stores the returned ID and fires search only if the ID still
// matches after the debounce delay.
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
	s.cursor = 0
	s.offset = 0
}

// SetSearchResults updates results from a streaming semantic search.
func (s *SearchOverlay) SetSearchResults(results []*SearchResult, loading bool) {
	if results == nil {
		results = []*SearchResult{}
	}
	s.results = results
	s.loading = loading
	if len(s.results) == 0 {
		s.cursor = 0
		s.offset = 0
	}
	if s.cursor >= len(s.results) {
		s.cursor = len(s.results) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

// SelectedMessage returns the currently focused result, or nil.
func (s *SearchOverlay) SelectedMessage() *data.Message {
	if s.cursor < 0 || s.cursor >= len(s.results) {
		return nil
	}
	return s.results[s.cursor].Message
}

// HandleKey processes a key press. Returns (queryChanged, closed, selected).
func (s *SearchOverlay) HandleKey(key string) (queryChanged bool, closed bool, selected bool) {
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
		if s.cursor > 0 {
			s.cursor--
			if s.cursor < s.offset {
				s.offset--
			}
		}
	case "down", "j":
		if s.cursor < len(s.results)-1 {
			s.cursor++
			if s.cursor >= s.offset+s.visibleRowCount() {
				s.offset++
			}
		}
	case "backspace", "ctrl+h":
		if len(s.query) > 0 {
			runes := []rune(s.query)
			s.query = string(runes[:len(runes)-1])
			s.cursor = 0
			s.offset = 0
			return true, false, false
		}
	default:
		if len(key) == 1 && key[0] >= 32 {
			s.query += key
			s.cursor = 0
			s.offset = 0
			return true, false, false
		}
	}
	return false, false, false
}

// visibleRowCount returns how many result rows fit inside the overlay.
func (s *SearchOverlay) visibleRowCount() int {
	boxH := s.height - 4
	if boxH < 10 {
		boxH = 10
	}
	// Padding(1,2) consumes 2 rows; input + divider + footer consume 3 more.
	vr := boxH - 5
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
	// Two adjacent Width(N) cells, each with explicit Background(Surface).
	// iconCellW=2: icon(1) + 1 padding from Width(2).
	const iconCellW = 2
	iconCell := lipgloss.NewStyle().
		Width(iconCellW).
		Background(surf).
		Foreground(theme.Accent).
		Render(icons.Search)

	inputAreaW := innerW - iconCellW
	if inputAreaW < 1 {
		inputAreaW = 1
	}
	var inputCell string
	if s.query == "" {
		inputCell = lipgloss.NewStyle().
			Width(inputAreaW).
			Background(surf).
			Foreground(theme.TextFaint).
			Render("Search mail…")
	} else {
		// leave 1 col for the cursor glyph; truncate then append it
		displayQ := util.TruncateText(util.SingleLine(s.query), inputAreaW-1) + "▌"
		inputCell = lipgloss.NewStyle().
			Width(inputAreaW).
			Background(surf).
			Foreground(theme.Text).
			Bold(true).
			Render(displayQ)
	}
	inputRow := iconCell + inputCell

	// ── Divider ──────────────────────────────────────────────────────────────
	divider := lipgloss.NewStyle().
		Background(surf).
		Foreground(theme.Border).
		Render(strings.Repeat("─", innerW))

	// ── Result rows ──────────────────────────────────────────────────────────
	blankLine := lipgloss.NewStyle().Background(surf).Width(innerW).Render("")
	var resultLines []string

	if len(s.results) == 0 {
		var stateMsg string
		var stateColor lipgloss.Color
		switch {
		case s.query == "":
			stateMsg = "Type to search"
			stateColor = theme.TextFaint
		case !s.CanSearch():
			stateMsg = "Keep typing…"
			stateColor = theme.TextFaint
		case s.loading:
			stateMsg = searchSpinnerFrames[s.spinnerFrame] + " Searching…"
			stateColor = theme.TextMuted
		default:
			stateMsg = "No results"
			stateColor = theme.TextMuted
		}
		// Center the message vertically within vr rows, all with Surface bg.
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
		for i := s.offset; i < len(s.results) && i < s.offset+vr; i++ {
			resultLines = append(resultLines, s.renderRow(s.results[i], i == s.cursor, innerW, sp))
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
	lines := make([]string, 0, 3+vr)
	lines = append(lines, inputRow, divider)
	lines = append(lines, resultLines...)
	lines = append(lines, footerStr)

	box := lipgloss.NewStyle().
		Width(boxW).
		Height(boxH).
		Background(surf).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))

	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, box)
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
	// FormatDate can return "Yesterday" (9 cols); replace it before truncating.
	const dateW = 6
	dateFmt := util.FormatDate(msg.Date)
	if dateFmt == "Yesterday" {
		dateFmt = "Yest."
	}
	dateRaw := util.TruncateText(util.SingleLine(dateFmt), dateW)
	dateCell := cell(dateW, metaFg, dateRaw)

	// Match-type indicator: 1-col character right before the date.
	// Rendered as a Width(2) cell (char + 1 padding space) to keep it tidy.
	// * = semantic (AI embedding match)  ~ = similar (cosine distance)
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

	// suffixW: sp(1) + ind(2) + sp(1) + date(6) = 10, constant across all rows
	suffixW := 1 + indCellW + 1 + dateW

	// From (fixed 18 cols)
	const fromW = 18
	fromCell := cell(fromW, rowFg, util.TruncateText(util.SingleLine(msg.FromString()), fromW))

	// Subject fills remaining space
	// Layout: unread(1) + sp(1) + from(18) + sp(1) + subject + suffixW = innerW
	subjectW := innerW - 1 - 1 - fromW - 1 - suffixW
	if subjectW < 1 {
		subjectW = 1
	}
	subjectCell := cell(subjectW, rowFg, util.TruncateText(util.SingleLine(msg.Subject), subjectW))

	// Subject gets a dimmer style when read and not selected
	if !selected && msg.IsRead() {
		subjectCell = lipgloss.NewStyle().
			Width(subjectW).
			Background(bg).
			Foreground(theme.TextMuted).
			Render(util.TruncateText(util.SingleLine(msg.Subject), subjectW))
	}

	spc := sp(bg)
	suffix := spc + indCell + spc + dateCell

	return unreadCell + spc + fromCell + spc + subjectCell + suffix
}
