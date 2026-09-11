package imap

import (
	"strings"
	"testing"
	"time"

	imaplib "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestParseBody_PlainText(t *testing.T) {
	raw := strings.NewReader("From: sender@example.com\r\nSubject: Test\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhello world")

	plain, html, attachments, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if plain != "hello world" {
		t.Fatalf("plain = %q, want hello world", plain)
	}
	if html != "" {
		t.Fatalf("html = %q, want empty", html)
	}
	if len(attachments) != 0 {
		t.Fatalf("attachments = %+v, want none", attachments)
	}
}

func TestParseBody_MultipartAlternativePrefersPlainAndKeepsHTML(t *testing.T) {
	raw := strings.NewReader(strings.Join([]string{
		"From: sender@example.com",
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=alt",
		"",
		"--alt",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"plain body",
		"--alt",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>html body</p>",
		"--alt--",
		"",
	}, "\r\n"))

	plain, html, attachments, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if plain != "plain body" {
		t.Fatalf("plain = %q, want plain body", plain)
	}
	if html != "<p>html body</p>" {
		t.Fatalf("html = %q, want html body", html)
	}
	if len(attachments) != 0 {
		t.Fatalf("attachments = %+v, want none", attachments)
	}
}

func TestParseBody_HTMLFallbackWhenNoPlainPart(t *testing.T) {
	raw := strings.NewReader(strings.Join([]string{
		"From: sender@example.com",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<div>Hello <b>world</b></div>",
	}, "\r\n"))

	plain, html, attachments, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if plain != "Hello world" {
		t.Fatalf("plain fallback = %q, want Hello world", plain)
	}
	if html != "<div>Hello <b>world</b></div>" {
		t.Fatalf("html = %q", html)
	}
	if len(attachments) != 0 {
		t.Fatalf("attachments = %+v, want none", attachments)
	}
}

func TestParseBody_MixedCaseHTMLContentType(t *testing.T) {
	raw := strings.NewReader(strings.Join([]string{
		"From: no-reply@mail.anthropic.com",
		"Content-Type: Text/HTML; charset=UTF-8",
		"",
		"<div>Your secure link to Claude.ai</div>",
	}, "\r\n"))

	plain, html, _, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if html != "<div>Your secure link to Claude.ai</div>" {
		t.Fatalf("html = %q", html)
	}
	if plain != "Your secure link to Claude.ai" {
		t.Fatalf("plain = %q", plain)
	}
}

func TestParseBody_DecodesTextCharset(t *testing.T) {
	raw := strings.NewReader("Content-Type: text/html; charset=iso-8859-1\r\n\r\n<p>cr\xe8me</p>")

	plain, html, _, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if html != "<p>crème</p>" || plain != "crème" {
		t.Fatalf("plain=%q html=%q", plain, html)
	}
}

func TestParseBody_ReplacesInvalidUTF8(t *testing.T) {
	raw := strings.NewReader("Content-Type: text/html; charset=utf-8\r\n\r\n<p>Claude \xff login</p>")

	plain, html, _, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if html == "" || plain == "" {
		t.Fatalf("body was discarded: plain=%q html=%q", plain, html)
	}
}

func TestParseBody_AttachmentFilenameFromDisposition(t *testing.T) {
	raw := strings.NewReader(strings.Join([]string{
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=mix",
		"",
		"--mix",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"hello",
		"--mix",
		"Content-Type: application/pdf",
		"Content-Disposition: attachment; filename=report.pdf",
		"",
		"PDFDATA",
		"--mix--",
		"",
	}, "\r\n"))

	plain, html, attachments, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if plain != "hello" || html != "" {
		t.Fatalf("unexpected body values plain=%q html=%q", plain, html)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %+v", attachments)
	}
	if attachments[0].Filename != "report.pdf" || attachments[0].ContentType != "application/pdf" || string(attachments[0].Data) != "PDFDATA" {
		t.Fatalf("unexpected attachment: %+v", attachments[0])
	}
}

func TestParseBody_AttachmentFallbackFilenameAndKnownCalendar(t *testing.T) {
	raw := strings.NewReader(strings.Join([]string{
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=mix",
		"",
		"--mix",
		"Content-Type: application/octet-stream",
		"Content-Disposition: attachment",
		"",
		"BIN",
		"--mix",
		"Content-Type: text/calendar; charset=utf-8",
		"",
		"BEGIN:VCALENDAR\nEND:VCALENDAR",
		"--mix--",
		"",
	}, "\r\n"))

	_, _, attachments, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if len(attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %+v", attachments)
	}
	if attachments[0].Filename != "attachment.bin" {
		t.Fatalf("fallback filename = %q, want attachment.bin", attachments[0].Filename)
	}
	if attachments[1].Filename != "invite.ics" {
		t.Fatalf("calendar filename = %q, want invite.ics", attachments[1].Filename)
	}
}

func TestParseBody_NestedMultipartIncludesAttachment(t *testing.T) {
	raw := strings.NewReader(strings.Join([]string{
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=outer",
		"",
		"--outer",
		"Content-Type: multipart/alternative; boundary=inner",
		"",
		"--inner",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"nested plain",
		"--inner",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>nested html</p>",
		"--inner--",
		"--outer",
		"Content-Type: image/png; name=chart.png",
		"",
		"PNGDATA",
		"--outer--",
		"",
	}, "\r\n"))

	plain, html, attachments, err := parseBody(raw)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if plain != "nested plain" || html != "<p>nested html</p>" {
		t.Fatalf("unexpected nested body plain=%q html=%q", plain, html)
	}
	if len(attachments) != 1 || attachments[0].Filename != "chart.png" {
		t.Fatalf("unexpected nested attachments: %+v", attachments)
	}
}

func TestConvertMessageBuffer_EnvelopeFields(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	buf := &imapclient.FetchMessageBuffer{
		UID:        42,
		RFC822Size: 1234,
		Flags:      []imaplib.Flag{imaplib.FlagSeen, imaplib.FlagFlagged},
		Envelope: &imaplib.Envelope{
			Subject:   "Subject",
			Date:      now,
			MessageID: "<msg@example.com>",
			InReplyTo: []string{"<parent@example.com>"},
			From:      []imaplib.Address{{Name: "Sender", Mailbox: "sender", Host: "example.com"}},
			To:        []imaplib.Address{{Name: "Recipient", Mailbox: "recipient", Host: "example.com"}},
			Cc:        []imaplib.Address{{Name: "Copy", Mailbox: "copy", Host: "example.com"}},
		},
	}

	msg := convertMessageBuffer(buf, "work", "INBOX")
	if msg.AccountName != "work" || msg.FolderName != "INBOX" || msg.UID != 42 || msg.Size != 1234 {
		t.Fatalf("unexpected basic fields: %+v", msg)
	}
	if msg.Subject != "Subject" || msg.MessageID != "<msg@example.com>" || msg.InReplyTo != "<parent@example.com>" || !msg.Date.Equal(now) {
		t.Fatalf("unexpected envelope fields: %+v", msg)
	}
	if len(msg.Flags) != 2 || !msg.IsRead() || !msg.IsStarred() {
		t.Fatalf("unexpected flags: %+v", msg.Flags)
	}
	if len(msg.From) != 1 || msg.From[0].Address != "sender@example.com" {
		t.Fatalf("unexpected from addresses: %+v", msg.From)
	}
	if len(msg.To) != 1 || msg.To[0].Address != "recipient@example.com" {
		t.Fatalf("unexpected to addresses: %+v", msg.To)
	}
	if len(msg.CC) != 1 || msg.CC[0].Address != "copy@example.com" {
		t.Fatalf("unexpected cc addresses: %+v", msg.CC)
	}
}
