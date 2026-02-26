package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/ui"
)

func runTUI() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cacheDir := cfg.Cache.Dir
	if cacheDir == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("finding cache dir: %w", err)
		}
		cacheDir = userCache + "/bubblmail"
	}

	store, err := cache.Open(cacheDir)
	if err != nil {
		return fmt.Errorf("opening cache: %w", err)
	}
	defer store.Close()

	app := ui.NewApp(cfg, store)

	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}
	return nil
}
