#!/bin/sh
set -eu

# printer-connector uninstaller
#
# Works for both vanilla Klipper (systemd-based) and Creality K1 Max installs.
#
# Usage:
#   sudo ./uninstall.sh
#   sudo ./uninstall.sh --yes      # Skip confirmation
#   sudo ./uninstall.sh --config-only   # Only remove config (keep binary)
#
# Note: Requires root permissions.

SKIP_CONFIRM="false"
CONFIG_ONLY="false"

# Default paths for vanilla Klipper/systemd installs
SYSTEMD_SERVICE_NAME="printer-connector"
SYSTEMD_UNIT_PATH="/etc/systemd/system/${SYSTEMD_SERVICE_NAME}.service"
VANILLA_BIN="/usr/data/printer-connector/printer-connector"
VANILLA_CONFIG="/usr/data/printer-connector/config.json"
VANILLA_STATE_DIR="/usr/data/printer-connector/state"
VANILLA_BASE_DIR="/usr/data/printer-connector"

# Default paths for Creality K1 Max installs
K1_BASE_DIR="/opt/printer-connector"
K1_BIN="$K1_BASE_DIR/printer-connector"
K1_CONFIG="$K1_BASE_DIR/config.json"
K1_STATE_DIR="$K1_BASE_DIR/state"
K1_SERVICE_SCRIPT="$K1_BASE_DIR/service.sh"
K1_INIT_SYMLINK="/etc/init.d/S99printer-connector"
K1_PIDFILE="/var/run/printer-connector.pid"
K1_LOGFILE="$K1_BASE_DIR/connector.log"

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
}

die() { 
    error "$*"
    exit 1
}

has_cmd() { 
    command -v "$1" >/dev/null 2>&1
}

need_root() {
    if [ "$(id -u)" -ne 0 ]; then
        die "Please run as root (use sudo)."
    fi
}

usage() {
    cat <<'USAGE'
printer-connector uninstaller

Usage:
  sudo ./uninstall.sh                # Interactive (asks for confirmation)
  sudo ./uninstall.sh --yes          # Skip confirmation
  sudo ./uninstall.sh --config-only  # Only remove config/credentials (keep binary)

This will remove:
  - Binary files
  - Configuration files (including credentials)
  - State directories
  - Systemd service (vanilla Klipper)
  - Init.d scripts (Creality K1)
  - Log files

USAGE
}

# Parse arguments
while [ $# -gt 0 ]; do
    case "$1" in
        --yes|-y)
            SKIP_CONFIRM="true"
            shift
            ;;
        --config-only)
            CONFIG_ONLY="true"
            shift
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        *)
            die "Unknown option: $1. Use --help for usage."
            ;;
    esac
done

need_root

# Detect installation type
INSTALL_TYPE="none"

if [ -f "$SYSTEMD_UNIT_PATH" ] || [ -d "$VANILLA_BASE_DIR" ]; then
    INSTALL_TYPE="vanilla"
elif [ -d "$K1_BASE_DIR" ] || [ -f "$K1_INIT_SYMLINK" ]; then
    INSTALL_TYPE="k1"
fi

if [ "$INSTALL_TYPE" = "none" ]; then
    warn "No printer-connector installation detected."
    warn "Checked for:"
    echo "  - $VANILLA_BASE_DIR (vanilla Klipper)"
    echo "  - $K1_BASE_DIR (Creality K1)"
    echo "  - $SYSTEMD_UNIT_PATH (systemd service)"
    echo ""
    exit 0
fi

# Display what will be removed
echo ""
info "═══════════════════════════════════════"
info "  Printer Connector Uninstaller"
info "═══════════════════════════════════════"
echo ""

info "Detected installation: $INSTALL_TYPE"
echo ""

if [ "$INSTALL_TYPE" = "vanilla" ]; then
    info "The following will be removed:"
    echo ""
    
    if [ "$CONFIG_ONLY" = "true" ]; then
        [ -f "$VANILLA_CONFIG" ] && echo "  - $VANILLA_CONFIG (config)"
        [ -d "$VANILLA_STATE_DIR" ] && echo "  - $VANILLA_STATE_DIR (state)"
    else
        [ -f "$SYSTEMD_UNIT_PATH" ] && echo "  - $SYSTEMD_UNIT_PATH (systemd service)"
        [ -d "$VANILLA_BASE_DIR" ] && echo "  - $VANILLA_BASE_DIR (all files)"
    fi
    
elif [ "$INSTALL_TYPE" = "k1" ]; then
    info "The following will be removed:"
    echo ""
    
    if [ "$CONFIG_ONLY" = "true" ]; then
        [ -f "$K1_CONFIG" ] && echo "  - $K1_CONFIG (config)"
        [ -d "$K1_STATE_DIR" ] && echo "  - $K1_STATE_DIR (state)"
        [ -f "$K1_LOGFILE" ] && echo "  - $K1_LOGFILE (logs)"
    else
        [ -f "$K1_INIT_SYMLINK" ] && echo "  - $K1_INIT_SYMLINK (init script)"
        [ -f "$K1_PIDFILE" ] && echo "  - $K1_PIDFILE (pid file)"
        [ -d "$K1_BASE_DIR" ] && echo "  - $K1_BASE_DIR (all files)"
    fi
fi

echo ""

# Confirmation prompt
if [ "$SKIP_CONFIRM" != "true" ]; then
    printf "${YELLOW}?${NC} Proceed with uninstallation? (y/N): "
    read -r confirm
    if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
        info "Uninstallation cancelled."
        exit 0
    fi
    echo ""
fi

# Perform uninstallation
if [ "$INSTALL_TYPE" = "vanilla" ]; then
    info "Uninstalling vanilla Klipper installation..."
    echo ""
    
    # Stop and disable systemd service
    if [ -f "$SYSTEMD_UNIT_PATH" ] && has_cmd systemctl; then
        info "Stopping systemd service..."
        if systemctl is-active --quiet "$SYSTEMD_SERVICE_NAME"; then
            systemctl stop "$SYSTEMD_SERVICE_NAME" 2>/dev/null || warn "Failed to stop service"
        fi
        
        if systemctl is-enabled --quiet "$SYSTEMD_SERVICE_NAME" 2>/dev/null; then
            systemctl disable "$SYSTEMD_SERVICE_NAME" 2>/dev/null || warn "Failed to disable service"
        fi
        success "Service stopped and disabled"
        
        if [ "$CONFIG_ONLY" != "true" ]; then
            rm -f "$SYSTEMD_UNIT_PATH"
            systemctl daemon-reload 2>/dev/null || true
            success "Systemd service removed"
        fi
    fi
    
    # Remove files
    if [ "$CONFIG_ONLY" = "true" ]; then
        [ -f "$VANILLA_CONFIG" ] && rm -f "$VANILLA_CONFIG" && success "Config file removed"
        [ -d "$VANILLA_STATE_DIR" ] && rm -rf "$VANILLA_STATE_DIR" && success "State directory removed"
    else
        if [ -d "$VANILLA_BASE_DIR" ]; then
            rm -rf "$VANILLA_BASE_DIR"
            success "Installation directory removed: $VANILLA_BASE_DIR"
        fi
    fi
    
elif [ "$INSTALL_TYPE" = "k1" ]; then
    info "Uninstalling Creality K1 installation..."
    echo ""
    
    # Stop service using K1 service script
    if [ -f "$K1_SERVICE_SCRIPT" ]; then
        info "Stopping service..."
        "$K1_SERVICE_SCRIPT" stop 2>/dev/null || true
        success "Service stopped"
    elif [ -f "$K1_PIDFILE" ]; then
        info "Stopping process..."
        if [ -f "$K1_PIDFILE" ] && kill -0 $(cat "$K1_PIDFILE") 2>/dev/null; then
            kill $(cat "$K1_PIDFILE") 2>/dev/null || true
            success "Process stopped"
        fi
    fi
    
    # Remove init.d symlink
    if [ "$CONFIG_ONLY" != "true" ] && [ -L "$K1_INIT_SYMLINK" ] || [ -f "$K1_INIT_SYMLINK" ]; then
        rm -f "$K1_INIT_SYMLINK"
        success "Init script removed"
    fi
    
    # Remove PID file
    if [ -f "$K1_PIDFILE" ]; then
        rm -f "$K1_PIDFILE"
        success "PID file removed"
    fi
    
    # Remove files
    if [ "$CONFIG_ONLY" = "true" ]; then
        [ -f "$K1_CONFIG" ] && rm -f "$K1_CONFIG" && success "Config file removed"
        [ -d "$K1_STATE_DIR" ] && rm -rf "$K1_STATE_DIR" && success "State directory removed"
        [ -f "$K1_LOGFILE" ] && rm -f "$K1_LOGFILE" && success "Log file removed"
    else
        if [ -d "$K1_BASE_DIR" ]; then
            rm -rf "$K1_BASE_DIR"
            success "Installation directory removed: $K1_BASE_DIR"
        fi
    fi
fi

# Final message
echo ""
info "═══════════════════════════════════════"
if [ "$CONFIG_ONLY" = "true" ]; then
    success "Configuration cleaned!"
    info "Binary remains at:"
    [ "$INSTALL_TYPE" = "vanilla" ] && echo "  $VANILLA_BIN"
    [ "$INSTALL_TYPE" = "k1" ] && echo "  $K1_BIN"
else
    success "Uninstallation complete!"
fi
info "═══════════════════════════════════════"
echo ""

if [ "$CONFIG_ONLY" != "true" ]; then
    info "printer-connector has been completely removed from your system."
fi

echo ""
