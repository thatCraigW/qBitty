package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds qBittorrent connection credentials and optional Sonarr/Radarr API settings for queue blocklist.
type Config struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	APIKey   string `json:"api_key,omitempty"`
	// SonarrURL and SonarrAPIKey enable blocklist for torrents in category "Sonarr" (GET/DELETE /api/v3/queue).
	SonarrURL    string `json:"sonarr_url,omitempty"`
	SonarrAPIKey string `json:"sonarr_api_key,omitempty"`
	// RadarrURL and RadarrAPIKey enable blocklist for torrents in category "Radarr".
	RadarrURL    string `json:"radarr_url,omitempty"`
	RadarrAPIKey string `json:"radarr_api_key,omitempty"`
	// AutoBlock configures automatic detection of Sonarr/Radarr torrents that hold
	// no importable media; nil or mode "off" disables it entirely.
	AutoBlock *AutoBlockConfig `json:"autoblock,omitempty"`
}

// AutoBlockConfig is the user-facing auto-block settings; every field is optional
// and falls back to the defaults in autoblock.go.
type AutoBlockConfig struct {
	// Mode is off, log, flag, or auto (increasing autonomy); unrecognized values disable the feature.
	Mode string `json:"mode,omitempty"`
	// MinMediaBytes is the size a media file must reach to count as real content.
	MinMediaBytes int64 `json:"min_media_bytes,omitempty"`
	// GraceSeconds is how long after a torrent is added before it may be judged.
	GraceSeconds int `json:"grace_seconds,omitempty"`
	// MaxPerHour caps blocklist actions per rolling hour in auto mode.
	MaxPerHour int `json:"max_per_hour,omitempty"`
	// BannedExtensions and MediaExtensions replace the built-in lists when non-empty.
	BannedExtensions []string `json:"banned_extensions,omitempty"`
	MediaExtensions  []string `json:"media_extensions,omitempty"`
	// LogPath overrides the default audit log location.
	LogPath string `json:"log_path,omitempty"`
}

// mergeConfigFromFileAndEnv loads the first existing config file if any, applies env overrides, and does not require qB fields to be set (inputs: none; output: merged config or I/O/JSON error).
func mergeConfigFromFileAndEnv() (*Config, error) {
	cfg := &Config{}
	if err := loadConfigFromFile(cfg); err != nil {
		return nil, err
	}
	applyEnvOverrides(cfg)
	return cfg, nil
}

// applyEnvOverrides replaces config fields from QB_*, SONARR_* / RADARR_*, and QBITTY_AUTOBLOCK_MODE when those env vars are non-empty (input/output: cfg pointer).
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("QB_URL"); v != "" {
		cfg.URL = v
	}
	if v := os.Getenv("QB_USER"); v != "" {
		cfg.Username = v
	}
	if v := os.Getenv("QB_PASS"); v != "" {
		cfg.Password = v
	}
	if v := os.Getenv("SONARR_URL"); v != "" {
		cfg.SonarrURL = v
	}
	if v := os.Getenv("SONARR_API_KEY"); v != "" {
		cfg.SonarrAPIKey = v
	}
	if v := os.Getenv("RADARR_URL"); v != "" {
		cfg.RadarrURL = v
	}
	if v := os.Getenv("RADARR_API_KEY"); v != "" {
		cfg.RadarrAPIKey = v
	}
	if v := os.Getenv("QBITTY_AUTOBLOCK_MODE"); v != "" {
		if cfg.AutoBlock == nil {
			cfg.AutoBlock = &AutoBlockConfig{}
		}
		cfg.AutoBlock.Mode = v
	}
}

// qbConfigIncomplete is true when url is missing OR (username/password AND api_key are all empty) after trim (input: merged config).
func qbConfigIncomplete(cfg *Config) bool {
	if cfg == nil {
		return true
	}
	return strings.TrimSpace(cfg.URL) == "" || (strings.TrimSpace(cfg.Username) == "" && strings.TrimSpace(cfg.Password) == "" && strings.TrimSpace(cfg.APIKey) == "")
}

// ErrConfigRequired is returned when url, username, or password is missing after merge (wizard and LoadConfig use this).
var ErrConfigRequired = errors.New("configuration required")

// validateRequiredQB returns ErrConfigRequired with setup hints if url, username, or password is empty (input: merged config).
func validateRequiredQB(cfg *Config) error {
	if qbConfigIncomplete(cfg) {
		return fmt.Errorf("%w\n\n  Create ~/.config/qbitty/config.json:\n\n    {\n      \"url\": \"https://your-qbittorrent:8080\",\n      \"username\": \"admin\",\n      \"password\": \"your-password\"\n    }\n\n  Or set environment variables: QB_URL, QB_USER, QB_PASS\n\n  Or run with QBITTY_WIZARD=1 (or --wizard) for interactive setup when credentials are missing\n\n  For qBittorrent v5.2+, you can also use an API key instead:\n    {\n      \"url\": \"https://your-qbittorrent:8080\",\n      \"api_key\": \"your-api-key-here\"\n    }", ErrConfigRequired)
	}
	return nil
}

// LoadConfig reads config from file + env and returns an error if any required qBittorrent field is missing (same behavior as before mergeConfigFromFileAndEnv existed).
func LoadConfig() (*Config, error) {
	cfg, err := mergeConfigFromFileAndEnv()
	if err != nil {
		return nil, err
	}
	if err := validateRequiredQB(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadConfigFromFile fills cfg from the first readable candidate path; skips missing files; returns an error on I/O or JSON parse failure (input/output: cfg pointer).
func loadConfigFromFile(cfg *Config) error {
	for _, path := range configFileCandidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read config file %s: %w", path, err)
		}
		data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
		if err := json.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("invalid JSON in config file %s: %w", path, err)
		}
		return nil
	}
	return nil
}

// PrimaryConfigWritePath returns the preferred path for creating or updating config.json (same order as load: XDG first, then ~/.config).
func PrimaryConfigWritePath() (string, error) {
	cands := configFileCandidates()
	if len(cands) == 0 {
		return "", fmt.Errorf("cannot determine config directory (set HOME or XDG_CONFIG_HOME)")
	}
	return cands[0], nil
}

// writeConfigFile writes cfg as indented JSON with 0600 permissions (inputs: path and config; output: I/O error).
func writeConfigFile(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// configFileCandidates returns paths to try in order: $XDG_CONFIG_HOME/qbitty/config.json (if set), then ~/.config/qbitty/config.json so a file under ~/.config still loads when XDG_CONFIG_HOME points elsewhere.
func configFileCandidates() []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(dir string) {
		p := filepath.Join(dir, "qbitty", "config.json")
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		add(xdg)
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".config"))
	}
	return out
}
