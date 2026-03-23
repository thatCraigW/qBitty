package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// QBClient holds the HTTP client and auth info
// for interacting with qbittorrent WebUI API.
type QBClient struct {
	Client  *http.Client
	BaseURL string
}

// NewQBClient loads config (file + env var fallback), creates an HTTP client, and authenticates.
func NewQBClient() (*QBClient, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

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

	api := &QBClient{
		Client:  client,
		BaseURL: strings.TrimRight(cfg.URL, "/"),
	}

	if err := api.Login(cfg.Username, cfg.Password); err != nil {
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

// postAction sends a POST with form data to the given API endpoint; returns error on non-200.
func (c *QBClient) postAction(endpoint string, data url.Values) error {
	resp, err := c.Client.PostForm(c.BaseURL+endpoint, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
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

// getJSON performs a GET request and decodes the JSON response into target.
func (c *QBClient) getJSON(endpoint string, target interface{}) error {
	resp, err := c.Client.Get(c.BaseURL + endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
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
