# Changelog

All notable changes to the printer-connector are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/), and the project aims to
follow [Semantic Versioning](https://semver.org/). Tagging `vX.Y.Z` triggers the
release workflow, which publishes the cross-compiled binaries and the macOS app.

## [Unreleased]

## [0.7.1] - 2026-07-23

### Fixed
- **Bambu printers never appeared in the menu-bar app**, even though the
  connector could discover them. Two independent causes, either of which hid
  them on its own:
  - Pairing only probed for Bambu when an **"I have Bambu Lab printers"
    checkbox was ticked**, and it defaulted to off. Pasting a token and
    clicking Connect — including after tapping "Pair" on a scanned printer,
    which routes through the same screen — registered Klipper printers and
    silently skipped every Bambu. The probe is now unconditional; the checkbox
    is gone. It existed because Bambu discovery used to be a slow SSDP listen
    that a running slicer could block, which the v0.7.0 TLS sweep fixed.
  - **Scanning discarded discovered Bambu printers.** The helper reports them
    under their own `bambu` key (they need an access code before they can be
    linked) and the scan handler only read `printers`, so a Bambu was found and
    then dropped before it reached the list.

## [0.7.0] - 2026-07-23

The first release validated against real Bambu Lab hardware (an A1 mini on
firmware 01.08.01.00). Bambu support previously existed but had never run
against a physical printer; every fix below was reproduced and verified on one.

**If you run a Bambu printer, this release is the difference between "no
printers found" and a working integration.**

### Added
- **Homebrew install for macOS** — `brew install --cask kurenn/tap/spoolr-connect`.
  Like `install-macos.sh`, the cask clears the download quarantine, so there's
  no Gatekeeper prompt; `brew upgrade` and `brew uninstall --zap` manage it.
- **TLS-certificate sweep for Bambu discovery**, alongside the existing SSDP
  listener. Every Bambu answers MQTT/TLS on `:8883` with a certificate whose
  subject is the printer serial, so printers are found without an access code
  and without cooperation from any slicer.
- Bambu telemetry now carries the **printer model and module firmware
  versions** (a `get_version` request is issued on connect).

### Changed
- `job.elapsed_s` is now **optional** in the canonical telemetry contract.
  Drivers that cannot report elapsed time omit it instead of publishing `0`.
- Bambu control commands publish at **QoS 0** (see Fixed).
- Command failures on shared code paths no longer name Moonraker when the
  printer is a Bambu.

### Fixed
- **Bambu printers were undiscoverable whenever a slicer was running.**
  Bambu Studio and Orca bind the SSDP port (UDP `:2021`) without
  `SO_REUSEPORT`, so the connector's listener could not start — and the bind
  error was swallowed, making a permanent failure look identical to "no
  printers on this network". Most Bambu owners run a slicer, so onboarding was
  blind for exactly the users it targeted. The TLS sweep now finds them
  regardless, and a degraded SSDP path is reported rather than hidden.
- **Remote control of a Bambu never worked.** Control commands were published
  at QoS 1, which Bambu's broker never acknowledges, so every command blocked
  the full publish timeout and reported failure — even when the printer had
  obeyed. Publishing at QoS 0 (what the printer expects, and what the driver's
  own working requests already used) fixes it.
- **`start_print` always failed on Bambu** with `print_error 0x0500C010`. The
  firmware verifies the 3MF's md5 before printing and the driver was sending an
  empty one. `StartPrint` now streams the file back over FTP, hashes it, and
  includes the digest.
- **Bambu file listings were always empty.** Only the FTP root was listed, and
  on real firmware the root holds nothing but directories — prints live under
  `/cache` and the factory samples under `/model`. Listings now cover all
  three, and the returned paths work with `start_print` and `delete_file`.
- **Open-frame Bambu models reported a chamber temperature they cannot
  measure.** An A1 mini publishes a fixed `chamber_temper` of 5 whether it is
  mid-print at 65 °C or cooling to ambient; that placeholder is no longer
  forwarded as a reading.
- **The agent ran commands the printer doesn't support.** `Driver.Capabilities`
  was documented as the gate for remote commands but nothing enforced it, so an
  unsupported action reached the driver and surfaced a confusing
  protocol-specific error. Unsupported actions are now refused up front with
  the printer's supported actions attached.

### Notes
- **Bambu remote control requires the printer in LAN Only Mode.** A
  cloud-bound Bambu accepts LAN telemetry but silently ignores LAN control
  commands, at any QoS — verified on hardware. Monitoring (discovery,
  telemetry, status, file listings) works in either mode; only pause / resume /
  cancel / start need LAN Only Mode.

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
