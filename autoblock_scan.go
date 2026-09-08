package main

import (
	"strings"
	"sync"
	"time"
)

// torrentFileFetcher returns a torrent's file list by hash; injected so the
// scanner can be tested without a qBittorrent instance.
type torrentFileFetcher func(hash string) ([]TorrentFile, error)

// autoBlockDetection is one suspicious torrent found by a scan.
type autoBlockDetection struct {
	Torrent  Torrent
	Verdict  fileVerdict
	QueueID  int  // Sonarr/Radarr queue row id; meaningful only when HasQueue
	HasQueue bool // false when *arr no longer tracks the download, so blocklist is impossible
}

// autoBlockNotReadyStates are qBittorrent states where the file list is absent or
// unreliable; the torrent is reconsidered on a later tick.
var autoBlockNotReadyStates = map[string]bool{
	"allocating":         true,
	"checkingresumedata": true,
	"moving":             true,
	"unknown":            true,
	"error":              true,
	"missingfiles":       true,
}

// autoBlockStateReady is true when a torrent's file list can be trusted (input: qB state; output: whether to inspect).
func autoBlockStateReady(state string) bool {
	s := strings.ToLower(strings.TrimSpace(state))
	if s == "" || strings.Contains(s, "metadl") {
		return false
	}
	return !autoBlockNotReadyStates[s]
}

// autoBlockCategoryQueue returns the cached *arr queue for a torrent's category, and
// whether the category is one qbitty manages (inputs: category and both queue snapshots).
func autoBlockCategoryQueue(category string, sonarrQ, radarrQ []arrQueueRecord) ([]arrQueueRecord, bool) {
	switch category {
	case arrCategorySonarr:
		return sonarrQ, true
	case arrCategoryRadarr:
		return radarrQ, true
	default:
		return nil, false
	}
}

// autoBlockScanner holds the per-hash memory that keeps a scan cheap and keeps a
// torrent from being reported twice.
type autoBlockScanner struct {
	mu       sync.Mutex
	verdicts map[string]fileVerdict // hash -> verdict, so a clean torrent is never re-fetched
	flagged  map[string]fileVerdict // hash -> suspicious verdict, for the UI in flag mode
	handled  map[string]bool        // hash -> already reported, so a detection fires once
	// deferred holds detections auto mode could not act on yet because the hourly cap
	// was spent. They are replayed on later ticks rather than dropped: the cap is a
	// rate limit, not a verdict that the torrent is fine.
	deferred map[string]autoBlockDetection
}

// newAutoBlockScanner returns an empty scanner (inputs: none; output: scanner).
func newAutoBlockScanner() *autoBlockScanner {
	return &autoBlockScanner{
		verdicts: make(map[string]fileVerdict),
		flagged:  make(map[string]fileVerdict),
		handled:  make(map[string]bool),
		deferred: make(map[string]autoBlockDetection),
	}
}

// deferDetection holds a detection back for a later tick (input: detection).
func (s *autoBlockScanner) deferDetection(d autoBlockDetection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deferred[d.Torrent.Hash] = d
}

// takeDeferred removes and returns every held-back detection, so a caller that
// cannot act on one must defer it again (inputs: none; output: detections).
func (s *autoBlockScanner) takeDeferred() []autoBlockDetection {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.deferred) == 0 {
		return nil
	}
	out := make([]autoBlockDetection, 0, len(s.deferred))
	for _, d := range s.deferred {
		out = append(out, d)
	}
	s.deferred = make(map[string]autoBlockDetection)
	return out
}

// flaggedVerdict reports whether a hash is currently flagged as suspicious
// (input: torrent hash; output: verdict and whether it was flagged).
func (s *autoBlockScanner) flaggedVerdict(hash string) (fileVerdict, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.flagged[hash]
	return v, ok
}

// flaggedCount returns how many torrents are currently flagged (inputs: none; output: count).
func (s *autoBlockScanner) flaggedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.flagged)
}

// forget drops all memory of a hash, so a re-added torrent is judged afresh (input: hash).
func (s *autoBlockScanner) forget(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.verdicts, hash)
	delete(s.flagged, hash)
	delete(s.handled, hash)
	delete(s.deferred, hash)
}

// pruneMissing drops memory of hashes no longer present in qBittorrent (input: set of live hashes).
func (s *autoBlockScanner) pruneMissing(live map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for h := range s.verdicts {
		if !live[h] {
			delete(s.verdicts, h)
		}
	}
	for h := range s.flagged {
		if !live[h] {
			delete(s.flagged, h)
		}
	}
	for h := range s.handled {
		if !live[h] {
			delete(s.handled, h)
		}
	}
	for h := range s.deferred {
		if !live[h] {
			delete(s.deferred, h)
		}
	}
}

// scan inspects Sonarr/Radarr torrents that have not been judged yet and returns
// newly found suspicious ones. A torrent is skipped while inside its grace window,
// while its state says the file list is not final, and forever after it has been
// judged clean. At most autoBlockScanBudgetPerTick file lists are fetched per call
// (inputs: file fetcher, settings, torrent list, both queue snapshots, current time;
// output: detections not previously reported).
func (s *autoBlockScanner) scan(fetch torrentFileFetcher, st autoBlockSettings, list []Torrent, sonarrQ, radarrQ []arrQueueRecord, now time.Time) []autoBlockDetection {
	live := make(map[string]bool, len(list))
	for _, t := range list {
		live[t.Hash] = true
	}
	s.pruneMissing(live)

	if !st.enabled() {
		return nil
	}

	var out []autoBlockDetection
	budget := autoBlockScanBudgetPerTick

	for _, t := range list {
		queue, managed := autoBlockCategoryQueue(t.Category, sonarrQ, radarrQ)
		if !managed {
			continue
		}
		if !autoBlockStateReady(t.State) {
			continue
		}
		if t.AddedOn > 0 && now.Unix()-t.AddedOn < int64(st.GraceSeconds) {
			continue
		}

		s.mu.Lock()
		_, judged := s.verdicts[t.Hash]
		s.mu.Unlock()
		if judged {
			continue
		}

		if budget <= 0 {
			break
		}
		files, err := fetch(t.Hash)
		if err != nil {
			continue // transient; retry next tick without caching a verdict
		}
		budget--
		if len(files) == 0 {
			continue // metadata still arriving; not a verdict of "no media"
		}

		v := classifyTorrentFiles(files, st.Rules)

		s.mu.Lock()
		s.verdicts[t.Hash] = v
		if v.suspicious() {
			s.flagged[t.Hash] = v
		}
		already := s.handled[t.Hash]
		if v.suspicious() {
			s.handled[t.Hash] = true
		}
		s.mu.Unlock()

		if !v.suspicious() || already {
			continue
		}

		rec := findQueueRecordForTorrent(queue, t.Hash, t.Name)
		d := autoBlockDetection{Torrent: t, Verdict: v}
		if rec != nil {
			d.QueueID = rec.ID
			d.HasQueue = true
		}
		out = append(out, d)
	}

	return out
}

// logEntryForDetection builds the audit record for a detection (inputs: detection and mode; output: log entry).
func logEntryForDetection(d autoBlockDetection, mode string) autoBlockLogEntry {
	action := autoBlockActionDetected
	if !d.HasQueue {
		action = autoBlockActionNoQueueRow
	}
	return autoBlockLogEntry{
		Mode:        mode,
		Action:      action,
		Hash:        d.Torrent.Hash,
		Name:        d.Torrent.Name,
		Category:    d.Torrent.Category,
		TotalFiles:  d.Verdict.TotalFiles,
		MediaFiles:  d.Verdict.MediaFiles,
		BannedFiles: d.Verdict.BannedFiles,
		QueueID:     d.QueueID,
	}
}

// autoBlockBlocklistFunc blocklists a torrent through the *arr owning its category;
// injected so auto mode can be exercised without Sonarr or Radarr.
type autoBlockBlocklistFunc func(category, hash, name string) error

// actOnDetection carries out auto mode's decision for one detection, returning the
// audit entry describing what actually happened and whether the detection should be
// retried later. Only auto mode acts at all; a detection *arr no longer tracks cannot
// be blocklisted, and one past the hourly cap is held for a later tick rather than
// dropped. A failed blocklist is recorded and not retried — the common causes (the
// queue row is gone, the release was already handled) do not improve with repetition,
// and the torrent stays flagged for the user to action with `b`
// (inputs: detection, settings, blocklist call, limiter, current time;
// output: log entry and whether to defer for a later tick).
func actOnDetection(d autoBlockDetection, st autoBlockSettings, blocklist autoBlockBlocklistFunc, limiter *autoBlockRateLimiter, now time.Time) (autoBlockLogEntry, bool) {
	e := logEntryForDetection(d, st.Mode)
	if !st.acts() || !d.HasQueue || blocklist == nil || limiter == nil {
		return e, false
	}
	if !limiter.allow(st.MaxPerHour, now) {
		e.Action = autoBlockActionRateCapped
		return e, true
	}
	if err := blocklist(d.Torrent.Category, d.Torrent.Hash, d.Torrent.Name); err != nil {
		e.Action = autoBlockActionError
		e.Error = err.Error()
		return e, false
	}
	e.Action = autoBlockActionBlocklisted
	return e, false
}
