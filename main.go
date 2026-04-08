package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/awesome-gocui/gocui"
)

const (
	resetColor  = "\033[0m"
	redColor    = "\033[31m"
	greenColor  = "\033[32m"
	yellowColor = "\033[33m"
	blueColor   = "\033[34m"
	cyanColor   = "\033[36m"
	greyMuted   = "\033[38;5;240m" // labels on status line
	whiteBright = "\033[97m"       // values on status line

	viewAPIError        = "apiError"
	apiRetryIntervalSec = 10
	// arrQueuePollInterval is how often Sonarr/Radarr GET /api/v3/queue runs (torrent list still polls qB every 1s).
	arrQueuePollInterval = 10 * time.Second
)

var (
	viewLeft      = "torrentList"
	viewDetails   = "details"
	viewShortcuts = "shortcuts" // shortcut hints row below torrent panel (stats live on torrent Footer / bottom border)
	viewOverlay      = "overlay"
	torrents         []Torrent
	filteredTorrents []Torrent
	torrentsMu       sync.RWMutex
	currentSelection int
	nameScrollOffset int // horizontal scroll into long names (rune index into selected torrent name)
	filterStatus     string
	filterCategory   string
	detailsVisible   bool
	detailsTab       = 5
	apiClient        *QBClient

	contentEditMode         bool
	contentFileSelection    int
	contentNameScrollOffset int // horizontal scroll for selected file name on Content tab (rune index)
	contentFileCache        []TorrentFile
	contentFileCacheHash    string

	arrSonarrClient *ArrHTTPClient // non-nil when config provides Sonarr URL + API key
	arrRadarrClient *ArrHTTPClient // non-nil when config provides Radarr URL + API key

	arrQueueMu         sync.RWMutex
	sonarrQueueRecords []arrQueueRecord // snapshot from last GET /api/v3/queue (Sonarr)
	radarrQueueRecords []arrQueueRecord // snapshot from last GET /api/v3/queue (Radarr)
)

// roundedFrameRunes matches lazygit's gui.Border "rounded" (─ │ ╭ ╮ ╰ ╯); assign to View.FrameRunes for framed panels.
var roundedFrameRunes = []rune{'─', '│', '╭', '╮', '╰', '╯'}

// apiErrorState holds plain-English overlay text and retry countdown for connection issues.
type apiErrorState struct {
	Title     string
	Lines     []string
	Footnote  string
	AutoRetry bool
	Countdown int
}

var (
	apiErrMu           sync.Mutex
	apiErr             *apiErrorState
	apiRecoveryRunning int32
)

// retryFootnote returns countdown text or a connecting message for the API error banner.
func retryFootnote(st *apiErrorState) string {
	if st.Countdown > 0 {
		return fmt.Sprintf("Retrying in %ds…", st.Countdown)
	}
	return "Reconnecting…"
}

// setAPIError records a user-facing API failure, clears the torrent list, and shows the centered modal copy.
func setAPIError(err error) {
	if err == nil {
		return
	}
	class := classifyQBAPIError(err)
	title, lines, auto := apiErrorModalContent(class, err)
	var foot string
	cd := 0
	if auto {
		cd = apiRetryIntervalSec
		foot = fmt.Sprintf("Retrying in %ds…", cd)
	} else {
		foot = "Press r to retry after fixing username/password."
	}
	apiErrMu.Lock()
	apiErr = &apiErrorState{
		Title:     title,
		Lines:     lines,
		Footnote:  foot,
		AutoRetry: auto,
		Countdown: cd,
	}
	apiErrMu.Unlock()

	torrentsMu.Lock()
	torrents = nil
	applyFilters()
	torrentsMu.Unlock()
}

// clearAPIError removes the API error overlay state.
func clearAPIError() {
	apiErrMu.Lock()
	apiErr = nil
	apiErrMu.Unlock()
}

// applyArrClientsFromConfig sets optional Sonarr/Radarr HTTP clients from cfg (inputs: loaded config).
func applyArrClientsFromConfig(cfg *Config) {
	if cfg == nil {
		arrSonarrClient, arrRadarrClient = nil, nil
		return
	}
	if arrCredentialsUsable(cfg.SonarrURL, cfg.SonarrAPIKey) {
		arrSonarrClient = NewArrHTTPClient(cfg.SonarrURL, cfg.SonarrAPIKey)
	} else {
		arrSonarrClient = nil
	}
	if arrCredentialsUsable(cfg.RadarrURL, cfg.RadarrAPIKey) {
		arrRadarrClient = NewArrHTTPClient(cfg.RadarrURL, cfg.RadarrAPIKey)
	} else {
		arrRadarrClient = nil
	}
}

// refreshArrQueueCaches refetches Sonarr/Radarr /api/v3/queue when configured; logs errors and leaves prior cache on failure (inputs: none; output: updates sonarrQueueRecords and radarrQueueRecords).
func refreshArrQueueCaches() {
	arrQueueMu.Lock()
	defer arrQueueMu.Unlock()
	if arrSonarrClient != nil {
		if recs, err := arrSonarrClient.FetchAllQueueRecords(); err == nil {
			sonarrQueueRecords = recs
		}
		// Optional integration: on failure keep prior cache; do not log (avoids TUI stderr spam when *arr is down or misconfigured).
	} else {
		sonarrQueueRecords = nil
	}
	if arrRadarrClient != nil {
		if recs, err := arrRadarrClient.FetchAllQueueRecords(); err == nil {
			radarrQueueRecords = recs
		}
	} else {
		radarrQueueRecords = nil
	}
}

// tryRecoverAPISync reloads config, logs in, and fetches torrents — updates or clears apiErr on failure/success.
func tryRecoverAPISync() {
	cfg, err := LoadConfig()
	if err != nil {
		setAPIError(err)
		return
	}
	applyArrClientsFromConfig(cfg)
	if err := apiClient.Login(cfg.Username, cfg.Password); err != nil {
		setAPIError(err)
		return
	}
	newT, err := apiClient.GetTorrents()
	if err != nil {
		setAPIError(err)
		return
	}
	clearAPIError()
	torrentsMu.Lock()
	torrents = newT
	applyFilters()
	torrentsMu.Unlock()
	refreshArrQueueCaches()
}

// triggerAPIRetry runs a manual reconnect in the background and refreshes the UI when done.
func triggerAPIRetry(g *gocui.Gui) {
	if !atomic.CompareAndSwapInt32(&apiRecoveryRunning, 0, 1) {
		return
	}
	go func() {
		defer atomic.StoreInt32(&apiRecoveryRunning, 0)
		tryRecoverAPISync()
		g.Update(func(gui *gocui.Gui) error {
			refreshDetailsPane(gui)
			return refreshUI(gui)
		})
	}()
}

func main() {
	jsonDump := flag.Bool("dump-json", false, "Fetch torrents info and output raw JSON")
	wizardFlag := flag.Bool("wizard", false, "Interactive setup when qBittorrent URL, username, or password is missing (same as QBITTY_WIZARD=1)")
	flag.Parse()

	cfg, err := mergeConfigFromFileAndEnv()
	if err != nil {
		log.Fatal(err)
	}
	if qbConfigIncomplete(cfg) {
		if !(*wizardFlag || wizardEnvEnabled()) {
			log.Fatal(validateRequiredQB(cfg))
		}
		cfg, err = runFirstLaunchWizard(cfg)
		if err != nil {
			log.Fatalf("Setup failed: %v", err)
		}
		if err := validateRequiredQB(cfg); err != nil {
			log.Fatal(err)
		}
	}
	applyArrClientsFromConfig(cfg)

	apiClient, err = NewQBClientFromConfig(cfg)
	if err != nil {
		log.Fatalf("Failed to create API client: %v", err)
	}

	if *jsonDump {
		if err := apiClient.Login(cfg.Username, cfg.Password); err != nil {
			log.Fatalf("Failed to login: %v", err)
		}
		jsonData, err := apiClient.GetTorrentsRaw()
		if err != nil {
			log.Fatalf("Failed to fetch torrents info: %v", err)
		}
		fmt.Println(string(jsonData))
		os.Exit(0)
	}

	if err := apiClient.Login(cfg.Username, cfg.Password); err != nil {
		setAPIError(err)
	} else {
		initialTorrents, ferr := apiClient.GetTorrents()
		if ferr != nil {
			setAPIError(ferr)
		} else {
			torrentsMu.Lock()
			torrents = initialTorrents
			applyFilters()
			torrentsMu.Unlock()
			refreshArrQueueCaches()
		}
	}

	g, err := gocui.NewGui(gocui.Output256, true)
	if err != nil {
		log.Panicln(err)
	}
	g.AfterDraw = overlayScrollbars
	defer g.Close()

	torrentTicker := time.NewTicker(time.Second)
	defer torrentTicker.Stop()
	arrQueueTicker := time.NewTicker(arrQueuePollInterval)
	defer arrQueueTicker.Stop()

	// Use a channel to signal exit
	done := make(chan struct{})

	// Start a goroutine to refresh torrent data periodically
	go func() {
		for {
			select {
			case <-torrentTicker.C:
				apiErrMu.Lock()
				st := apiErr
				var shouldRecover bool
				if st != nil && st.AutoRetry {
					if atomic.LoadInt32(&apiRecoveryRunning) == 0 {
						if st.Countdown > 0 {
							st.Countdown--
						}
						st.Footnote = retryFootnote(st)
						shouldRecover = st.Countdown == 0
					} else {
						st.Footnote = "Reconnecting…"
					}
				}
				apiErrMu.Unlock()

				if st != nil {
					if shouldRecover && atomic.CompareAndSwapInt32(&apiRecoveryRunning, 0, 1) {
						go func() {
							defer atomic.StoreInt32(&apiRecoveryRunning, 0)
							tryRecoverAPISync()
							g.Update(func(gui *gocui.Gui) error {
								refreshDetailsPane(gui)
								return refreshUI(gui)
							})
						}()
					} else {
						g.Update(func(gui *gocui.Gui) error {
							return refreshUI(gui)
						})
					}
					continue
				}

				newTorrents, err := apiClient.GetTorrents()
				if err != nil {
					g.Update(func(gui *gocui.Gui) error {
						setAPIError(err)
						return refreshUI(gui)
					})
					continue
				}

				torrentsMu.Lock()
				torrents = newTorrents
				applyFilters()
				torrentsMu.Unlock()

				g.Update(func(gui *gocui.Gui) error {
					refreshDetailsPane(g)
					return refreshUI(g)
				})
			case <-arrQueueTicker.C:
				refreshArrQueueCaches()
				g.Update(func(gui *gocui.Gui) error {
					return refreshUI(gui)
				})
			case <-done:
				return
			}
		}
	}()

	g.SetManagerFunc(layout)

	if err := keybindings(g, apiClient); err != nil {
		log.Panicln(err)
	}

	if err := g.MainLoop(); err != nil && !errors.Is(err, gocui.ErrQuit) {
		log.Panicln(err)
	}

	// Signal goroutine to exit
	close(done)
}

func layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	// Torrent bottom border on row maxY-2; shortcuts view shares y0=maxY-2 so hints land on maxY-1.
	listY1 := maxY - 2
	if detailsVisible {
		listY1 = maxY * 2 / 5
	}

	if v, err := g.SetView(viewLeft, 0, 0, maxX-1, listY1, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Title = "Torrents"
		v.Highlight = true
		v.Editable = false
		v.SelBgColor = gocui.ColorBlue
		v.SelFgColor = gocui.ColorWhite
		refreshTorrentList(g, v)
		scrollTorrentListSelectionIntoView(v)
		_ = v.SetCursor(0, currentSelection+1)
	}
	if lv, lerr := g.View(viewLeft); lerr == nil {
		lv.Footer = ""
		lv.FooterSpans = torrentFooterSpans()
		lv.FrameRunes = roundedFrameRunes
	}

	if detailsVisible {
		// Share bottom row y=maxY-2 with the shortcuts view (same as torrents when details are hidden) so there is no blank line above the hint bar.
		detailsY1 := maxY - 2
		if listY1+1 >= detailsY1 {
			detailsY1 = listY1 + 3
		}
		detailsMainY1 := detailsY1
		if v, err := g.SetView(viewDetails, 0, listY1+1, maxX-1, detailsMainY1, 0); err != nil {
			if !errors.Is(err, gocui.ErrUnknownView) {
				return err
			}
			v.Title = "Details"
			v.Wrap = true
			refreshDetailsPane(g)
		}
		if dv, derr := g.View(viewDetails); derr == nil {
			dv.Title = "Details"
			dv.FrameRunes = roundedFrameRunes
		}
	} else {
		g.DeleteView(viewDetails)
	}

	v, err := g.SetView(viewShortcuts, 0, maxY-2, maxX-1, maxY, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		return err
	}
	v.Frame = false
	v.FgColor = gocui.ColorBlue
	v.BgColor = gocui.ColorDefault
	drawBottomPanel(v)

	if err := layoutAPIErrorOverlay(g, maxX, maxY); err != nil {
		return err
	}

	if _, err := g.View(viewOverlay); err != nil {
		g.SetCurrentView(viewLeft)
	}

	return nil
}

// layoutAPIErrorOverlay draws a centered modal-style panel for API connectivity/auth errors, or removes it when healthy.
func layoutAPIErrorOverlay(g *gocui.Gui, maxX, maxY int) error {
	apiErrMu.Lock()
	st := apiErr
	apiErrMu.Unlock()

	if st == nil {
		if _, err := g.View(viewAPIError); err == nil {
			_ = g.DeleteView(viewAPIError)
		}
		return nil
	}

	width := 58
	if width > maxX-4 {
		width = maxX - 4
	}
	if width < 30 {
		width = maxX - 2
		if width < 20 {
			width = 20
		}
	}
	innerLines := 1 + len(st.Lines) + 2
	height := innerLines
	if height > maxY-4 {
		height = maxY - 4
	}
	if height < 5 {
		height = 5
	}
	x0 := (maxX - width) / 2
	y0 := (maxY - height) / 2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	x1 := x0 + width - 1
	y1 := y0 + height - 1
	if x1 >= maxX {
		x1 = maxX - 1
	}
	if y1 >= maxY {
		y1 = maxY - 1
	}

	v, err := g.SetView(viewAPIError, x0, y0, x1, y1, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		return err
	}
	v.FrameRunes = roundedFrameRunes
	v.Wrap = true
	v.Title = st.Title
	v.Clear()
	for _, ln := range st.Lines {
		fmt.Fprintf(v, " %s\n", ln)
	}
	fmt.Fprintln(v)
	fmt.Fprintf(v, " %s%s%s", yellowColor, st.Footnote, resetColor)
	return nil
}

// nameWidthForTorrentListInner returns the Name column width from the torrent view inner width (input: innerW).
func nameWidthForTorrentListInner(innerW int) int {
	const (
		statusWidth     = 12
		progressWidth   = 9
		dlSpeedWidth    = 12
		ulSpeedWidth    = 12
		etaWidth        = 6
		sizeWidth       = 10
		seedsPeersWidth = 10
		padding         = 8
	)
	fixed := statusWidth + progressWidth + dlSpeedWidth + ulSpeedWidth + etaWidth + sizeWidth + seedsPeersWidth + padding
	nameWidth := innerW - fixed
	if nameWidth < 20 {
		nameWidth = 20
	}
	return nameWidth
}

// torrentNameCell returns a padded name field for one row: ellipsis truncation, or horizontal slice when selected and long (inputs: full name, column width, scroll offset in runes, selected; output: padded string of width colWidth).
func torrentNameCell(full string, colWidth int, scroll int, selected bool) string {
	rs := []rune(full)
	if len(rs) <= colWidth {
		return fmt.Sprintf("%-*s", colWidth, string(rs))
	}
	if !selected {
		return fmt.Sprintf("%-*s", colWidth, truncateName(full, colWidth-1))
	}
	maxScroll := len(rs) - colWidth
	s := scroll
	if s < 0 {
		s = 0
	}
	if s > maxScroll {
		s = maxScroll
	}
	vis := string(rs[s : s+colWidth])
	return fmt.Sprintf("%-*s", colWidth, vis)
}

// scrollNameColumn moves the name viewport by delta runes when the selected name overflows; returns true if the list should redraw (inputs: g, delta; output: whether offset changed).
func scrollNameColumn(g *gocui.Gui, delta int) bool {
	t := getSelectedTorrent()
	if t == nil {
		return false
	}
	lv, err := g.View(viewLeft)
	if err != nil {
		return false
	}
	innerW, _ := lv.Size()
	nw := nameWidthForTorrentListInner(innerW)
	rs := []rune(t.Name)
	if len(rs) <= nw {
		return false
	}
	maxScroll := len(rs) - nw
	prev := nameScrollOffset
	nameScrollOffset += delta
	if nameScrollOffset < 0 {
		nameScrollOffset = 0
	}
	if nameScrollOffset > maxScroll {
		nameScrollOffset = maxScroll
	}
	return nameScrollOffset != prev
}

// contentTabNameWidth returns the file name column width in the Content tab for the given details view (input: v; output: width in cells).
func contentTabNameWidth(v *gocui.View) int {
	viewW, _ := v.Size()
	const (
		sizeW = 10
		progW = 7
		prioW = 8
	)
	fixedW := sizeW + progW + prioW + 4
	nameW := viewW - fixedW
	if nameW < 20 {
		nameW = 20
	}
	return nameW
}

// scrollContentFileNameColumn moves the file name viewport on the Content tab; returns true if the details pane should redraw (inputs: g, delta; output: whether offset changed).
func scrollContentFileNameColumn(g *gocui.Gui, delta int) bool {
	v, err := g.View(viewDetails)
	if err != nil {
		return false
	}
	if len(contentFileCache) == 0 || contentFileSelection < 0 || contentFileSelection >= len(contentFileCache) {
		return false
	}
	nameW := contentTabNameWidth(v)
	f := contentFileCache[contentFileSelection]
	rs := []rune(f.Name)
	if len(rs) <= nameW {
		return false
	}
	maxScroll := len(rs) - nameW
	prev := contentNameScrollOffset
	contentNameScrollOffset += delta
	if contentNameScrollOffset < 0 {
		contentNameScrollOffset = 0
	}
	if contentNameScrollOffset > maxScroll {
		contentNameScrollOffset = maxScroll
	}
	return contentNameScrollOffset != prev
}

// scrollTorrentListSelectionIntoView sets the torrent list view origin so the header + selected row stay in the visible area (input: v).
func scrollTorrentListSelectionIntoView(v *gocui.View) {
	torrentsMu.RLock()
	n := len(filteredTorrents)
	torrentsMu.RUnlock()
	lineCount := 1 + n
	_, vh := v.Size()
	if vh < 1 {
		return
	}
	// Size().Y counts inner rows; one fewer line is actually drawable with the
	// framed title, matching linesPosOnScreen/cursor visibility (avoids selection one row past the last painted line).
	visibleH := vh - 1
	if visibleH < 1 {
		visibleH = 1
	}
	var selLine int
	if n == 0 {
		selLine = 0
	} else {
		selLine = currentSelection + 1
		if selLine > lineCount-1 {
			selLine = lineCount - 1
		}
	}
	maxOy := lineCount - visibleH
	if maxOy < 0 {
		maxOy = 0
	}
	oy := 0
	if selLine >= visibleH {
		oy = selLine - visibleH + 1
	}
	if oy > maxOy {
		oy = maxOy
	}
	_ = v.SetOrigin(0, oy)
}

// overlayScrollbars paints scroll thumbs on the right frame edge of scrollable panes after views draw (input: g); uses no extra columns.
func overlayScrollbars(g *gocui.Gui) error {
	if err := overlayScrollThumbOnViewFrame(g, viewLeft, true); err != nil {
		return err
	}
	if detailsVisible {
		if err := overlayScrollThumbOnViewFrame(g, viewDetails, false); err != nil {
			return err
		}
	}
	return nil
}

// overlayScrollThumbOnViewFrame replaces the right vertical border with │ / █ when content overflows (input: g, view name, torrentSubtractsHeaderForViewport).
func overlayScrollThumbOnViewFrame(g *gocui.Gui, viewName string, torrentList bool) error {
	v, err := g.View(viewName)
	if err != nil || !v.Frame {
		return nil
	}
	_, y0, x1, y1 := v.Dimensions()
	edgeTop, edgeBot := y0+1, y1-1
	h := edgeBot - edgeTop + 1
	if h < 1 {
		return nil
	}
	var totalLines, oy, visibleLines int
	if torrentList {
		torrentsMu.RLock()
		n := len(filteredTorrents)
		torrentsMu.RUnlock()
		totalLines = 1 + n
		oy, _ = v.Origin()
		_, vh := v.Size()
		if vh < 1 {
			return nil
		}
		visibleLines = vh - 1
		if visibleLines < 1 {
			visibleLines = 1
		}
	} else {
		totalLines = v.LinesHeight()
		oy, _ = v.Origin()
		_, vh := v.Size()
		if vh < 1 {
			return nil
		}
		visibleLines = vh
	}
	if totalLines <= visibleLines {
		return nil
	}
	maxOy := totalLines - visibleLines
	if maxOy < 0 {
		maxOy = 0
	}
	thumbH := (visibleLines*h + totalLines - 1) / totalLines
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > h {
		thumbH = h
	}
	thumbStart := 0
	if maxOy > 0 && h > thumbH {
		thumbStart = (oy*(h-thumbH) + maxOy/2) / maxOy
	}
	if thumbStart < 0 {
		thumbStart = 0
	}
	if thumbStart+thumbH > h {
		thumbStart = h - thumbH
	}
	trackFg := g.FrameColor
	if v.FrameColor != gocui.ColorDefault {
		trackFg = v.FrameColor
	}
	if g.Highlight && g.CurrentView() == v {
		trackFg = g.SelFrameColor
	}
	thumbFg := gocui.ColorWhite
	bg := g.BgColor
	for i := 0; i < h; i++ {
		y := edgeTop + i
		ch := '│'
		fg := trackFg
		if i >= thumbStart && i < thumbStart+thumbH {
			ch = '█'
			fg = thumbFg
		}
		if err := g.SetRune(x1, y, ch, fg, bg); err != nil {
			return err
		}
	}
	return nil
}

func refreshTorrentList(g *gocui.Gui, v *gocui.View) {
	innerW, _ := v.Size()
	nameWidth := nameWidthForTorrentListInner(innerW)

	const (
		statusWidth     = 12
		progressWidth   = 9
		dlSpeedWidth    = 12
		ulSpeedWidth    = 12
		etaWidth        = 6
		sizeWidth       = 10
		seedsPeersWidth = 10
	)

	v.Clear()

	torrentsMu.RLock()
	localTorrents := filteredTorrents
	fStatus := filterStatus
	fCategory := filterCategory
	torrentsMu.RUnlock()

	arrQueueMu.RLock()
	sq := sonarrQueueRecords
	rq := radarrQueueRecords
	arrQueueMu.RUnlock()

	if currentSelection >= 0 && currentSelection < len(localTorrents) {
		rs := []rune(localTorrents[currentSelection].Name)
		if len(rs) > nameWidth {
			maxScroll := len(rs) - nameWidth
			if nameScrollOffset > maxScroll {
				nameScrollOffset = maxScroll
			}
		} else {
			nameScrollOffset = 0
		}
	}

	title := "Torrents"
	if fStatus != "" || fCategory != "" {
		parts := []string{}
		if fStatus != "" {
			parts = append(parts, fStatus)
		}
		if fCategory != "" {
			parts = append(parts, fCategory)
		}
		title += " [" + strings.Join(parts, ", ") + "]"
	}
	v.Title = title

	fmt.Fprintf(v, "%s%-*s %-*s %-*s %-*s %-*s %-*s %-*s %-*s%s\n",
		"\033[38;5;245m",
		nameWidth, "Name",
		progressWidth, "Progress%",
		statusWidth, "Status",
		dlSpeedWidth, "DownSpeed",
		ulSpeedWidth, "UpSpeed",
		etaWidth, "ETA",
		sizeWidth, "Size",
		seedsPeersWidth, "Seeds/Peers",
		resetColor)

	for i, t := range localTorrents {
		downloadSpeedColor := colorForSpeed(t.DownloadSpeed)
		uploadSpeedColor := colorForSpeed(t.UploadSpeed)

		nameCol := torrentNameCell(t.Name, nameWidth, nameScrollOffset, i == currentSelection)
		dlSpeedStr := formatSpeed(t.DownloadSpeed)
		uploadSpeedStr := formatSpeed(t.UploadSpeed)
		sizeStr := formatSize(t.Size)
		etaStr := formatETA(t.ETA)

		displayStatus, displayStatusColor := torrentStatusColumnText(t, sq, rq)

		// Determine if status is moving, stopped, or stalled (use qB state for ETA/speed, not *arr label)
		statusLower := strings.ToLower(cleanStatusString(t.State))

		// Only override when status is exactly 'moving', 'stopped', or 'stalled'
		if statusLower != "downloading" && statusLower != "stalled" {
			etaStr = "--"
			dlSpeedStr = "--"
			uploadSpeedStr = "--"
		}

		progressCol := fmt.Sprintf("%*s", progressWidth, fmt.Sprintf("%.2f%%", t.Progress*100))
		statusCol := fmt.Sprintf("%s%-*s%s", displayStatusColor, statusWidth, displayStatus, resetColor)
		dlSpeedCol := fmt.Sprintf("%s%-*s%s", downloadSpeedColor, dlSpeedWidth, dlSpeedStr, resetColor)
		ulSpeedCol := fmt.Sprintf("%s%-*s%s", uploadSpeedColor, ulSpeedWidth, uploadSpeedStr, resetColor)
		etaCol := fmt.Sprintf("%-*s", etaWidth, etaStr)
		sizeCol := fmt.Sprintf("%-*s", sizeWidth, sizeStr)
		seedsPeersCol := fmt.Sprintf("%-*d/%d", seedsPeersWidth-5, t.Seeds, t.Peers)

		fmt.Fprintf(v, "%s %s %s %s %s %s %s %s\n", nameCol, progressCol, statusCol, dlSpeedCol, ulSpeedCol, etaCol, sizeCol, seedsPeersCol)
	}
}

func truncateName(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}
	if maxLen > 3 {
		return name[:maxLen-3] + "..."
	}
	return name[:maxLen]
}

func formatSpeed(bps int64) string {
	if bps <= 0 {
		return "0 B/s"
	}
	units := []string{"B/s", "KB/s", "MB/s", "GB/s", "TB/s"}
	value := float64(bps)
	var i int
	for i = 0; i < len(units)-1 && value >= 1024; i++ {
		value /= 1024
	}
	result := fmt.Sprintf("%.2f %s", value, units[i])
	if len(result) > 12 {
		return result[:12]
	}
	return result
}

func formatSize(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(bytes)
	var i int
	for i = 0; i < len(units)-1 && value >= 1024; i++ {
		value /= 1024
	}
	return fmt.Sprintf("%.2f %s", value, units[i])
}

func cleanStatusString(status string) string {
	switch status {
	case "downloading":
		return "downloading"
	case "uploading", "stalledUP", "queuedUP":
		return "seeding"
	case "stoppedDL", "stoppedUP", "stopped":
		return "stopped"
	case "pausedDL", "pausedUP":
		return "paused"
	case "error":
		return "error"
	case "missingFiles":
		return "missing files"
	default:
		return status
	}
}

func colorForState(state string) string {
	switch state {
	case "downloading":
		return yellowColor
	case "uploading", "stalledUP", "queuedUP":
		return greenColor
	case "pausedDL", "pausedUP":
		return blueColor
	case "error":
		return redColor
	default:
		return resetColor
	}
}

func colorForSpeed(speed int64) string {
	if speed > 0 {
		return greenColor
	}
	return redColor
}

func formatETA(seconds int64) string {
	if seconds <= 0 {
		return "--"
	}
	if seconds < 3600 {
		minutes := (seconds + 59) / 60 // round up to nearest minute
		return fmt.Sprintf("%dm", minutes)
	} else if seconds < 86400 {
		hours := float64(seconds) / 3600
		return fmt.Sprintf("%.1fh", hours) // round to nearest decimal hour
	} else if seconds < 604800 {
		days := float64(seconds) / 86400
		return fmt.Sprintf("%.0fd", days) // round to nearest day
	} else {
		return ">1w"
	}
}

func keybindings(g *gocui.Gui, client *QBClient) error {
	if err := g.SetKeybinding(viewLeft, gocui.KeyArrowDown, gocui.ModNone, cursorDown); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, gocui.KeyArrowUp, gocui.ModNone, cursorUp); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 'q', gocui.ModNone, quit); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 's', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return toggleTorrent(g, client)
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 'd', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return confirmDeleteTorrent(g, client)
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 'b', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return confirmBlocklistTorrent(g, client)
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, '+', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return changePriority(g, client, true)
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, '-', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return changePriority(g, client, false)
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 'a', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return showInputDialog(g, "Add Torrent URL", func(input string) error {
			return client.AddTorrentURL(input)
		})
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 'm', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return showInputDialog(g, "Add Magnet Link", func(input string) error {
			return client.AddTorrentURL(input)
		})
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, gocui.KeySpace, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		detailsVisible = !detailsVisible
		if !detailsVisible {
			clearContentEditForTabChange(g)
		}
		return nil
	}); err != nil {
		return err
	}
	for i := 1; i <= 5; i++ {
		tab := i
		if err := g.SetKeybinding(viewLeft, rune('0'+tab), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			if !detailsVisible {
				detailsVisible = true
			}
			prevTab := detailsTab
			detailsTab = tab
			if prevTab != detailsTab {
				clearContentEditForTabChange(g)
			}
			refreshDetailsPane(g)
			return nil
		}); err != nil {
			return err
		}
	}
	if err := g.SetKeybinding(viewLeft, gocui.KeyArrowLeft, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if detailsVisible && detailsTab == 5 && scrollContentFileNameColumn(g, -1) {
			refreshDetailsPane(g)
			return nil
		}
		if detailsVisible && detailsTab > 1 {
			prevTab := detailsTab
			detailsTab--
			if prevTab != detailsTab {
				clearContentEditForTabChange(g)
			}
			refreshDetailsPane(g)
			return nil
		}
		if scrollNameColumn(g, -1) {
			return refreshUI(g)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, gocui.KeyArrowRight, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if detailsVisible && detailsTab == 5 && scrollContentFileNameColumn(g, 1) {
			refreshDetailsPane(g)
			return nil
		}
		if detailsVisible && detailsTab < 5 {
			prevTab := detailsTab
			detailsTab++
			if prevTab != detailsTab {
				clearContentEditForTabChange(g)
			}
			refreshDetailsPane(g)
			return nil
		}
		if scrollNameColumn(g, 1) {
			return refreshUI(g)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 'f', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return showFilterDialog(g)
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 'r', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		apiErrMu.Lock()
		has := apiErr != nil
		apiErrMu.Unlock()
		if has {
			triggerAPIRetry(g)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 'e', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return toggleContentEditMode(g)
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 'p', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return contentCyclePriority(g, client)
	}); err != nil {
		return err
	}
	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func cursorDown(g *gocui.Gui, v *gocui.View) error {
	if detailsVisible && detailsTab == 5 && contentEditMode {
		return contentCursorDown(g)
	}
	torrentsMu.RLock()
	count := len(filteredTorrents)
	torrentsMu.RUnlock()
	if currentSelection < count-1 {
		currentSelection++
		nameScrollOffset = 0
		contentNameScrollOffset = 0
		if contentEditMode {
			contentEditMode = false
			contentFileSelection = 0
			if vv, err := g.View(viewDetails); err == nil {
				_ = vv.SetOrigin(0, 0)
			}
		}
		refreshDetailsPane(g)
		return refreshUI(g)
	}
	return nil
}

func cursorUp(g *gocui.Gui, v *gocui.View) error {
	if detailsVisible && detailsTab == 5 && contentEditMode {
		return contentCursorUp(g)
	}
	if currentSelection > 0 {
		currentSelection--
		nameScrollOffset = 0
		contentNameScrollOffset = 0
		if contentEditMode {
			contentEditMode = false
			contentFileSelection = 0
			if vv, err := g.View(viewDetails); err == nil {
				_ = vv.SetOrigin(0, 0)
			}
		}
		refreshDetailsPane(g)
		return refreshUI(g)
	}
	return nil
}

// clearContentEditForTabChange exits file-edit mode when leaving the Content tab or switching tabs (input: g).
func clearContentEditForTabChange(g *gocui.Gui) {
	if !contentEditMode {
		return
	}
	contentEditMode = false
	contentFileSelection = 0
	if vv, err := g.View(viewDetails); err == nil {
		_ = vv.SetOrigin(0, 0)
	}
}

// contentCursorDown moves selection down one row in Content edit mode (input: g; output: nil).
func contentCursorDown(g *gocui.Gui) error {
	if len(contentFileCache) == 0 {
		return nil
	}
	t := getSelectedTorrent()
	if t == nil || t.Hash != contentFileCacheHash {
		return nil
	}
	if contentFileSelection < len(contentFileCache)-1 {
		contentFileSelection++
		contentNameScrollOffset = 0
		refreshDetailsPane(g)
	}
	return nil
}

// contentCursorUp moves selection up one row in Content edit mode (input: g; output: nil).
func contentCursorUp(g *gocui.Gui) error {
	if contentFileSelection > 0 {
		contentFileSelection--
		contentNameScrollOffset = 0
		refreshDetailsPane(g)
	}
	return nil
}

// scrollContentFileIntoView adjusts the details view origin so the selected file row stays visible (inputs: v, fileCount).
func scrollContentFileIntoView(v *gocui.View, fileCount int) {
	if fileCount == 0 {
		return
	}
	const linesBeforeFiles = 4 // tab row + 2 blank lines + column header
	selLine := linesBeforeFiles + contentFileSelection
	_, vh := v.Size()
	if vh < 1 {
		return
	}
	_, oy := v.Origin()
	if selLine < oy {
		_ = v.SetOrigin(0, selLine)
		return
	}
	if selLine >= oy+vh {
		_ = v.SetOrigin(0, selLine-vh+1)
	}
}

// nextFilePriority returns the next qBittorrent file priority in cycle: skip → normal → high → maximum (input: current priority).
func nextFilePriority(p int) int {
	switch p {
	case 0:
		return 1
	case 1:
		return 6
	case 6:
		return 7
	case 7:
		return 0
	default:
		return 1
	}
}

// contentCyclePriority applies next priority to the selected file in Content edit mode (inputs: g, client; output: nil).
func contentCyclePriority(g *gocui.Gui, client *QBClient) error {
	if !contentEditMode {
		return nil
	}
	t := getSelectedTorrent()
	if t == nil {
		return nil
	}
	if len(contentFileCache) == 0 || contentFileCacheHash != t.Hash {
		return nil
	}
	if contentFileSelection < 0 || contentFileSelection >= len(contentFileCache) {
		return nil
	}
	f := contentFileCache[contentFileSelection]
	newP := nextFilePriority(f.Priority)
	if err := client.SetFilePriority(t.Hash, f.Index, newP); err != nil {
		log.Printf("file priority: %v", err)
		return nil
	}
	refreshDetailsPane(g)
	return nil
}

// toggleContentEditMode toggles Content tab file selection mode when on that tab (input: g; output: error from refresh).
func toggleContentEditMode(g *gocui.Gui) error {
	if !detailsVisible || detailsTab != 5 {
		return nil
	}
	t := getSelectedTorrent()
	if t == nil {
		return nil
	}
	if len(contentFileCache) == 0 || contentFileCacheHash != t.Hash {
		refreshDetailsPane(g)
		return nil
	}
	contentEditMode = !contentEditMode
	if !contentEditMode {
		contentFileSelection = 0
		contentNameScrollOffset = 0
		if vv, err := g.View(viewDetails); err == nil {
			_ = vv.SetOrigin(0, 0)
		}
	} else {
		if contentFileSelection >= len(contentFileCache) {
			contentFileSelection = len(contentFileCache) - 1
		}
		if contentFileSelection < 0 {
			contentFileSelection = 0
		}
	}
	refreshDetailsPane(g)
	return nil
}

// refreshUI refreshes the torrent list, shortcuts bar, API error overlay, and cursor (inputs: g; output: error from view ops).
func refreshUI(g *gocui.Gui) error {
	v, err := g.View(viewLeft)
	if err != nil {
		return err
	}
	refreshTorrentList(g, v)
	v.Footer = ""
	v.FooterSpans = torrentFooterSpans()
	if sv, serr := g.View(viewShortcuts); serr == nil {
		drawBottomPanel(sv)
	}
	mx, my := g.Size()
	if err := layoutAPIErrorOverlay(g, mx, my); err != nil {
		return err
	}
	scrollTorrentListSelectionIntoView(v)
	return v.SetCursor(0, currentSelection+1)
}

// getSelectedTorrent returns a copy of the currently selected torrent, or nil if none.
func getSelectedTorrent() *Torrent {
	torrentsMu.RLock()
	defer torrentsMu.RUnlock()
	if currentSelection >= 0 && currentSelection < len(filteredTorrents) {
		t := filteredTorrents[currentSelection]
		return &t
	}
	return nil
}

// isStoppedOrPaused returns true if the torrent state indicates it is not active.
func isStoppedOrPaused(state string) bool {
	switch state {
	case "stoppedDL", "stoppedUP", "stopped", "pausedDL", "pausedUP":
		return true
	}
	return false
}

// toggleTorrent stops a running torrent or starts a stopped one.
func toggleTorrent(g *gocui.Gui, client *QBClient) error {
	t := getSelectedTorrent()
	if t == nil {
		return nil
	}
	if isStoppedOrPaused(t.State) {
		if err := client.StartTorrents(t.Hash); err != nil {
			log.Printf("start error: %v", err)
		}
	} else {
		if err := client.StopTorrents(t.Hash); err != nil {
			log.Printf("stop error: %v", err)
		}
	}
	return nil
}

// changePriority increases or decreases the selected torrent's queue priority.
func changePriority(g *gocui.Gui, client *QBClient, increase bool) error {
	t := getSelectedTorrent()
	if t == nil {
		return nil
	}
	var err error
	if increase {
		err = client.IncreasePriority(t.Hash)
	} else {
		err = client.DecreasePriority(t.Hash)
	}
	if err != nil {
		log.Printf("priority error: %v", err)
	}
	return nil
}

// confirmDeleteTorrent shows a y/n confirmation overlay before deleting.
func confirmDeleteTorrent(g *gocui.Gui, client *QBClient) error {
	t := getSelectedTorrent()
	if t == nil {
		return nil
	}
	name := truncateName(t.Name, 40)
	hash := t.Hash
	return showConfirmDialog(g, fmt.Sprintf("Delete '%s'? (y/n)", name), func() error {
		return client.DeleteTorrent(hash, false)
	})
}

// truncateForDialog shortens text for modal width (input: string and max runes; output: possibly truncated string).
func truncateForDialog(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

// showInfoDialog shows a one-shot message overlay; dismiss with Enter or Esc (inputs: title and body text).
func showInfoDialog(g *gocui.Gui, title, body string) error {
	maxX, maxY := g.Size()
	lines := strings.Split(body, "\n")
	width := 20
	for _, line := range lines {
		w := len(line) + 4
		if w > width {
			width = w
		}
	}
	if width > maxX-4 {
		width = maxX - 4
	}
	height := len(lines) + 2
	if height > maxY-4 {
		height = maxY - 4
	}
	if height < 3 {
		height = 3
	}
	x0 := maxX/2 - width/2
	y0 := maxY/2 - height/2
	v, err := g.SetView(viewOverlay, x0, y0, x0+width, y0+height, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		return err
	}
	v.FrameRunes = roundedFrameRunes
	v.Title = title
	v.Clear()
	for _, line := range lines {
		if len(line) > width-2 {
			line = truncateForDialog(line, width-4)
		}
		fmt.Fprintf(v, " %s\n", line)
	}
	g.SetKeybinding(viewOverlay, gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return closeOverlay(g)
	})
	g.SetKeybinding(viewOverlay, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return closeOverlay(g)
	})
	_, err = g.SetCurrentView(viewOverlay)
	return err
}

// refreshTorrentsFromAPI reloads the torrent list from qBittorrent and refreshes the UI (inputs: g and API client; output: error from API or refresh).
func refreshTorrentsFromAPI(g *gocui.Gui, client *QBClient) error {
	newT, err := client.GetTorrents()
	if err != nil {
		return err
	}
	torrentsMu.Lock()
	torrents = newT
	applyFilters()
	torrentsMu.Unlock()
	refreshArrQueueCaches()
	refreshDetailsPane(g)
	return refreshUI(g)
}

// confirmQBRemoveOnly asks to delete the torrent in qBittorrent only; explains that qB has no release blocklist (inputs: g, client, hash, extra context line).
func confirmQBRemoveOnly(g *gocui.Gui, client *QBClient, hash string, contextLine string) error {
	msg := "Remove torrent from qBittorrent only? (y/n)\n" +
		"qBittorrent cannot block future grabs; only Sonarr/Radarr blocklists do that.\n" +
		contextLine
	return showConfirmDialog(g, msg, func() error {
		if err := client.DeleteTorrent(hash, false); err != nil {
			return err
		}
		return refreshTorrentsFromAPI(g, client)
	})
}

// confirmBlocklistTorrent blocklists via *arr when configured for the category; otherwise offers qBittorrent-only removal (inputs: g, qB client).
func confirmBlocklistTorrent(g *gocui.Gui, client *QBClient) error {
	t := getSelectedTorrent()
	if t == nil {
		return nil
	}
	hash := t.Hash
	name := t.Name

	switch t.Category {
	case arrCategorySonarr:
		if arrSonarrClient != nil {
			msg := "Sonarr: blocklist this release and remove from client? (y/n)"
			return showConfirmDialog(g, msg, func() error {
				if err := blocklistTorrentViaArr(arrSonarrClient, hash, name); err != nil {
					return err
				}
				return refreshTorrentsFromAPI(g, client)
			})
		}
		return confirmQBRemoveOnly(g, client, hash,
			"Sonarr API not configured — set SONARR_URL and SONARR_API_KEY for *arr blocklist.")
	case arrCategoryRadarr:
		if arrRadarrClient != nil {
			msg := "Radarr: blocklist this release and remove from client? (y/n)"
			return showConfirmDialog(g, msg, func() error {
				if err := blocklistTorrentViaArr(arrRadarrClient, hash, name); err != nil {
					return err
				}
				return refreshTorrentsFromAPI(g, client)
			})
		}
		return confirmQBRemoveOnly(g, client, hash,
			"Radarr API not configured — set RADARR_URL and RADARR_API_KEY for *arr blocklist.")
	default:
		return confirmQBRemoveOnly(g, client, hash,
			"Category is not Sonarr/Radarr — configure *arr + matching category to blocklist there.")
	}
}

// showConfirmDialog displays a y/n confirmation overlay; message may contain newlines; calls onConfirm if user presses y.
func showConfirmDialog(g *gocui.Gui, message string, onConfirm func() error) error {
	maxX, maxY := g.Size()
	lines := strings.Split(message, "\n")
	width := 30
	for _, line := range lines {
		w := len(line) + 4
		if w > width {
			width = w
		}
	}
	if width > maxX-4 {
		width = maxX - 4
	}
	if width < 30 {
		width = 30
	}
	viewH := len(lines) + 2
	if viewH < 3 {
		viewH = 3
	}
	if viewH > maxY-4 {
		viewH = maxY - 4
	}
	x0 := maxX/2 - width/2
	y0 := (maxY-viewH)/2
	if y0 < 0 {
		y0 = 0
	}

	v, err := g.SetView(viewOverlay, x0, y0, x0+width, y0+viewH, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		return err
	}
	v.FrameRunes = roundedFrameRunes
	v.Title = "Confirm"
	v.Clear()
	for _, line := range lines {
		show := line
		if len(show) > width-2 {
			show = truncateForDialog(show, width-4)
		}
		fmt.Fprintf(v, " %s\n", show)
	}

	g.SetKeybinding(viewOverlay, 'y', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if err := onConfirm(); err != nil {
			log.Printf("action error: %v", err)
			if err2 := closeOverlay(g); err2 != nil {
				return err2
			}
			return showInfoDialog(g, "Error", truncateForDialog(err.Error(), 400))
		}
		return closeOverlay(g)
	})
	g.SetKeybinding(viewOverlay, 'n', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return closeOverlay(g)
	})
	g.SetKeybinding(viewOverlay, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return closeOverlay(g)
	})

	_, err = g.SetCurrentView(viewOverlay)
	return err
}

// showInputDialog displays a text input overlay; calls onSubmit with the entered text on Enter.
func showInputDialog(g *gocui.Gui, title string, onSubmit func(string) error) error {
	maxX, maxY := g.Size()
	width := 70
	if width > maxX-4 {
		width = maxX - 4
	}
	x0 := maxX/2 - width/2
	y0 := maxY/2 - 1

	v, err := g.SetView(viewOverlay, x0, y0, x0+width, y0+2, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		return err
	}
	v.FrameRunes = roundedFrameRunes
	v.Title = title
	v.Editable = true
	v.Clear()

	g.SetKeybinding(viewOverlay, gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		input := strings.TrimSpace(v.Buffer())
		if input != "" {
			if err := onSubmit(input); err != nil {
				log.Printf("submit error: %v", err)
			}
		}
		return closeOverlay(g)
	})
	g.SetKeybinding(viewOverlay, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return closeOverlay(g)
	})

	_, err = g.SetCurrentView(viewOverlay)
	return err
}

// refreshDetailsPane re-renders the details pane with the currently selected torrent and active tab.
func refreshDetailsPane(g *gocui.Gui) {
	if !detailsVisible {
		return
	}
	v, err := g.View(viewDetails)
	if err != nil {
		return
	}
	t := getSelectedTorrent()
	if t == nil {
		v.Clear()
		v.SetOrigin(0, 0)
		fmt.Fprintln(v, " No torrent selected")
		v.Footer = ""
		v.FooterSpans = nil
		v.FooterSpansLeft = nil
		return
	}
	renderDetailsTab(v, apiClient, t.Hash, detailsTab)
	applyDetailsPaneFooter(v)
}

// contentTabFooterLeftSpans returns left-aligned shortcut segments for the Content tab bottom border (input: none; output: spans, uses contentEditMode).
func contentTabFooterLeftSpans() []gocui.FooterSpan {
	y, b := gocui.ColorYellow, gocui.ColorBlue
	if contentEditMode {
		return []gocui.FooterSpan{
			{Text: "↑↓", Fg: y},
			{Text: " move  ", Fg: b},
			{Text: "p", Fg: y},
			{Text: " priority  ", Fg: b},
			{Text: "e", Fg: y},
			{Text: " exit  ", Fg: b},
			{Text: "←→", Fg: y},
			{Text: " name", Fg: b},
		}
	}
	return []gocui.FooterSpan{
		{Text: "e", Fg: y},
		{Text: " edit  ", Fg: b},
		{Text: "←→", Fg: y},
		{Text: " scroll name", Fg: b},
	}
}

// applyDetailsPaneFooter sets the details frame bottom-border footers for the Content tab (left shortcuts, right file total); clears on other tabs (input: v = details view).
func applyDetailsPaneFooter(v *gocui.View) {
	v.Footer = ""
	v.FooterSpans = nil
	v.FooterSpansLeft = nil
	if detailsTab != 5 {
		return
	}
	t := getSelectedTorrent()
	if t == nil || contentFileCacheHash != t.Hash || len(contentFileCache) == 0 {
		return
	}
	grey := gocui.Get256Color(240)
	white := gocui.ColorWhite
	v.FooterSpans = []gocui.FooterSpan{
		{Text: " Total: ", Fg: grey},
		{Text: fmt.Sprintf("%d", len(contentFileCache)), Fg: white},
		{Text: " files", Fg: grey},
	}
	v.FooterSpansLeft = contentTabFooterLeftSpans()
}

// renderDetailsTab clears the overlay and draws the tab header + content for the given tab number.
func renderDetailsTab(v *gocui.View, client *QBClient, hash string, tab int) {
	v.Clear()
	v.SetOrigin(0, 0)
	v.SetCursor(0, 0)
	if tab != 5 {
		contentNameScrollOffset = 0
	}
	writeTabHeaders(v, tab)

	switch tab {
	case 1:
		renderGeneralTab(v, client, hash)
	case 2:
		renderTrackersTab(v, client, hash)
	case 3:
		renderPeersTab(v, client, hash)
	case 4:
		renderHTTPSourcesTab(v, client, hash)
	case 5:
		renderContentTab(v, client, hash)
	}
}

// writeTabHeaders renders the numbered tab bar with the active tab highlighted.
func writeTabHeaders(v *gocui.View, activeTab int) {
	grey := "\033[38;5;245m"
	tabs := []string{"General", "Trackers", "Peers", "HTTP Sources", "Content"}
	for i, name := range tabs {
		num := i + 1
		if num == activeTab {
			fmt.Fprintf(v, " %s%d %s%s", yellowColor, num, name, resetColor)
		} else {
			fmt.Fprintf(v, " %s%d %s%s", grey, num, name, resetColor)
		}
		if i < len(tabs)-1 {
			fmt.Fprintf(v, " %s│%s", grey, resetColor)
		}
	}
	fmt.Fprintln(v)
	fmt.Fprintln(v)
}

func renderGeneralTab(v *gocui.View, client *QBClient, hash string) {
	t := getSelectedTorrent()
	if t == nil {
		fmt.Fprintln(v, " No torrent selected")
		return
	}

	props, err := client.GetTorrentProperties(hash)
	if err != nil {
		fmt.Fprintf(v, " Error fetching properties: %v\n", err)
		return
	}

	label := "\033[38;5;245m"
	val := resetColor

	fmt.Fprintf(v, " %sName%s           %s\n", label, val, t.Name)
	fmt.Fprintf(v, " %sHash%s           %s\n", label, val, t.Hash)
	fmt.Fprintf(v, " %sState%s          %s\n", label, val, cleanStatusString(t.State))
	fmt.Fprintf(v, " %sProgress%s       %.2f%%\n", label, val, t.Progress*100)
	fmt.Fprintln(v)

	fmt.Fprintf(v, " %sDown Speed%s     %s (avg: %s)\n", label, val, formatSpeed(t.DownloadSpeed), formatSpeed(props.DlSpeedAvg))
	fmt.Fprintf(v, " %sUp Speed%s       %s (avg: %s)\n", label, val, formatSpeed(t.UploadSpeed), formatSpeed(props.UpSpeedAvg))
	fmt.Fprintf(v, " %sETA%s            %s\n", label, val, formatETA(t.ETA))
	fmt.Fprintln(v)

	fmt.Fprintf(v, " %sTotal Size%s     %s\n", label, val, formatSize(props.TotalSize))
	fmt.Fprintf(v, " %sDownloaded%s     %s\n", label, val, formatSize(props.TotalDownloaded))
	fmt.Fprintf(v, " %sUploaded%s       %s\n", label, val, formatSize(props.TotalUploaded))
	fmt.Fprintf(v, " %sWasted%s         %s\n", label, val, formatSize(props.TotalWasted))
	fmt.Fprintf(v, " %sRatio%s          %.3f\n", label, val, props.ShareRatio)
	fmt.Fprintln(v)

	fmt.Fprintf(v, " %sSeeds%s          %d (%d total)\n", label, val, props.Seeds, props.SeedsTotal)
	fmt.Fprintf(v, " %sPeers%s          %d (%d total)\n", label, val, props.Peers, props.PeersTotal)
	fmt.Fprintf(v, " %sConnections%s    %d (limit: %d)\n", label, val, props.NbConnections, props.NbConnectionsLimit)
	fmt.Fprintln(v)

	fmt.Fprintf(v, " %sCategory%s       %s\n", label, val, t.Category)
	fmt.Fprintf(v, " %sSave Path%s      %s\n", label, val, props.SavePath)
	addedTime := formatTimestamp(props.AdditionDate)
	completedTime := formatTimestamp(props.CompletionDate)
	fmt.Fprintf(v, " %sAdded%s          %s\n", label, val, addedTime)
	fmt.Fprintf(v, " %sCompleted%s      %s\n", label, val, completedTime)
	fmt.Fprintf(v, " %sTime Active%s    %s\n", label, val, formatETA(props.TimeElapsed))
	fmt.Fprintf(v, " %sSeeding Time%s   %s\n", label, val, formatETA(props.SeedingTime))
	fmt.Fprintf(v, " %sPieces%s         %d / %d (%s each)\n", label, val, props.PiecesHave, props.PiecesNum, formatSize(props.PieceSize))
	if props.Comment != "" {
		fmt.Fprintf(v, " %sComment%s        %s\n", label, val, props.Comment)
	}
	if props.CreatedBy != "" {
		fmt.Fprintf(v, " %sCreated By%s     %s\n", label, val, props.CreatedBy)
	}
}

func renderTrackersTab(v *gocui.View, client *QBClient, hash string) {
	trackers, err := client.GetTorrentTrackers(hash)
	if err != nil {
		fmt.Fprintf(v, " Error: %v\n", err)
		return
	}
	if len(trackers) == 0 {
		fmt.Fprintln(v, " No trackers")
		return
	}

	grey := "\033[38;5;245m"
	fmt.Fprintf(v, " %s%-4s %-45s %-12s %-6s %-6s %-6s%s\n",
		grey, "Tier", "URL", "Status", "Seeds", "Peers", "Lchs", resetColor)

	for _, t := range trackers {
		fmt.Fprintf(v, " %-4d %-45s %-12s %-6d %-6d %-6d\n",
			t.Tier, truncateName(t.URL, 44), trackerStatusStr(t.Status),
			t.NumSeeds, t.NumPeers, t.NumLeeches)
		if t.Msg != "" {
			fmt.Fprintf(v, "      %s%s%s\n", grey, t.Msg, resetColor)
		}
	}
}

func renderPeersTab(v *gocui.View, client *QBClient, hash string) {
	peers, err := client.GetTorrentPeers(hash)
	if err != nil {
		fmt.Fprintf(v, " Error: %v\n", err)
		return
	}
	if len(peers) == 0 {
		fmt.Fprintln(v, " No peers connected")
		return
	}

	grey := "\033[38;5;245m"
	fmt.Fprintf(v, " %s%-22s %-22s %-8s %-11s %-11s %-5s %-3s%s\n",
		grey, "IP", "Client", "Progress", "Down", "Up", "Flags", "CC", resetColor)

	for _, p := range peers {
		addr := fmt.Sprintf("%s:%d", p.IP, p.Port)
		fmt.Fprintf(v, " %-22s %-22s %5.1f%%   %-11s %-11s %-5s %-3s\n",
			truncateName(addr, 21),
			truncateName(p.Client, 21),
			p.Progress*100,
			formatSpeed(p.DlSpeed),
			formatSpeed(p.UpSpeed),
			p.Flags,
			p.CountryCode)
	}
	fmt.Fprintf(v, "\n %sTotal: %d peers%s\n", grey, len(peers), resetColor)
}

func renderHTTPSourcesTab(v *gocui.View, client *QBClient, hash string) {
	seeds, err := client.GetTorrentWebSeeds(hash)
	if err != nil {
		fmt.Fprintf(v, " Error: %v\n", err)
		return
	}
	if len(seeds) == 0 {
		fmt.Fprintln(v, " No HTTP sources")
		return
	}
	for _, s := range seeds {
		fmt.Fprintf(v, " %s\n", s.URL)
	}
}

func renderContentTab(v *gocui.View, client *QBClient, hash string) {
	files, err := client.GetTorrentFiles(hash)
	if err != nil {
		fmt.Fprintf(v, " Error: %v\n", err)
		contentFileCache = nil
		contentFileCacheHash = ""
		return
	}
	if len(files) == 0 {
		fmt.Fprintln(v, " No files")
		contentFileCache = nil
		contentFileCacheHash = hash
		return
	}

	contentFileCache = append([]TorrentFile(nil), files...)
	contentFileCacheHash = hash
	if contentFileSelection >= len(files) {
		contentFileSelection = len(files) - 1
	}
	if contentFileSelection < 0 {
		contentFileSelection = 0
	}

	sizeW := 10
	progW := 7
	prioW := 8

	nameW := contentTabNameWidth(v)
	if contentFileSelection >= 0 && contentFileSelection < len(files) {
		rs := []rune(files[contentFileSelection].Name)
		if len(rs) > nameW {
			maxScroll := len(rs) - nameW
			if contentNameScrollOffset > maxScroll {
				contentNameScrollOffset = maxScroll
			}
		} else {
			contentNameScrollOffset = 0
		}
	}

	grey := "\033[38;5;245m"
	fmt.Fprintf(v, " %s%-*s %*s %*s %-*s%s\n",
		grey, nameW, "Name", sizeW, "Size", progW, "Prog", prioW, "Priority", resetColor)

	selBg := "\033[44;97m"
	for i, f := range files {
		prefix := " "
		suffix := ""
		if contentEditMode && i == contentFileSelection {
			prefix = " " + selBg
			suffix = resetColor
		}
		nameCell := torrentNameCell(f.Name, nameW, contentNameScrollOffset, i == contentFileSelection)
		fmt.Fprintf(v, "%s%s %*s %5.1f%% %-*s%s\n",
			prefix,
			nameCell,
			sizeW, formatSize(f.Size),
			f.Progress*100,
			prioW, filePriorityStr(f.Priority),
			suffix)
	}
	if !contentEditMode {
		_ = v.SetOrigin(0, 0)
	} else {
		scrollContentFileIntoView(v, len(files))
	}
}

// formatTimestamp converts a unix timestamp to a readable string, or "--" if invalid.
func formatTimestamp(ts int64) string {
	if ts <= 0 {
		return "--"
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
}

func trackerStatusStr(status int) string {
	switch status {
	case 0:
		return "Disabled"
	case 1:
		return "Not contacted"
	case 2:
		return "Working"
	case 3:
		return "Updating"
	case 4:
		return "Not working"
	default:
		return fmt.Sprintf("Unknown(%d)", status)
	}
}

func filePriorityStr(priority int) string {
	switch priority {
	case 0:
		return "Skip"
	case 1:
		return "Normal"
	case 6:
		return "High"
	case 7:
		return "Maximum"
	default:
		return fmt.Sprintf("%d", priority)
	}
}

// applyFilters rebuilds filteredTorrents from torrents using active filters. Must be called with torrentsMu held.
func applyFilters() {
	oldSel := currentSelection
	filtered := make([]Torrent, 0, len(torrents))
	for _, t := range torrents {
		if filterStatus != "" && cleanStatusString(t.State) != filterStatus {
			continue
		}
		if filterCategory != "" && t.Category != filterCategory {
			continue
		}
		filtered = append(filtered, t)
	}
	filteredTorrents = filtered
	if currentSelection >= len(filteredTorrents) {
		if len(filteredTorrents) > 0 {
			currentSelection = len(filteredTorrents) - 1
		} else {
			currentSelection = 0
		}
	}
	if currentSelection != oldSel {
		nameScrollOffset = 0
		contentNameScrollOffset = 0
	}
}

// getUniqueStatuses returns ["All"] plus every distinct cleaned status in the unfiltered torrent list.
func getUniqueStatuses() []string {
	torrentsMu.RLock()
	defer torrentsMu.RUnlock()
	seen := map[string]bool{}
	statuses := []string{"All"}
	for _, t := range torrents {
		s := cleanStatusString(t.State)
		if !seen[s] {
			seen[s] = true
			statuses = append(statuses, s)
		}
	}
	return statuses
}

// getUniqueCategories returns ["All"] plus every distinct category in the unfiltered torrent list.
func getUniqueCategories() []string {
	torrentsMu.RLock()
	defer torrentsMu.RUnlock()
	seen := map[string]bool{}
	cats := []string{"All"}
	for _, t := range torrents {
		if t.Category != "" && !seen[t.Category] {
			seen[t.Category] = true
			cats = append(cats, t.Category)
		}
	}
	return cats
}

// showFilterDialog displays a dialog to filter by status and/or category using ←/→ to cycle options.
func showFilterDialog(g *gocui.Gui) error {
	statusOpts := getUniqueStatuses()
	catOpts := getUniqueCategories()

	row := 0
	statusIdx := 0
	catIdx := 0

	torrentsMu.RLock()
	for i, s := range statusOpts {
		if (filterStatus == "" && s == "All") || s == filterStatus {
			statusIdx = i
			break
		}
	}
	for i, c := range catOpts {
		if (filterCategory == "" && c == "All") || c == filterCategory {
			catIdx = i
			break
		}
	}
	torrentsMu.RUnlock()

	maxX, maxY := g.Size()
	width := 52
	if width > maxX-4 {
		width = maxX - 4
	}
	x0 := maxX/2 - width/2
	y0 := maxY/2 - 3

	v, err := g.SetView(viewOverlay, x0, y0, x0+width, y0+6, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		return err
	}
	v.FrameRunes = roundedFrameRunes
	v.Title = "Filter"

	grey := "\033[38;5;245m"
	redraw := func() {
		v.Clear()
		ind0, ind1 := " ", " "
		if row == 0 {
			ind0 = yellowColor + "▸" + resetColor
		}
		if row == 1 {
			ind1 = yellowColor + "▸" + resetColor
		}
		fmt.Fprintf(v, " %s %sStatus:%s   ◀ %s%s%s ▶\n", ind0, grey, resetColor, yellowColor, statusOpts[statusIdx], resetColor)
		fmt.Fprintf(v, " %s %sCategory:%s ◀ %s%s%s ▶\n", ind1, grey, resetColor, yellowColor, catOpts[catIdx], resetColor)
		fmt.Fprintln(v)
		fmt.Fprintf(v, " %s←/→ cycle  ↑/↓ switch  Enter apply  r reset%s", grey, resetColor)
	}
	redraw()

	g.SetKeybinding(viewOverlay, gocui.KeyArrowUp, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if row > 0 {
			row--
		}
		redraw()
		return nil
	})
	g.SetKeybinding(viewOverlay, gocui.KeyArrowDown, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if row < 1 {
			row++
		}
		redraw()
		return nil
	})
	g.SetKeybinding(viewOverlay, gocui.KeyArrowLeft, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if row == 0 {
			statusIdx--
			if statusIdx < 0 {
				statusIdx = len(statusOpts) - 1
			}
		} else {
			catIdx--
			if catIdx < 0 {
				catIdx = len(catOpts) - 1
			}
		}
		redraw()
		return nil
	})
	g.SetKeybinding(viewOverlay, gocui.KeyArrowRight, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if row == 0 {
			statusIdx = (statusIdx + 1) % len(statusOpts)
		} else {
			catIdx = (catIdx + 1) % len(catOpts)
		}
		redraw()
		return nil
	})
	g.SetKeybinding(viewOverlay, gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		torrentsMu.Lock()
		if statusIdx == 0 {
			filterStatus = ""
		} else {
			filterStatus = statusOpts[statusIdx]
		}
		if catIdx == 0 {
			filterCategory = ""
		} else {
			filterCategory = catOpts[catIdx]
		}
		applyFilters()
		torrentsMu.Unlock()
		closeOverlay(g)
		nameScrollOffset = 0
		contentNameScrollOffset = 0
		return refreshUI(g)
	})
	g.SetKeybinding(viewOverlay, 'r', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		torrentsMu.Lock()
		filterStatus = ""
		filterCategory = ""
		applyFilters()
		torrentsMu.Unlock()
		closeOverlay(g)
		nameScrollOffset = 0
		contentNameScrollOffset = 0
		return refreshUI(g)
	})
	g.SetKeybinding(viewOverlay, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return closeOverlay(g)
	})

	_, err = g.SetCurrentView(viewOverlay)
	return err
}

// closeOverlay removes the overlay view and restores focus to the torrent list.
func closeOverlay(g *gocui.Gui) error {
	g.DeleteKeybindings(viewOverlay)
	if err := g.DeleteView(viewOverlay); err != nil {
		return err
	}
	_, err := g.SetCurrentView(viewLeft)
	return err
}

// torrentFooterSpans returns grey label and white value segments for the torrent footer (input: none; output: spans for drawListFooter).
func torrentFooterSpans() []gocui.FooterSpan {
	torrentsMu.RLock()
	list := torrents
	torrentsMu.RUnlock()
	var active, inactive int
	var peerSum int64
	var dlTotal, ulTotal int64
	for _, t := range list {
		if isTorrentInactiveState(t.State) {
			inactive++
		} else {
			active++
		}
		peerSum += int64(t.Peers)
		dlTotal += t.DownloadSpeed
		ulTotal += t.UploadSpeed
	}
	grey := gocui.Get256Color(240)
	white := gocui.ColorWhite
	return []gocui.FooterSpan{
		{Text: " Active ", Fg: grey},
		{Text: fmt.Sprintf("%d", active), Fg: white},
		{Text: "  Inactive ", Fg: grey},
		{Text: fmt.Sprintf("%d", inactive), Fg: white},
		{Text: "  Peers ", Fg: grey},
		{Text: fmt.Sprintf("%d", peerSum), Fg: white},
		{Text: "  Down ", Fg: grey},
		{Text: formatSpeed(dlTotal), Fg: white},
		{Text: "  Up ", Fg: grey},
		{Text: formatSpeed(ulTotal), Fg: white},
	}
}

// drawBottomPanel writes shortcut hints into the shortcuts view (input: v; output: one-line hints).
func drawBottomPanel(v *gocui.View) {
	v.Clear()
	writeShortcutsBarContent(v)
}

// writeShortcutsBarContent writes shortcut hint text without clearing the view (input: v).
func writeShortcutsBarContent(v *gocui.View) {
	apiErrMu.Lock()
	hasAPIErr := apiErr != nil
	apiErrMu.Unlock()
	if hasAPIErr {
		fmt.Fprintf(v, " %sr%s retry now  %sq%s quit",
			yellowColor, resetColor,
			yellowColor, resetColor,
		)
		return
	}
	// Keys in yellow (lazygit-style); descriptions use view FgColor (blue).
	fmt.Fprintf(v, " %s←%s%s/%s→%s name  %s⎵%s details  %ss%s stop/start  %sd%s delete  %sb%s blocklist  %s+%s%s/%s-%s priority  %sf%s filter  %sa%s add url  %sm%s magnet  %sq%s quit",
		yellowColor, resetColor, blueColor, yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor, blueColor, yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
	)
}

// isTorrentInactiveState returns true for stopped, paused, error, or missing-files states (input: qBittorrent state string).
func isTorrentInactiveState(state string) bool {
	switch state {
	case "stoppedDL", "stoppedUP", "stopped", "pausedDL", "pausedUP", "error", "missingFiles":
		return true
	default:
		return false
	}
}

