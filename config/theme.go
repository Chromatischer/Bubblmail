package config

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds the resolved color palette for the current terminal.
type Theme struct {
	IsDark bool

	// Base colors
	Background lipgloss.Color
	Surface    lipgloss.Color
	SurfaceAlt lipgloss.Color
	Overlay    lipgloss.Color

	// Text colors
	Text      lipgloss.Color
	TextMuted lipgloss.Color
	TextFaint lipgloss.Color

	// Semantic colors
	Accent   lipgloss.Color
	Selected lipgloss.Color
	Border   lipgloss.Color
	Unread   lipgloss.Color
	Starred  lipgloss.Color
	Error    lipgloss.Color
	Success  lipgloss.Color
	Warning  lipgloss.Color
}

// NewTheme creates a theme based on terminal background detection and config.
func NewTheme(cfg *Config) *Theme {
	// NO_COLOR support
	if os.Getenv("NO_COLOR") != "" {
		return newMonoTheme()
	}

	isDark := lipgloss.HasDarkBackground()

	if cfg.Theme.Mode == "light" {
		isDark = false
	} else if cfg.Theme.Mode == "dark" {
		isDark = true
	}

	accent := lipgloss.Color(cfg.Theme.Accent)

	if isDark {
		return &Theme{
			IsDark:     true,
			Background: lipgloss.Color("#1E1E2E"),
			Surface:    lipgloss.Color("#313244"),
			SurfaceAlt: lipgloss.Color("#45475A"),
			Overlay:    lipgloss.Color("#585B70"),
			Text:       lipgloss.Color("#CDD6F4"),
			TextMuted:  lipgloss.Color("#A6ADC8"),
			TextFaint:  lipgloss.Color("#6C7086"),
			Accent:     accent,
			Selected:   lipgloss.Color("#89B4FA"),
			Border:     lipgloss.Color("#585B70"),
			Unread:     lipgloss.Color("#89DCEB"),
			Starred:    lipgloss.Color("#F9E2AF"),
			Error:      lipgloss.Color("#F38BA8"),
			Success:    lipgloss.Color("#A6E3A1"),
			Warning:    lipgloss.Color("#F9E2AF"),
		}
	}

	return &Theme{
		IsDark:     false,
		Background: lipgloss.Color("#EFF1F5"),
		Surface:    lipgloss.Color("#E6E9EF"),
		SurfaceAlt: lipgloss.Color("#DCE0E8"),
		Overlay:    lipgloss.Color("#BCC0CC"),
		Text:       lipgloss.Color("#4C4F69"),
		TextMuted:  lipgloss.Color("#6C6F85"),
		TextFaint:  lipgloss.Color("#9CA0B0"),
		Accent:     accent,
		Selected:   lipgloss.Color("#1E66F5"),
		Border:     lipgloss.Color("#BCC0CC"),
		Unread:     lipgloss.Color("#04A5E5"),
		Starred:    lipgloss.Color("#DF8E1D"),
		Error:      lipgloss.Color("#D20F39"),
		Success:    lipgloss.Color("#40A02B"),
		Warning:    lipgloss.Color("#DF8E1D"),
	}
}

func newMonoTheme() *Theme {
	return &Theme{
		IsDark:     true,
		Background: lipgloss.Color(""),
		Surface:    lipgloss.Color(""),
		SurfaceAlt: lipgloss.Color(""),
		Overlay:    lipgloss.Color(""),
		Text:       lipgloss.Color(""),
		TextMuted:  lipgloss.Color(""),
		TextFaint:  lipgloss.Color(""),
		Accent:     lipgloss.Color(""),
		Selected:   lipgloss.Color(""),
		Border:     lipgloss.Color(""),
		Unread:     lipgloss.Color(""),
		Starred:    lipgloss.Color(""),
		Error:      lipgloss.Color(""),
		Success:    lipgloss.Color(""),
		Warning:    lipgloss.Color(""),
	}
}
