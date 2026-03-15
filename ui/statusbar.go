package ui

import (
	"fmt"
	"strings"

	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/charmbracelet/lipgloss"
)

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

// AdvanceSpinner advances the spinner frame.
func (sb *StatusBar) AdvanceSpinner() {
	sb.spinner = (sb.spinner + 1) % len(spinnerFrames)
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
		hints = []hint{
			{icons.ArrowUpDown, "j/k", "scroll"},
			{icons.ArrowLeftRight, "←/→", "event action"},
			{icons.FloppyDisk, "enter", "copy event"},
			{icons.Reply, "r", "reply"},
			{icons.ReplyAll, "R", "reply all"},
			{icons.Forward, "f", "forward"},
			{icons.Star, "s", "star"},
			{icons.Trash, "d", "delete"},
			{icons.ArrowLeft, "esc/q/h", "back"},
			{icons.Help, "?", "help"},
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
			{icons.Compose, "c", "compose"},
			{icons.Reply, "r", "reply"},
			{icons.Star, "s", "star"},
			{icons.Read, "m", "mark read"},
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

	// Right side: flash message + status
	var statusParts []string
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
		statusParts = append(statusParts, lipgloss.NewStyle().
			Foreground(color).
			Render(sb.message))
	}
	if sb.loading {
		statusParts = append(statusParts, lipgloss.NewStyle().
			Foreground(theme.TextMuted).
			Render(fmt.Sprintf("%s Loading…", icons.Syncing)))
	}
	if sb.embedding.Queued > 0 || sb.embedding.InFlight > 0 {
		label := fmt.Sprintf("Embeddings %d in flight · %d queued", sb.embedding.InFlight, sb.embedding.Queued)
		statusParts = append(statusParts, lipgloss.NewStyle().
			Foreground(theme.TextMuted).
			Render(label))
	}
	var rightStr string
	if len(statusParts) > 0 {
		rightStr = strings.Join(statusParts, sepStyle.Render("  •  "))
	}

	rightW := lipgloss.Width(rightStr)
	hintsW := lipgloss.Width(hintsStr)
	gap := sb.width - hintsW - rightW - 2
	if gap < 1 {
		gap = 1
	}
	barContent := hintsStr + strings.Repeat(" ", gap) + rightStr

	divider := lipgloss.NewStyle().
		Foreground(theme.Border).
		Render(strings.Repeat("─", sb.width))

	row := lipgloss.NewStyle().
		Width(sb.width).
		Padding(0, 1).
		Render(barContent)

	return lipgloss.JoinVertical(lipgloss.Left, divider, row)
}
