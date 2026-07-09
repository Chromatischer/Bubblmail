package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newMailSendTestCommand(args ...string) *cobra.Command {
	cmd := &cobra.Command{Use: "send"}
	addMailSendFlags(cmd)
	cmd.SetArgs(args)
	return cmd
}

func TestReadMailSendDraftValidatesBodySource(t *testing.T) {
	base := []string{
		"--account", "work",
		"--to", "alice@example.com",
		"--subject", "Hello",
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "neither body nor body file",
			args: base,
			want: "exactly one of --body or --body-file is required",
		},
		{
			name: "both body and body file",
			args: append(append([]string{}, base...), "--body", "hi", "--body-file", "body.txt"),
			want: "exactly one of --body or --body-file is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newMailSendTestCommand(tt.args...)
			if err := cmd.ParseFlags(tt.args); err != nil {
				t.Fatalf("ParseFlags() error = %v", err)
			}
			_, _, err := readMailSendDraft(cmd)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("readMailSendDraft() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestReadMailSendDraftParsesRecipientsAndAttachments(t *testing.T) {
	args := []string{
		"--account", "work",
		"--to", "Alice <alice@example.com>, bob@example.com",
		"--cc", "Carol <carol@example.com>",
		"--subject", "Hello",
		"--body", "Hi",
		"--attach", "/tmp/a.txt,/tmp/b.pdf",
	}
	cmd := newMailSendTestCommand(args...)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	account, draft, err := readMailSendDraft(cmd)
	if err != nil {
		t.Fatalf("readMailSendDraft() error = %v", err)
	}
	if account != "work" {
		t.Fatalf("account = %q, want work", account)
	}
	if len(draft.To) != 2 || draft.To[0].Address != "alice@example.com" || draft.To[1].Address != "bob@example.com" {
		t.Fatalf("To = %#v", draft.To)
	}
	if len(draft.CC) != 1 || draft.CC[0].Address != "carol@example.com" {
		t.Fatalf("CC = %#v", draft.CC)
	}
	if len(draft.Attachments) != 2 {
		t.Fatalf("Attachments len = %d, want 2", len(draft.Attachments))
	}
	if draft.Attachments[0].Filename != filepath.Base(draft.Attachments[0].Path) {
		t.Fatalf("first attachment = %#v", draft.Attachments[0])
	}
}
