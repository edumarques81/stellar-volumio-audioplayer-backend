#!/bin/bash
# Run all 9 pre-cutover gates (G1-G9) and 3 done-gates. Prints PASS/FAIL
# per gate and exits non-zero if any gate fails.
#
# Pre-cutover usage (after PHASE 2, before flipping config.json):
#   bash deploy/verify-cutover.sh
# Done-gate usage (after PHASE 3, post-flip):
#   bash deploy/verify-cutover.sh --done
set -uo pipefail

PI_HOST="${PI_HOST:-eduardo@192.168.86.25}"
PI_IP="${PI_IP:-192.168.86.25}"
NAS_IP="${NAS_IP:-192.168.86.26}"
FAILED=0

check() {
  local name="$1" status="$2"
  if [ "$status" = "PASS" ]; then
    echo "  ✓ $name"
  else
    echo "  ✗ $name"
    FAILED=$((FAILED + 1))
  fi
}

echo "=== Mac-side pre-cutover gates ==="

# G1: binary cross-compiled clean
if strings ~/stellar-backend/stellar 2>/dev/null | grep -qE '/(proc|sys|mnt)/'; then check "G1a strings clean" FAIL; else check "G1a strings clean" PASS; fi
if nm ~/stellar-backend/stellar 2>/dev/null | grep -qE 'wlr_randr|nmcli|mount\.cifs'; then check "G1b nm clean" FAIL; else check "G1b nm clean" PASS; fi

# G2: LaunchAgent loaded
if launchctl print "gui/$(id -u)/com.stellar.backend" 2>/dev/null | grep -q 'state = running'; then check "G2 LaunchAgent running" PASS; else check "G2 LaunchAgent running" FAIL; fi

# G3: env file perms tight
if [ "$(stat -f '%Lp' ~/.config/stellar-backend/env 2>/dev/null)" = "600" ]; then check "G3 env perms 0600" PASS; else check "G3 env perms 0600" FAIL; fi

# G4: backend responding
if curl -fsS --max-time 2 http://localhost:3000/api/v1/getState 2>/dev/null | grep -q status; then check "G4 backend /getState OK" PASS; else check "G4 backend /getState OK" FAIL; fi

# G5: MPD reachable
if nc -z -w 2 "$PI_IP" 6600 2>/dev/null; then check "G5 MPD on Pi reachable" PASS; else check "G5 MPD on Pi reachable" FAIL; fi

# G6: spectrum ingest requires bearer
SPEC_CODE=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 2 -X POST http://localhost:3000/internal/spectrum 2>/dev/null)
if [ "$SPEC_CODE" = "401" ]; then check "G6 /internal/spectrum requires bearer" PASS; else check "G6 /internal/spectrum requires bearer (got $SPEC_CODE)" FAIL; fi

echo ""
echo "=== Pi-side pre-cutover gates ==="

# G7: lcd-control + mount-control active
G7_OUT=$(ssh "$PI_HOST" 'systemctl is-active lcd-control stellar-mount-control 2>/dev/null' 2>/dev/null)
G7_SSH_OK=$?
if [ $G7_SSH_OK -ne 0 ] || [ -z "$G7_OUT" ]; then
  check "G7 lcd-control + mount-control active" FAIL
else
  ACTIVE_NONACTIVE=$(echo "$G7_OUT" | grep -cv '^active$')
  if [ "$ACTIVE_NONACTIVE" = "0" ]; then check "G7 lcd-control + mount-control active" PASS; else check "G7 lcd-control + mount-control active" FAIL; fi
fi

# G8: LCD control responds with token, refuses without
LCD_TOK=$(ssh "$PI_HOST" 'sudo cat /etc/lcd-control/token' 2>/dev/null || echo "")
if [ -n "$LCD_TOK" ] && curl -fsS --max-time 2 "http://$PI_IP:8081/api/screen/status" -H "X-Auth-Token: $LCD_TOK" 2>/dev/null | grep -q status; then check "G8a LCD with token OK" PASS; else check "G8a LCD with token OK" FAIL; fi
LCD_NOAUTH=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 2 "http://$PI_IP:8081/api/screen/status" 2>/dev/null)
if [ "$LCD_NOAUTH" = "401" ]; then check "G8b LCD without token refused (401)" PASS; else check "G8b LCD without token refused (got $LCD_NOAUTH)" FAIL; fi

# G9: Mount control responds with token, refuses without
MNT_TOK=$(ssh "$PI_HOST" 'sudo cat /etc/stellar-mount-control/token' 2>/dev/null || echo "")
if [ -n "$MNT_TOK" ] && curl -fsS --max-time 5 "http://$PI_IP:8082/api/mount/shares?host=$NAS_IP" -H "X-Auth-Token: $MNT_TOK" >/dev/null 2>&1; then check "G9a Mount with token OK" PASS; else check "G9a Mount with token OK" FAIL; fi
MNT_NOAUTH=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 2 "http://$PI_IP:8082/api/mount/shares?host=$NAS_IP" 2>/dev/null)
if [ "$MNT_NOAUTH" = "401" ]; then check "G9b Mount without token refused (401)" PASS; else check "G9b Mount without token refused (got $MNT_NOAUTH)" FAIL; fi

# G10 (M1.E): six read-handler endpoints on mount-control respond + decode correctly.
# Uses the smoke script in Volumio2-UI/ (cross-repo absolute path; the script is
# self-contained and accepts PI_HOST, PI_PORT, TOKEN env vars).
# Note: the smoke script also runs the 3 M1.E.1 POST probes (G11 below covers them
# inline for an explicit, self-contained gate).
SMOKE_SCRIPT="$HOME/workspace/stellar-streamer/Volumio2-UI/scripts/smoke-mount-control-info.sh"
if [ -n "$MNT_TOK" ] && [ -x "$SMOKE_SCRIPT" ] && PI_HOST="$PI_IP" PI_PORT=8082 TOKEN="$MNT_TOK" "$SMOKE_SCRIPT" >/dev/null 2>&1; then
  check "G10 mount-control info endpoints (M1.E) all OK" PASS
else
  check "G10 mount-control info endpoints (M1.E) all OK" FAIL
fi

# G11 (M1.E.1): write endpoint POST smoke (idempotent — MPD not restarted).
# Sends current value back to each POST endpoint; the handler detects "no change"
# and returns 200+success=true without touching MPD. Reuses MNT_TOK from G9.
if [ -n "$MNT_TOK" ]; then
  G11_FAIL=0
  for ENDPOINT_BODY in \
    "/api/audio/dsd|$(curl -fsS -m 5 -H "X-Auth-Token: $MNT_TOK" "http://$PI_IP:8082/api/audio/dsd" 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(json.dumps({'mode':d['mode']}))" 2>/dev/null || echo '{"mode":"native"}')|success" \
    "/api/audio/mixer|$(curl -fsS -m 5 -H "X-Auth-Token: $MNT_TOK" "http://$PI_IP:8082/api/audio/mixer" 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(json.dumps({'enabled':d['enabled']}))" 2>/dev/null || echo '{"enabled":false}')|success" \
    "/api/audio/bitperfect/apply|{}|success"; do
    ENDPOINT="${ENDPOINT_BODY%%|*}"
    REST="${ENDPOINT_BODY#*|}"
    BODY="${REST%%|*}"
    EXPECT_KEY="${REST##*|}"
    RESP=$(curl -fsS -m 35 -X POST \
      -H "Content-Type: application/json" \
      -H "X-Auth-Token: $MNT_TOK" \
      -d "$BODY" \
      "http://$PI_IP:8082$ENDPOINT" 2>/dev/null) || { G11_FAIL=1; break; }
    echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); assert '$EXPECT_KEY' in d, f'missing $EXPECT_KEY: {d}'" 2>/dev/null || { G11_FAIL=1; break; }
  done
  if [ "$G11_FAIL" -eq 0 ]; then check "G11 mount-control write endpoints (M1.E.1) all OK" PASS; else check "G11 mount-control write endpoints (M1.E.1) all OK" FAIL; fi
else
  check "G11 mount-control write endpoints (M1.E.1) all OK" FAIL
fi

if [ "${1:-}" = "--done" ]; then
  echo ""
  echo "=== Done-gates (post-cutover) ==="
  if ssh "$PI_HOST" 'systemctl is-enabled stellar-backend' 2>/dev/null | grep -q disabled; then check "D1 Pi stellar-backend disabled" PASS; else check "D1 Pi stellar-backend disabled" FAIL; fi
  if ssh "$PI_HOST" 'systemctl is-enabled stellar-spectrum' 2>/dev/null | grep -q enabled; then check "D2 Pi stellar-spectrum enabled" PASS; else check "D2 Pi stellar-spectrum enabled" FAIL; fi
  if launchctl print-disabled "gui/$(id -u)" 2>/dev/null | grep -q '"com.stellar.backend" => disabled = false'; then check "D3 Mac LaunchAgent autostarts" PASS; else check "D3 Mac LaunchAgent autostarts" FAIL; fi
fi

echo ""
if [ "$FAILED" -eq 0 ]; then
  echo "ALL GATES PASS"
  exit 0
else
  echo "$FAILED GATE(S) FAILED"
  exit 1
fi
