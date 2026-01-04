#!/bin/sh
# Universal update script for printer-connector
# Works for both vanilla Klipper (Raspberry Pi) and K1 Max installations
# 
# K1 Max Usage:
#   wget -O - https://raw.githubusercontent.com/kurenn/printer-connector/main/update.sh | sh
# Or:
#   wget https://raw.githubusercontent.com/kurenn/printer-connector/main/update.sh
#   sh update.sh
#
# Raspberry Pi Usage:
#   curl -fsSL https://raw.githubusercontent.com/kurenn/printer-connector/main/update.sh | bash
# Or:
#   wget https://raw.githubusercontent.com/kurenn/printer-connector/main/update.sh
#   bash update.sh

GITHUB_REPO="kurenn/printer-connector"

# Colors for output (disabled on BusyBox for safety)
if [ -n "$BASH_VERSION" ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

info() {
    printf "${BLUE}[INFO]${NC} %s\n" "$1"
}

success() {
    printf "${GREEN}[OK]${NC} %s\n" "$1"
}

warn() {
    printf "${YELLOW}[WARN]${NC} %s\n" "$1"
}

error() {
    printf "${RED}[ERROR]${NC} %s\n" "$1"
    exit 1
}

# Detect download tool (wget for K1 Max, curl for Raspberry Pi)
if command -v wget >/dev/null 2>&1; then
    DOWNLOADER="wget"
elif command -v curl >/dev/null 2>&1; then
    DOWNLOADER="curl"
else
    error "Neither wget nor curl found"
fi

# Welcome banner
echo ""
info "==========================================="
info "  Printer Connector - Update Script"
info "==========================================="
echo ""

# Auto-detect installation directory
if [ -d "/opt/printer-connector" ]; then
    INSTALL_DIR="/opt/printer-connector"
    SERVICE_MANAGER="init.d"
    info "Detected K1 installation at /opt/printer-connector"
elif [ -d "$HOME/printer-connector" ]; then
    INSTALL_DIR="$HOME/printer-connector"
    SERVICE_MANAGER="systemd"
    info "Detected Klipper installation at $HOME/printer-connector"
else
    error "Printer connector installation not found in /opt/printer-connector or $HOME/printer-connector"
fi

BIN_FILE="$INSTALL_DIR/printer-connector"
CONFIG_FILE="$INSTALL_DIR/config.json"

# Verify connector is installed
if [ ! -f "$BIN_FILE" ]; then
    error "Connector binary not found at $BIN_FILE"
fi

if [ ! -f "$CONFIG_FILE" ]; then
    error "Config file not found at $CONFIG_FILE"
fi

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    aarch64|arm64)
        BINARY_NAME="printer-connector-linux-arm64"
        ;;
    x86_64|amd64)
        BINARY_NAME="printer-connector-linux-amd64"
        ;;
    mips)
        BINARY_NAME="printer-connector-mips"
        ;;
    *)
        error "Unsupported architecture: $ARCH"
        ;;
esac

info "Detected architecture: $ARCH (using $BINARY_NAME)"

# Get current version
CURRENT_VERSION="unknown"
if $BIN_FILE --version 2>/dev/null | grep -q "version"; then
    CURRENT_VERSION=$($BIN_FILE --version 2>/dev/null | awk '/version/ {print $3}')
fi
info "Current version: $CURRENT_VERSION"

# Get latest version from GitHub (skip on K1 Max due to SSL issues)
LATEST_VERSION=""
if [ "$DOWNLOADER" = "curl" ]; then
    info "Checking for latest version..."
    LATEST_VERSION=$(curl -s "https://api.github.com/repos/$GITHUB_REPO/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
fi

if [ -z "$LATEST_VERSION" ]; then
    warn "Using latest release (version check skipped)"
    DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/latest/download/$BINARY_NAME"
else
    info "Latest version: $LATEST_VERSION"
    if [ "$CURRENT_VERSION" = "$LATEST_VERSION" ]; then
        success "You are already running the latest version!"
        echo ""
        printf "Do you want to re-install anyway? [y/N] "
        read -r REPLY
        case "$REPLY" in
            [Yy]*)
                ;;
            *)
                info "Update cancelled"
                exit 0
                ;;
        esac
    fi
    DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/download/$LATEST_VERSION/$BINARY_NAME"
fi

# Stop the service
info "Stopping printer-connector service..."
if [ "$SERVICE_MANAGER" = "systemd" ]; then
    if systemctl is-active --quiet printer-connector 2>/dev/null; then
        sudo systemctl stop printer-connector
        success "Service stopped"
    else
        warn "Service was not running"
    fi
elif [ "$SERVICE_MANAGER" = "init.d" ]; then
    if [ -f /etc/init.d/S99printer-connector ]; then
        /etc/init.d/S99printer-connector stop 2>/dev/null || true
        success "Service stopped"
    else
        warn "Service was not running"
    fi
fi

# Backup current binary
info "Backing up current binary..."
BACKUP_FILE="$BIN_FILE.backup-$(date +%Y%m%d-%H%M%S)"
cp "$BIN_FILE" "$BACKUP_FILE"
success "Backup created: $BACKUP_FILE"

# Download new binary
info "Downloading latest version..."
TEMP_BIN="/tmp/printer-connector-update-$$"

if [ "$DOWNLOADER" = "wget" ]; then
    # BusyBox wget (K1 Max) - no SSL support, use http
    HTTP_URL=$(echo "$DOWNLOAD_URL" | sed 's|https://github.com|http://github.com|')
    info "Using HTTP download (K1 Max has limited SSL support)"
    if ! wget -O "$TEMP_BIN" "$HTTP_URL" 2>/dev/null; then
        # Try redirector service if GitHub http fails
        warn "Direct download failed, trying mirror..."
        HTTP_URL="http://github.com/$GITHUB_REPO/releases/latest/download/$BINARY_NAME"
        if ! wget -O "$TEMP_BIN" "$HTTP_URL" 2>/dev/null; then
            error "Failed to download from $HTTP_URL"
        fi
    fi
elif [ "$DOWNLOADER" = "curl" ]; then
    # Full curl with SSL (Raspberry Pi)
    if ! curl -L -f -o "$TEMP_BIN" "$DOWNLOAD_URL"; then
        error "Failed to download from $DOWNLOAD_URL"
    fi
fi

chmod +x "$TEMP_BIN"
success "Downloaded successfully"

# Verify the binary works
info "Verifying new binary..."
if ! "$TEMP_BIN" --version >/dev/null 2>&1; then
    rm -f "$TEMP_BIN"
    error "Downloaded binary verification failed. Update aborted."
fi

# Install new binary
info "Installing new binary..."
mv "$TEMP_BIN" "$BIN_FILE"
success "Binary updated"

# Get new version
NEW_VERSION=$($BIN_FILE --version 2>/dev/null | awk '/version/ {print $3}')
info "New version: $NEW_VERSION"

# Start the service
info "Starting printer-connector service..."
if [ "$SERVICE_MANAGER" = "systemd" ]; then
    sudo systemctl start printer-connector
    sleep 2
    if systemctl is-active --quiet printer-connector; then
        success "Service started successfully"
    else
        error "Service failed to start. Check logs with: sudo journalctl -u printer-connector -f"
    fi
elif [ "$SERVICE_MANAGER" = "init.d" ]; then
    /etc/init.d/S99printer-connector start
    sleep 2
    success "Service started"
fi

# Final status check
echo ""
success "==========================================="
success "  Update completed successfully!"
success "==========================================="
echo ""
info "Summary:"
echo "  Old version: $CURRENT_VERSION"
echo "  New version: $NEW_VERSION"
echo "  Backup: $BACKUP_FILE"
echo ""
info "To check status:"
if [ "$SERVICE_MANAGER" = "systemd" ]; then
    echo "  sudo systemctl status printer-connector"
    echo "  sudo journalctl -u printer-connector -f"
else
    echo "  /etc/init.d/S99printer-connector status"
fi
echo ""
info "To rollback (if needed):"
echo "  1. Stop service"
echo "  2. mv $BACKUP_FILE $BIN_FILE"
echo "  3. Start service"
echo ""
