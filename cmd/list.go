package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	imaplib "github.com/bubblmail/bubblmail/imap"
)

const defaultListLimit = 50

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.AddCommand(listUnreadCmd)
	listCmd.AddCommand(listMailboxCmd)
	listCmd.AddCommand(listAccountCmd)
	listCmd.AddCommand(listSearchCmd)
	listCmd.AddCommand(listCountsCmd)

	addListFlags(listUnreadCmd)
	addListFlags(listMailboxCmd)
	addListFlags(listAccountCmd)
	addListFlags(listSearchCmd)
	addCountsFlags(listCountsCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List messages and counts",
	Example: strings.Join([]string{
		"bubblmail list unread --limit 25",
		"bubblmail list mailbox INBOX --format detailed",
		"bubblmail list account personal --unread",
		"bubblmail list search \"invoice\" --account work",
		"bubblmail list counts --refresh",
	}, "\n"),
}

var listUnreadCmd = &cobra.Command{
	Use:   "unread",
	Short: "List unread messages",
	Example: strings.Join([]string{
		"bubblmail list unread",
		"bubblmail list unread --account work --mailbox INBOX",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := readListOptions(cmd)
		if err != nil {
			return err
		}
		opts.unreadOnly = true
		return runList(cmd, opts, func(store *cache.Store) ([]*data.Message, error) {
			return store.GetMessagesFiltered(opts.account, opts.mailbox, true, opts.limit)
		})
	},
}

var listMailboxCmd = &cobra.Command{
	Use:   "mailbox <name>",
	Short: "List messages in a mailbox",
	Args:  cobra.ExactArgs(1),
	Example: strings.Join([]string{
		"bubblmail list mailbox INBOX",
		"bubblmail list mailbox Work --account personal --unread",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := readListOptions(cmd)
		if err != nil {
			return err
		}
		opts.mailbox = args[0]
		return runList(cmd, opts, func(store *cache.Store) ([]*data.Message, error) {
			return store.GetMessagesFiltered(opts.account, opts.mailbox, opts.unreadOnly, opts.limit)
		})
	},
}

var listAccountCmd = &cobra.Command{
	Use:   "account <name>",
	Short: "List messages for an account",
	Args:  cobra.ExactArgs(1),
	Example: strings.Join([]string{
		"bubblmail list account work",
		"bubblmail list account work --limit 10 --format detailed",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := readListOptions(cmd)
		if err != nil {
			return err
		}
		opts.account = args[0]
		return runList(cmd, opts, func(store *cache.Store) ([]*data.Message, error) {
			return store.GetMessagesFiltered(opts.account, opts.mailbox, opts.unreadOnly, opts.limit)
		})
	},
}

var listSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search cached messages",
	Args:  cobra.ExactArgs(1),
	Example: strings.Join([]string{
		"bubblmail list search \"receipt\"",
		"bubblmail list search \"project x\" --account work --unread",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := readListOptions(cmd)
		if err != nil {
			return err
		}
		opts.query = args[0]
		return runList(cmd, opts, func(store *cache.Store) ([]*data.Message, error) {
			msgs, err := store.SearchLocalFiltered(opts.query, opts.account, opts.mailbox, opts.limit)
			if err != nil {
				return nil, err
			}
			if opts.unreadOnly {
				msgs = filterUnread(msgs)
			}
			return msgs, nil
		})
	},
}

var listCountsCmd = &cobra.Command{
	Use:   "counts",
	Short: "Show unread/total counts per mailbox",
	Example: strings.Join([]string{
		"bubblmail list counts",
		"bubblmail list counts --account work --refresh",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, store, err := loadConfigAndStore()
		if err != nil {
			return err
		}
		defer store.Close()

		refresh, _ := cmd.Flags().GetBool("refresh")
		account, _ := cmd.Flags().GetString("account")
		if refresh {
			if err := refreshFolderCounts(cfg, store, account); err != nil {
				return err
			}
		}

		var folders []*data.Folder
		if account != "" {
			folders, err = store.GetFolders(account)
		} else {
			folders, err = store.GetAllFolders()
		}
		if err != nil {
			return err
		}
		return writeCounts(folders)
	},
}

type listOptions struct {
	account    string
	mailbox    string
	query      string
	format     listFormat
	limit      int
	refresh    bool
	unreadOnly bool
}

func addListFlags(cmd *cobra.Command) {
	cmd.Flags().String("account", "", "Filter by account")
	cmd.Flags().String("mailbox", "", "Filter by mailbox")
	cmd.Flags().String("format", formatCompact, "Output format: compact or detailed")
	cmd.Flags().Int("limit", defaultListLimit, "Max messages to show")
	cmd.Flags().Bool("refresh", false, "Refresh IMAP before listing")
	cmd.Flags().Bool("unread", false, "Only show unread messages")
}

func addCountsFlags(cmd *cobra.Command) {
	cmd.Flags().String("account", "", "Filter by account")
	cmd.Flags().Bool("refresh", false, "Refresh IMAP before listing")
}

func readListOptions(cmd *cobra.Command) (listOptions, error) {
	account, _ := cmd.Flags().GetString("account")
	mailbox, _ := cmd.Flags().GetString("mailbox")
	format, _ := cmd.Flags().GetString("format")
	limit, _ := cmd.Flags().GetInt("limit")
	refresh, _ := cmd.Flags().GetBool("refresh")
	unreadOnly, _ := cmd.Flags().GetBool("unread")
	if limit <= 0 {
		return listOptions{}, fmt.Errorf("limit must be positive")
	}
	return listOptions{
		account:    account,
		mailbox:    mailbox,
		format:     normalizeFormat(format),
		limit:      limit,
		refresh:    refresh,
		unreadOnly: unreadOnly,
	}, nil
}

func runList(cmd *cobra.Command, opts listOptions, query func(*cache.Store) ([]*data.Message, error)) error {
	cfg, store, err := loadConfigAndStore()
	if err != nil {
		return err
	}
	defer store.Close()

	if opts.refresh {
		if err := refreshMessages(cfg, store, opts); err != nil {
			return err
		}
	}

	messages, err := query(store)
	if err != nil {
		return err
	}

	output, err := formatMessages(messages, opts.format)
	if err != nil {
		return err
	}
	if output != "" {
		fmt.Fprintln(os.Stdout, output)
	}
	return nil
}

func filterUnread(messages []*data.Message) []*data.Message {
	filtered := make([]*data.Message, 0, len(messages))
	for _, m := range messages {
		if !m.IsRead() {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func loadConfigAndStore() (*config.Config, *cache.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	cacheDir := cfg.Cache.Dir
	if cacheDir == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return nil, nil, fmt.Errorf("finding cache dir: %w", err)
		}
		cacheDir = userCache + "/bubblmail"
	}

	store, err := cache.Open(cacheDir)
	if err != nil {
		return nil, nil, fmt.Errorf("opening cache: %w", err)
	}
	return cfg, store, nil
}

func refreshMessages(cfg *config.Config, store *cache.Store, opts listOptions) error {
	accounts, err := selectAccounts(cfg, opts.account)
	if err != nil {
		return err
	}

	for _, acct := range accounts {
		client, err := imaplib.Connect(acct)
		if err != nil {
			return err
		}
		if err := refreshMessagesForAccount(client, store, opts); err != nil {
			client.Close()
			return err
		}
		client.Close()
	}
	return nil
}

func refreshMessagesForAccount(client *imaplib.Client, store *cache.Store, opts listOptions) error {
	account := client.AccountName()
	if opts.mailbox != "" {
		msg := client.FetchMessages(opts.mailbox, opts.limit)()
		if listMsg, ok := msg.(imaplib.MessageListMsg); ok {
			if listMsg.Err != nil {
				return listMsg.Err
			}
			return store.UpsertMessages(listMsg.Messages)
		}
		return nil
	}

	foldersMsg := client.FetchFolders()()
	if msg, ok := foldersMsg.(imaplib.FolderListMsg); ok {
		if msg.Err != nil {
			return msg.Err
		}
		if err := store.UpsertFolders(account, msg.Folders); err != nil {
			return err
		}
	}

	// Default to INBOX if no mailbox filter provided.
	folder := opts.mailbox
	if folder == "" {
		folder = "INBOX"
	}
	msg := client.FetchMessages(folder, opts.limit)()
	if listMsg, ok := msg.(imaplib.MessageListMsg); ok {
		if listMsg.Err != nil {
			return listMsg.Err
		}
		return store.UpsertMessages(listMsg.Messages)
	}
	return nil
}

func refreshFolderCounts(cfg *config.Config, store *cache.Store, account string) error {
	accounts, err := selectAccounts(cfg, account)
	if err != nil {
		return err
	}
	for _, acct := range accounts {
		client, err := imaplib.Connect(acct)
		if err != nil {
			return err
		}
		foldersMsg := client.FetchFolders()()
		if msg, ok := foldersMsg.(imaplib.FolderListMsg); ok {
			if msg.Err != nil {
				client.Close()
				return msg.Err
			}
			if err := store.UpsertFolders(acct.Name, msg.Folders); err != nil {
				client.Close()
				return err
			}
			for _, f := range msg.Folders {
				unread, total, err := client.FolderStatus(f.Name)
				if err != nil {
					client.Close()
					return err
				}
				if err := store.UpdateFolderCounts(acct.Name, f.Name, unread, total); err != nil {
					client.Close()
					return err
				}
			}
		}
		client.Close()
	}
	return nil
}

func selectAccounts(cfg *config.Config, account string) ([]*config.AccountConfig, error) {
	if len(cfg.Accounts) == 0 {
		return nil, fmt.Errorf("no accounts configured")
	}
	if account != "" {
		for i := range cfg.Accounts {
			if cfg.Accounts[i].Name == account {
				return []*config.AccountConfig{&cfg.Accounts[i]}, nil
			}
		}
		return nil, fmt.Errorf("account %q not found", account)
	}
	accounts := make([]*config.AccountConfig, 0, len(cfg.Accounts))
	for i := range cfg.Accounts {
		accounts = append(accounts, &cfg.Accounts[i])
	}
	return accounts, nil
}

func writeCounts(folders []*data.Folder) error {
	if len(folders) == 0 {
		return nil
	}
	lines := make([]string, 0, len(folders))
	for _, f := range folders {
		name := f.Name
		if f.DisplayName != "" {
			name = f.DisplayName
		}
		line := fmt.Sprintf("%s  %s  %d/%d", f.AccountName, name, f.Unread, f.Total)
		lines = append(lines, line)
	}
	fmt.Fprintln(os.Stdout, strings.Join(lines, "\n"))
	return nil
}
