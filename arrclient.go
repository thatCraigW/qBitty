package main

import (
	"bytes"
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

// arrCredentialsUsable is true when both API key and a valid http(s) base URL with a host are set (inputs: *arr base URL and API key from config; output: whether to enable the client).
func arrCredentialsUsable(baseURL, apiKey string) bool {
	if strings.TrimSpace(apiKey) == "" {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return strings.TrimSpace(u.Host) != ""
}

// arrQueueResponse is the paged JSON body from GET /api/v3/queue (Sonarr or Radarr v3).
type arrQueueResponse struct {
	Records      []arrQueueRecord `json:"records"`
	TotalRecords int              `json:"totalRecords"`
}

// queueStatusNames maps Sonarr QueueStatus enum indices to JSON string names (same order as NzbDrone.Core.Queue.QueueStatus).
var queueStatusNames = []string{
	"unknown", "queued", "paused", "downloading", "completed", "failed", "warning", "delay", "downloadClientUnavailable", "fallback",
}

// trackedDownloadStateNames maps TrackedDownloadState enum indices (Sonarr/Radarr).
var trackedDownloadStateNames = []string{
	"downloading", "importBlocked", "importPending", "importing", "imported", "failedPending", "failed", "ignored",
}

// trackedDownloadStatusNames maps TrackedDownloadStatus enum indices (ok, warning, error).
var trackedDownloadStatusNames = []string{"ok", "warning", "error"}

// arrQueueRecord is one queue row; id is used for DELETE, downloadId/title for matching the qBittorrent job.
type arrQueueRecord struct {
	ID                    int    `json:"-"`
	DownloadID            string `json:"downloadId"`
	Title                 string `json:"title"`
	Status                string `json:"-"`
	TrackedDownloadStatus string `json:"-"`
	TrackedDownloadState  string `json:"-"`
}

// UnmarshalJSON decodes Sonarr/Radarr queue rows, accepting string or numeric enum values from the API (input: JSON bytes; output: fills arrQueueRecord).
func (r *arrQueueRecord) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID                   int             `json:"id"`
		DownloadID           string          `json:"downloadId"`
		Title                string          `json:"title"`
		Status               json.RawMessage `json:"status"`
		TrackedDownloadStatus json.RawMessage `json:"trackedDownloadStatus"`
		TrackedDownloadState  json.RawMessage `json:"trackedDownloadState"`
	}
	var aux raw
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	r.ID = aux.ID
	r.DownloadID = aux.DownloadID
	r.Title = aux.Title
	r.Status = decodeEnumField(aux.Status, queueStatusNames)
	r.TrackedDownloadStatus = decodeEnumField(aux.TrackedDownloadStatus, trackedDownloadStatusNames)
	r.TrackedDownloadState = decodeEnumField(aux.TrackedDownloadState, trackedDownloadStateNames)
	return nil
}

// decodeEnumField returns a string enum name from JSON string or numeric index (inputs: raw JSON fragment, ordered enum names; output: lowercase name or empty).
func decodeEnumField(raw json.RawMessage, names []string) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return ""
	}
	i := int(n)
	if i >= 0 && i < len(names) {
		return names[i]
	}
	return ""
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

// shouldShowArrColumnStatus is true when the *arr queue row is past active client download (import pipeline, completed, failures) (inputs: queue row; output: whether to replace the Status column).
func shouldShowArrColumnStatus(rec *arrQueueRecord) bool {
	if rec == nil {
		return false
	}
	td := strings.ToLower(strings.TrimSpace(rec.TrackedDownloadState))
	qs := strings.ToLower(strings.TrimSpace(rec.Status))
	if td == "downloading" {
		return false
	}
	if td != "" {
		return true
	}
	switch qs {
	case "completed", "failed", "warning", "delay", "fallback":
		return true
	case "queued", "paused", "downloading", "unknown", "":
		return false
	default:
		return qs != ""
	}
}

// humanizeArrEnum turns camelCase enum names into short spaced lowercase text (input: API enum string; output: display fragment).
func humanizeArrEnum(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, " ") {
		return strings.ToLower(s)
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// truncateStatusRunes limits status text to max runes for fixed-width columns (inputs: text, max runes; output: possibly truncated string).
func truncateStatusRunes(s string, max int) string {
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max <= 1 {
		return ""
	}
	return string(rs[:max-1]) + "…"
}

// formatArrQueueStatusShort builds a short label from tracked state or queue status (input: queue row; output: text fitting the Status column).
func formatArrQueueStatusShort(rec *arrQueueRecord) string {
	td := strings.TrimSpace(rec.TrackedDownloadState)
	qs := strings.TrimSpace(rec.Status)
	var pick string
	if td != "" && !strings.EqualFold(td, "downloading") {
		pick = td
	} else if qs != "" {
		pick = qs
	} else {
		pick = "unknown"
	}
	s := humanizeArrEnum(pick)
	return truncateStatusRunes(s, 12)
}

// colorForArrQueueStatus picks ANSI color for *arr-derived status (input: queue row; output: ANSI fg sequence).
func colorForArrQueueStatus(rec *arrQueueRecord) string {
	ts := strings.ToLower(strings.TrimSpace(rec.TrackedDownloadStatus))
	if ts == "error" {
		return redColor
	}
	if ts == "warning" {
		return yellowColor
	}
	td := strings.ToLower(strings.TrimSpace(rec.TrackedDownloadState))
	if strings.Contains(td, "fail") {
		return redColor
	}
	qs := strings.ToLower(strings.TrimSpace(rec.Status))
	if qs == "failed" {
		return redColor
	}
	if qs == "warning" {
		return yellowColor
	}
	return cyanColor
}

// torrentStatusColumnText returns status label and color for the torrent list: *arr pipeline text for Sonarr/Radarr category when the queue matches and is post-download; otherwise qB status (inputs: torrent, cached Sonarr/Radarr queue rows; output: text and ANSI color).
func torrentStatusColumnText(t Torrent, sonarrQ, radarrQ []arrQueueRecord) (string, string) {
	base := cleanStatusString(t.State)
	baseColor := colorForState(t.State)

	var records []arrQueueRecord
	switch t.Category {
	case arrCategorySonarr:
		if arrSonarrClient == nil {
			return base, baseColor
		}
		records = sonarrQ
	case arrCategoryRadarr:
		if arrRadarrClient == nil {
			return base, baseColor
		}
		records = radarrQ
	default:
		return base, baseColor
	}

	rec := findQueueRecordForTorrent(records, t.Hash, t.Name)
	if rec == nil || !shouldShowArrColumnStatus(rec) {
		return base, baseColor
	}
	return formatArrQueueStatusShort(rec), colorForArrQueueStatus(rec)
}
