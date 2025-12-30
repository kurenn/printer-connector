#!/bin/sh
set -eu

# printer-connector installer (Linux + systemd)
#
# Interactive usage (recommended):
#   sudo ./install.sh
#
# Non-interactive usage:
#   sudo ./install.sh --bin ./printer-connector \
#     --cloud-url http://192.168.1.50:3000 \
#     --pairing-token PAIR_TOKEN \
#     --printer "1|Voron|http://127.0.0.1:7125" \
#     [--printer "2|K1|http://192.168.1.99:7125"]
#
# Notes:
# - By default, the installer uses a pairing token, runs the connector once to obtain
#   connector_id + connector_secret, then enables a systemd service.
# - Requires: systemd, root, network access to cloud + Moonraker.

BIN_SRC=""
CLOUD_URL=""
PAIRING_TOKEN=""
CONNECTOR_ID=""
CONNECTOR_SECRET=""
SITE_NAME=""
CONFIG_PATH="/usr/data/printer-connector/config.json"
STATE_DIR="/usr/data/printer-connector/state"
BIN_DST="/usr/data/printer-connector/printer-connector"
SERVICE_USER="root"
SERVICE_NAME="printer-connector"
LOG_LEVEL="info"
SKIP_PAIR="false"
NO_START="false"
PRINTER_SPECS=""

die() { echo "ERROR: $*" >&2; exit 1; }
has_cmd() { command -v "$1" >/dev/null 2>&1; }

need_root() {
  if [ "$(id -u)" -ne 0 ]; then
    die "Please run as root (use sudo)."
  fi
}

usage() {
  cat <<'USAGE'
printer-connector installer

Interactive:
  sudo ./install.sh

Non-interactive required flags:
  --bin PATH
  --cloud-url URL
  EITHER:
    --pairing-token TOKEN
  OR:
    --skip-pair --connector-id ID --connector-secret SECRET
  AND:
    --printer "ID|NAME|URL"   (repeatable)

Optional:
  --site-name NAME
  --config PATH
  --state-dir PATH
  --bin-dst PATH
  --log-level debug|info|warn|error
  --no-start

Examples:
  sudo ./install.sh --bin ./printer-connector \
    --cloud-url http://192.168.1.50:3000 \
    --pairing-token ABC123 \
    --printer "1|Voron|http://127.0.0.1:7125"
USAGE
}

eval_var() {
  eval "echo \"\${$1}\""
}

set_var() {
  eval "$1=\"\$2\""
}

prompt() {
  # Usage: prompt "Message" VAR_NAME [default]
  msg="$1"
  var="$2"
  def="${3:-}"
  cur="$(eval_var "$var")"
  
  if [ -n "$cur" ]; then return 0; fi

  input=""
  if [ -n "$def" ]; then
    printf "%s [%s]: " "$msg" "$def" >/dev/tty
    read -r input </dev/tty || true
    input="${input:-$def}"
  else
    printf "%s: " "$msg" >/dev/tty
    read -r input </dev/tty || true
  fi

  if [ -z "$input" ]; then
    die "Missing required value for: $msg"
  fi
  set_var "$var" "$input"
}

prompt_secret() {
  # Usage: prompt_secret "Message" VAR_NAME
  msg="$1"
  var="$2"
  cur="$(eval_var "$var")"
  
  if [ -n "$cur" ]; then return 0; fi

  stty -echo 2>/dev/null || true
  printf "%s: " "$msg" >/dev/tty
  read -r input </dev/tty || true
  stty echo 2>/dev/null || true
  echo >/dev/tty
  
  if [ -z "$input" ]; then
    die "Missing required secret for: $msg"
  fi
  set_var "$var" "$input"
}



add_printer_interactive() {
  pid=""
  pname=""
  purl=""
  prompt "Printer ID (must match Rails printer id)" pid
  prompt "Printer display name" pname
  prompt "Moonraker base URL (e.g. http://127.0.0.1:7125)" purl
  
  if [ -z "$PRINTER_SPECS" ]; then
    PRINTER_SPECS="${pid}|${pname}|${purl}"
  else
    PRINTER_SPECS="${PRINTER_SPECS}
${pid}|${pname}|${purl}"
  fi
}

# Parse args
while [ $# -gt 0 ]; do
  case "$1" in
    --bin)
      BIN_SRC="${2:-}"
      shift 2
      ;;
    --cloud-url)
      CLOUD_URL="${2:-}"
      shift 2
      ;;
    --pairing-token)
      PAIRING_TOKEN="${2:-}"
      shift 2
      ;;
    --connector-id)
      CONNECTOR_ID="${2:-}"
      shift 2
      ;;
    --connector-secret)
      CONNECTOR_SECRET="${2:-}"
      shift 2
      ;;
    --site-name)
      SITE_NAME="${2:-}"
      shift 2
      ;;
    --config)
      CONFIG_PATH="${2:-}"
      shift 2
      ;;
    --state-dir)
      STATE_DIR="${2:-}"
      shift 2
      ;;
    --bin-dst)
      BIN_DST="${2:-}"
      shift 2
      ;;
    --log-level)
      LOG_LEVEL="${2:-}"
      shift 2
      ;;
    --printer)
      if [ -z "$PRINTER_SPECS" ]; then
        PRINTER_SPECS="${2:-}"
      else
        PRINTER_SPECS="${PRINTER_SPECS}
${2:-}"
      fi
      shift 2
      ;;
    --skip-pair)
      SKIP_PAIR="true"
      shift
      ;;
    --no-start)
      NO_START="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "Unknown argument: $1 (use --help)"
      ;;
  esac
done

need_root

if ! has_cmd systemctl; then
  die "systemctl not found (systemd required)."
fi

# Interactive prompts for missing values
prompt "Path to compiled printer-connector binary" BIN_SRC
[ -f "$BIN_SRC" ] || die "Binary not found: $BIN_SRC"

prompt "Cloud URL (Rails app base URL)" CLOUD_URL
case "$CLOUD_URL" in
  http://*|https://*)
    ;;
  *)
    die "cloud_url must start with http:// or https://"
    ;;
esac

if [ "$SKIP_PAIR" = "true" ]; then
  prompt "Existing connector ID (from Rails)" CONNECTOR_ID
  prompt_secret "Existing connector secret (will be stored on this device)" CONNECTOR_SECRET
else
  # If not provided via flags, ask.
  prompt "Pairing token (generate in Rails UI)" PAIRING_TOKEN
fi

HOSTNAME_VAL="$(hostname 2>/dev/null || echo "printer")"
if [ -z "$SITE_NAME" ]; then
  # Only prompt if interactive (tty)
  printf "Site name (optional) [%s]: " "$HOSTNAME_VAL" >/dev/tty
  read -r SITE_NAME </dev/tty || true
  SITE_NAME="${SITE_NAME:-$HOSTNAME_VAL}"
fi

if [ -z "$PRINTER_SPECS" ]; then
  local_count=""
  prompt "How many printers will this connector manage?" local_count "1"
  
  # Validate number
  case "$local_count" in
    ''|*[!0-9]*)
      die "Invalid printer count: $local_count"
      ;;
  esac
  
  if [ "$local_count" -lt 1 ]; then
    die "Invalid printer count: $local_count"
  fi
  
  i=1
  while [ "$i" -le "$local_count" ]; do
    echo "---- Printer #$i ----" >/dev/tty
    add_printer_interactive
    i=$((i + 1))
  done
fi

# Validate log level
case "$LOG_LEVEL" in
  debug|info|warn|error) ;;
  *) die "Invalid --log-level: $LOG_LEVEL (debug|info|warn|error)" ;;
esac

# Create dirs first
CONFIG_DIR="$(dirname "$CONFIG_PATH")"
BIN_DIR="$(dirname "$BIN_DST")"
install -d -m 0755 "$CONFIG_DIR"
install -d -m 0755 "$STATE_DIR"
install -d -m 0755 "$BIN_DIR"
chown -R "$SERVICE_USER:$SERVICE_USER" "$STATE_DIR"

# Install binary
install -m 0755 "$BIN_SRC" "$BIN_DST"

# Build printers JSON array
printers_json=""
echo "$PRINTER_SPECS" | while IFS= read -r spec; do
  [ -z "$spec" ] && continue
  
  # Parse pipe-separated values
  pid="${spec%%|*}"
  rest="${spec#*|}"
  pname="${rest%%|*}"
  purl="${rest#*|}"
  
  [ -z "$pid" ] || [ -z "$pname" ] || [ -z "$purl" ] && die "Invalid --printer spec: $spec (expected ID|NAME|URL)"
  
  case "$purl" in
    http://*|https://)
      ;;
    *)
      die "Printer URL must start with http:// or https:// (got: $purl)"
      ;;
  esac

  # Escape quotes in name and url
  pname_esc="$(echo "$pname" | sed 's/\"/\\\"/g')"
  purl_esc="$(echo "$purl" | sed 's/\"/\\\"/g')"

  entry="{\"printer_id\":${pid},\"name\":\"${pname_esc}\",\"base_url\":\"${purl_esc}\"}"
  if [ -z "$printers_json" ]; then
    printers_json="$entry"
  else
    printers_json="${printers_json},${entry}"
  fi
done

# Write config (paired OR pairing token)
tmp_cfg="$(mktemp)"
if [ "$SKIP_PAIR" = "true" ]; then
  cat >"$tmp_cfg" <<JSON
{
  "cloud_url": "${CLOUD_URL}",
  "connector_id": "${CONNECTOR_ID}",
  "connector_secret": "${CONNECTOR_SECRET}",
  "site_name": "${SITE_NAME}",
  "poll_commands_seconds": 3,
  "push_snapshots_seconds": 30,
  "heartbeat_seconds": 10,
  "state_dir": "${STATE_DIR}",
  "moonraker": [ ${printers_json} ]
}
JSON
else
  cat >"$tmp_cfg" <<JSON
{
  "cloud_url": "${CLOUD_URL}",
  "pairing_token": "${PAIRING_TOKEN}",
  "site_name": "${SITE_NAME}",
  "poll_commands_seconds": 3,
  "push_snapshots_seconds": 30,
  "heartbeat_seconds": 10,
  "state_dir": "${STATE_DIR}",
  "moonraker": [ ${printers_json} ]
}
JSON
fi

install -m 0640 "$tmp_cfg" "$CONFIG_PATH"
rm -f "$tmp_cfg"
chown root:"$SERVICE_USER" "$CONFIG_PATH"

# systemd unit
UNIT_PATH="/etc/systemd/system/${SERVICE_NAME}.service"
cat >"$UNIT_PATH" <<UNIT
[Unit]
Description=Printer Connector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
ExecStart=${BIN_DST} --config ${CONFIG_PATH} --log-level ${LOG_LEVEL}
Restart=always
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${STATE_DIR}
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
UNIT

# Pair once (as root) to exchange pairing token for connector_id + connector_secret
if [ "$SKIP_PAIR" != "true" ]; then
  echo "==> Running initial pairing (one-shot)..." >/dev/tty
  echo "    (This will rewrite ${CONFIG_PATH} to include connector_id + connector_secret and remove pairing_token)" >/dev/tty
  "${BIN_DST}" --config "${CONFIG_PATH}" --log-level debug --once || die "Pairing run failed"

  chmod 0640 "${CONFIG_PATH}"
  chown root:"$SERVICE_USER" "${CONFIG_PATH}"
fi

systemctl daemon-reload

if [ "$NO_START" != "true" ]; then
  systemctl enable --now "${SERVICE_NAME}.service"
  echo "==> Service started. Logs:" >/dev/tty
  echo "    journalctl -u ${SERVICE_NAME} -f" >/dev/tty
else
  echo "==> Not starting service (--no-start). You can start it later with:" >/dev/tty
  echo "    systemctl enable --now ${SERVICE_NAME}.service" >/dev/tty
fi

echo "==> Done." >/dev/tty
