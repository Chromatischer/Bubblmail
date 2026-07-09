package smtp

import (
	"fmt"
	"net/mail"
	"strings"

	"github.com/bubblmail/bubblmail/data"
)

// ParseAddresses parses one or more address-list strings into Bubblmail
// addresses. Each value may itself contain comma-separated RFC 5322 addresses.
func ParseAddresses(values []string) ([]data.Address, error) {
	joined := strings.TrimSpace(strings.Join(nonEmptyStrings(values), ","))
	if joined == "" {
		return nil, nil
	}

	parsed, err := mail.ParseAddressList(joined)
	if err != nil {
		return nil, err
	}
	out := make([]data.Address, 0, len(parsed))
	for _, addr := range parsed {
		out = append(out, data.Address{Name: addr.Name, Address: addr.Address})
	}
	return out, nil
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// FormatRecipientList returns a concise human-readable recipient list.
func FormatRecipientList(addrs []data.Address) string {
	parts := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		switch {
		case addr.Name != "" && addr.Address != "":
			parts = append(parts, fmt.Sprintf("%s <%s>", addr.Name, addr.Address))
		case addr.Address != "":
			parts = append(parts, addr.Address)
		case addr.Name != "":
			parts = append(parts, addr.Name)
		}
	}
	return strings.Join(parts, ", ")
}
