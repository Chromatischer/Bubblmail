package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// stateFileName is where UI state that is not configuration lives. It sits
// beside config.toml but is written by the app rather than by the user, so it
// is deliberately a separate file: rewriting config.toml on every fold would
// reformat the user's comments away.
const stateFileName = "state.json"

// UIState is the part of the interface that should survive a restart but is
// not worth asking the user to configure.
type UIState struct {
	// CollapsedFolders holds the folders whose children are hidden, keyed by
	// account and mailbox path (see FolderKey).
	CollapsedFolders []string `json:"collapsed_folders,omitempty"`
	// SidebarHidden records whether the user dismissed the sidebar.
	SidebarHidden bool `json:"sidebar_hidden,omitempty"`
}

// FolderKey builds the identifier used for a folder in the persisted state.
// Account and mailbox are joined with a tab, which no IMAP mailbox name may
// contain, so the two halves can never be confused for one another.
func FolderKey(account, mailbox string) string {
	return account + "\t" + mailbox
}

func statePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bubblmail", stateFileName), nil
}

// LoadState reads the persisted UI state. A missing or unreadable file is not
// an error: state is a convenience, and the app must start either way.
func LoadState() *UIState {
	st := &UIState{}
	path, err := statePath()
	if err != nil {
		return st
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, st)
	return st
}

// Save writes the UI state, creating the directory if needed. It writes
// through a temporary file so an interrupted write cannot leave a truncated
// state file behind.
func (s *UIState) Save() error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	sort.Strings(s.CollapsedFolders)
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
