package ui

import (
	"context"
	"strings"
	"time"

	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/embeddings"
	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) toggleStar() tea.Cmd {
	threads := a.inboxView.SelectedThreads()
	if len(threads) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, t := range threads {
		msg := t.Latest()
		if msg == nil {
			continue
		}
		starred := msg.IsStarred()
		newFlags := toggleFlag(msg.Flags, data.FlagFlagged, !starred)
		_ = a.store.SetFlags(msg.AccountName, msg.FolderName, msg.UID, newFlags)
		msg.Flags = newFlags
		// Recompute thread aggregate so the inbox re-renders immediately.
		t.Starred = false
		for _, m := range t.Messages {
			if m.IsStarred() {
				t.Starred = true
				break
			}
		}
		client, ok := a.imapClients[msg.AccountName]
		if ok {
			cmds = append(cmds, client.SetFlag(msg.FolderName, msg.UID, data.FlagFlagged, !starred))
		}
	}
	a.inboxView.ClearSelection()
	return tea.Batch(cmds...)
}

func (a *App) toggleRead() tea.Cmd {
	threads := a.inboxView.SelectedThreads()
	if len(threads) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, t := range threads {
		msg := t.Latest()
		if msg == nil {
			continue
		}
		read := msg.IsRead()
		newFlags := toggleFlag(msg.Flags, data.FlagSeen, !read)
		_ = a.store.SetFlags(msg.AccountName, msg.FolderName, msg.UID, newFlags)
		msg.Flags = newFlags
		// Recompute thread aggregate so the inbox re-renders immediately.
		t.HasUnread = false
		for _, m := range t.Messages {
			if !m.IsRead() {
				t.HasUnread = true
				break
			}
		}
		client, ok := a.imapClients[msg.AccountName]
		if ok {
			cmds = append(cmds, client.SetFlag(msg.FolderName, msg.UID, data.FlagSeen, !read))
		}
	}
	a.inboxView.ClearSelection()
	return tea.Batch(cmds...)
}

func (a *App) quickMoveMessage() tea.Cmd {
	if a.viewID != ViewInbox && a.viewID != ViewSmartFolder {
		return nil
	}
	threads := a.inboxView.SelectedThreads()
	if len(threads) == 0 {
		return nil
	}
	if a.embClient == nil {
		// No embeddings — open folder picker; selection stays active so
		// moveMessageToFolder will act on all selected threads when confirmed.
		a.openFolderPicker()
		return nil
	}
	var cmds []tea.Cmd
	for _, t := range threads {
		msg := t.Latest()
		if msg == nil {
			continue
		}
		if cmd := a.quickMoveSingleMessage(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	a.inboxView.ClearSelection()
	return tea.Batch(cmds...)
}

func (a *App) quickMoveSingleMessage(msg *data.Message) tea.Cmd {
	account := a.activeAccount
	folder := a.activeFolder
	uid := msg.UID
	msgID := msg.ID

	return func() tea.Msg {
		model := a.cfg.Embeddings.Model
		if model == "" {
			model = "openai/text-embedding-3-small"
		}
		vec, norm, err := a.store.GetEmbedding(msgID, model)
		if err != nil {
			return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, err: err}
		}
		if vec == nil {
			bodyText, _, err := a.store.GetBody(msgID)
			if err != nil {
				return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, err: err}
			}
			content := strings.TrimSpace(msg.Subject + "\n" + bodyText)
			if bodyText == "" && msg.Snippet != "" {
				content = strings.TrimSpace(msg.Subject + "\n" + msg.Snippet)
			}
			maxChars := a.cfg.Embeddings.MaxContentChars
			if maxChars <= 0 {
				maxChars = 8000
			}
			if len(content) > maxChars {
				content = content[:maxChars]
			}
			if content != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				vecs, err := a.embClient.EmbedTexts(ctx, []string{content})
				cancel()
				if err == nil && len(vecs) > 0 {
					vec = vecs[0]
					norm = embeddings.VectorNorm(vec)
					hash := embeddings.HashContent(content)
					_ = a.store.SaveEmbedding(msgID, model, vec, norm, hash)
					_ = a.store.BuildFolderEmbeddings(account, model, a.cfg.Embeddings.FolderSampleLimit)
				}
			}
		}
		if vec == nil || norm == 0 {
			return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, dest: ""}
		}
		folders, vectors, norms, _, err := a.store.ListFolderEmbeddings(account, model)
		if err != nil {
			return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, err: err}
		}
		bestScore := float32(0)
		bestFolder := ""
		for i, f := range folders {
			if f == nil {
				continue
			}
			if !f.IsSelectable() {
				continue
			}
			if f.Name == folder {
				continue
			}
			if isSystemFolder(f.DisplayName, f.Name) {
				continue
			}
			score := embeddings.CosineSimilarity(vec, norm, vectors[i], norms[i])
			if score > bestScore {
				bestScore = score
				bestFolder = f.Name
			}
		}
		threshold := float32(a.cfg.Embeddings.AutoMoveThreshold)
		if threshold <= 0 {
			threshold = 0.27
		}
		if bestFolder == "" || bestScore < threshold {
			return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, dest: "", score: bestScore}
		}
		return quickMoveResultMsg{msgID: msgID, account: account, folder: folder, uid: uid, dest: bestFolder, score: bestScore}
	}
}

// prefetchMoveHint computes the AI folder suggestion for the currently selected
// thread and returns a moveHintMsg so the status bar can show the destination
// before the user confirms the move.
func (a *App) prefetchMoveHint() tea.Cmd {
	if a.embClient == nil {
		return nil
	}
	threads := a.inboxView.SelectedThreads()
	if len(threads) == 0 {
		t := a.inboxView.SelectedThread()
		if t == nil {
			return nil
		}
		threads = []*data.Thread{t}
	}
	msg := threads[0].Latest()
	if msg == nil {
		return nil
	}
	account := a.activeAccount
	folder := a.activeFolder
	msgID := msg.ID
	uid := msg.UID

	return func() tea.Msg {
		model := a.cfg.Embeddings.Model
		if model == "" {
			model = "openai/text-embedding-3-small"
		}
		vec, norm, err := a.store.GetEmbedding(msgID, model)
		if err != nil || vec == nil || norm == 0 {
			return moveHintMsg{}
		}
		_ = uid
		folders, vectors, norms, _, err := a.store.ListFolderEmbeddings(account, model)
		if err != nil {
			return moveHintMsg{}
		}
		bestScore := float32(0)
		bestFolder := ""
		for i, f := range folders {
			if f == nil {
				continue
			}
			if !f.IsSelectable() {
				continue
			}
			if f.Name == folder {
				continue
			}
			if isSystemFolder(f.DisplayName, f.Name) {
				continue
			}
			score := embeddings.CosineSimilarity(vec, norm, vectors[i], norms[i])
			if score > bestScore {
				bestScore = score
				bestFolder = f.Name
			}
		}
		threshold := float32(a.cfg.Embeddings.AutoMoveThreshold)
		if threshold <= 0 {
			threshold = 0.27
		}
		if bestFolder == "" || bestScore < threshold {
			return moveHintMsg{}
		}
		return moveHintMsg{dest: bestFolder}
	}
}

func isSystemFolder(displayName, name string) bool {
	label := strings.ToLower(strings.TrimSpace(displayName))
	full := strings.ToLower(strings.TrimSpace(name))
	if full == "inbox" {
		return true
	}
	system := []string{"inbox", "trash", "deleted", "deleted items", "deleted messages", "bin", "sent", "sent items", "sent mail", "drafts", "archive", "all mail", "junk", "spam"}
	for _, s := range system {
		if label == s || full == s {
			return true
		}
	}
	return false
}

func (a *App) closeQuickMenu() {
	if a.quickMenu == nil {
		return
	}
	a.quickMenu.side = quickMenuNone
	a.quickMenu.step = 0
	a.statusbar.SetMoveHint("")
	a.applyQuickMenuState()
}

func (a *App) openQuickMenu(side quickMenuSide) tea.Cmd {
	if a.quickMenu == nil {
		a.quickMenu = &quickMenuState{}
	}
	if a.quickMenu.side != side {
		// Switching sides — reset to step 0
		a.quickMenu.side = side
		a.quickMenu.step = 0
		a.applyQuickMenuState()
		return nil
	}
	// Same side — advance step. If already on the last step, execute it.
	const maxSteps = 2
	if a.quickMenu.step >= maxSteps-1 {
		return a.handleQuickMenuEnter()
	}
	a.quickMenu.step++
	a.applyQuickMenuState()
	// Prefetch the move suggestion when landing on the move step (left side, step 1).
	if a.quickMenu.side == quickMenuLeft && a.quickMenu.step == 1 {
		return a.prefetchMoveHint()
	}
	return nil
}

func removeByUID(msgs []*data.Message, uid uint32) []*data.Message {
	result := msgs[:0]
	for _, m := range msgs {
		if m.UID != uid {
			result = append(result, m)
		}
	}
	return result
}

func toggleFlag(flags []data.Flag, f data.Flag, add bool) []data.Flag {
	var result []data.Flag
	for _, existing := range flags {
		if existing != f {
			result = append(result, existing)
		}
	}
	if add {
		result = append(result, f)
	}
	return result
}
