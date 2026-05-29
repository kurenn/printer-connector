# Spoolr Connect (printer-connector) — Claude Code Project Guide

**Stack:** Go 1.23 LAN agent (`cmd/connector` + `internal/*`) + Swift 6 SwiftUI macOS menu-bar app (`macos/SpoolrConnect`, SwiftPM).

This is the on-premise **connector** that bridges 3D printers on a local network (Klipper/Moonraker, Bambu Lab; PrusaLink & Elegoo/SDCP planned) to the Spoolr cloud (the `print_dock` Rails app). It is **push-based**: the connector lives behind NAT, so the cloud can never reach in — the agent *polls* the cloud for commands and *pushes* heartbeats, telemetry snapshots, webcam frames, backups, and g-code out to the `api/v1` namespace. Treat pairing/registration, the cloud client, remote printer-control commands, and anything that drives physical hardware with extra care — they cross trust boundaries.

---

## How work happens on this project

### 1. gofmt is a hard CI gate — never skip it

CI (`.github/workflows/ci.yml`) runs, in order: **`gofmt -l .` → `go vet ./...` → `go build ./...` → `go test -race ./...`**. The very first step fails the whole run if *any* file is not gofmt-clean. **Always `gofmt -w` your changes before committing** (or `go fmt ./...`). A red CI on this repo is most often just an unformatted file. CI is **Go-only** — the Swift menu-bar app is not built or tested in CI (verify it locally with `swift test`).

### 2. Work in a worktree, finish with a PR

Every non-trivial change starts in a git worktree off `main`, never on `main` directly:

```sh
git worktree add .worktrees/<short-name> -b <branch-name> origin/main
cd .worktrees/<short-name>
```

Branch naming: `feature/…`, `fix/…`, `chore/…`, `refactor/…`. Before opening the PR, locally reproduce CI (`gofmt -l .` clean, `go vet ./...`, `go build ./...`, `go test -race ./...`) and — if you touched `macos/` — `swift build && swift test`. Open the PR with `gh pr create`, let CI go green, then squash-merge.

> **Heads-up for stacked PRs:** merging a base PR with `--delete-branch` will **auto-close** any PR stacked on that branch. Rebase the dependent branch `--onto origin/main` and open a fresh PR.

### 3. Verify before claiming done

Run the real commands and show output. Tests must be deterministic — never hit the real network or a real printer; use `net/http/httptest`, `t.TempDir()`, and fakes (see existing `*_test.go`).

---

## Core principles

- **Simplicity first** — minimal-impact changes, no speculative abstractions, no error handling for impossible cases.
- **Root causes, not workarounds** — fix the underlying issue.
- **Don't branch on printer type** — add a `driver.Driver` implementation instead (see below).

---

## Architecture

### The Go agent (`cmd/connector` + `internal/`)

- **`cmd/connector`** — the CLI / `package main`. Subcommands: the default (run the agent), **`discover`** (LAN sweep → JSON), **`register --token <T>`** (discover + register every printer under one pairing token, write `connector.json`), plus log-level helpers. The Swift app shells out to `discover` and `register`. This package is thin glue (flags + network + `os.Exit`); keep logic in `internal/` where it's testable.
- **`internal/agent`** — the long-running agent. `Run(ctx)` launches supervised goroutines via `superviseLoop` (panic recovery + restart after `restartDelay`): **heartbeat, commands, snapshots, webcam, webcam_stream, watchdog**. New background work = a new `superviseLoop`'d loop, not an unsupervised `go func`. `pair()` performs first-run registration.
- **`internal/driver`** — the **protocol seam**. `Driver` is the only way the agent talks to a printer (Telemetry, QueryObjects, Pause/Resume/Cancel/Home/StartPrint, file ops, history, webcam, `Capabilities()`). `Telemetry` is the canonical, protocol-agnostic shape (`SchemaVersion = 1`, mirrors `print-contracts/schemas/printer_telemetry.json`). **Adding a protocol means adding a `Driver` implementation — never branching on printer type across the agent or cloud.** `Capabilities()` gates which remote commands a printer accepts.
- **`internal/moonraker`, `internal/bambu`** — the `Driver` implementations (Moonraker HTTP/WebSocket; Bambu native MQTT+FTPS).
- **`internal/cloud`** — the HTTP client to the Rails `api/v1`. Methods: `Register`, `Heartbeat`, `PushSnapshots` (`/snapshots/batch`), `GetCommands`/`CompleteCommand`, `GetWebcamRequests`/`GetWebcamStreamRequests`/`MarkWebcamRequestFailed`/`UploadWebcamSnapshot`/`UploadWebcamStreamFrame`, `UploadBackup`/`UploadGcode` (to presigned URLs), `SetCredentials`. Auth is the connector secret via `Authorization` + `X-Connector-Id` headers. Wire format is **snake_case JSON** (e.g. register sends per-printer `host`/`moonraker_port`). This is the trust boundary with the cloud — it has the broadest test coverage (`client_test.go`, httptest round-trips).
- **`internal/discovery`** — LAN sweep (probe Moonraker `:7125`) + Bambu **SSDP** (`239.255.255.250:2021`).
- **`internal/gcode`, `internal/backup`** — fetch the active print's g-code (for the cloud's 3D viewer) and printer config backups; upload via presigned URLs.
- **`internal/config`** — `connector.json` at `~/Library/Application Support/Spoolr/connector.json` (`connector_id`, `connector_secret`, `cloud_url`, poll intervals, `printers[]`). `DefaultCloudURL = https://www.spoolr.io`.
- **`internal/util`** — backoff and shared helpers.

### The macOS menu-bar app (`macos/SpoolrConnect`)

SwiftPM package (`swift build` / `swift run` / `swift test`; package the `.app` with `scripts/build_app.sh [--install]`). It's the on-ramp UI: scan the LAN, paste a pairing code, link printers, then run the agent.

- **`FleetModel`** — the popover state machine (`attention`/`empty`/`tokenEntry`/`scanning`/`linking`/`bambuCredentials`/`justPaired`). Drives the views in `RootView`/`TransientViews`/`AttentionModeView`.
- **`RegisterService` / `DiscoveryService` / `AgentService`** — shell out to the **bundled** `connector` binary (`register`, `discover`, and the long-running agent). Pairing always requires a code (the auth boundary) — there is no fake/mock pairing path.
- **`ConnectorConfig`** — reads `connector.json` on launch so a relaunch stays paired (boots the connected home instead of re-prompting); also feeds the rescan dedupe (`registeredHosts`).
- The UI's per-printer fleet data is still sample/mock; **real fleet status lives in the web app**, not the menu-bar UI (the live-telemetry seam is unwired).

---

## Conventions

- **Tests** — table-driven where sensible; `httptest.NewServer` for HTTP; `t.TempDir()` for files; fakes for printers. Mirror the existing `*_test.go` in each package. `cmd/connector` and pure interfaces (`internal/driver`) are intentionally light on tests.
- **No printer-type conditionals** — go through `driver.Driver`.
- **Loops are supervised** — wrap new agent loops in `superviseLoop`.
- **Pairing tokens are single-use** — `register` consumes one and writes `connector.json`. `RegisterService` runs `register --token` **without** `--cloud`, so the cloud URL resolves: existing `connector.json` `cloud_url` → `CLOUD_URL` env → `DefaultCloudURL` (production). To pair against **dev**, `connector.json` must already carry `cloud_url: "http://localhost:3000"` before registering.

## Build & deploy

- **Connector binary:** `GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=<ver>" -o dist/printer-connector ./cmd/connector`.
- **Menu-bar `.app`:** `macos/SpoolrConnect/scripts/build_app.sh [--install]` — builds release, **bundles a fresh connector binary** at `Contents/Resources/printer-connector`, ad-hoc codesigns, backs up the existing app, installs to `/Applications`, and relaunches. `connector.json` lives **outside** the bundle, so reinstalling keeps the pairing. Do **not** boot the app with `bin/dev`-style foreman detached; run the binary directly.
- **Swap a dev connector into the installed app:** quit the app → replace `Contents/Resources/printer-connector` → `codesign --force --sign - <path>` (ad-hoc) → relaunch.
