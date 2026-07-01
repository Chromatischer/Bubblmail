package ui

import (
	"github.com/bubblmail/bubblmail/data"
	tea "github.com/charmbracelet/bubbletea"
)

type mailClient interface {
	FetchFolders() tea.Cmd
	FetchMessages(folder string, limit int) tea.Cmd
	FetchMoreMessages(folder string, skip, limit int) tea.Cmd
	FetchBody(folder string, uid uint32, msgID int64) tea.Cmd
	SetFlag(folder string, uid uint32, flag data.Flag, set bool) tea.Cmd
	MoveMessage(sourceFolder string, uid uint32, destFolder string) tea.Cmd
	MoveMessageSync(sourceFolder string, uid uint32, destFolder string) (uint32, error)
	MoveToTrash(sourceFolder string, uid uint32, trashFolder string) tea.Cmd
	CreateFolder(name string) tea.Cmd
	AppendMessage(folder string, raw []byte, flags []data.Flag) tea.Cmd
	SearchIMAP(folder, query string) tea.Cmd
}
