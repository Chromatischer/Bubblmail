package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
)

func init() {
	rootCmd.AddCommand(embeddingsCmd)
	embeddingsCmd.AddCommand(embeddingsStatusCmd)
}

var embeddingsCmd = &cobra.Command{
	Use:   "embeddings",
	Short: "Manage embeddings for semantic search",
}

var embeddingsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show embeddings coverage for each account",
	RunE: func(cmd *cobra.Command, args []string) error {
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

		model := cfg.Embeddings.Model
		if model == "" {
			model = "openai/text-embedding-3-small"
		}
		if len(cfg.Accounts) == 0 {
			fmt.Println("No accounts configured. Run: bubblmail auth add")
			return nil
		}
		for _, a := range cfg.Accounts {
			total, err := store.CountMessages(a.Name)
			if err != nil {
				return fmt.Errorf("counting messages: %w", err)
			}
			embedded, err := store.CountEmbeddings(a.Name, model)
			if err != nil {
				return fmt.Errorf("counting embeddings: %w", err)
			}
			pct := 0.0
			if total > 0 {
				pct = (float64(embedded) / float64(total)) * 100
			}
			fmt.Printf("%s\n  model     %s\n  embedded  %d / %d (%.1f%%)\n\n", a.Name, model, embedded, total, pct)
		}
		return nil
	},
}
