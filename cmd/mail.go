package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bubblmail/bubblmail/data"
	imaplib "github.com/bubblmail/bubblmail/imap"
	"github.com/bubblmail/bubblmail/util"
)

func init() {
	rootCmd.AddCommand(mailCmd)
	mailCmd.AddCommand(mailViewCmd)
	mailCmd.AddCommand(mailMoveCmd)

	addMailTargetFlags(mailViewCmd)
	addMailTargetFlags(mailMoveCmd)
}

var mailCmd = &cobra.Command{
	Use:   "mail",
	Short: "View and move messages",
}

var mailViewCmd = &cobra.Command{
	Use:   "view <uid>",
	Short: "View a message",
	Args:  cobra.ExactArgs(1),
	Example: strings.Join([]string{
		"bubblmail mail view 42 --account work --mailbox INBOX",
		"bubblmail mail view 42 --account work --mailbox Clients/Acme",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		uid, err := parseUID(args[0])
		if err != nil {
			return err
		}
		account, mailbox, err := readMailTarget(cmd)
		if err != nil {
			return err
		}
		cfg, store, err := loadConfigAndStore()
		if err != nil {
			return err
		}
		defer store.Close()

		msg, err := store.GetMessageByUID(account, mailbox, uid)
		if err != nil {
			return fmt.Errorf("finding cached message: %w", err)
		}
		bodyText, bodyHTML, err := store.GetBody(msg.ID)
		if err != nil {
			return fmt.Errorf("reading cached body: %w", err)
		}
		if bodyText == "" && bodyHTML == "" {
			acct, err := selectAccount(cfg, account)
			if err != nil {
				return err
			}
			client, err := imaplib.Connect(acct)
			if err != nil {
				return err
			}
			bodyText, bodyHTML, _, err = client.FetchBodySync(mailbox, uid)
			closeErr := client.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if err := store.UpsertBody(msg.ID, bodyText, bodyHTML); err != nil {
				return fmt.Errorf("caching body: %w", err)
			}
		}

		fmt.Fprintln(os.Stdout, formatMessageView(msg, bodyText, bodyHTML))
		return nil
	},
}

var mailMoveCmd = &cobra.Command{
	Use:   "move <uid> <dest-folder>",
	Short: "Move a message to another folder",
	Args:  cobra.ExactArgs(2),
	Example: strings.Join([]string{
		"bubblmail mail move 42 Archive --account work --mailbox INBOX",
		"bubblmail mail move 42 Clients/Acme --account work --mailbox INBOX",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		uid, err := parseUID(args[0])
		if err != nil {
			return err
		}
		destFolder, err := normalizeFolderPath(args[1])
		if err != nil {
			return err
		}
		account, sourceFolder, err := readMailTarget(cmd)
		if err != nil {
			return err
		}
		cfg, store, err := loadConfigAndStore()
		if err != nil {
			return err
		}
		defer store.Close()
		acct, err := selectAccount(cfg, account)
		if err != nil {
			return err
		}
		client, err := imaplib.Connect(acct)
		if err != nil {
			return err
		}
		destUID, err := client.MoveMessageSync(sourceFolder, uid, destFolder)
		if err != nil {
			client.Close()
			return err
		}
		if err := client.Close(); err != nil {
			return err
		}
		if err := store.MoveMessage(account, sourceFolder, uid, destFolder, destUID); err != nil {
			return fmt.Errorf("updating cache: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Moved %s/%d to %s\n", sourceFolder, uid, destFolder)
		return nil
	},
}

func addMailTargetFlags(cmd *cobra.Command) {
	cmd.Flags().String("account", "", "Account name")
	cmd.Flags().String("mailbox", "", "Source mailbox, using / for nested folders")
}

func readMailTarget(cmd *cobra.Command) (string, string, error) {
	account, _ := cmd.Flags().GetString("account")
	account = strings.TrimSpace(account)
	if account == "" {
		return "", "", errors.New("account is required")
	}
	mailbox, _ := cmd.Flags().GetString("mailbox")
	mailbox, err := normalizeFolderPath(mailbox)
	if err != nil {
		return "", "", fmt.Errorf("mailbox: %w", err)
	}
	return account, mailbox, nil
}

func parseUID(raw string) (uint32, error) {
	uid64, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil || uid64 == 0 {
		return 0, fmt.Errorf("uid must be a positive integer")
	}
	return uint32(uid64), nil
}

func normalizeFolderPath(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("folder name is required")
	}
	if strings.Contains(name, `\`) {
		return "", errors.New("use / for nested folders")
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return "", fmt.Errorf("folder paths must use non-empty /-separated segments")
		}
	}
	return strings.Join(parts, "/"), nil
}

func formatMessageView(msg *data.Message, bodyText, bodyHTML string) string {
	body := bodyText
	if strings.TrimSpace(body) == "" {
		body = bodyHTML
	}
	lines := []string{
		fmt.Sprintf("Account: %s", msg.AccountName),
		fmt.Sprintf("Folder: %s", msg.FolderName),
		fmt.Sprintf("UID: %d", msg.UID),
		fmt.Sprintf("From: %s", formatAddresses(msg.From)),
		fmt.Sprintf("To: %s", formatAddresses(msg.To)),
	}
	if len(msg.CC) > 0 {
		lines = append(lines, fmt.Sprintf("Cc: %s", formatAddresses(msg.CC)))
	}
	lines = append(lines,
		fmt.Sprintf("Date: %s", util.FormatDateTime(msg.Date)),
		fmt.Sprintf("Subject: %s", emptySubject(msg.Subject)),
		"",
		body,
	)
	return strings.Join(lines, "\n")
}

func formatAddresses(addrs []data.Address) string {
	parts := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		parts = append(parts, addr.String())
	}
	return strings.Join(parts, ", ")
}

func emptySubject(subject string) string {
	if strings.TrimSpace(subject) == "" {
		return "(no subject)"
	}
	return subject
}
