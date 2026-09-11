// Package smtp provides a tea.Cmd builder for sending email via SMTP.
package smtp

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
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

const userAgent = "Bubblmail/0.1.0"

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
	Account string
	Raw     []byte
	Err     error
}

// BuildRawMessage builds the raw RFC 2822 bytes for a composed message.
// The returned bytes can be used both for SMTP DATA and for IMAP APPEND.
func BuildRawMessage(draft *ComposedMessage) ([]byte, error) {
	if draft == nil {
		return nil, fmt.Errorf("message is required")
	}
	from, err := formatAddress(draft.From)
	if err != nil {
		return nil, fmt.Errorf("invalid From address: %w", err)
	}
	to, err := formatAddresses(draft.To)
	if err != nil {
		return nil, fmt.Errorf("invalid To address: %w", err)
	}
	if to == "" {
		return nil, fmt.Errorf("at least one recipient is required")
	}
	cc, err := formatAddresses(draft.CC)
	if err != nil {
		return nil, fmt.Errorf("invalid Cc address: %w", err)
	}
	messageID, err := newMessageID(draft.From.Address)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	date := time.Now().Format(time.RFC1123Z)
	buf.WriteString("Date: " + date + "\r\n")
	buf.WriteString("Message-ID: " + messageID + "\r\n")
	buf.WriteString("From: " + from + "\r\n")
	buf.WriteString("To: " + to + "\r\n")
	if cc != "" {
		buf.WriteString("Cc: " + cc + "\r\n")
	}
	buf.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", draft.Subject) + "\r\n")
	buf.WriteString("User-Agent: " + userAgent + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")

	if len(draft.Attachments) == 0 {
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		qw := quotedprintable.NewWriter(&buf)
		if _, err := qw.Write([]byte(normalizeCRLF(draft.Body))); err != nil {
			return nil, fmt.Errorf("encoding message body: %w", err)
		}
		if err := qw.Close(); err != nil {
			return nil, fmt.Errorf("encoding message body: %w", err)
		}
	} else {
		mw := multipart.NewWriter(&buf)
		buf.WriteString("Content-Type: " + mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": mw.Boundary()}) + "\r\n")
		buf.WriteString("\r\n")

		th := make(textproto.MIMEHeader)
		th.Set("Content-Type", "text/plain; charset=utf-8")
		th.Set("Content-Transfer-Encoding", "quoted-printable")
		pw, err := mw.CreatePart(th)
		if err != nil {
			return nil, fmt.Errorf("creating text part: %w", err)
		}
		qw := quotedprintable.NewWriter(pw)
		if _, err := qw.Write([]byte(normalizeCRLF(draft.Body))); err != nil {
			return nil, fmt.Errorf("encoding message body: %w", err)
		}
		if err := qw.Close(); err != nil {
			return nil, fmt.Errorf("encoding message body: %w", err)
		}

		for _, a := range draft.Attachments {
			fileData, err := os.ReadFile(a.Path)
			if err != nil {
				return nil, fmt.Errorf("reading attachment %s: %w", a.Filename, err)
			}
			ct := mimeTypeForFile(a.Filename)
			ah := make(textproto.MIMEHeader)
			ah.Set("Content-Type", mime.FormatMediaType(ct, map[string]string{"name": a.Filename}))
			ah.Set("Content-Transfer-Encoding", "base64")
			ah.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename}))
			aw, err := mw.CreatePart(ah)
			if err != nil {
				return nil, fmt.Errorf("creating attachment part: %w", err)
			}
			if err := writeBase64(aw, fileData); err != nil {
				return nil, fmt.Errorf("encoding attachment %s: %w", a.Filename, err)
			}
		}
		if err := mw.Close(); err != nil {
			return nil, fmt.Errorf("closing MIME message: %w", err)
		}
	}
	return buf.Bytes(), nil
}

// SendMessage returns a tea.Cmd that sends the composed message via SMTP.
func SendMessage(cfg *config.AccountConfig, draft *ComposedMessage) tea.Cmd {
	return func() tea.Msg {
		raw, err := BuildRawMessage(draft)
		if err == nil {
			err = sendRawMessage(cfg, draft, raw)
		}
		return SendResultMsg{Account: cfg.Name, Raw: raw, Err: err}
	}
}

func sendRawMessage(cfg *config.AccountConfig, draft *ComposedMessage, raw []byte) error {
	password, err := cfg.ResolvePassword()
	if err != nil {
		return fmt.Errorf("resolving password: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	client, err := connectSMTP(addr, cfg.SMTPHost, cfg.SMTPPort)
	if err != nil {
		return err
	}
	defer client.Close()

	auth := smtp.PlainAuth("", cfg.Username, password, cfg.SMTPHost)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP authentication failed: %w", err)
	}

	fromEmail := cfg.Username
	if draft.From.Address != "" {
		fromEmail, err = envelopeAddress(draft.From.Address)
		if err != nil {
			return fmt.Errorf("invalid SMTP sender: %w", err)
		}
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

func connectSMTP(addr, host string, port int) (*smtp.Client, error) {
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	if port == 465 {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("connecting to SMTP server: %w", err)
		}
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("creating SMTP client: %w", err)
		}
		return client, nil
	}

	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connecting to SMTP server: %w", err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("creating SMTP client: %w", err)
	}
	if ok, _ := client.Extension("STARTTLS"); !ok {
		client.Close()
		return nil, fmt.Errorf("SMTP server does not support STARTTLS")
	}
	if err := client.StartTLS(tlsCfg); err != nil {
		client.Close()
		return nil, fmt.Errorf("starting SMTP TLS: %w", err)
	}
	return client, nil
}

func formatAddress(a data.Address) (string, error) {
	parsed, err := mail.ParseAddress(a.Address)
	if err != nil {
		return "", err
	}
	if a.Name != "" {
		parsed.Name = a.Name
	}
	return parsed.String(), nil
}

func formatAddresses(addrs []data.Address) (string, error) {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if strings.TrimSpace(a.Address) == "" {
			continue
		}
		formatted, err := formatAddress(a)
		if err != nil {
			return "", err
		}
		parts = append(parts, formatted)
	}
	return strings.Join(parts, ", "), nil
}

func newMessageID(from string) (string, error) {
	domain := "localhost"
	address, err := envelopeAddress(from)
	if err != nil {
		return "", err
	}
	if at := strings.LastIndexByte(address, '@'); at >= 0 && at+1 < len(address) {
		domain = address[at+1:]
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generating Message-ID: %w", err)
	}
	return fmt.Sprintf("<%x@%s>", random, domain), nil
}

func normalizeCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

func writeBase64(w io.Writer, data []byte) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	for len(encoded) > 76 {
		if _, err := fmt.Fprint(w, encoded[:76], "\r\n"); err != nil {
			return err
		}
		encoded = encoded[76:]
	}
	_, err := fmt.Fprint(w, encoded, "\r\n")
	return err
}

func collectRecipients(to, cc []data.Address) []string {
	var out []string
	for _, a := range to {
		if a.Address != "" {
			if address, err := envelopeAddress(a.Address); err == nil {
				out = append(out, address)
			}
		}
	}
	for _, a := range cc {
		if a.Address != "" {
			if address, err := envelopeAddress(a.Address); err == nil {
				out = append(out, address)
			}
		}
	}
	return out
}

func envelopeAddress(raw string) (string, error) {
	address, err := mail.ParseAddress(raw)
	if err != nil {
		return "", err
	}
	return address.Address, nil
}
