# qBitty

A terminal UI client for qBittorrent, built with Go and [gocui](https://github.com/awesome-gocui/gocui).

## Screenshots

Terminal captures from **2026-03-30** (bundled under [`docs/screenshots/`](docs/screenshots/)):

**Main torrent list** — status, speeds, and columns; long torrent names can be scrolled horizontally with **←** / **→** when the name does not fit the column.

![Main torrent list](docs/screenshots/qbitty-1-main-list.png)

**Details pane, Content tab — file priority edit mode** — press **`e`**, use **↑** / **↓** to choose a file, **`p`** to cycle priority; **←** / **→** scroll long file paths in the Name column.

![Content tab with file edit mode](docs/screenshots/qbitty-2-content-edit.png)

**Details pane, Content tab — browse mode** — **`e`** enters edit mode; **←** / **→** scroll the selected file name when paths are wider than the column.

![Content tab (browse mode)](docs/screenshots/qbitty-3-content-browse.png)

## Features

- Real-time torrent list with status, progress, speeds, ETA, size, and seed/peer counts
- **Long names:** **←** / **→** scroll the torrent name in the list (when it exceeds the Name column). On the **Content** tab, the same keys scroll the file path for the selected row (after **`e`**, the highlighted file row).
- Split-pane details view with 5 tabs (matching the qBittorrent WebUI):
  - **General** -- transfer info, speeds, connections, dates, piece info
  - **Trackers** -- tracker URLs, status, seed/peer/leech counts
  - **Peers** -- connected peers with client, speed, country info
  - **HTTP Sources** -- web seed URLs
  - **Content** -- file list with size, progress, and priority; **`e`** enters edit mode to change per-file priority (**`p`** cycles Skip → Normal → High → Maximum)
- Filter torrents by status and/or category
- Torrent actions: stop/start, delete, increase/decrease priority
- Add new torrents by URL or magnet link
- Auto-refreshes every second
- **When qBittorrent is unreachable or login fails**, the app stays open with a short explanation, an empty list, **`r`** to retry manually, and (for connection issues) a **10s countdown** before automatic retry

### What’s new in v0.2.0

- Clear **plain-English errors** when the Web UI cannot be reached or credentials are wrong (no sudden exit)
- **Automatic reconnect** with countdown for connectivity problems; **`r`** retry any time the status banner is shown
- **Content tab:** **`e`** / **`↑` `↓`** / **`p`** to inspect and cycle file download priority (qBittorrent API)
- **Stability:** fixed a crash when classifying some DNS/network errors
- **UI:** shortcut bar labels render correctly again

## Requirements

- **From source:** Go 1.22+
- A running qBittorrent instance with the WebUI API enabled

## Installation

### Homebrew (recommended)

If you use a custom tap that ships this formula (for example `thatcraigw/tap` from [`homebrew-tap`](https://github.com/thatCraigW/homebrew-tap)):

```bash
brew tap thatcraigw/tap
brew install qbitty
```

Upgrade after a new release:

```bash
brew update
brew upgrade qbitty
```

If your tap path differs, replace `thatcraigw/tap` with the name you used with `brew tap`.

### Build from source

```bash
go build -o qbitty .
```

Install the binary somewhere on your `PATH` if you want to run `qbitty` from anywhere.

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

### HTTPS and connection security

qBitty will warn if the configured URL uses plain HTTP, since credentials are sent in cleartext. There are a few approaches depending on your setup:

**Localhost only (HTTP is fine)** — If qBittorrent and qBitty run on the same machine, `http://localhost:8080` is safe. Traffic on localhost never leaves your machine, so there is nothing to intercept.

**Self-signed certificate** — To enable HTTPS on the qBittorrent WebUI, generate a self-signed cert and configure it in *Tools > Options > Web UI > Use HTTPS*:

```bash
openssl req -x509 -newkey rsa:2048 -keyout qbt-key.pem -out qbt-cert.pem -days 3650 -nodes -subj "/CN=localhost"
```

Then point the WebUI settings to `qbt-cert.pem` and `qbt-key.pem`.

**OrbStack / Docker** — If qBittorrent runs in an OrbStack or Docker container, you can use OrbStack's built-in HTTPS support (e.g. `https://qbittorrent.orb.local`) which provides a trusted local certificate automatically, avoiding self-signed cert hassle.

## Usage

```bash
# Launch the TUI (config file)
qbitty
# or, from the build directory: ./qbitty

# Or with env vars
QB_URL=https://localhost:8080 QB_USER=admin QB_PASS=secret qbitty

# Dump raw torrent JSON to stdout (still exits on login failure)
qbitty --dump-json
```

## Keyboard Shortcuts

| Key         | Action                                                                 |
|-------------|------------------------------------------------------------------------|
| `Up/Down`   | Navigate torrent list; on **Content** tab with **`e`** edit on, move file row |
| `Space`     | Toggle details pane                                                    |
| `1-5`       | Switch details tab (opens pane if closed)                              |
| `Left/Right`| Scroll long **names** (torrent list or **Content** tab file path) when they overflow; otherwise switch details tab (see below) |
| `s`         | Stop/start selected torrent                                            |
| `d`         | Delete selected torrent (with confirmation)                            |
| `+` / `-`   | Increase/decrease queue priority                                       |
| `e`         | On **Content** tab: toggle file-priority edit (`e` again to exit)      |
| `p`         | In file edit mode: cycle priority (Skip → Normal → High → Maximum)   |
| `f`         | Filter by status and/or category                                       |
| `a`         | Add torrent by URL                                                     |
| `m`         | Add torrent by magnet link                                             |
| `r`         | When the connection/login banner is visible: retry now                 |
| `q`         | Quit                                                                   |

**Details tab navigation with `Left` / `Right`:** On the **Content** tab (**5**), **←** / **→** scroll the file name first when the path is longer than the column; at the ends of the scroll (or if the name fits), **←** moves to the previous tab and **→** scrolls the torrent name in the main list (there is no tab to the right of Content). On other tabs, **←** / **→** move between tabs as before.

## License

MIT
