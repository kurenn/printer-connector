You are GitHub Copilot working inside the repository `printer-connector` (Go).

PROJECT GOAL
Build a small, reliable “connector agent” that runs on a Raspberry Pi / Linux host inside Klipper printers and securely connects them to a Rails cloud app (“PrintDock”). The agent:
- Registers once with a pairing token to obtain long-lived credentials (connector_id + connector_secret).
- Sends heartbeats to keep the connector “online”.
- Pushes periodic snapshots (Moonraker status payloads) to the cloud.
- Polls for pending commands (pause/resume/cancel/etc.) and executes them via Moonraker.
- Reports command completion back to the cloud.
- Must be stable, observable, and safe to run unattended (systemd service).

TECH STACK / CONSTRAINTS
- Language: Go 1.23, zero external dependencies (stdlib only).
- Logging: slog (structured logging). Always include connector_id, printer_id, command_id, duration_ms, or error in log attrs.
- Configuration: JSON file on disk (0600 perms). On first run contains pairing_token; after pairing it MUST be atomically rewritten using config.SaveAtomic (write temp + rename) to remove pairing_token and store connector_id + connector_secret.
- OS targets: Linux arm64 (Raspberry Pi), also works on amd64 for local dev.
- No interactive prompts inside the agent; only the install script is interactive.
- The agent must not require Docker or external services (no tailscale/ngrok/etc. in MVP).
- Never log secrets or pairing tokens.

API CONTRACT (Rails)
Assume these endpoints exist and must be called by the agent:
- POST /api/v1/connectors/register        (public, pairing token)
- POST /api/v1/connectors/:id/heartbeat   (auth)
- GET  /api/v1/connectors/:id/commands    (auth) -> returns JSON ARRAY of commands
- POST /api/v1/commands/:id/complete      (auth)
- POST /api/v1/snapshots/batch            (auth)
Auth headers:
- Authorization: Bearer <connector_secret>
- X-Connector-Id: <connector_id>

IMPORTANT: connector_id may be returned as a number or string from Rails on register; handle both using cloud.StringOrNumber type.

Moonraker API calls:
- POST /printer/objects/query with objects: print_stats, virtual_sdcard, extruder, heater_bed, toolhead, pause_resume
- POST /printer/print/pause, /printer/print/resume, /printer/print/cancel
- POST /printer/print/start?filename=<filename>

DESIGN GUIDELINES
- Keep modules separated:
  - internal/config: Load/Config.Validate/SaveAtomic (write temp + rename). Config defaults: PollCommandsSeconds=3, PushSnapshotsSeconds=30, HeartbeatSeconds=10, StateDir=/var/lib/printer-connector.
  - internal/cloud: HTTP client with 5s timeout, 2s dial, 3s TLS handshake. All requests use doJSON helper. Auth via authHeaders() returning Bearer token + X-Connector-Id.
  - internal/moonraker: Minimal Moonraker client. QueryObjects() polls status; Pause/Resume/Cancel/StartPrint execute commands.
  - internal/agent: Three concurrent goroutines (heartbeatLoop, commandsLoop, snapshotsLoop). Each uses ticker + exponential backoff (util.Backoff 1s-60s) on errors.
  - internal/util: Backoff helper with jitter (0.75-1.25x multiplier).
  - cmd/connector: CLI flags --config (required), --log-level (debug|info|warn|error), --once (debug mode: run one iteration and exit).
- Agent.Run() flow: if pairing_token present, call pair() first; then launch 3 goroutines; wait for ctx.Done() or first error from errCh.
- Pairing: pair() exchanges pairing_token for connector_id + connector_secret, and automatically populates printer_ids from Rails response (matched by array index to cfg.Moonraker entries). Config is atomically rewritten with all credentials.
- Commands: pollAndExecuteCommands fetches up to 20 commands, executes each sequentially, captures post-command snapshot, calls CompleteCommand with status="succeeded" or "failed".
- Heartbeat: includes uptime_seconds, version, and per-printer reachability (pings QueryObjects to test).
- Use context cancellation (SIGINT/SIGTERM captured in main) for graceful shutdown.
- Network behavior:
  - Use timeouts on all HTTP clients (cloud: 5s total, moonraker: 5s total).
  - On transient failures, log warning and continue with exponential backoff.
  - Do not crash on 4xx/5xx unless it is unrecoverable (e.g., auth invalid repeatedly).
- Security:
  - Persist connector_secret in config after pairing using SaveAtomic with 0600 permissions.
  - Validate that commands are only executed for printer_id present in cfg.Moonraker slice.

TESTING EXPECTATIONS
- Add unit tests for:
  - config rewrite behavior (pairing token removed, id/secret saved)
  - cloud.StringOrNumber unmarshal (handles both "123" and 123 from JSON)
  - cloud client decoding for commands (array response)
  - util.Backoff exponential growth with jitter
- Add lightweight integration tests/mocks where practical (httptest.Server for cloud API).

BUILD & RUN
Build:
```bash
go build -o printer-connector ./cmd/connector
```

Cross-compile for Raspberry Pi:
```bash
GOOS=linux GOARCH=arm64 go build -o dist/printer-connector-linux-arm64 ./cmd/connector
```

Run locally:
```bash
./printer-connector --config config/config.dev.json --log-level debug
```

Debug mode (one iteration):
```bash
./printer-connector --config config/config.dev.json --once
```

INSTALL SCRIPT
- install.sh must remain POSIX-ish bash and work on Debian/Raspberry Pi OS.
- Default paths: BIN_DST=/usr/data/printer-connector/printer-connector, CONFIG_PATH=/usr/data/printer-connector/config.json, STATE_DIR=/usr/data/printer-connector/state
- Supports interactive mode (prompts for cloud_url, pairing_token, printer details) or non-interactive with flags.
- Creates systemd unit at /etc/systemd/system/printer-connector.service with User=root, Restart=always.
- Runs agent once with --once to complete pairing before enabling service.
- Do not echo secrets in output (use [REDACTED] or similar).

WHEN MAKING CHANGES
- Keep diffs small and focused.
- Prefer backward compatibility for config fields (don’t rename without migration).
- If you change an endpoint path or payload shape, update both cloud client types and README examples.
- When adding new command actions, update internal/agent/commands.go switch statement and document in README.
- All new code should follow existing patterns: slog for logging, context propagation, error wrapping with fmt.Errorf.

TASK STYLE
When asked to implement a feature, produce:
1) The code changes (with clear file paths)
2) Any necessary config/schema changes
3) Tests
4) A short “How to try locally” section (commands)

DO NOT
- Introduce heavy frameworks.
- Add unrelated refactors.
- Print or log connector secrets.