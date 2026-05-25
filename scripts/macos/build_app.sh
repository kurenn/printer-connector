#!/usr/bin/env bash
# Build "Spoolr Connect.app" — the macOS menubar connector. Compiles the
# spoolr-menubar binary (cgo/systray) and assembles a minimal .app bundle.
#
#   OUT_DIR=/tmp/x VERSION=0.2.0 ./build_app.sh
#
# Outputs: $OUT_DIR/Spoolr Connect.app  (OUT_DIR defaults to <repo>/build)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
APP_NAME="Spoolr Connect"
OUT_DIR="${OUT_DIR:-$REPO/build}"
VERSION="${VERSION:-0.1.0}"
APP="$OUT_DIR/$APP_NAME.app"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "build_app.sh only builds the macOS .app (run on macOS)." >&2
  exit 1
fi

echo "Building spoolr-menubar v${VERSION} ..."
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

CGO_ENABLED=1 go build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" \
  -o "$APP/Contents/MacOS/spoolr-menubar" \
  "$REPO/cmd/spoolr-menubar"

sed "s/__VERSION__/$VERSION/g" "$HERE/Info.plist" > "$APP/Contents/Info.plist"

# Optional branded icon (generic app icon is used if absent).
if [ -f "$HERE/AppIcon.icns" ]; then
  cp "$HERE/AppIcon.icns" "$APP/Contents/Resources/AppIcon.icns"
fi

echo "Built: $APP"
