package cmd

import (
	"testing"
	"time"

	"github.com/bubblmail/bubblmail/data"
)

func TestNormalizeFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
	}{
		{
			name:     "empty string defaults to compact",
			input:    "",
			wantName: formatCompact,
		},
		{
			name:     "compact format",
			input:    "compact",
			wantName: formatCompact,
		},
		{
			name:     "detailed format",
			input:    "detailed",
			wantName: formatDetailed,
		},
		{
			name:     "detail alias",
			input:    "detail",
			wantName: formatDetailed,
		},
		{
			name:     "full alias",
			input:    "full",
			wantName: formatDetailed,
		},
		{
			name:     "uppercase compact",
			input:    "COMPACT",
			wantName: formatCompact,
		},
		{
			name:     "whitespace trimmed",
			input:    "  detailed  ",
			wantName: formatDetailed,
		},
		{
			name:     "unknown format preserved",
			input:    "custom",
			wantName: "custom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeFormat(tt.input)
			if got.name != tt.wantName {
				t.Errorf("normalizeFormat() = %v, want %v", got.name, tt.wantName)
			}
		})
	}
}

func TestFormatMessages_Compact(t *testing.T) {
	now := time.Now()
	messages := []*data.Message{
		{
			From:    []data.Address{{Name: "Alice", Address: "alice@example.com"}},
			Subject: "Test Subject 1",
			Date:    now,
		},
		{
			From:    []data.Address{{Name: "Bob", Address: "bob@example.com"}},
			Subject: "Test Subject 2",
			Date:    now.Add(-time.Hour),
		},
	}

	format := listFormat{name: formatCompact}
	result, err := formatMessages(messages, format)
	if err != nil {
		t.Fatalf("formatMessages() error = %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result")
	}

	lines := splitLines(result)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	for _, line := range lines {
		if line == "" {
			t.Errorf("unexpected empty line in compact output")
		}
	}
}

func TestFormatMessages_Detailed(t *testing.T) {
	now := time.Now()
	messages := []*data.Message{
		{
			AccountName: "work",
			FolderName:  "INBOX",
			From:        []data.Address{{Name: "Alice", Address: "alice@example.com"}},
			Subject:     "Test Subject",
			Date:        now,
			Flags:       []data.Flag{data.FlagSeen},
		},
	}

	format := listFormat{name: formatDetailed}
	result, err := formatMessages(messages, format)
	if err != nil {
		t.Fatalf("formatMessages() error = %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result")
	}

	lines := splitLines(result)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	if !containsSubstring(lines[0], "work") {
		t.Errorf("expected detailed output to contain account name, got: %s", lines[0])
	}

	if !containsSubstring(lines[0], "INBOX") {
		t.Errorf("expected detailed output to contain folder name, got: %s", lines[0])
	}
}

func TestFormatMessages_Empty(t *testing.T) {
	messages := []*data.Message{}

	format := listFormat{name: formatCompact}
	result, err := formatMessages(messages, format)
	if err != nil {
		t.Fatalf("formatMessages() error = %v", err)
	}

	if result != "" {
		t.Errorf("expected empty result for empty messages, got: %s", result)
	}
}

func TestFormatMessages_NoSubject(t *testing.T) {
	now := time.Now()
	messages := []*data.Message{
		{
			From:    []data.Address{{Name: "Alice", Address: "alice@example.com"}},
			Subject: "",
			Date:    now,
		},
	}

	format := listFormat{name: formatCompact}
	result, err := formatMessages(messages, format)
	if err != nil {
		t.Fatalf("formatMessages() error = %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result")
	}

	if !containsSubstring(result, "(no subject)") {
		t.Errorf("expected output to contain '(no subject)', got: %s", result)
	}
}

func TestFormatMessages_UnreadFlags(t *testing.T) {
	now := time.Now()
	messages := []*data.Message{
		{
			From:    []data.Address{{Name: "Alice", Address: "alice@example.com"}},
			Subject: "Test Subject",
			Date:    now,
			Flags:   []data.Flag{},
		},
		{
			From:    []data.Address{{Name: "Bob", Address: "bob@example.com"}},
			Subject: "Test Subject 2",
			Date:    now,
			Flags:   []data.Flag{data.FlagFlagged},
		},
	}

	format := listFormat{name: formatDetailed}
	result, err := formatMessages(messages, format)
	if err != nil {
		t.Fatalf("formatMessages() error = %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result")
	}

	if !containsSubstring(result, "unread") {
		t.Errorf("expected detailed output to contain 'unread' flag, got: %s", result)
	}

	if !containsSubstring(result, "star") {
		t.Errorf("expected detailed output to contain 'star' flag, got: %s", result)
	}
}

func TestRenderFlags(t *testing.T) {
	tests := []struct {
		name     string
		flags    []data.Flag
		expected string
	}{
		{
			name:     "no flags (unread)",
			flags:    []data.Flag{},
			expected: "unread",
		},
		{
			name:     "read (seen flag)",
			flags:    []data.Flag{data.FlagSeen},
			expected: "-",
		},
		{
			name:     "flagged and unread",
			flags:    []data.Flag{data.FlagFlagged},
			expected: "unread,star",
		},
		{
			name:     "read and flagged",
			flags:    []data.Flag{data.FlagSeen, data.FlagFlagged},
			expected: "star",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &data.Message{Flags: tt.flags}
			got := renderFlags(msg)
			if got != tt.expected {
				t.Errorf("renderFlags() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func splitLines(s string) []string {
	lines := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
