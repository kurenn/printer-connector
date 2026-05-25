#!/usr/bin/env bash
# Regenerate the macOS icons from the brand sources in this folder:
#   - cmd/spoolr-menubar/icon.png  — menu-bar template (mono) from logo-mark-mono.svg
#   - scripts/macos/AppIcon.icns   — app icon (color) from AppIcon-source.png
#
# Sources come from the Spoolr Brand Kit (logo-mark-compact.svg → logo-mark-mono.svg
# here; icon-512.png → AppIcon-source.png). Requires rsvg-convert (brew install
# librsvg) plus sips + iconutil (built into macOS). Build does NOT need these —
# the generated icon.png is embedded and AppIcon.icns is committed.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"

# 1. Menu-bar template icon (44px, black + alpha; macOS tints it).
command -v rsvg-convert >/dev/null || { echo "need rsvg-convert (brew install librsvg)" >&2; exit 1; }
rsvg-convert -w 44 -h 44 "$HERE/logo-mark-mono.svg" -o "$REPO/cmd/spoolr-menubar/icon.png"
echo "wrote cmd/spoolr-menubar/icon.png"

# 2. App icon .icns from the 512px color source (upscale 512→1024 for the @2x).
SRC="$HERE/AppIcon-source.png"
SET="$(mktemp -d)/AppIcon.iconset"
mkdir -p "$SET"
gen() { sips -z "$1" "$1" "$SRC" --out "$SET/$2" >/dev/null; }
gen 16   icon_16x16.png
gen 32   icon_16x16@2x.png
gen 32   icon_32x32.png
gen 64   icon_32x32@2x.png
gen 128  icon_128x128.png
gen 256  icon_128x128@2x.png
gen 256  icon_256x256.png
gen 512  icon_256x256@2x.png
gen 512  icon_512x512.png
gen 1024 icon_512x512@2x.png
iconutil -c icns "$SET" -o "$HERE/AppIcon.icns"
echo "wrote scripts/macos/AppIcon.icns"
