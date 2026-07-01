package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/util"
	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) copySuggestedEvent() tea.Cmd {
	ev := a.readerView.SuggestedEvent()
	if ev == nil || !ev.HasEvent {
		return nil
	}
	action := a.readerView.FocusedEventAction()
	if action == "reject" {
		return a.rejectSuggestedEvent()
	}
	content := ev.PlainText
	label := "Copied suggested event"
	if action == "json" {
		content = a.readerView.SuggestedEventJSON()
		label = "Copied suggested event JSON"
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if err := util.CopyToClipboard(content); err != nil {
		return a.flash("Copy failed: "+err.Error(), "err")
	}
	return a.flash(label, "ok")
}

func (a *App) rejectSuggestedEvent() tea.Cmd {
	ev := a.readerView.SuggestedEvent()
	if ev == nil {
		return nil
	}
	if err := a.store.RejectSuggestedEvent(ev.MessageID); err != nil {
		return a.flash("Reject failed: "+err.Error(), "err")
	}
	// Hide the suggestion in the current view.
	rejected := *ev
	rejected.Rejected = true
	rejected.HasEvent = false
	a.readerView.SetSuggestedEvent(&rejected)
	return a.flash("Event suggestion rejected", "ok")
}

func (a *App) executeAttachmentAction() tea.Cmd {
	msg := a.readerView.AttachmentSource()
	if msg == nil {
		return nil
	}
	idx := a.readerView.FocusedAttachmentIndex()
	if idx < 0 || idx >= len(msg.Attachments) {
		return nil
	}
	att := msg.Attachments[idx]
	switch a.readerView.FocusedAttachmentAction() {
	case "open":
		return openAttachmentDefaultCmd(att)
	case "download":
		return downloadAttachmentCmd(att)
	case "editor":
		return openAttachmentEditorCmd(att)
	}
	return nil
}

// attachActionResultMsg is returned when an attachment action completes.
type attachActionResultMsg struct {
	action string
	path   string
	err    error
}

func openAttachmentDefaultCmd(att data.Attachment) tea.Cmd {
	return func() tea.Msg {
		path, err := writeAttachmentTemp(att)
		if err != nil {
			return attachActionResultMsg{action: "open", err: err}
		}
		if err := exec.Command("xdg-open", path).Start(); err != nil {
			return attachActionResultMsg{action: "open", err: err}
		}
		return attachActionResultMsg{action: "open", path: path}
	}
}

func downloadAttachmentCmd(att data.Attachment) tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return attachActionResultMsg{action: "download", err: fmt.Errorf("locating home directory: %w", err)}
		}
		dir := filepath.Join(home, "Downloads")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return attachActionResultMsg{action: "download", err: fmt.Errorf("creating downloads directory: %w", err)}
		}
		dest := filepath.Join(dir, safeAttachmentFilename(att.Filename))
		if err := os.WriteFile(dest, att.Data, 0644); err != nil {
			return attachActionResultMsg{action: "download", err: err}
		}
		return attachActionResultMsg{action: "download", path: dest}
	}
}

func openAttachmentEditorCmd(att data.Attachment) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	path, err := writeAttachmentTemp(att)
	if err != nil {
		return func() tea.Msg { return attachActionResultMsg{action: "editor", err: err} }
	}
	return tea.ExecProcess(exec.Command(editor, path), func(err error) tea.Msg {
		return attachActionResultMsg{action: "editor", path: path, err: err}
	})
}

func writeAttachmentTemp(att data.Attachment) (string, error) {
	ext := filepath.Ext(safeAttachmentFilename(att.Filename))
	f, err := os.CreateTemp("", "bubblmail-*"+ext)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(att.Data); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func safeAttachmentFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "attachment"
	}
	return name
}
