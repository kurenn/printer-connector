#!/usr/bin/env bash
# Install "Spoolr Connect.app" on this Mac: build it, copy to /Applications,
# clear the Gatekeeper quarantine (unsigned local build), optionally enable
# launch-at-login, and open it. Pair from the menubar (Set up Spoolr Connect…).
#
#   ./install.sh                 # install to /Applications + launch at login
#   ./install.sh --user          # install to ~/Applications instead
#   ./install.sh --no-login      # skip the launch-at-login LaunchAgent
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_NAME="Spoolr Connect"
LABEL="io.spoolr.connect"
LOGIN=1
DEST="/Applications"

for arg in "$@"; do
  case "$arg" in
    --no-login) LOGIN=0 ;;
    --user)     DEST="$HOME/Applications" ;;
    -h|--help)  grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

# 1. Build the bundle into a temp dir.
BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "$BUILD_DIR"' EXIT
OUT_DIR="$BUILD_DIR" bash "$HERE/build_app.sh"
SRC="$BUILD_DIR/$APP_NAME.app"

# 2. Install (fall back to ~/Applications if /Applications isn't writable).
if [ ! -w "$DEST" ]; then
  echo "$DEST is not writable — installing to ~/Applications instead."
  DEST="$HOME/Applications"
fi
mkdir -p "$DEST"
rm -rf "$DEST/$APP_NAME.app"
cp -R "$SRC" "$DEST/"
APP="$DEST/$APP_NAME.app"
echo "Installed: $APP"

# 3. Clear the quarantine bit so the unsigned local build opens without a
#    right-click→Open dance.
xattr -dr com.apple.quarantine "$APP" 2>/dev/null || true

# 4. Launch exactly one instance.
#    With launch-at-login, a per-user LaunchAgent runs `open -a` so the bundle's
#    LSUIElement is honored (menubar-only, no Dock icon) and RunAtLoad launches
#    it immediately — so we must NOT also `open` it here, or we'd get duplicate
#    menu-bar items. Without login, we just open it directly.
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
if [ "$LOGIN" = "1" ]; then
  mkdir -p "$HOME/Library/LaunchAgents"
  cat > "$PLIST" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/bin/open</string>
    <string>-a</string>
    <string>$APP</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
PL
  launchctl unload "$PLIST" 2>/dev/null || true
  launchctl load "$PLIST" # RunAtLoad → open -a → launches now (single instance)
  echo "Launch-at-login enabled ($PLIST). Remove that file to disable."
else
  open "$APP"
fi

echo
echo "Done. Look for the Spoolr ring icon in your menu bar, then 'Set up Spoolr Connect…' to pair."
