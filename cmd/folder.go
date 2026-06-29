package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bubblmail/bubblmail/data"
	imaplib "github.com/bubblmail/bubblmail/imap"
)

func init() {
	rootCmd.AddCommand(folderCmd)
	folderCmd.AddCommand(folderCreateCmd)
	folderCmd.AddCommand(folderMoveCmd)
	folderCmd.AddCommand(folderDeleteCmd)

	addFolderAccountFlag(folderCreateCmd)
	addFolderAccountFlag(folderMoveCmd)
	addFolderAccountFlag(folderDeleteCmd)
}

var folderCmd = &cobra.Command{
	Use:   "folder",
	Short: "Create, rename, and delete folders",
}

var folderCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a folder",
	Args:  cobra.ExactArgs(1),
	Example: strings.Join([]string{
		"bubblmail folder create Archive --account work",
		"bubblmail folder create Clients/Acme --account work",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := normalizeFolderPath(args[0])
		if err != nil {
			return err
		}
		cfg, store, err := loadConfigAndStore()
		if err != nil {
			return err
		}
		defer store.Close()
		account, err := readFolderAccount(cmd)
		if err != nil {
			return err
		}
		acct, err := selectAccount(cfg, account)
		if err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		client, err := imaplib.Connect(acct)
		if err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			client.Close()
			return err
		}
		if err := client.CreateFolderSync(name); err != nil {
			client.Close()
			return err
		}
		if err := client.Close(); err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		if err := store.UpsertFolders(account, []*data.Folder{folderFromPath(account, name)}); err != nil {
			return fmt.Errorf("updating cache: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Created folder %s\n", name)
		return nil
	},
}

var folderMoveCmd = &cobra.Command{
	Use:   "move <old-name> <new-name>",
	Short: "Rename or move a folder",
	Args:  cobra.ExactArgs(2),
	Example: strings.Join([]string{
		"bubblmail folder move Archive Projects/Archive --account work",
		"bubblmail folder move Clients/Acme Clients/Acme-Old --account work",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		oldName, err := normalizeFolderPath(args[0])
		if err != nil {
			return err
		}
		newName, err := normalizeFolderPath(args[1])
		if err != nil {
			return err
		}
		cfg, store, err := loadConfigAndStore()
		if err != nil {
			return err
		}
		defer store.Close()
		account, err := readFolderAccount(cmd)
		if err != nil {
			return err
		}
		acct, err := selectAccount(cfg, account)
		if err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		client, err := imaplib.Connect(acct)
		if err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			client.Close()
			return err
		}
		if err := client.RenameFolder(oldName, newName); err != nil {
			client.Close()
			return err
		}
		if err := client.Close(); err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		if err := store.RenameFolder(account, oldName, newName); err != nil {
			return fmt.Errorf("updating cache: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Moved folder %s to %s\n", oldName, newName)
		return nil
	},
}

var folderDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a folder",
	Args:  cobra.ExactArgs(1),
	Example: strings.Join([]string{
		"bubblmail folder delete Archive --account work",
		"bubblmail folder delete Clients/Acme --account work",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := normalizeFolderPath(args[0])
		if err != nil {
			return err
		}
		cfg, store, err := loadConfigAndStore()
		if err != nil {
			return err
		}
		defer store.Close()
		account, err := readFolderAccount(cmd)
		if err != nil {
			return err
		}
		acct, err := selectAccount(cfg, account)
		if err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		client, err := imaplib.Connect(acct)
		if err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			client.Close()
			return err
		}
		if err := client.DeleteFolder(name); err != nil {
			client.Close()
			return err
		}
		if err := client.Close(); err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		if err := store.DeleteFolder(account, name); err != nil {
			return fmt.Errorf("updating cache: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Deleted folder %s\n", name)
		return nil
	},
}

func addFolderAccountFlag(cmd *cobra.Command) {
	cmd.Flags().String("account", "", "Account name")
}

func readFolderAccount(cmd *cobra.Command) (string, error) {
	account, _ := cmd.Flags().GetString("account")
	account = strings.TrimSpace(account)
	if account == "" {
		return "", errors.New("account is required")
	}
	return account, nil
}

func folderFromPath(account, name string) *data.Folder {
	parts := strings.Split(name, "/")
	return &data.Folder{
		AccountName: account,
		Name:        name,
		DisplayName: parts[len(parts)-1],
		Delimiter:   "/",
		Depth:       len(parts) - 1,
	}
}
