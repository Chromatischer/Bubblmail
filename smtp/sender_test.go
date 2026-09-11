package smtp

import (
	"bufio"
	"bytes"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"

	"github.com/bubblmail/bubblmail/data"
)

func TestBuildRawMessageProducesValidHeadersAndQuotedPrintableBody(t *testing.T) {
	draft := &ComposedMessage{
		From:    data.Address{Name: "Renée", Address: "sender@example.com"},
		To:      []data.Address{{Name: "Recipient", Address: "person@example.net"}},
		Subject: "Résumé",
		Body:    "crème brûlée\nsecond line",
	}

	raw, err := BuildRawMessage(draft)
	if err != nil {
		t.Fatalf("BuildRawMessage: %v", err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if !strings.HasPrefix(msg.Header.Get("Message-ID"), "<") || !strings.HasSuffix(msg.Header.Get("Message-ID"), "@example.com>") {
		t.Fatalf("invalid Message-ID: %q", msg.Header.Get("Message-ID"))
	}
	if got := msg.Header.Get("Content-Transfer-Encoding"); got != "quoted-printable" {
		t.Fatalf("Content-Transfer-Encoding = %q", got)
	}
	if got := msg.Header.Get("User-Agent"); got != userAgent {
		t.Fatalf("User-Agent = %q, want %q", got, userAgent)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got, want := string(body), "crème brûlée\r\nsecond line"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestBuildRawMessageRejectsHeaderInjection(t *testing.T) {
	draft := &ComposedMessage{
		From: data.Address{Address: "sender@example.com"},
		To:   []data.Address{{Address: "recipient@example.net\r\nBcc: victim@example.net"}},
	}
	if _, err := BuildRawMessage(draft); err == nil {
		t.Fatal("BuildRawMessage accepted an injected recipient header")
	}
}

func TestWriteBase64WrapsMIMELines(t *testing.T) {
	var buf bytes.Buffer
	if err := writeBase64(&buf, bytes.Repeat([]byte("a"), 200)); err != nil {
		t.Fatalf("writeBase64: %v", err)
	}
	scanner := bufio.NewScanner(&buf)
	for scanner.Scan() {
		if len(scanner.Text()) > 76 {
			t.Fatalf("base64 line has %d characters", len(scanner.Text()))
		}
	}
}
