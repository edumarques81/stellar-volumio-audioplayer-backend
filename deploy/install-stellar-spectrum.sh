#!/bin/bash
# Install stellar-spectrum daemon + systemd unit on a Raspberry Pi.
# Run as root on the Pi: sudo bash install-stellar-spectrum.sh
#
# WARNING: Pi-only. Do NOT run on Mac/desktop.
set -euo pipefail

if [ "$EUID" -ne 0 ]; then
  echo "ERROR: Run as root (sudo bash install-stellar-spectrum.sh)" >&2
  exit 1
fi

if [ ! -f /proc/cpuinfo ] || ! grep -q "Raspberry Pi" /proc/cpuinfo; then
  echo "WARNING: This does not look like a Raspberry Pi."
  read -p "Continue anyway? (y/N) " -n 1 -r
  echo
  [[ ! $REPLY =~ ^[Yy]$ ]] && exit 1
fi

echo "[1/4] Creating /etc/stellar-spectrum/..."
mkdir -p /etc/stellar-spectrum

echo "[2/4] Provisioning env file (if absent)..."
if [ ! -f /etc/stellar-spectrum/env ]; then
  KEY=$(openssl rand -hex 32)
  cat > /etc/stellar-spectrum/env <<ENV
STELLAR_SPECTRUM_KEY=${KEY}
STELLAR_MAC_URL=http://192.168.86.221:3000/internal/spectrum
STELLAR_SPECTRUM_FPS=20
STELLAR_SPECTRUM_NUMBINS=64
ENV
  chmod 600 /etc/stellar-spectrum/env
  echo "  → Generated new STELLAR_SPECTRUM_KEY. Copy this value to the Mac's"
  echo "    ~/.config/stellar-backend/env STELLAR_SPECTRUM_KEY field:"
  echo ""
  echo "    ${KEY}"
  echo ""
else
  echo "  → Env file already exists at /etc/stellar-spectrum/env, leaving as-is."
fi

echo "[3/4] Installing systemd unit..."
install -m 644 "$(dirname "$0")/stellar-spectrum.service" \
  /etc/systemd/system/stellar-spectrum.service
systemctl daemon-reload

echo "[4/4] Done. To enable + start:"
echo "  sudo systemctl enable --now stellar-spectrum"
echo ""
echo "To check status:"
echo "  sudo systemctl status stellar-spectrum"
echo "  sudo journalctl -u stellar-spectrum -f"
