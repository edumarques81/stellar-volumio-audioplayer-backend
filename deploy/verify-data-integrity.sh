#!/bin/bash
# Data-integrity verification for the SSD music library (Phase 1, DATA-03). Run
# ON THE PI. Prints PASS/FAIL per gate and exits non-zero if any fails.
#
# Asserts ROADMAP Phase 1 success criterion 1: MPD's index of the SSD-hosted
# music (the "USB" source in MPD's music_directory, symlinked to
# /mnt/ssd/Music) matches the real (non-`._`) audio file count on disk, with
# zero `._`-prefixed AppleDouble sidecar entries remaining anywhere in MPD's
# index.
#
# NOTE on scope: MPD's music_directory (/var/lib/mpd/music) aggregates THREE
# sources -- USB (-> /mnt/ssd/Music, the SSD this script checks), INTERNAL
# (the Pi's own onboard storage, unrelated to this phase), and NAS (-> a
# currently-unmounted /mnt/NAS share, intentionally off). `mpc stats`' global
# Songs: line sums all three, so I1 below deliberately scopes to
# `mpc listall USB` rather than `mpc stats`, to avoid a false FAIL from
# INTERNAL's unrelated song count.
#
# Usage (on the Pi):
#   bash ~/stellar-backend/deploy/verify-data-integrity.sh
set -uo pipefail

MUSIC_ROOT="${MUSIC_ROOT:-/mnt/ssd/Music}"
MPD_SOURCE="${MPD_SOURCE:-USB}"
FAILED=0

check() {
  local name="$1" status="$2"
  if [ "$status" = "PASS" ]; then echo "  OK $name"; else echo "  FAIL $name"; FAILED=$((FAILED + 1)); fi
}

echo "=== SSD music library data integrity (DATA-03) ==="

REAL_FILE_COUNT=$(find "$MUSIC_ROOT" -type f \
  \( -iname '*.flac' -o -iname '*.wav' -o -iname '*.dsf' -o -iname '*.dff' \
     -o -iname '*.mp3' -o -iname '*.m4a' -o -iname '*.ape' -o -iname '*.wv' \
     -o -iname '*.aiff' -o -iname '*.alac' \) \
  ! -name '._*' 2>/dev/null | wc -l | tr -d ' ')

MPD_SOURCE_COUNT=$(mpc listall "$MPD_SOURCE" 2>/dev/null | wc -l | tr -d ' ')

if [ "$REAL_FILE_COUNT" = "$MPD_SOURCE_COUNT" ]; then
  check "I1 mpc listall $MPD_SOURCE count ($MPD_SOURCE_COUNT) == real file count on $MUSIC_ROOT ($REAL_FILE_COUNT)" PASS
else
  check "I1 mpc listall $MPD_SOURCE count ($MPD_SOURCE_COUNT) == real file count on $MUSIC_ROOT ($REAL_FILE_COUNT)" FAIL
fi

# Anchored to a path separator immediately before the ._ prefix so this can
# never false-positive on a real filename that merely contains the substring
# "._" mid-name (e.g. "Track._Remix.flac" would not match).
DOTUNDERSCORE_COUNT=$(mpc listall 2>/dev/null | grep -c '/\._' || true)

if [ "$DOTUNDERSCORE_COUNT" = "0" ]; then
  check "I2 zero ._ entries in MPD's full index" PASS
else
  check "I2 zero ._ entries in MPD's full index ($DOTUNDERSCORE_COUNT found)" FAIL
fi

echo ""
if [ "$FAILED" -eq 0 ]; then
  echo "=== ALL GATES PASS -- SSD data integrity verified ==="
  exit 0
else
  echo "=== $FAILED gate(s) FAILED ==="
  exit 1
fi
