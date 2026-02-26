package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bubblmail/bubblmail/config"
	imaplib "github.com/bubblmail/bubblmail/imap"
	"github.com/bubblmail/bubblmail/setup"
)

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authAddCmd)
	authCmd.AddCommand(authListCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authRemoveCmd)
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage IMAP/SMTP accounts",
}

var authAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add or update an account interactively",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		return setup.RunWizard(cfg)
	},
}

var authListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured accounts",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if len(cfg.Accounts) == 0 {
			fmt.Println("No accounts configured. Run: bubblmail auth add")
			return nil
		}
		for _, a := range cfg.Accounts {
			tag := ""
			if a.Name == cfg.General.DefaultAccount {
				tag = "  (default)"
			}
			auth := "password"
			if a.Password == "" && a.PasswordCmd != "" {
				auth = "password_cmd"
			} else if a.Password == "" {
				auth = "no password set"
			}
			fmt.Printf("%s%s\n  imap  %s:%d\n  smtp  %s:%d\n  user  %s\n  auth  %s\n\n",
				a.Name, tag,
				a.IMAPHost, a.IMAPPort,
				a.SMTPHost, a.SMTPPort,
				a.Username,
				auth,
			)
		}
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Test IMAP connection for each configured account",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if len(cfg.Accounts) == 0 {
			fmt.Println("No accounts configured. Run: bubblmail auth add")
			return nil
		}
		for i := range cfg.Accounts {
			a := &cfg.Accounts[i]
			fmt.Printf("%-20s  testing... ", a.Name)
			client, err := imaplib.Connect(a)
			if err != nil {
				fmt.Printf("FAILED: %v\n", err)
				continue
			}
			client.Close()
			fmt.Printf("OK  (%s:%d)\n", a.IMAPHost, a.IMAPPort)
		}
		return nil
	},
}

var authRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an account from the config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		idx := -1
		for i, a := range cfg.Accounts {
			if a.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("account %q not found", name)
		}
		cfg.Accounts = append(cfg.Accounts[:idx], cfg.Accounts[idx+1:]...)
		if cfg.General.DefaultAccount == name {
			cfg.General.DefaultAccount = ""
			if len(cfg.Accounts) > 0 {
				cfg.General.DefaultAccount = cfg.Accounts[0].Name
			}
		}
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
		fmt.Printf("Removed account %q.\n", name)
		return nil
	},
}
