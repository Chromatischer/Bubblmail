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
	Accent  lipgloss.Color
	Border  lipgloss.Color
	Unread  lipgloss.Color
	Starred lipgloss.Color
	Error   lipgloss.Color
	Success lipgloss.Color
	Warning lipgloss.Color

	// Chrome is the background shared by the header, sidebar and status bar.
	// The three of them frame the content area, so they must agree.
	Chrome lipgloss.Color
	// ChromeAlt is a one-step-raised chrome fill used for cursor rows and
	// inline input wells inside chrome regions.
	ChromeAlt lipgloss.Color
	// AccentSoft is a dimmed accent used for gutter bars and passive
	// highlights where a full Accent fill would shout.
	AccentSoft lipgloss.Color

	// Palette is a fixed ring of hues used for identity and nothing else: a
	// folder kind, a smart category and a sender each take a colour from it and
	// keep that colour everywhere they appear, so the eye can find them without
	// reading the row. Interaction stays the single Accent — no colour in this
	// ring ever means selected, focused or urgent.
	Palette []lipgloss.Color
}

// PaletteAt returns ring colour i, wrapping. Callers index it directly when a
// hue carries a fixed meaning — sent is green, drafts are yellow — rather than
// being derived from a name.
func (t *Theme) PaletteAt(i int) lipgloss.Color {
	if len(t.Palette) == 0 {
		return t.TextMuted
	}
	return t.Palette[((i%len(t.Palette))+len(t.Palette))%len(t.Palette)]
}

// Hue picks a stable colour from Palette for an arbitrary key. The same string
// always maps to the same colour within a theme, which is the entire point:
// a sender who is green in the list is green in the reader.
func (t *Theme) Hue(key string) lipgloss.Color {
	if len(t.Palette) == 0 {
		return t.TextMuted
	}
	// FNV-1a, inlined to keep the dependency out of the config package.
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return t.Palette[h%uint32(len(t.Palette))]
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
			Border:     lipgloss.Color("#585B70"),
			Unread:     lipgloss.Color("#89DCEB"),
			Starred:    lipgloss.Color("#F9E2AF"),
			Error:      lipgloss.Color("#F38BA8"),
			Success:    lipgloss.Color("#A6E3A1"),
			Warning:    lipgloss.Color("#F9E2AF"),
			Chrome:     lipgloss.Color("#181825"),
			ChromeAlt:  lipgloss.Color("#313244"),
			AccentSoft: lipgloss.Color("#45475A"),
			Palette: []lipgloss.Color{
				"#89B4FA", // blue
				"#A6E3A1", // green
				"#FAB387", // peach
				"#CBA6F7", // mauve
				"#94E2D5", // teal
				"#F5C2E7", // pink
				"#F9E2AF", // yellow
				"#74C7EC", // sapphire
			},
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
		Border:     lipgloss.Color("#BCC0CC"),
		Unread:     lipgloss.Color("#04A5E5"),
		Starred:    lipgloss.Color("#DF8E1D"),
		Error:      lipgloss.Color("#D20F39"),
		Success:    lipgloss.Color("#40A02B"),
		Warning:    lipgloss.Color("#DF8E1D"),
		Chrome:     lipgloss.Color("#E6E9EF"),
		ChromeAlt:  lipgloss.Color("#DCE0E8"),
		AccentSoft: lipgloss.Color("#BCC0CC"),
		Palette: []lipgloss.Color{
			"#1E66F5", // blue
			"#40A02B", // green
			"#FE640B", // peach
			"#8839EF", // mauve
			"#179299", // teal
			"#EA76CB", // pink
			"#DF8E1D", // yellow
			"#209FB5", // sapphire
		},
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
		Border:     lipgloss.Color(""),
		Unread:     lipgloss.Color(""),
		Starred:    lipgloss.Color(""),
		Error:      lipgloss.Color(""),
		Success:    lipgloss.Color(""),
		Warning:    lipgloss.Color(""),
		Chrome:     lipgloss.Color(""),
		ChromeAlt:  lipgloss.Color(""),
		AccentSoft: lipgloss.Color(""),
		// No palette: Hue falls back to TextMuted, which is also empty here.
	}
}
