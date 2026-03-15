package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	if cfg.General.SyncIntervalMinutes != 5 {
		t.Errorf("SyncIntervalMinutes = %d, want 5", cfg.General.SyncIntervalMinutes)
	}

	if cfg.General.PageSize != 50 {
		t.Errorf("PageSize = %d, want 50", cfg.General.PageSize)
	}

	if cfg.Theme.Mode != "auto" {
		t.Errorf("Theme.Mode = %q, want auto", cfg.Theme.Mode)
	}

	if cfg.Theme.Accent != "#7C3AED" {
		t.Errorf("Theme.Accent = %q, want #7C3AED", cfg.Theme.Accent)
	}

	if cfg.Embeddings.Model != "openai/text-embedding-3-small" {
		t.Errorf("Embeddings.Model = %q, want openai/text-embedding-3-small", cfg.Embeddings.Model)
	}

	if cfg.Embeddings.BatchSize != 16 {
		t.Errorf("Embeddings.BatchSize = %d, want 16", cfg.Embeddings.BatchSize)
	}

	if cfg.Embeddings.MaxCandidates != 5000 {
		t.Errorf("Embeddings.MaxCandidates = %d, want 5000", cfg.Embeddings.MaxCandidates)
	}

	if len(cfg.Classification.Categories) == 0 {
		t.Error("Classification.Categories should not be empty")
	}

	if !cfg.Classification.Enabled {
		t.Error("Classification.Enabled should be true by default")
	}
}

func TestLoad_AppliesDefaultsToPartialConfig(t *testing.T) {
	content := `
[general]
sync_interval_minutes = 10

[theme]
accent = "#FF0000"

[embeddings]
batch_size = 32
`

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "bubblmail", "config.toml")

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg := DefaultConfig()

	cfg.General.SyncIntervalMinutes = 10
	cfg.Theme.Accent = "#FF0000"
	cfg.Embeddings.BatchSize = 32

	if cfg.General.SyncIntervalMinutes != 10 {
		t.Errorf("SyncIntervalMinutes = %d, want 10", cfg.General.SyncIntervalMinutes)
	}

	if cfg.General.PageSize != 50 {
		t.Errorf("PageSize = %d, want 50 (default)", cfg.General.PageSize)
	}

	if cfg.Theme.Accent != "#FF0000" {
		t.Errorf("Theme.Accent = %q, want #FF0000", cfg.Theme.Accent)
	}

	if cfg.Theme.Mode != "auto" {
		t.Errorf("Theme.Mode = %q, want auto (default)", cfg.Theme.Mode)
	}

	if cfg.Embeddings.BatchSize != 32 {
		t.Errorf("Embeddings.BatchSize = %d, want 32", cfg.Embeddings.BatchSize)
	}

	if cfg.Embeddings.MaxCandidates != 5000 {
		t.Errorf("Embeddings.MaxCandidates = %d, want 5000 (default)", cfg.Embeddings.MaxCandidates)
	}
}

func TestLoad_ReturnsDefaultsWhenNoConfigFile(t *testing.T) {
	configDir := t.TempDir()

	oldUserConfigDir := userConfigDirFunc
	defer func() { userConfigDirFunc = oldUserConfigDir }()
	userConfigDirFunc = func() (string, error) {
		return configDir, nil
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg == nil {
		t.Fatal("Load() returned nil")
	}

	if cfg.General.SyncIntervalMinutes != 5 {
		t.Errorf("SyncIntervalMinutes = %d, want 5 (default)", cfg.General.SyncIntervalMinutes)
	}
}

func TestResolvePassword_Plaintext(t *testing.T) {
	account := &AccountConfig{
		Password: "plaintext123",
	}

	got, err := account.ResolvePassword()
	if err != nil {
		t.Fatalf("ResolvePassword() error = %v", err)
	}

	if got != "plaintext123" {
		t.Errorf("ResolvePassword() = %q, want plaintext123", got)
	}
}

func TestResolvePassword_Command(t *testing.T) {
	account := &AccountConfig{
		PasswordCmd: "echo password",
	}

	got, err := account.ResolvePassword()
	if err != nil {
		t.Fatalf("ResolvePassword() error = %v", err)
	}

	if got != "password" {
		t.Errorf("ResolvePassword() = %q, want password", got)
	}
}

func TestResolvePassword_NoPasswordOrCommand(t *testing.T) {
	account := &AccountConfig{
		Password:    "",
		PasswordCmd: "",
	}

	got, err := account.ResolvePassword()
	if err != nil {
		t.Fatalf("ResolvePassword() error = %v", err)
	}

	if got != "" {
		t.Errorf("ResolvePassword() = %q, want empty string", got)
	}
}

func TestResolveAPIKey_ConfigKey(t *testing.T) {
	emb := &EmbeddingsConfig{
		APIKey: "config-key-123",
	}

	got, err := emb.ResolveAPIKey()
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v", err)
	}

	if got != "config-key-123" {
		t.Errorf("ResolveAPIKey() = %q, want config-key-123", got)
	}
}

func TestResolveAPIKey_Command(t *testing.T) {
	emb := &EmbeddingsConfig{
		APIKeyCmd: "echo api-key",
	}

	got, err := emb.ResolveAPIKey()
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v", err)
	}

	if got != "api-key" {
		t.Errorf("ResolveAPIKey() = %q, want api-key", got)
	}
}

func TestResolveAPIKey_EnvironmentFallback(t *testing.T) {
	oldEnv := os.Getenv("OPENROUTER_API_KEY")
	defer os.Setenv("OPENROUTER_API_KEY", oldEnv)

	os.Setenv("OPENROUTER_API_KEY", "env-api-key-456")

	emb := &EmbeddingsConfig{
		APIKey:    "",
		APIKeyCmd: "",
	}

	got, err := emb.ResolveAPIKey()
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v", err)
	}

	if got != "env-api-key-456" {
		t.Errorf("ResolveAPIKey() = %q, want env-api-key-456", got)
	}
}

func TestResolveAPIKey_FallbackOrder(t *testing.T) {
	oldEnv := os.Getenv("OPENROUTER_API_KEY")
	defer os.Setenv("OPENROUTER_API_KEY", oldEnv)

	tests := []struct {
		name     string
		apiKey   string
		apiCmd   string
		envKey   string
		expected string
	}{
		{
			name:     "config key takes precedence",
			apiKey:   "config-key",
			apiCmd:   "echo cmd-key",
			envKey:   "env-key",
			expected: "config-key",
		},
		{
			name:     "environment used when no config or command",
			apiKey:   "",
			apiCmd:   "",
			envKey:   "env-key",
			expected: "env-key",
		},
		{
			name:     "empty when nothing set",
			apiKey:   "",
			apiCmd:   "",
			envKey:   "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("OPENROUTER_API_KEY", tt.envKey)

			emb := &EmbeddingsConfig{
				APIKey:    tt.apiKey,
				APIKeyCmd: tt.apiCmd,
			}

			got, err := emb.ResolveAPIKey()
			if err != nil {
				t.Fatalf("ResolveAPIKey() error = %v", err)
			}

			if got != tt.expected {
				t.Errorf("ResolveAPIKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

type userConfigDirFuncType func() (string, error)

var userConfigDirFunc userConfigDirFuncType = func() (string, error) {
	return os.UserConfigDir()
}
