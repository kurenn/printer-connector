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

## Run
```bash
sudo ./printer-connector --config /etc/printer-connector/config.json
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
