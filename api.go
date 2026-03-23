package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

// QBClient holds the HTTP client and auth info
// for interacting with qbittorrent WebUI API.
type QBClient struct {
	Client  *http.Client
	BaseURL string
}

// NewQBClient creates a new client and logs in using
// credentials from environment variables (QB_URL, QB_USER, QB_PASS).
func NewQBClient() (*QBClient, error) {
	baseURL := os.Getenv("QB_URL")
	username := os.Getenv("QB_USER")
	password := os.Getenv("QB_PASS")

	if baseURL == "" || username == "" || password == "" {
		return nil, errors.New("QB_URL, QB_USER and QB_PASS environment variables must be set")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid QB_URL: %w", err)
	}
	if parsed.Scheme == "http" {
		log.Println("WARNING: QB_URL uses plain HTTP — credentials will be sent in cleartext. Use HTTPS for secure connections.")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}

	api := &QBClient{
		Client:  client,
		BaseURL: strings.TrimRight(baseURL, "/"),
	}

	if err := api.Login(username, password); err != nil {
		return nil, err
	}
	return api, nil
}

// Login performs POST to /api/v2/auth/login and stores session cookie.
func (c *QBClient) Login(username, password string) error {
	loginURL := c.BaseURL + "/api/v2/auth/login"
	data := url.Values{}
	data.Set("username", username)
	data.Set("password", password)

	resp, err := c.Client.PostForm(loginURL, data)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	// qbittorrent returns HTTP 200 with "" or "Ok." body on success
	// and HTTP 403 or message on failure
	if resp.StatusCode != 200 || (string(body) != "" && string(body) != "Ok.") {
		return fmt.Errorf("login failed: status %d body %q", resp.StatusCode, string(body))
	}

	return nil
}

// GetTorrents fetches torrent info and parses into Torrent structs
func (c *QBClient) GetTorrents() ([]Torrent, error) {
	url := c.BaseURL + "/api/v2/torrents/info"
	resp, err := c.Client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var torrents []Torrent
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		return nil, err
	}

	return torrents, nil
}

// GetTorrentsRaw fetches raw JSON from torrents/info endpoint
func (c *QBClient) GetTorrentsRaw() ([]byte, error) {
	url := c.BaseURL + "/api/v2/torrents/info"
	resp, err := c.Client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	const maxResponseSize = 64 * 1024 * 1024 // 64 MB
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, err
	}

	return data, nil
}
