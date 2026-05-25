# Spoolr Connect — macOS menubar app

A Tailscale-style menubar app that *is* the connector: it runs the agent in the
background and shows, at a glance, which printers are online. On first launch it
pairs from the menubar — no terminal, no SSH.

## Install

```bash
scripts/macos/install.sh            # build → /Applications → launch at login → open
scripts/macos/install.sh --user     # install to ~/Applications instead
scripts/macos/install.sh --no-login # skip the launch-at-login LaunchAgent
```

Then click **Spoolr** in the menu bar → **Set up Spoolr Connect…**, paste the
pairing token from the Spoolr app (Add printer), and it discovers + adopts every
printer on your network.

- Config lives at `~/Library/Application Support/Spoolr/connector.json`.
- The build is **unsigned** (local). `install.sh` clears the Gatekeeper
  quarantine so it opens directly; for distribution it would need Apple
  Developer signing + notarization (separate step).
- Launch-at-login is a per-user LaunchAgent at
  `~/Library/LaunchAgents/io.spoolr.connect.plist` — delete it to disable.

## Build only (no install)

```bash
OUT_DIR=/tmp/x VERSION=0.1.0 scripts/macos/build_app.sh   # → "$OUT_DIR/Spoolr Connect.app"
```

## Headless status check

```bash
"/Applications/Spoolr Connect.app/Contents/MacOS/spoolr-menubar" --check
```

## Follow-ups

- Branded `AppIcon.icns` (build picks it up automatically if dropped next to
  `Info.plist`); the menu bar currently shows the text title "Spoolr".
- Code signing + notarization for distribution outside this machine.
- Reuse this bundle in a published GitHub release so the web onboarding's
  Download button serves it directly.
