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
	viewRight        = "torrentDetails"
	torrents         []Torrent
	torrentsMu       sync.RWMutex
	currentSelection = 0
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
	torrentsMu.Unlock()

	g, err := gocui.NewGui(gocui.OutputNormal, true)
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

	if err := keybindings(g); err != nil {
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

	// Define fixed widths
	statusWidth := 12
	progressWidth := 9
	dlSpeedWidth := 12
	ulSpeedWidth := 12
	etaWidth := 6
	sizeWidth := 10
	seedsPeersWidth := 10
	padding := 8 // spaces between columns

	// Calculate dynamic nameWidth
	usedWidth := statusWidth + progressWidth + dlSpeedWidth + ulSpeedWidth + etaWidth + sizeWidth + seedsPeersWidth + padding
	nameWidth := maxX - usedWidth
	if nameWidth < 20 {
		nameWidth = 20 // min width
	}

	// Top view for torrent list
	if v, err := g.SetView(viewLeft, 0, 1, maxX-1, (maxY*2)/3-1, 0); err != nil {
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

	// Bottom view for details (initially empty)
	if v, err := g.SetView(viewRight, 0, (maxY*2)/3, maxX-1, maxY-1, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		v.Title = "Details"
		v.Wrap = true
		v.Clear()
		fmt.Fprintln(v, "Select a torrent to see details")
	}

	g.SetCurrentView(viewLeft)

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

	// Print header
	fmt.Fprintf(v, "%-*s %-*s %-*s %-*s %-*s %-*s %-*s %-*s\n",
		nameWidth, "Name",
		progressWidth, "Progress%",
		statusWidth, "Status",
		dlSpeedWidth, "DownSpeed",
		ulSpeedWidth, "UpSpeed",
		etaWidth, "ETA",
		sizeWidth, "Size",
		seedsPeersWidth, "Seeds/Peers")

	torrentsMu.RLock()
	localTorrents := torrents
	torrentsMu.RUnlock()

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

func keybindings(g *gocui.Gui) error {
	// Bind arrow keys explicitly to torrent list view
	if err := g.SetKeybinding(viewLeft, gocui.KeyArrowDown, gocui.ModNone, cursorDown); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, gocui.KeyArrowUp, gocui.ModNone, cursorUp); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewLeft, 'q', gocui.ModNone, quit); err != nil {
		return err
	}
	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func cursorDown(g *gocui.Gui, v *gocui.View) error {
	torrentsMu.RLock()
	count := len(torrents)
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

// Additional helper functions here (truncateName, formatSpeed, etc.)
