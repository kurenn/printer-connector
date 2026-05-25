#!/bin/bash
# Build "Spoolr Connect.app" from the SwiftPM executable.
#
#   ./scripts/build_app.sh            # → dist/Spoolr Connect.app
#   ./scripts/build_app.sh --install  # also replace /Applications/Spoolr Connect.app
#                                      # (backs up the existing app first)
#
# Produces a status-bar (LSUIElement) bundle with the same id as the Go app
# (io.spoolr.connect) so it's a drop-in for the existing LaunchAgent.
set -euo pipefail

cd "$(dirname "$0")/.."          # → macos/SpoolrConnect
APP_NAME="Spoolr Connect"
BUNDLE_ID="io.spoolr.connect"
EXEC="SpoolrConnect"
VERSION="0.1.0"
DIST="dist"
APP="$DIST/$APP_NAME.app"

echo "▸ swift build -c release"
swift build -c release
BIN="$(swift build -c release --show-bin-path)/$EXEC"

echo "▸ assembling $APP"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN" "$APP/Contents/MacOS/$EXEC"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>$APP_NAME</string>
  <key>CFBundleDisplayName</key><string>$APP_NAME</string>
  <key>CFBundleIdentifier</key><string>$BUNDLE_ID</string>
  <key>CFBundleExecutable</key><string>$EXEC</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>CFBundleVersion</key><string>$VERSION</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSUIElement</key><true/>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSHumanReadableCopyright</key><string>Spoolr</string>
</dict>
</plist>
PLIST

echo "APPL????" > "$APP/Contents/PkgInfo"

echo "▸ go build connector helper (powers real network discovery)"
APP_ABS="$(pwd)/$APP"
REPO_ROOT="$(cd ../.. && pwd)"
( cd "$REPO_ROOT" && go build -o "$APP_ABS/Contents/Resources/printer-connector" ./cmd/connector )

echo "▸ ad-hoc codesign"
codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || echo "  (codesign skipped)"

echo "✓ built $APP"

if [[ "${1:-}" == "--install" ]]; then
  DEST="/Applications/$APP_NAME.app"
  echo "▸ installing to $DEST"
  # Quit any running instance.
  osascript -e "tell application \"$APP_NAME\" to quit" >/dev/null 2>&1 || true
  pkill -f "Spoolr Connect.app/Contents/MacOS" >/dev/null 2>&1 || true
  sleep 1
  # Preserve the existing app (reversible).
  if [[ -d "$DEST" ]]; then
    BACKUP="/Applications/$APP_NAME (backup $(date +%Y%m%d-%H%M%S)).app"
    echo "  backing up existing → $BACKUP"
    mv "$DEST" "$BACKUP"
  fi
  cp -R "$APP" "$DEST"
  echo "▸ launching"
  open "$DEST"
  echo "✓ installed + launched $DEST"
fi
