package ui

import (
	"strings"
	"testing"
)

func TestReaderHintsAreStateAware(t *testing.T) {
	sb := NewStatusBar(NewStyles(testTheme()))
	sb.SetWidth(160)

	render := func(hasAttach, attachActive, hasEvent bool) string {
		sb.SetReaderState(hasAttach, attachActive, hasEvent)
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
