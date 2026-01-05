#!/bin/bash
set -e

# Interactive installer for vanilla Klipper machines (Raspberry Pi, etc.)
# Usage:
#   wget -O - https://raw.githubusercontent.com/kurenn/printer-connector/main/install-klipper.sh | bash
# Or:
#   wget https://raw.githubusercontent.com/kurenn/printer-connector/main/install-klipper.sh
#   bash install-klipper.sh

USER_HOME="$HOME"
INSTALL_DIR="$USER_HOME/printer-connector"
CONFIG_FILE="$INSTALL_DIR/config.json"
BIN_FILE="$INSTALL_DIR/printer-connector"
STATE_DIR="$INSTALL_DIR/state"
GITHUB_REPO="kurenn/printer-connector"
CLOUD_URL="http://192.168.68.50:3000"
MOONRAKER_URL="http://127.0.0.1:7125"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

success() {
    echo -e "${GREEN}✓${NC} $1"
}

warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

error() {
    echo -e "${RED}✗${NC} $1"
    exit 1
}

prompt() {
    echo -e "${YELLOW}?${NC} $1"
}

# Welcome banner
echo ""
info "═══════════════════════════════════════════════"
info "  Printer Connector Installer (Vanilla Klipper)"
info "═══════════════════════════════════════════════"
echo ""

# Check architecture
ARCH=$(uname -m)
info "Detected architecture: $ARCH"

case "$ARCH" in
    aarch64|arm64)
        BINARY_NAME="printer-connector"
        ;;
    x86_64|amd64)
        warn "x86_64 detected - using ARM64 binary (may not work on x86)"
        BINARY_NAME="printer-connector"
        ;;
    *)
        error "Unsupported architecture: $ARCH"
        ;;
esac

# Check if systemd is available
if ! command -v systemctl &> /dev/null; then
    error "systemd not found. This installer requires systemd."
fi

# Check if running as root
if [ "$(id -u)" -eq 0 ]; then
    error "Do not run this script as root. Run as your regular user (e.g., 'pi' or 'kurenn')"
fi

# Check sudo access
if ! sudo -n true 2>/dev/null; then
    warn "This script requires sudo access. You may be prompted for your password."
fi

# Prompt for pairing token
prompt "Enter your pairing token from the cloud app:"
read -r PAIRING_TOKEN

if [ -z "$PAIRING_TOKEN" ]; then
    error "Pairing token is required"
fi

# Prompt for printer name
prompt "Enter printer name (e.g., Voron, Ender 3):"
read -r PRINTER_NAME

if [ -z "$PRINTER_NAME" ]; then
    PRINTER_NAME="My Printer"
    info "Using default name: $PRINTER_NAME"
fi

# Prompt for UI port
prompt "Enter Fluidd/Mainsail web UI port (default: 80):"
read -r UI_PORT

if [ -z "$UI_PORT" ]; then
    UI_PORT=80
fi

info "Configuration:"
echo "  • Cloud URL: $CLOUD_URL"
echo "  • Pairing Token: [REDACTED]"
echo "  • Printer Name: $PRINTER_NAME"
echo "  • Moonraker URL: $MOONRAKER_URL"
echo "  • UI Port: $UI_PORT"
echo "  • Install Dir: $INSTALL_DIR"
echo ""

# Create directories
info "Creating directories..."
mkdir -p "$INSTALL_DIR"
mkdir -p "$STATE_DIR"
success "Directories created"

# Download binary
info "Downloading printer-connector binary..."
BINARY_URL="https://raw.githubusercontent.com/$GITHUB_REPO/main/$BINARY_NAME"

if command -v wget &> /dev/null; then
    wget --no-check-certificate -q -O "$BIN_FILE" "$BINARY_URL" || error "Failed to download binary"
elif command -v curl &> /dev/null; then
    curl -sL -o "$BIN_FILE" "$BINARY_URL" || error "Failed to download binary"
else
    error "Neither wget nor curl found. Please install one of them."
fi

chmod +x "$BIN_FILE"
success "Binary downloaded and made executable"

# Verify binary
info "Verifying binary..."
if ! file "$BIN_FILE" | grep -q "ELF.*executable"; then
    error "Downloaded file is not a valid executable"
fi
success "Binary verified"

# Create config JSON
info "Creating configuration file..."
cat > "$CONFIG_FILE" <<EOF
{
  "cloud_url": "$CLOUD_URL",
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
      "ui_port": $UI_PORT
    }
  ]
}
EOF

chmod 600 "$CONFIG_FILE"
success "Configuration file created"

# Test Moonraker connection
info "Testing Moonraker connection..."
if command -v curl &> /dev/null; then
    if ! curl -sf "$MOONRAKER_URL/server/info" > /dev/null; then
        warn "Could not connect to Moonraker at $MOONRAKER_URL"
        warn "Make sure Moonraker is running and accessible"
    else
        success "Moonraker is reachable"
    fi
fi

# Run pairing
info "Pairing with cloud (this may take a few seconds)..."
echo ""
info "Binary location: $BIN_FILE"
info "Config location: $CONFIG_FILE"
info "Testing binary execution..."

# Test if binary is executable
if [ ! -x "$BIN_FILE" ]; then
    error "Binary is not executable: $BIN_FILE"
fi

# Show what we're about to run
echo ""
echo "==============================================="
echo "Running pairing command:"
echo "$BIN_FILE --config $CONFIG_FILE --log-level info --once"
echo "==============================================="
echo ""

# Run pairing with visible output (don't capture)
set +e
"$BIN_FILE" --config "$CONFIG_FILE" --log-level info --once 2>&1
PAIRING_EXIT=$?
set -e

echo ""
echo "==============================================="
echo "Pairing command completed with exit code: $PAIRING_EXIT"
echo "==============================================="
echo ""

if [ $PAIRING_EXIT -ne 0 ]; then
    error "Pairing failed with exit code $PAIRING_EXIT"
fi

# Wait a moment for config to be written
sleep 1

# Show config file contents
info "Checking config file for connector_id..."
echo ""
cat "$CONFIG_FILE"
echo ""

# Check if pairing succeeded (config should now have connector_id)
if grep -q '"connector_id"' "$CONFIG_FILE"; then
    success "Pairing successful!"
else
    error "Pairing failed - connector_id not found in config"
fi

# Create systemd service
info "Creating systemd service..."
SERVICE_FILE="/etc/systemd/system/printer-connector.service"

sudo tee "$SERVICE_FILE" > /dev/null <<EOF
[Unit]
Description=Printer Connector Agent
After=network-online.target moonraker.service
Wants=network-online.target

[Service]
Type=simple
User=$USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$BIN_FILE --config $CONFIG_FILE --log-level info
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

success "Systemd service created"

# Enable and start service
info "Enabling and starting service..."
sudo systemctl daemon-reload
sudo systemctl enable printer-connector.service
sudo systemctl start printer-connector.service
success "Service enabled and started"

# Check service status
sleep 2
if sudo systemctl is-active --quiet printer-connector.service; then
    success "Service is running!"
else
    warn "Service may not be running. Check with: sudo systemctl status printer-connector"
fi

# Final message
echo ""
success "═══════════════════════════════════════════════"
success "  Installation Complete!"
success "═══════════════════════════════════════════════"
echo ""
info "Service management commands:"
echo "  • Status:  sudo systemctl status printer-connector"
echo "  • Logs:    journalctl -u printer-connector -f"
echo "  • Restart: sudo systemctl restart printer-connector"
echo "  • Stop:    sudo systemctl stop printer-connector"
echo ""
info "Config location: $CONFIG_FILE"
info "Binary location: $BIN_FILE"
echo ""
info "Check logs to verify connection:"
echo "  journalctl -u printer-connector -f"
echo ""
