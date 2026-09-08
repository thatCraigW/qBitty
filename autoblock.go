package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Auto-block modes, in increasing order of autonomy: off does nothing, log only
// records what it would have done, flag additionally marks torrents in the UI,
// and auto blocklists via Sonarr/Radarr.
const (
	autoBlockModeOff  = "off"
	autoBlockModeLog  = "log"
	autoBlockModeFlag = "flag"
	autoBlockModeAuto = "auto"
)

const (
	// defaultAutoBlockMinMediaBytes is the size a media file must reach to count as
	// real content; smaller ones are decoys padding an otherwise executable payload.
	defaultAutoBlockMinMediaBytes = 50 * 1024 * 1024
	// defaultAutoBlockGraceSeconds is how long after AddedOn a torrent is left alone,
	// so a still-arriving file list is never judged as complete.
	defaultAutoBlockGraceSeconds = 60
	// defaultAutoBlockMaxPerHour caps actions so a rule bug cannot walk the whole queue.
	defaultAutoBlockMaxPerHour = 5
	// autoBlockScanBudgetPerTick limits torrents/files calls per scan on large queues.
	autoBlockScanBudgetPerTick = 20
)

// defaultMediaExtensions are the containers Sonarr/Radarr can actually import. A
// torrent holding none of these has nothing for *arr to do, whatever else is in it.
// Disc images are deliberately absent: see defaultBannedExtensions. So is .ts —
// a real video container, but one this setup's players cannot open, so a release
// delivered as .ts is as useless here as an executable.
var defaultMediaExtensions = []string{
	".mkv", ".mp4", ".avi", ".m4v", ".mov", ".wmv", ".mpg", ".mpeg", ".m2v",
	".m2ts", ".mts", ".flv", ".webm", ".ogm", ".ogv", ".divx", ".xvid",
	".rm", ".rmvb", ".vob", ".mk3d", ".asf", ".3gp",
}

// defaultBannedExtensions are executables, scripts, archives, disc images, and
// .ts. Archives, .iso and .ts are safe to list here only because a match alone
// never triggers: the torrent must also contain no importable media. .ts is the
// one entry that is not malware — it is banned because this setup's players
// cannot open a transport stream, which makes such a release equally unusable.
var defaultBannedExtensions = []string{
	".exe", ".msi", ".scr", ".bat", ".cmd", ".com", ".pif", ".vbs", ".vbe",
	".js", ".jse", ".wsf", ".wsh", ".hta", ".ps1", ".psm1", ".lnk", ".url",
	".reg", ".sys", ".dll", ".apk", ".dmg", ".pkg", ".jar",
	".iso", ".img", ".nrg", ".mdf", ".ts",
	".7z", ".zip", ".zipx", ".rar", ".arj", ".cab", ".gz", ".bz2", ".xz",
	".tar", ".lzh", ".001",
}

// autoBlockRules is the resolved extension sets and size floor used by classification.
type autoBlockRules struct {
	MediaExts     map[string]bool
	BannedExts    map[string]bool
	MinMediaBytes int64
}

// autoBlockSettings is the fully-defaulted runtime configuration for the scanner.
type autoBlockSettings struct {
	Mode         string
	Rules        autoBlockRules
	GraceSeconds int
	MaxPerHour   int
	LogPath      string
}

// enabled is true when the scanner should run at all (inputs: settings; output: whether to scan).
func (s autoBlockSettings) enabled() bool {
	return s.Mode != "" && s.Mode != autoBlockModeOff
}

// acts is true when a detection should result in a blocklist call rather than a record (inputs: settings; output: whether to call *arr).
func (s autoBlockSettings) acts() bool {
	return s.Mode == autoBlockModeAuto
}

// fileVerdict is the result of inspecting one torrent's file list.
type fileVerdict struct {
	TotalFiles  int
	MediaFiles  int
	BannedFiles []string // file names that matched the banned set, for the log and confirm dialog
}

// suspicious is true when the torrent has a known file list, nothing importable,
// and at least one banned file — the two independent signals must agree
// (inputs: verdict; output: whether to flag).
func (v fileVerdict) suspicious() bool {
	return v.TotalFiles > 0 && v.MediaFiles == 0 && len(v.BannedFiles) > 0
}

// fileExtLower returns the lowercased extension of a torrent-relative path (input: file name; output: ".ext" or empty).
func fileExtLower(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	return strings.ToLower(filepath.Ext(name))
}

// classifyTorrentFiles counts importable media above the size floor and collects
// banned file names (inputs: qB file list and resolved rules; output: verdict).
func classifyTorrentFiles(files []TorrentFile, r autoBlockRules) fileVerdict {
	v := fileVerdict{TotalFiles: len(files)}
	for _, f := range files {
		ext := fileExtLower(f.Name)
		if ext == "" {
			continue
		}
		if r.MediaExts[ext] && f.Size >= r.MinMediaBytes {
			v.MediaFiles++
		}
		if r.BannedExts[ext] {
			v.BannedFiles = append(v.BannedFiles, f.Name)
		}
	}
	return v
}

// extensionSet normalizes a list of extensions into a lookup set, tolerating
// entries written with or without a leading dot (input: extensions; output: set).
func extensionSet(exts []string) map[string]bool {
	out := make(map[string]bool, len(exts))
	for _, e := range exts {
		e = strings.ToLower(strings.TrimSpace(e))
		e = strings.TrimPrefix(e, "*")
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		out[e] = true
	}
	return out
}

// normalizeAutoBlockMode trims and lowercases a configured mode (input: raw string;
// output: valid mode and whether the input was recognized; unknown values disable the feature).
func normalizeAutoBlockMode(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return autoBlockModeOff, true
	case autoBlockModeOff:
		return autoBlockModeOff, true
	case autoBlockModeLog:
		return autoBlockModeLog, true
	case autoBlockModeFlag:
		return autoBlockModeFlag, true
	case autoBlockModeAuto:
		return autoBlockModeAuto, true
	default:
		return autoBlockModeOff, false
	}
}

// autoBlockSettingsFromConfig resolves config (with env override already applied)
// into runtime settings, substituting defaults for anything unset (inputs: config;
// output: settings and whether the configured mode was recognized).
func autoBlockSettingsFromConfig(cfg *Config) (autoBlockSettings, bool) {
	ab := AutoBlockConfig{}
	if cfg != nil && cfg.AutoBlock != nil {
		ab = *cfg.AutoBlock
	}
	mode, ok := normalizeAutoBlockMode(ab.Mode)

	media := defaultMediaExtensions
	if len(ab.MediaExtensions) > 0 {
		media = ab.MediaExtensions
	}
	banned := defaultBannedExtensions
	if len(ab.BannedExtensions) > 0 {
		banned = ab.BannedExtensions
	}

	minBytes := int64(defaultAutoBlockMinMediaBytes)
	if ab.MinMediaBytes > 0 {
		minBytes = ab.MinMediaBytes
	}
	grace := defaultAutoBlockGraceSeconds
	if ab.GraceSeconds > 0 {
		grace = ab.GraceSeconds
	}
	maxPerHour := defaultAutoBlockMaxPerHour
	if ab.MaxPerHour > 0 {
		maxPerHour = ab.MaxPerHour
	}

	return autoBlockSettings{
		Mode: mode,
		Rules: autoBlockRules{
			MediaExts:     extensionSet(media),
			BannedExts:    extensionSet(banned),
			MinMediaBytes: minBytes,
		},
		GraceSeconds: grace,
		MaxPerHour:   maxPerHour,
		LogPath:      strings.TrimSpace(ab.LogPath),
	}, ok
}

// Audit log actions.
const (
	autoBlockActionDetected     = "detected"        // suspicious, and a matching *arr queue row exists
	autoBlockActionNoQueueRow   = "detected_no_arr" // suspicious, but *arr no longer tracks it — not actionable
	autoBlockActionBlocklisted  = "blocklisted"
	autoBlockActionError        = "error"
	autoBlockActionRateCapped   = "skipped_rate_cap"
	autoBlockActionSessionStart = "session_start" // marks when qbitty was watching, so gaps in coverage are visible
)

// autoBlockLogEntry is one JSON line in the audit log; this is the evidence used
// to decide whether the rule is trustworthy enough to promote from log to auto.
type autoBlockLogEntry struct {
	Time     string `json:"time"`
	Mode     string `json:"mode"`
	Action   string `json:"action"`
	Hash     string `json:"hash,omitempty"`
	Name     string `json:"name,omitempty"`
	Category string `json:"category,omitempty"`
	// TotalFiles and MediaFiles are always emitted: "media_files":0 is the finding itself.
	TotalFiles  int      `json:"total_files"`
	MediaFiles  int      `json:"media_files"`
	BannedFiles []string `json:"banned_files,omitempty"`
	QueueID     int      `json:"queue_id,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// defaultAutoBlockLogPath returns $XDG_STATE_HOME/qbitty/autoblock.log, falling
// back to ~/.local/state (inputs: none; output: path or error when no home is known).
func defaultAutoBlockLogPath() (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); xdg != "" {
		return filepath.Join(xdg, "qbitty", "autoblock.log"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "qbitty", "autoblock.log"), nil
}

var autoBlockLogMu sync.Mutex

// appendAutoBlockLog appends one JSON line to the audit log, creating the
// directory on first write (inputs: log path override and entry; output: I/O error).
func appendAutoBlockLog(logPath string, e autoBlockLogEntry) error {
	if strings.TrimSpace(logPath) == "" {
		p, err := defaultAutoBlockLogPath()
		if err != nil {
			return err
		}
		logPath = p
	}
	if e.Time == "" {
		e.Time = time.Now().Format(time.RFC3339)
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	autoBlockLogMu.Lock()
	defer autoBlockLogMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// autoBlockStatusLabel is the status-column text shown in place of the qB/*arr
// status for a flagged torrent. ASCII and 8 bytes wide so the column's byte-based
// padding still aligns.
const autoBlockStatusLabel = "NO MEDIA"

// autoBlockMarksUI is true when the mode surfaces findings in the interface. Log
// mode deliberately does not: it is a silent recording pass, which is what makes it
// safe to leave running while you decide whether to trust the rule
// (input: active mode; output: whether the UI shows findings).
func autoBlockMarksUI(mode string) bool {
	return mode == autoBlockModeFlag || mode == autoBlockModeAuto
}

// autoBlockStatusOverride reports the status-column text for a torrent flagged as
// holding no importable media. The *arr pipeline status is exactly the misleading
// part for these torrents — Sonarr will happily report "Downloading" for a payload
// it can never import — so it is replaced rather than sat beside (inputs: whether
// flagged and the active mode; output: text, ANSI color, and whether to override).
func autoBlockStatusOverride(flagged bool, mode string) (string, string, bool) {
	if !flagged || !autoBlockMarksUI(mode) {
		return "", "", false
	}
	return autoBlockStatusLabel, redColor, true
}

// autoBlockEvidenceMaxFiles is how many banned file names a confirm dialog lists
// before summarizing the rest; enough to judge by, short enough to stay on screen.
const autoBlockEvidenceMaxFiles = 5

// autoBlockEvidenceLines renders the finding as dialog text, so the decision to
// blocklist is made against the actual file list rather than a bare warning
// (inputs: verdict; output: lines, empty when the verdict is not a finding).
func autoBlockEvidenceLines(v fileVerdict) []string {
	if !v.suspicious() {
		return nil
	}
	lines := []string{
		fmt.Sprintf("Flagged: %d file(s), no importable media.", v.TotalFiles),
	}
	shown := v.BannedFiles
	if len(shown) > autoBlockEvidenceMaxFiles {
		shown = shown[:autoBlockEvidenceMaxFiles]
	}
	for _, name := range shown {
		lines = append(lines, "  "+truncateName(filepath.Base(name), 60))
	}
	if rest := len(v.BannedFiles) - len(shown); rest > 0 {
		lines = append(lines, fmt.Sprintf("  ...and %d more", rest))
	}
	return lines
}

// autoBlockRateLimiter caps blocklist actions over a rolling hour. This is the last
// line of defence against a bad rule: if classification is ever wrong, the damage is
// bounded to MaxPerHour releases instead of the whole queue.
type autoBlockRateLimiter struct {
	mu    sync.Mutex
	times []time.Time
}

// allow consumes one slot when the rolling hour holds fewer than max actions. A cap
// of zero or less allows nothing, so a misconfigured cap fails closed
// (inputs: cap and current time; output: whether the action may proceed).
func (l *autoBlockRateLimiter) allow(max int, now time.Time) bool {
	if max <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-time.Hour)
	kept := l.times[:0]
	for _, t := range l.times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.times = kept
	if len(l.times) >= max {
		return false
	}
	l.times = append(l.times, now)
	return true
}
