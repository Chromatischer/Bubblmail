package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the top-level configuration.
type Config struct {
	General    GeneralConfig    `toml:"general"`
	Theme      ThemeConfig      `toml:"theme"`
	Accounts   []AccountConfig  `toml:"account"`
	Cache      CacheConfig      `toml:"cache"`
	Embeddings EmbeddingsConfig `toml:"embeddings"`
}

// GeneralConfig holds general app settings.
type GeneralConfig struct {
	SyncIntervalMinutes int    `toml:"sync_interval_minutes"`
	PageSize            int    `toml:"page_size"`
	DefaultAccount      string `toml:"default_account"`
}

// ThemeConfig holds theme settings.
type ThemeConfig struct {
	Mode   string `toml:"mode"`
	Accent string `toml:"accent"`
}

// AccountConfig holds per-account settings.
type AccountConfig struct {
	Name        string `toml:"name"`
	IMAPHost    string `toml:"imap_host"`
	IMAPPort    int    `toml:"imap_port"`
	SMTPHost    string `toml:"smtp_host"`
	SMTPPort    int    `toml:"smtp_port"`
	Username    string `toml:"username"`
	Password    string `toml:"password"`
	PasswordCmd string `toml:"password_cmd"`
}

// CacheConfig holds cache settings.
type CacheConfig struct {
	Dir string `toml:"dir"`
}

// EmbeddingsConfig holds settings for semantic search embeddings.
type EmbeddingsConfig struct {
	Model                   string `toml:"model"`
	APIKey                  string `toml:"api_key"`
	APIKeyCmd               string `toml:"api_key_cmd"`
	BaseURL                 string `toml:"base_url"`
	BatchSize               int    `toml:"batch_size"`
	StreamBatch             int    `toml:"stream_batch"`
	TopSemantic             int    `toml:"top_semantic"`
	TopSimilar              int    `toml:"top_similar"`
	MaxCandidates           int    `toml:"max_candidates"`
	MaxContentChars         int    `toml:"max_content_chars"`
	PrefetchBodies          bool   `toml:"prefetch_bodies"`
	PrefetchBatch           int    `toml:"prefetch_batch"`
	PrefetchIntervalSeconds int    `toml:"prefetch_interval_seconds"`
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() *Config {
	cacheDir, _ := os.UserCacheDir()
	return &Config{
		General: GeneralConfig{
			SyncIntervalMinutes: 5,
			PageSize:            50,
		},
		Theme: ThemeConfig{
			Mode:   "auto",
			Accent: "#7C3AED",
		},
		Cache: CacheConfig{
			Dir: filepath.Join(cacheDir, "bubblmail"),
		},
		Embeddings: EmbeddingsConfig{
			Model:                   "openai/text-embedding-3-small",
			BaseURL:                 "https://openrouter.ai/api/v1",
			BatchSize:               16,
			StreamBatch:             128,
			TopSemantic:             30,
			TopSimilar:              30,
			MaxCandidates:           5000,
			MaxContentChars:         8000,
			PrefetchBodies:          true,
			PrefetchBatch:           10,
			PrefetchIntervalSeconds: 2,
		},
	}
}

// Load loads the config from ~/.config/bubblmail/config.toml.
// If the file doesn't exist, it returns defaults.
func Load() (*Config, error) {
	cfg := DefaultConfig()

	configDir, err := os.UserConfigDir()
	if err != nil {
		return cfg, nil
	}

	configPath := filepath.Join(configDir, "bubblmail", "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return cfg, nil
	}

	if _, err := toml.DecodeFile(configPath, cfg); err != nil {
		return nil, err
	}

	// Apply defaults for zero values
	if cfg.General.SyncIntervalMinutes == 0 {
		cfg.General.SyncIntervalMinutes = 5
	}
	if cfg.General.PageSize == 0 {
		cfg.General.PageSize = 50
	}
	if cfg.Theme.Mode == "" {
		cfg.Theme.Mode = "auto"
	}
	if cfg.Theme.Accent == "" {
		cfg.Theme.Accent = "#7C3AED"
	}
	if cfg.Embeddings.Model == "" {
		cfg.Embeddings.Model = "openai/text-embedding-3-small"
	}
	if cfg.Embeddings.BaseURL == "" {
		cfg.Embeddings.BaseURL = "https://openrouter.ai/api/v1"
	}
	if cfg.Embeddings.BatchSize == 0 {
		cfg.Embeddings.BatchSize = 16
	}
	if cfg.Embeddings.StreamBatch == 0 {
		cfg.Embeddings.StreamBatch = 128
	}
	if cfg.Embeddings.TopSemantic == 0 {
		cfg.Embeddings.TopSemantic = 30
	}
	if cfg.Embeddings.TopSimilar == 0 {
		cfg.Embeddings.TopSimilar = 30
	}
	if cfg.Embeddings.MaxCandidates == 0 {
		cfg.Embeddings.MaxCandidates = 5000
	}
	if cfg.Embeddings.MaxContentChars == 0 {
		cfg.Embeddings.MaxContentChars = 8000
	}
	if cfg.Embeddings.PrefetchBatch == 0 {
		cfg.Embeddings.PrefetchBatch = 10
	}
	if cfg.Embeddings.PrefetchIntervalSeconds == 0 {
		cfg.Embeddings.PrefetchIntervalSeconds = 2
	}

	return cfg, nil
}

// Save writes the config to ~/.config/bubblmail/config.toml, creating the
// directory if it does not exist.
func (c *Config) Save() error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(configDir, "bubblmail")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "config.toml"))
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

// ResolvePassword returns the account password, running PasswordCmd if needed.
func (a *AccountConfig) ResolvePassword() (string, error) {
	if a.Password != "" {
		return a.Password, nil
	}
	if a.PasswordCmd == "" {
		return "", nil
	}
	parts := strings.Fields(a.PasswordCmd)
	out, err := exec.Command(parts[0], parts[1:]...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ResolveAPIKey returns the embeddings API key, running APIKeyCmd if needed.
func (e *EmbeddingsConfig) ResolveAPIKey() (string, error) {
	if e.APIKey != "" {
		return e.APIKey, nil
	}
	if e.APIKeyCmd != "" {
		parts := strings.Fields(e.APIKeyCmd)
		out, err := exec.Command(parts[0], parts[1:]...).Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	if envKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")); envKey != "" {
		return envKey, nil
	}
	return "", nil
}
