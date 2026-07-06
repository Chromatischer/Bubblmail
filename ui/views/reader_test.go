package views

import (
	"testing"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
)

func TestReaderAttachmentEnterExit(t *testing.T) {
	v := NewReaderView(&config.Theme{})
	v.SetSize(80, 24)
	v.SetMessage(&data.Message{
		Subject: "Hi",
		Attachments: []data.Attachment{
			{Filename: "a.pdf", ContentType: "application/pdf"},
			{Filename: "b.png", ContentType: "image/png"},
		},
	})

	if !v.HasAttachments() {
		t.Fatal("expected HasAttachments")
	}
	if v.AttachFocusActive() {
		t.Fatal("attachment focus should start inactive")
	}

	v.EnterAttachments()
	if !v.AttachFocusActive() || v.FocusedAttachmentIndex() != 0 {
		t.Fatalf("after Enter: active=%v idx=%d, want active idx 0",
			v.AttachFocusActive(), v.FocusedAttachmentIndex())
	}

	v.FocusNextAttachment(1) // cycle within the section: 0 -> 1
	if v.FocusedAttachmentIndex() != 1 {
		t.Fatalf("after Tab: idx=%d, want 1", v.FocusedAttachmentIndex())
	}

	v.ExitAttachments()
	if v.AttachFocusActive() {
		t.Fatal("after Exit: focus should be inactive")
	}
}

func TestReaderEnterAttachmentsNoop(t *testing.T) {
	v := NewReaderView(&config.Theme{})
	v.SetSize(80, 24)
	v.SetMessage(&data.Message{Subject: "no files"})
	v.EnterAttachments()
	if v.AttachFocusActive() {
		t.Fatal("EnterAttachments must be a no-op without attachments")
	}
}
