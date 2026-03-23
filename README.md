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

qBitty loads credentials from a **config file** first, then applies any **environment variable** overrides on top. This means you can use either method (or both).

### Config file (recommended)

Create `~/.config/qbitty/config.json`:

```json
{
  "url": "https://localhost:8080",
  "username": "admin",
  "password": "your-password"
}
```

Restrict permissions so only your user can read it:

```bash
chmod 600 ~/.config/qbitty/config.json
```

### Environment variables (alternative / override)

You can use environment variables instead of a config file, or to override individual values from the config file:

| Variable  | Description                          | Example                    |
|-----------|--------------------------------------|----------------------------|
| `QB_URL`  | qBittorrent WebUI URL                | `https://localhost:8080`   |
| `QB_USER` | WebUI username                       | `admin`                    |
| `QB_PASS` | WebUI password                       | `adminadmin`               |

### Resolution order

1. Read `~/.config/qbitty/config.json` (if it exists)
2. Override with `QB_URL` / `QB_USER` / `QB_PASS` environment variables (if set)

This is useful if you want to keep your URL and username in the config file but pass the password via an env var for extra safety.

## Usage

```bash
# Launch the TUI (config file)
./qbitty

# Or with env vars
QB_URL=https://localhost:8080 QB_USER=admin QB_PASS=secret ./qbitty

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
