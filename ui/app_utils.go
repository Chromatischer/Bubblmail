package ui

import (
	"fmt"
	"os"

	"github.com/bubblmail/bubblmail/data"
	tea "github.com/charmbracelet/bubbletea"
)

// fetchSmartFolder loads messages for a smart folder category into the inbox view.
func (a *App) fetchSmartFolder(category string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := a.store.GetMessagesByCategory(a.activeAccount, category)
		if err != nil {
			return smartFolderMsg{category: category, err: err}
		}
		return smartFolderMsg{category: category, messages: msgs}
	}
}

// smartFolderMsg delivers messages for a smart folder.
type smartFolderMsg struct {
	category string
	messages []*data.Message
	err      error
}

// refreshSmartCounts reloads category counts from the database for the active account.
func (a *App) refreshSmartCounts() {
	counts, err := a.store.GetCategoryCounts(a.activeAccount)
	if err == nil {
		a.smartCounts = counts
		a.sidebar.SetSmartCounts(a.smartCounts, a.cfg.Classification.Categories)
	}
}

func (a *App) debugLog(format string, args ...any) {
	if a.debugFile == nil {
		f, err := os.OpenFile("/tmp/bubblmail-debug.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		a.debugFile = f
	}
	fmt.Fprintf(a.debugFile, format+"\n", args...)
}
