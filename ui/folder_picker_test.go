package ui

import (
	"strings"
	"testing"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/util"
)

func testTheme() *config.Theme {
	return &config.Theme{
		Background: "#1E1E2E", Surface: "#313244", SurfaceAlt: "#45475A",
		Overlay: "#585B70", Text: "#CDD6F4", TextMuted: "#A6ADC8",
		TextFaint: "#6C7086", Accent: "#89B4FA", Selected: "#89B4FA",
		Border: "#45475A",
	}
}

func TestFolderTreePrefixes(t *testing.T) {
	mk := func(depth int) *data.Folder { return &data.Folder{Depth: depth} }
	// Triage > {Green, Orange, Red}, Private > {Rowing, Work}, AliExpress,
	// Archive > {2019 > {11, 09}, 2018 > {11}}
	folders := []*data.Folder{
		mk(0),               // Triage
		mk(1), mk(1), mk(1), // Green, Orange, Red
		mk(0),        // Private
		mk(1), mk(1), // Rowing, Work
		mk(0),        // AliExpress
		mk(0),        // Archive
		mk(1),        // 2019
		mk(2), mk(2), // 11, 09
		mk(1), // 2018
		mk(2), // 11
	}
	want := []string{
		"",     // Triage
		"├ ",   // Green
		"├ ",   // Orange
		"└ ",   // Red
		"",     // Private
		"├ ",   // Rowing
		"└ ",   // Work
		"",     // AliExpress
		"",     // Archive
		"├ ",   // 2019
		"│ ├ ", // 11
		"│ └ ", // 09
		"└ ",   // 2018
		"  └ ", // 11
	}
	got := folderTreePrefixes(folders)
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFolderPickerViewWidthInvariant(t *testing.T) {
	mk := func(name, disp string, depth int) *data.Folder {
		return &data.Folder{Name: name, DisplayName: disp, Depth: depth}
	}
	// Enough nested folders to force scrolling within the height.
	folders := []*data.Folder{
		mk("Triage", "Triage", 0),
		mk("Triage/Green", "Green", 1),
		mk("Triage/Orange", "Orange", 1),
		mk("Triage/Red", "Red", 1),
		mk("Archive", "Archive", 0),
		mk("Archive/2019", "2019", 1),
		mk("Archive/2019/11", "11", 2),
		mk("Archive/2019/09", "09", 2),
		mk("Archive/2018", "2018", 1),
		mk("Archive/2018/11", "11", 2),
		mk("AliExpress", "AliExpress", 0),
		mk("Work", "Work", 0),
	}

	check := func(t *testing.T, query string) {
		f := NewFolderPickerOverlay(NewStyles(testTheme()))
		f.SetSize(60, 16)
		f.Open(folders, "", nil)
		for _, r := range query {
			f.HandleKey(string(r))
		}
		out := f.View()
		var width = -1
		for i, line := range strings.Split(out, "\n") {
			w := util.VisibleWidth(line)
			if width == -1 {
				width = w
			} else if w != width {
				t.Errorf("query %q line %d width = %d, want %d\n%q", query, i, w, width, line)
			}
		}
	}
	check(t, "")      // tree mode + scrollbar
	check(t, "arc")   // filter mode + highlight
	check(t, "zzzzz") // no matches
}

func TestFolderPickerCollapseMirror(t *testing.T) {
	mk := func(name, disp string, depth int) *data.Folder {
		return &data.Folder{Name: name, DisplayName: disp, Depth: depth, Delimiter: "/", Attributes: nil}
	}
	folders := []*data.Folder{
		mk("Triage", "Triage", 0),
		mk("Archive", "Archive", 0),
		mk("Archive/2019", "2019", 1),
		mk("Archive/2019/11", "11", 2),
		mk("Archive/2018", "2018", 1),
	}
	names := func(fs []*data.Folder) []string {
		out := make([]string, len(fs))
		for i, f := range fs {
			out[i] = f.Name
		}
		return out
	}

	f := NewFolderPickerOverlay(NewStyles(testTheme()))
	f.Open(folders, "", map[string]bool{"Archive": true})

	// Browse mode: Archive's descendants are hidden, Archive itself remains.
	got := names(f.filtered)
	want := []string{"Triage", "Archive"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("browse filtered = %v, want %v", got, want)
	}

	// Filtering reaches into the collapsed subtree.
	for _, r := range "2019" {
		f.HandleKey(string(r))
	}
	got = names(f.filtered)
	if len(got) == 0 || got[0] != "Archive/2019" {
		t.Errorf("filtered for %q = %v, want it to include Archive/2019", "2019", got)
	}

	// Clearing the query returns to the collapsed browse view.
	f.HandleKey("ctrl+u")
	if strings.Join(names(f.filtered), ",") != strings.Join(want, ",") {
		t.Errorf("after clear filtered = %v, want %v", names(f.filtered), want)
	}
}

func TestFindMatchRange(t *testing.T) {
	cases := []struct {
		s, q             string
		wantStart, wantN int // wantN = -1 means no match
	}{
		{"Archive", "arc", 0, 3},
		{"INBOX/Archived", "archived", 6, 8},
		{"Triage", "x", -1, 0},
		{"Triage", "", -1, 0},
		{"Café", "café", 0, 5}, // é is 2 bytes
	}
	for _, c := range cases {
		start, end := findMatchRange(c.s, c.q)
		if c.wantN == 0 && c.q == "" || c.wantStart == -1 {
			if start != -1 {
				t.Errorf("findMatchRange(%q,%q) = %d,%d; want no match", c.s, c.q, start, end)
			}
			continue
		}
		if start != c.wantStart || end != c.wantStart+c.wantN {
			t.Errorf("findMatchRange(%q,%q) = %d,%d; want %d,%d", c.s, c.q, start, end, c.wantStart, c.wantStart+c.wantN)
		}
		if start >= 0 && !strings.EqualFold(c.s[start:end], c.q) {
			t.Errorf("matched substring %q != query %q (fold)", c.s[start:end], c.q)
		}
	}
}
