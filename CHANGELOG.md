# Changelog

All notable changes to the printer-connector are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/), and the project aims to
follow [Semantic Versioning](https://semver.org/). Tagging `vX.Y.Z` triggers the
release workflow, which publishes the cross-compiled binaries and the macOS app.

## [Unreleased]

### Added
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
