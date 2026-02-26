package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/bubblmail/bubblmail/config"
)

// Styles holds all shared lipgloss styles.
type Styles struct {
	Theme *config.Theme

	// Layout panels
	Header    lipgloss.Style
	Sidebar   lipgloss.Style
	Content   lipgloss.Style
	StatusBar lipgloss.Style

	// Thread list
	ListItem         lipgloss.Style
	ListItemSelected lipgloss.Style
	ListItemUnread   lipgloss.Style
	ListMeta         lipgloss.Style
	ListMetaSelected lipgloss.Style
	ListSecondary    lipgloss.Style
	Tag              lipgloss.Style

	// Modal / form
	Modal      lipgloss.Style
	ModalTitle lipgloss.Style
	FormLabel  lipgloss.Style
	FormInput  lipgloss.Style
	FormButton lipgloss.Style

	// Misc
	Title   lipgloss.Style
	Help    lipgloss.Style
	Error   lipgloss.Style
	Success lipgloss.Style
	Muted   lipgloss.Style
	Faint   lipgloss.Style
}

// NewStyles creates a Styles instance from the given theme.
func NewStyles(theme *config.Theme) *Styles {
	s := &Styles{Theme: theme}

	s.Header = lipgloss.NewStyle().
		Foreground(theme.Text).
		Padding(0, 1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(theme.Border)

	s.Sidebar = lipgloss.NewStyle().
		Foreground(theme.Text).
		Padding(0, 1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderRight(true).
		BorderForeground(theme.Border)

	s.Content = lipgloss.NewStyle()

	s.StatusBar = lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Padding(0, 1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(theme.Border)

	// List items — 1 col padding each side
	s.ListItem = lipgloss.NewStyle().
		Foreground(theme.Text).
		Padding(0, 1)

	s.ListItemSelected = lipgloss.NewStyle().
		Background(theme.Selected).
		Foreground(theme.Background).
		Bold(true).
		Padding(0, 1)

	s.ListItemUnread = lipgloss.NewStyle().
		Foreground(theme.Text).
		Bold(true).
		Padding(0, 1)

	s.ListMeta = lipgloss.NewStyle().
		Foreground(theme.TextMuted)

	s.ListMetaSelected = lipgloss.NewStyle().
		Foreground(theme.Background).
		Background(theme.Selected)

	s.ListSecondary = lipgloss.NewStyle().
		Foreground(theme.TextMuted)

	s.Tag = lipgloss.NewStyle().
		Foreground(theme.Background).
		Background(theme.Accent).
		Padding(0, 1)

	// Modal
	s.Modal = lipgloss.NewStyle().
		Foreground(theme.Text).
		Background(theme.Surface).
		Padding(1, 2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent)

	s.ModalTitle = lipgloss.NewStyle().
		Foreground(theme.Accent).
		Background(theme.Surface).
		Bold(true).
		Align(lipgloss.Center)

	s.FormLabel = lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Width(12).
		Align(lipgloss.Right).
		MarginRight(1)

	s.FormInput = lipgloss.NewStyle().
		Foreground(theme.Text).
		Background(theme.SurfaceAlt).
		Padding(0, 1)

	s.FormButton = lipgloss.NewStyle().
		Foreground(theme.Background).
		Background(theme.Accent).
		Padding(0, 2).
		Bold(true)

	// Misc
	s.Title = lipgloss.NewStyle().
		Foreground(theme.Accent).
		Bold(true)

	s.Help = lipgloss.NewStyle().
		Foreground(theme.TextFaint)

	s.Error = lipgloss.NewStyle().
		Foreground(theme.Error)

	s.Success = lipgloss.NewStyle().
		Foreground(theme.Success)

	s.Muted = lipgloss.NewStyle().
		Foreground(theme.TextMuted)

	s.Faint = lipgloss.NewStyle().
		Foreground(theme.TextFaint)

	return s
}
