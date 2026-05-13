package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/bubblmail/bubblmail/data"
)

func TestNormalizeFolderPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "plain folder", input: "Archive", want: "Archive"},
		{name: "nested folder keeps slash syntax", input: "Clients/Acme", want: "Clients/Acme"},
		{name: "trims surrounding whitespace", input: "  Clients/Acme  ", want: "Clients/Acme"},
		{name: "rejects empty folder", input: "", wantErr: true},
		{name: "rejects leading slash", input: "/Archive", wantErr: true},
		{name: "rejects trailing slash", input: "Archive/", wantErr: true},
		{name: "rejects empty segment", input: "Clients//Acme", wantErr: true},
		{name: "rejects backslash nesting", input: `Clients\Acme`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeFolderPath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeFolderPath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("normalizeFolderPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseUID(t *testing.T) {
	got, err := parseUID("42")
	if err != nil {
		t.Fatalf("parseUID() error = %v", err)
	}
	if got != 42 {
		t.Fatalf("parseUID() = %d, want 42", got)
	}

	if _, err := parseUID("0"); err == nil {
		t.Fatal("parseUID(\"0\") expected error")
	}
	if _, err := parseUID("-1"); err == nil {
		t.Fatal("parseUID(\"-1\") expected error")
	}
}

func TestFormatMessageViewUsesCachedPlainBody(t *testing.T) {
	msg := &data.Message{
		UID:         42,
		AccountName: "work",
		FolderName:  "Clients/Acme",
		From:        []data.Address{{Name: "Alice", Address: "alice@example.com"}},
		To:          []data.Address{{Address: "team@example.com"}},
		Subject:     "Project update",
		Date:        time.Date(2026, 5, 13, 10, 30, 0, 0, time.UTC),
	}

	out := formatMessageView(msg, "Plain body text", "<p>HTML body</p>")

	for _, want := range []string{
		"Account: work",
		"Folder: Clients/Acme",
		"UID: 42",
		"From: Alice",
		"To: team@example.com",
		"Subject: Project update",
		"Plain body text",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatMessageView() missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<p>HTML body</p>") {
		t.Fatalf("formatMessageView() should prefer plain body over HTML, got:\n%s", out)
	}
}
