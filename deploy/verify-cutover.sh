#!/bin/bash
# Post-migration verification for the Pi-hosted topology (Pi migration
# 2026-07-03). Run ON THE PI — all checks are localhost. Prints PASS/FAIL per
# gate and exits non-zero if any fails.
#
# This REPLACES the old M1.C Mac-cutover script, whose gates were the reverse
# of what we now want (it asserted stellar-backend *disabled*, expected a Mac
# LaunchAgent, and probed the retired lcd-control:8081 / mount-control:8082
# proxies). The migration moved the backend + frontend back onto the Pi, so the
# gates below assert the Pi-native state.
#
# Usage (on the Pi):
#   bash ~/stellar-backend/deploy/verify-cutover.sh
set -uo pipefail

BASE="${BASE:-http://localhost:3000}"
FAILED=0

check() {
  local name="$1" status="$2"
  if [ "$status" = "PASS" ]; then echo "  ✓ $name"; else echo "  ✗ $name"; FAILED=$((FAILED + 1)); fi
}
is_active()  { [ "$(systemctl is-active  "$1" 2>/dev/null)" = "active" ]; }
is_enabled() { [ "$(systemctl is-enabled "$1" 2>/dev/null)" = "enabled" ]; }

echo "=== Backend service (Pi-hosted) ==="
if is_active stellar-backend.service; then check "S1 stellar-backend active" PASS; else check "S1 stellar-backend active" FAIL; fi
if is_enabled stellar-backend.service; then check "S2 stellar-backend enabled (boots on power-on)" PASS; else check "S2 stellar-backend enabled" FAIL; fi
NOTIFY=$(systemctl show stellar-backend.service -p Type --value 2>/dev/null)
if [ "$NOTIFY" = "notify" ]; then check "S3 Type=notify (sd_notify readiness)" PASS; else check "S3 Type=notify (got '$NOTIFY')" FAIL; fi
WD=$(systemctl show stellar-backend.service -p WatchdogUSec --value 2>/dev/null)
if [ -n "$WD" ] && [ "$WD" != "0" ] && [ "$WD" != "infinity" ]; then check "S4 WatchdogSec set ($WD)" PASS; else check "S4 WatchdogSec set" FAIL; fi

echo "=== HTTP surface (same-origin on :3000) ==="
if [ "$(curl -fsS -o /dev/null -w '%{http_code}' -m 5 "$BASE/health" 2>/dev/null)" = "200" ]; then check "H1 /health 200" PASS; else check "H1 /health 200" FAIL; fi
if [ "$(curl -fsS -o /dev/null -w '%{http_code}' -m 5 "$BASE/ready" 2>/dev/null)" = "200" ]; then check "H2 /ready 200 (MPD+cache ready)" PASS; else check "H2 /ready 200" FAIL; fi
if curl -fsS -m 5 "$BASE/" 2>/dev/null | grep -qi '<div id="app"'; then check "H3 frontend served same-origin (index.html)" PASS; else check "H3 frontend served same-origin" FAIL; fi

echo "=== MPD localhost + bit-perfect (DO-NOT-TOUCH) ==="
MPD_HOST_FLAG=$(systemctl show stellar-backend.service -p ExecStart --value 2>/dev/null | grep -o '\-mpd-host [^ ]*' | awk '{print $2}')
if [ "$MPD_HOST_FLAG" = "127.0.0.1" ] || [ "$MPD_HOST_FLAG" = "localhost" ]; then check "M1 backend -mpd-host localhost" PASS; else check "M1 backend -mpd-host localhost (got '$MPD_HOST_FLAG')" FAIL; fi
if grep -qE '^\s*mixer_type\s+"none"' /etc/mpd.conf 2>/dev/null; then check "M2 mpd mixer_type none (bit-perfect)" PASS; else check "M2 mpd mixer_type none" FAIL; fi
# NB: with mixer_type "none" MPD omits/negates the volume line, so key off a
# field that is always present in `status`.
if printf 'status\n' | nc -q1 localhost 6600 2>/dev/null | grep -qE '^(repeat|state|playlist):'; then check "M3 MPD reachable on localhost:6600" PASS; else check "M3 MPD reachable" FAIL; fi
PORT_FLAG=$(systemctl show stellar-backend.service -p ExecStart --value 2>/dev/null | grep -o '\-port [0-9]*' | awk '{print $2}')
if [ "$PORT_FLAG" = "3000" ]; then check "M4 backend -port 3000 (not default 3001)" PASS; else check "M4 backend -port 3000 (got '$PORT_FLAG')" FAIL; fi
# M5/M6: the LCD VU meter depends entirely on MPD's output device naming our
# extplug tap rather than the raw card. `device "hw:2,0"` is bit-perfect, passes
# M2, and silently means nothing writes the spectrum FIFO -- both needles park
# on the left end stop, which looks exactly like silence. That regression sat
# unnoticed from 2026-10-01 to 2026-10-16 because nothing checked for it.
# The leading ^\s* excludes the thirteen commented-out decoy blocks.
MPD_DEV=$(grep -E '^\s*device\s+"' /etc/mpd.conf 2>/dev/null | head -1 | sed -E 's/.*"(.*)".*/\1/')
if [ "$MPD_DEV" = "stellar_dac_tap" ]; then check "M5 mpd output via ALSA tap (LCD VU meter alive)" PASS; else check "M5 mpd output via ALSA tap (got '$MPD_DEV' -- VU meter dead, see deploy/README-alsa-tap.md)" FAIL; fi
# A stale or missing module is worse than no VU meter: mpd cannot open the
# device at all, so the symptom is no sound.
if ls /usr/lib/*/alsa-lib/libasound_module_pcm_stellar_tap.so >/dev/null 2>&1; then check "M6 stellar_tap extplug installed" PASS; else check "M6 stellar_tap extplug installed" FAIL; fi

echo "=== Power actions (shutdown / reboot) ==="
# P1: the backend runs unprivileged with no logind session, so /sbin/shutdown
# (a systemctl symlink) is refused by polkit -- `pkcheck --action-id
# org.freedesktop.login1.reboot --process <backend pid>` returns "Authorization
# requires authentication". DefaultShutdown/DefaultReboot therefore go through
# `sudo -n`, which needs /etc/sudoers.d/stellar-power. Without that file every
# power button is a silent no-op: the UI shows "Rebooting in 3 seconds..." and
# the Pi stays up. `sudo -n -l` resolves the grant WITHOUT running anything.
if sudo -n -l /sbin/shutdown >/dev/null 2>&1; then check "P1 sudo -n /sbin/shutdown permitted (power buttons work)" PASS; else check "P1 sudo -n /sbin/shutdown permitted -- install /etc/sudoers.d/stellar-power, see deploy/README-systemd.md" FAIL; fi

echo "=== Daemons repointed to localhost ==="
# The daemon env files are root-readable only; use sudo -n (passwordless on the
# appliance). If sudo is unavailable the check reports FAIL with a hint.
if sudo -n grep -q 'localhost:3000/internal/airplay' /etc/stellar-airplay/env 2>/dev/null; then check "D1 airplay daemon -> localhost" PASS; else check "D1 airplay daemon -> localhost (needs sudo to read env)" FAIL; fi
if sudo -n grep -q 'localhost:3000/internal/spectrum' /etc/stellar-spectrum/env 2>/dev/null; then check "D2 spectrum daemon -> localhost" PASS; else check "D2 spectrum daemon -> localhost (needs sudo to read env)" FAIL; fi
if is_active stellar-airplay.service; then check "D3 stellar-airplay active" PASS; else check "D3 stellar-airplay active" FAIL; fi

echo "=== Retired proxies (in-process on Linux) ==="
if ! is_active lcd-control.service; then check "R1 lcd-control retired (in-process LCD)" PASS; else check "R1 lcd-control retired" FAIL; fi
if ! is_active stellar-mount-control.service; then check "R2 mount-control retired (in-process mount)" PASS; else check "R2 mount-control retired" FAIL; fi

echo "=== Kiosk + self-healing ==="
if grep -q 'http://localhost:3000' /usr/local/bin/stellar-kiosk.sh 2>/dev/null; then check "K1 kiosk URL -> localhost:3000" PASS; else check "K1 kiosk URL -> localhost:3000" FAIL; fi
if is_active stellar-kiosk-watchdog.timer; then check "K2 kiosk watchdog timer active" PASS; else check "K2 kiosk watchdog timer active" FAIL; fi

echo ""
if [ "$FAILED" -eq 0 ]; then
  echo "=== ALL GATES PASS — Pi-hosted cutover verified ==="
  exit 0
else
  echo "=== $FAILED gate(s) FAILED ==="
  exit 1
fi
