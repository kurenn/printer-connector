# 🖨️ Spoolr Connect (printer-connector)

> An open-source on-premise agent that bridges 3D printers on your local network to the [Spoolr](https://www.spoolr.io) cloud — outbound-only, no port forwarding, no VPN.

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.23-00ADD8.svg)](https://golang.org)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux%20%7C%20Raspberry%20Pi%20%7C%20K1-lightgrey.svg)](https://github.com/kurenn/printer-connector)
[![Release](https://img.shields.io/github/v/release/kurenn/printer-connector?label=release)](https://github.com/kurenn/printer-connector/releases/latest)

## 📋 Table of Contents

- [What is Printer Connector?](#what-is-printer-connector)
- [Why Use Printer Connector?](#why-use-printer-connector)
- [How It Works](#how-it-works)
- [Supported Printers](#supported-printers)
- [Quick Start](#quick-start)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
  - [Verification](#verification)
- [Updating](#updating)
- [Uninstallation](#uninstallation)
- [Configuration](#configuration)
- [Troubleshooting](#troubleshooting)
- [Advanced Usage](#advanced-usage)
- [Development](#development)
- [API Integration (for Backend Developers)](#api-integration-for-backend-developers)
- [FAQ](#faq)
- [License](#license)

---

## 🤔 What is Spoolr Connect?

**Spoolr Connect** is a small Go agent that runs on a machine inside your network (a Raspberry Pi, your Mac, a Windows PC, or the printer itself on a Creality K1) and bridges your 3D printers to the [Spoolr](https://www.spoolr.io) cloud. It speaks each printer family's native protocol — **Moonraker** for Klipper / Voron / K1, **MQTT + FTPS** for Bambu Lab in LAN mode — so you can:

- 📊 **Monitor** print status, temperatures, and progress from anywhere
- 🎮 **Control** prints remotely (pause, resume, cancel, home, start a file)
- 📸 **See live webcam snapshots** of the print bed
- 🚀 **Send g-code** straight from a browser-side slicer to the printer
- 🔔 **Get push notifications** for print start, completion, and failures

It runs as a background service, a macOS menu-bar app, or a one-line shell installer — **whichever fits the machine you put it on**.

---

## 💡 Why Spoolr Connect?

### The problem
Most 3D printers can't be reached from outside your home / shop network without:
- Opening ports on your router (security risk)
- Setting up a VPN (complex for non-technical users)
- Trusting third-party agents that ship your stream to someone else's cloud

### The solution
Spoolr Connect is **push-based**: the agent lives behind NAT, polls Spoolr for commands, and pushes telemetry out — the cloud can never reach in. Plus:

- ✅ **Outbound-only HTTPS** — no open ports, no port-forwarding, no VPN
- ✅ **Open source, MIT licensed** — read the code, build it yourself, run it offline
- ✅ **Secure auth** — pairing tokens are single-use; per-connector secrets after pairing
- ✅ **Multi-platform** — macOS menu-bar app, Windows Service, Linux systemd, K1 / K1 Max init.d
- ✅ **Multi-protocol** — Klipper / Moonraker + Bambu Lab today; PrusaLink and Elegoo / SDCP planned
- ✅ **Lightweight** — single static Go binary, < 6 MB, no runtime deps

---

## 🔧 How It Works

```
┌─────────────────┐         ┌──────────────────┐         ┌─────────────┐
│   Your Phone    │         │  Spoolr Cloud    │         │  Connector  │
│   or Computer   │◄────────┤  (spoolr.io)     │◄────────┤  + Printer  │
│                 │         │                  │         │  on your    │
│  Web Interface  │         │  REST  api/v1    │         │  network    │
└─────────────────┘         └──────────────────┘         └─────────────┘
                                     ▲                          │
                                     │       outbound HTTPS     │
                                     └──────────────────────────┘
```

The agent runs supervised background loops, each on its own cadence:

1. **Pairing** (one time) — you paste a pairing token into the menu-bar app, the Windows installer, or `register --token <T>`. The connector discovers every printer on your LAN, registers them with the cloud under that token, and gets back a per-connector secret. The token is single-use.
2. **Heartbeat** (~10s) — "I'm alive" so the cloud can show your fleet as online.
3. **Telemetry snapshots** (~30s) — temperatures, print progress, current layer, ETA — pushed to `/api/v1/snapshots/batch`.
4. **Commands** (~3s) — agent polls `/api/v1/commands` and runs anything queued: pause, resume, cancel, home, start a print, upload a g-code file, snapshot a webcam frame.
5. **Webcam frames** — opportunistic snapshots + an optional MJPEG-style stream pushed up while a viewer is open in the web app.
6. **Periodic re-discovery** (added in v0.5.0) — sweeps the LAN after pairing and auto-adopts newly-found printers, so plugging in a second printer doesn't require re-running the installer.

All cadences are configurable via `connector.json`.

---

## 🖨️ Supported Printers

Each printer family is implemented as a `driver.Driver` — the agent itself never branches on printer type. Adding a new protocol is a new driver, not a fork.

### ✅ Klipper / Moonraker (HTTP + WebSocket)
- Voron 2.4 / Trident / 0.1 / Switchwire
- Creality **K1 / K1 Max** (Klipper-based, ships with Moonraker)
- Prusa MK3 / MK4 running Klipper
- Ender 3 / Ender 5 with a Klipper upgrade
- Any custom Klipper build with Moonraker exposed (default port `7125`)

### ✅ Bambu Lab (native MQTT + FTPS, LAN mode)
- X1 / X1C, P1P / P1S, A1 / A1 mini
- Requires **LAN Mode / Developer Mode** on the printer
- Needs the printer's **access code** + **serial number** (the menu-bar app and the Windows installer prompt for these during pairing)
- See [docs/BAMBU_INTEGRATION.md](docs/BAMBU_INTEGRATION.md) for the protocol details

### 🛠 Planned
- **PrusaLink** — for Prusa printers not running Klipper
- **Elegoo / SDCP** — Saturn, Mars resin printers

### 📋 What you need
- Klipper: Moonraker reachable on the LAN (usually `:7125`)
- Bambu: printer in LAN Mode, MQTT `:8883` + FTPS `:990` reachable
- A machine to host the connector — see Installation below for the four options

---

## 🚀 Quick Start

### Prerequisites

Before installing, make sure you have:

1. **A 3D printer running Klipper + Moonraker**
   - Check by opening: `http://YOUR_PRINTER_IP:7125/server/info`
   - You should see JSON response with Moonraker info

2. **SSH access to your printer's Raspberry Pi**
   ```bash
   ssh pi@YOUR_PRINTER_IP
   ```
   Or for K1 Max: `ssh root@YOUR_K1_IP`

3. **A pairing token from your cloud account**
   - On [spoolr.io](https://www.spoolr.io), go to **Connectors → Add Connector** and copy the code shown.
   - It looks like: `PAIR_abc123xyz456` — single-use, expires within ~24 h.

---

### Installation

Pick the option that matches **where you want the connector to run** — not where the printer is:

| Where it runs               | Best for                                         | Option |
|-----------------------------|--------------------------------------------------|--------|
| 🖥  Your **Mac**             | The fastest setup; LAN discovery + GUI pairing   | [3](#option-3-macos-menu-bar-app-spoolr-connect) |
| 🪟  A **Windows** PC         | Always-on desktop; runs as a background Service  | [4](#option-4-windows-gui-installer)             |
| 🥧  A **Raspberry Pi**       | Same Pi already runs Klipper/Moonraker           | [1](#option-1-vanilla-klipper-raspberry-pi-with-systemd) |
| 🟢  A **Creality K1 / K1 Max** (on the printer itself) | No extra hardware       | [2](#option-2-creality-k1--k1-max) |

The agent is the same Go binary in every case — the install paths just wrap it in the right service for the host OS.

#### Option 1: Vanilla Klipper (Raspberry Pi with systemd)

Run this **one command** on your Raspberry Pi:

```bash
wget -qO- https://raw.githubusercontent.com/kurenn/printer-connector/main/install-klipper.sh | sudo bash
```

Or download and run manually:

```bash
# Download the installer
wget https://raw.githubusercontent.com/kurenn/printer-connector/main/install-klipper.sh

# Make it executable
chmod +x install-klipper.sh

# Run the installer
sudo ./install-klipper.sh
```

The installer will ask you for:
1. **Cloud URL**: Defaults to `https://www.spoolr.io`; override only if you self-host.
2. **Pairing Token**: The code from your Spoolr connectors page — one token registers every printer on the LAN.
3. **Printer Details**: Auto-discovered; the installer only prompts if discovery returned nothing.

#### Option 2: Creality K1 / K1 Max

Run this **one command** on your K1:

```bash
wget -qO- https://raw.githubusercontent.com/kurenn/printer-connector/main/install-k1.sh | sh
```

Or download and run manually:

```bash
# Download the K1 installer
wget https://raw.githubusercontent.com/kurenn/printer-connector/main/install-k1.sh

# Make it executable
chmod +x install-k1.sh

# Run the installer
sudo sh install-k1.sh
```

#### Option 3: macOS menu-bar app (Spoolr Connect)

The **Spoolr Connect** menu-bar app (universal, Apple Silicon + Intel) is both the
installer *and* the day-to-day dashboard — it scans your LAN, pairs your fleet,
runs the agent in the background, and surfaces live status without ever needing a
terminal. Install with **one command** (no security prompt this way):

```bash
curl -fsSL https://raw.githubusercontent.com/kurenn/printer-connector/main/install-macos.sh | bash
```

The app is ad-hoc signed (not notarized — Spoolr has no paid Apple account), so a
browser download instead shows a one-time Gatekeeper prompt. See
**[docs/INSTALL-macOS.md](docs/INSTALL-macOS.md)** for the download/"Open Anyway"
flow and build-from-source instructions.

**What the popover shows (left-click the menu-bar icon):**

- 📊 **Live per-printer status** — every printer in your fleet with its current
  state (printing / idle / error / offline), job filename, layer, progress %, and
  ETA, updated every 3 s from the agent's local `status.json`.
- 🟢 **Agent-health row** — if the bundled agent subprocess stops writing for
  > 90 s, the popover surfaces "Agent not responding" (yellow) or "Agent
  stopped" (red) with a one-click **Restart Agent** button.
- ⬆️ **Update banner** — when a newer release ships, a yellow strip at the top
  of the popover offers **Download**. The app checks GitHub on launch
  (debounced to once per 12 h) and every 24 h after.

**What the right-click menu does (right-click or ctrl-click the menu-bar icon):**

- **Check for Updates Now** — forces an immediate check, ignoring the 24 h
  debounce.
- **Launch at Login** — toggle that registers the app with
  `SMAppService.mainApp`, so the agent comes back up automatically after every
  reboot.
- **Restart Agent** — same as the popover button, useful if you've already
  closed the popover.
- **Quit Spoolr Connect**.

#### Option 4: Windows (GUI installer)

Download **`SpoolrConnect-Setup.exe`** from the
[latest release](https://github.com/kurenn/printer-connector/releases/latest)
and run it. The wizard collects your pairing code and installs the connector as
the **Spoolr Connect** Windows Service (auto-start, background — no desktop window).

The installer is unsigned (no Authenticode cert); SmartScreen will warn. See
**[docs/INSTALL-Windows.md](docs/INSTALL-Windows.md)** for the full walkthrough,
including how to dismiss the SmartScreen dialog and verify the service is running.

### What the Installer Does

1. ✅ Creates installation directory
2. ✅ Downloads the connector binary
3. ✅ Generates configuration file
4. ✅ Tests connection to Moonraker
5. ✅ Completes pairing with cloud
6. ✅ Creates auto-start service
7. ✅ Starts the connector

---

### Verification

After installation, verify everything is working:

#### Check Service Status

**For Klipper (systemd):**
```bash
sudo systemctl status printer-connector
```

You should see: `Active: active (running)`

**For K1 Max:**
```bash
/opt/printer-connector/service.sh status
# or check process
ps | grep printer-connector
```

#### View Logs

**For Klipper (systemd):**
```bash
# Real-time logs
sudo journalctl -u printer-connector -f

# Last 50 lines
sudo journalctl -u printer-connector -n 50
```

**For K1 Max:**
```bash
tail -f /opt/printer-connector/connector.log
```

#### What to Look For:

✅ **Success indicators:**
```
INFO connector_id=abc123 msg="Pairing successful"
INFO connector_id=abc123 printer_id=1 msg="Heartbeat sent"
INFO connector_id=abc123 printer_id=1 msg="Snapshot pushed"
```

❌ **Common errors:**
- `connection refused`: Moonraker is not running
- `401 Unauthorized`: Pairing failed or invalid token
- `timeout`: Network issues or wrong cloud URL

---

## 🔄 Updating

How you update depends on how you installed:

| Install                          | How to update                                                                 |
|----------------------------------|-------------------------------------------------------------------------------|
| **macOS menu-bar app**           | The app checks GitHub on launch + every 24 h and shows an "Update available" banner — click **Download**, then re-run `install-macos.sh`. Or right-click the menu-bar icon → **Check for Updates Now** to skip the debounce. |
| **Windows Service**              | Download the latest `SpoolrConnect-Setup.exe` from [Releases](https://github.com/kurenn/printer-connector/releases/latest) and run it — the installer upgrades in place. |
| **Linux / Raspberry Pi / K1**    | Run `update.sh` (covered below).                                              |

### Linux / Pi / K1 — `update.sh`

Run this **one command** on your printer:

```bash
wget -qO- https://raw.githubusercontent.com/kurenn/printer-connector/main/update.sh | bash
```

Or download and run manually:

```bash
wget https://raw.githubusercontent.com/kurenn/printer-connector/main/update.sh
bash update.sh
```

### What the Update Script Does:

1. ✅ **Auto-detects** your installation (K1 Max or vanilla Klipper)
2. ✅ **Checks** current and latest versions
3. ✅ **Stops** the service gracefully
4. ✅ **Backs up** the current binary (just in case)
5. ✅ **Downloads** the latest version from GitHub
6. ✅ **Verifies** the new binary works
7. ✅ **Installs** and restarts the service

### Update Output Example:

```
ℹ ═══════════════════════════════════════════════
ℹ   Printer Connector - Update Script
ℹ ═══════════════════════════════════════════════

ℹ Detected Klipper installation at /home/pi/printer-connector
ℹ Detected architecture: aarch64 (using printer-connector-linux-arm64)
ℹ Current version: 0.1.0
ℹ Checking for latest version...
ℹ Latest version: 0.2.0
ℹ Stopping printer-connector service...
✓ Service stopped
ℹ Backing up current binary...
✓ Backup created: /home/pi/printer-connector/printer-connector.backup-20260103-143022
ℹ Downloading latest version...
✓ Downloaded successfully
ℹ Verifying new binary...
ℹ Installing new binary...
✓ Binary updated
ℹ New version: 0.2.0
ℹ Starting printer-connector service...
✓ Service started successfully

✓ ═══════════════════════════════════════════════
✓   Update completed successfully!
✓ ═══════════════════════════════════════════════

ℹ Summary:
  Old version: 0.1.0
  New version: 0.2.0
  Backup: /home/pi/printer-connector/printer-connector.backup-20260103-143022
```

### Rollback (If Needed)

If the update causes issues, you can roll back to the previous version:

**For Klipper (systemd):**
```bash
sudo systemctl stop printer-connector
mv ~/printer-connector/printer-connector.backup-* ~/printer-connector/printer-connector
sudo systemctl start printer-connector
```

**For K1 Max:**
```bash
/etc/init.d/S99printer-connector stop
mv /opt/printer-connector/printer-connector.backup-* /opt/printer-connector/printer-connector
/etc/init.d/S99printer-connector start
```

### Checking Your Current Version

To see what version you're currently running:

```bash
# For Klipper
~/printer-connector/printer-connector --version

# For K1 Max
/opt/printer-connector/printer-connector --version
```

---

## �🗑️ Uninstallation

To completely remove Printer Connector from your system:

### Simple Uninstall

```bash
# Download the uninstaller
wget https://raw.githubusercontent.com/kurenn/printer-connector/main/uninstall.sh

# Make it executable
chmod +x uninstall.sh

# Run it (will ask for confirmation)
sudo ./uninstall.sh
```

### Uninstall Options

```bash
# Skip confirmation prompt
sudo ./uninstall.sh --yes

# Only remove configuration and credentials (keep binary)
sudo ./uninstall.sh --config-only
```

### What Gets Removed

The uninstaller automatically detects your installation type and removes:

- ✅ Binary files (`/usr/data/printer-connector/` or `/opt/printer-connector/`)
- ✅ Configuration files (including stored credentials)
- ✅ State directories (persistent data)
- ✅ Systemd service (Klipper) or init.d scripts (K1)
- ✅ Log files
- ✅ Auto-start configurations

**Note:** Your Klipper installation and printer settings are NOT affected.

---

## ⚙️ Configuration

The connector config is created automatically when you pair. **You don't normally edit it by hand** — the menu-bar app, the Windows installer, and the `register` subcommand all write it for you.

### Where it lives

| Host                  | Path                                                              |
|-----------------------|-------------------------------------------------------------------|
| macOS (menu-bar app)  | `~/Library/Application Support/Spoolr/connector.json`             |
| Windows (Service)     | `C:\ProgramData\SpoolrConnect\connector.json`                     |
| Linux (systemd)       | `/usr/data/printer-connector/connector.json`                      |
| Creality K1 / K1 Max  | `/opt/printer-connector/connector.json`                           |

### Example (after pairing)

```json
{
  "cloud_url": "https://www.spoolr.io",
  "connector_id": "conn_abc123",
  "connector_secret": "•••• keep this secret ••••",
  "site_name": "Workshop",
  "poll_commands_seconds": 3,
  "push_snapshots_seconds": 30,
  "heartbeat_seconds": 10,
  "rediscover_seconds": 300,
  "state_dir": "./state",
  "printers": [
    {
      "printer_id": 1,
      "type": "moonraker",
      "name": "Voron 2.4",
      "base_url": "http://192.168.1.70:7125",
      "ui_port": 4408
    },
    {
      "printer_id": 2,
      "type": "bambu",
      "name": "X1 Carbon",
      "host": "192.168.1.84",
      "access_code": "12345678",
      "serial": "01S00C123456789"
    }
  ]
}
```

One connector instance manages **every printer it discovers** under a single pairing token — you don't run one connector per printer. After pairing, the agent keeps re-scanning the LAN and adopts new printers automatically (`rediscover_seconds`).

### Fields

| Field                       | Description                                                                 |
|-----------------------------|-----------------------------------------------------------------------------|
| `cloud_url`                 | Spoolr API root. Defaults to `https://www.spoolr.io`; the `CLOUD_URL` env var overrides it (useful for self-hosted/dev backends). |
| `connector_id`              | Written at pairing time. Identifies this connector to the cloud.            |
| `connector_secret`          | Written at pairing time. Bearer credential — treat it like a password.      |
| `site_name`                 | Human-readable location label (optional).                                   |
| `poll_commands_seconds`     | How often to poll `/api/v1/commands`. Default `3`.                          |
| `push_snapshots_seconds`    | How often to push telemetry. Default `30`.                                  |
| `heartbeat_seconds`         | How often to send the keep-alive ping. Default `10`.                        |
| `rediscover_seconds`        | LAN re-discovery cadence (v0.5.0+). Default `300`; `-1` disables.           |
| `state_dir`                 | Working directory for backoff/queue state.                                  |
| `printers[].type`           | `moonraker` (default), `bambu`, `prusalink` (planned), `sdcp` (planned).    |
| `printers[].base_url`       | Moonraker only — the printer's Moonraker URL.                               |
| `printers[].host`           | Bambu only — the printer's LAN IP.                                          |
| `printers[].access_code` / `serial` | Bambu only — required to auth the MQTT + FTPS sessions.             |
| `printers[].ui_port`        | Optional — used by the cloud to deep-link back to the printer's local UI.   |

### Security

⚠️
- The config file is created with `600` permissions (owner read/write only)
- The pairing token is **single-use** — once a `connector_id` + `connector_secret` are written, the token can't be re-used
- The `connector_secret` is the only credential the agent uses after pairing — don't commit it, don't paste it into chats
- The repo's `.gitignore` blocks every `config/*.json` and `connector.json` from being committed by accident

---

## 🔍 Troubleshooting

### Common Issues and Solutions

#### 1. Installation Fails

**Problem:** `Permission denied` or `Access denied`

**Solution:**
```bash
# Make sure you're using sudo
sudo ./install-klipper.sh   # For vanilla Klipper
# or
sudo ./install-k1.sh        # For K1 Max

# Check if you're root on K1
whoami  # should show "root"
```

---

#### 2. Can't Connect to Moonraker

**Problem:** `connection refused` or `timeout` errors

**Solution:**
```bash
# Test Moonraker directly
curl http://127.0.0.1:7125/server/info

# If that fails, check if Moonraker is running
sudo systemctl status moonraker

# Check Moonraker port in moonraker.conf
cat ~/printer_data/config/moonraker.conf | grep port
```

---

#### 3. Pairing Fails

**Problem:** `401 Unauthorized` or `invalid pairing token`

**Solution:**
- Verify your pairing token is correct (copy-paste from cloud service)
- Check that your cloud URL is correct and reachable
- Ensure the token hasn't expired (tokens typically expire after 24 hours)
- Get a new pairing token from your cloud service

---

#### 4. Service Won't Start

**Problem:** Service starts then immediately stops

**Solution:**
```bash
# Check detailed logs
sudo journalctl -u printer-connector -n 100

# Look for error messages
sudo journalctl -u printer-connector | grep ERROR

# Try running manually to see errors
sudo /usr/data/printer-connector/printer-connector \
  --config /usr/data/printer-connector/config.json \
  --log-level debug
```

---

#### 5. Commands Not Working

**Problem:** Can send commands from cloud but nothing happens

**Solution:**
- Check that printer_id in config matches your cloud service
- Verify Moonraker is responsive: `curl http://127.0.0.1:7125/printer/info`
- Check logs for command execution: `journalctl -u printer-connector -f`
- Make sure printer is not in error state

---

#### 6. High CPU or Memory Usage

**Problem:** Connector using too many resources

**Solution:**
```bash
# Increase poll intervals in config
{
  "poll_commands_seconds": 5,      # Increase from 3
  "push_snapshots_seconds": 60,    # Increase from 30
  "heartbeat_seconds": 30          # Increase from 10
}

# Restart service after config change
sudo systemctl restart printer-connector
```

---

### Getting Help

If you're still stuck:

1. **Check Logs:** Always start by checking logs for error messages
2. **Test Connectivity:** Verify Moonraker and cloud service are reachable
3. **Debug Mode:** Run with `--log-level debug` for detailed output
4. **Open an Issue:** Visit [GitHub Issues](https://github.com/kurenn/printer-connector/issues)

Include in your issue:
- Printer type (Voron, K1, etc.)
- Installation method used
- Relevant log excerpts
- Config file (with secrets redacted)

---

## 🛠️ Advanced Usage

### Manual Installation (For Developers)

If you want to install without the automated script:

#### 1. Build the Binary

```bash
# Install Go 1.23+
# Then clone and build
git clone https://github.com/kurenn/printer-connector.git
cd printer-connector
go build -o printer-connector ./cmd/connector

# For Raspberry Pi (from another machine)
GOOS=linux GOARCH=arm64 go build -o printer-connector-arm64 ./cmd/connector

# For K1 Max (MIPS little-endian):
# Use Docker for consistent cross-compilation:
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -c "GOOS=linux GOARCH=mipsle go build -ldflags='-s -w' -o printer-connector-mips ./cmd/connector"
```

#### 2. Create Config Manually

```bash
sudo mkdir -p /usr/data/printer-connector
sudo nano /usr/data/printer-connector/config.json
# Paste your configuration
sudo chmod 600 /usr/data/printer-connector/config.json
```

#### 3. Create systemd Service

Create `/etc/systemd/system/printer-connector.service`:

```ini
[Unit]
Description=Printer Connector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root
ExecStart=/usr/data/printer-connector/printer-connector --config /usr/data/printer-connector/config.json --log-level info
Restart=always
RestartSec=3
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

#### 4. Enable and Start

```bash
sudo systemctl daemon-reload
sudo systemctl enable printer-connector
sudo systemctl start printer-connector
```

---

### Command-Line Interface

The binary is a single executable with subcommands. Run with no subcommand to start the long-running agent.

```bash
printer-connector [--config PATH] [--log-level LEVEL] [--once] [--version]
printer-connector discover [--timeout 5s]
printer-connector register --token <TOKEN> [--cloud <URL>] [--site <NAME>]
printer-connector run-service        # Windows Service mode
printer-connector install-service    # Register the Windows Service
printer-connector uninstall-service  # Unregister the Windows Service
```

| Flag / subcommand        | Purpose                                                                                                |
|--------------------------|--------------------------------------------------------------------------------------------------------|
| (no subcommand)          | Run the agent: heartbeat, snapshots, commands, webcam, rediscovery loops.                              |
| `--config PATH`          | Config file (defaults to the platform-standard `connector.json` path).                                 |
| `--log-level LEVEL`      | `debug` / `info` / `warn` / `error` (default `info`).                                                  |
| `--once`                 | Run one iteration of each loop and exit — handy for smoke tests.                                       |
| `--version`              | Print the version and exit.                                                                            |
| `discover`               | LAN sweep (Moonraker `:7125` probe + Bambu SSDP) → JSON of found printers.                             |
| `register --token <T>`   | Discover, register **every** printer found, write `connector.json`, exit. The macOS app shells out to this. |

**Examples:**

```bash
# Pair against production (one token registers every printer found on the LAN)
./printer-connector register --token PAIR_abc123 --site "Workshop"

# Pair against a local dev backend
./printer-connector register --token PAIR_abc123 --cloud http://localhost:3000

# Run the agent with explicit config + debug logs
./printer-connector --config ./connector.json --log-level debug

# JSON of every printer the agent can see on the network
./printer-connector discover --timeout 10s
```

---

### Multiple printers — one connector

**Important paradigm change since v0.4.0:** a single connector instance manages **all printers on the network**, not one printer per install. `register --token <T>` discovers every printer and registers them together; after pairing, `rediscover_seconds` keeps adopting newly-found ones. You should **not** run multiple connectors on the same network — pick the host (Mac, Pi, K1, Windows PC) that's most likely to be on, install once, and the cloud sees the whole fleet.

---

## 💻 Development

### Prerequisites for Development

- Go **1.23** (the version CI uses; newer should work)
- Git
- Xcode + Swift 6 (only if you touch `macos/SpoolrConnect`)
- Inno Setup 6 (only if you regenerate the Windows installer)

### Setting Up Development Environment

```bash
# Clone the repository
git clone https://github.com/kurenn/printer-connector.git
cd printer-connector

# Install dependencies (uses stdlib only)
go mod download

# Build
go build -o printer-connector ./cmd/connector

# Run tests
go test ./...

# Create your local dev config from the template (config.dev.json is gitignored —
# never commit a real connector_secret), then register or fill it in:
cp config/config.dev.example.json config/config.dev.json

# Run with dev config
./printer-connector --config config/config.dev.json --log-level debug

# Or set CLOUD_URL environment variable for local development
export CLOUD_URL=http://localhost:3000
./printer-connector --config config/config.dev.json --log-level debug
```

### Environment Variables

The connector supports the following environment variables:

- **`CLOUD_URL`**: Override the cloud API URL
  - Default: `https://www.spoolr.io` (production)
  - Development: `export CLOUD_URL=http://localhost:3000` or `http://192.168.1.50:3000`
  - Takes precedence over config file `cloud_url` field

Example for local Rails development:
```bash
export CLOUD_URL=http://localhost:3000
./printer-connector --config config/config.dev.json --log-level debug
```

For systemd services, add to the service file:
```ini
[Service]
Environment="CLOUD_URL=http://192.168.1.50:3000"
```

### Project Structure

```
printer-connector/
├── cmd/connector/                 # CLI: default (agent), discover, register, *-service
├── internal/
│   ├── agent/                     # Supervised loops: heartbeat, snapshots,
│   │                              #   commands, webcam, webcam_stream, watchdog,
│   │                              #   rediscovery, status-file writer
│   ├── driver/                    # The protocol seam (Driver interface)
│   ├── moonraker/                 # Driver — Klipper / Moonraker (HTTP + WS)
│   ├── bambu/                     # Driver — Bambu Lab (MQTT + FTPS)
│   ├── discovery/                 # LAN sweep + Bambu SSDP
│   ├── cloud/                     # HTTP client for api/v1
│   ├── config/                    # connector.json loader + defaults
│   ├── gcode/                     # Active-print g-code upload (for viewer)
│   ├── backup/                    # Printer config backups
│   └── util/                      # Backoff, shared helpers
├── macos/SpoolrConnect/           # SwiftPM menu-bar app (Swift 6 / SwiftUI)
│   ├── Sources/SpoolrConnect/     # UI + state machine + agent shell-out
│   ├── Tests/SpoolrConnectTests/  # XCTest
│   └── scripts/build_app.sh       # Build + bundle "Spoolr Connect.app"
├── windows/installer.iss          # Inno Setup script for SpoolrConnect-Setup.exe
├── docs/                          # GitHub Pages site + INSTALL-{macOS,Windows}.md
├── install-{klipper,k1,macos}.sh  # One-line installers
├── update.sh / uninstall.sh       # Self-managed install lifecycle
├── CHANGELOG.md                   # Keep-a-changelog
└── .github/workflows/             # ci.yml (lint/build/test), release.yml (cut releases)
```

### Making Changes

1. Create a feature branch: `git checkout -b feature/my-feature`
2. Make your changes
3. Test thoroughly: `go test ./...`
4. Build for target platforms:
   ```bash
   GOOS=linux GOARCH=arm64 go build -o dist/printer-connector-arm64 ./cmd/connector
   # K1 Max requires MIPS little-endian with Docker for proper cross-compilation
   docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -c "GOOS=linux GOARCH=mipsle go build -ldflags='-s -w' -o dist/printer-connector-mips ./cmd/connector"
   ```
5. Commit and push: `git commit -am "Add feature"`
6. Open a Pull Request

---

## 🔌 API Integration (for Backend Developers)

**Are you building or maintaining the Spoolr Rails backend (the `print_dock` repo)?**

The connector communicates with your API using a specific protocol and expects certain endpoints and response formats. We've created comprehensive documentation to help you integrate:

📖 **[API Integration Guide](docs/API_INTEGRATION.md)**

This document includes:
- Complete API endpoint specifications
- Request/response payload examples
- Authentication flow details
- Command types and parameters
- Error handling expectations
- Rails controller code examples
- Testing and debugging tips

**Quick Links:**
- [Pairing/Registration](docs/API_INTEGRATION.md#1-pairingregistration)
- [Command Types](docs/API_INTEGRATION.md#command-types)
- [File Upload Implementation](docs/API_INTEGRATION.md#5-upload_file)
- [Rails Controller Skeleton](docs/API_INTEGRATION.md#example-rails-controller-skeleton)

---

## ❓ FAQ

### General Questions

**Q: Is this free?**  
A: The connector is open-source (MIT) and free. The cloud service it pairs with — [Spoolr](https://www.spoolr.io) — may have its own pricing.

**Q: Do I need to open ports on my router?**  
A: No! The connector makes outbound connections only.

**Q: Can I use this without the cloud service?**  
A: No, the connector requires a compatible cloud API endpoint.

**Q: Does this work with OctoPrint?**  
A: Not currently. Klipper/Moonraker + Bambu Lab are supported today; PrusaLink and Elegoo / SDCP are planned. OctoPrint isn't on the near-term roadmap.

**Q: How much bandwidth does it use?**  
A: Minimal. Typically <1MB per hour (heartbeats + snapshots + occasional commands).

**Q: How do I know the agent is actually running?**  
A: On macOS the popover surfaces it directly — a green dot in the header
means healthy, yellow ("Agent not responding") means the agent hasn't written
its status file in > 90 s, red ("Agent stopped") means the subprocess died.
Both yellow and red come with a one-click **Restart Agent** button. On Linux /
Pi / K1, `systemctl status printer-connector` (or `/opt/printer-connector/service.sh status` on the K1) is the source of truth; on Windows, look for **Spoolr
Connect** under `services.msc`.

---

### Security Questions

**Q: Is my printer secure?**  
A: Yes. The connector uses:
- Encrypted HTTPS connections
- Token-based authentication
- No open ports required
- No direct internet exposure

**Q: What if someone steals my pairing token?**  
A: Pairing tokens are single-use and expire quickly (typically 24 hours). After pairing, the token is deleted.

**Q: Where are my credentials stored?**  
A: In the config file with 600 permissions (readable only by owner/root).

**Q: Can the cloud service control my printer without permission?**  
A: Commands are only executed if you explicitly send them from your authenticated cloud account.

---

### Technical Questions

**Q: Why is it written in Go?**  
A: Go provides:
- Small binary size
- Low resource usage
- Easy cross-compilation
- No runtime dependencies
- Great stdlib for HTTP/JSON

**Q: Can I run multiple connectors on one Pi?**  
A: Yes! Each connector manages one printer, so if you have multiple printers connected to the same Pi, install separate connector instances with different config files and service names.

**Q: Does it support webcams?**  
A: Yes. The agent pushes opportunistic snapshots and an optional MJPEG-style stream while a viewer is open in the Spoolr web app.

**Q: What happens if my internet goes down?**  
A: The connector will keep retrying with exponential backoff. Your printer continues working normally; you just can't access it remotely until internet is restored.

---

## 📝 Technical Details

### API Endpoints Used

The connector communicates with these cloud endpoints:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/connectors/register` | POST | One-time pairing |
| `/api/v1/connectors/:id/heartbeat` | POST | Keep-alive signals |
| `/api/v1/connectors/:id/commands` | GET | Poll for commands |
| `/api/v1/commands/:id/complete` | POST | Report command results |
| `/api/v1/snapshots/batch` | POST | Push printer status |

### Moonraker Endpoints Used

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/printer/objects/query` | POST | Get printer status |
| `/printer/print/pause` | POST | Pause current print |
| `/printer/print/resume` | POST | Resume paused print |
| `/printer/print/cancel` | POST | Cancel current print |
| `/printer/print/start` | POST | Start a print job |

---

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

---

## 🙏 Acknowledgments

- Klipper team for the amazing 3D printer firmware
- Moonraker team for the excellent API
- The Voron community for inspiration and testing

---

## 📧 Support

- **Issues:** [GitHub Issues](https://github.com/kurenn/printer-connector/issues)
- **Discussions:** [GitHub Discussions](https://github.com/kurenn/printer-connector/discussions)

---

**Made with ❤️ for the 3D printing community**
