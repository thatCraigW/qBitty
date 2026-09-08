package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const mb = 1 << 20

// defaultTestRules mirrors what an unconfigured install gets.
func defaultTestRules(t *testing.T) autoBlockRules {
	t.Helper()
	st, ok := autoBlockSettingsFromConfig(&Config{AutoBlock: &AutoBlockConfig{Mode: autoBlockModeLog}})
	if !ok {
		t.Fatal("log mode should be recognized")
	}
	return st.Rules
}

func f(name string, sizeMB int64) TorrentFile {
	return TorrentFile{Name: name, Size: sizeMB * mb}
}

func TestClassifyTorrentFiles(t *testing.T) {
	rules := defaultTestRules(t)

	// The Newsroom and Lioness cases are the real shapes from a live library: both
	// contain files on qBittorrent's own exclusion list (.txt) yet are perfectly good
	// releases. They are the regression guard against triggering on junk files.
	newsroom := []TorrentFile{f("RARBG.txt", 0)}
	for i := 0; i < 10; i++ {
		newsroom = append(newsroom, f(fmt.Sprintf("Newsroom.S01E%02d.mp4", i+1), 900))
	}
	for i := 0; i < 21; i++ {
		newsroom = append(newsroom, f(fmt.Sprintf("Subs/Newsroom.S01E%02d.srt", i+1), 0))
	}

	tests := []struct {
		name           string
		files          []TorrentFile
		wantSuspicious bool
		wantMedia      int
		wantBanned     int
	}{
		{
			name:           "season pack with txt and srt is clean",
			files:          newsroom,
			wantSuspicious: false, wantMedia: 10, wantBanned: 0,
		},
		{
			name:           "single episode is clean",
			files:          []TorrentFile{f("Lioness.2023.S03E05.1080p.WEB.H264-CAKES.mkv", 2400)},
			wantSuspicious: false, wantMedia: 1, wantBanned: 0,
		},
		{
			name:           "executable only is suspicious",
			files:          []TorrentFile{f("Movie.2024.1080p.x264.exe", 3)},
			wantSuspicious: true, wantMedia: 0, wantBanned: 1,
		},
		{
			name:           "double extension with undersized decoy is suspicious",
			files:          []TorrentFile{f("Movie.2024.1080p.mkv", 2), f("Movie.2024.1080p.mkv.exe", 4)},
			wantSuspicious: true, wantMedia: 0, wantBanned: 1,
		},
		{
			name:           "oversized real media alongside an exe is not suspicious",
			files:          []TorrentFile{f("Movie.2024.1080p.mkv", 2400), f("setup.exe", 4)},
			wantSuspicious: false, wantMedia: 1, wantBanned: 1,
		},
		{
			name:           "archive only is suspicious",
			files:          []TorrentFile{f("Movie.2024.1080p.rar", 1400), f("Movie.2024.1080p.r00", 1400)},
			wantSuspicious: true, wantMedia: 0, wantBanned: 1,
		},
		{
			name:           "disc image is suspicious",
			files:          []TorrentFile{f("Movie.2024.COMPLETE.BLURAY.iso", 40000)},
			wantSuspicious: true, wantMedia: 0, wantBanned: 1,
		},
		{
			name:           "empty file list yields no verdict",
			files:          nil,
			wantSuspicious: false, wantMedia: 0, wantBanned: 0,
		},
		{
			name:           "no media and no banned file is not suspicious",
			files:          []TorrentFile{f("readme.nfo", 0), f("cover.jpg", 0)},
			wantSuspicious: false, wantMedia: 0, wantBanned: 0,
		},
		{
			name:           "uppercase extensions still match",
			files:          []TorrentFile{f("MOVIE.2024.EXE", 3)},
			wantSuspicious: true, wantMedia: 0, wantBanned: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := classifyTorrentFiles(tc.files, rules)
			if got := v.suspicious(); got != tc.wantSuspicious {
				t.Fatalf("suspicious = %v, want %v (verdict %+v)", got, tc.wantSuspicious, v)
			}
			if v.MediaFiles != tc.wantMedia {
				t.Errorf("MediaFiles = %d, want %d", v.MediaFiles, tc.wantMedia)
			}
			if len(v.BannedFiles) != tc.wantBanned {
				t.Errorf("BannedFiles = %v, want %d", v.BannedFiles, tc.wantBanned)
			}
			if v.TotalFiles != len(tc.files) {
				t.Errorf("TotalFiles = %d, want %d", v.TotalFiles, len(tc.files))
			}
		})
	}
}

func TestFileExtLower(t *testing.T) {
	cases := map[string]string{
		"a/b/Movie.MKV":        ".mkv",
		"Movie.2024.mkv.exe":   ".exe",
		"Subs/eng.srt":         ".srt",
		"no_extension":         "",
		"dir\\Windows.Path.7z": ".7z",
		"":                     "",
	}
	for in, want := range cases {
		if got := fileExtLower(in); got != want {
			t.Errorf("fileExtLower(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtensionSetNormalization(t *testing.T) {
	set := extensionSet([]string{"exe", ".MSI", "*.7z", "  .zip  ", "", "*"})
	for _, want := range []string{".exe", ".msi", ".7z", ".zip"} {
		if !set[want] {
			t.Errorf("missing %q in %v", want, set)
		}
	}
	if len(set) != 4 {
		t.Errorf("unexpected entries: %v", set)
	}
}

func TestNormalizeAutoBlockMode(t *testing.T) {
	for _, in := range []string{"", "off", "LOG", " flag ", "auto"} {
		if _, ok := normalizeAutoBlockMode(in); !ok {
			t.Errorf("%q should be recognized", in)
		}
	}
	mode, ok := normalizeAutoBlockMode("blocklist-everything")
	if ok {
		t.Error("garbage mode should not be recognized")
	}
	if mode != autoBlockModeOff {
		t.Errorf("unrecognized mode must disable, got %q", mode)
	}
}

func TestAutoBlockSettingsFromConfig(t *testing.T) {
	st, _ := autoBlockSettingsFromConfig(nil)
	if st.enabled() {
		t.Error("nil config must not enable auto-block")
	}

	st, _ = autoBlockSettingsFromConfig(&Config{})
	if st.enabled() {
		t.Error("absent autoblock block must not enable auto-block")
	}

	st, _ = autoBlockSettingsFromConfig(&Config{AutoBlock: &AutoBlockConfig{Mode: "log"}})
	if !st.enabled() || st.acts() {
		t.Errorf("log mode should be enabled but not acting: %+v", st)
	}
	if st.Rules.MinMediaBytes != defaultAutoBlockMinMediaBytes ||
		st.GraceSeconds != defaultAutoBlockGraceSeconds ||
		st.MaxPerHour != defaultAutoBlockMaxPerHour {
		t.Errorf("defaults not applied: %+v", st)
	}
	if !st.Rules.BannedExts[".iso"] || !st.Rules.MediaExts[".mkv"] || st.Rules.MediaExts[".iso"] {
		t.Error("default extension sets are wrong")
	}

	st, _ = autoBlockSettingsFromConfig(&Config{AutoBlock: &AutoBlockConfig{
		Mode:             "auto",
		MinMediaBytes:    1234,
		GraceSeconds:     7,
		MaxPerHour:       2,
		BannedExtensions: []string{"foo"},
		MediaExtensions:  []string{"bar"},
	}})
	if !st.acts() {
		t.Error("auto mode should act")
	}
	if st.Rules.MinMediaBytes != 1234 || st.GraceSeconds != 7 || st.MaxPerHour != 2 {
		t.Errorf("overrides not applied: %+v", st)
	}
	if !st.Rules.BannedExts[".foo"] || st.Rules.BannedExts[".exe"] {
		t.Error("custom banned list should replace, not extend, the default")
	}
	if !st.Rules.MediaExts[".bar"] || st.Rules.MediaExts[".mkv"] {
		t.Error("custom media list should replace, not extend, the default")
	}
}

func TestAutoBlockStateReady(t *testing.T) {
	ready := []string{"downloading", "stalledDL", "pausedDL", "queuedDL", "uploading", "forcedDL"}
	notReady := []string{"metaDL", "forcedMetaDL", "allocating", "checkingResumeData", "moving", "unknown", "error", "missingFiles", ""}
	for _, s := range ready {
		if !autoBlockStateReady(s) {
			t.Errorf("%q should be ready", s)
		}
	}
	for _, s := range notReady {
		if autoBlockStateReady(s) {
			t.Errorf("%q should not be ready", s)
		}
	}
}

// --- scanner ---

func logModeSettings(t *testing.T) autoBlockSettings {
	t.Helper()
	st, _ := autoBlockSettingsFromConfig(&Config{AutoBlock: &AutoBlockConfig{Mode: autoBlockModeLog}})
	return st
}

func suspiciousTorrent(hash, name string, addedAgo time.Duration, now time.Time) Torrent {
	return Torrent{
		Hash: hash, Name: name, Category: arrCategorySonarr,
		State: "downloading", AddedOn: now.Add(-addedAgo).Unix(),
	}
}

func exeFiles() []TorrentFile { return []TorrentFile{f("payload.exe", 3)} }

func TestScanDetectsSuspiciousTorrentOnce(t *testing.T) {
	now := time.Now()
	st := logModeSettings(t)
	s := newAutoBlockScanner()
	tor := suspiciousTorrent("h1", "Bad.Release.S01E01", time.Hour, now)
	queue := []arrQueueRecord{{ID: 99, DownloadID: "h1", Title: "Bad.Release.S01E01"}}

	calls := 0
	fetch := func(hash string) ([]TorrentFile, error) { calls++; return exeFiles(), nil }

	got := s.scan(fetch, st, []Torrent{tor}, queue, nil, now)
	if len(got) != 1 {
		t.Fatalf("expected 1 detection, got %d", len(got))
	}
	if !got[0].HasQueue || got[0].QueueID != 99 {
		t.Errorf("queue row not matched: %+v", got[0])
	}
	if _, ok := s.flaggedVerdict("h1"); !ok {
		t.Error("torrent should be flagged")
	}

	// A second scan must neither refetch nor report the same torrent again.
	if got2 := s.scan(fetch, st, []Torrent{tor}, queue, nil, now); len(got2) != 0 {
		t.Errorf("detection repeated: %+v", got2)
	}
	if calls != 1 {
		t.Errorf("file list fetched %d times, want 1", calls)
	}
	if s.flaggedCount() != 1 {
		t.Errorf("flaggedCount = %d, want 1", s.flaggedCount())
	}
}

func TestScanRespectsGraceWindow(t *testing.T) {
	now := time.Now()
	st := logModeSettings(t)
	s := newAutoBlockScanner()
	fresh := suspiciousTorrent("h1", "Fresh", 5*time.Second, now)
	fetch := func(string) ([]TorrentFile, error) { t.Fatal("must not fetch inside grace window"); return nil, nil }

	if got := s.scan(fetch, st, []Torrent{fresh}, nil, nil, now); len(got) != 0 {
		t.Fatalf("detected inside grace window: %+v", got)
	}
}

func TestScanSkipsUnmanagedCategoryAndNotReadyState(t *testing.T) {
	now := time.Now()
	st := logModeSettings(t)
	s := newAutoBlockScanner()
	fetch := func(string) ([]TorrentFile, error) { t.Fatal("must not fetch"); return nil, nil }

	other := suspiciousTorrent("h1", "Manual", time.Hour, now)
	other.Category = "Movies"
	meta := suspiciousTorrent("h2", "NoMeta", time.Hour, now)
	meta.State = "metaDL"

	if got := s.scan(fetch, st, []Torrent{other, meta}, nil, nil, now); len(got) != 0 {
		t.Fatalf("unexpected detections: %+v", got)
	}
}

func TestScanTreatsEmptyFileListAsNoVerdict(t *testing.T) {
	now := time.Now()
	st := logModeSettings(t)
	s := newAutoBlockScanner()
	tor := suspiciousTorrent("h1", "Pending", time.Hour, now)

	empty := func(string) ([]TorrentFile, error) { return nil, nil }
	if got := s.scan(empty, st, []Torrent{tor}, nil, nil, now); len(got) != 0 {
		t.Fatalf("empty file list must not count as 'no media': %+v", got)
	}

	// Once metadata arrives, the same torrent is judged — proving no verdict was cached.
	queue := []arrQueueRecord{{ID: 1, DownloadID: "h1"}}
	got := s.scan(func(string) ([]TorrentFile, error) { return exeFiles(), nil }, st, []Torrent{tor}, queue, nil, now)
	if len(got) != 1 {
		t.Fatalf("expected detection after metadata arrived, got %d", len(got))
	}
}

func TestScanDoesNotCacheVerdictOnFetchError(t *testing.T) {
	now := time.Now()
	st := logModeSettings(t)
	s := newAutoBlockScanner()
	tor := suspiciousTorrent("h1", "Flaky", time.Hour, now)

	failing := func(string) ([]TorrentFile, error) { return nil, fmt.Errorf("boom") }
	if got := s.scan(failing, st, []Torrent{tor}, nil, nil, now); len(got) != 0 {
		t.Fatal("fetch error must not produce a detection")
	}
	queue := []arrQueueRecord{{ID: 1, DownloadID: "h1"}}
	if got := s.scan(func(string) ([]TorrentFile, error) { return exeFiles(), nil }, st, []Torrent{tor}, queue, nil, now); len(got) != 1 {
		t.Fatal("torrent should be retried after a transient fetch error")
	}
}

func TestScanReportsSuspiciousWithoutQueueRow(t *testing.T) {
	now := time.Now()
	st := logModeSettings(t)
	s := newAutoBlockScanner()
	tor := suspiciousTorrent("h1", "Orphan", time.Hour, now)

	got := s.scan(func(string) ([]TorrentFile, error) { return exeFiles(), nil }, st, []Torrent{tor}, nil, nil, now)
	if len(got) != 1 {
		t.Fatalf("expected 1 detection, got %d", len(got))
	}
	if got[0].HasQueue {
		t.Error("should report that *arr no longer tracks it")
	}
	if e := logEntryForDetection(got[0], st.Mode); e.Action != autoBlockActionNoQueueRow {
		t.Errorf("action = %q, want %q", e.Action, autoBlockActionNoQueueRow)
	}
}

func TestScanCleanTorrentIsNeverRefetched(t *testing.T) {
	now := time.Now()
	st := logModeSettings(t)
	s := newAutoBlockScanner()
	tor := suspiciousTorrent("h1", "Good.Release", time.Hour, now)

	calls := 0
	fetch := func(string) ([]TorrentFile, error) {
		calls++
		return []TorrentFile{f("Good.Release.mkv", 2400)}, nil
	}
	for i := 0; i < 3; i++ {
		if got := s.scan(fetch, st, []Torrent{tor}, nil, nil, now); len(got) != 0 {
			t.Fatalf("clean torrent detected: %+v", got)
		}
	}
	if calls != 1 {
		t.Errorf("clean torrent fetched %d times, want 1", calls)
	}
}

func TestScanBudgetLimitsFetchesPerTick(t *testing.T) {
	now := time.Now()
	st := logModeSettings(t)
	s := newAutoBlockScanner()

	var list []Torrent
	for i := 0; i < autoBlockScanBudgetPerTick+5; i++ {
		list = append(list, suspiciousTorrent(fmt.Sprintf("h%d", i), fmt.Sprintf("T%d", i), time.Hour, now))
	}
	calls := 0
	fetch := func(string) ([]TorrentFile, error) {
		calls++
		return []TorrentFile{f("clean.mkv", 2400)}, nil
	}
	s.scan(fetch, st, list, nil, nil, now)
	if calls != autoBlockScanBudgetPerTick {
		t.Errorf("fetched %d, want budget of %d", calls, autoBlockScanBudgetPerTick)
	}
	// The remainder is picked up on the following tick.
	s.scan(fetch, st, list, nil, nil, now)
	if calls != len(list) {
		t.Errorf("after second tick fetched %d, want %d", calls, len(list))
	}
}

func TestScanDisabledModeDoesNothing(t *testing.T) {
	now := time.Now()
	s := newAutoBlockScanner()
	off, _ := autoBlockSettingsFromConfig(nil)
	fetch := func(string) ([]TorrentFile, error) { t.Fatal("must not fetch when disabled"); return nil, nil }
	if got := s.scan(fetch, off, []Torrent{suspiciousTorrent("h1", "X", time.Hour, now)}, nil, nil, now); len(got) != 0 {
		t.Fatal("disabled scanner produced detections")
	}
}

func TestScanForgetsRemovedTorrents(t *testing.T) {
	now := time.Now()
	st := logModeSettings(t)
	s := newAutoBlockScanner()
	tor := suspiciousTorrent("h1", "Bad", time.Hour, now)
	fetch := func(string) ([]TorrentFile, error) { return exeFiles(), nil }

	if got := s.scan(fetch, st, []Torrent{tor}, nil, nil, now); len(got) != 1 {
		t.Fatal("expected initial detection")
	}
	// Torrent disappears from qBittorrent, then comes back: it must be judged afresh.
	s.scan(fetch, st, nil, nil, nil, now)
	if s.flaggedCount() != 0 {
		t.Errorf("flagged entry survived removal: %d", s.flaggedCount())
	}
	if got := s.scan(fetch, st, []Torrent{tor}, nil, nil, now); len(got) != 1 {
		t.Fatal("re-added torrent should be detected again")
	}
}

// --- audit log ---

func TestAppendAutoBlockLogWritesJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "autoblock.log")
	entries := []autoBlockLogEntry{
		{Mode: autoBlockModeLog, Action: autoBlockActionSessionStart},
		{Mode: autoBlockModeLog, Action: autoBlockActionDetected, Hash: "h1", Name: "Bad", Category: arrCategorySonarr, TotalFiles: 1, BannedFiles: []string{"payload.exe"}, QueueID: 7},
	}
	for _, e := range entries {
		if err := appendAutoBlockLog(path, e); err != nil {
			t.Fatal(err)
		}
	}

	fh, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()

	var decoded []autoBlockLogEntry
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		var e autoBlockLogEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("line is not valid JSON: %v", err)
		}
		if e.Time == "" {
			t.Error("entry missing timestamp")
		}
		decoded = append(decoded, e)
	}
	if len(decoded) != 2 {
		t.Fatalf("got %d lines, want 2", len(decoded))
	}
	if decoded[1].Hash != "h1" || decoded[1].QueueID != 7 || len(decoded[1].BannedFiles) != 1 {
		t.Errorf("second entry round-tripped wrong: %+v", decoded[1])
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("log permissions = %o, want 600", perm)
	}
}

func TestAutoBlockStatusOverride(t *testing.T) {
	tests := []struct {
		name    string
		flagged bool
		mode    string
		want    bool
	}{
		{"flag mode marks the row", true, autoBlockModeFlag, true},
		{"auto mode marks the row", true, autoBlockModeAuto, true},
		{"log mode stays silent in the UI", true, autoBlockModeLog, false},
		{"off mode stays silent in the UI", true, autoBlockModeOff, false},
		{"unflagged torrent is never marked", false, autoBlockModeFlag, false},
		{"unflagged torrent in auto mode", false, autoBlockModeAuto, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text, color, ok := autoBlockStatusOverride(tc.flagged, tc.mode)
			if ok != tc.want {
				t.Fatalf("override = %v, want %v", ok, tc.want)
			}
			if !ok {
				if text != "" || color != "" {
					t.Errorf("no override should yield empty text/color, got %q/%q", text, color)
				}
				return
			}
			if text != autoBlockStatusLabel {
				t.Errorf("text = %q, want %q", text, autoBlockStatusLabel)
			}
			if color != redColor {
				t.Errorf("color = %q, want red", color)
			}
		})
	}
}

// The status column is padded with %-*s, which counts bytes; a multi-byte label
// would render narrower than the column and misalign every row after it.
func TestAutoBlockStatusLabelFitsStatusColumn(t *testing.T) {
	const statusWidth = 12 // must match refreshTorrentList
	if n := len(autoBlockStatusLabel); n > statusWidth {
		t.Errorf("label is %d bytes, wider than the %d-wide status column", n, statusWidth)
	}
	for i := 0; i < len(autoBlockStatusLabel); i++ {
		if autoBlockStatusLabel[i] >= 0x80 {
			t.Fatalf("label %q is not ASCII; byte padding would misalign the column", autoBlockStatusLabel)
		}
	}
}

func TestAutoBlockEvidenceLines(t *testing.T) {
	t.Run("not suspicious yields nothing", func(t *testing.T) {
		v := fileVerdict{TotalFiles: 3, MediaFiles: 1, BannedFiles: []string{"a.exe"}}
		if got := autoBlockEvidenceLines(v); got != nil {
			t.Errorf("expected no lines for an importable torrent, got %v", got)
		}
	})

	t.Run("lists banned files under the cap", func(t *testing.T) {
		v := fileVerdict{TotalFiles: 2, BannedFiles: []string{"dir/payload.exe", "readme.zip"}}
		got := autoBlockEvidenceLines(v)
		if len(got) != 3 {
			t.Fatalf("expected header + 2 files, got %d: %v", len(got), got)
		}
		if !strings.Contains(got[0], "2 file(s)") || !strings.Contains(got[0], "no importable media") {
			t.Errorf("header does not state the finding: %q", got[0])
		}
		// Directory components are stripped so long release paths stay readable.
		if !strings.Contains(got[1], "payload.exe") || strings.Contains(got[1], "dir/") {
			t.Errorf("expected basename only, got %q", got[1])
		}
	})

	t.Run("summarizes the tail past the cap", func(t *testing.T) {
		var banned []string
		for i := 0; i < autoBlockEvidenceMaxFiles+3; i++ {
			banned = append(banned, fmt.Sprintf("f%d.exe", i))
		}
		got := autoBlockEvidenceLines(fileVerdict{TotalFiles: len(banned), BannedFiles: banned})
		if len(got) != autoBlockEvidenceMaxFiles+2 {
			t.Fatalf("expected header + %d files + summary, got %d: %v", autoBlockEvidenceMaxFiles, len(got), got)
		}
		if last := got[len(got)-1]; !strings.Contains(last, "and 3 more") {
			t.Errorf("tail not summarized: %q", last)
		}
	})

	t.Run("long names are truncated for the dialog", func(t *testing.T) {
		long := strings.Repeat("x", 200) + ".exe"
		got := autoBlockEvidenceLines(fileVerdict{TotalFiles: 1, BannedFiles: []string{long}})
		if len(got[1]) > 70 {
			t.Errorf("file line not truncated: %d chars", len(got[1]))
		}
	})
}

func autoModeSettings(t *testing.T, maxPerHour int) autoBlockSettings {
	t.Helper()
	st, ok := autoBlockSettingsFromConfig(&Config{AutoBlock: &AutoBlockConfig{
		Mode: autoBlockModeAuto, MaxPerHour: maxPerHour,
	}})
	if !ok {
		t.Fatal("auto mode should be recognized")
	}
	return st
}

// detectionFor builds the detection a scan would produce for a suspicious torrent.
func detectionFor(hash string, hasQueue bool, now time.Time) autoBlockDetection {
	return autoBlockDetection{
		Torrent:  suspiciousTorrent(hash, "Bad."+hash, time.Hour, now),
		Verdict:  fileVerdict{TotalFiles: 1, BannedFiles: []string{"payload.exe"}},
		QueueID:  7,
		HasQueue: hasQueue,
	}
}

func TestRateLimiterCapsPerRollingHour(t *testing.T) {
	var l autoBlockRateLimiter
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !l.allow(3, now) {
			t.Fatalf("action %d should be allowed", i)
		}
	}
	if l.allow(3, now) {
		t.Error("fourth action within the hour should be capped")
	}
	// Slots free as the hour rolls forward.
	if !l.allow(3, now.Add(61*time.Minute)) {
		t.Error("slot should be free once the first action ages out")
	}
}

func TestRateLimiterZeroCapFailsClosed(t *testing.T) {
	var l autoBlockRateLimiter
	if l.allow(0, time.Now()) || l.allow(-1, time.Now()) {
		t.Error("a non-positive cap must allow nothing")
	}
}

func TestActOnDetectionLogAndFlagModesDoNotAct(t *testing.T) {
	now := time.Now()
	for _, mode := range []string{autoBlockModeLog, autoBlockModeFlag} {
		st := logModeSettings(t)
		st.Mode = mode
		var l autoBlockRateLimiter
		called := false
		blocklist := func(string, string, string) error { called = true; return nil }

		e, retry := actOnDetection(detectionFor("h1", true, now), st, blocklist, &l, now)
		if called {
			t.Errorf("%s mode called *arr", mode)
		}
		if retry {
			t.Errorf("%s mode should not defer", mode)
		}
		if e.Action != autoBlockActionDetected {
			t.Errorf("%s mode action = %q, want %q", mode, e.Action, autoBlockActionDetected)
		}
	}
}

func TestActOnDetectionAutoModeBlocklists(t *testing.T) {
	now := time.Now()
	st := autoModeSettings(t, 5)
	var l autoBlockRateLimiter
	var gotCategory, gotHash, gotName string
	blocklist := func(category, hash, name string) error {
		gotCategory, gotHash, gotName = category, hash, name
		return nil
	}

	d := detectionFor("h1", true, now)
	e, retry := actOnDetection(d, st, blocklist, &l, now)
	if retry {
		t.Error("a completed blocklist should not be deferred")
	}
	if e.Action != autoBlockActionBlocklisted {
		t.Errorf("action = %q, want %q", e.Action, autoBlockActionBlocklisted)
	}
	if gotCategory != arrCategorySonarr || gotHash != "h1" || gotName != d.Torrent.Name {
		t.Errorf("blocklist called with (%q, %q, %q)", gotCategory, gotHash, gotName)
	}
	// The evidence must survive into the audit record, not just the outcome.
	if e.TotalFiles != 1 || e.MediaFiles != 0 || len(e.BannedFiles) != 1 {
		t.Errorf("evidence missing from entry: %+v", e)
	}
}

func TestActOnDetectionWithoutQueueRowNeverCallsArr(t *testing.T) {
	now := time.Now()
	st := autoModeSettings(t, 5)
	var l autoBlockRateLimiter
	blocklist := func(string, string, string) error {
		t.Fatal("*arr must not be called for a torrent it no longer tracks")
		return nil
	}

	e, retry := actOnDetection(detectionFor("h1", false, now), st, blocklist, &l, now)
	if retry {
		t.Error("an unactionable detection should not be deferred")
	}
	if e.Action != autoBlockActionNoQueueRow {
		t.Errorf("action = %q, want %q", e.Action, autoBlockActionNoQueueRow)
	}
}

func TestActOnDetectionRecordsBlocklistFailure(t *testing.T) {
	now := time.Now()
	st := autoModeSettings(t, 5)
	var l autoBlockRateLimiter
	blocklist := func(string, string, string) error { return fmt.Errorf("sonarr returned 500") }

	e, retry := actOnDetection(detectionFor("h1", true, now), st, blocklist, &l, now)
	if retry {
		t.Error("a failed blocklist is not retried; it stays flagged for manual action")
	}
	if e.Action != autoBlockActionError || !strings.Contains(e.Error, "500") {
		t.Errorf("entry = %+v, want an error action carrying the cause", e)
	}
}

func TestActOnDetectionDefersPastTheHourlyCap(t *testing.T) {
	now := time.Now()
	st := autoModeSettings(t, 2)
	var l autoBlockRateLimiter
	calls := 0
	blocklist := func(string, string, string) error { calls++; return nil }

	for i := 0; i < 2; i++ {
		if _, retry := actOnDetection(detectionFor(fmt.Sprintf("h%d", i), true, now), st, blocklist, &l, now); retry {
			t.Fatalf("detection %d should be within the cap", i)
		}
	}

	capped := detectionFor("h3", true, now)
	e, retry := actOnDetection(capped, st, blocklist, &l, now)
	if !retry {
		t.Error("a capped detection must be deferred, not dropped")
	}
	if e.Action != autoBlockActionRateCapped {
		t.Errorf("action = %q, want %q", e.Action, autoBlockActionRateCapped)
	}
	if calls != 2 {
		t.Errorf("*arr called %d times, want 2 — the cap must be enforced before the call", calls)
	}

	// Once the hour rolls forward, the same detection goes through.
	if _, retry := actOnDetection(capped, st, blocklist, &l, now.Add(61*time.Minute)); retry {
		t.Error("deferred detection should proceed after the hour rolls forward")
	}
	if calls != 3 {
		t.Errorf("*arr called %d times after the cap freed, want 3", calls)
	}
}

func TestScannerDeferredDetectionsRoundTrip(t *testing.T) {
	now := time.Now()
	s := newAutoBlockScanner()
	d := detectionFor("h1", true, now)
	s.deferDetection(d)

	got := s.takeDeferred()
	if len(got) != 1 || got[0].Torrent.Hash != "h1" {
		t.Fatalf("takeDeferred = %+v, want the deferred detection", got)
	}
	if again := s.takeDeferred(); len(again) != 0 {
		t.Errorf("takeDeferred returned %+v twice; it must clear", again)
	}
}

func TestScannerDropsDeferredDetectionForRemovedTorrent(t *testing.T) {
	now := time.Now()
	st := autoModeSettings(t, 5)
	s := newAutoBlockScanner()
	s.deferDetection(detectionFor("h1", true, now))

	// A scan with the torrent gone prunes it, so auto mode cannot blocklist a
	// release that is no longer in qBittorrent.
	s.scan(func(string) ([]TorrentFile, error) { return nil, nil }, st, nil, nil, nil, now)
	if got := s.takeDeferred(); len(got) != 0 {
		t.Errorf("deferred detection survived removal: %+v", got)
	}
}

func TestForgetDropsDeferredDetection(t *testing.T) {
	s := newAutoBlockScanner()
	s.deferDetection(detectionFor("h1", true, time.Now()))
	s.forget("h1")
	if got := s.takeDeferred(); len(got) != 0 {
		t.Errorf("forget left a deferred detection: %+v", got)
	}
}

func TestDefaultExtensionListsDoNotOverlap(t *testing.T) {
	r := defaultTestRules(t)
	for ext := range r.MediaExts {
		if r.BannedExts[ext] {
			t.Errorf("%s is in both the media and banned lists; media wins in classifyTorrentFiles, so the ban would never fire", ext)
		}
	}
}

func TestTransportStreamReleaseIsFlagged(t *testing.T) {
	r := defaultTestRules(t)
	// A .ts release is real video, but unplayable in this setup, so it must be
	// treated exactly like a payload with no media at all.
	v := classifyTorrentFiles([]TorrentFile{
		f("Some.Release.S01E01.ts", 900),
		f("Some.Release.S01E02.ts", 880),
	}, r)
	if v.MediaFiles != 0 {
		t.Errorf("MediaFiles = %d, want 0 — .ts must not count as importable media", v.MediaFiles)
	}
	if !v.suspicious() {
		t.Errorf("a .ts-only release should be flagged: %+v", v)
	}
	if len(v.BannedFiles) != 2 {
		t.Errorf("BannedFiles = %v, want both .ts files", v.BannedFiles)
	}
}

func TestMixedTransportStreamAndPlayableMediaIsNotFlagged(t *testing.T) {
	r := defaultTestRules(t)
	// A pack containing a real .mkv alongside a .ts extra is still importable, so
	// the two-signal rule must leave it alone.
	v := classifyTorrentFiles([]TorrentFile{
		f("Some.Release.S01E01.mkv", 1200),
		f("extras/behind.the.scenes.ts", 300),
	}, r)
	if v.MediaFiles != 1 {
		t.Errorf("MediaFiles = %d, want 1", v.MediaFiles)
	}
	if v.suspicious() {
		t.Errorf("a release with playable media must not be flagged: %+v", v)
	}
}

func TestRelatedTransportStreamContainersStillCountAsMedia(t *testing.T) {
	r := defaultTestRules(t)
	// Only .ts was banned; .m2ts and .mts were left alone deliberately.
	for _, name := range []string{"disc.m2ts", "clip.mts"} {
		v := classifyTorrentFiles([]TorrentFile{f(name, 900)}, r)
		if v.MediaFiles != 1 {
			t.Errorf("%s: MediaFiles = %d, want 1", name, v.MediaFiles)
		}
	}
}
