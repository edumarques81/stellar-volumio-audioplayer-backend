#!/bin/bash
# Restart Mac stellar backend + Volumio2-UI Vite dev server.
# Both run shell-spawned (not LaunchAgent / launchctl) per the NordVPN ES
# filter workaround — the responsible-process heuristic blocks launchd-
# spawned unsigned binaries but allows shell-spawned. See memory note
# reference_nordvpn_launchd_vs_shell_filter.
#
# Logs go to ~/Library/Logs/stellar-{backend,frontend}.{out,err}.log.
# Run from anywhere; flags below select which half to (re)start.
#
# Usage:
#   stellar-restart.sh            # restart both
#   stellar-restart.sh backend    # backend only
#   stellar-restart.sh frontend   # frontend only
#   stellar-restart.sh status     # show pids + last log lines, no restart

set -euo pipefail

FRONTEND_DIR="${HOME}/workspace/stellar-streamer/Volumio2-UI"
# Find the launcher next to this script (works whether invoked directly or via symlink).
RESOLVED_SCRIPT="$(perl -MCwd=abs_path -le 'print abs_path(shift)' "$0")"
SCRIPT_DIR="$(dirname "$RESOLVED_SCRIPT")"
LAUNCHER="${SCRIPT_DIR}/stellar-backend-launcher.sh"
LOG_DIR="${HOME}/Library/Logs"
BACKEND_OUT="${LOG_DIR}/stellar-backend.out.log"
BACKEND_ERR="${LOG_DIR}/stellar-backend.err.log"
FRONTEND_OUT="${LOG_DIR}/stellar-frontend.out.log"
FRONTEND_ERR="${LOG_DIR}/stellar-frontend.err.log"
BACKEND_PORT=3000
FRONTEND_PORT=5173

backend_pid() { pgrep -f '^/Users/.*/stellar-backend/stellar -port' || true; }
frontend_pid() { lsof -nP -iTCP:${FRONTEND_PORT} -sTCP:LISTEN -t 2>/dev/null || true; }

stop_backend() {
  local pids
  pids=$(backend_pid)
  if [ -n "$pids" ]; then
    echo "[backend] stopping pids: $pids"
    # SIGTERM first, then KILL after 2s if still alive
    echo "$pids" | xargs kill 2>/dev/null || true
    sleep 2
    pids=$(backend_pid)
    if [ -n "$pids" ]; then
      echo "[backend] still alive, SIGKILL: $pids"
      echo "$pids" | xargs kill -9 2>/dev/null || true
    fi
  fi
  # Also kill the caffeinate wrapper if it lingered without its child
  pkill -f 'caffeinate.*stellar-backend/stellar' 2>/dev/null || true
  sleep 1
}

start_backend() {
  if [ ! -x "$LAUNCHER" ]; then
    echo "FATAL: launcher missing or not executable: $LAUNCHER" >&2
    return 1
  fi
  echo "[backend] starting via $LAUNCHER"
  nohup "$LAUNCHER" > "$BACKEND_OUT" 2> "$BACKEND_ERR" &
  disown
  sleep 3
  local pids
  pids=$(backend_pid)
  if [ -z "$pids" ]; then
    echo "[backend] FAILED to start. Last 20 err lines:" >&2
    tail -20 "$BACKEND_ERR" >&2
    return 1
  fi
  echo "[backend] running pid=$pids — logs: $BACKEND_ERR"
}

stop_frontend() {
  local pids
  pids=$(frontend_pid)
  if [ -n "$pids" ]; then
    echo "[frontend] stopping pid on :${FRONTEND_PORT}: $pids"
    echo "$pids" | xargs kill 2>/dev/null || true
    sleep 2
    pids=$(frontend_pid)
    if [ -n "$pids" ]; then
      echo "[frontend] still alive, SIGKILL: $pids"
      echo "$pids" | xargs kill -9 2>/dev/null || true
    fi
  fi
  # Also catch detached vite/esbuild children
  pkill -f 'vite.*Volumio2-UI' 2>/dev/null || true
}

start_frontend() {
  if [ ! -d "$FRONTEND_DIR" ]; then
    echo "FATAL: frontend dir missing: $FRONTEND_DIR" >&2
    return 1
  fi
  echo "[frontend] starting Vite in $FRONTEND_DIR"
  (
    cd "$FRONTEND_DIR"
    nohup npm run dev -- --host 0.0.0.0 > "$FRONTEND_OUT" 2> "$FRONTEND_ERR" &
    disown
  )
  sleep 5
  local pids
  pids=$(frontend_pid)
  if [ -z "$pids" ]; then
    echo "[frontend] FAILED to start on :${FRONTEND_PORT}. Last 20 err lines:" >&2
    tail -20 "$FRONTEND_ERR" >&2
    return 1
  fi
  echo "[frontend] running pid=$pids on :${FRONTEND_PORT} — logs: $FRONTEND_ERR"
}

probe_backend() {
  echo -n "[backend] /api/v1/getState: "
  if curl -fsS -m 3 "http://localhost:${BACKEND_PORT}/api/v1/getState" >/dev/null; then
    echo "OK"
  else
    echo "no response yet (MPD may still be coming up — retry in a few seconds)"
  fi
}

probe_frontend() {
  echo -n "[frontend] :${FRONTEND_PORT}: "
  if curl -fsS -m 3 "http://localhost:${FRONTEND_PORT}/" >/dev/null; then
    echo "OK"
  else
    echo "no response yet"
  fi
}

cmd_status() {
  local bp fp
  bp=$(backend_pid)
  fp=$(frontend_pid)
  echo "[backend]  pid=${bp:-none}"
  echo "[frontend] pid=${fp:-none}"
  if [ -n "$bp" ]; then probe_backend; fi
  if [ -n "$fp" ]; then probe_frontend; fi
  echo
  echo "Tail logs:"
  echo "  tail -f $BACKEND_ERR"
  echo "  tail -f $FRONTEND_ERR"
}

case "${1:-both}" in
  backend)
    stop_backend
    start_backend
    probe_backend
    ;;
  frontend)
    stop_frontend
    start_frontend
    probe_frontend
    ;;
  both|"")
    stop_backend
    stop_frontend
    start_backend
    start_frontend
    probe_backend
    probe_frontend
    ;;
  status)
    cmd_status
    ;;
  *)
    echo "usage: $(basename "$0") [backend|frontend|both|status]" >&2
    exit 2
    ;;
esac
