package ui

import (
	"fmt"
	"strings"

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
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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

// AdvanceSpinner advances the spinner frame.
func (sb *StatusBar) AdvanceSpinner() {
	sb.spinner = (sb.spinner + 1) % len(spinnerFrames)
}

// View renders the status bar for the given context.
// context is "inbox", "reader", "composer", "search", "folder".
func (sb *StatusBar) View(context string) string {
	theme := sb.styles.Theme

	keyStyle := lipgloss.NewStyle().
		Foreground(theme.Accent).
		Background(theme.Surface).
		Bold(true)
	descStyle := lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Background(theme.Surface)
	sepStyle := lipgloss.NewStyle().
		Foreground(theme.TextFaint).
		Background(theme.Surface)

	type hint struct{ key, desc string }

	var hints []hint
	switch context {
	case "composer":
		hints = []hint{
			{"ctrl+enter", "send"},
			{"tab", "next field"},
			{"esc", "cancel"},
		}
	case "reader":
		hints = []hint{
			{"j/k", "scroll"},
			{"r", "reply"},
			{"R", "reply all"},
			{"f", "forward"},
			{"s", "star"},
			{"d", "delete"},
			{"esc/h", "back"},
			{"?", "help"},
			{"q", "quit"},
		}
	case "search":
		hints = []hint{
			{"type", "search"},
			{"enter", "select"},
			{"ctrl+f", "server search"},
			{"esc", "close"},
		}
	case "folder":
		hints = []hint{
			{"enter", "open"},
			{"esc", "back"},
			{"b", "sidebar"},
			{"?", "help"},
			{"q", "quit"},
		}
	default: // inbox
		hints = []hint{
			{"j/k", "navigate"},
			{"enter", "open"},
			{"c", "compose"},
			{"r", "reply"},
			{"s", "star"},
			{"m", "mark read"},
			{"/", "search"},
			{"b", "sidebar"},
			{"?", "help"},
			{"q", "quit"},
		}
	}

	var parts []string
	for _, h := range hints {
		parts = append(parts, fmt.Sprintf("%s %s",
			keyStyle.Render(h.key),
			descStyle.Render(h.desc),
		))
	}

	hintsStr := strings.Join(parts, sepStyle.Render("  "))

	// Right side: flash message or spinner
	var rightStr string
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
		rightStr = lipgloss.NewStyle().
			Foreground(color).
			Background(theme.Surface).
			Render(sb.message)
	} else if sb.loading {
		rightStr = lipgloss.NewStyle().
			Foreground(theme.TextMuted).
			Background(theme.Surface).
			Render(spinnerFrames[sb.spinner] + " Loading…")
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
		Background(theme.Surface).
		Width(sb.width).
		Padding(0, 1).
		Render(barContent)

	return lipgloss.JoinVertical(lipgloss.Left, divider, row)
}
