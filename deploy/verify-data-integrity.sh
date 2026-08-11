#!/bin/bash
# Data-integrity verification for the SSD music library (Phase 1,
# DATA-03/DATA-04). Run ON THE PI. Prints PASS/FAIL per gate and exits
# non-zero if any fails.
#
# I1/I2 assert ROADMAP Phase 1 success criterion 1: MPD's index of the
# SSD-hosted music (the "USB" source in MPD's music_directory, symlinked to
# /mnt/ssd/Music) matches the real (non-`._`) audio file count on disk, with
# zero `._`-prefixed AppleDouble sidecar entries remaining anywhere in MPD's
# index.
#
# I3 (Plan 01-05) asserts ROADMAP Phase 1 success criterion 4 LIVE against
# the deployed, hardened backend: writing one throwaway `._`-prefixed file
# into the music library and rescanning must not change any album's
# track_count or any artist's album_count in the backend's cache DB. This is
# a *live* regression test (real MPD indexing + the real deployed cache
# builder), complementing the synthetic-attrs unit tests from Plan 01-01/
# 01-02, which cannot fully substitute for it (real MPD's FLAC/DSF decoders
# may handle malformed resource-fork bytes differently than a mocked
# `mpd.Attrs` slice assumes).
#
# NOTE on scope: MPD's music_directory (/var/lib/mpd/music) aggregates THREE
# sources -- USB (-> /mnt/ssd/Music, the SSD this script checks), INTERNAL
# (the Pi's own onboard storage, unrelated to this phase), and NAS (-> a
# currently-unmounted /mnt/NAS share, intentionally off). `mpc stats`' global
# Songs: line sums all three, so I1 below deliberately scopes to
# `mpc listall USB` rather than `mpc stats`, to avoid a false FAIL from
# INTERNAL's unrelated song count.
#
# I3 dependencies: `deploy/rebuild-cache.py` (sibling script, resolved
# relative to this script's own directory so it works regardless of deploy
# path) and its `python-socketio` package on the Pi's python3. sudo access
# to remount /mnt/ssd rw/ro (passwordless on this single-user appliance, per
# Plan 01-03/01-04 precedent).
#
# Usage (on the Pi):
#   bash ~/stellar-backend/deploy/verify-data-integrity.sh
#
# I3 env var overrides (sane defaults shown):
#   TEST_ALBUM_DIR   -- album folder to write the throwaway file into
#                        (default: an existing Jacob Collier album under
#                        MUSIC_ROOT)
#   LIBRARY_DB        -- path to the backend's cache sqlite DB
#   BACKEND_URL       -- Socket.IO URL for triggering cache rebuilds
#   REBUILD_TIMEOUT_S -- seconds to wait for each cache rebuild to complete
set -uo pipefail

MUSIC_ROOT="${MUSIC_ROOT:-/mnt/ssd/Music}"
MOUNT_POINT="${MOUNT_POINT:-/mnt/ssd}"
MPD_SOURCE="${MPD_SOURCE:-USB}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_ALBUM_DIR="${TEST_ALBUM_DIR:-$MUSIC_ROOT/Jacob Collier/Djesse Vol. 4 (Deluxe)}"
LIBRARY_DB="${LIBRARY_DB:-$HOME/stellar-backend/data/library.db}"
BACKEND_URL="${BACKEND_URL:-http://localhost:3000}"
REBUILD_TIMEOUT_S="${REBUILD_TIMEOUT_S:-60}"
REBUILD_SCRIPT="${REBUILD_SCRIPT:-$SCRIPT_DIR/rebuild-cache.py}"
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

# --- I3: live ._-recurrence regression (DATA-04, Plan 01-05) ---------------
# Writes one throwaway `._`-prefixed file into $TEST_ALBUM_DIR, rescans MPD,
# triggers a live backend cache rebuild, and asserts the FULL per-album
# track_count + per-artist album_count snapshot from the cache DB is
# byte-identical before and after -- then deletes the throwaway file,
# rescans again, and re-asserts the snapshot still matches the ORIGINAL
# baseline (confirms cleanup didn't itself perturb anything). Diffing the
# whole table (not just $TEST_ALBUM_DIR's own album) means this gate needs
# no per-album ID bookkeeping and catches a regression anywhere in the
# library, not just the one album being written to.
I3_SKIP_REASON=""
if [ ! -d "$TEST_ALBUM_DIR" ]; then
  I3_SKIP_REASON="TEST_ALBUM_DIR not found: $TEST_ALBUM_DIR"
elif [ ! -f "$LIBRARY_DB" ]; then
  I3_SKIP_REASON="LIBRARY_DB not found: $LIBRARY_DB"
elif [ ! -f "$REBUILD_SCRIPT" ]; then
  I3_SKIP_REASON="REBUILD_SCRIPT not found: $REBUILD_SCRIPT"
elif ! command -v sqlite3 >/dev/null 2>&1; then
  I3_SKIP_REASON="sqlite3 not found on PATH"
elif ! command -v python3 >/dev/null 2>&1; then
  I3_SKIP_REASON="python3 not found on PATH"
fi

snapshot_counts() {
  {
    sqlite3 "$LIBRARY_DB" "SELECT 'album:' || id || '=' || track_count FROM albums ORDER BY id;"
    sqlite3 "$LIBRARY_DB" "SELECT 'artist:' || name || '=' || album_count FROM artists ORDER BY name;"
  }
}

rebuild_and_wait() {
  python3 "$REBUILD_SCRIPT" "$BACKEND_URL" "$REBUILD_TIMEOUT_S" >/dev/null
}

if [ -n "$I3_SKIP_REASON" ]; then
  check "I3 live ._-recurrence regression (DATA-04) -- SKIPPED: $I3_SKIP_REASON" FAIL
else
  TEST_FILE="$TEST_ALBUM_DIR/._verify-regression-test.flac"
  BASELINE_SNAPSHOT=$(snapshot_counts)
  I3_OK=1

  # Unconditional restore-to-ro on ANY exit from this point on (success,
  # failure, or interrupt), mirroring Plan 01-04's trap-based pattern.
  trap 'sudo mount -o remount,ro "$MOUNT_POINT" >/dev/null 2>&1' EXIT

  sudo mount -o remount,rw "$MOUNT_POINT"
  head -c 4096 /dev/urandom | sudo tee "$TEST_FILE" > /dev/null
  sudo mount -o remount,ro "$MOUNT_POINT"

  mpc update >/dev/null
  timeout 30 mpc idle database >/dev/null 2>&1 || true
  rebuild_and_wait || I3_OK=0

  DIRTY_SNAPSHOT=$(snapshot_counts)
  if [ "$DIRTY_SNAPSHOT" != "$BASELINE_SNAPSHOT" ]; then
    I3_OK=0
  fi

  sudo mount -o remount,rw "$MOUNT_POINT"
  sudo rm -f -- "$TEST_FILE"
  sudo mount -o remount,ro "$MOUNT_POINT"

  mpc update >/dev/null
  timeout 30 mpc idle database >/dev/null 2>&1 || true
  rebuild_and_wait || I3_OK=0

  CLEAN_SNAPSHOT=$(snapshot_counts)
  if [ "$CLEAN_SNAPSHOT" != "$BASELINE_SNAPSHOT" ]; then
    I3_OK=0
  fi

  if [ -f "$TEST_FILE" ]; then
    I3_OK=0
  fi

  if [ "$I3_OK" -eq 1 ]; then
    check "I3 live ._-recurrence regression (DATA-04): track_count/album_count unchanged with+without throwaway ._ file" PASS
  else
    check "I3 live ._-recurrence regression (DATA-04): track_count/album_count unchanged with+without throwaway ._ file" FAIL
  fi
fi

echo ""
if [ "$FAILED" -eq 0 ]; then
  echo "=== ALL GATES PASS -- SSD data integrity verified ==="
  exit 0
else
  echo "=== $FAILED gate(s) FAILED ==="
  exit 1
fi
