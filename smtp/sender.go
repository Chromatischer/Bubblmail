// Package smtp provides a tea.Cmd builder for sending email via SMTP.
package smtp

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	tea "github.com/charmbracelet/bubbletea"
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

// BuildRawMessage builds the raw RFC 2822 bytes for a composed message.
// The returned bytes can be used both for SMTP DATA and for IMAP APPEND.
func BuildRawMessage(draft *ComposedMessage) ([]byte, error) {
	if draft == nil {
		return nil, fmt.Errorf("draft is nil")
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
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		if err := writeQuotedPrintable(&buf, draft.Body); err != nil {
			return nil, fmt.Errorf("encoding text body: %w", err)
		}
	} else {
		mw := multipart.NewWriter(&buf)
		buf.WriteString("Content-Type: multipart/mixed; boundary=\"" + mw.Boundary() + "\"\r\n")
		buf.WriteString("\r\n")

		th := make(textproto.MIMEHeader)
		th.Set("Content-Type", "text/plain; charset=utf-8")
		th.Set("Content-Transfer-Encoding", "quoted-printable")
		pw, err := mw.CreatePart(th)
		if err != nil {
			return nil, fmt.Errorf("creating text part: %w", err)
		}
		if err := writeQuotedPrintable(pw, draft.Body); err != nil {
			return nil, fmt.Errorf("encoding text part: %w", err)
		}

		for _, a := range draft.Attachments {
			fileData, err := os.ReadFile(a.Path)
			if err != nil {
				return nil, fmt.Errorf("reading attachment %s: %w", a.Filename, err)
			}
			ct := mimeTypeForFile(a.Filename)
			ah := make(textproto.MIMEHeader)
			ah.Set("Content-Type", ct)
			ah.Set("Content-Transfer-Encoding", "base64")
			ah.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", a.Filename))
			aw, err := mw.CreatePart(ah)
			if err != nil {
				return nil, fmt.Errorf("creating attachment part: %w", err)
			}
			enc := base64.NewEncoder(base64.StdEncoding, aw)
			if _, err := enc.Write(fileData); err != nil {
				return nil, fmt.Errorf("encoding attachment %s: %w", a.Filename, err)
			}
			if err := enc.Close(); err != nil {
				return nil, fmt.Errorf("encoding attachment %s: %w", a.Filename, err)
			}
		}
		if err := mw.Close(); err != nil {
			return nil, fmt.Errorf("closing multipart message: %w", err)
		}
	}
	return buf.Bytes(), nil
}

func writeQuotedPrintable(w interface{ Write([]byte) (int, error) }, body string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(body)); err != nil {
		return err
	}
	return qp.Close()
}

// SendMessage returns a tea.Cmd that sends the composed message via SMTP.
func SendMessage(cfg *config.AccountConfig, draft *ComposedMessage) tea.Cmd {
	return func() tea.Msg {
		err := sendMessage(cfg, draft)
		return SendResultMsg{Err: err}
	}
}

// Send sends the composed message via SMTP synchronously.
func Send(cfg *config.AccountConfig, draft *ComposedMessage) error {
	return sendMessage(cfg, draft)
}

func sendMessage(cfg *config.AccountConfig, draft *ComposedMessage) error {
	password, err := cfg.ResolvePassword()
	if err != nil {
		return fmt.Errorf("resolving password: %w", err)
	}

	raw, err := BuildRawMessage(draft)
	if err != nil {
		return err
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
	if _, err := w.Write(raw); err != nil {
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
