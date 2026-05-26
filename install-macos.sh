#!/bin/bash
# One-line macOS installer for the Spoolr Connect menu-bar app.
#
#   curl -fsSL https://raw.githubusercontent.com/kurenn/printer-connector/main/install-macos.sh | bash
#
# Downloads the latest ad-hoc-signed Spoolr Connect.app from GitHub Releases and
# installs it to /Applications. Because the download happens HERE (not in a web
# browser), macOS does not attach the quarantine flag — so the app launches
# without the "unverified developer" warning.
#
# The app is ad-hoc signed, NOT Developer-ID signed or notarized (Spoolr does not
# pay for an Apple Developer account). It is universal (Apple Silicon + Intel).
# If you instead download the .zip from the Releases page in a browser, macOS
# will quarantine it; see README "Installing on macOS" for how to open it.
#
# Env overrides:
#   VERSION=v0.2.0   install a specific release (default: latest)
set -euo pipefail

REPO="kurenn/printer-connector"
APP_NAME="Spoolr Connect"
ASSET="SpoolrConnect-macos.zip"
VERSION="${VERSION:-latest}"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "This installer is macOS-only. For printer hosts use install-klipper.sh / install-k1.sh." >&2
  exit 1
fi

if [ "$VERSION" = "latest" ]; then
  URL="https://github.com/$REPO/releases/latest/download/$ASSET"
else
  URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
fi

DEST="/Applications/$APP_NAME.app"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "▸ downloading Spoolr Connect ($VERSION)…"
curl -fSL --progress-bar "$URL" -o "$TMP/app.zip"

echo "▸ extracting…"
ditto -x -k "$TMP/app.zip" "$TMP/extracted"
SRC="$TMP/extracted/$APP_NAME.app"
[ -d "$SRC" ] || { echo "error: '$APP_NAME.app' not found in the downloaded archive" >&2; exit 1; }

# Defensive: clear quarantine in case the archive picked it up somewhere upstream.
xattr -dr com.apple.quarantine "$SRC" 2>/dev/null || true

echo "▸ stopping any running instance…"
osascript -e "tell application \"$APP_NAME\" to quit" >/dev/null 2>&1 || true
pkill -f "$APP_NAME.app/Contents/MacOS" >/dev/null 2>&1 || true
sleep 1

# /Applications is usually writable by admins; fall back to sudo if not.
run() { "$@" 2>/dev/null || sudo "$@"; }

if [ -d "$DEST" ]; then
  BACKUP="/Applications/$APP_NAME (backup $(date +%Y%m%d-%H%M%S)).app"
  echo "▸ backing up existing app → $(basename "$BACKUP")"
  run mv "$DEST" "$BACKUP"
fi

echo "▸ installing to $DEST…"
run cp -R "$SRC" "$DEST"

echo "▸ launching…"
open "$DEST"
echo "✓ Spoolr Connect installed. Look for the icon in your menu bar."
