#!/usr/bin/env bash
#
# uninstall-mac.sh — fully remove Spoolr Connect from a Mac.
#
# Wipes the .app bundle, ALL state (Library caches, prefs, http storage, saved
# app state, Application Support, /tmp scratch), the LaunchAgent so it doesn't
# auto-relaunch at login, and any leftover `CLOUD_URL=…localhost…` launchctl
# env that would silently re-point a fresh install at a dev server.
#
# Idempotent — safe to run when some pieces are already gone.
#
# Usage:
#   bin/uninstall-mac.sh           # interactive (prompts before deleting)
#   bin/uninstall-mac.sh --yes     # non-interactive (no prompt)
#
# Discovered the hard way during release-day smoke testing — when a user
# pairs against dev, then tries to re-pair against prod, stale state in any
# of these locations (especially /tmp/spoolr-connector.json and the launchd
# `CLOUD_URL` env) silently hijacks the new pair. Wiping in a single pass
# fixes it.

set -euo pipefail

YES=0
case "${1:-}" in
  -y|--yes) YES=1 ;;
  -h|--help)
    sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
    ;;
  "") ;;
  *) echo "unknown arg: $1 (try --help)" >&2; exit 2 ;;
esac

# Paths Spoolr Connect (bundle id io.spoolr.connect) writes to on macOS.
APP="/Applications/Spoolr Connect.app"
LAUNCH_AGENT="$HOME/Library/LaunchAgents/io.spoolr.connect.plist"
PREFS="$HOME/Library/Preferences/io.spoolr.connect.plist"
CACHES="$HOME/Library/Caches/io.spoolr.connect"
HTTP_STORAGE="$HOME/Library/HTTPStorages/io.spoolr.connect"
APP_SUPPORT="$HOME/Library/Application Support/Spoolr"
SAVED_STATE="$HOME/Library/Saved Application State/io.spoolr.connect.savedState"
TMP_FILES=(
  /tmp/spoolr-connector.json
  /tmp/spoolr-connector.log
  /tmp/spoolr-connector-debug.log
  /tmp/spoolr_app
  /tmp/printer-connector
)

# ── Inventory: collect targets that actually exist (and process state)
present=()
[ -d "$APP" ]          && present+=("$APP")
[ -f "$LAUNCH_AGENT" ] && present+=("$LAUNCH_AGENT")
[ -f "$PREFS" ]        && present+=("$PREFS")
[ -d "$CACHES" ]       && present+=("$CACHES")
[ -d "$HTTP_STORAGE" ] && present+=("$HTTP_STORAGE")
[ -d "$APP_SUPPORT" ]  && present+=("$APP_SUPPORT")
[ -d "$SAVED_STATE" ]  && present+=("$SAVED_STATE")
for t in "${TMP_FILES[@]}"; do
  [ -e "$t" ] && present+=("$t")
done

running_pid="$(pgrep -x SpoolrConnect 2>/dev/null | head -1 || true)"
cloud_url_env="$(launchctl getenv CLOUD_URL 2>/dev/null || true)"

# ── Dry-run preview
echo "── Spoolr Connect uninstall plan ──────────────────────────────────"
if [ -n "$running_pid" ]; then
  echo "  • kill running SpoolrConnect (PID $running_pid)"
fi
if [ "${#present[@]}" -eq 0 ] && [ -z "$running_pid" ] && [ -z "$cloud_url_env" ]; then
  echo "  (nothing to do — Spoolr Connect appears to be fully removed already.)"
  exit 0
fi
for p in "${present[@]}"; do
  echo "  • remove  $p"
done
if [ -n "$cloud_url_env" ]; then
  echo "  • unset launchctl CLOUD_URL (currently: $cloud_url_env)"
fi
echo "───────────────────────────────────────────────────────────────────"

# ── Confirm
if [ "$YES" -ne 1 ]; then
  printf "Proceed? [y/N] "
  read -r reply
  case "$reply" in
    [yY]|[yY][eE][sS]) ;;
    *) echo "aborted."; exit 1 ;;
  esac
fi

# ── Stop running process first so files aren't held open
if [ -n "$running_pid" ]; then
  killall SpoolrConnect 2>/dev/null || true
  # give it a beat to exit cleanly
  for _ in 1 2 3 4 5; do
    pgrep -x SpoolrConnect >/dev/null 2>&1 || break
    sleep 0.5
  done
  echo "  ✔ killed SpoolrConnect"
fi

# ── Unload LaunchAgent (so it doesn't auto-relaunch on next login)
if [ -f "$LAUNCH_AGENT" ]; then
  # `bootout` is the modern form; `unload` for older macOS. Either may error if
  # the agent isn't loaded; that's fine for our purposes.
  launchctl bootout "gui/$(id -u)/io.spoolr.connect" 2>/dev/null || true
  launchctl unload "$LAUNCH_AGENT" 2>/dev/null || true
fi

# ── Delete everything we inventoried
for p in "${present[@]}"; do
  rm -rf -- "$p"
  echo "  ✔ removed $p"
done

# ── Unset stale launchd-level CLOUD_URL (a dev leftover; harmless if no longer set)
if [ -n "$cloud_url_env" ]; then
  launchctl unsetenv CLOUD_URL
  echo "  ✔ unset launchctl CLOUD_URL"
fi

echo "───────────────────────────────────────────────────────────────────"
echo "Done. Reinstall fresh from the latest release when you're ready."
