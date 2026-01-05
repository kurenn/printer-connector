# K1 Max Installation Guide

Quick installation guide for Creality K1 Max printers.

## Interactive Installation (Recommended)

### Method 1: Direct from GitHub (requires internet on K1)

```bash
# SSH into your K1 Max
ssh root@<K1_IP>

# Download and run installer
wget -O - https://raw.githubusercontent.com/kurenn/printer-connector/main/install-k1.sh | sh
```

### Method 2: Copy and paste script

1. **On your computer**, download the script:
   ```bash
   curl -O https://raw.githubusercontent.com/kurenn/printer-connector/main/install-k1.sh
   ```

2. **Copy to K1 Max**:
   ```bash
   scp install-k1.sh root@<K1_IP>:/tmp/
   ```

3. **SSH into K1 Max and run**:
   ```bash
   ssh root@<K1_IP>
   cd /tmp
   chmod +x install-k1.sh
   ./install-k1.sh
   ```

## What the installer does:

1. ✅ Checks architecture (MIPS)
2. ✅ Asks for pairing token (from Rails app)
3. ✅ Asks for printer name
4. ✅ Downloads `printer-connector-mips` binary from GitHub
5. ✅ Creates `/opt/printer-connector/` directory
6. ✅ Generates `config.json` with correct settings
7. ✅ Tests Moonraker connection
8. ✅ Performs pairing with cloud (exchanges token for credentials)
9. ✅ Auto-updates `printer_id` in config

## Installation Prompts

```
? Enter your pairing token: <paste token from Rails>
? Enter printer name (e.g., K1 Max): K1 Max
? Enter site name (optional, press Enter to skip): My Workshop
```

## After Installation

### Start the connector:
```bash
/opt/printer-connector/printer-connector \
  --config /opt/printer-connector/config.json \
  --log-level info
```

### Run in background:
```bash
nohup /opt/printer-connector/printer-connector \
  --config /opt/printer-connector/config.json \
  --log-level info \
  > /opt/printer-connector/connector.log 2>&1 &
```

### View logs:
```bash
tail -f /opt/printer-connector/connector.log
```

### Stop the connector:
```bash
pkill printer-connector
```

## Troubleshooting

### "Moonraker not reachable"
```bash
# Check if Moonraker is running
netstat -tulpn | grep 7125

# Test manually
curl http://127.0.0.1:7125/server/info
```

### "Pairing failed"
- Verify pairing token is valid (check Rails app)
- Check K1 Max has internet access
- Verify cloud URL is reachable from K1

### "Binary not found after download"
Try manual download:
```bash
wget -O /opt/printer-connector/printer-connector \
  https://github.com/kurenn/printer-connector/raw/main/printer-connector-mips
chmod +x /opt/printer-connector/printer-connector
```

## File Locations

- Binary: `/opt/printer-connector/printer-connector`
- Config: `/opt/printer-connector/config.json`
- State: `/opt/printer-connector/state/`
- Logs: `/opt/printer-connector/connector.log` (if running in background)

## Default Settings

- **Cloud URL**: `http://192.168.68.50:3000`
- **Moonraker**: `http://127.0.0.1:7125`
- **Poll commands**: Every 5 seconds
- **Push snapshots**: Every 30 seconds
- **Heartbeat**: Every 15 seconds

## K1 Max Specifics

- Architecture: **MIPS** (not ARM)
- SSH default: `root@<printer_ip>` (password may be `creality3d` or none)
- Moonraker runs on port 7125 by default
- `/opt` partition is used for third-party software

## Manual Configuration

If you need to edit the config manually:

```bash
vi /opt/printer-connector/config.json
```

Example config:
```json
{
  "cloud_url": "https://your-cloud.com",
  "connector_id": "75",
  "connector_secret": "c_abc...",
  "poll_commands_seconds": 5,
  "push_snapshots_seconds": 30,
  "heartbeat_seconds": 15,
  "state_dir": "/opt/printer-connector/state",
  "moonraker": [
    {
      "printer_id": 262,
      "name": "K1 Max",
      "base_url": "http://127.0.0.1:7125"
    }
  ]
}
```

## Re-pairing

If you need to pair again with a new token:

1. Get new pairing token from Rails
2. Edit config:
   ```bash
   vi /config/printer-connector/config.json
   ```
3. Remove `connector_id` and `connector_secret`
4. Add `"pairing_token": "NEW_TOKEN"`
5. Set `"printer_id": 0` in moonraker entry
6. Run connector once:
   ```bash
   /opt/printer-connector/printer-connector \
     --config /opt/printer-connector/config.json \
     --once
   ```
