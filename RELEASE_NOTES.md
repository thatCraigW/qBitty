# qBitty v0.2.0

Release notes for **v0.2.0** (from v0.1.0).

---

## Highlights

This release improves what happens when **qBittorrent can’t be reached**, fixes a **crash** in that situation, and adds **file priority editing** from the Content tab.

---

## Connection and sign-in

- **Stay in the app when something goes wrong** — If the Web UI URL is unreachable or login fails, qBitty no longer quits. You’ll see the main screen (empty list) and a **short explanation in plain language** (for example: server unreachable vs. wrong username/password).
- **Automatic retry** — For typical connection problems, the app **counts down and retries** about every 10 seconds so it can recover when qBittorrent comes back.
- **Wrong password** — You’ll get a simple message to **check your config or environment variables**, then press **`r`** to try again (no endless auto-retry loop for bad credentials).
- **Stability** — Fixed a **crash** that could happen when classifying certain network/DNS errors.

---

## Content tab (files)

- **`e` — edit file priorities** — On the **Content** tab, press **`e`** to select files; **`↑` / `↓`** move the highlight; **`p`** cycles priority: **Skip → Normal → High → Maximum**. Press **`e`** again to leave edit mode.
- **Hints** — The bottom of the details pane shows which keys apply on that tab or while editing.

---

## Other fixes

- **Shortcuts bar** — Shortcut labels (including quit) display correctly again (no broken placeholder text in the status bar).

---

## Upgrading

- **From source:** Check out tag `v0.2.0` and rebuild as usual.
- **Homebrew:** After your tap points at `v0.2.0`, run:

  ```bash
  brew update && brew upgrade qbitty
  ```

  (Use the actual formula name if yours differs.)

---

## Full diff

Compare to v0.1.0 on GitHub:

<https://github.com/thatCraigW/qBitty/compare/v0.1.0...v0.2.0>
