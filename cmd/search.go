package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/embeddings"
)

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.AddCommand(searchDiagCmd)

	searchDiagCmd.Flags().StringP("query", "q", "", "query to search for")
	searchDiagCmd.Flags().StringP("account", "a", "", "account name to use")
	searchDiagCmd.Flags().Int("limit", 200, "max embedding candidates to scan")
	searchDiagCmd.Flags().Int("top", 5, "top semantic results to print")
}

var searchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search utilities",
}

var searchDiagCmd = &cobra.Command{
	Use:   "diag",
	Short: "Diagnose local and semantic search",
	RunE: func(cmd *cobra.Command, args []string) error {
		query, _ := cmd.Flags().GetString("query")
		query = strings.TrimSpace(query)
		if query == "" {
			return errors.New("query is required")
		}
		accountFlag, _ := cmd.Flags().GetString("account")
		limit, _ := cmd.Flags().GetInt("limit")
		topN, _ := cmd.Flags().GetInt("top")
		if limit <= 0 {
			limit = 200
		}
		if topN <= 0 {
			topN = 5
		}

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

		account := strings.TrimSpace(accountFlag)
		if account == "" {
			if len(cfg.Accounts) == 0 {
				return errors.New("no accounts configured")
			}
			account = cfg.Accounts[0].Name
		}

		fmt.Printf("Search diagnostics for %q (account %q)\n\n", query, account)

		local, err := store.SearchLocal(query)
		if err != nil {
			return fmt.Errorf("local search: %w", err)
		}
		accountLocal := make([]int, 0, len(local))
		for i, msg := range local {
			if msg != nil && msg.AccountName == account {
				accountLocal = append(accountLocal, i)
			}
		}
		fmt.Printf("Local FTS results: %d total, %d for account\n", len(local), len(accountLocal))
		for i := 0; i < len(accountLocal) && i < 5; i++ {
			msg := local[accountLocal[i]]
			fmt.Printf("  %s  %s\n", msg.Date.Format("2006-01-02"), msg.Subject)
		}
		fmt.Println()

		client, err := embeddings.NewClient(cfg.Embeddings)
		if err != nil {
			fmt.Printf("Semantic search: unavailable (%v)\n", err)
			return nil
		}
		model := strings.TrimSpace(cfg.Embeddings.Model)
		if model == "" {
			model = "openai/text-embedding-3-small"
		}
		msgs, vectors, norms, err := store.ListEmbeddingCandidates(account, model, limit)
		if err != nil {
			return fmt.Errorf("listing embedding candidates: %w", err)
		}
		fmt.Printf("Embedding candidates: %d (limit %d)\n", len(msgs), limit)
		if len(msgs) == 0 {
			fmt.Println("Semantic results: none (no embeddings for this account/model)")
			return nil
		}

		vecs, err := client.EmbedTexts(cmd.Context(), []string{query})
		if err != nil {
			return fmt.Errorf("embedding query: %w", err)
		}
		if len(vecs) == 0 {
			return errors.New("embedding query: empty response")
		}
		queryVec := vecs[0]
		queryNorm := embeddings.VectorNorm(queryVec)
		top := embeddings.TopK(msgs, vectors, norms, queryVec, queryNorm, topN)
		fmt.Printf("Semantic top %d:\n", topN)
		for _, hit := range top {
			if hit.Message == nil {
				continue
			}
			fmt.Printf("  %.4f  %s  %s\n", hit.Score, hit.Message.Date.Format("2006-01-02"), hit.Message.Subject)
		}
		return nil
	},
}
