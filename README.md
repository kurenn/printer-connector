# 🖨️ Printer Connector

> A secure bridge that connects your 3D printer to the cloud, enabling remote monitoring and control from anywhere.

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.23+-00ADD8.svg)](https://golang.org)
[![Platform](https://img.shields.io/badge/platform-linux%20%7C%20arm64%20%7C%20amd64-lightgrey.svg)](https://github.com/kurenn/printer-connector)

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

## 🤔 What is Printer Connector?

**Printer Connector** is a lightweight software agent that runs on your 3D printer (typically a Raspberry Pi running Klipper). It acts as a secure bridge between your local printer and a cloud application, allowing you to:

- 📊 **Monitor** your printer's status in real-time from anywhere
- 🎮 **Control** your prints remotely (pause, resume, cancel, start)
- 📸 **Receive** automatic snapshots of your printer's state
- 🔔 **Get notified** about print progress and issues

Think of it as a secure tunnel that lets your printer talk to the cloud without exposing it directly to the internet.

---

## 💡 Why Use Printer Connector?

### The Problem
Most 3D printers on local networks can't be accessed from outside your home/office without:
- Opening ports on your router (security risk)
- Setting up VPNs (complex for beginners)
- Using third-party services (privacy concerns)

### The Solution
Printer Connector solves this by:
- ✅ **Running locally** on your printer's Raspberry Pi
- ✅ **Creating outbound connections** (no open ports needed)
- ✅ **Using secure authentication** (pairing tokens and secrets)
- ✅ **Working with Klipper** (via Moonraker API)
- ✅ **Being lightweight** (minimal resource usage, written in Go)
- ✅ **Zero external dependencies** (stdlib only)

---

## 🔧 How It Works

```
┌─────────────────┐         ┌──────────────────┐         ┌─────────────┐
│   Your Phone    │         │  Cloud Service   │         │  Your 3D    │
│   or Computer   │◄────────┤  (PrintDock)     │◄────────┤  Printer    │
│                 │         │                  │         │  + Pi       │
│  Web Interface  │         │   REST API       │         │  Connector  │
└─────────────────┘         └──────────────────┘         └─────────────┘
```

### Step-by-Step Process:

1. **Pairing** (one time):
   - You get a pairing token from your cloud service
   - Connector uses this token to register and get permanent credentials
   - Token is automatically removed after successful pairing

2. **Heartbeat** (every 10-15 seconds):
   - Connector sends "I'm alive" signals to the cloud
   - Cloud knows when your printer is online/offline

3. **Snapshots** (every 30 seconds):
   - Connector reads printer status from Moonraker
   - Sends data (temperatures, print progress, etc.) to cloud
   - You see real-time updates in your app

4. **Commands** (every 3-5 seconds):
   - Connector asks cloud: "Any commands for me?"
   - Cloud responds with actions (pause, resume, cancel, start print)
   - Connector executes commands via Moonraker
   - Results are sent back to cloud

---

## 🖨️ Supported Printers

Printer Connector works with any 3D printer running **Klipper** firmware with **Moonraker** API access:

### ✅ Tested & Supported:
- Voron 2.4, Trident, 0.1, Switchwire
- Creality K1 / K1 Max
- Prusa MK3/MK4 (with Klipper)
- Ender 3 / Ender 5 (with Klipper upgrade)
- Any custom Klipper build

### 📋 Requirements:
- Klipper firmware installed
- Moonraker API accessible (usually port 7125)
- Raspberry Pi or similar Linux device (arm64 or amd64)
- Network connectivity

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

3. **A pairing token from your cloud service**
   - Get this from your PrintDock account settings
   - It looks like: `PAIR_abc123xyz456`

---

### Installation

Choose the installation method for your printer type:

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
1. **Cloud URL**: Your PrintDock server address (e.g., `https://printdock.example.com`)
2. **Pairing Token**: The token you got from PrintDock (one token per printer)
3. **Printer Details**: Name and Moonraker URL for your printer

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

## � Updating

To update Printer Connector to the latest version, use the update script:

### Quick Update (Recommended)

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

The configuration file is automatically created during installation at:
- **Klipper**: `/usr/data/printer-connector/config.json`
- **K1 Max**: `/opt/printer-connector/config.json`

### Example Configuration

```json
{
  "cloud_url": "https://printdock.example.com",
  "pairing_token": "PAIR_abc123xyz",
  "site_name": "Workshop",
  "poll_commands_seconds": 3,
  "push_snapshots_seconds": 30,
  "heartbeat_seconds": 10,
  "state_dir": "/var/lib/printer-connector",
  "moonraker": [
    {
      "printer_id": 0,
      "name": "Voron 2.4",
      "base_url": "http://127.0.0.1:7125",
      "ui_port": 80
    }
  ]
}
```

**Note:** Each connector instance manages ONE printer. The `printer_id` is automatically assigned by the backend during pairing. If you have multiple printers, install a separate connector for each one with its own pairing token.

### Configuration Fields Explained

| Field | Description | Example |
|-------|-------------|---------|
| `cloud_url` | Your cloud service URL | `https://printdock.example.com` |
| `pairing_token` | One-time token (removed after pairing) | `PAIR_abc123` |
| `connector_id` | Auto-added after pairing | `conn_xyz789` |
| `connector_secret` | Auto-added after pairing (keep secure!) | `secret_key_here` |
| `site_name` | Optional name for this location | `"Home Workshop"` |
| `poll_commands_seconds` | How often to check for commands | `3` (default) |
| `push_snapshots_seconds` | How often to send status updates | `30` (default) |
| `heartbeat_seconds` | How often to send "I'm alive" signal | `10` (default) |
| `state_dir` | Directory for persistent state | `/var/lib/printer-connector` |
| `moonraker.printer_id` | Auto-assigned by backend during pairing | `0` |
| `moonraker.name` | Display name for this printer | `"Voron 2.4"` |
| `moonraker.base_url` | Moonraker API endpoint | `http://127.0.0.1:7125` |
| `moonraker.ui_port` | Optional web UI port | `80` or `4409` |

### Security Notes

⚠️ **Important:**
- Configuration file is automatically set to `600` permissions (owner read/write only)
- `pairing_token` is automatically removed after successful pairing
- `connector_secret` must be kept secure - it's your permanent credential
- Never commit config files with secrets to git

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

### Command-Line Options

```bash
printer-connector [OPTIONS]

Options:
  --config PATH         Path to config file (required)
  --log-level LEVEL     Logging level: debug|info|warn|error (default: info)
  --once               Run once and exit (useful for testing pairing)
  --help               Show help message
```

**Examples:**

```bash
# Standard run
./printer-connector --config config.json

# Debug mode
./printer-connector --config config.json --log-level debug

# Test pairing without running service
./printer-connector --config config.json --once
```

---

### Running Multiple Printers

**Important:** Each connector instance is paired with ONE printer. The backend assigns one printer per pairing token.

If you have multiple printers, you need to:
1. Get a separate pairing token for each printer from PrintDock
2. Install a separate connector instance for each printer
3. Use different configuration files and service names

**Example for 2 printers:**

```bash
# Printer 1 - Voron
sudo ./install-klipper.sh --config /usr/data/printer-connector-voron/config.json \
  --pairing-token PAIR_voron_token

# Printer 2 - Ender 3  
sudo ./install-klipper.sh --config /usr/data/printer-connector-ender/config.json \
  --pairing-token PAIR_ender_token
```

**Note:** Each connector will have its own systemd service and run independently.

---

### Non-Interactive Installation

For automated deployments or scripts:

```bash
sudo ./install-klipper.sh \
  --bin ./printer-connector \
  --cloud-url https://printdock.example.com \
  --pairing-token PAIR_abc123 \
  --printer "0|Voron 2.4|http://127.0.0.1:7125" \
  --log-level info
```

**Note:** Each installation handles one printer. For multiple printers, run the installer separately with different pairing tokens.

---

## 💻 Development

### Prerequisites for Development

- Go 1.23 or higher
- Git
- (Optional) Docker for testing

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
  - Development: `export CLOUD_URL=http://localhost:3000` or `http://192.168.68.50:3000`
  - Takes precedence over config file `cloud_url` field

Example for local Rails development:
```bash
export CLOUD_URL=http://localhost:3000
./printer-connector --config config/config.dev.json --log-level debug
```

For systemd services, add to the service file:
```ini
[Service]
Environment="CLOUD_URL=http://192.168.68.50:3000"
```

### Project Structure

```
printer-connector/
├── cmd/
│   └── connector/          # Main entry point
│       └── main.go
├── internal/
│   ├── agent/              # Core agent logic
│   │   ├── agent.go        # Main agent orchestration
│   │   ├── commands.go     # Command polling and execution
│   │   ├── heartbeat.go    # Heartbeat loop
│   │   └── snapshots.go    # Snapshot collection and push
│   ├── cloud/              # Cloud API client
│   │   ├── client.go       # HTTP client implementation
│   │   ├── types.go        # Request/response types
│   │   └── string_or_number.go
│   ├── config/             # Configuration management
│   │   └── config.go       # Config loading and validation
│   ├── moonraker/          # Moonraker API client
│   │   └── client.go       # Moonraker API implementation
│   └── util/               # Utilities
│       └── backoff.go      # Exponential backoff
├── config/
│   └── config.dev.json     # Development config template
├── install-klipper.sh      # Vanilla Klipper installer
├── install-k1.sh           # K1 Max installer
├── uninstall.sh            # Uninstaller
├── go.mod                  # Go module definition
└── README.md               # This file
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

**Are you building or maintaining the Rails/PrintDock backend?**

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
A: The connector is open-source and free. Your cloud service (PrintDock) may have its own pricing.

**Q: Do I need to open ports on my router?**  
A: No! The connector makes outbound connections only.

**Q: Can I use this without the cloud service?**  
A: No, the connector requires a compatible cloud API endpoint.

**Q: Does this work with OctoPrint?**  
A: No, it's designed specifically for Klipper/Moonraker. OctoPrint support may come in the future.

**Q: How much bandwidth does it use?**  
A: Minimal. Typically <1MB per hour (heartbeats + snapshots + occasional commands).

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
A: Not yet. Currently only status and control. Webcam streaming may be added in a future version.

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
