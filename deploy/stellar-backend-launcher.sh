#!/bin/bash
# Wrapper invoked by ~/Library/LaunchAgents/com.stellar.backend.plist.
# Sources ~/.config/stellar-backend/env (refusing if perms drift wider than 600),
# then exec's caffeinate -dis ./stellar with MPD flags.
set -euo pipefail

ENV_FILE="${HOME}/.config/stellar-backend/env"
BACKEND_DIR="${HOME}/stellar-backend"
BIN="${BACKEND_DIR}/stellar"

if [ ! -f "$ENV_FILE" ]; then
  echo "FATAL: missing $ENV_FILE" >&2
  exit 1
fi

PERMS=$(stat -f '%Lp' "$ENV_FILE")
if [ "$PERMS" != "600" ]; then
  echo "FATAL: $ENV_FILE perms are $PERMS, must be 600" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

cd "$BACKEND_DIR"
exec /usr/bin/caffeinate -dis "$BIN" \
  -mpd-host "${STELLAR_MPD_HOST:-192.168.86.25}" \
  -mpd-port "${STELLAR_MPD_PORT:-6600}"
