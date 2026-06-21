package ui

import (
	"strings"
	"testing"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/util"
)

// TestSearchRowWidthInvariant guards the segmented highlight + scrollbar render
// against width drift: every line of the overlay must be the same visible width.
func TestSearchRowWidthInvariant(t *testing.T) {
	s := NewSearchOverlay(NewStyles(testTheme()))
	s.SetSize(72, 14)
	s.Open()
	for _, r := range "report" {
		s.HandleKey(string(r))
	}
	var msgs []*data.Message
	for i := 0; i < 20; i++ { // more than fits → scrollbar shown
		msgs = append(msgs, &data.Message{
			Subject: "Quarterly report number twelve and more text",
			From:    []data.Address{{Name: "Sender Person"}},
		})
	}
	s.SetResults(msgs)

	width := -1
	for i, line := range strings.Split(s.View(), "\n") {
		w := util.VisibleWidth(line)
		if width == -1 {
			width = w
		} else if w != width {
			t.Errorf("line %d width = %d, want %d", i, w, width)
		}
	}
}
