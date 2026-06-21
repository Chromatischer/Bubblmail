package ui

import (
	"strings"
	"testing"
)

func TestReaderHintsAreStateAware(t *testing.T) {
	sb := NewStatusBar(NewStyles(testTheme()))
	sb.SetWidth(160)

	render := func(hasAttach, attachActive, hasEvent bool) string {
		sb.SetReaderState(hasAttach, attachActive, hasEvent, false)
		return sb.View("reader")
	}

	plain := render(false, false, false)
	if strings.Contains(plain, "event action") {
		t.Error("plain reader should not advertise event actions")
	}
	if strings.Contains(plain, "attachments") {
		t.Error("plain reader should not advertise attachments")
	}
	if !strings.Contains(plain, "move") {
		t.Error("reader should advertise move (v)")
	}

	withAttach := render(true, false, false)
	if !strings.Contains(withAttach, "attachments") {
		t.Error("reader with attachments should advertise the a key")
	}

	withEvent := render(false, false, true)
	if !strings.Contains(withEvent, "event action") {
		t.Error("reader with an event should advertise event actions")
	}

	inSection := render(true, true, false)
	if !strings.Contains(inSection, "cycle") || !strings.Contains(inSection, "(tab)") {
		t.Error("attachment section should advertise tab to cycle")
	}
	if strings.Contains(inSection, "reply") {
		t.Error("attachment section should not show message-level actions like reply")
	}
}

func TestReaderThreadJumpHint(t *testing.T) {
	sb := NewStatusBar(NewStyles(testTheme()))
	sb.SetWidth(160)

	sb.SetReaderState(false, false, false, true)
	if !strings.Contains(sb.View("reader"), "prev/next msg") {
		t.Error("multi-message thread should advertise the [/] jump keys")
	}
	sb.SetReaderState(false, false, false, false)
	if strings.Contains(sb.View("reader"), "prev/next msg") {
		t.Error("single message should not advertise per-message jumps")
	}
}

func TestStatusBarSelectionChip(t *testing.T) {
	sb := NewStatusBar(NewStyles(testTheme()))
	sb.SetWidth(160)

	sb.SetSelectionCount(3)
	if !strings.Contains(sb.View("inbox"), "3 selected") {
		t.Error("multi-selection should surface a '3 selected' chip")
	}
	sb.SetSelectionCount(1)
	if strings.Contains(sb.View("inbox"), "selected") {
		t.Error("a single selected thread is not a multi-selection; no chip")
	}
	sb.SetSelectionCount(0)
	if strings.Contains(sb.View("inbox"), "selected") {
		t.Error("no selection should show no chip")
	}
}

func TestStatusBarFlashSeq(t *testing.T) {
	sb := NewStatusBar(NewStyles(testTheme()))
	s0 := sb.MessageSeq()
	sb.SetMessage("hello", "ok")
	if sb.MessageSeq() == s0 {
		t.Error("SetMessage should bump the flash generation")
	}
	if !sb.HasMessage() {
		t.Error("HasMessage should be true after SetMessage")
	}
	sb.ClearMessage()
	if sb.HasMessage() {
		t.Error("HasMessage should be false after ClearMessage")
	}
}
