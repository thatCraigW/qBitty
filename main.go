package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/awesome-gocui/gocui"
)

const (
	resetColor  = "\033[0m"
	redColor    = "\033[31m"
	greenColor  = "\033[32m"
	yellowColor = "\033[33m"
	blueColor   = "\033[34m"
)

var (
	viewLeft         = "torrentList"
	viewShortcuts    = "shortcuts"
	viewOverlay      = "overlay"
	torrents         []Torrent
	filteredTorrents []Torrent
	torrentsMu       sync.RWMutex
	currentSelection int
	filterStatus     string
	filterCategory   string
)

func main() {
	jsonDump := flag.Bool("dump-json", false, "Fetch torrents info and output raw JSON")
	flag.Parse()

	apiClient, err := NewQBClient()
	if err != nil {
		log.Fatalf("Failed to login: %v", err)
	}

	if *jsonDump {
		jsonData, err := apiClient.GetTorrentsRaw()
		if err != nil {
			log.Fatalf("Failed to fetch torrents info: %v", err)
		}
		fmt.Println(string(jsonData))
		os.Exit(0)
	}

	initialTorrents, err := apiClient.GetTorrents()
	if err != nil {
		log.Fatalf("Failed to fetch torrents: %v", err)
	}
	torrentsMu.Lock()
	torrents = initialTorrents
	applyFilters()
	torrentsMu.Unlock()

	g, err := gocui.NewGui(gocui.Output256, true)
	if err != nil {
		log.Panicln(err)
	}
	defer g.Close()

	// Create a ticker that fires every 1 second
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// Use a channel to signal exit
	done := make(chan struct{})

	// Start a goroutine to refresh torrent data periodically
	go func() {
		for {
			select {
			case <-ticker.C:
				// Fetch updated torrent data
				newTorrents, err := apiClient.GetTorrents()
				if err != nil {
					log.Printf("Error refreshing torrents: %v", err)
					continue
				}

			torrentsMu.Lock()
			torrents = newTorrents
			applyFilters()
			torrentsMu.Unlock()

			g.Update(func(g *gocui.Gui) error {
				return refreshUI(g)
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

	if v, err := g.SetView(viewLeft, 0, 0, maxX-1, maxY-3, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Title = "Torrents"
		v.Highlight = true
		v.Editable = false
		v.SelBgColor = gocui.ColorBlue
		v.SelFgColor = gocui.ColorWhite
		refreshTorrentList(g, v)
		v.SetCursor(0, currentSelection)
		v.SetOrigin(0, currentSelection)
	}

	if v, err := g.SetView(viewShortcuts, -1, maxY-2, maxX, maxY, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Frame = false
		drawShortcutsBar(v)
	}

	if _, err := g.View(viewOverlay); err != nil {
		g.SetCurrentView(viewLeft)
	}

	return nil
}

func refreshTorrentList(g *gocui.Gui, v *gocui.View) {
	maxX, _ := g.Size()

	// Fixed widths
	statusWidth := 12
	progressWidth := 9
	dlSpeedWidth := 12
	ulSpeedWidth := 12
	etaWidth := 6
	sizeWidth := 10
	seedsPeersWidth := 10
	padding := 8

	nameWidth := maxX - (statusWidth + progressWidth + dlSpeedWidth + ulSpeedWidth + etaWidth + sizeWidth + seedsPeersWidth + padding)
	if nameWidth < 20 {
		nameWidth = 20
	}

	v.Clear()

	torrentsMu.RLock()
	localTorrents := filteredTorrents
	fStatus := filterStatus
	fCategory := filterCategory
	torrentsMu.RUnlock()

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

	for _, t := range localTorrents {
		colorState := colorForState(t.State)
		downloadSpeedColor := colorForSpeed(t.DownloadSpeed)
		uploadSpeedColor := colorForSpeed(t.UploadSpeed)

		name := truncateName(t.Name, nameWidth-1)
		dlSpeedStr := formatSpeed(t.DownloadSpeed)
		uploadSpeedStr := formatSpeed(t.UploadSpeed)
		sizeStr := formatSize(t.Size)
		etaStr := formatETA(t.ETA)

		cleanStatus := cleanStatusString(t.State)

		// Determine if status is moving, stopped, or stalled
		statusLower := strings.ToLower(cleanStatus)

		// Only override when status is exactly 'moving', 'stopped', or 'stalled'
		if statusLower != "downloading" && statusLower != "stalled" {
			etaStr = "--"
			dlSpeedStr = "--"
			uploadSpeedStr = "--"
		}

		nameCol := fmt.Sprintf("%-*s", nameWidth, name)
		progressCol := fmt.Sprintf("%*s", progressWidth, fmt.Sprintf("%.2f%%", t.Progress*100))
		statusCol := fmt.Sprintf("%s%-*s%s", colorState, statusWidth, cleanStatus, resetColor)
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
		return showDetailsDialog(g, client)
	}); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 'f', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return showFilterDialog(g)
	}); err != nil {
		return err
	}
	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func cursorDown(g *gocui.Gui, v *gocui.View) error {
	torrentsMu.RLock()
	count := len(filteredTorrents)
	torrentsMu.RUnlock()
	if currentSelection < count-1 {
		currentSelection++
		if err := v.SetCursor(0, currentSelection+1); err != nil {
			ox, oy := v.Origin()
			if err := v.SetOrigin(ox, oy+1); err != nil {
				return err
			}
		}
		// Refresh UI after cursor change
		return refreshUI(g)
	}
	return nil
}

func cursorUp(g *gocui.Gui, v *gocui.View) error {
	if currentSelection > 0 {
		currentSelection--
		if err := v.SetCursor(0, currentSelection+1); err != nil {
			ox, oy := v.Origin()
			if err := v.SetOrigin(ox, oy-1); err != nil {
				return err
			}
		}
		// Refresh UI after cursor change
		return refreshUI(g)
	}
	return nil
}

// refreshUI refreshes the torrent list view content and cursor position
func refreshUI(g *gocui.Gui) error {
	v, err := g.View(viewLeft)
	if err != nil {
		return err
	}
	refreshTorrentList(g, v)
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

// showConfirmDialog displays a y/n confirmation overlay; calls onConfirm if user presses y.
func showConfirmDialog(g *gocui.Gui, message string, onConfirm func() error) error {
	maxX, maxY := g.Size()
	width := len(message) + 4
	if width < 30 {
		width = 30
	}
	if width > maxX-4 {
		width = maxX - 4
	}
	x0 := maxX/2 - width/2
	y0 := maxY/2 - 1

	v, err := g.SetView(viewOverlay, x0, y0, x0+width, y0+2, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		return err
	}
	v.Title = "Confirm"
	v.Clear()
	fmt.Fprintf(v, " %s", message)

	g.SetKeybinding(viewOverlay, 'y', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if err := onConfirm(); err != nil {
			log.Printf("action error: %v", err)
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

// showDetailsDialog displays a tabbed modal with detailed torrent info (like qBittorrent WebUI).
func showDetailsDialog(g *gocui.Gui, client *QBClient) error {
	t := getSelectedTorrent()
	if t == nil {
		return nil
	}
	hash := t.Hash

	maxX, maxY := g.Size()
	width := maxX * 4 / 5
	if width < 60 {
		width = 60
	}
	if width > maxX-4 {
		width = maxX - 4
	}
	height := maxY * 4 / 5
	if height < 15 {
		height = 15
	}
	if height > maxY-4 {
		height = maxY - 4
	}
	x0 := maxX/2 - width/2
	y0 := maxY/2 - height/2

	v, err := g.SetView(viewOverlay, x0, y0, x0+width, y0+height, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		return err
	}
	v.Title = "Details"
	v.Wrap = true

	currentTab := 5
	renderDetailsTab(v, client, hash, currentTab)

	for i := 1; i <= 5; i++ {
		tab := i
		g.SetKeybinding(viewOverlay, rune('0'+tab), gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			currentTab = tab
			renderDetailsTab(v, client, hash, currentTab)
			return nil
		})
	}
	g.SetKeybinding(viewOverlay, gocui.KeyArrowLeft, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if currentTab > 1 {
			currentTab--
			renderDetailsTab(v, client, hash, currentTab)
		}
		return nil
	})
	g.SetKeybinding(viewOverlay, gocui.KeyArrowRight, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if currentTab < 5 {
			currentTab++
			renderDetailsTab(v, client, hash, currentTab)
		}
		return nil
	})

	g.SetKeybinding(viewOverlay, gocui.KeyArrowDown, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		ox, oy := v.Origin()
		v.SetOrigin(ox, oy+1)
		return nil
	})
	g.SetKeybinding(viewOverlay, gocui.KeyArrowUp, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		ox, oy := v.Origin()
		if oy > 0 {
			v.SetOrigin(ox, oy-1)
		}
		return nil
	})
	g.SetKeybinding(viewOverlay, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return closeOverlay(g)
	})
	g.SetKeybinding(viewOverlay, gocui.KeySpace, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return closeOverlay(g)
	})
	g.SetKeybinding(viewOverlay, 'q', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return closeOverlay(g)
	})

	_, err = g.SetCurrentView(viewOverlay)
	return err
}

// renderDetailsTab clears the overlay and draws the tab header + content for the given tab number.
func renderDetailsTab(v *gocui.View, client *QBClient, hash string, tab int) {
	v.Clear()
	v.SetOrigin(0, 0)
	v.SetCursor(0, 0)
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
		return
	}
	if len(files) == 0 {
		fmt.Fprintln(v, " No files")
		return
	}

	sizeW := 10
	progW := 7
	prioW := 8
	fixedW := sizeW + progW + prioW + 4 // 4 for leading space + gaps between cols

	viewW, _ := v.Size()
	nameW := viewW - fixedW
	if nameW < 20 {
		nameW = 20
	}

	grey := "\033[38;5;245m"
	fmt.Fprintf(v, " %s%-*s %*s %*s %-*s%s\n",
		grey, nameW, "Name", sizeW, "Size", progW, "Prog", prioW, "Priority", resetColor)

	for _, f := range files {
		fmt.Fprintf(v, " %-*s %*s %5.1f%% %-*s\n",
			nameW, truncateName(f.Name, nameW),
			sizeW, formatSize(f.Size),
			f.Progress*100,
			prioW, filePriorityStr(f.Priority))
	}
	fmt.Fprintf(v, "\n %sTotal: %d files%s\n", grey, len(files), resetColor)
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
		return refreshUI(g)
	})
	g.SetKeybinding(viewOverlay, 'r', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		torrentsMu.Lock()
		filterStatus = ""
		filterCategory = ""
		applyFilters()
		torrentsMu.Unlock()
		closeOverlay(g)
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

// drawShortcutsBar writes the keyboard shortcut hints into the given view.
func drawShortcutsBar(v *gocui.View) {
	fmt.Fprintf(v, " %s⎵%s details  %ss%s stop/start  %sd%s delete  %s+%s pri up  %s-%s pri down  %sf%s filter  %sa%s add url  %sm%s magnet  %sq%s quit",
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
	)
}
