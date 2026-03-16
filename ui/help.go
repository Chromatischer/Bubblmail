package ui

import (
	"strings"

	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/charmbracelet/lipgloss"
)

// HelpOverlay renders a centered keyboard-shortcut reference box.
type HelpOverlay struct {
	styles *Styles
}

// NewHelpOverlay creates a new help overlay.
func NewHelpOverlay(styles *Styles) *HelpOverlay {
	return &HelpOverlay{styles: styles}
}

// View renders the help overlay centered in a container of (w, h).
func (h *HelpOverlay) View(w, height int) string {
	theme := h.styles.Theme

	keyStyle := lipgloss.NewStyle().
		Foreground(theme.Accent).
		Background(theme.Surface).
		Bold(true).
		Width(18)

	descStyle := lipgloss.NewStyle().
		Foreground(theme.Text).
		Background(theme.Surface).
		Width(26)

	emptyStyle := lipgloss.NewStyle().
		Background(theme.Surface).
		Width(44)

	titleStyle := lipgloss.NewStyle().
		Foreground(theme.Accent).
		Background(theme.Surface).
		Bold(true).
		Align(lipgloss.Center).
		Width(44)

	lines := AllHelpLines()
	var rows []string
	rows = append(rows, titleStyle.Render(icons.Help+" Keyboard Shortcuts"))
	rows = append(rows, emptyStyle.Render(""))

	for _, l := range lines {
		if l.Key == "" {
			rows = append(rows, emptyStyle.Render(""))
			continue
		}
		rows = append(rows, keyStyle.Render(l.Key)+descStyle.Render(l.Desc))
	}

	content := strings.Join(rows, "\n")

	return components.ModalBox(theme, content, 0, 0, w, height)
}
