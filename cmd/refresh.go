package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(refreshCmd)
	refreshCmd.Flags().String("account", "", "Refresh a specific account")
	refreshCmd.Flags().String("mailbox", "", "Refresh a specific mailbox")
	refreshCmd.Flags().Int("limit", defaultListLimit, "Max messages to fetch per mailbox")
	refreshCmd.Flags().Bool("counts", false, "Only refresh folder counts")
}

var refreshCmd = &cobra.Command{
	Use:     "refresh",
	Short:   "Refresh the local cache from IMAP",
	Example: "bubblmail refresh --account work --mailbox INBOX",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, store, err := loadConfigAndStore()
		if err != nil {
			return err
		}
		defer store.Close()

		account, _ := cmd.Flags().GetString("account")
		mailbox, _ := cmd.Flags().GetString("mailbox")
		limit, _ := cmd.Flags().GetInt("limit")
		countsOnly, _ := cmd.Flags().GetBool("counts")
		if limit <= 0 {
			return fmt.Errorf("limit must be positive")
		}

		if countsOnly {
			return refreshFolderCounts(cmd.Context(), cfg, store, account)
		}

		opts := listOptions{
			account: account,
			mailbox: mailbox,
			limit:   limit,
		}
		if err := refreshMessages(cmd.Context(), cfg, store, opts); err != nil {
			return err
		}
		return refreshFolderCounts(cmd.Context(), cfg, store, account)
	},
}
