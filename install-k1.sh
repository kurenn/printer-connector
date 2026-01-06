#!/bin/sh
set -e

# Interactive installer for K1 Max printer
# Usage 1: Transfer binary first, then run installer
#   scp printer-connector-mips root@<K1_IP>:/tmp/printer-connector
#   scp install-k1.sh root@<K1_IP>:/tmp/
#   ssh root@<K1_IP> "cd /tmp && sh install-k1.sh"
#
# Usage 2: Run directly (will attempt GitHub download)
#   ssh root@<K1_IP> "sh -c 'cd /tmp && sh install-k1.sh'"

INSTALL_DIR="/opt/printer-connector"
CONFIG_FILE="$INSTALL_DIR/config.json"
BIN_FILE="$INSTALL_DIR/printer-connector"
STATE_DIR="$INSTALL_DIR/state"
GITHUB_REPO="kurenn/printer-connector"
MOONRAKER_URL="http://127.0.0.1:7125"

# Cloud URL: defaults to production, can be overridden with CLOUD_URL env var
CLOUD_URL="${CLOUD_URL:-https://www.spoolr.io}"

# Check if binary was manually transferred to /tmp
MANUAL_BIN="/tmp/printer-connector"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info() {
    printf "${BLUE}==>${NC} %s\n" "$1"
}

success() {
    printf "${GREEN}✓${NC} %s\n" "$1"
}

warn() {
    printf "${YELLOW}!${NC} %s\n" "$1"
}

error() {
    printf "${RED}✗${NC} %s\n" "$1"
    exit 1
}

prompt() {
    printf "${BLUE}?${NC} %s: " "$1"
}

# Check if running as root
if [ "$(id -u)" -ne 0 ]; then
    error "This script must be run as root. Use: sudo sh install-k1.sh"
fi

# Welcome banner
echo ""
info "═══════════════════════════════════════"
info "  Printer Connector Installer (K1 Max)"
info "═══════════════════════════════════════"
echo ""

# Check architecture
ARCH=$(uname -m)
if [ "$ARCH" != "mips" ]; then
    warn "Detected architecture: $ARCH (expected: mips)"
    warn "This installer is designed for K1 Max printers"
    prompt "Continue anyway? (y/N)"
    read -r continue_install
    if [ "$continue_install" != "y" ] && [ "$continue_install" != "Y" ]; then
        error "Installation cancelled"
    fi
fi

# Step 1: Gather information
echo ""
info "Step 1: Configuration"
echo ""

prompt "Enter your pairing token"
read -r PAIRING_TOKEN

if [ -z "$PAIRING_TOKEN" ]; then
    error "Pairing token is required"
fi

prompt "Enter printer name (e.g., K1 Max)"
read -r PRINTER_NAME

if [ -z "$PRINTER_NAME" ]; then
    PRINTER_NAME="K1 Max"
    info "Using default name: $PRINTER_NAME"
fi

# Step 2: Create directories
echo ""
info "Step 2: Creating directories"
mkdir -p "$INSTALL_DIR"
mkdir -p "$STATE_DIR"
success "Directories created at $INSTALL_DIR"

# Step 3: Download binary
echo ""
info "Step 3: Installing printer-connector binary"

# Check if binary was manually transferred
if [ -f "$MANUAL_BIN" ]; then
    info "Found manually transferred binary at $MANUAL_BIN"
    cp "$MANUAL_BIN" "$BIN_FILE"
    chmod +x "$BIN_FILE"
    success "Binary installed from manual transfer"
else
    # Download from GitHub
    info "Downloading binary from GitHub..."
    
    DOWNLOAD_URL="https://raw.githubusercontent.com/$GITHUB_REPO/main/printer-connector-mips"
    
    if wget --no-check-certificate -O "$BIN_FILE" "$DOWNLOAD_URL" 2>/dev/null; then
        # Check if download was successful (file size > 1MB)
        if [ -f "$BIN_FILE" ] && [ "$(wc -c < "$BIN_FILE")" -gt 1000000 ]; then
            chmod +x "$BIN_FILE"
            FILE_SIZE=$(ls -lh "$BIN_FILE" | awk '{print $5}')
            success "Binary downloaded successfully ($FILE_SIZE)"
        else
            rm -f "$BIN_FILE"
            error "Download failed: file too small or corrupted"
        fi
    else
        error "Failed to download binary from GitHub.

Please manually transfer it:
  scp printer-connector-mips root@<K1_IP>:/tmp/printer-connector
  ssh root@<K1_IP> 'cd /tmp && sh install-k1.sh'
"
    fi
fi

# Step 4: Generate config
echo ""
info "Step 4: Generating configuration"

# Create config JSON (cloud_url will use default or CLOUD_URL env var)
cat > "$CONFIG_FILE" <<EOF
{
  "pairing_token": "$PAIRING_TOKEN",
  "poll_commands_seconds": 5,
  "push_snapshots_seconds": 30,
  "heartbeat_seconds": 15,
  "state_dir": "$STATE_DIR",
  "moonraker": [
    {
      "printer_id": 0,
      "name": "$PRINTER_NAME",
      "base_url": "$MOONRAKER_URL",
      "ui_port": 4409
    }
  ]
}
EOF

chmod 600 "$CONFIG_FILE"
success "Configuration file created at $CONFIG_FILE"

# Step 5: Test Moonraker connection
echo ""
info "Step 5: Testing Moonraker connection"
if wget --no-check-certificate -qO- --timeout=3 "$MOONRAKER_URL/server/info" >/dev/null 2>&1 || \
   curl -fsSLk --max-time 3 "$MOONRAKER_URL/server/info" >/dev/null 2>&1; then
    success "Moonraker is reachable at $MOONRAKER_URL"
else
    warn "Could not connect to Moonraker at $MOONRAKER_URL"
    warn "Make sure Moonraker is running before starting the connector"
fi

# Step 6: Perform pairing
echo ""
info "Step 6: Pairing with cloud"
info "Running connector once to complete pairing..."

if "$BIN_FILE" --config "$CONFIG_FILE" --log-level info --once 2>&1 | tee /tmp/pairing.log; then
    success "Pairing completed successfully!"
    
    # Check if config was updated (pairing_token should be removed)
    if grep -q '"connector_id"' "$CONFIG_FILE"; then
        success "Connector registered and config updated"
        # Show connector ID
        CONNECTOR_ID=$(grep '"connector_id"' "$CONFIG_FILE" | sed -E 's/.*"([^"]+)".*/\1/')
        info "Connector ID: $CONNECTOR_ID"
    else
        warn "Pairing may have failed. Check the logs above."
    fi
else
    warn "Pairing failed. You can try again manually:"
    echo "  $BIN_FILE --config $CONFIG_FILE --log-level debug --once"
fi

# Step 7: Setup service
echo ""
info "Step 7: Setting up auto-start service"

# Create service script in /opt where we have space
cat > "$INSTALL_DIR/service.sh" <<'SERVICEEOF'
#!/bin/sh

BIN="/opt/printer-connector/printer-connector"
CONFIG="/opt/printer-connector/config.json"
PIDFILE="/var/run/printer-connector.pid"
LOGFILE="/opt/printer-connector/connector.log"

start() {
    if [ -f "$PIDFILE" ] && kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "printer-connector already running"
        return 0
    fi
    echo "Starting printer-connector..."
    nohup "$BIN" --config "$CONFIG" --log-level info >> "$LOGFILE" 2>&1 &
    echo $! > "$PIDFILE"
    sleep 2
    if kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "OK"
    else
        echo "FAIL"
        rm -f "$PIDFILE"
    fi
}

stop() {
    if [ ! -f "$PIDFILE" ]; then
        echo "printer-connector not running"
        return 0
    fi
    echo "Stopping printer-connector..."
    kill $(cat "$PIDFILE") 2>/dev/null
    rm -f "$PIDFILE"
    echo "OK"
}

case "$1" in
    start) start ;;
    stop) stop ;;
    restart) stop; sleep 2; start ;;
    *) echo "Usage: $0 {start|stop|restart}"; exit 1 ;;
esac
SERVICEEOF

chmod +x "$INSTALL_DIR/service.sh"
success "Service script created"

# Try to create symlink in /etc/init.d for auto-start
if ln -sf "$INSTALL_DIR/service.sh" /etc/init.d/S99printer-connector 2>/dev/null; then
    success "Auto-start enabled (symlink created)"
    
    # Start the service
    /etc/init.d/S99printer-connector start
    sleep 2
    
    if ps | grep -v grep | grep printer-connector > /dev/null; then
        success "Service started successfully!"
    else
        warn "Service may have failed to start. Check logs."
    fi
    
    echo ""
    info "Service commands:"
    echo "  /etc/init.d/S99printer-connector start    # Start"
    echo "  /etc/init.d/S99printer-connector stop     # Stop"
    echo "  /etc/init.d/S99printer-connector restart  # Restart"
    echo "  tail -f $INSTALL_DIR/connector.log        # View logs"
    echo ""
    success "Service will auto-start on boot!"
    
else
    warn "Could not create symlink in /etc/init.d (filesystem full/read-only)"
    echo ""
    info "Manual startup required after each reboot:"
    echo "  $INSTALL_DIR/service.sh start"
    echo ""
    info "Or create symlink manually if possible:"
    echo "  ln -sf $INSTALL_DIR/service.sh /etc/init.d/S99printer-connector"
    
    # Try to start anyway
    "$INSTALL_DIR/service.sh" start
fi

# Installation complete
echo ""
info "═══════════════════════════════════════"
success "Installation Complete!"
info "═══════════════════════════════════════"
echo ""
info "Installation directory: $INSTALL_DIR"
info "Configuration file: $CONFIG_FILE"
info "Binary: $BIN_FILE"
echo ""

if [ "$setup_service" = "y" ] || [ "$setup_service" = "Y" ]; then
    info "Service is running! View logs with:"
    echo "  journalctl -u printer-connector -f"
else
    info "To start the connector manually:"
    echo "  $BIN_FILE --config $CONFIG_FILE --log-level info"
    echo ""
    info "To run in debug mode:"
    echo "  $BIN_FILE --config $CONFIG_FILE --log-level debug"
    echo ""
    info "To run in background:"
    echo "  nohup $BIN_FILE --config $CONFIG_FILE --log-level info > $INSTALL_DIR/connector.log 2>&1 &"
    echo ""
    info "To setup service later:"
    echo "  Run this installer again or manually create systemd service"
fi
echo ""
