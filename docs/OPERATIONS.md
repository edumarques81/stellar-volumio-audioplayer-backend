# Operations — Stellar Backend on Mac

Runbook for the M1.C topology where the backend runs on the Mac (LaunchAgent-supervised) and three small services run on the Pi (lcd-control, stellar-mount-control, stellar-spectrum).

For the design rationale see `docs/superpowers/specs/2026-05-19-m1c-cutover-design.md`.

## Quick-glance topology

```
Mac (192.168.86.221, Eduardos-Laptop.local)
  • launchd LaunchAgent ~/Library/LaunchAgents/com.stellar.backend.plist
  • Wrapper:  /usr/local/bin/stellar-backend-launcher.sh
  • Env:      ~/.config/stellar-backend/env  (0600)
  • Binary:   ~/stellar-backend/stellar
  • Data dir: ~/stellar-backend/data/library.db (DB) + ~/Library/Application Support/stellar/ (sources.json + local music)
  • Logs:     ~/Library/Logs/stellar-backend.{out,err}.log
  • Listens on :3000 (Socket.IO + REST + /internal/spectrum ingest)

Raspberry Pi 5 (192.168.86.25, eduardo@stellar.local)
  • mpd.service                       → playback (unchanged from pre-M1.C)
  • stellar-spectrum.service          → /tmp/mpd_spectrum.fifo → POST Mac:3000/internal/spectrum
  • lcd-control.service               → :8081, X-Auth-Token, /api/screen/{on,off,status}
  • stellar-mount-control.service     → :8082, X-Auth-Token, /api/mount/{shares,devices,mount,unmount,is-mounted,mountpoint,symlink}
  • stellar-backend.service           → STOPPED + DISABLED (kept for rollback)
```

## Mac backend ops

> **Note:** The LaunchAgent (`com.stellar.backend`) is permanently disabled because NordVPN Threat Protection Pro blocks launchd-spawned unsigned binaries (responsible-process heuristic — see memory `reference_nordvpn_launchd_vs_shell_filter`). The Mac stellar backend is now started shell-spawned via `deploy/stellar-restart.sh`, which `~/bin/stellar-restart.sh` symlinks to. `launchctl print` is still useful for inspection if the LaunchAgent ever gets re-enabled, but `launchctl kickstart -k` will not work.

### Health checks

```bash
# Quick: are we up?
curl -fsS --max-time 2 http://localhost:3000/api/v1/getState >/dev/null && echo UP || echo DOWN

# Detailed launchd state
launchctl print gui/$(id -u)/com.stellar.backend | grep -E 'state|last exit code|program'

# Recent log lines
tail -n 50 ~/Library/Logs/stellar-backend.out.log
tail -n 50 ~/Library/Logs/stellar-backend.err.log
```

### Start / stop / restart

```bash
# Start (first time after install or after explicit stop)
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.stellar.backend.plist

# Stop
launchctl bootout gui/$(id -u)/com.stellar.backend

# Restart
~/bin/stellar-restart.sh backend

# Tail live logs while restarting
tail -f ~/Library/Logs/stellar-backend.{out,err}.log
```

### Redeploy after binary change

```bash
cd ~/workspace/stellar-streamer/stellar-volumio-audioplayer-backend
make build-darwin
install -m 755 bin/stellar-darwin-arm64 ~/stellar-backend/stellar
~/bin/stellar-restart.sh backend
```

The `deploy/install-mac-backend.sh` script does the same thing idempotently — running it on subsequent invocations rebuilds the binary and reloads the agent.

### Env file ops

```bash
# Verify perms tight (must print 600)
stat -f '%Lp' ~/.config/stellar-backend/env

# Edit env file (will fail-fast the next service start if perms drift wider than 600)
chmod 600 ~/.config/stellar-backend/env
${EDITOR:-vi} ~/.config/stellar-backend/env
~/bin/stellar-restart.sh backend
```

## Pi services ops (via ssh)

All Pi commands assume `ssh eduardo@192.168.86.25` (or `eduardo@stellar.local` when mDNS resolves).

### Status of all three new services

```bash
ssh eduardo@192.168.86.25 'sudo systemctl status lcd-control stellar-mount-control stellar-spectrum --no-pager'
```

### Individual control

```bash
# Replace <service> with lcd-control | stellar-mount-control | stellar-spectrum
ssh eduardo@192.168.86.25 'sudo systemctl {start|stop|restart|status} <service>'
```

### Tail logs

```bash
ssh eduardo@192.168.86.25 'sudo journalctl -u stellar-spectrum -f'
ssh eduardo@192.168.86.25 'sudo journalctl -u lcd-control -f'
ssh eduardo@192.168.86.25 'sudo journalctl -u stellar-mount-control -f'
```

### Health probes (bearer-token authenticated)

```bash
# LCD
TOK=$(ssh eduardo@192.168.86.25 'sudo cat /etc/lcd-control/token')
curl -fsS http://192.168.86.25:8081/api/screen/status -H "X-Auth-Token: $TOK"

# Mount control
TOK=$(ssh eduardo@192.168.86.25 'sudo cat /etc/stellar-mount-control/token')
curl -fsS "http://192.168.86.25:8082/api/mount/devices" -H "X-Auth-Token: $TOK"
```

## Rollback to Pi-resident backend

If the Mac backend needs to be taken out of the loop (Mac in shop, troubleshooting, etc.):

```bash
# 1. Stop the Mac backend
launchctl bootout gui/$(id -u)/com.stellar.backend

# 2. Revert frontend config.json on the Volumio2-UI repo
cd ~/workspace/stellar-streamer/Volumio2-UI
git revert <commit-that-flipped-backendUrl>
# OR manually: edit public/config.json backendUrl back to http://192.168.86.25:3000

# 3. On the Pi: stop spectrum daemon, re-enable backend
ssh eduardo@192.168.86.25 'sudo systemctl stop stellar-spectrum && sudo systemctl disable stellar-spectrum'
ssh eduardo@192.168.86.25 'sudo systemctl enable --now stellar-backend'

# 4. Hard-reload the kiosk so it picks up the reverted config.json
# See "Reload kiosk via CDP" below.
```

The Pi `stellar-backend.service` unit + binary are retained indefinitely. Re-cutting over later is a `launchctl bootstrap` + Pi service swap.

## Reload kiosk via CDP (Chrome DevTools Protocol)

The kiosk's chromium runs with `--remote-debugging-port=9222` bound to localhost on the Pi. To reload remotely without a keyboard attached:

```bash
ssh eduardo@192.168.86.25 'python3 -c "
import json, websocket, urllib.request
tabs = json.loads(urllib.request.urlopen(\"http://localhost:9222/json\").read())
ws = websocket.create_connection(tabs[0][\"webSocketDebuggerUrl\"])
ws.send(json.dumps({\"id\":1,\"method\":\"Page.reload\",\"params\":{\"ignoreCache\":True}}))
print(ws.recv())
ws.close()
"'
```

(Requires `python3-websocket` on the Pi: `sudo apt install python3-websocket`.)

## Token rotation

Each of the three Pi services has its own token at `/etc/<service>/token`. To rotate:

```bash
SERVICE=lcd-control   # or stellar-mount-control or stellar-spectrum
NEW=$(openssl rand -hex 32)
ssh eduardo@192.168.86.25 "echo $NEW | sudo tee /etc/${SERVICE}/token >/dev/null && sudo systemctl restart ${SERVICE}"
# Manually edit ~/.config/stellar-backend/env on Mac, update the matching STELLAR_*_TOKEN field
~/bin/stellar-restart.sh backend
echo "New token (paste into Mac env): $NEW"
```

For `stellar-spectrum`, the token lives inside `/etc/stellar-spectrum/env` as `STELLAR_SPECTRUM_KEY=` rather than its own dedicated token file. Edit that line directly and restart.

## Library cache on Mac

The library DB lives at `~/stellar-backend/data/library.db`. First-time bootstrap rebuilds it from MPD (this can take an hour for a 5000-album library because enrichment is rate-limited).

```bash
# Force a full rebuild (rare — only if the DB is corrupted)
launchctl bootout gui/$(id -u)/com.stellar.backend
rm ~/stellar-backend/data/library.db
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.stellar.backend.plist
# Watch the InitializeCache / FullBuild progress in logs
tail -f ~/Library/Logs/stellar-backend.out.log | grep -E 'cache|library'
```

## Known tech debt (deferred from M1.C)

- **`paths.DataDir()` not used for the library DB.** `internal/transport/socketio/server.go:127` and `internal/transport/socketio/enrichment_handlers.go:81` hardcode `$HOME/stellar-backend/data/library.db` and `$HOME/stellar-backend/data/cache` respectively, bypassing `internal/infra/paths.DataDir()`. Migrating means a Pi-side mv + code change + test updates. Slated for M1.G.
- **`STELLAR_SPECTRUM_SOURCE=local` codepath still present** in `cmd/stellar/main.go`. Retained for rollback fidelity throughout M1; removal becomes safe after the Mac topology has been stable for a few weeks.
- **`Volumio2-UI/.env`** has `STELLAR_BACKEND_FOLDER` pointing at a pre-reorg path. Independent cleanup.
- **`stellar-arm64`, `stellar-arm64-cgo`, `stellar-arm64-nocgo`** tracked binaries at backend repo root are stale. Independent cleanup.

## Cutover verification

A single command runs all pre-cutover gates (G1-G9) and optionally the three done-gates:

```bash
cd ~/workspace/stellar-streamer/stellar-volumio-audioplayer-backend
bash deploy/verify-cutover.sh        # pre-cutover (G1-G9)
bash deploy/verify-cutover.sh --done # post-cutover (G1-G9 + D1-D3)
```

Per-row PASS/FAIL with summary. Exits non-zero on any failure.
