#!/usr/bin/env bash
# verify-audio.sh — 20-minute before/after audio-verification protocol
#
# Usage:
#   ./deploy/verify-audio.sh before [BASE_URL]
#   ./deploy/verify-audio.sh after  [BASE_URL]
#
# Arguments:
#   LABEL      "before" or "after" (or any descriptive string, e.g. "pi-v1")
#   BASE_URL   backend base URL (default: http://localhost:3000)
#
# Environment overrides (for rapid CI / smoke runs):
#   DURATION_SECS    total sampling window in seconds (default: 1200 = 20 min)
#   INTERVAL_SECS    seconds between samples             (default: 10)
#
# Pass/fail criteria (matching MIGRATION-PLAN.md §7):
#   PASS when ALL of:
#     • xruns delta == 0  (no new DAC xruns during the window)
#     • throttle_mask stayed 0x0 throughout (no CPU/voltage throttle)
#     • pswpout delta == 0  (no swap-out events — no memory pressure)
#     • max 1-min load < 4  (number of Pi cores)
#
# The script requires curl and jq.

set -euo pipefail

# ---------------------------------------------------------------------------
# Arguments / defaults
# ---------------------------------------------------------------------------

LABEL="${1:-before}"
BASE_URL="${2:-http://localhost:3000}"
DURATION_SECS="${DURATION_SECS:-1200}"
INTERVAL_SECS="${INTERVAL_SECS:-10}"
PASS_LOAD_THRESHOLD=4   # Pi 5 has 4 cores

# ---------------------------------------------------------------------------
# Dependency check
# ---------------------------------------------------------------------------

for cmd in curl jq; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "ERROR: '$cmd' is required but not installed" >&2
        exit 1
    fi
done

# ---------------------------------------------------------------------------
# Report directory + file
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPORT_DIR="${SCRIPT_DIR}/audio-reports"
mkdir -p "$REPORT_DIR"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
REPORT_FILE="${REPORT_DIR}/${LABEL}-${TIMESTAMP}.txt"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

log_report() {
    echo "$@" | tee -a "$REPORT_FILE"
}

fetch_snapshot() {
    # Returns raw JSON or empty string on error.
    curl -s --max-time 5 "${BASE_URL}/metrics" 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Start
# ---------------------------------------------------------------------------

log_report "==================================================================="
log_report "Stellar Audio Verification — label=${LABEL}"
log_report "Started:    $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
log_report "Backend:    ${BASE_URL}"
log_report "Duration:   ${DURATION_SECS}s  Interval: ${INTERVAL_SECS}s"
log_report "==================================================================="
log_report ""

# Verify metrics endpoint is available.
echo "Checking ${BASE_URL}/metrics ..."
PROBE="$(fetch_snapshot)"
if [ -z "$PROBE" ]; then
    echo "ERROR: ${BASE_URL}/metrics returned empty response." >&2
    echo "Ensure the backend is running with STELLAR_METRICS=1" >&2
    exit 1
fi
if ! echo "$PROBE" | jq empty 2>/dev/null; then
    echo "ERROR: /metrics response is not valid JSON" >&2
    exit 1
fi
echo "Metrics endpoint OK."
log_report ""

# ---------------------------------------------------------------------------
# Baseline (first sample)
# ---------------------------------------------------------------------------

FIRST_SNAP="$(fetch_snapshot)"

FIRST_XRUNS="$(echo "$FIRST_SNAP" | jq -r '.xruns // -1')"
FIRST_PSWPOUT="$(echo "$FIRST_SNAP" | jq -r '.pswpout // 0')"
FIRST_THROTTLE="$(echo "$FIRST_SNAP" | jq -r '.throttle_mask // 0')"

log_report "--- Baseline (t=0s) ---"
log_report "xruns:         ${FIRST_XRUNS}"
log_report "pswpout:       ${FIRST_PSWPOUT}"
log_report "throttle_mask: $(printf '0x%x' "$FIRST_THROTTLE")"
log_report ""

# ---------------------------------------------------------------------------
# Sampling loop
# ---------------------------------------------------------------------------

SAMPLE=0
LAST_SNAP="$FIRST_SNAP"

MAX_LOAD1=0
MAX_THROTTLE=0
ALSA_TRANSITIONS=0
PREV_ALSA_STATE="$(echo "$FIRST_SNAP" | jq -r '.alsa.state // "unknown"')"
MAX_GOROUTINES=0
MIN_GOROUTINES=999999
MAX_GC_P99=0

log_report "--- Samples ---"
log_report "TIME(s)  XRUNS  LOAD1  THROTTLE  PSWPOUT  ALSA_STATE  GOROUTINES"
log_report "-------  -----  -----  --------  -------  ----------  ----------"

END_TIME=$(( $(date +%s) + DURATION_SECS ))

while [ "$(date +%s)" -lt "$END_TIME" ]; do
    sleep "$INTERVAL_SECS"
    ELAPSED=$(( DURATION_SECS - (END_TIME - $(date +%s)) ))

    SNAP="$(fetch_snapshot)"
    if [ -z "$SNAP" ] || ! echo "$SNAP" | jq empty 2>/dev/null; then
        log_report "$(printf '%7d' "$ELAPSED")  [fetch error — skipping sample]"
        continue
    fi

    XRUNS="$(echo "$SNAP" | jq -r '.xruns // -1')"
    LOAD1="$(echo "$SNAP" | jq -r '.load.load1 // 0')"
    THROTTLE="$(echo "$SNAP" | jq -r '.throttle_mask // 0')"
    PSWPOUT="$(echo "$SNAP" | jq -r '.pswpout // 0')"
    ALSA_STATE="$(echo "$SNAP" | jq -r '.alsa.state // "unknown"')"
    GOROUTINES="$(echo "$SNAP" | jq -r '.runtime.goroutines // 0')"
    GC_P99="$(echo "$SNAP" | jq -r '.runtime.gc_pause_p99_ms // 0')"

    # Track maxima/minima
    # Use awk for float comparison (bash only does integer arithmetic)
    if awk "BEGIN{exit !($LOAD1 > $MAX_LOAD1)}"; then
        MAX_LOAD1="$LOAD1"
    fi
    if [ "$THROTTLE" -gt "$MAX_THROTTLE" ]; then
        MAX_THROTTLE="$THROTTLE"
    fi
    if [ "$GOROUTINES" -gt "$MAX_GOROUTINES" ]; then
        MAX_GOROUTINES="$GOROUTINES"
    fi
    if [ "$GOROUTINES" -lt "$MIN_GOROUTINES" ]; then
        MIN_GOROUTINES="$GOROUTINES"
    fi
    if awk "BEGIN{exit !($GC_P99 > $MAX_GC_P99)}"; then
        MAX_GC_P99="$GC_P99"
    fi
    if [ "$ALSA_STATE" != "$PREV_ALSA_STATE" ]; then
        ALSA_TRANSITIONS=$(( ALSA_TRANSITIONS + 1 ))
        log_report "  [ALSA state transition: ${PREV_ALSA_STATE} → ${ALSA_STATE} at t=${ELAPSED}s]"
        PREV_ALSA_STATE="$ALSA_STATE"
    fi

    LAST_SNAP="$SNAP"
    SAMPLE=$(( SAMPLE + 1 ))

    log_report "$(printf '%7d' "$ELAPSED")  $(printf '%5s' "$XRUNS")  $(printf '%5s' "$LOAD1")  $(printf '%8s' "0x$(printf '%x' "$THROTTLE")")  $(printf '%7s' "$PSWPOUT")  $(printf '%10s' "$ALSA_STATE")  $(printf '%10s' "$GOROUTINES")"
done

# ---------------------------------------------------------------------------
# Final snapshot and deltas
# ---------------------------------------------------------------------------

FINAL_XRUNS="$(echo "$LAST_SNAP" | jq -r '.xruns // -1')"
FINAL_PSWPOUT="$(echo "$LAST_SNAP" | jq -r '.pswpout // 0')"
FINAL_ALSA_STATE="$(echo "$LAST_SNAP" | jq -r '.alsa.state // "unknown"')"
FINAL_GC_P99="$(echo "$LAST_SNAP" | jq -r '.runtime.gc_pause_p99_ms // 0')"
FINAL_GOROUTINES="$(echo "$LAST_SNAP" | jq -r '.runtime.goroutines // 0')"

if [ "$FIRST_XRUNS" -ge 0 ] && [ "$FINAL_XRUNS" -ge 0 ]; then
    DELTA_XRUNS=$(( FINAL_XRUNS - FIRST_XRUNS ))
else
    DELTA_XRUNS=-1
fi
DELTA_PSWPOUT=$(( FINAL_PSWPOUT - FIRST_PSWPOUT ))

# ---------------------------------------------------------------------------
# Pass/fail evaluation
# ---------------------------------------------------------------------------

PASS=true
FAIL_REASONS=()

# 1. xrun delta
if [ "$DELTA_XRUNS" -lt 0 ]; then
    FAIL_REASONS+=("xruns: unavailable (CAP_SYSLOG not granted — grant AmbientCapabilities in Phase 3)")
elif [ "$DELTA_XRUNS" -ne 0 ]; then
    PASS=false
    FAIL_REASONS+=("xruns delta = ${DELTA_XRUNS} (want 0)")
fi

# 2. throttle — any non-zero mask during the run is a FAIL
if [ "$MAX_THROTTLE" -ne 0 ]; then
    PASS=false
    FAIL_REASONS+=("throttle_mask peaked at 0x$(printf '%x' "$MAX_THROTTLE") (want 0x0 throughout)")
fi

# 3. pswpout delta
if [ "$DELTA_PSWPOUT" -ne 0 ]; then
    PASS=false
    FAIL_REASONS+=("pswpout delta = ${DELTA_PSWPOUT} (want 0 — swap-out events indicate memory pressure)")
fi

# 4. max 1-min load < cores (4)
if awk "BEGIN{exit !($MAX_LOAD1 >= $PASS_LOAD_THRESHOLD)}"; then
    PASS=false
    FAIL_REASONS+=("max load1 = ${MAX_LOAD1} >= ${PASS_LOAD_THRESHOLD} cores (CPU saturated)")
fi

# ---------------------------------------------------------------------------
# Report summary
# ---------------------------------------------------------------------------

log_report ""
log_report "==================================================================="
log_report "SUMMARY — label=${LABEL}   samples=${SAMPLE}"
log_report "==================================================================="
log_report ""
log_report "--- Deltas (end − start) ---"
log_report "  xruns delta:    ${DELTA_XRUNS}"
log_report "  pswpout delta:  ${DELTA_PSWPOUT}"
log_report ""
log_report "--- Peaks ---"
log_report "  max load1:      ${MAX_LOAD1}"
log_report "  max throttle:   0x$(printf '%x' "$MAX_THROTTLE")"
log_report "  ALSA transitions: ${ALSA_TRANSITIONS}"
log_report "  goroutines min/max: ${MIN_GOROUTINES} / ${MAX_GOROUTINES}"
log_report "  GC pause p99 max: ${MAX_GC_P99} ms"
log_report ""
log_report "--- Final state ---"
log_report "  xruns:      ${FINAL_XRUNS}"
log_report "  pswpout:    ${FINAL_PSWPOUT}"
log_report "  alsa state: ${FINAL_ALSA_STATE}"
log_report "  gc p99 ms:  ${FINAL_GC_P99}"
log_report "  goroutines: ${FINAL_GOROUTINES}"
log_report ""
log_report "--- Pass/Fail Criteria ---"
if [ "$DELTA_XRUNS" -lt 0 ]; then
    log_report "  [WARN] xrun count  unavailable (grant CAP_SYSLOG in Phase 3 unit)"
elif [ "$DELTA_XRUNS" -eq 0 ]; then
    log_report "  [PASS] xruns delta = 0"
else
    log_report "  [FAIL] xruns delta = ${DELTA_XRUNS}"
fi

if [ "$MAX_THROTTLE" -eq 0 ]; then
    log_report "  [PASS] throttle stayed 0x0"
else
    log_report "  [FAIL] throttle peaked at 0x$(printf '%x' "$MAX_THROTTLE")"
fi

if [ "$DELTA_PSWPOUT" -eq 0 ]; then
    log_report "  [PASS] pswpout delta = 0"
else
    log_report "  [FAIL] pswpout delta = ${DELTA_PSWPOUT}"
fi

if awk "BEGIN{exit !($MAX_LOAD1 < $PASS_LOAD_THRESHOLD)}"; then
    log_report "  [PASS] max load1 ${MAX_LOAD1} < ${PASS_LOAD_THRESHOLD} cores"
else
    log_report "  [FAIL] max load1 ${MAX_LOAD1} >= ${PASS_LOAD_THRESHOLD} cores"
fi
log_report ""

if $PASS; then
    log_report "OVERALL: PASS"
    RESULT_MSG="PASS"
else
    log_report "OVERALL: FAIL"
    RESULT_MSG="FAIL"
    for reason in "${FAIL_REASONS[@]}"; do
        log_report "  - ${reason}"
    done
fi

log_report ""
log_report "Report written to: ${REPORT_FILE}"
log_report "==================================================================="

echo ""
echo "=== RESULT: ${RESULT_MSG} ==="
echo "Report: ${REPORT_FILE}"

if [ "$RESULT_MSG" = "FAIL" ]; then
    exit 1
fi
exit 0
