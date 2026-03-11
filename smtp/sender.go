// Package smtp provides a tea.Cmd builder for sending email via SMTP.
package smtp

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
)

// Attachment is a file to include in the email.
type Attachment struct {
	Path     string // filesystem path
	Filename string // filename in the email
}

// ComposedMessage is a draft email ready to send.
type ComposedMessage struct {
	From        data.Address
	To          []data.Address
	CC          []data.Address
	Subject     string
	Body        string
	Attachments []Attachment
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

	if len(draft.Attachments) == 0 {
		// Simple text-only message.
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(draft.Body)
	} else {
		// Multipart/mixed with text body + file attachments.
		mw := multipart.NewWriter(&buf)
		buf.WriteString("Content-Type: multipart/mixed; boundary=\"" + mw.Boundary() + "\"\r\n")
		buf.WriteString("\r\n")

		// Text part.
		th := make(textproto.MIMEHeader)
		th.Set("Content-Type", "text/plain; charset=utf-8")
		th.Set("Content-Transfer-Encoding", "quoted-printable")
		pw, err := mw.CreatePart(th)
		if err != nil {
			return fmt.Errorf("creating text part: %w", err)
		}
		fmt.Fprint(pw, draft.Body)

		// Attachment parts.
		for _, a := range draft.Attachments {
			fileData, err := os.ReadFile(a.Path)
			if err != nil {
				return fmt.Errorf("reading attachment %s: %w", a.Filename, err)
			}

			ct := mimeTypeForFile(a.Filename)
			ah := make(textproto.MIMEHeader)
			ah.Set("Content-Type", ct)
			ah.Set("Content-Transfer-Encoding", "base64")
			ah.Set("Content-Disposition",
				fmt.Sprintf("attachment; filename=%q", a.Filename))

			aw, err := mw.CreatePart(ah)
			if err != nil {
				return fmt.Errorf("creating attachment part: %w", err)
			}

			enc := base64.NewEncoder(base64.StdEncoding, aw)
			enc.Write(fileData)
			enc.Close()
		}

		mw.Close()
	}

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

// mimeTypeForFile returns a MIME type based on the file extension.
func mimeTypeForFile(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".zip":
		return "application/zip"
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".txt":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	default:
		return "application/octet-stream"
	}
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
