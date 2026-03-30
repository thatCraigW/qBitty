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
