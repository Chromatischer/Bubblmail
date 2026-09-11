package ui

import (
	"fmt"
	"github.com/bubblmail/bubblmail/config"

	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// statusBarRows is the fixed height of the status bar: the rule that closes the
// content area, plus one chrome row.
//
// It is fixed on purpose. The previous version grew a third row whenever a
// flash message arrived, which meant every flash re-ran the whole layout and
// shoved the mail list up by a line. Transient feedback must not move the
// thing the user is reading, so messages now share the single row with the
// hints and the hints give way instead.
// One row of faint type at the foot of the screen. No rule above it and no
// fill behind it — it reads as a footer because of what it says and how quiet
// it is, not because it has been boxed off.
const statusBarRows = 1

// StatusHitZone records the screen x-range and action key for one hint.
type StatusHitZone = components.HintZone

// StatusBar renders the bottom chrome: context-sensitive key hints on the
// left, transient status on the right.
type StatusBar struct {
	theme     *config.Theme
	width     int
	message   string
	msgKind   string // "info", "ok", "err"
	spinner   int
	loading   bool
	embedding embeddingStats
	moveHint  string // suggested destination folder for quick-move
	hitZones  []StatusHitZone
}

var spinnerFrames = []string{
	icons.Spinner1,
	icons.Spinner2,
	icons.Spinner3,
	icons.Spinner4,
	icons.Spinner5,
	icons.Spinner6,
}

// NewStatusBar creates a new status bar.
func NewStatusBar(theme *config.Theme) *StatusBar {
	return &StatusBar{theme: theme}
}

// SetWidth sets the status bar width.
func (sb *StatusBar) SetWidth(w int) { sb.width = w }

// SetMessage sets a temporary flash message.
func (sb *StatusBar) SetMessage(msg, kind string) {
	sb.message = msg
	sb.msgKind = kind
}

// ClearMessage clears the flash message.
func (sb *StatusBar) ClearMessage() {
	sb.message = ""
	sb.msgKind = ""
}

// SetLoading sets the spinner state.
func (sb *StatusBar) SetLoading(loading bool) { sb.loading = loading }

// SetEmbeddingStats updates the embedding queue state.
func (sb *StatusBar) SetEmbeddingStats(stats embeddingStats) { sb.embedding = stats }

// SetMoveHint sets the suggested destination folder for the quick-move hint.
// Pass "" to clear, "?" to indicate the folder picker will open, or a folder
// name to indicate the AI-chosen destination.
func (sb *StatusBar) SetMoveHint(hint string) { sb.moveHint = hint }

// MoveHint returns the current move hint value.
func (sb *StatusBar) MoveHint() string { return sb.moveHint }

// HitTest returns the action key for the hint clicked at (x, y), where y is
// relative to the top of the status bar. The bar is one row, so y must be 0.
// Returns "" if no actionable hint was hit.
func (sb *StatusBar) HitTest(x, y int) string {
	if y != 0 {
		return ""
	}
	for _, z := range sb.hitZones {
		if x >= z.X0 && x <= z.X1 {
			return z.Action
		}
	}
	return ""
}

// AdvanceSpinner advances the spinner frame.
func (sb *StatusBar) AdvanceSpinner() {
	sb.spinner = (sb.spinner + 1) % len(spinnerFrames)
}

// Height returns the number of terminal lines the status bar occupies. It is
// constant, so layout never has to render the bar to find out how tall it is.
func (sb *StatusBar) Height(string) int { return statusBarRows }

// hintsFor returns the key hints for a context. Priority marks what may be
// dropped as the terminal narrows: 0 never goes, higher numbers go first.
func (sb *StatusBar) hintsFor(context string) []components.Hint {
	switch context {
	case "composer":
		return []components.Hint{
			{Icon: icons.Send, Key: "ctrl+s", Desc: "send", Action: "ctrl+s"},
			{Icon: icons.ChevronRight, Key: "tab", Desc: "next field", Action: "tab", Priority: 2},
			{Icon: icons.ArrowUpDown, Key: "pgup/pgdn", Desc: "scroll", Action: "ctrl+d", Priority: 3},
			{Icon: icons.Close, Key: "esc", Desc: "cancel", Action: "esc"},
		}
	case "reader":
		return []components.Hint{
			{Icon: icons.ArrowUpDown, Key: "j/k", Desc: "scroll", Action: "j"},
			{Icon: icons.ArrowLeft, Key: "esc", Desc: "back", Action: "esc"},
			{Icon: icons.Reply, Key: "r", Desc: "reply", Action: "r", Priority: 1},
			{Icon: icons.ReplyAll, Key: "R", Desc: "reply all", Action: "R", Priority: 4},
			{Icon: icons.Forward, Key: "f", Desc: "forward", Action: "f", Priority: 4},
			{Icon: icons.Star, Key: "s", Desc: "star", Action: "s", Priority: 3},
			{Icon: icons.Trash, Key: "d", Desc: "delete", Action: "d", Priority: 3},
			{Icon: icons.ArrowLeftRight, Key: "←/→", Desc: "event", Action: "right", Priority: 5},
			{Icon: icons.Help, Key: "?", Desc: "help", Action: "?", Priority: 2},
		}
	case "search":
		return []components.Hint{
			{Icon: icons.Search, Key: "type", Desc: "to search"},
			{Icon: icons.ArrowUpDown, Key: "↑/↓", Desc: "results", Action: "down", Priority: 2},
			{Icon: icons.Check, Key: "↵", Desc: "open", Action: "enter"},
			{Icon: icons.Search, Key: "ctrl+f", Desc: "server search", Action: "ctrl+f", Priority: 3},
			{Icon: icons.Close, Key: "esc", Desc: "close", Action: "esc"},
		}
	case "move":
		return []components.Hint{
			{Icon: icons.Search, Key: "type", Desc: "to filter"},
			{Icon: icons.ArrowUpDown, Key: "j/k", Desc: "navigate", Action: "down", Priority: 2},
			{Icon: icons.FolderOpen, Key: "↵", Desc: "move here", Action: "enter"},
			{Icon: icons.Close, Key: "esc", Desc: "cancel", Action: "esc"},
		}
	case "new-folder":
		return []components.Hint{
			{Key: "type", Desc: "folder name"},
			{Icon: icons.Check, Key: "↵", Desc: "create", Action: "enter"},
			{Icon: icons.Close, Key: "esc", Desc: "cancel", Action: "esc"},
		}
	case "quick":
		applyDesc, applyIcon := "apply", icons.Check
		switch sb.moveHint {
		case "?":
			applyDesc, applyIcon = "pick folder", icons.FolderOpen
		case "":
			// still computing, or a non-move step — keep the generic label
		default:
			applyDesc, applyIcon = "→ "+sb.moveHint, icons.FolderOpen
		}
		return []components.Hint{
			{Icon: icons.ArrowLeftRight, Key: "←/→", Desc: "quick actions", Action: "right"},
			{Icon: applyIcon, Key: "↵", Desc: applyDesc, Action: "enter"},
			{Icon: icons.ArrowUpDown, Key: "j/k", Desc: "navigate", Action: "j", Priority: 2},
			{Icon: icons.Close, Key: "esc", Desc: "close", Action: "esc"},
		}
	case "sidebar":
		return []components.Hint{
			{Icon: icons.ArrowUpDown, Key: "j/k", Desc: "navigate", Action: "j"},
			{Icon: icons.FolderOpen, Key: "↵", Desc: "open folder", Action: "enter"},
			{Icon: icons.ChevronRight, Key: "space", Desc: "fold", Action: " ", Priority: 1},
			{Icon: icons.FolderNew, Key: "n", Desc: "new folder", Action: "n", Priority: 2},
			{Icon: icons.Close, Key: "esc", Desc: "cancel", Action: "esc"},
		}
	case "folder":
		return []components.Hint{
			{Icon: icons.ArrowUpDown, Key: "j/k", Desc: "navigate", Action: "j"},
			{Icon: icons.FolderOpen, Key: "↵", Desc: "open", Action: "enter"},
			{Icon: icons.ArrowLeft, Key: "esc", Desc: "back", Action: "esc"},
			{Icon: icons.FolderTree, Key: "b", Desc: "sidebar", Action: "b", Priority: 2},
			{Icon: icons.Help, Key: "?", Desc: "help", Action: "?", Priority: 1},
		}
	default: // inbox
		return []components.Hint{
			{Icon: icons.ArrowUpDown, Key: "j/k", Desc: "navigate", Action: "j"},
			{Icon: icons.MailOpen, Key: "↵", Desc: "open", Action: "enter"},
			{Icon: icons.ArrowLeftRight, Key: "←/→", Desc: "quick actions", Action: "right", Priority: 3},
			{Icon: icons.Compose, Key: "c", Desc: "compose", Action: "c", Priority: 2},
			{Icon: icons.Reply, Key: "r", Desc: "reply", Action: "r", Priority: 4},
			{Icon: icons.Search, Key: "/", Desc: "search", Action: "/", Priority: 2},
			{Icon: icons.FolderTree, Key: "b", Desc: "sidebar", Action: "b", Priority: 5},
			{Icon: icons.Help, Key: "?", Desc: "help", Action: "?", Priority: 1},
			{Icon: icons.Quit, Key: "qq", Desc: "quit", Action: "q", Priority: 6},
		}
	}
}

// statusCluster renders the right-hand transient segment: a flash message if
// one is pending, otherwise whatever background work is in flight.
func (sb *StatusBar) statusCluster(budget int) (string, int) {
	theme := sb.theme
	const bg = lipgloss.Color("")
	on := func(st lipgloss.Style) lipgloss.Style {
		if bg != "" {
			return st.Background(bg)
		}
		return st
	}

	if sb.message != "" {
		fg := theme.TextMuted
		glyph := icons.Info
		switch sb.msgKind {
		case "ok":
			fg, glyph = theme.Success, icons.Check
		case "err":
			fg, glyph = theme.Error, icons.Error
		}
		plain := glyph + " " + util.SingleLine(sb.message)
		plain = util.TruncateText(plain, budget)
		return on(lipgloss.NewStyle().Foreground(fg).Bold(true)).Render(plain),
			util.VisibleWidth(plain)
	}

	var plain string
	switch {
	case sb.embedding.InFlight > 0 || sb.embedding.Queued > 0:
		plain = fmt.Sprintf("%s indexing %d/%d",
			spinnerFrames[sb.spinner], sb.embedding.InFlight, sb.embedding.Queued)
	case sb.loading:
		plain = spinnerFrames[sb.spinner] + " loading…"
	default:
		return "", 0
	}
	plain = util.TruncateText(plain, budget)
	return on(lipgloss.NewStyle().Foreground(theme.TextMuted)).Render(plain),
		util.VisibleWidth(plain)
}

// View renders the status bar for the given context.
// context is "inbox", "reader", "composer", "search", "folder", "move",
// "new-folder", "quick" or "sidebar".
func (sb *StatusBar) View(context string) string {
	theme := sb.theme
	const bg = lipgloss.Color("")

	// One column of padding each side; the hint bar starts at column 1.
	const pad = 1
	inner := sb.width - pad*2
	if inner < 1 {
		inner = 1
	}

	// Transient status claims at most a third of the bar, so hints never
	// vanish entirely behind a long message.
	statusBudget := inner / 3
	if sb.message != "" {
		statusBudget = inner * 2 / 3
	}
	status, statusW := sb.statusCluster(statusBudget)

	gapBeforeStatus := 0
	if statusW > 0 {
		gapBeforeStatus = 2
	}
	hintBudget := inner - statusW - gapBeforeStatus
	if hintBudget < 0 {
		hintBudget = 0
	}

	hints, hintsW, zones := components.HintBar(
		theme, sb.hintsFor(context), hintBudget, pad, bg)
	sb.hitZones = zones

	fill := inner - hintsW - statusW
	if fill < 0 {
		fill = 0
	}

	row := components.Fill(pad, bg) +
		hints +
		components.Fill(fill, bg) +
		status +
		components.Fill(pad, bg)

	rowStyle := lipgloss.NewStyle().MaxWidth(sb.width).Width(sb.width)
	if bg != "" {
		rowStyle = rowStyle.Background(bg)
	}

	return rowStyle.Render(row)
}
