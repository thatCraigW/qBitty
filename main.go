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
	viewLeft      = "torrentList"
	viewShortcuts = "shortcuts"
	viewOverlay   = "overlay"
	torrents      []Torrent
	torrentsMu    sync.RWMutex
	currentSelection int
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
			if currentSelection >= len(torrents) && len(torrents) > 0 {
				currentSelection = len(torrents) - 1
			}
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

// getSelectedTorrent returns a copy of the currently selected torrent, or nil if none.
func getSelectedTorrent() *Torrent {
	torrentsMu.RLock()
	defer torrentsMu.RUnlock()
	if currentSelection >= 0 && currentSelection < len(torrents) {
		t := torrents[currentSelection]
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
	fmt.Fprintf(v, " %ss%s stop/start  %sd%s delete  %s+%s pri up  %s-%s pri down  %sa%s add url  %sm%s magnet  %sq%s quit",
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
		yellowColor, resetColor,
	)
}
