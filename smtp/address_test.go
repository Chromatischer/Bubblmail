package smtp

import "testing"

func TestParseAddressesAcceptsRepeatedAndCommaSeparatedValues(t *testing.T) {
	got, err := ParseAddresses([]string{
		`Alice Example <alice@example.com>, bob@example.com`,
		`Carol <carol@example.com>`,
	})
	if err != nil {
		t.Fatalf("ParseAddresses() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ParseAddresses() len = %d, want 3: %#v", len(got), got)
	}
	if got[0].Name != "Alice Example" || got[0].Address != "alice@example.com" {
		t.Fatalf("first address = %#v", got[0])
	}
	if got[1].Name != "" || got[1].Address != "bob@example.com" {
		t.Fatalf("second address = %#v", got[1])
	}
	if got[2].Name != "Carol" || got[2].Address != "carol@example.com" {
		t.Fatalf("third address = %#v", got[2])
	}
}

func TestParseAddressesRejectsInvalidAddress(t *testing.T) {
	if _, err := ParseAddresses([]string{"not an address"}); err == nil {
		t.Fatal("ParseAddresses() error = nil, want invalid address error")
	}
}

func TestFormatRecipientList(t *testing.T) {
	addrs, err := ParseAddresses([]string{`Alice <alice@example.com>, bob@example.com`})
	if err != nil {
		t.Fatalf("ParseAddresses() error = %v", err)
	}
	got := FormatRecipientList(addrs)
	want := "Alice <alice@example.com>, bob@example.com"
	if got != want {
		t.Fatalf("FormatRecipientList() = %q, want %q", got, want)
	}
}
