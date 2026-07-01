package ui

import (
	"errors"
	"fmt"
	"time"

	imaplib "github.com/bubblmail/bubblmail/imap"
	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) handleMessageBodyResult(msg imaplib.MessageBodyMsg) tea.Cmd {
	if msg.Err != nil {
		if msg.MsgID > 0 {
			a.prefetchSkip[msg.MsgID] = true
		}
		if errors.Is(msg.Err, imaplib.ErrMessageNotFound) {
			if msg.MsgID > 0 {
				_ = a.store.UpsertBody(msg.MsgID, "", "")
			}
		} else {
			details := fmt.Sprintf("Body fetch error (%s %s uid=%d id=%d): %s", msg.Account, msg.Folder, msg.UID, msg.MsgID, msg.Err.Error())
			a.flash(details, "err")
		}
		return nil
	}

	if msg.MsgID > 0 {
		if err := a.store.UpsertBody(msg.MsgID, msg.Text, msg.HTML); err != nil {
			a.flash("Cache error: "+err.Error(), "err")
		}
		if m := a.findMessageByID(msg.MsgID); m != nil {
			m.Attachments = msg.Attachments
		}
	}
	if a.viewID == ViewReader {
		a.readerView.UpdateMessageBody(msg.Folder, msg.UID, msg.Text, msg.HTML, msg.Attachments)
	}

	if msg.MsgID > 0 && a.embQueue != nil && msg.Text != "" {
		if m := a.findMessageByID(msg.MsgID); m != nil {
			cmd := a.embedMessage(m, msg.Text)
			if a.cfg.Embeddings.FolderSampleLimit > 0 {
				return tea.Batch(cmd, func() tea.Msg {
					_ = a.store.BuildFolderEmbeddings(m.AccountName, a.cfg.Embeddings.Model, a.cfg.Embeddings.FolderSampleLimit)
					return nil
				})
			}
			return cmd
		}
	}

	if msg.MsgID <= 0 || a.viewID != ViewReader {
		return nil
	}
	m := a.findMessageByID(msg.MsgID)
	if m == nil {
		return nil
	}
	if current := a.readerView.CurrentMessage(); current != nil && current.ID == msg.MsgID {
		return a.loadSuggestedEvent(m)
	}
	if t := a.readerView.CurrentThread(); t != nil {
		if latest := t.Latest(); latest != nil && latest.ID == msg.MsgID {
			return a.loadSuggestedEvent(latest)
		}
	}
	return nil
}

func (a *App) handleClassifyReady(msg classifyResultReadyMsg) tea.Cmd {
	if a.classifyQueue == nil || !msg.ok {
		return nil
	}
	changed := a.applyClassifyResult(msg.result)
	for {
		select {
		case r := <-a.classifyResults:
			if a.applyClassifyResult(r) {
				changed = true
			}
		default:
			cmds := []tea.Cmd{a.waitForClassifyResult()}
			if changed {
				a.refreshSmartCounts()
				if a.viewID == ViewSmartFolder && a.smartFolder != "" {
					cmds = append(cmds, a.fetchSmartFolder(a.smartFolder))
				}
			}
			return tea.Batch(cmds...)
		}
	}
}

func (a *App) handleSuggestReady(msg suggestResultReadyMsg) tea.Cmd {
	if a.suggestQueue == nil || !msg.ok {
		return nil
	}
	a.applySuggestResult(msg.result)
	for {
		select {
		case r := <-a.suggestResults:
			a.applySuggestResult(r)
		default:
			return a.waitForSuggestResult()
		}
	}
}

func (a *App) embeddingPrefetchInterval() time.Duration {
	interval := time.Duration(a.cfg.Embeddings.PrefetchIntervalSeconds) * time.Second
	if interval <= 0 {
		return 2 * time.Second
	}
	return interval
}
