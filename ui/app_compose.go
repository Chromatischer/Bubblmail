package ui

import (
	"fmt"

	"github.com/bubblmail/bubblmail/data"
	outsmtp "github.com/bubblmail/bubblmail/smtp"
	"github.com/bubblmail/bubblmail/ui/composer"
	"github.com/bubblmail/bubblmail/ui/icons"
	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) openCompose() tea.Cmd {
	from := a.senderAddress()
	a.comp.OpenNew(from)
	a.comp.SetSize(a.width, a.height-a.headerHeight()-a.statusHeight())
	return nil
}

func (a *App) openReply(msg *data.Message, replyAll bool) tea.Cmd {
	from := a.senderAddress()
	a.comp.OpenReply(from, msg, replyAll)
	a.comp.SetSize(a.width, a.height-a.headerHeight()-a.statusHeight())
	return nil
}

func (a *App) openForward(msg *data.Message) tea.Cmd {
	from := a.senderAddress()
	a.comp.OpenForward(from, msg)
	a.comp.SetSize(a.width, a.height-a.headerHeight()-a.statusHeight())
	return nil
}

func (a *App) senderAddress() data.Address {
	for _, acfg := range a.cfg.Accounts {
		if acfg.Name == a.activeAccount {
			return data.Address{Address: acfg.Username}
		}
	}
	return data.Address{}
}

func (a *App) handleComposerResult(r *composer.Result) tea.Cmd {
	switch r.Action {
	case "send":
		if r.Draft == nil {
			return nil
		}
		for i := range a.cfg.Accounts {
			if a.cfg.Accounts[i].Name == a.activeAccount {
				acfg := &a.cfg.Accounts[i]
				a.flash(fmt.Sprintf("%s Sending…", icons.Send), "info")
				cmds := []tea.Cmd{outsmtp.SendMessage(acfg, r.Draft)}
				// Append a copy to Sent if we can find the folder.
				if sent := a.findSentFolder(a.activeAccount); sent != "" {
					if client, ok := a.imapClients[a.activeAccount]; ok {
						if raw, err := outsmtp.BuildRawMessage(r.Draft); err == nil {
							cmds = append(cmds, client.AppendMessage(sent, raw, []data.Flag{data.FlagSeen}))
						}
					}
				}
				return tea.Batch(cmds...)
			}
		}
		a.flash("No account configured for sending", "err")
	case "save-draft":
		if r.Draft == nil {
			return nil
		}
		if drafts := a.findDraftsFolder(a.activeAccount); drafts != "" {
			if client, ok := a.imapClients[a.activeAccount]; ok {
				if raw, err := outsmtp.BuildRawMessage(r.Draft); err == nil {
					return client.AppendMessage(drafts, raw, []data.Flag{data.FlagDraft, data.FlagSeen})
				}
			}
		}
	case "cancel":
		// nothing
	}
	return nil
}
