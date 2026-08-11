#!/usr/bin/env python3
"""Trigger a Stellar backend library cache rebuild via Socket.IO and block
until it completes (Phase 1, DATA-04 support script).

The backend exposes no HTTP endpoint for cache rebuilds -- only the
Socket.IO events `library:cache:rebuild` (fire) and `library:cache:status`
(poll), answered by `pushLibraryCacheStatus`. This script is a thin,
scriptable client for those events so `deploy/verify-data-integrity.sh`'s I3
gate (and any future automation) can trigger + await a rebuild without a
browser.

Freshness is confirmed by the `lastUpdated` timestamp advancing past its
pre-rebuild value while `isBuilding` is false, not just `isBuilding` going
false on its own -- a rebuild that finishes in under our poll interval would
otherwise be missed entirely (observed live 2026-08-11: a no-op rebuild can
complete in ~1s).

Requires: python3 with the `python-socketio` package (present on the Pi as
of Plan 01-05; `pip3 show python-socketio` to confirm).

Usage:
    python3 rebuild-cache.py [URL] [TIMEOUT_SECONDS]

Exit code 0 on a confirmed completed rebuild, 1 on timeout.
"""
import sys
import time
import json

import socketio

URL = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:3000"
TIMEOUT_S = float(sys.argv[2]) if len(sys.argv) > 2 else 60.0

sio = socketio.Client(logger=False, engineio_logger=False)
last_status = {}


@sio.on("pushLibraryCacheStatus")
def on_status(data):
    global last_status
    last_status = data


def main() -> int:
    sio.connect(URL, wait_timeout=10)

    # Capture pre-rebuild lastUpdated as a freshness marker.
    sio.emit("library:cache:status")
    time.sleep(1.0)
    prior_ts = last_status.get("lastUpdated")

    sio.emit("library:cache:rebuild")

    start = time.time()
    ok = False
    while time.time() - start < TIMEOUT_S:
        time.sleep(1.0)
        sio.emit("library:cache:status")
        time.sleep(0.5)
        if last_status.get("isBuilding") is False and last_status.get("lastUpdated") != prior_ts:
            ok = True
            break

    sio.disconnect()
    print("PRIOR_TS:", prior_ts)
    print("FINAL:", json.dumps(last_status))
    if not ok:
        print("WARNING: did not observe a completed rebuild (timestamp advance) within timeout")
        return 1
    print("REBUILD_OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
