package cmd

import (
	"fmt"
	"strings"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/util"
)

const (
	formatCompact  = "compact"
	formatDetailed = "detailed"
)

type listFormat struct {
	name string
}

func (f listFormat) isCompact() bool {
	return f.name == formatCompact
}

func (f listFormat) isDetailed() bool {
	return f.name == formatDetailed
}

func normalizeFormat(name string) listFormat {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == formatCompact {
		return listFormat{name: formatCompact}
	}
	if name == formatDetailed || name == "detail" || name == "full" {
		return listFormat{name: formatDetailed}
	}
	return listFormat{name: name}
}

func formatMessages(messages []*data.Message, format listFormat) (string, error) {
	if format.isCompact() {
		return formatMessagesCompact(messages), nil
	}
	if format.isDetailed() {
		return formatMessagesDetailed(messages), nil
	}
	return "", fmt.Errorf("unknown format %q", format.name)
}

func formatMessagesCompact(messages []*data.Message) string {
	const (
		dateW = 10
		fromW = 24
	)
	lines := make([]string, 0, len(messages))
	for _, m := range messages {
		date := util.FormatDate(m.Date)
		from := util.TruncateText(util.SingleLine(m.FromString()), fromW)
		subject := util.SingleLine(m.Subject)
		if subject == "" {
			subject = "(no subject)"
		}
		line := fmt.Sprintf("%s  %s  %s",
			util.PadRight(date, dateW),
			util.PadRight(from, fromW),
			subject,
		)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func formatMessagesDetailed(messages []*data.Message) string {
	const (
		dateW    = 16
		accountW = 14
		folderW  = 18
		fromW    = 24
		flagsW   = 8
	)
	lines := make([]string, 0, len(messages))
	for _, m := range messages {
		date := util.FormatDateTime(m.Date)
		account := util.TruncateText(m.AccountName, accountW)
		folder := util.TruncateText(m.FolderName, folderW)
		from := util.TruncateText(util.SingleLine(m.FromString()), fromW)
		subject := util.SingleLine(m.Subject)
		if subject == "" {
			subject = "(no subject)"
		}
		flags := renderFlags(m)
		line := fmt.Sprintf("%s  %s  %s  %s  %s  %s",
			util.PadRight(date, dateW),
			util.PadRight(account, accountW),
			util.PadRight(folder, folderW),
			util.PadRight(from, fromW),
			util.PadRight(flags, flagsW),
			subject,
		)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func renderFlags(m *data.Message) string {
	flags := make([]string, 0, 2)
	if !m.IsRead() {
		flags = append(flags, "unread")
	}
	if m.IsStarred() {
		flags = append(flags, "star")
	}
	if len(flags) == 0 {
		return "-"
	}
	return strings.Join(flags, ",")
}
