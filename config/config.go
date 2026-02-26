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
	General  GeneralConfig   `toml:"general"`
	Theme    ThemeConfig     `toml:"theme"`
	Accounts []AccountConfig `toml:"account"`
	Cache    CacheConfig     `toml:"cache"`
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

	return cfg, nil
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
