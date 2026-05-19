#!/bin/bash
# Idempotent installer for the Mac-side stellar-backend.
#   First run: creates env template at ~/.config/stellar-backend/env, exits 0
#              with "fill in env then re-run" message.
#   Second+ run: builds darwin/arm64 binary, installs to ~/stellar-backend/,
#                (re)loads LaunchAgent via launchctl bootstrap + kickstart.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# 1. Ensure user-side directories exist
mkdir -p "${HOME}/stellar-backend"
mkdir -p "${HOME}/.config/stellar-backend"
mkdir -p "${HOME}/Library/Logs"
mkdir -p "${HOME}/Library/LaunchAgents"

# 2. Install wrapper (system path — needs sudo)
sudo install -m 755 deploy/stellar-backend-launcher.sh \
  /usr/local/bin/stellar-backend-launcher.sh

# 3. Install LaunchAgent plist (user path — no sudo)
install -m 644 deploy/com.stellar.backend.plist \
  "${HOME}/Library/LaunchAgents/com.stellar.backend.plist"

# 4. First-run: create env template and stop
if [ ! -f "${HOME}/.config/stellar-backend/env" ]; then
  install -m 600 deploy/env.example "${HOME}/.config/stellar-backend/env"
  echo ""
  echo "============================================================"
  echo " First-run setup complete."
  echo " Edit ${HOME}/.config/stellar-backend/env and fill in every"
  echo " blank value, then re-run this script."
  echo "============================================================"
  exit 0
fi

# 5. Build darwin/arm64 binary
echo "→ Building darwin/arm64 binary..."
make build-darwin
install -m 755 bin/stellar-darwin-arm64 "${HOME}/stellar-backend/stellar"

# 6. (Re)load LaunchAgent. bootout is allowed to fail (first install).
echo "→ Reloading LaunchAgent..."
launchctl bootout "gui/$(id -u)/com.stellar.backend" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "${HOME}/Library/LaunchAgents/com.stellar.backend.plist"
launchctl kickstart -k "gui/$(id -u)/com.stellar.backend"

echo ""
echo "→ Backend started."
echo "  Logs: tail -f ~/Library/Logs/stellar-backend.{out,err}.log"
echo "  Status: launchctl print gui/\$(id -u)/com.stellar.backend | grep state"
