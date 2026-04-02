package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	arrCategorySonarr = "Sonarr"
	arrCategoryRadarr = "Radarr"
)

// arrQueueResponse is the paged JSON body from GET /api/v3/queue (Sonarr or Radarr v3).
type arrQueueResponse struct {
	Records      []arrQueueRecord `json:"records"`
	TotalRecords int              `json:"totalRecords"`
}

// arrQueueRecord is one queue row; id is used for DELETE, downloadId/title for matching the qBittorrent job.
type arrQueueRecord struct {
	ID         int    `json:"id"`
	DownloadID string `json:"downloadId"`
	Title      string `json:"title"`
}

// ArrHTTPClient calls Sonarr or Radarr /api/v3 with X-Api-Key (inputs: base URL and API key; output: queue fetch and delete).
type ArrHTTPClient struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewArrHTTPClient builds a client with a 30s timeout; baseURL is trimmed of trailing slashes.
func NewArrHTTPClient(baseURL, apiKey string) *ArrHTTPClient {
	return &ArrHTTPClient{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:  strings.TrimSpace(apiKey),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// FetchAllQueueRecords returns all queue rows, following pagination (input: none; output: records or API error).
func (c *ArrHTTPClient) FetchAllQueueRecords() ([]arrQueueRecord, error) {
	if c.BaseURL == "" || c.APIKey == "" {
		return nil, fmt.Errorf("arr client not configured")
	}
	const pageSize = 200
	var all []arrQueueRecord
	for page := 1; ; page++ {
		apiPath := fmt.Sprintf("/api/v3/queue?page=%d&pageSize=%d", page, pageSize)
		var resp arrQueueResponse
		if err := c.getJSON(apiPath, &resp); err != nil {
			return nil, err
		}
		if len(resp.Records) == 0 {
			break
		}
		all = append(all, resp.Records...)
		if len(all) >= resp.TotalRecords || len(resp.Records) < pageSize {
			break
		}
	}
	return all, nil
}

// DeleteQueueBlocklist removes a queue item, deletes from the download client, and blocklists the release (input: Sonarr/Radarr queue id).
func (c *ArrHTTPClient) DeleteQueueBlocklist(queueID int) error {
	if c.BaseURL == "" || c.APIKey == "" {
		return fmt.Errorf("arr client not configured")
	}
	u, err := url.Parse(c.BaseURL + "/api/v3/queue/" + strconv.Itoa(queueID))
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("removeFromClient", "true")
	q.Set("blocklist", "true")
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.APIKey)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("arr API returned %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *ArrHTTPClient) getJSON(apiPath string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+apiPath, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("arr API GET %s returned %d: %s", apiPath, res.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// queueRecordMatchesTorrent returns true if a *arr queue row corresponds to the qBittorrent hash or name (inputs: queue row, torrent hash and name).
func queueRecordMatchesTorrent(rec *arrQueueRecord, hash, name string) bool {
	if rec == nil {
		return false
	}
	h := strings.TrimSpace(strings.ToLower(hash))
	if h != "" && rec.DownloadID != "" {
		did := strings.TrimSpace(strings.ToLower(rec.DownloadID))
		if did == h {
			return true
		}
		if strings.HasSuffix(did, h) || strings.HasPrefix(did, h) {
			return true
		}
		// Some clients store non-hex ids; allow suffix match of 40-char hash.
		if len(h) == 40 && strings.Contains(did, h) {
			return true
		}
	}
	if name != "" && rec.Title != "" {
		if strings.EqualFold(strings.TrimSpace(rec.Title), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// findQueueRecordForTorrent returns the first matching queue row, or nil (inputs: queue list, torrent hash and name).
func findQueueRecordForTorrent(records []arrQueueRecord, hash, name string) *arrQueueRecord {
	for i := range records {
		if queueRecordMatchesTorrent(&records[i], hash, name) {
			return &records[i]
		}
	}
	return nil
}

// blocklistTorrentViaArr fetches the app queue, finds the torrent, and DELETEs with blocklist (inputs: *arr client, torrent hash and name; output: error).
func blocklistTorrentViaArr(client *ArrHTTPClient, hash, name string) error {
	if client == nil {
		return fmt.Errorf("arr client not configured")
	}
	records, err := client.FetchAllQueueRecords()
	if err != nil {
		return err
	}
	rec := findQueueRecordForTorrent(records, hash, name)
	if rec == nil {
		return fmt.Errorf("no queue entry matching this torrent — blocklist only works while Sonarr/Radarr still tracks the download")
	}
	return client.DeleteQueueBlocklist(rec.ID)
}
