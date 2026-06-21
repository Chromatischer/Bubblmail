package views

import (
	"strings"
	"testing"
	"time"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
)

func longBody(prefix string, n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = prefix + " line"
	}
	return strings.Join(lines, "\n")
}

func threadWith(n int) *data.Thread {
	msgs := make([]*data.Message, n)
	for i := 0; i < n; i++ {
		msgs[i] = &data.Message{
			Subject: "Subject",
			From:    []data.Address{{Name: "Sender"}},
			Date:    time.Now().Add(time.Duration(i) * time.Minute),
			Body:    longBody("msg", 15),
		}
	}
	return &data.Thread{ID: "t", Subject: "Subject", Messages: msgs, LastDate: time.Now()}
}

func TestReaderJumpBetweenMessages(t *testing.T) {
	v := NewReaderView(&config.Theme{})
	v.SetSize(80, 6) // small viewport so offsets aren't clamped by max scroll
	v.SetThread(threadWith(3))

	if !v.CanJumpMessages() {
		t.Fatal("a 3-message thread should allow per-message jumps")
	}
	if len(v.msgLineOffsets) != 3 {
		t.Fatalf("expected 3 message offsets, got %d", len(v.msgLineOffsets))
	}

	v.GoToTop()
	if v.scrollY != 0 {
		t.Fatalf("GoToTop should reset scroll, got %d", v.scrollY)
	}

	v.JumpToMessage(1)
	if v.scrollY != v.msgLineOffsets[1] {
		t.Errorf("next jump: scrollY=%d, want offset[1]=%d", v.scrollY, v.msgLineOffsets[1])
	}
	v.JumpToMessage(1)
	if v.scrollY != v.msgLineOffsets[2] {
		t.Errorf("second next jump: scrollY=%d, want offset[2]=%d", v.scrollY, v.msgLineOffsets[2])
	}
	v.JumpToMessage(1) // clamps at the last message
	if v.scrollY != v.msgLineOffsets[2] {
		t.Errorf("jump past end should stay on last message, got %d", v.scrollY)
	}

	// A previous-jump while parked mid-message first snaps to that message's top.
	v.scrollY = v.msgLineOffsets[2] + 3
	v.JumpToMessage(-1)
	if v.scrollY != v.msgLineOffsets[2] {
		t.Errorf("prev from mid-message should snap to its top: got %d, want %d", v.scrollY, v.msgLineOffsets[2])
	}
	v.JumpToMessage(-1)
	if v.scrollY != v.msgLineOffsets[1] {
		t.Errorf("prev again should reach offset[1]=%d, got %d", v.msgLineOffsets[1], v.scrollY)
	}
}

func TestReaderJumpSingleMessageNoop(t *testing.T) {
	v := NewReaderView(&config.Theme{})
	v.SetSize(80, 24)
	v.SetMessage(&data.Message{Subject: "Hi", Body: "short body"})
	if v.CanJumpMessages() {
		t.Error("single-message mode should not advertise message jumps")
	}
	v.JumpToMessage(1) // must not panic
}

func TestReaderSingleModeShowsScrollPercent(t *testing.T) {
	v := NewReaderView(&config.Theme{})

	// Tall content in a short viewport overflows → indicator present.
	v.SetSize(80, 6)
	v.SetMessage(&data.Message{Subject: "Hi", Body: longBody("body", 50)})
	if !strings.Contains(v.View(), "%") {
		t.Error("overflowing single-message body should show a scroll percentage")
	}

	// Short content that fits → no indicator.
	v.SetSize(80, 40)
	v.SetMessage(&data.Message{Subject: "Hi", Body: "one short line"})
	if strings.Contains(v.View(), "%") {
		t.Error("content that fits should not show a scroll percentage")
	}
}
