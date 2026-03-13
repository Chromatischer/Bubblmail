package composer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/charmbracelet/lipgloss"
)

// filePicker is a fuzzy-matching file-path autocomplete popup that
// appears in the composer body area when the user types @.
type filePicker struct {
	active     bool
	query      string
	cwd        string
	matches    []string
	cursor     int
	theme      *config.Theme
	confirming bool   // waiting for compress-dir confirmation
	pendingDir string // dir path awaiting confirm
}

func newFilePicker(theme *config.Theme) *filePicker {
	cwd, _ := os.Getwd()
	return &filePicker{theme: theme, cwd: cwd}
}

func (fp *filePicker) activate() {
	fp.active = true
	fp.query = ""
	fp.cursor = 0
	fp.confirming = false
	fp.pendingDir = ""
	fp.refresh()
}

func (fp *filePicker) isActive() bool {
	return fp.active
}

// handleKey processes a key in the picker.
// Returns: path, isDir (was compressed dir), accepted, cancelled.
// accepted=true means a file/dir was chosen; caller should attach it.
func (fp *filePicker) handleKey(key string) (path string, isDir bool, accepted bool, cancelled bool) {
	if fp.confirming {
		switch key {
		case "y", "Y", "enter":
			fp.active = false
			fp.confirming = false
			return fp.pendingDir, true, true, false
		case "n", "N", "esc":
			fp.active = false
			fp.confirming = false
			return "", false, false, true
		}
		return "", false, false, false
	}

	switch key {
	case "esc":
		fp.active = false
		return "", false, false, true
	case "enter":
		if len(fp.matches) == 0 {
			return "", false, false, false
		}
		sel := fp.matches[fp.cursor]
		fullPath := filepath.Join(fp.cwd, sel)
		info, err := os.Stat(fullPath)
		if err == nil && info.IsDir() {
			fp.confirming = true
			fp.pendingDir = fullPath
			return "", false, false, false
		}
		fp.active = false
		return fullPath, false, true, false
	case "tab":
		// Tab-complete the current selection into the query.
		//   • Directory → append its path + "/" so the user drills in.
		//   • File      → accept immediately (same as enter).
		if len(fp.matches) == 0 {
			return "", false, false, false
		}
		sel := fp.matches[fp.cursor]
		fullPath := filepath.Join(fp.cwd, sel)
		info, err := os.Stat(fullPath)
		if err == nil && info.IsDir() {
			// Complete the query to "sel/" and stay in the picker.
			fp.query = sel + "/"
			fp.cursor = 0
			fp.refresh()
			return "", false, false, false
		}
		fp.active = false
		return fullPath, false, true, false
	case "up":
		if fp.cursor > 0 {
			fp.cursor--
		}
	case "down":
		if fp.cursor < len(fp.matches)-1 {
			fp.cursor++
		}
	case "backspace", "ctrl+h":
		if len(fp.query) > 0 {
			runes := []rune(fp.query)
			fp.query = string(runes[:len(runes)-1])
			fp.cursor = 0
			fp.refresh()
		}
	default:
		if len(key) == 1 && key[0] >= 32 {
			fp.query += key
			fp.cursor = 0
			fp.refresh()
		}
	}
	return "", false, false, false
}

func (fp *filePicker) refresh() {
	fp.matches = fp.findMatches()
	if fp.cursor >= len(fp.matches) {
		fp.cursor = len(fp.matches) - 1
	}
	if fp.cursor < 0 {
		fp.cursor = 0
	}
}

func (fp *filePicker) findMatches() []string {
	// Split query into the explicit directory prefix (everything up to and
	// including the last "/") and the fuzzy filter on what follows.
	q := fp.query
	dirPart := ""
	filterPart := strings.ToLower(q)
	if idx := strings.LastIndex(q, "/"); idx >= 0 {
		dirPart = q[:idx+1]
		filterPart = strings.ToLower(q[idx+1:])
	}

	baseDir := filepath.Join(fp.cwd, dirPart)
	if _, err := os.Stat(baseDir); err != nil {
		return nil
	}

	var results []string
	fp.collectEntries(baseDir, dirPart, "", filterPart, 2, &results)
	return results
}

// collectEntries reads dir and appends matching paths to results.
//
//   - cwdPrefix  – the part of the query the user already typed ("src/" etc.)
//   - relFromBase – path walked so far below baseDir ("" at top level)
//   - filter      – fuzzy filter applied to the name portion after dirPart
//   - depth       – remaining recursion levels (2 = two levels below baseDir)
//
// Hidden dirs are never recursed into automatically; they appear in the listing
// at the explicit level so the user can select them and type further.
func (fp *filePicker) collectEntries(dir, cwdPrefix, relFromBase, filter string, depth int, results *[]string) {
	if len(*results) >= 60 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if len(*results) >= 60 {
			return
		}
		name := entry.Name()
		isHidden := strings.HasPrefix(name, ".")

		// At the explicit (top) level show everything, including hidden
		// entries so the user can see and select them.  In recursed sub-
		// directories skip hidden entries entirely.
		if isHidden && relFromBase != "" {
			continue
		}

		relPath := relFromBase + name
		displayKey := cwdPrefix + relPath

		if filter == "" || fuzzyMatch(strings.ToLower(relPath), filter) || fuzzyMatch(strings.ToLower(name), filter) {
			*results = append(*results, displayKey)
		}

		// Recurse into non-hidden subdirs only.
		if entry.IsDir() && depth > 0 && !isHidden {
			fp.collectEntries(filepath.Join(dir, name), cwdPrefix, relPath+"/", filter, depth-1, results)
		}
	}
}

// fuzzyMatch returns true if every rune of pattern appears in s in order.
func fuzzyMatch(s, pattern string) bool {
	pi := 0
	for _, c := range s {
		if pi < len(pattern) && c == rune(pattern[pi]) {
			pi++
		}
	}
	return pi == len(pattern)
}

const pickerMaxVisible = 7

// view renders the file picker occupying bodyW columns and up to height rows.
// Renders nothing when inactive.
func (fp *filePicker) view(bodyW, height int) string {
	if !fp.active {
		return ""
	}
	theme := fp.theme

	// ── Compress-confirm state ────────────────────────────────────────────
	if fp.confirming {
		name := filepath.Base(fp.pendingDir)

		// Row 1: icon + question, SurfaceAlt background.
		q1 := lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.SurfaceAlt).Render(" " + icons.Package + " ")
		q2 := lipgloss.NewStyle().Foreground(theme.Text).Background(theme.SurfaceAlt).Render("Compress ")
		q3 := lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.SurfaceAlt).Bold(true).Render(name)
		q4 := lipgloss.NewStyle().Foreground(theme.Text).Background(theme.SurfaceAlt).Render(" as zip?")
		questionLine := q1 + q2 + q3 + q4
		questionPlainW := 3 + len([]rune("Compress "+name+" as zip?"))
		if pad := bodyW - questionPlainW; pad > 0 {
			questionLine += lipgloss.NewStyle().Background(theme.SurfaceAlt).Render(strings.Repeat(" ", pad))
		}

		// Row 2: y / n options, Surface background.
		yKey := lipgloss.NewStyle().Foreground(theme.Success).Background(theme.Surface).Bold(true).Render("[y]")
		yDesc := lipgloss.NewStyle().Foreground(theme.TextMuted).Background(theme.Surface).Render(" zip  ")
		nKey := lipgloss.NewStyle().Foreground(theme.Error).Background(theme.Surface).Bold(true).Render("[n]")
		nDesc := lipgloss.NewStyle().Foreground(theme.TextMuted).Background(theme.Surface).Render(" cancel")
		const optPlainW = 2 + 3 + 6 + 3 + 7 // "  [y] zip  [n] cancel"
		optPad := ""
		if pad := bodyW - optPlainW; pad > 0 {
			optPad = strings.Repeat(" ", pad)
		}
		optLine := lipgloss.NewStyle().Background(theme.Surface).Render("  ") +
			yKey + yDesc + nKey + nDesc +
			lipgloss.NewStyle().Background(theme.Surface).Render(optPad)

		return questionLine + "\n" + optLine
	}

	// ── Query line ────────────────────────────────────────────────────────
	// Split query into already-typed dir prefix (dimmed) and filter term (bright).
	var queryDir, queryFilter string
	if idx := strings.LastIndex(fp.query, "/"); idx >= 0 {
		queryDir = fp.query[:idx+1]
		queryFilter = fp.query[idx+1:]
	} else {
		queryFilter = fp.query
	}

	iconSt := lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.SurfaceAlt)
	dirSt := lipgloss.NewStyle().Foreground(theme.TextFaint).Background(theme.SurfaceAlt)
	filterSt := lipgloss.NewStyle().Foreground(theme.Text).Background(theme.SurfaceAlt)
	cursorSt := lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.SurfaceAlt).Bold(true)

	queryRendered := iconSt.Render(" "+icons.Search+" ") +
		dirSt.Render(queryDir) +
		filterSt.Render(queryFilter) +
		cursorSt.Render("▌")

	// Pad to full width (measure with plain runes, not ANSI).
	queryPlainW := 3 + len([]rune(fp.query)) + 1 // icon cell + query + cursor
	queryPad := bodyW - queryPlainW
	if queryPad > 0 {
		queryRendered += lipgloss.NewStyle().Background(theme.SurfaceAlt).Render(strings.Repeat(" ", queryPad))
	}

	var lines []string
	lines = append(lines, queryRendered)

	if len(fp.matches) == 0 {
		empty := lipgloss.NewStyle().Background(theme.Surface).Width(bodyW).
			Render("  " + lipgloss.NewStyle().Foreground(theme.TextFaint).Render("no matches"))
		lines = append(lines, empty)
		return strings.Join(lines, "\n")
	}

	// ── Match list ────────────────────────────────────────────────────────
	maxVis := pickerMaxVisible
	if height-1 < maxVis {
		maxVis = height - 1
	}
	if maxVis < 1 {
		maxVis = 1
	}

	// Scroll window centred on cursor.
	start := fp.cursor - maxVis/2
	if start < 0 {
		start = 0
	}
	if start+maxVis > len(fp.matches) {
		start = len(fp.matches) - maxVis
	}
	if start < 0 {
		start = 0
	}
	end := start + maxVis
	if end > len(fp.matches) {
		end = len(fp.matches)
	}

	// Scroll indicators (plain-width strings rendered in-line at end of row).
	aboveCount := start
	belowCount := len(fp.matches) - end

	for i := start; i < end; i++ {
		match := fp.matches[i]
		fullPath := filepath.Join(fp.cwd, match)
		isDir := false
		if info, err := os.Stat(fullPath); err == nil && info.IsDir() {
			isDir = true
		}

		// Split display into directory part (dimmed) + basename (bright).
		dir, base := filepath.Split(match)

		selected := i == fp.cursor

		// Choose background and icon.
		var bg lipgloss.Color
		if selected {
			bg = theme.SurfaceAlt
		} else {
			bg = theme.Surface
		}

		baseSt := lipgloss.NewStyle().Background(bg)
		dimSt := lipgloss.NewStyle().Foreground(theme.TextFaint).Background(bg)

		var fileIcon string
		var iconColor lipgloss.Color
		if isDir {
			if selected {
				fileIcon = icons.FolderOpen + " "
			} else {
				fileIcon = icons.Folder + " "
			}
			iconColor = theme.Starred
		} else {
			fileIcon = icons.Attachment + " "
			iconColor = theme.TextFaint
		}

		var prefix string
		if selected {
			prefix = lipgloss.NewStyle().Foreground(theme.Accent).Background(bg).Bold(true).Render(" ▸ ")
			baseSt = baseSt.Foreground(theme.Accent).Bold(true)
		} else {
			prefix = lipgloss.NewStyle().Background(bg).Render("   ")
			if isDir {
				baseSt = baseSt.Foreground(theme.Text)
			} else {
				baseSt = baseSt.Foreground(theme.TextMuted)
			}
		}

		iconRendered := lipgloss.NewStyle().Foreground(iconColor).Background(bg).Render(fileIcon)
		dirRendered := dimSt.Render(dir)
		baseRendered := baseSt.Render(base)

		// Measure plain width to compute padding.
		plainW := 3 + 2 + len([]rune(dir)) + len([]rune(base)) // prefix(3) + icon(2) + dir + base
		padW := bodyW - plainW

		// Attach scroll hint to first/last visible row.
		var scrollHint string
		scrollHintPlain := ""
		if i == start && aboveCount > 0 {
			scrollHintPlain = icons.ChevronUp + " " + itoa(aboveCount)
			scrollHint = lipgloss.NewStyle().Foreground(theme.TextFaint).Background(bg).
				Render(scrollHintPlain)
			padW -= len([]rune(scrollHintPlain))
		} else if i == end-1 && belowCount > 0 {
			scrollHintPlain = icons.ChevronDown + " " + itoa(belowCount)
			scrollHint = lipgloss.NewStyle().Foreground(theme.TextFaint).Background(bg).
				Render(scrollHintPlain)
			padW -= len([]rune(scrollHintPlain))
		}

		pad := ""
		if padW > 0 {
			pad = strings.Repeat(" ", padW)
		}

		row := prefix + iconRendered + dirRendered + baseRendered +
			lipgloss.NewStyle().Background(bg).Render(pad) + scrollHint
		lines = append(lines, row)
	}

	return strings.Join(lines, "\n")
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
