package ui

import (
	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
)

// demoInitData holds pre-canned folders and messages for demo mode.
type demoInitData struct {
	account  string
	folders  []*data.Folder
	messages []*data.Message
}

// NewDemoApp creates an App pre-loaded with demo data, bypassing IMAP entirely.
// store should already be seeded with the same messages/categories/events via
// the demo fixtures package before calling this function.
func NewDemoApp(store *cache.Store, account string, folders []*data.Folder, messages []*data.Message) *App {
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{
		{Name: account, Username: account},
	}
	// Disable background workers that would try to reach external services.
	cfg.Embeddings.PrefetchBodies = false
	cfg.Classification.Enabled = false
	// Keep the default categories so the smart-folder sidebar is visible.
	cfg.Classification.Categories = config.DefaultCategories

	app := NewApp(cfg, store)
	app.demoMode = true
	app.demoData = &demoInitData{
		account:  account,
		folders:  folders,
		messages: messages,
	}
	return app
}
