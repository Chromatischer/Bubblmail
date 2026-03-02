package ui

import (
	"strings"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// SearchOverlay is a floating search box with FTS5 results list.
type SearchOverlay struct {
	styles  *Styles
	width   int
	height  int
	active  bool
	query   string
	results []*SearchResult
	cursor  int
	offset  int
	loading bool

	// debounce state
	pendingQuery string
	debounceID   int
}

// SearchResult holds a search match and labels.
type SearchResult struct {
	Message  *data.Message
	Score    float32
	Semantic bool
	Similar  bool
}

// NewSearchOverlay creates a new search overlay.
func NewSearchOverlay(styles *Styles) *SearchOverlay {
	return &SearchOverlay{styles: styles}
}

// SetSize sets the overlay dimensions.
func (s *SearchOverlay) SetSize(w, h int) {
	s.width = w
	s.height = h
}

// Open opens the search overlay.
func (s *SearchOverlay) Open() {
	s.active = true
	s.query = ""
	s.results = nil
	s.cursor = 0
	s.offset = 0
	s.loading = false
}

// Close closes the overlay.
func (s *SearchOverlay) Close() {
	s.active = false
	s.query = ""
	s.results = nil
	s.loading = false
}

// IsActive returns true if the overlay is open.
func (s *SearchOverlay) IsActive() bool {
	return s.active
}

// Query returns the current search query.
func (s *SearchOverlay) Query() string {
	return s.query
}

// SetResults updates the search results.
func (s *SearchOverlay) SetResults(msgs []*data.Message) {
	results := make([]*SearchResult, 0, len(msgs))
	for _, msg := range msgs {
		results = append(results, &SearchResult{Message: msg})
	}
	s.results = results
	s.cursor = 0
	s.offset = 0
}

// SetSearchResults updates streaming search results.
func (s *SearchOverlay) SetSearchResults(results []*SearchResult, loading bool) {
	s.results = results
	s.loading = loading
	if s.cursor >= len(s.results) {
		s.cursor = len(s.results) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

// SelectedMessage returns the currently selected message, or nil.
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
			visibleRows := s.height - 6
			if visibleRows < 1 {
				visibleRows = 1
			}
			if s.cursor >= s.offset+visibleRows {
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
		// Printable characters
		if len(key) == 1 && key[0] >= 32 {
			s.query += key
			s.cursor = 0
			s.offset = 0
			return true, false, false
		}
	}
	return false, false, false
}

// View renders the search overlay.
func (s *SearchOverlay) View() string {
	theme := s.styles.Theme

	boxWidth := s.width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}
	boxHeight := s.height - 4
	if boxHeight < 10 {
		boxHeight = 10
	}

	// Input line
	inputStyle := lipgloss.NewStyle().
		Foreground(theme.Text).
		Background(theme.SurfaceAlt).
		Width(boxWidth-4).
		Padding(0, 1)

	queryDisplay := s.query
	if queryDisplay == "" {
		queryDisplay = lipgloss.NewStyle().
			Foreground(theme.TextFaint).
			Background(theme.SurfaceAlt).
			Render("Type to search…")
	} else {
		queryDisplay = inputStyle.Render(s.query + "▌")
	}

	inputLine := "  " + queryDisplay

	// Results
	visibleRows := boxHeight - 4
	if visibleRows < 1 {
		visibleRows = 1
	}

	var resultLines []string
	if len(s.results) == 0 && s.query != "" {
		emptyMsg := lipgloss.NewStyle().
			Foreground(theme.TextFaint).
			Background(theme.Surface).
			Render(func() string {
				if s.loading {
					return "  Searching…"
				}
				return "  No results"
			}())
		resultLines = append(resultLines, emptyMsg)
	}

	for i := s.offset; i < len(s.results) && i < s.offset+visibleRows; i++ {
		res := s.results[i]
		msg := res.Message
		isSelected := i == s.cursor

		from := util.TruncateText(msg.FromString(), 20)
		subject := util.TruncateText(msg.Subject, boxWidth-30)
		date := util.FormatDate(msg.Date)

		var lineStyle, metaStyle lipgloss.Style
		if isSelected {
			lineStyle = lipgloss.NewStyle().
				Background(theme.Selected).
				Foreground(theme.Background).
				Bold(true)
			metaStyle = lineStyle
		} else {
			lineStyle = lipgloss.NewStyle().
				Foreground(theme.Text).
				Background(theme.Surface)
			metaStyle = lipgloss.NewStyle().
				Foreground(theme.TextMuted).
				Background(theme.Surface)
		}

		unread := " "
		if !msg.IsRead() {
			unread = lipgloss.NewStyle().
				Foreground(theme.Unread).
				Background(func() lipgloss.Color {
					if isSelected {
						return theme.Selected
					}
					return theme.Surface
				}()).
				Render("●")
		}

		badge := ""
		if res.Semantic || res.Similar {
			var tags []string
			if res.Semantic {
				tags = append(tags, "SEM")
			}
			if res.Similar {
				tags = append(tags, "SIM")
			}
			badgeStyle := lipgloss.NewStyle().
				Foreground(theme.Background).
				Background(theme.Accent).
				Padding(0, 1)
			badge = badgeStyle.Render(strings.Join(tags, " "))
		}

		badgeW := lipgloss.Width(badge)
		fromW := 20
		subjectW := boxWidth - fromW - 12 - badgeW
		if subjectW < 10 {
			subjectW = 10
		}
		from = util.PadRight(from, fromW)
		subject = util.TruncateText(msg.Subject, subjectW)
		subject = util.PadRight(subject, subjectW)

		line := " " + unread + " " + lineStyle.Render(from+" "+subject)
		if badge != "" {
			line += " " + badge
		}
		line += " " + metaStyle.Render(date)
		resultLines = append(resultLines, line)
	}

	// Pad to boxHeight
	bgLine := lipgloss.NewStyle().Background(theme.Surface).Width(boxWidth).Render("")
	for len(resultLines) < visibleRows {
		resultLines = append(resultLines, bgLine)
	}

	countLine := ""
	if len(s.results) > 0 {
		countLabel := util.PluralCount(len(s.results), "result", "results")
		if s.loading {
			countLabel += " · streaming"
		}
		countLine = lipgloss.NewStyle().
			Foreground(theme.TextFaint).
			Background(theme.Surface).
			Render("  " + strings.Repeat("─", 10) + " " + countLabel)
	}

	content := strings.Join(append([]string{inputLine, countLine}, resultLines...), "\n")

	box := lipgloss.NewStyle().
		Width(boxWidth).
		Height(boxHeight).
		Background(theme.Surface).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		Padding(1, 0).
		Render(content)

	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, box)
}
