package smtp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bubblmail/bubblmail/data"
)

func TestBuildRawMessagePlainText(t *testing.T) {
	raw, err := BuildRawMessage(&ComposedMessage{
		From:    data.Address{Name: "Grüße", Address: "from@example.com"},
		To:      []data.Address{{Name: "Alice", Address: "alice@example.com"}},
		CC:      []data.Address{{Address: "cc@example.com"}},
		Subject: "Café update",
		Body:    "hello\nworld",
	})
	if err != nil {
		t.Fatalf("BuildRawMessage: %v", err)
	}
	text := string(raw)

	for _, want := range []string{
		"From: =?utf-8?q?Gr=C3=BC=C3=9Fe?= <from@example.com>\r\n",
		"To: Alice <alice@example.com>\r\n",
		"Cc: cc@example.com\r\n",
		"Subject: =?utf-8?q?Caf=C3=A9_update?=\r\n",
		"Content-Type: text/plain; charset=utf-8\r\n",
		"\r\nhello\nworld",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("raw message missing %q:\n%s", want, text)
		}
	}
}

func TestBuildRawMessageWithAttachment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("attached text"), 0600); err != nil {
		t.Fatalf("write attachment: %v", err)
	}

	raw, err := BuildRawMessage(&ComposedMessage{
		From:        data.Address{Address: "from@example.com"},
		To:          []data.Address{{Address: "to@example.com"}},
		Subject:     "With attachment",
		Body:        "body",
		Attachments: []Attachment{{Path: path, Filename: "note.txt"}},
	})
	if err != nil {
		t.Fatalf("BuildRawMessage: %v", err)
	}
	text := string(raw)

	for _, want := range []string{
		"Content-Type: multipart/mixed;",
		"Content-Type: text/plain",
		"Content-Disposition: attachment; filename=\"note.txt\"",
		"YXR0YWNoZWQgdGV4dA==",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("raw message missing %q:\n%s", want, text)
		}
	}
}

func TestBuildRawMessageMissingAttachment(t *testing.T) {
	_, err := BuildRawMessage(&ComposedMessage{
		From:        data.Address{Address: "from@example.com"},
		To:          []data.Address{{Address: "to@example.com"}},
		Subject:     "Missing attachment",
		Attachments: []Attachment{{Path: filepath.Join(t.TempDir(), "missing.txt"), Filename: "missing.txt"}},
	})
	if err == nil {
		t.Fatal("BuildRawMessage error = nil, want missing attachment error")
	}
	if !strings.Contains(err.Error(), "reading attachment missing.txt") {
		t.Fatalf("error = %q, want attachment filename context", err.Error())
	}
}

func TestCollectRecipientsSkipsEmptyAddresses(t *testing.T) {
	got := collectRecipients(
		[]data.Address{{Address: "to@example.com"}, {Name: "empty"}},
		[]data.Address{{Address: "cc@example.com"}, {}},
	)
	want := []string{"to@example.com", "cc@example.com"}
	if len(got) != len(want) {
		t.Fatalf("recipients = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recipients = %#v, want %#v", got, want)
		}
	}
}
