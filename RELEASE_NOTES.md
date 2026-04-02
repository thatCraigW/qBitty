# qBitty v0.6.0

Release notes for **v0.6.0** (from v0.5.0).

---

## Highlights

- **Sonarr / Radarr integration (`b`)** — Optional **`sonarr_url`**, **`sonarr_api_key`**, **`radarr_url`**, **`radarr_api_key`** in **`config.json`** (or **`SONARR_URL`**, **`SONARR_API_KEY`**, **`RADARR_URL`**, **`RADARR_API_KEY`** in the environment). When the selected torrent’s qBittorrent **category** is **`Sonarr`** or **`Radarr`** and the matching API is configured, **`b`** confirms, then finds the download in that app’s **`/api/v3/queue`** and **`DELETE`s** it with **`removeFromClient=true`** and **`blocklist=true`**. If *arr is not configured for that category, or the category is something else, **`b`** offers **removal from qBittorrent only**, with text that the client does not maintain a release blocklist like *arr.
- **Config loading** — Tries **`$XDG_CONFIG_HOME/qbitty/config.json`** first (when set), then **`~/.config/qbitty/config.json`**, so a config under **`~/.config`** is still read if **`XDG_CONFIG_HOME`** is not `~/.config`. **UTF-8 BOM** is stripped. **Malformed JSON** returns an error at startup instead of being ignored.
- **README** — Copy-paste **JSON** examples (qBittorrent-only and with *arr), env reference, and **`b`** documented in shortcuts.

---

## Upgrading

- **From source:** Check out tag `v0.6.0` and rebuild as usual.
- **Homebrew:** After your tap points at `v0.6.0`, run:

  ```bash
  brew update && brew upgrade qbitty
  ```

  (Use the actual formula name if yours differs.)

---

## Full diff

Compare to v0.5.0 on GitHub:

<https://github.com/thatCraigW/qBitty/compare/v0.5.0...v0.6.0>

---

# qBitty v0.5.0

Release notes for **v0.5.0** (from v0.4.0).

---

## Highlights

- **Details panel title** — **Details** appears in the **top border** of the details pane, consistent with **Torrents** on the main list.
- **Content tab on the frame** — **Total: N files** is **right-aligned** on the **details** panel’s **bottom border**; **Content** shortcut hints (**`e`**, **`←→`**, edit-mode keys) are **left-aligned** on that same border—content uses the full pane height without a second footer view below the list.
- **Scroll indicators** — When content **overflows vertically**, a **scrollbar thumb** is drawn on the **right frame edge** of the torrent list and details pane ([lazygit](https://github.com/jesseduffield/lazygit)-style: **no extra terminal column**; the thumb overlays the border). Details reflect **per-tab** buffer height.
- **Layout** — Details **bottom row** aligns with the **global shortcut strip** so there is **no spare blank line** above the hints when the details pane is open; list **footers** use the **same horizontal inset** as panel titles.
- **Global shortcut bar** — Hints use compact **`←/→ name`** and **`+/- priority`** (yellow keys, blue slash and descriptions).

---

## Details and Content tab

- **gocui** — **`FooterSpansLeft`** for left-aligned bottom-border segments; **`AfterDraw`** hook runs after views render (used for frame-edge scrollbars).
- **Torrent / details footers** — **`drawListFooter`** inset matches **title** row padding (one cell inside the frame on left and right).

---

## Upgrading

- **From source:** Check out tag `v0.5.0` and rebuild as usual.
- **Homebrew:** After your tap points at `v0.5.0`, run:

  ```bash
  brew update && brew upgrade qbitty
  ```

  (Use the actual formula name if yours differs.)

---

## Full diff

Compare to v0.4.0 on GitHub:

<https://github.com/thatCraigW/qBitty/compare/v0.4.0...v0.5.0>

---

# qBitty v0.4.0

Release notes for **v0.4.0** (from v0.3.0).

---

## Highlights

- **Vertical scrolling** — Moving the selection with **↑** / **↓** keeps the **torrent list** and **Content** tab file list scrolled so the **active row stays in view** (also after opening the details pane or resizing the terminal).
- **Aggregate status** — **Active**, **inactive**, **peers**, and total **download** / **upload** speeds are shown **on the torrent panel’s bottom border**, right-aligned (lazygit-style embedded footer). Labels use **dark grey**; numeric values and speeds use **white**.
- **Rounded panel borders** — Framed views (torrent list, details, overlays) use **rounded** corners (**╭ ╮ ╰ ╯**), matching the common lazygit **`gui.border: rounded`** look.
- **Shortcut colors** — The bottom **shortcut** row uses **blue** for descriptions and **yellow** for key hints; the **Content** tab’s footer line uses the **same** scheme for **`e`**, **`←→`**, and edit-mode keys.
- **Layout and gocui** — Tighter spacing between the torrent frame and the shortcut row (no stray blank line); vendored **gocui** updates include **footer** / **multi-span** drawing, **list footer** on the bottom frame row, and a **highlight** fix when horizontally scrolling long names.

---

## Vertical scrolling

- **Torrent list** — When the selection moves off-screen, the list **scrolls** so the selected torrent stays visible.
- **Content tab** — When **file rows** extend past the visible area, selection changes **scroll** the list accordingly (including in **`e`** edit mode).

---

## Status line (torrent footer)

- Totals are **embedded in the bottom border** of the **Torrents** panel (not a separate strip that overwrites the frame).
- **Two-tone text:** grey labels (**Active**, **Inactive**, **Peers**, **Down**, **Up**) and white values, via **footer spans** in the UI layer.

---

## Visual polish

- **Rounded `FrameRunes`** on framed views for a softer outline.
- **Shortcut row:** `FgColor` blue + ANSI **yellow** keys for the global hint bar; **Content** tab hints updated to match.
- **Geometry:** Torrent bottom edge and shortcut view aligned so there is **no empty row** between the border and the bottom hint line.

---

## Upgrading

- **From source:** Check out tag `v0.4.0` and rebuild as usual.
- **Homebrew:** After your tap points at `v0.4.0`, run:

  ```bash
  brew update && brew upgrade qbitty
  ```

  (Use the actual formula name if yours differs.)

---

## Full diff

Compare to v0.3.0 on GitHub:

<https://github.com/thatCraigW/qBitty/compare/v0.3.0...v0.4.0>

---

# qBitty v0.3.0

Release notes for **v0.3.0** (from v0.2.0).

---

## Highlights

- **Horizontal name scrolling** — Use **←** and **→** to pan long **torrent names** in the main list and long **file paths** on the **Content** tab when text is wider than the column.
- **README** — New **Screenshots** section with three **terminal captures** (stored under `docs/screenshots/`) so users can see how qBitty looks before installing.

---

## Horizontal name scrolling

- **Torrent list** — For the **selected** row, if the name does not fit the Name column, **←** / **→** move the visible slice of the string (other rows stay ellipsis-truncated). Scroll resets when you move the selection or change filters.
- **Content tab** — The same keys scroll the **file name** column for the **selected file** (`contentFileSelection`). On tab 5, **←** / **→** try file-name scrolling first; when the name fits or you are at the scroll limit, behavior falls through to **tab navigation** and (on the last tab) **torrent** name scrolling in the list, as described in the README.
- **Shortcut bar** — Hints include **←→ name** where relevant; the Content tab footer mentions scrolling long paths.

---

## README and screenshots

- **`docs/screenshots/`** — Three PNGs (main list, Content tab in edit mode, Content tab in browse mode) for the README **Screenshots** section.
- **Keyboard shortcuts** — Table updated for **Left** / **Right**, including how they interact with the details tabs.

---

## Upgrading

- **From source:** Check out tag `v0.3.0` and rebuild as usual.
- **Homebrew:** After your tap points at `v0.3.0`, run:

  ```bash
  brew update && brew upgrade qbitty
  ```

  (Use the actual formula name if yours differs.)

---

## Full diff

Compare to v0.2.0 on GitHub:

<https://github.com/thatCraigW/qBitty/compare/v0.2.0...v0.3.0>

---

# Earlier: qBitty v0.2.0

Release notes for **v0.2.0** (from v0.1.0).

## Highlights (v0.2.0)

This release improved what happens when **qBittorrent can’t be reached**, fixed a **crash** in that situation, and added **file priority editing** from the Content tab.

## Connection and sign-in (v0.2.0)

- **Stay in the app when something goes wrong** — If the Web UI URL is unreachable or login fails, qBitty no longer quits. You’ll see the main screen (empty list) and a **short explanation in plain language** (for example: server unreachable vs. wrong username/password).
- **Automatic retry** — For typical connection problems, the app **counts down and retries** about every 10 seconds so it can recover when qBittorrent comes back.
- **Wrong password** — You’ll get a simple message to **check your config or environment variables**, then press **`r`** to try again (no endless auto-retry loop for bad credentials).
- **Stability** — Fixed a **crash** that could happen when classifying certain network/DNS errors.

## Content tab (v0.2.0)

- **`e` — edit file priorities** — On the **Content** tab, press **`e`** to select files; **`↑` / `↓`** move the highlight; **`p`** cycles priority: **Skip → Normal → High → Maximum**. Press **`e`** again to leave edit mode.
- **Hints** — The bottom of the details pane shows which keys apply on that tab or while editing.

## Other fixes (v0.2.0)

- **Shortcuts bar** — Shortcut labels (including quit) display correctly again (no broken placeholder text in the status bar).

## Full diff (v0.2.0)

<https://github.com/thatCraigW/qBitty/compare/v0.1.0...v0.2.0>
