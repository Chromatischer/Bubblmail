package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Header renders the 2-row top bar: app name, account+folder, and sync indicator.
type Header struct {
	styles        *Styles
	width         int
	activeAccount string
	activeFolder  string
	syncState     string // "", "syncing", "synced", "error"
}

// NewHeader creates a new header component.
func NewHeader(styles *Styles) *Header {
	return &Header{styles: styles}
}

// SetWidth sets the header width.
func (h *Header) SetWidth(w int) {
	h.width = w
}

// SetAccount updates the active account name.
func (h *Header) SetAccount(account string) {
	h.activeAccount = account
}

// SetFolder updates the active folder name.
func (h *Header) SetFolder(folder string) {
	h.activeFolder = folder
}

// SetSyncState sets the sync indicator state.
func (h *Header) SetSyncState(state string) {
	h.syncState = state
}

// View renders the header.
func (h *Header) View() string {
	theme := h.styles.Theme

	// Row 1: app name + sync indicator
	appName := lipgloss.NewStyle().
		Foreground(theme.Accent).
		Bold(true).
		Render("bubblmail")

	var syncStr string
	switch h.syncState {
	case "syncing":
		syncStr = lipgloss.NewStyle().
			Foreground(theme.Warning).
			Render("⟳ syncing")
	case "synced":
		syncStr = lipgloss.NewStyle().
			Foreground(theme.Success).
			Render("● synced")
	case "error":
		syncStr = lipgloss.NewStyle().
			Foreground(theme.Error).
			Render("✗ sync error")
	}

	row1Width := h.width - lipgloss.Width(appName) - lipgloss.Width(syncStr) - 2
	if row1Width < 0 {
		row1Width = 0
	}
	gap1 := strings.Repeat(" ", row1Width)

	row1 := lipgloss.NewStyle().
		Width(h.width).
		Render(appName + gap1 + syncStr)

	// Row 2: account + folder breadcrumb
	var breadcrumb string
	if h.activeAccount != "" && h.activeFolder != "" {
		acctStyle := lipgloss.NewStyle().
			Foreground(theme.TextMuted)
		folderStyle := lipgloss.NewStyle().
			Foreground(theme.Text).
			Bold(true)
		breadcrumb = acctStyle.Render(h.activeAccount) + " › " + folderStyle.Render(h.activeFolder)
	} else if h.activeAccount != "" {
		breadcrumb = lipgloss.NewStyle().
			Foreground(theme.TextMuted).
			Render(h.activeAccount)
	}

	row2 := lipgloss.NewStyle().
		Foreground(theme.Text).
		Width(h.width).
		Padding(0, 1).
		Render(fmt.Sprintf(" %s", breadcrumb))

	divider := lipgloss.NewStyle().
		Foreground(theme.Border).
		Render(strings.Repeat("─", h.width))

	return lipgloss.JoinVertical(lipgloss.Left, row1, row2, divider)
}
