package ui

import (
	"fmt"
	"strings"

	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/charmbracelet/lipgloss"
)

// StatusHitZone records the screen x-range and action key for one hint in the hints row.
type StatusHitZone struct {
	X0, X1    int
	ActionKey string
}

// StatusBar renders the bottom hint bar with context-sensitive keys and flash messages.
type StatusBar struct {
	styles    *Styles
	width     int
	message   string
	msgKind   string // "info", "ok", "err"
	spinner   int
	loading   bool
	embedding embeddingStats
	moveHint  string // suggested destination folder for quick-move
	hitZones  []StatusHitZone

	// Reader sub-state, used to build context-aware hints for the reader view.
	readerHasAttach    bool
	readerAttachActive bool
	readerHasEvent     bool
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
func NewStatusBar(styles *Styles) *StatusBar {
	return &StatusBar{styles: styles}
}

// SetWidth sets the status bar width.
func (sb *StatusBar) SetWidth(w int) {
	sb.width = w
}

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
func (sb *StatusBar) SetLoading(loading bool) {
	sb.loading = loading
}

// SetEmbeddingStats updates the embedding queue state.
func (sb *StatusBar) SetEmbeddingStats(stats embeddingStats) {
	sb.embedding = stats
}

// SetMoveHint sets the suggested destination folder for the quick-move hint.
// Pass "" to clear, "?" to indicate the folder picker will open, or a folder
// name to indicate the AI-chosen destination.
func (sb *StatusBar) SetMoveHint(hint string) {
	sb.moveHint = hint
}

// MoveHint returns the current move hint value.
func (sb *StatusBar) MoveHint() string { return sb.moveHint }

// SetReaderState updates reader-specific flags so the "reader" context can show
// hints that match what the user can actually do right now.
func (sb *StatusBar) SetReaderState(hasAttach, attachActive, hasEvent bool) {
	sb.readerHasAttach = hasAttach
	sb.readerAttachActive = attachActive
	sb.readerHasEvent = hasEvent
}

// HitTest returns the action key for the hint clicked at (x, y), where y is
// relative to the top of the status bar (0 = divider, 1 = hints row).
// Returns "" if no actionable hint was hit.
func (sb *StatusBar) HitTest(x, y int) string {
	if y != 1 {
		return ""
	}
	for _, z := range sb.hitZones {
		if x >= z.X0 && x <= z.X1 {
			return z.ActionKey
		}
	}
	return ""
}

// hintActionKey maps a display key string (as shown in the status bar) to the
// tea.KeyMsg string expected by handleKey. Returns "" for informational hints.
func hintActionKey(displayKey string) string {
	switch displayKey {
	case "j/k":
		return "j"
	case "←/→":
		return "right"
	case "l/r":
		return "l"
	case "esc/q/h":
		return "esc"
	case `esc/\`:
		return "esc"
	case "esc/q":
		return "esc"
	case "pgup/pgdn":
		return "ctrl+d"
	case "type":
		return "" // informational only
	default:
		return displayKey
	}
}

// AdvanceSpinner advances the spinner frame.
func (sb *StatusBar) AdvanceSpinner() {
	sb.spinner = (sb.spinner + 1) % len(spinnerFrames)
}

// Height returns the number of terminal lines the status bar will occupy for
// the given context. Call this before layout so contentH is correct.
func (sb *StatusBar) Height(context string) int {
	rendered := sb.View(context)
	return strings.Count(rendered, "\n") + 1
}

// View renders the status bar for the given context.
// context is "inbox", "reader", "composer", "search", "folder".
func (sb *StatusBar) View(context string) string {
	theme := sb.styles.Theme

	iconStyle := lipgloss.NewStyle().
		Foreground(theme.Accent)
	keyStyle := lipgloss.NewStyle().
		Foreground(theme.TextFaint)
	descStyle := lipgloss.NewStyle().
		Foreground(theme.TextMuted)
	sepStyle := lipgloss.NewStyle().
		Foreground(theme.TextFaint)

	type hint struct{ icon, key, desc string }

	var hints []hint
	switch context {
	case "composer":
		hints = []hint{
			{icons.Send, "ctrl+enter", "send"},
			{icons.ChevronRight, "tab", "next field"},
			{icons.ArrowUpDown, "pgup/pgdn", "scroll"},
			{icons.Close, "esc", "cancel"},
		}
	case "reader":
		if sb.readerAttachActive {
			// Inside the attachment section: show how to drive it.
			hints = []hint{
				{icons.Attachment, "tab", "cycle"},
				{icons.ArrowLeftRight, "←/→", "action"},
				{icons.Check, "enter", "run"},
				{icons.ArrowLeft, "esc", "back"},
				{icons.Help, "?", "help"},
			}
		} else {
			hints = []hint{{icons.ArrowUpDown, "j/k", "scroll"}}
			// Event hints only when the mail actually has a suggested event.
			if sb.readerHasEvent {
				hints = append(hints,
					hint{icons.ArrowLeftRight, "←/→", "event action"},
					hint{icons.FloppyDisk, "enter", "copy event"},
				)
			}
			hints = append(hints,
				hint{icons.Reply, "r", "reply"},
				hint{icons.Forward, "f", "forward"},
				hint{icons.FolderOpen, "v", "move"},
				hint{icons.Trash, "d", "delete"},
			)
			// Attachment entry only when the mail has attachments.
			if sb.readerHasAttach {
				hints = append(hints, hint{icons.Attachment, "a", "attachments"})
			}
			hints = append(hints,
				hint{icons.ArrowLeft, "esc/q/h", "back"},
				hint{icons.Help, "?", "help"},
			)
		}
	case "search":
		hints = []hint{
			{icons.Search, "type", "search"},
			{icons.Check, "enter", "select"},
			{icons.Search, "ctrl+f", "server search"},
			{icons.Close, "esc", "close"},
		}
	case "move":
		hints = []hint{
			{icons.Search, "type", "filter"},
			{icons.ArrowUpDown, "j/k", "navigate"},
			{icons.FolderOpen, "enter", "move here"},
			{icons.Close, "esc", "cancel"},
		}
	case "new-folder":
		hints = []hint{
			{"", "type", "folder name"},
			{icons.Check, "enter", "create"},
			{icons.Close, "esc", "cancel"},
		}
	case "palette":
		hints = []hint{
			{icons.Search, "type", "filter"},
			{icons.ArrowUpDown, "↑/↓", "navigate"},
			{icons.Check, "enter", "run"},
			{icons.Close, "esc", "close"},
		}
	case "quick":
		applyDesc := "apply"
		applyIcon := icons.Check
		switch sb.moveHint {
		case "?":
			applyDesc = "pick folder"
			applyIcon = icons.FolderOpen
		case "":
			// still computing or non-move step — generic
		default:
			applyDesc = "→ " + sb.moveHint
			applyIcon = icons.FolderOpen
		}
		hints = []hint{
			{icons.ArrowLeftRight, "←/→", "quick actions"},
			{applyIcon, "enter", applyDesc},
			{icons.ArrowUpDown, "j/k", "navigate"},
			{icons.Close, "esc", "close"},
		}
	case "sidebar":
		hints = []hint{
			{icons.ArrowUpDown, "j/k", "navigate"},
			{icons.FolderOpen, "enter", "open folder"},
			{icons.FolderNew, "n", "new folder"},
			{icons.Close, "esc/\\", "cancel"},
		}
	case "folder":
		hints = []hint{
			{icons.FolderOpen, "enter", "open"},
			{icons.ArrowLeft, "esc/q", "back"},
			{icons.FolderTree, "b", "sidebar"},
			{icons.Help, "?", "help"},
		}
	default: // inbox
		hints = []hint{
			{icons.ArrowUpDown, "j/k", "navigate"},
			{icons.MailOpen, "enter", "open"},
			{icons.ArrowLeftRight, "l/r", "quick actions"},
			{icons.Compose, "c", "compose"},
			{icons.Reply, "r", "reply"},
			{icons.Search, "/", "search"},
			{icons.FolderTree, "b", "sidebar"},
			{icons.Help, "?", "help"},
			{icons.Quit, "q", "quit (double)"},
		}
	}

	var parts []string
	for _, h := range hints {
		if h.icon == "" {
			parts = append(parts, fmt.Sprintf("%s %s",
				descStyle.Render(h.desc),
				keyStyle.Render("("+h.key+")"),
			))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s %s",
			iconStyle.Render(h.icon),
			descStyle.Render(h.desc),
			keyStyle.Render("("+h.key+")"),
		))
	}

	hintsStr := strings.Join(parts, sepStyle.Render("  "))

	// Build mouse hit zones — one per hint, using visual widths.
	// The hints row has Padding(0,1), so hints start at x=1. Hints are
	// separated by a 2-column gap ("  ").
	var newHitZones []StatusHitZone
	curX := 1
	for i, part := range parts {
		if i > 0 {
			curX += 2 // "  " separator
		}
		w := lipgloss.Width(part)
		if ak := hintActionKey(hints[i].key); ak != "" {
			newHitZones = append(newHitZones, StatusHitZone{X0: curX, X1: curX + w - 1, ActionKey: ak})
		}
		curX += w
	}
	sb.hitZones = newHitZones

	// Inline right side: loading + embedding stats (short, predictable width).
	var inlineParts []string
	if sb.loading {
		inlineParts = append(inlineParts, lipgloss.NewStyle().
			Foreground(theme.TextMuted).
			Render(fmt.Sprintf("%s Loading…", icons.Syncing)))
	}
	if sb.embedding.Queued > 0 || sb.embedding.InFlight > 0 {
		label := fmt.Sprintf("Embeddings %d in flight · %d queued", sb.embedding.InFlight, sb.embedding.Queued)
		inlineParts = append(inlineParts, lipgloss.NewStyle().
			Foreground(theme.TextMuted).
			Render(label))
	}
	var inlineRight string
	if len(inlineParts) > 0 {
		inlineRight = strings.Join(inlineParts, sepStyle.Render("  •  "))
	}

	hintsW := lipgloss.Width(hintsStr)
	inlineW := lipgloss.Width(inlineRight)
	contentW := sb.width - 2 // account for Padding(0,1)
	gap := contentW - hintsW - inlineW
	if gap < 1 {
		gap = 1
	}
	hintsRow := lipgloss.NewStyle().
		Width(sb.width).
		Padding(0, 1).
		Render(hintsStr + strings.Repeat(" ", gap) + inlineRight)

	divider := lipgloss.NewStyle().
		Foreground(theme.Border).
		Render(strings.Repeat("─", sb.width))

	// Flash message on its own line so it is always fully readable.
	if sb.message != "" {
		var color lipgloss.Color
		switch sb.msgKind {
		case "ok":
			color = theme.Success
		case "err":
			color = theme.Error
		default:
			color = theme.TextMuted
		}
		msgRow := lipgloss.NewStyle().
			Width(sb.width).
			Padding(0, 1).
			Foreground(color).
			Render(sb.message)
		return lipgloss.JoinVertical(lipgloss.Left, divider, hintsRow, msgRow)
	}

	return lipgloss.JoinVertical(lipgloss.Left, divider, hintsRow)
}
