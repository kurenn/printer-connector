#!/bin/bash
# Build "Spoolr Connect.app" from the SwiftPM executable.
#
#   ./scripts/build_app.sh              # → dist/Spoolr Connect.app (host arch, fast)
#   ./scripts/build_app.sh --install    # also replace /Applications/Spoolr Connect.app
#                                        # (backs up the existing app first)
#   ./scripts/build_app.sh --universal  # arm64 + x86_64 fat binary (for distribution)
#
# Env: APP_VERSION overrides the bundle version (the release workflow sets it
# from the git tag). Flags may be combined, e.g. `--universal --install`.
#
# Produces a status-bar (LSUIElement) bundle with the same id as the Go app
# (io.spoolr.connect). The app is ad-hoc signed (codesign --sign -), which is
# enough to run on Apple Silicon; it is NOT Developer-ID signed or notarized, so
# downloaded copies need the quarantine flag cleared (see install-macos.sh /
# README) — that's the deliberate $0 distribution tradeoff.
set -euo pipefail

cd "$(dirname "$0")/.."          # → macos/SpoolrConnect
APP_NAME="Spoolr Connect"
BUNDLE_ID="io.spoolr.connect"
EXEC="SpoolrConnect"
VERSION="${APP_VERSION:-0.1.0}"; VERSION="${VERSION#v}"  # accept a v-prefixed tag
DIST="dist"
APP="$DIST/$APP_NAME.app"

INSTALL=false
UNIVERSAL=false
for arg in "$@"; do
  case "$arg" in
    --install)   INSTALL=true ;;
    --universal) UNIVERSAL=true ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

SWIFT_FLAGS=(-c release)
if $UNIVERSAL; then SWIFT_FLAGS+=(--arch arm64 --arch x86_64); fi

echo "▸ swift build ${SWIFT_FLAGS[*]}"
swift build "${SWIFT_FLAGS[@]}"
BIN="$(swift build "${SWIFT_FLAGS[@]}" --show-bin-path)/$EXEC"

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
  <key>NSLocalNetworkUsageDescription</key><string>Spoolr Connect scans your local network to discover and pair Klipper, Moonraker, and Bambu Lab 3D printers.</string>
</dict>
</plist>
PLIST

echo "APPL????" > "$APP/Contents/PkgInfo"

echo "▸ build connector helper (powers real network discovery)"
APP_ABS="$(pwd)/$APP"
DIST_ABS="$(pwd)/$DIST"
REPO_ROOT="$(cd ../.. && pwd)"
HELPER="$APP_ABS/Contents/Resources/printer-connector"
# Stamp the bundled helper with the app version (matches the release workflow),
# so it reports the same version as the .app rather than the "dev" default.
LDFLAGS="-X main.version=$VERSION"
if $UNIVERSAL; then
  echo "  (universal: arm64 + x86_64)"
  ( cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o "$DIST_ABS/pc-arm64" ./cmd/connector )
  ( cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$DIST_ABS/pc-amd64" ./cmd/connector )
  lipo -create "$DIST_ABS/pc-arm64" "$DIST_ABS/pc-amd64" -output "$HELPER"
  rm -f "$DIST_ABS/pc-arm64" "$DIST_ABS/pc-amd64"
else
  ( cd "$REPO_ROOT" && go build -ldflags "$LDFLAGS" -o "$HELPER" ./cmd/connector )
fi

echo "▸ ad-hoc codesign"
codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || echo "  (codesign skipped)"

echo "✓ built $APP"

if $INSTALL; then
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
