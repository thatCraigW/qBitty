# qBitty

A terminal UI client for qBittorrent, built with Go and [gocui](https://github.com/awesome-gocui/gocui).

## Features

- Real-time torrent list with status, progress, speeds, ETA, size, and seed/peer counts
- Split-pane details view with 5 tabs (matching the qBittorrent WebUI):
  - **General** -- transfer info, speeds, connections, dates, piece info
  - **Trackers** -- tracker URLs, status, seed/peer/leech counts
  - **Peers** -- connected peers with client, speed, country info
  - **HTTP Sources** -- web seed URLs
  - **Content** -- file list with size, progress, and priority
- Filter torrents by status and/or category
- Torrent actions: stop/start, delete, increase/decrease priority
- Add new torrents by URL or magnet link
- Auto-refreshes every second

## Requirements

- Go 1.22+
- A running qBittorrent instance with the WebUI API enabled

## Installation

```bash
go build -o qbitty .
```

## Configuration

Set the following environment variables:

| Variable  | Description                          | Example                    |
|-----------|--------------------------------------|----------------------------|
| `QB_URL`  | qBittorrent WebUI URL                | `https://localhost:8080`   |
| `QB_USER` | WebUI username                       | `admin`                    |
| `QB_PASS` | WebUI password                       | `adminadmin`               |

## Usage

```bash
# Launch the TUI
export QB_URL=https://localhost:8080
export QB_USER=admin
export QB_PASS=yourpassword
./qbitty

# Dump raw torrent JSON to stdout
./qbitty --dump-json
```

## Keyboard Shortcuts

| Key         | Action                                      |
|-------------|---------------------------------------------|
| `Up/Down`   | Navigate torrent list                       |
| `Space`     | Toggle details pane                         |
| `1-5`       | Switch details tab (opens pane if closed)   |
| `Left/Right`| Switch details tab                          |
| `s`         | Stop/start selected torrent                 |
| `d`         | Delete selected torrent (with confirmation) |
| `+` / `-`   | Increase/decrease queue priority            |
| `f`         | Filter by status and/or category            |
| `a`         | Add torrent by URL                          |
| `m`         | Add torrent by magnet link                  |
| `q`         | Quit                                        |

## License

MIT
