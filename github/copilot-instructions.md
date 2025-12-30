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
- Language: Go (keep dependencies minimal).
- Logging: use structured logging (slog or project logger), include connector_id, printer_id, request ids when possible.
- Configuration: JSON file on disk. On first run it may contain pairing_token; after pairing it MUST be rewritten to remove pairing_token and store connector_id + connector_secret.
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

IMPORTANT: connector_id may be returned as a number or string from Rails on register; handle both.

DESIGN GUIDELINES
- Keep modules separated:
  - internal/config: load/save config, atomic writes (write temp + rename), file perms guidance.
  - internal/cloud: http client for Rails API, typed request/response structs, error handling.
  - internal/moonraker: minimal Moonraker client for needed endpoints (printer.info/status, pause/resume/cancel, etc.).
  - internal/agent: orchestration loop (heartbeat ticker, snapshot ticker, command poll loop), graceful shutdown.
  - cmd/connector: CLI entrypoint (flags: --config, --log-level, --once).
- Polling intervals should be configurable in config with sane defaults.
- Use context cancellation and handle SIGINT/SIGTERM to exit cleanly.
- Network behavior:
  - Use timeouts on all HTTP clients.
  - On transient failures, log warning and keep running.
  - Do not crash on 4xx/5xx unless it is unrecoverable (e.g., auth invalid repeatedly).
- Security:
  - Persist connector_secret in config after pairing; restrict config file permissions (install script does chmod/chown).
  - Validate that snapshots/commands are only for printers listed in config.

TESTING EXPECTATIONS
- Add unit tests for:
  - config rewrite behavior (pairing token removed, id/secret saved)
  - JSON “string or number” unmarshal helper
  - cloud client decoding for commands (array response)
- Add lightweight integration tests/mocks where practical (httptest server).

INSTALL SCRIPT
- install.sh must remain POSIX-ish bash and work on Debian/Raspberry Pi OS.
- It should run interactively when no args are passed and write /etc/printer-connector/config.json.
- It runs the agent once to pair, then starts systemd service printer-connector.service.
- Do not include secrets in echoed output.

WHEN MAKING CHANGES
- Keep diffs small and focused.
- Prefer backward compatibility for config fields (don’t rename without migration).
- If you change an endpoint path or payload shape, update the client + docs/comments accordingly.

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