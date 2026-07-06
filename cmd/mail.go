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
	mailCmd.AddCommand(mailReadCmd)
	mailCmd.AddCommand(mailUnreadCmd)

	addMailTargetFlags(mailViewCmd)
	addMailTargetFlags(mailMoveCmd)
	addMailReadFlags(mailReadCmd)
	addMailReadFlags(mailUnreadCmd)
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
			if err := cmd.Context().Err(); err != nil {
				return err
			}
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
			if err := cmd.Context().Err(); err != nil {
				return err
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
	Use:   "move <uid> [uid...] <dest-folder>",
	Short: "Move messages to another folder",
	Args:  cobra.MinimumNArgs(2),
	Example: strings.Join([]string{
		"bubblmail mail move 42 Archive --account work --mailbox INBOX",
		"bubblmail mail move 42 43 44 Archive --account work --mailbox INBOX",
		"bubblmail mail move 42 Clients/Acme --account work --mailbox INBOX",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		destFolder, err := normalizeFolderPath(args[len(args)-1])
		if err != nil {
			return err
		}
		uids := make([]uint32, 0, len(args)-1)
		for _, arg := range args[:len(args)-1] {
			uid, err := parseUID(arg)
			if err != nil {
				return err
			}
			uids = append(uids, uid)
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
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		client, err := imaplib.Connect(acct)
		if err != nil {
			return err
		}

		succeeded, failed := 0, 0
		var firstErr error
		for i, uid := range uids {
			if err := cmd.Context().Err(); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				failed += len(uids) - i
				break
			}
			destUID, err := client.MoveMessageSync(sourceFolder, uid, destFolder)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("uid %d: %w", uid, err)
				}
				failed++
				continue
			}
			if err := store.MoveMessage(account, sourceFolder, uid, destFolder, destUID); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("uid %d: updating cache: %w", uid, err)
				}
				failed++
				continue
			}
			succeeded++
		}
		closeErr := client.Close()
		if firstErr == nil && closeErr != nil {
			firstErr = closeErr
		}

		if len(uids) == 1 && failed == 0 && firstErr == nil {
			fmt.Fprintf(os.Stdout, "Moved %s/%d to %s\n", sourceFolder, uids[0], destFolder)
			return nil
		}
		if failed > 0 {
			fmt.Fprintf(os.Stdout, "Moved %d message(s) from %s to %s. (%d failed; first error: %v)\n", succeeded, sourceFolder, destFolder, failed, firstErr)
			return firstErr
		}
		if firstErr != nil {
			return firstErr
		}
		fmt.Fprintf(os.Stdout, "Moved %d message(s) from %s to %s.\n", succeeded, sourceFolder, destFolder)
		return nil
	},
}

var mailReadCmd = &cobra.Command{
	Use:   "read [uid...]",
	Short: "Mark messages as read",
	Example: strings.Join([]string{
		"bubblmail mail read 42 43 --account work --mailbox INBOX",
		"bubblmail mail read --all --account work --mailbox Triage/Green",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMailSetSeen(cmd, args, true)
	},
}

var mailUnreadCmd = &cobra.Command{
	Use:   "unread [uid...]",
	Short: "Mark messages as unread",
	Example: strings.Join([]string{
		"bubblmail mail unread 42 43 --account work --mailbox INBOX",
		"bubblmail mail unread --all --account work --mailbox Triage/Green",
	}, "\n"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMailSetSeen(cmd, args, false)
	},
}

type mailFlagTarget struct {
	uid   uint32
	flags []data.Flag
}

func runMailSetSeen(cmd *cobra.Command, args []string, setSeen bool) error {
	all, _ := cmd.Flags().GetBool("all")
	if all && len(args) > 0 {
		return errors.New("--all cannot be used with explicit UIDs")
	}
	if !all && len(args) == 0 {
		return errors.New("provide at least one UID or use --all")
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

	targets, err := mailFlagTargets(store, account, mailbox, args, all, setSeen)
	if err != nil {
		return err
	}
	state := "unread"
	if setSeen {
		state = "read"
	}
	if len(targets) == 0 {
		fmt.Fprintf(os.Stdout, "Marked 0 message(s) in %s as %s.\n", mailbox, state)
		return nil
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

	succeeded, failed := 0, 0
	var firstErr error
	for i, target := range targets {
		if err := cmd.Context().Err(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failed += len(targets) - i
			break
		}
		if err := client.SetFlagSync(mailbox, target.uid, data.FlagSeen, setSeen); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("uid %d: %w", target.uid, err)
			}
			failed++
			continue
		}
		flags := target.flags
		if flags == nil {
			cached, err := store.GetMessageByUID(account, mailbox, target.uid)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("uid %d: reading cache: %w", target.uid, err)
				}
				failed++
				continue
			}
			flags = cached.Flags
		}
		if err := store.SetFlags(account, mailbox, target.uid, withFlag(flags, data.FlagSeen, setSeen)); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("uid %d: updating cache: %w", target.uid, err)
			}
			failed++
			continue
		}
		succeeded++
	}
	closeErr := client.Close()
	if firstErr == nil && closeErr != nil {
		firstErr = closeErr
	}

	if failed > 0 {
		fmt.Fprintf(os.Stdout, "Marked %d message(s) in %s as %s. (%d failed; first error: %v)\n", succeeded, mailbox, state, failed, firstErr)
		return firstErr
	}
	if firstErr != nil {
		return firstErr
	}
	fmt.Fprintf(os.Stdout, "Marked %d message(s) in %s as %s.\n", succeeded, mailbox, state)
	return nil
}

func mailFlagTargets(store interface {
	GetMessagesFiltered(string, string, bool, int) ([]*data.Message, error)
}, account, mailbox string, args []string, all, setSeen bool) ([]mailFlagTarget, error) {
	if !all {
		targets := make([]mailFlagTarget, 0, len(args))
		for _, arg := range args {
			uid, err := parseUID(arg)
			if err != nil {
				return nil, err
			}
			targets = append(targets, mailFlagTarget{uid: uid})
		}
		return targets, nil
	}

	msgs, err := store.GetMessagesFiltered(account, mailbox, setSeen, 0)
	if err != nil {
		return nil, fmt.Errorf("listing cached messages: %w", err)
	}
	targets := make([]mailFlagTarget, 0, len(msgs))
	for _, msg := range msgs {
		if setSeen || msg.IsRead() {
			targets = append(targets, mailFlagTarget{uid: msg.UID, flags: msg.Flags})
		}
	}
	return targets, nil
}

func withFlag(flags []data.Flag, flag data.Flag, set bool) []data.Flag {
	out := make([]data.Flag, 0, len(flags)+1)
	for _, f := range flags {
		if f == flag {
			continue
		}
		out = append(out, f)
	}
	if set {
		out = append(out, flag)
	}
	return out
}

func addMailTargetFlags(cmd *cobra.Command) {
	cmd.Flags().String("account", "", "Account name")
	cmd.Flags().String("mailbox", "", "Source mailbox, using / for nested folders")
}

func addMailReadFlags(cmd *cobra.Command) {
	addMailTargetFlags(cmd)
	cmd.Flags().Bool("all", false, "Mark all messages currently not in the target state")
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
