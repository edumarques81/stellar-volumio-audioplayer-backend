# M1.C — Backend relocation cutover (Pi → Mac)

_Date: 2026-05-19 · Spec for the cutover phase of the linear-blanket initiative_

## Goal / WHY

The driving goal — same one that motivated M1.A — is **freeing the Raspberry Pi 5 to play music without audio hiccups**. M1.A was the *enabler*: it stripped Linux/Pi-specific coupling from the backend so it can run on a Mac. M1.C is where the load actually moves off the Pi.

After M1.C:

- The Pi runs `mpd.service` (unchanged) plus three small HTTP-or-FIFO services: `lcd-control.service`, `stellar-mount-control.service`, `stellar-spectrum.service`. None of them do FFT, enrichment, bio LLM, or library cache work.
- The Mac runs `stellar-backend` as a launchd-supervised always-on process. It owns the Socket.IO server, SQLite library cache + enrichment goroutines, bio pipeline, NAS-browse UI mediation, and LCD power control — talking to the Pi over the LAN.
- The Mac-hosted backend speaks MPD-over-network to the Pi (existing `-mpd-host` flag from M1.A's spec). MPD on the Pi remains the single source of truth for playback state.

The user-visible win is **playback should stop glitching** because the Pi's CPU isn't being preempted by background enrichment, FFT compute at 30 fps, or Socket.IO broadcast bursts.

The Mac is the *interim* host. Long-term destination is a Windows mini-PC (per Plan B from the 2026-04-21 topology doc). Every M1.C artefact is designed to port: env-file secret pattern (works on systemd-style EnvironmentFile too), HTTP+bearer Pi services (LAN protocol independent of host OS), build-tag-free `RemoteController` shape (compiles on `_windows.go` too).

## User-locked decisions (this brainstorm)

1. **M1.C scope: single fat phase.** Not split into "M1.C-pre / M1.C-cutover". One brainstorm → one plan → one execution wave. Includes the LCD HTTP client in backend, the `stellar-spectrum.service` unit, the Mac LaunchAgent, plus a *new* `stellar-mount-control.service` on the Pi (surfaced during brainstorm — see decision #5).
2. **Mac power state: always plugged in + caffeinated.** Mac stays at a fixed spot, on AC. The LaunchAgent wrapper invokes `caffeinate -dis` so display+idle+system sleep are suppressed while the backend is the running process. Wi-Fi stays awake on AC by default. Lid-closed clamshell with external power is fine.
3. **Secret management: env file at `~/.config/stellar-backend/env`, mode 0600.** The LaunchAgent wrapper script `source`s it before exec. Plist contains no secrets. Pattern mirrors systemd EnvironmentFile so it ports cleanly to the future Windows host. A wrapper-script perms check (`stat -f '%Lp'`) refuses to start if the file's perms ever drift wider than 600 — guardrail surfaces a clear journal message instead of silently launching with leakable secrets.
4. **Library cache: rebuild from MPD on first Mac boot, no scp from Pi.** Mac starts with empty `~/stellar-backend/data/library.db`. `InitializeCache()` triggers `FullBuild()` from MPD's library, enrichment goroutine catches up over hours (Fanart.tv + Wikipedia + Deezer rate-limited). User-visible cost: LCD shows generic icons / default-album.svg for some artists during the first hour. Accepted because (a) the `cache.DB.Clear()` artwork-wipe bug was fixed in `c0e8741` so subsequent refreshes preserve enrichment, and (b) the 4 known-broken albumart-fallback artists from the parked Task #10 will re-enrich, and the `/artistart` 302-redirect fix from `2ee6814` will handle them correctly this time around.
5. **NAS browse: new `stellar-mount-control.service` on Pi.** Mac backend's mounter cannot mount the Pi's MPD library on the Mac filesystem — MPD on Pi wouldn't see it. So a tiny Pi-side service mirrors the lcd-control pattern: HTTP `:8082`, `X-Auth-Token` bearer, three endpoints (`GET /api/mount/shares`, `POST /api/mount`, `POST /api/mount/unmount`). Backend's darwin/windows `mounter`/`discoverer` become HTTP clients to it. Mac-local darwin impls added in M1.A are **deleted in-place** in M1.C — no deprecation window — the remote impl supersedes them outright. Rationale: keeping superseded dead code costs reviewer cognition with no real safety benefit; `git revert` covers the edge case if we ever change our minds.
6. **Status indicator: none.** No SwiftBar plugin, no menu-bar app. CLI runbook in `docs/OPERATIONS.md` documents `launchctl print` / `systemctl is-active` checks and start/stop/restart commands instead. Re-openable later if the user wants ambient signal.
7. **Pi-side rollback: indefinite retention.** Pi `stellar-backend.service` unit file stays installed in `/etc/systemd/system/`, just `stop`ped and `disable`d. Pi binary stays at `~eduardo/stellar-backend/stellar` (21 MB, no reason to delete). Re-enabling is a one-liner. The `STELLAR_SPECTRUM_SOURCE=local` codepath in `cmd/stellar/main.go` also stays — never deleted in M1.C — so rolling back is one env flip + one systemd swap.
8. **Frontend serving topology: unchanged.** Kiosk keeps loading from Mac Vite dev server at `http://192.168.86.221:5173/`. Only `config.json`'s `backendUrl` flips Pi:3000 → Mac:3000. Prod build + Caddy/static-serving is Plan B Windows-host territory, not M1.C.
9. **Token rotation: documented in OPERATIONS.md, not automated.** One-line `openssl rand -hex 32 | ssh + launchctl kickstart` sequence per service. No rotation cron, no Vault, no keychain. Three tokens total (lcd / mount / spectrum), each shared between exactly two files (one on Pi, one on Mac).
10. **HTTP-over-LAN, no TLS.** All three Pi services are token-gated HTTP on the home LAN. Matches the existing lcd-control choice. mTLS + self-signed certs add ops burden disproportionate to the single-household-LAN threat model.

## Out of scope (explicit)

The following are deliberately NOT in M1.C, with deferral destinations:

- **Pi-side NAS mount for MPD's `/var/lib/mpd/music`** — unchanged from current Pi state. The new `stellar-mount-control` exposes ad-hoc mounting for the UI's NAS-browser, separate concern from the persistent playback mount. (no phase — sysadmin concern)
- **`paths.DataDir()` adoption for `library.db`** — `server.go:127` + `enrichment_handlers.go:81` keep the `$HOME/stellar-backend/data/...` literal. Migrating means a Pi-side `mv` + code change + test updates. Tech debt flagged in OPERATIONS.md. (M1.G)
- **Mac status indicator** (SwiftBar/menu-bar app) — per decision #6, CLI runbook only. (never, unless re-opened)
- **Frontend off Vite dev server onto prod build** — per decision #8, kiosk-on-Vite is fine. (Plan B / Windows host)
- **`stellar-spectrum` daemon code changes** — daemon shipped in M1.B (commits `951d0a1` + `4c1a572` + `d0d2884`). M1.C only adds its systemd unit + enables it. (M1.B, shipped)
- **Tearing out the in-process FFT from `cmd/stellar`** — `STELLAR_SPECTRUM_SOURCE=local` codepath stays for rollback fidelity. (M1.G or later)
- **MPD systemd unit, fstab, or any Pi OS-level changes** — outside the backend repo. (M1.F)
- **iPhone app (stellar-ios) config change** — user flips backend URL via in-app setting; no code change. (no phase — user setting)
- **`Volumio2-UI/.env` cleanup** (`STELLAR_BACKEND_FOLDER` stale path per memory `reference_stellar_env_stale_backend_path`) — parking lot, unrelated to M1.C. (parking lot)
- **`config.ts` doc-drift** — `?layout=lcd` URL param reference at `Volumio2-UI/src/lib/config.ts:9-16` is stale per memory `feedback_layout_url_param_dead`. Same parking lot. (M1.G)
- **Token-storage migration to Keychain** — env file picked per decision #3. Keychain becomes an option only if the Mac becomes shared or laptop changes. (open, no phase)
- **HTTPS / mTLS on Pi services** — per decision #10, LAN HTTP+bearer is the agreed bar.
- **Re-enable disabled E2E tests** (`vu-meter.spec.ts`, `artists-page.spec.ts`) — NordVPN extension issue per memory `reference_nordvpn_threat_protection_blocks_chromium`. Unrelated. (parking lot)
- **Backend binary size investigation** (+8% from M1.A's new abstraction packages) — doesn't affect cutover. (M1.G or follow-up)
- **Pi binary cleanup** (`stellar-arm64`, `stellar-arm64-cgo`, `stellar-arm64-nocgo` tracked in backend repo root) — parking lot. (independent cleanup)

## Architecture

### Topology

```
┌──────────────────────────────────────┐                ┌────────────────────────────────────┐
│  Mac (Eduardos-Laptop, .221)         │                │  Raspberry Pi 5 (192.168.86.25)    │
│                                      │                │                                    │
│  launchd LaunchAgent                 │                │  systemd                           │
│   ├── caffeinate -dis ./stellar      │                │   ├── mpd.service ─────────► USB → DAC
│   │     ENV ← ~/.config/stellar-     │                │   ├── stellar-spectrum.service ◄── new
│   │           backend/env (0600)     │                │   │     reads /tmp/mpd_spectrum.fifo
│   │     STELLAR_SPECTRUM_SOURCE=     │                │   │     POSTs /internal/spectrum
│   │       remote                     │                │   │     to Mac:3000 (bearer)
│   │     -mpd-host 192.168.86.25      │                │   │
│   │                                  │  WebSocket :3000  ├── lcd-control.service ◄── new install
│   └── Vite dev :5173 (existing)      │◄───────────────►│   │   HTTP :8081 (X-Auth-Token)
│        config.json:                  │                │   │   wraps lcd_on / lcd_off scripts
│        backendUrl=http://.221:3000   │  HTTP :8081     │   │
│                                      │◄───────────────►│   ├── stellar-mount-control.service ◄── new
│  Backend (cross-compiled darwin/arm64)│                │   │   HTTP :8082 (X-Auth-Token)
│   ├── socketio server on :3000       │  HTTP :8082     │   │   wraps mount.cifs + smbclient -L
│   ├── /internal/spectrum ingest      │◄───────────────►│   │
│   ├── lcd.RemoteController ──────────┘                 │   ├── chromium kiosk
│   │   POSTs Pi:8081/api/screen/{on,off}                │   │   loads .221:5173 (unchanged)
│   ├── sources.RemoteMounter ─────────┘                 │   │
│   │   POSTs Pi:8082/api/mount                          │   └── stellar-backend.service ◄── stopped + disabled
│   ├── MPD client → Pi:6600                             │       (unit file + binary retained for rollback)
│   ├── cache.NewDB($HOME/stellar-                       │
│   │   backend/data/library.db) ── fresh, FullBuild()   │
│   └── enrichment goroutine (FANART + WIKI + LLM)       │
└──────────────────────────────────────┘                └────────────────────────────────────┘
```

### Pi-side service shapes

**`lcd-control.service`** — already exists in `Volumio2-UI/pi-kiosk/` (`lcd-control-service.js` + `lcd-control.service`). Deploy via existing `Volumio2-UI/scripts/install-lcd-control.sh`. Endpoints:

- `POST /api/screen/off`
- `POST /api/screen/on`
- `GET /api/screen/status` → `{"on": true|false}`

Auth: `X-Auth-Token` against `/etc/lcd-control/token` (one `openssl rand -hex 32`).

**`stellar-mount-control.service`** — new, mirror of lcd-control's shape. New files:

- `Volumio2-UI/pi-kiosk/mount-control-service.js` (~200 lines Node, same `http.createServer` + token check + endpoint dispatch as lcd-control)
- `Volumio2-UI/pi-kiosk/stellar-mount-control.service` (systemd unit, `User=root` because `mount.cifs` needs root, `ProtectSystem=false`, `PrivateTmp=false`, `Restart=always`)
- `Volumio2-UI/scripts/install-mount-control.sh` (new installer mirroring `install-lcd-control.sh`)

Endpoints:

| Method+Path | Body | Behaviour | Shell call |
|---|---|---|---|
| `GET /api/mount/shares?host=<ip>` | — | List SMB shares on a host | `smbclient -L //<ip> -N` (parse Sharename column) |
| `POST /api/mount` | `{host, share, mountpoint, username, password}` | Mount SMB share | `mount.cifs //<host>/<share> /mnt/NAS/<mountpoint> -o username=<>,password=<>,uid=mpd,gid=audio,iocharset=utf8` |
| `POST /api/mount/unmount` | `{mountpoint}` | Unmount | `umount /mnt/NAS/<mountpoint>` |

Auth: `X-Auth-Token` against `/etc/stellar-mount-control/token`.

**`stellar-spectrum.service`** — new systemd unit wrapping M1.B's existing `cmd/stellar-spectrum` binary. New files:

- `stellar-volumio-audioplayer-backend/deploy/stellar-spectrum.service` (systemd unit, `User=eduardo`, `Group=audio`, `EnvironmentFile=/etc/stellar-spectrum/env`, `Restart=always`)
- `stellar-volumio-audioplayer-backend/deploy/install-stellar-spectrum.sh` (deploys the unit + provisions `/etc/stellar-spectrum/env`)

The `/etc/stellar-spectrum/env` (0600) contains:

```
STELLAR_SPECTRUM_KEY=<shared with Mac ~/.config/stellar-backend/env>
STELLAR_MAC_URL=http://192.168.86.221:3000/internal/spectrum
STELLAR_SPECTRUM_FPS=20
STELLAR_SPECTRUM_NUMBINS=64
```

### Mac-side artefacts

All in new `stellar-volumio-audioplayer-backend/deploy/` directory:

**`deploy/com.stellar.backend.plist`** (LaunchAgent template, IP/user placeholders substituted by installer):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.stellar.backend</string>
  <key>ProgramArguments</key>
  <array><string>/usr/local/bin/stellar-backend-launcher.sh</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key><false/>
    <key>NetworkState</key><true/>
  </dict>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>/Users/eduardomarques/Library/Logs/stellar-backend.out.log</string>
  <key>StandardErrorPath</key><string>/Users/eduardomarques/Library/Logs/stellar-backend.err.log</string>
  <key>WorkingDirectory</key><string>/Users/eduardomarques/stellar-backend</string>
</dict>
</plist>
```

Notable choices: `KeepAlive.SuccessfulExit=false` means restart on crash but honour graceful shutdown; `KeepAlive.NetworkState=true` means don't thrash when LAN is down; `ThrottleInterval=10` caps restart frequency. No `EnvironmentVariables` block — secrets come from the wrapper.

**`deploy/stellar-backend-launcher.sh`** (wrapper, installed to `/usr/local/bin/`):

```bash
#!/bin/bash
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
```

**`deploy/env.example`** (env template, copied to `~/.config/stellar-backend/env` on first install):

```
# --- Spectrum (shared with Pi stellar-spectrum.service) ---
STELLAR_SPECTRUM_SOURCE=remote
STELLAR_SPECTRUM_KEY=<openssl rand -hex 32 — SAME value as Pi /etc/stellar-spectrum/env>

# --- LCD remote control ---
STELLAR_LCD_REMOTE_URL=http://192.168.86.25:8081
STELLAR_LCD_REMOTE_TOKEN=<SAME value as Pi /etc/lcd-control/token>

# --- NAS mount remote control ---
STELLAR_MOUNT_REMOTE_URL=http://192.168.86.25:8082
STELLAR_MOUNT_REMOTE_TOKEN=<SAME value as Pi /etc/stellar-mount-control/token>

# --- Bio + enrichment ---
ANTHROPIC_API_KEY=<copy from ~/.zshrc>
ANTHROPIC_MODEL=claude-haiku-4-5-20251001
FANART_API_KEY=<copy from Volumio2-UI/.env>

# --- Power endpoint trust ---
STELLAR_POWER_TRUSTED_REMOTES=192.168.86.25,192.168.86.221

# --- MPD remote host ---
STELLAR_MPD_HOST=192.168.86.25
STELLAR_MPD_PORT=6600
```

**`deploy/install-mac-backend.sh`** — idempotent installer. First run: creates env template at `~/.config/stellar-backend/env`, exits 0 with a "fill in env then re-run" message. Second run: builds via `make build-darwin`, installs binary, (re)loads LaunchAgent via `launchctl bootstrap` + `kickstart -k`.

### Backend code changes

**`internal/infra/lcd/lcd_remote.go`** (NEW, no build tag):

```go
type RemoteController struct {
    baseURL string         // e.g. "http://192.168.86.25:8081"
    token   string         // X-Auth-Token value
    client  *http.Client   // 2s timeout
}

func NewRemoteController(baseURL, token string) *RemoteController
func (r *RemoteController) Status(ctx context.Context) (Status, error)  // GET  /api/screen/status
func (r *RemoteController) Set(ctx context.Context, on bool) error      // POST /api/screen/{on,off}
```

`lcd_darwin.go` and `lcd_windows.go` `NewPlatform()` body:

```go
func NewPlatform() Controller {
    url := os.Getenv("STELLAR_LCD_REMOTE_URL")
    tok := os.Getenv("STELLAR_LCD_REMOTE_TOKEN")
    if url == "" || tok == "" {
        return &stubController{} // ErrUnsupported, same as M1.A
    }
    return NewRemoteController(url, tok)
}
```

The stub-fallback preserves M1.A's "Mac compiles & boots cleanly without env file" guarantee — `go test ./...` on a fresh dev Mac stays green.

Tests in `lcd_remote_test.go`: `httptest.NewServer` for happy-path, auth-fail (401), timeout, 5xx. Same pattern as `cmd/stellar-spectrum/forwarder_test.go`.

**`internal/domain/sources/mounter_remote.go`** + **`discoverer_remote.go`** (NEW, no build tag) — `RemoteMounter` and `RemoteDiscoverer` implementing the existing `Mounter` and `Discoverer` interfaces via the three mount-control endpoints. Same env-driven `NewPlatform()` pattern in `mounter_darwin.go` / `mounter_windows.go` / `discoverer_darwin.go` / `discoverer_windows.go`.

**M1.A's local darwin sources impls** (`mounter_darwin.go` `mount_smbfs` body, `discoverer_darwin.go` `dns-sd` body) are **deleted in-place**. Reason: section-4b design — keeping dead code for a deprecation window adds reviewer cognition cost for no real safety win. `git revert` covers us if we ever change our mind.

Tests in `mounter_remote_test.go` + `discoverer_remote_test.go`: same `httptest.NewServer` shape.

**Spectrum source flip**: no new code. `cmd/stellar/main.go:260` already reads `STELLAR_SPECTRUM_SOURCE`. Env file sets it to `remote`. Backend opens `/internal/spectrum` ingest, doesn't open the FIFO.

**Power endpoint trusted-remotes**: env file value bumps from current single-IP to `192.168.86.25,192.168.86.221` (Pi + Mac itself, since LCD control may proxy a power command).

**Hardcoded DB path**: unchanged in M1.C (see decision #4 + out-of-scope). `server.go:127` keeps `$HOME/stellar-backend/data/library.db`. On Mac this resolves under `/Users/eduardomarques/stellar-backend/data/` which `install-mac-backend.sh` creates.

### Frontend changes

```diff
# Volumio2-UI/public/config.json
-{"backendUrl": "http://192.168.86.25:3000"}
+{"backendUrl": "http://192.168.86.221:3000"}
```

Plus populate `Volumio2-UI/public/config.json.example` (currently empty) with the same shape + a brief comment about overriding. Vite serves `public/` as-is, no rebuild required.

### Code-change diff summary

| File | Change | Est. lines |
|---|---|---|
| `internal/infra/lcd/lcd_remote.go` | NEW | ~120 |
| `internal/infra/lcd/lcd_remote_test.go` | NEW | ~180 |
| `internal/infra/lcd/lcd_darwin.go` | env-driven NewPlatform | ~15 mod |
| `internal/infra/lcd/lcd_windows.go` | env-driven NewPlatform | ~15 mod |
| `internal/domain/sources/mounter_remote.go` | NEW | ~140 |
| `internal/domain/sources/discoverer_remote.go` | NEW | ~100 |
| `internal/domain/sources/{mounter,discoverer}_remote_test.go` | NEW | ~250 |
| `internal/domain/sources/mounter_darwin.go` | env-driven NewPlatform + delete local impl | -180 / +20 |
| `internal/domain/sources/discoverer_darwin.go` | same | -150 / +20 |
| `Volumio2-UI/public/config.json` | IP flip | 1 |
| `Volumio2-UI/public/config.json.example` | populate | ~5 |
| `Volumio2-UI/pi-kiosk/mount-control-service.js` | NEW | ~200 |
| `Volumio2-UI/pi-kiosk/stellar-mount-control.service` | NEW | ~25 |
| `Volumio2-UI/scripts/install-mount-control.sh` | NEW | ~100 |
| `stellar-volumio-audioplayer-backend/deploy/` (7 files: env.example, com.stellar.backend.plist, stellar-backend-launcher.sh, install-mac-backend.sh, install-stellar-spectrum.sh, stellar-spectrum.service, verify-cutover.sh) | NEW | ~400 |
| `stellar-volumio-audioplayer-backend/docs/OPERATIONS.md` | NEW | ~150 |
| `stellar-volumio-audioplayer-backend/docs/ARCHITECTURE.md` | M1.C section | ~50 mod |

Rough total: **~1850 lines added, ~330 deleted**, across two repos. Comparable to M1.A's footprint.

## Cutover sequence

```
PHASE 1 — Pi-side install (no behaviour change yet)
  1. Deploy lcd-control.service:        sudo bash Volumio2-UI/scripts/install-lcd-control.sh
  2. Deploy stellar-mount-control:      sudo bash Volumio2-UI/scripts/install-mount-control.sh
  3. Deploy stellar-spectrum.service:   sudo bash stellar-volumio-audioplayer-backend/deploy/install-stellar-spectrum.sh
     ... but do NOT enable stellar-spectrum yet (Pi backend still has in-process FFT)
  4. Provision tokens (run on Pi):
       openssl rand -hex 32 | sudo tee /etc/lcd-control/token
       openssl rand -hex 32 | sudo tee /etc/stellar-mount-control/token
       openssl rand -hex 32 | sudo tee -a /etc/stellar-spectrum/env  # prefix with "STELLAR_SPECTRUM_KEY="
  5. Smoke-test lcd-control + mount-control with curl + token (gates G7–G9 below)

PHASE 2 — Mac-side install (no behaviour change yet — Pi still serves clients)
  6. cd stellar-volumio-audioplayer-backend && bash deploy/install-mac-backend.sh
     ... first run creates env template at ~/.config/stellar-backend/env, exits 0
  7. User fills in env (paste tokens from PHASE 1 step 4, paste ANTHROPIC_API_KEY, FANART_API_KEY)
  8. Re-run: bash deploy/install-mac-backend.sh
     ... builds binary via make build-darwin, loads LaunchAgent, backend starts on Mac :3000
     ... BUT: kiosk + iPhone still point at Pi :3000, so Mac backend has zero clients yet
  9. Verify Mac backend is healthy (gates G1–G6 below)

PHASE 3 — The actual flip (60-second window)
 10. Edit Volumio2-UI/public/config.json: backendUrl Pi:3000 → Mac:3000
 11. Hard-reload kiosk via SSH+CDP (per memory `reference_pi_chromium_cdp_reload`)
 12. On Pi:  sudo systemctl stop stellar-backend
             sudo systemctl disable stellar-backend       # keep unit file + binary for rollback
             sudo systemctl enable --now stellar-spectrum
 13. Kiosk should now show Mac-backed UI with spectrum bars flowing Pi daemon → Mac ingest → Socket.IO broadcast

PHASE 4 — Verification (smoke matrix S1–S15 + done-gates below)
```

The window between step 11 and step 12 is the only risky moment: kiosk reload uses new `config.json` so it picks Mac, but Pi backend is still emitting. Mac wins because the kiosk's Socket.IO connection points at the Mac URL. No split-brain — clients on a single URL see a single backend.

## Rollback

Single-command, ~30 seconds:

```bash
# On Mac
launchctl bootout gui/$(id -u)/com.stellar.backend

# Revert config.json on the frontend repo
cd ~/workspace/stellar-streamer/Volumio2-UI
git checkout HEAD~1 -- public/config.json   # or manual edit Mac IP → Pi IP

# On Pi
sudo systemctl stop stellar-spectrum
sudo systemctl disable stellar-spectrum
sudo systemctl enable --now stellar-backend

# Reload kiosk via CDP
```

Retention: indefinite. Pi binary + unit stay; Mac binary + LaunchAgent stay (just `bootout`ed). Re-cutover after rollback is a `launchctl bootstrap` + Pi service swap.

## Falsifiable success gates

### G1–G6 — Mac-side pre-cutover (after PHASE 2, before flipping config.json)

```bash
# G1 — Backend cross-compiled clean for darwin/arm64 (M1.A guarantee preserved)
strings ~/stellar-backend/stellar | grep -E '/(proc|sys|mnt)/' && echo FAIL || echo PASS
nm ~/stellar-backend/stellar | grep -E 'wlr_randr|nmcli|mount\.cifs' && echo FAIL || echo PASS

# G2 — LaunchAgent loaded and process alive
launchctl print gui/$(id -u)/com.stellar.backend | grep -q 'state = running' && echo PASS || echo FAIL

# G3 — Env file perms tight
test "$(stat -f '%Lp' ~/.config/stellar-backend/env)" = "600" && echo PASS || echo FAIL

# G4 — Backend listening on :3000 + responding to /api/v1/getState
curl -fsS --max-time 2 http://localhost:3000/api/v1/getState | jq -e '.status' >/dev/null && echo PASS || echo FAIL

# G5 — MPD reachable from Mac over LAN to Pi
nc -zv 192.168.86.25 6600 2>&1 | grep -q succeeded && echo PASS || echo FAIL

# G6 — /internal/spectrum endpoint requires bearer (no key = 401)
test "$(curl -fsS -o /dev/null -w '%{http_code}' -X POST http://localhost:3000/internal/spectrum)" = "401" && echo PASS || echo FAIL
```

### G7–G9 — Pi-side pre-cutover (after PHASE 1)

```bash
# G7 — All three new Pi services active (stellar-spectrum installed but not yet enabled — that flips in PHASE 3)
test "$(ssh eduardo@192.168.86.25 'systemctl is-active lcd-control stellar-mount-control' | grep -cv '^active$')" = "0" && echo PASS || echo FAIL

# G8 — LCD control responds with token, refuses without
TOK=$(ssh eduardo@192.168.86.25 'sudo cat /etc/lcd-control/token')
curl -fsS http://192.168.86.25:8081/api/screen/status -H "X-Auth-Token: $TOK" | jq -e '.on' >/dev/null && echo PASS || echo FAIL
test "$(curl -fsS -o /dev/null -w '%{http_code}' http://192.168.86.25:8081/api/screen/status)" = "401" && echo PASS || echo FAIL

# G9 — Mount control responds with token, refuses without
TOK=$(ssh eduardo@192.168.86.25 'sudo cat /etc/stellar-mount-control/token')
curl -fsS "http://192.168.86.25:8082/api/mount/shares?host=192.168.86.26" -H "X-Auth-Token: $TOK" >/dev/null && echo PASS || echo FAIL
test "$(curl -fsS -o /dev/null -w '%{http_code}' http://192.168.86.25:8082/api/mount/shares)" = "401" && echo PASS || echo FAIL
```

### S1–S15 — Post-cutover end-to-end smoke matrix (after PHASE 3)

Run from a kiosk-equivalent browser pointed at `http://192.168.86.221:5173/`. Each row is a manual click-through with logs tailed.

| # | Action | Expected | Backend log signal |
|---|---|---|---|
| S1 | Page load | UI renders, Socket.IO connects to .221:3000 | `INF Socket connected` with kiosk UA |
| S2 | Play any track | MPD on Pi starts, audio out of DAC | `INF MPD state: play` from Mac backend |
| S3 | Pause / resume | Transport responds <100ms | `INF MPD state: pause/play` |
| S4 | Tap LCD-off tile | Pi LCD goes dark | Mac: `INF lcd: POST .25:8081/api/screen/off → 200` |
| S5 | Tap LCD-on tile | Pi LCD comes back | Mac mirror `→ 200` |
| S6 | Browse Library → Artists | Tiles render (mostly default-album.svg first hour) | `INF library:cache:updated` after FullBuild |
| S7 | Add to queue, reorder | Queue updates everywhere | `INF queue updated` |
| S8 | Open NAS browser, list shares on 192.168.86.26 | Real share names appear | Mac: `GET .25:8082/api/mount/shares?host=… → 200`; Pi journal shows `smbclient -L` exec |
| S9 | (Optional) Mount + unmount one share | mount.cifs runs on Pi | Mac `→ 200`; Pi journal shows `mount.cifs` exec |
| S10 | VU meter view, play loud passage | Bars react with L/R asymmetry | Mac: `INF /internal/spectrum: 20 frames/s` |
| S11 | iPhone app connects | Same UI works | Two concurrent Socket connections in log |
| S12 | Kill Pi spectrum daemon, wait 5s | VU bars freeze, no Mac crash | Backend: ingest backoff retries, no panic |
| S13 | Restart Pi spectrum daemon | VU bars resume within 10s | Backend: `INF /internal/spectrum: resumed` |
| S14 | Lock Mac screen 5 min, unlock | Backend still up, still serving | `launchctl print` still `state = running`; caffeinate honoured |
| S15 | Pi reboot | Pi services return, kiosk reloads, Mac doesn't crash | Mac: MPD reconnect backoff, ingest stalls then resumes |

S15 is specifically there because we saw an unplanned Pi reboot at the start of this very session — validates the failure mode is non-fatal.

### Done-gates — "M1.C is complete"

All G1–G9 PASS, all S1–S15 PASS, AND:

```bash
# Pi backend permanently disabled
ssh eduardo@192.168.86.25 'systemctl is-enabled stellar-backend' | grep -q disabled && echo PASS || echo FAIL

# Pi spectrum permanently enabled
ssh eduardo@192.168.86.25 'systemctl is-enabled stellar-spectrum' | grep -q enabled && echo PASS || echo FAIL

# Mac backend autostart on login
launchctl print-disabled gui/$(id -u) | grep -q '"com.stellar.backend" => disabled = false' && echo PASS || echo FAIL
```

Plus a cold-reboot survival check: physically power-cycle both machines, log in, observe the topology re-converges without manual intervention.

A small `deploy/verify-cutover.sh` script wraps G1–G9 + the three done-gates as a single command.

## CLI runbook (lives in `docs/OPERATIONS.md`)

### Mac backend ops

```bash
# Is it running?
launchctl print gui/$(id -u)/com.stellar.backend | grep -E 'state|last exit code'
curl -fsS http://localhost:3000/api/v1/getState >/dev/null && echo UP || echo DOWN

# Start / Stop / Restart
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.stellar.backend.plist
launchctl bootout    gui/$(id -u)/com.stellar.backend
launchctl kickstart -k gui/$(id -u)/com.stellar.backend

# Tail logs
tail -f ~/Library/Logs/stellar-backend.out.log
tail -f ~/Library/Logs/stellar-backend.err.log

# Redeploy after binary rebuild
cd ~/workspace/stellar-streamer/stellar-volumio-audioplayer-backend
make build-darwin
install -m 755 bin/stellar-darwin-arm64 ~/stellar-backend/stellar
launchctl kickstart -k gui/$(id -u)/com.stellar.backend
```

### Pi services ops (via ssh)

```bash
# Status (one-liner)
sudo systemctl status lcd-control stellar-mount-control stellar-spectrum --no-pager

# Individual control
sudo systemctl {start|stop|restart} <service>

# Tail logs
sudo journalctl -u stellar-spectrum -f
sudo journalctl -u lcd-control -f
sudo journalctl -u stellar-mount-control -f

# Rollback to Pi-resident backend
sudo systemctl stop stellar-spectrum && sudo systemctl disable stellar-spectrum
sudo systemctl enable --now stellar-backend

# Health checks
curl -fsS http://192.168.86.25:8081/api/screen/status -H "X-Auth-Token: $(ssh eduardo@192.168.86.25 sudo cat /etc/lcd-control/token)"
```

### Token rotation

```bash
NEW=$(openssl rand -hex 32)
ssh eduardo@192.168.86.25 "echo $NEW | sudo tee /etc/<service>/token && sudo systemctl restart <service>"
# Manually edit ~/.config/stellar-backend/env on Mac with same value
launchctl kickstart -k gui/$(id -u)/com.stellar.backend
```

## Execution shape

Subagent-driven per the M1.A precedent. 7 commit waves (each wave gets implementer + code-reviewer + spec-compliance reviewer):

1. `deploy/` skeleton — env.example, plist, launcher, install-mac-backend.sh, install-stellar-spectrum.sh, stellar-spectrum.service, verify-cutover.sh (backend repo)
2. `lcd_remote.go` + tests + darwin/windows `NewPlatform()` (backend repo)
3. `mount-control-service.js` + stellar-mount-control.service + install-mount-control.sh (Volumio2-UI repo)
4. `mounter_remote.go` + `discoverer_remote.go` + tests + darwin/windows `NewPlatform()` + delete dead M1.A local darwin impls (backend repo)
5. `OPERATIONS.md` + `ARCHITECTURE.md` M1.C section (backend repo)
6. `public/config.json` flip + `.example` populate (Volumio2-UI repo)
7. Apply the cutover sequence + run verify gates + post-cutover docs touch-ups

Waves 1–6 land before the actual cutover (Pi backend still serving). Wave 7 IS the cutover.

## Non-goals reminder

This phase **does** materially reduce Pi load — that's its entire point. But it does NOT close M1 by itself. M1.D (cache pre-build verification) lands once the Mac backend has been stable a few days. M1.F (Pi OS audit) can run anytime — independent. M1.G (docs reconciliation) closes M1, picks up the parking-lot items M1.C leaves behind (paths.DataDir adoption, in-process FFT removal, etc.).
