package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type uiState struct {
	CollapsedFolders []string `json:"collapsed_folders"`
	TipSeen          bool     `json:"tip_seen"` // first-run hint has been shown
}

func uiStatePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bubblmail", "ui_state.json"), nil
}

func loadUIState() (*uiState, error) {
	path, err := uiStatePath()
	if err != nil {
		return &uiState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &uiState{}, nil
		}
		return &uiState{}, err
	}
	var s uiState
	if err := json.Unmarshal(data, &s); err != nil {
		return &uiState{}, err
	}
	return &s, nil
}

func saveUIState(s *uiState) error {
	path, err := uiStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
