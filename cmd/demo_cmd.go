package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/bubblmail/bubblmail/cache"
	demopkg "github.com/bubblmail/bubblmail/demo"
	"github.com/bubblmail/bubblmail/ui"
)

func init() {
	rootCmd.AddCommand(demoCmd)
}

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Launch the TUI with demo data (no account required)",
	Long: `Launches the full Bubblmail TUI pre-loaded with realistic fake email data.

No IMAP account or configuration is needed. All data is ephemeral — nothing
is written to your real mail cache. Use this to explore the interface before
setting up an account.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDemoTUI()
	},
}

func runDemoTUI() error {
	// Create a temporary directory for the demo cache so we never touch the
	// user's real mail data.
	tmpDir, err := os.MkdirTemp("", "bubblmail-demo-*")
	if err != nil {
		return fmt.Errorf("creating demo cache dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := cache.Open(tmpDir)
	if err != nil {
		return fmt.Errorf("opening demo cache: %w", err)
	}
	defer store.Close()

	account := demopkg.DemoAccount
	folders := demopkg.BuildDemoFolders()
	msgs := demopkg.BuildDemoMessages()

	// Seed messages into the store so that category and event lookups work.
	// UpsertMessages sets m.ID on each message.
	if err := store.UpsertMessages(msgs); err != nil {
		return fmt.Errorf("seeding demo messages: %w", err)
	}
	if err := store.UpsertFolders(account, folders); err != nil {
		return fmt.Errorf("seeding demo folders: %w", err)
	}

	// Seed smart-folder categories.
	cats := demopkg.BuildDemoCategories()
	catByUID := make(map[uint32]string, len(cats))
	for _, c := range cats {
		catByUID[c.UID] = c.Category
	}
	for _, m := range msgs {
		if cat, ok := catByUID[m.UID]; ok {
			if err := store.UpsertCategory(m.ID, cat, "demo", 1.0); err != nil {
				return fmt.Errorf("seeding demo category: %w", err)
			}
		}
	}

	// Seed suggested event for the weekend-thread follow-up (UID 8).
	for _, m := range msgs {
		if m.UID == 8 {
			ev := demopkg.BuildDemoSuggestedEvent(m.ID)
			if err := store.UpsertSuggestedEvent(ev); err != nil {
				return fmt.Errorf("seeding demo event: %w", err)
			}
			break
		}
	}

	app := ui.NewDemoApp(store, account, folders, msgs)

	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running demo TUI: %w", err)
	}
	return nil
}
