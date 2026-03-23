package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds qBittorrent connection credentials.
type Config struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoadConfig reads ~/.config/qbitty/config.json then applies env var overrides; returns error if any field is missing.
func LoadConfig() (*Config, error) {
	cfg := &Config{}

	if path, err := configFilePath(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, cfg)
		}
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

// configFilePath returns the path to ~/.config/qbitty/config.json.
func configFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "qbitty", "config.json"), nil
}
