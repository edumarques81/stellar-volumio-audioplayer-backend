#!/bin/bash
# Install stellar-airplay daemon + systemd unit on a Raspberry Pi.
# Run as root on the Pi: sudo bash install-stellar-airplay.sh
#
# WARNING: Pi-only. Do NOT run on Mac/desktop.
#
# Pre-requisites:
#   - shairport-sync 3.x installed and running
#   - /etc/shairport-sync.conf has metadata { enabled = "yes"; ... } and
#     ignore_volume_control = "yes" under general.
#     (See deploy/configure-shairport-airplay.sh for the conf patch.)
set -euo pipefail

if [ "$EUID" -ne 0 ]; then
  echo "ERROR: Run as root (sudo bash install-stellar-airplay.sh)" >&2
  exit 1
fi

if [ ! -f /proc/cpuinfo ] || ! grep -q "Raspberry Pi" /proc/cpuinfo; then
  echo "WARNING: This does not look like a Raspberry Pi."
  read -p "Continue anyway? (y/N) " -n 1 -r
  echo
  [[ ! $REPLY =~ ^[Yy]$ ]] && exit 1
fi

echo "[1/4] Creating /etc/stellar-airplay/..."
mkdir -p /etc/stellar-airplay

echo "[2/4] Provisioning env file (if absent)..."
if [ ! -f /etc/stellar-airplay/env ]; then
  KEY=$(openssl rand -hex 32)
  cat > /etc/stellar-airplay/env <<ENV
STELLAR_AIRPLAY_KEY=${KEY}
STELLAR_AIRPLAY_MAC_URL=http://192.168.86.221:3000/internal/airplay
STELLAR_AIRPLAY_METADATA_PIPE=/tmp/shairport-sync-metadata
ENV
  chmod 600 /etc/stellar-airplay/env
  echo "  → Generated new STELLAR_AIRPLAY_KEY. Copy this value to the Mac's"
  echo "    ~/.config/stellar-backend/env STELLAR_AIRPLAY_KEY field:"
  echo ""
  echo "    ${KEY}"
  echo ""
else
  echo "  → Env file already exists at /etc/stellar-airplay/env, leaving as-is."
fi

echo "[3/4] Installing systemd unit..."
install -m 644 "$(dirname "$0")/stellar-airplay.service" \
  /etc/systemd/system/stellar-airplay.service
systemctl daemon-reload

echo "[4/4] Done. To enable + start:"
echo "  sudo systemctl enable --now stellar-airplay"
echo ""
echo "To check status:"
echo "  sudo systemctl status stellar-airplay"
echo "  sudo journalctl -u stellar-airplay -f"
