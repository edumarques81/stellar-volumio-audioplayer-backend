#!/bin/bash
# Patch /etc/shairport-sync.conf to enable the metadata pipe and lock
# AirPlay volume at 0 dB (full-scale, matching MPD playback level).
#
# Run as root on the Pi: sudo bash configure-shairport-airplay.sh
# Idempotent: re-runs are no-ops if the settings are already present.
set -euo pipefail

CONF=/etc/shairport-sync.conf
BACKUP="${CONF}.bak.$(date +%Y%m%d-%H%M%S)"

if [ "$EUID" -ne 0 ]; then
  echo "ERROR: Run as root" >&2
  exit 1
fi
if [ ! -f "$CONF" ]; then
  echo "ERROR: $CONF not found; is shairport-sync installed?" >&2
  exit 1
fi

echo "[1/3] Backing up $CONF → $BACKUP"
cp -a "$CONF" "$BACKUP"

echo "[2/3] Ensuring general.ignore_volume_control = yes"
if grep -qE '^\s*ignore_volume_control\s*=' "$CONF"; then
  sed -i -E 's|^\s*ignore_volume_control\s*=.*|    ignore_volume_control = "yes";|' "$CONF"
else
  # Insert inside the general { } block.
  awk '
    /^general[[:space:]]*=[[:space:]]*\{/ { in_general=1 }
    in_general && /^};?$/ {
      print "    ignore_volume_control = \"yes\";"
      in_general=0
    }
    { print }
  ' "$CONF" > "${CONF}.tmp" && mv "${CONF}.tmp" "$CONF"
fi

echo "[3/3] Ensuring metadata block exists"
if ! grep -qE '^\s*metadata\s*=' "$CONF"; then
  cat >> "$CONF" <<'METADATA'

metadata =
{
    enabled = "yes";
    include_cover_art = "yes";
    pipe_name = "/tmp/shairport-sync-metadata";
    pipe_timeout = 5000;
};
METADATA
else
  echo "  metadata block already present; leaving in place."
fi

echo
echo "Done. Restart shairport-sync to apply:"
echo "  sudo systemctl restart shairport-sync"
echo
echo "Verify the pipe is being written:"
echo "  sudo head -c 256 /tmp/shairport-sync-metadata | xxd"
