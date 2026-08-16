package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// QBClient holds the HTTP client and auth info
// for interacting with qbittorrent WebUI API.
type QBClient struct {
	Client  *http.Client
	BaseURL string
	apiKey  string // optional X-Api-Key header for qBittorrent v5.2+
}

// NewQBClient loads config (file + env var fallback), creates an HTTP client, and authenticates.
func NewQBClient() (*QBClient, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	api, err := NewQBClientFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	if err := api.Login(cfg.Username, cfg.Password, cfg.APIKey); err != nil {
		return nil, err
	}
	return api, nil
}

// NewQBClientFromConfig builds a QBClient with cookie jar, timeout, and API key; does not call Login (for TUI retry flows).
func NewQBClientFromConfig(cfg *Config) (*QBClient, error) {
	parsed, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme == "http" {
		log.Println("WARNING: URL uses plain HTTP — credentials will be sent in cleartext. Use HTTPS for secure connections.")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}

	return &QBClient{
		Client:  client,
		BaseURL: strings.TrimRight(cfg.URL, "/"),
		apiKey:  cfg.APIKey, // Store API key for use in subsequent requests
	}, nil
}

// Login performs authentication - uses Basic Auth (username/password) with optional X-Api-Key header.
func (c *QBClient) Login(username, password, apiKey string) error {
	loginURL := c.BaseURL + "/api/v2/auth/login"

	// qBittorrent v5.2+ requires both Basic Auth and API key for secure authentication
	data := url.Values{}
	data.Set("username", username)
	data.Set("password", password)

	client := &http.Client{
		Jar:     c.Client.Jar,
		Timeout: c.Client.Timeout,
		Transport: c.Client.Transport,
	}

	req, err := http.NewRequest("POST", loginURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	// Add API key header if provided (required for qBittorrent v5.2+)
	if apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	// qBittorrent <5.2: HTTP 200 with "" or "Ok."; ≥5.2: HTTP 204 No Content on success (empty body).
	loginOK := (resp.StatusCode == http.StatusOK && (len(body) == 0 || string(body) == "Ok.")) ||
		resp.StatusCode == http.StatusNoContent
	if !loginOK {
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

// getJSON performs a GET request with optional API key header and decodes the JSON response into target.
func (c *QBClient) getJSON(endpoint string, target interface{}) error {
	req, err := http.NewRequest("GET", c.BaseURL+endpoint, nil)
	if err != nil {
		return err
	}

	client := &http.Client{
		Jar:     c.Client.Jar,
		Timeout: c.Client.Timeout,
		Transport: c.Client.Transport,
	}

	// Add API key header if provided (required for qBittorrent v5.2+)
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

// postAction sends a POST with form data to the given API endpoint; returns error on non-200.
func (c *QBClient) postAction(endpoint string, data url.Values) error {
	client := &http.Client{
		Jar:     c.Client.Jar,
		Timeout: c.Client.Timeout,
		Transport: c.Client.Transport,
	}

	req, err := http.NewRequest("POST", c.BaseURL+endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Add API key header if provided (required for qBittorrent v5.2+)
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API %s returned status %d: %s", endpoint, resp.StatusCode, string(body))
	}
	return nil
}

// StopTorrents sends a stop command for the given torrent hashes (pipe-separated).
func (c *QBClient) StopTorrents(hashes string) error {
	return c.postAction("/api/v2/torrents/stop", url.Values{"hashes": {hashes}})
}

// StartTorrents sends a start command for the given torrent hashes (pipe-separated).
func (c *QBClient) StartTorrents(hashes string) error {
	return c.postAction("/api/v2/torrents/start", url.Values{"hashes": {hashes}})
}

// RecheckTorrents queues a force recheck for the given torrent hashes (pipe-separated).
func (c *QBClient) RecheckTorrents(hashes string) error {
	return c.postAction("/api/v2/torrents/recheck", url.Values{"hashes": {hashes}})
}

// DeleteTorrent removes a torrent; deleteFiles controls whether downloaded data is also removed.
func (c *QBClient) DeleteTorrent(hash string, deleteFiles bool) error {
	df := "false"
	if deleteFiles {
		df = "true"
	}
	return c.postAction("/api/v2/torrents/delete", url.Values{
		"hashes":      {hash},
		"deleteFiles": {df},
	})
}

// IncreasePriority raises queue priority for the given torrent hash.
func (c *QBClient) IncreasePriority(hash string) error {
	return c.postAction("/api/v2/torrents/increasePrio", url.Values{"hashes": {hash}})
}

// DecreasePriority lowers queue priority for the given torrent hash.
func (c *QBClient) DecreasePriority(hash string) error {
	return c.postAction("/api/v2/torrents/decreasePrio", url.Values{"hashes": {hash}})
}

// AddTorrentURL adds a torrent by URL or magnet link.
func (c *QBClient) AddTorrentURL(torrentURL string) error {
	return c.postAction("/api/v2/torrents/add", url.Values{"urls": {torrentURL}})
}

// GetTorrentProperties fetches detailed properties for a single torrent by hash.
func (c *QBClient) GetTorrentProperties(hash string) (*TorrentProperties, error) {
	var props TorrentProperties
	err := c.getJSON("/api/v2/torrents/properties?hash="+hash, &props)
	return &props, err
}

// GetTorrentTrackers fetches the tracker list for a torrent by hash.
func (c *QBClient) GetTorrentTrackers(hash string) ([]Tracker, error) {
	var trackers []Tracker
	err := c.getJSON("/api/v2/torrents/trackers?hash="+hash, &trackers)
	return trackers, err
}

// GetTorrentPeers fetches connected peers for a torrent by hash.
func (c *QBClient) GetTorrentPeers(hash string) ([]Peer, error) {
	var result PeersResponse
	if err := c.getJSON("/api/v2/sync/torrentPeers?hash="+hash, &result); err != nil {
		return nil, err
	}
	peers := make([]Peer, 0, len(result.Peers))
	for _, p := range result.Peers {
		peers = append(peers, p)
	}
	return peers, nil
}

// GetTorrentWebSeeds fetches HTTP sources (web seeds) for a torrent by hash.
func (c *QBClient) GetTorrentWebSeeds(hash string) ([]WebSeed, error) {
	var seeds []WebSeed
	err := c.getJSON("/api/v2/torrents/webseeds?hash="+hash, &seeds)
	return seeds, err
}

// GetTorrentFiles fetches the file list for a torrent by hash.
func (c *QBClient) GetTorrentFiles(hash string) ([]TorrentFile, error) {
	var files []TorrentFile
	err := c.getJSON("/api/v2/torrents/files?hash="+hash, &files)
	return files, err
}

// SetFilePriority sets one file's download priority for a torrent (0 skip, 1 normal, 6 high, 7 maximum); inputs hash/file id/priority, output error from API.
func (c *QBClient) SetFilePriority(hash string, fileID int, priority int) error {
	return c.postAction("/api/v2/torrents/filePrio", url.Values{
		"hash":     {hash},
		"id":       {strconv.Itoa(fileID)},
		"priority": {strconv.Itoa(priority)},
	})
}
