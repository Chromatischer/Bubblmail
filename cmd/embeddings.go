package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/embeddings"
)

func init() {
	rootCmd.AddCommand(embeddingsCmd)
	embeddingsCmd.AddCommand(embeddingsStatusCmd)
	embeddingsCmd.AddCommand(embeddingsEmbedCmd)
	embeddingsEmbedCmd.AddCommand(embeddingsEmbedForceCmd)
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

var embeddingsEmbedCmd = &cobra.Command{
	Use:   "embed",
	Short: "Backfill embeddings for cached bodies",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runEmbeddingsBackfill(cmd.Context(), false)
	},
}

var embeddingsEmbedForceCmd = &cobra.Command{
	Use:   "force",
	Short: "Re-embed all cached bodies",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runEmbeddingsBackfill(cmd.Context(), true)
	},
}

func runEmbeddingsBackfill(ctx context.Context, force bool) error {
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

	client, err := embeddings.NewClient(cfg.Embeddings)
	if err != nil {
		return fmt.Errorf("creating embeddings client: %w", err)
	}
	model := cfg.Embeddings.Model
	if model == "" {
		model = "openai/text-embedding-3-small"
	}
	if len(cfg.Accounts) == 0 {
		fmt.Println("No accounts configured. Run: bubblmail auth add")
		return nil
	}

	batchSize := cfg.Embeddings.BatchSize
	if batchSize <= 0 {
		batchSize = 16
	}
	maxChars := cfg.Embeddings.MaxContentChars
	if maxChars <= 0 {
		maxChars = 8000
	}

	for _, a := range cfg.Accounts {
		msgs, bodies, err := store.ListBodiesForEmbedding(a.Name, model, force)
		if err != nil {
			return fmt.Errorf("listing cached bodies: %w", err)
		}
		if len(msgs) == 0 {
			fmt.Printf("%s: nothing to embed\n", a.Name)
			continue
		}
		fmt.Printf("%s: embedding %d messages\n", a.Name, len(msgs))

		batchTexts := make([]string, 0, batchSize)
		batchIDs := make([]int64, 0, batchSize)
		batchHashes := make([]string, 0, batchSize)

		flush := func() error {
			if len(batchTexts) == 0 {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			vecs, err := client.EmbedTexts(ctx, batchTexts)
			if err != nil {
				return err
			}
			if len(vecs) != len(batchTexts) {
				return fmt.Errorf("embeddings response mismatch")
			}
			for i := range vecs {
				vec := vecs[i]
				norm := embeddings.VectorNorm(vec)
				if err := store.SaveEmbedding(batchIDs[i], model, vec, norm, batchHashes[i]); err != nil {
					return err
				}
			}
			if err := store.BuildFolderEmbeddings(a.Name, model, cfg.Embeddings.FolderSampleLimit); err != nil {
				return fmt.Errorf("building folder embeddings: %w", err)
			}
			batchTexts = batchTexts[:0]
			batchIDs = batchIDs[:0]
			batchHashes = batchHashes[:0]
			return nil
		}

		for i, msg := range msgs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if msg == nil {
				continue
			}
			content := strings.TrimSpace(msg.Subject + "\n" + bodies[i])
			if content == "" {
				continue
			}
			if len(content) > maxChars {
				content = content[:maxChars]
			}
			hash := embeddings.HashContent(content)
			if !force {
				upToDate, err := store.EmbeddingUpToDate(msg.ID, model, hash)
				if err != nil || upToDate {
					continue
				}
			}
			batchTexts = append(batchTexts, content)
			batchIDs = append(batchIDs, msg.ID)
			batchHashes = append(batchHashes, hash)
			if len(batchTexts) >= batchSize {
				if err := flush(); err != nil {
					return fmt.Errorf("embedding batch: %w", err)
				}
			}
		}
		if err := flush(); err != nil {
			return fmt.Errorf("embedding batch: %w", err)
		}
	}

	return nil
}
