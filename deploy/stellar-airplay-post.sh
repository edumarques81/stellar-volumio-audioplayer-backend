#!/bin/sh
# Stellar AirPlay session-end hook.
#
# shairport-sync invokes this when the AirPlay sender (iPhone, etc.)
# disconnects — the authoritative signal that the session has truly
# ended (as opposed to per-track `pend` events, which fire on every
# track boundary mid-session).
#
# We POST {ended: true} directly to the Mac backend so the
# pushAirplayEnded event broadcasts and connected clients (iOS app, LCD
# kiosk) revert to their pre-AirPlay views. The daemon could not do
# this itself reliably — see cmd/stellar-airplay/main.go for the
# heartbeat-gating attempt that failed.

set -e

ENV_FILE="/etc/stellar-airplay/env"
LOG="/var/log/stellar-airplay.log"

echo "$(date -Is) airplay session END (post-hook)" >> "$LOG" 2>/dev/null || true

if [ ! -f "$ENV_FILE" ]; then
  echo "$(date -Is) post-hook: missing $ENV_FILE" >> "$LOG" 2>/dev/null || true
  exit 0
fi

# shellcheck disable=SC1090
. "$ENV_FILE"

if [ -z "${STELLAR_AIRPLAY_MAC_URL:-}" ] || [ -z "${STELLAR_AIRPLAY_KEY:-}" ]; then
  echo "$(date -Is) post-hook: STELLAR_AIRPLAY_MAC_URL / _KEY unset" >> "$LOG" 2>/dev/null || true
  exit 0
fi

# 3-second connect timeout, 5-second total. Non-fatal on failure —
# we never want the hook to block shairport on a network blip.
curl --silent --show-error --max-time 5 --connect-timeout 3 \
  -X POST \
  -H "Authorization: Bearer ${STELLAR_AIRPLAY_KEY}" \
  -H "Content-Type: application/json" \
  -d "{\"ended\":true}" \
  "${STELLAR_AIRPLAY_MAC_URL}/state" \
  >> "$LOG" 2>&1 || true

exit 0
