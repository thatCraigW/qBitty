package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds qBittorrent connection credentials and optional Sonarr/Radarr API settings for queue blocklist.
type Config struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	// SonarrURL and SonarrAPIKey enable blocklist for torrents in category "Sonarr" (GET/DELETE /api/v3/queue).
	SonarrURL    string `json:"sonarr_url,omitempty"`
	SonarrAPIKey string `json:"sonarr_api_key,omitempty"`
	// RadarrURL and RadarrAPIKey enable blocklist for torrents in category "Radarr".
	RadarrURL    string `json:"radarr_url,omitempty"`
	RadarrAPIKey string `json:"radarr_api_key,omitempty"`
}

// LoadConfig reads the first existing config file from configFileCandidates, then applies env var overrides; returns error if any required field is missing or the config file is invalid JSON.
func LoadConfig() (*Config, error) {
	cfg := &Config{}

	if err := loadConfigFromFile(cfg); err != nil {
		return nil, err
	}

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

	if cfg.URL == "" || cfg.Username == "" || cfg.Password == "" {
		return nil, fmt.Errorf(
			"configuration required\n\n" +
				"  Create ~/.config/qbitty/config.json:\n\n" +
				"    {\n" +
				"      \"url\": \"https://your-qbittorrent:8080\",\n" +
				"      \"username\": \"admin\",\n" +
				"      \"password\": \"your-password\"\n" +
				"    }\n\n" +
				"  Or set environment variables: QB_URL, QB_USER, QB_PASS")
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
