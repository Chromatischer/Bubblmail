// Package setup provides the interactive account setup wizard.
package setup

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/imap"
)

// RunWizard runs the interactive account setup wizard, writing the result to
// the user's config file.
func RunWizard(cfg *config.Config) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Bubblmail account setup")
	fmt.Println("=======================")
	fmt.Println()
	fmt.Println("Add or update an account in ~/.config/bubblmail/config.toml.")
	fmt.Println("Press Enter to accept the default value shown in [brackets].")
	fmt.Println()

	// -- Account --
	fmt.Println("-- Account --")
	fmt.Println()

	name := Prompt(reader, "Account name", "")
	if name == "" {
		fmt.Println("Aborted: account name is required.")
		return nil
	}

	imapHost := Prompt(reader, "IMAP host", "")
	imapPortStr := Prompt(reader, "IMAP port", "993")
	smtpHost := Prompt(reader, "SMTP host", "")
	smtpPortStr := Prompt(reader, "SMTP port", "587")
	username := Prompt(reader, "Username", "")

	imapPort, err := strconv.Atoi(imapPortStr)
	if err != nil || imapPort <= 0 || imapPort > 65535 {
		imapPort = 993
	}
	smtpPort, err := strconv.Atoi(smtpPortStr)
	if err != nil || smtpPort <= 0 || smtpPort > 65535 {
		smtpPort = 587
	}

	fmt.Println()

	// -- Authentication --
	fmt.Println("-- Authentication --")
	fmt.Println()

	var password, passwordCmd string
	if PromptYesNo(reader, "Use a command for password (e.g. pass, secret-tool)?", false) {
		passwordCmd = Prompt(reader, "Password command", "")
	} else {
		password = PromptPassword(reader)
	}

	fmt.Println()

	acc := config.AccountConfig{
		Name:        name,
		IMAPHost:    imapHost,
		IMAPPort:    imapPort,
		SMTPHost:    smtpHost,
		SMTPPort:    smtpPort,
		Username:    username,
		Password:    password,
		PasswordCmd: passwordCmd,
	}

	if PromptYesNo(reader, "Test IMAP connection now?", true) {
		testIMAPConnection(&acc)
	}

	fmt.Println()

	// Update existing account by name, or append.
	updated := false
	for i, a := range cfg.Accounts {
		if a.Name == name {
			cfg.Accounts[i] = acc
			updated = true
			break
		}
	}
	if !updated {
		cfg.Accounts = append(cfg.Accounts, acc)
		if cfg.General.DefaultAccount == "" {
			cfg.General.DefaultAccount = name
		}
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	configDir, _ := os.UserConfigDir()
	fmt.Printf("Configuration saved to %s/bubblmail/config.toml\n", configDir)
	return nil
}

func testIMAPConnection(acc *config.AccountConfig) {
	fmt.Print("  Testing connection... ")
	client, err := imap.Connect(acc)
	if err != nil {
		fmt.Printf("FAILED: %v\n", err)
		return
	}
	client.Close()
	fmt.Println("OK")
}

// Prompt prompts for a single line of input with an optional default.
func Prompt(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}
	line, _ := reader.ReadString('\n')
	val := strings.TrimSpace(line)
	if val == "" {
		return defaultVal
	}
	return val
}

// PromptYesNo prompts for a yes/no answer. The defaultYes flag controls which
// option is accepted on an empty response and which is shown capitalised.
func PromptYesNo(reader *bufio.Reader, label string, defaultYes bool) bool {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}
	fmt.Printf("  %s [%s]: ", label, hint)
	line, _ := reader.ReadString('\n')
	val := strings.TrimSpace(strings.ToLower(line))
	if val == "" {
		return defaultYes
	}
	return val == "y" || val == "yes"
}

// PromptPassword prompts for a password, hiding input when the terminal
// supports it and falling back to visible input otherwise.
func PromptPassword(reader *bufio.Reader) string {
	fmt.Print("  Password: ")
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		if reader.Buffered() > 0 {
			reader.Discard(reader.Buffered()) //nolint:errcheck
		}
		pw, err := term.ReadPassword(fd)
		fmt.Println()
		if err == nil {
			fmt.Printf("  (password set, %d characters)\n", len(pw))
			return string(pw)
		}
		fmt.Printf("  (hidden input failed: %v, falling back to visible input)\n", err)
		fmt.Print("  Password (visible): ")
	}
	line, _ := reader.ReadString('\n')
	pw := strings.TrimSpace(line)
	fmt.Printf("  (password set, %d characters)\n", len(pw))
	return pw
}
