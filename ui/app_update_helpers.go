package ui

import (
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/thread"
)

func (a *App) setInboxMessages(messages []*data.Message) []*data.Thread {
	a.loadedMessages = messages
	a.fetchedCount = len(messages)
	a.loadingMore = false
	a.allLoaded = false
	return a.rebuildInboxThreads(false)
}

func (a *App) appendInboxMessages(messages []*data.Message) []*data.Thread {
	a.fetchedCount += len(messages)
	a.loadedMessages = append(a.loadedMessages, messages...)
	return a.rebuildInboxThreads(true)
}

func (a *App) removeInboxMessageByUID(uid uint32) {
	cursor := a.inboxView.CursorPos()
	a.loadedMessages = removeByUID(a.loadedMessages, uid)
	a.fetchedCount = len(a.loadedMessages)
	threads := a.rebuildInboxThreads(true)
	if cursor < len(threads) {
		a.inboxView.SetCursor(cursor)
	}
	if a.viewID == ViewReader {
		a.viewID = ViewInbox
	}
}

func (a *App) rebuildInboxThreads(preserveCursor bool) []*data.Thread {
	threads := thread.BuildThreads(a.visibleMessages())
	if preserveCursor {
		a.inboxView.AppendThreads(threads)
	} else {
		a.inboxView.SetThreads(threads)
	}
	a.applyQuickMenuState()
	if a.unreadOnly {
		a.header.SetUnreadFilter(true, len(threads))
	}
	return threads
}
