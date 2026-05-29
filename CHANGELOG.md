# Changelog

All notable changes to the printer-connector are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/), and the project aims to
follow [Semantic Versioning](https://semver.org/). Tagging `vX.Y.Z` triggers the
release workflow, which publishes the cross-compiled binaries and the macOS app.

## [Unreleased]

## [0.6.0] - 2026-05-29

### Added
- The macOS menu-bar app now checks GitHub for new releases — on launch
  (debounced to once per 12h) and every 24h after — and shows a one-line
  "Update available" banner at the top of the popover when a newer version
  is published. "Download" opens the release page; "Dismiss" hides the
  banner until a strictly newer version appears.
- **Launch at Login** — right-click the menu-bar icon to toggle
  `SMAppService.mainApp` registration, so the agent comes back up
  automatically after every reboot without having to relaunch the app
  by hand.
- **Right-click menu** on the menu-bar icon: *Check for Updates Now*
  (force-runs the update check, ignoring the 24 h debounce),
  *Launch at Login* (toggle), *Restart Agent*, *Quit*.
- **Agent-health warning row** in the popover footer: shows "Agent
  stopped" (red) when the bundled subprocess isn't running, or
  "Agent not responding" (yellow) when it's alive but hasn't written
  status.json in over 90 s. Both come with a one-click **Restart
  Agent** button.

### Changed
- The release workflow now publishes the matching `CHANGELOG.md` section as
  the GitHub Release body (previously the release body was empty).
- The menu-bar popover footer now shows the real bundled app version
  (read from `Info.plist`) instead of a hardcoded placeholder.

### Fixed
- The menu-bar popover would advertise "PRINTING" with frozen progress
  forever if the agent stopped writing `status.json` (crash, kill, wedged
  poll loop). The Swift app now checks `status.json`'s `updated_at` and
  forces every printer to `offline` (clearing progress / ETA / layer) once
  the file is more than 90 s stale.
- The snapshot loop could stall for minutes if a single printer's TCP
  socket accepted but never replied (Moonraker mid-restart, K1 asleep,
  Bambu MQTT hung mid-publish), because `QueryObjects` had no per-call
  deadline. Each printer poll now runs under a 10 s `context.WithTimeout`,
  so one unresponsive printer is recorded as unreachable within seconds
  and the rest of the cycle still completes.

## [0.5.0] - 2026-05-28

### Added
- **Live per-printer status in the menu bar:** the agent writes a local status
  file that the menu-bar app reads, so the popover shows real printer status
  instead of sample data (wires the previously-unwired live-telemetry seam).
- **Periodic LAN re-discovery:** after pairing, the agent keeps sweeping the
  network and auto-adopts newly-found printers.
- **Streamed slicer uploads:** the agent streams g-code from a signed cloud URL
  straight to the printer, with optional autostart.
- **Windows support:** Inno Setup installer plus Windows Service registration.
- GitHub Pages download page for the macOS app.
- `LICENSE` (MIT) — the README advertised MIT but no license file existed.
- `CHANGELOG.md`.
- CI now builds and tests the macOS menu-bar app (`swift build && swift test`),
  not just the Go agent.

### Changed
- Single-sourced the version: a plain `go build` now reports `dev`; the release
  workflow and `build_app.sh` stamp the real version via `-ldflags` (so the
  bundled menu-bar helper no longer mis-reports `0.1.0`).
- Bumped GitHub Actions off the deprecated Node 20 runtime
  (`actions/checkout@v5`, `actions/setup-go@v6`).
- Menu-bar polish: brighter dim text tiers for legible labels, a solid dark
  panel matching the design, a real app icon, and removal of the dead
  "Add by IP" row.

### Fixed
- The menu-bar "Open dashboard" link was a no-op; it now opens the dashboard.

## [0.4.0] - 2026-05-26

### Added
- **macOS app distribution ($0, no Apple Developer account):** universal
  (arm64 + x86_64), ad-hoc-signed `Spoolr Connect.app`; `install-macos.sh`
  one-line installer; a release job that publishes `SpoolrConnect-macos.zip`.
  `NSLocalNetworkUsageDescription` added so discovery works on macOS Sequoia+.
- `CLAUDE.md` project guide; substantially expanded Go (`internal/cloud`) and
  Swift test coverage.

### Fixed
- CI was red on every run (incl. `main`) due to a gofmt-unclean tree — fixed.
- Menu-bar app now persists its pairing across restarts (no more re-pairing).
- Network-scan "Pair" now runs the real pairing flow instead of a mock; a rescan
  hides already-linked printers.

### Security
- Stopped committing `config/config.dev.json` (held a real dev connector secret);
  gitignored real configs, added a placeholder example, scrubbed home LAN IPs
  from docs/examples.

## [0.3.0] - 2026-05-23
## [0.2.0] - 2026-05-23
## [0.1.0] - 2025-12-31

Earlier releases predate this changelog. `v0.1.0` added Creality K1 / K1 Max
support; `v0.2.0`–`v0.3.0` iterated on discovery, Bambu support, and the
install-from-releases flow. See the git history and GitHub Releases for details.
