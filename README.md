# printer-connector (MVP)

A lightweight LAN connector agent that bridges a Rails cloud API and local Moonraker (Klipper) instances.

## Features (v0.1.0)
- Pairing via one-time token: `POST /api/v1/connectors/register`
- Heartbeat loop: `POST /api/v1/connectors/heartbeat`
- Commands poll loop (pause/resume/cancel/start_print): `GET /api/v1/connectors/:id/commands`
- Command completion: `POST /api/v1/commands/:id/complete`
- Snapshot batch push: `POST /api/v1/snapshots/batch`
- Moonraker object query snapshots: `POST /printer/objects/query`

## Build
```bash
go build -o printer-connector ./cmd/connector
```

Cross-compile:
```bash
GOOS=linux GOARCH=amd64 go build -o dist/printer-connector-linux-amd64 ./cmd/connector
GOOS=linux GOARCH=arm64 go build -o dist/printer-connector-linux-arm64 ./cmd/connector
```

## Installation

### Quick Install (Automated)

**Vanilla Klipper / Linux with systemd:**
```bash
# Download and run the installer
curl -fsSL https://raw.githubusercontent.com/kurenn/printer-connector/main/install.sh -o install.sh
chmod +x install.sh
sudo ./install.sh
```

**Creality K1 Max:**
```bash
# Download and run the K1 installer
curl -fsSL https://raw.githubusercontent.com/kurenn/printer-connector/main/install-k1.sh -o install-k1.sh
chmod +x install-k1.sh
sudo sh install-k1.sh
```

The installer will:
1. Prompt for your cloud URL and pairing token
2. Configure printer connection details
3. Create a systemd service (or init.d script on K1)
4. Complete the pairing process
5. Start the service automatically

### Manual Run
```bash
sudo ./printer-connector --config /etc/printer-connector/config.json
```

## Uninstallation

To remove printer-connector from your system:

```bash
# Download and run the uninstaller
curl -fsSL https://raw.githubusercontent.com/kurenn/printer-connector/main/uninstall.sh -o uninstall.sh
chmod +x uninstall.sh
sudo ./uninstall.sh
```

The uninstaller automatically detects your installation type (vanilla Klipper or K1 Max) and removes:
- Binary files
- Configuration files (including credentials)
- State directories
- Systemd service (vanilla) or init.d scripts (K1)
- Log files

**Options:**
```bash
sudo ./uninstall.sh           # Interactive (asks for confirmation)
sudo ./uninstall.sh --yes     # Skip confirmation
sudo ./uninstall.sh --config-only  # Only remove config/credentials (keep binary)
```

## Example config
```json
{
  "cloud_url": "https://your-app.com",
  "pairing_token": "PAIR_TOKEN_FROM_RAILS",
  "site_name": "Workshop",
  "poll_commands_seconds": 3,
  "push_snapshots_seconds": 30,
  "heartbeat_seconds": 10,
  "state_dir": "/var/lib/printer-connector",
  "moonraker": [
    { "printer_id": 1, "name": "Gonzalez", "base_url": "http://192.168.68.86:7125" }
  ]
}
```

On first run, the agent will exchange `pairing_token` for `connector_id` + `connector_secret`,
then rewrite the config (atomically) to remove `pairing_token`.

## systemd unit (recommended)
Create `/etc/systemd/system/printer-connector.service`:

```ini
[Unit]
Description=Printer Connector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=printer-connector
Group=printer-connector
ExecStart=/usr/local/bin/printer-connector --config /etc/printer-connector/config.json
Restart=always
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/printer-connector
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

Enable:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now printer-connector
sudo journalctl -u printer-connector -f
```
