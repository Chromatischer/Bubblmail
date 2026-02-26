// Package smtp provides a tea.Cmd builder for sending email via SMTP.
package smtp

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"mime"
	"net/smtp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
)

// ComposedMessage is a draft email ready to send.
type ComposedMessage struct {
	From    data.Address
	To      []data.Address
	CC      []data.Address
	Subject string
	Body    string
}

// SendResultMsg carries the result of a send operation.
type SendResultMsg struct {
	Err error
}

// SendMessage returns a tea.Cmd that sends the composed message via SMTP.
func SendMessage(cfg *config.AccountConfig, draft *ComposedMessage) tea.Cmd {
	return func() tea.Msg {
		err := sendMessage(cfg, draft)
		return SendResultMsg{Err: err}
	}
}

func sendMessage(cfg *config.AccountConfig, draft *ComposedMessage) error {
	password, err := cfg.ResolvePassword()
	if err != nil {
		return fmt.Errorf("resolving password: %w", err)
	}

	// Build RFC 2822 message.
	var buf bytes.Buffer
	date := time.Now().Format(time.RFC1123Z)
	buf.WriteString("Date: " + date + "\r\n")
	buf.WriteString("From: " + formatAddress(draft.From) + "\r\n")
	buf.WriteString("To: " + formatAddresses(draft.To) + "\r\n")
	if len(draft.CC) > 0 {
		buf.WriteString("Cc: " + formatAddresses(draft.CC) + "\r\n")
	}
	buf.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", draft.Subject) + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(draft.Body)

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	tlsCfg := &tls.Config{ServerName: cfg.SMTPHost}
	tlsConn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("connecting to SMTP server: %w", err)
	}
	defer tlsConn.Close()

	client, err := smtp.NewClient(tlsConn, cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("creating SMTP client: %w", err)
	}
	defer client.Close()

	auth := smtp.PlainAuth("", cfg.Username, password, cfg.SMTPHost)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP authentication failed: %w", err)
	}

	fromEmail := cfg.Username
	if draft.From.Address != "" {
		fromEmail = draft.From.Address
	}
	if err := client.Mail(fromEmail); err != nil {
		return fmt.Errorf("SMTP MAIL command failed: %w", err)
	}

	rcpts := collectRecipients(draft.To, draft.CC)
	for _, rcpt := range rcpts {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("SMTP RCPT command failed for %s: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA command failed: %w", err)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("closing message writer: %w", err)
	}

	return client.Quit()
}

func formatAddress(a data.Address) string {
	if a.Name != "" {
		return fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", a.Name), a.Address)
	}
	return a.Address
}

func formatAddresses(addrs []data.Address) string {
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = formatAddress(a)
	}
	return strings.Join(parts, ", ")
}

func collectRecipients(to, cc []data.Address) []string {
	var out []string
	for _, a := range to {
		if a.Address != "" {
			out = append(out, a.Address)
		}
	}
	for _, a := range cc {
		if a.Address != "" {
			out = append(out, a.Address)
		}
	}
	return out
}
