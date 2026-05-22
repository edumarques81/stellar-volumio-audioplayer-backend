# M1.E — Read-handler proxy through Pi (Settings page accuracy)

_Date: 2026-05-23 · Spec for the read-handler proxy phase of the linear-blanket initiative_

## Goal / WHY

After M1.C the backend lives on the Mac, but six Socket.IO read handlers still execute against the Mac host instead of the Pi audio appliance. The Settings page on the kiosk consequently reports the **Mac's** hostname, network interface, audio engine config, and DSD/mixer mode — none of which describes the appliance the user is actually listening to. Today's behaviour is **silently misleading**: the field is populated, but with the wrong data.

M1.E proxies those six reads through the existing `stellar-mount-control.service` (the same Pi-side HTTP surface M1.D wired up for `shutdown`/`reboot`) so the Settings tab reflects Pi reality. When the Pi is unreachable, the proxy returns zero-value payloads and the Settings tab renders "—" instead of fabricating Mac data.

This is the smallest delta that closes the M1.C cutover's user-visible truth gap. It is **not** a frontend redesign, a write-path proxy, or a debugging exercise for the three Pi-frontend regressions captured in [M1.F](#parked-followups).

## User-locked decisions (this brainstorm)

1. **Scope: six READ endpoints.** `getSystemInfo`, `getDeviceInfo`, `getNetworkStatus`, `getBitPerfect`, `getDsdMode`, `getMixerMode`. `getAudioStatus` was initially in scope but dropped after re-reading `internal/audio/controller.go:56-84` — it is derived entirely from MPD's `audio` string and already correct on the Mac because MPD-over-network reaches the Pi. Audio-config **writes** (`setDsdMode`, `setMixerMode`, `applyBitPerfect`) are parked as a separate phase because the read fix is independently shippable and the writes need their own privilege-escalation thinking on the Pi.
2. **Cadence: request-only.** The existing 30-second `pushNetworkStatus` watcher (`internal/infra/netinfo/`) is **disabled when remote info is active**, so we don't blast the Pi with periodic GETs. Frontend pulls on Settings-tab mount and reconnect — which already happens — and that is sufficient for a kiosk Pi on Ethernet.
3. **Pi-unreachable behaviour: honest empty payload.** Mac emits the `pushXxx` event with zero-value fields and logs `Warn`. No retries, no caching, no last-known-good fallback. The Settings page already renders empty fields as "—", which is the truth-telling UX we want (the rest of the app will be visibly broken anyway if the Pi is unreachable).
4. **Identity: runtime proxy, not install-time migration.** `getSystemInfo` and `getDeviceInfo` come from the Pi every request, not from an installer-copied `device.json` on the Mac. Keeps the architectural pattern consistent with the other four reads; avoids a "Mac identity stays stale if Pi identity rotates" footgun.
5. **Architecture: extend `stellar-mount-control.service` with six new GET endpoints.** No new Pi service, no new systemd unit, no new install script, no new token. Reuses the existing `STELLAR_MOUNT_REMOTE_URL` + `STELLAR_MOUNT_REMOTE_TOKEN` env vars wired in M1.D. Mount-control is now explicitly the "Pi RPC surface" by usage; making that explicit costs nothing. (Approach B — separate `stellar-info.service` on :8083 — was rejected as plumbing-heavy for theoretical isolation. Approach C — full Pi-side Go daemon — is an M3+ direction.)
6. **Mac-side client: one shared `RemoteInfoClient` with six methods.** ~140 lines in `internal/transport/socketio/remote_info.go`. Mild deviation from the M1.A "one `Remote*` type per domain" precedent (M1.A had `RemoteController` / `RemoteMounter` / `RemoteDiscoverer`, M1.D added `RemoteSystemActions`), accepted because these six reads share the same shape (GET + token + JSON decode) and the same destination (Pi:8082). One file consolidates the HTTP wiring instead of duplicating it six times.

## Out of scope (explicit)

- **Audio-config writes** (`setDsdMode`, `setMixerMode`, `applyBitPerfect`) — these `os.WriteFile`/`sudo tee` against `/etc/mpd.conf` on the host; broken on Mac since M1.C. Parked as **M1.E.1 audio-config write proxy**.
- **Pi-frontend regressions (M1.F):**
  1. LCD not updating between tracks (pushState event reachability or store reactivity).
  2. Right nav-column Refresh button spins forever.
  3. Settings page LCD on/off switch doesn't work (regression in M1.A's `RemoteController` write path).
  All three are **bug investigations** routed through `superpowers:systematic-debugging`, not feature design. Should ship **before** M1.E because the kiosk is visibly broken in user-facing ways today.
- **MPD-watcher panic** (`internal/infra/mpd/client.go:404` nil-deref when Pi disappears) — pre-existing, surfaced by M1.D reboot test; manual restart required after every Pi reboot until fixed. Independent fix, separate phase.
- **30s network watcher in remote mode** — disabled per decision #2. The watcher infrastructure stays in `internal/infra/netinfo/` for the Linux/Pi-resident build path (still useful if someone ever runs the backend on the Pi for development) but doesn't start when `remoteInfo != nil`.
- **OPERATIONS.md restart drift** — current doc says `launchctl kickstart` for restarts, but the LaunchAgent is permanently disabled per the NordVPN-ES filter workaround. New Mac topology uses `~/bin/stellar-restart.sh` (shell-spawned). Parked.
- **Schema migrations**, **bias toward more endpoints** — keep the surface tight; six is enough to close the Settings-tab truth gap.

## Architecture

### Topology delta from M1.C

```
┌──────────────────────────────────────┐                ┌──────────────────────────────────────┐
│  Mac (.221)                          │                │  Pi (192.168.86.25)                  │
│                                      │                │                                      │
│  stellar (cross-compiled darwin/arm64)│                │  stellar-mount-control.service       │
│                                      │                │  (Node, :8082, X-Auth-Token)         │
│  ── socketio.Server                  │                │  existing 9 endpoints:               │
│     ├── getSystemInfo                │                │   /api/mount/{shares,devices,mount,  │
│     │     ├── if remote: GET ────────┼───────────────►│    unmount,is-mounted,mountpoint,    │
│     │     └── else: GetSystemInfo()  │                │    symlink}                          │
│     ├── getDeviceInfo                │                │   /api/system/{shutdown,reboot}      │
│     │     ├── if remote: GET ────────┼───────────────►│                                      │
│     │     └── else: deviceService    │                │  NEW 6 endpoints (this phase):       │
│     ├── getNetworkStatus             │                │   GET /api/system/info               │
│     │     ├── if remote: GET ────────┼───────────────►│   GET /api/system/device             │
│     │     └── else: netReporter      │                │   GET /api/network/status            │
│     ├── getBitPerfect                │                │   GET /api/audio/bitperfect          │
│     │     ├── if remote: GET ────────┼───────────────►│   GET /api/audio/dsd                 │
│     │     └── else: audio_config     │                │   GET /api/audio/mixer               │
│     ├── getDsdMode  ──────────────────┼──┘            │                                      │
│     └── getMixerMode ─────────────────┼──┘            │  Same X-Auth-Token, same port,       │
│                                      │                │  same systemd unit, same             │
│  ── RemoteInfoClient (NEW)           │                │  /etc/stellar-mount-control/token    │
│       baseURL/token from env         │                │                                      │
│       6 methods, 5s timeout          │                │                                      │
│                                      │                │                                      │
│  ── netinfo 30s watcher              │                │                                      │
│       disabled if remoteInfo != nil  │                │                                      │
└──────────────────────────────────────┘                └──────────────────────────────────────┘
```

### Pi-side endpoints (six new in `mount-control-service.js`)

All require `X-Auth-Token` (existing token, no rotation). All return `application/json`. All wrap shell-out in try/catch returning `{ "error": "...", "code": "..." }` with HTTP 500 on failure, HTTP 401 on missing/wrong token, HTTP 200 with payload on success. Estimated added size: ~150 lines on top of the current 326-line file.

#### `GET /api/system/info` → mirrors `socketio.SystemInfo`

```json
{
  "id": "stellar.local",
  "host": "stellar.local",
  "name": "stellar.local",
  "type": "audio_player",
  "serviceName": "stellar",
  "hardware": "Raspberry Pi 5 Model B Rev 1.0",
  "variant": "stellar-pi"
}
```

Shell: `hostname` + `cat /proc/device-tree/model`. The two binary-version fields (`systemversion`, `builddate`) stay Mac-side and are merged in by the Mac handler — those describe the running backend binary, not the Pi.

#### `GET /api/system/device` → `pushDeviceInfo` payload

```json
{ "uuid": "<from /etc/machine-id>", "name": "<from hostname>" }
```

Shell: `cat /etc/machine-id` (persisted across boots) + `hostname`. Falls back to `hostname` for both if `/etc/machine-id` is missing.

#### `GET /api/network/status` → `netinfo.Status`

```json
{ "type": "ethernet" | "wifi", "ip": "192.168.86.25", "ssid": "", "strength": 0, "interface": "eth0" }
```

Shell: `ip -j route get 1.1.1.1` to pick the default-route interface, `ip -j addr show dev <iface>` for IP, plus `iwgetid` + `iwconfig <iface>` only when the interface is wireless. JSON-parse the `ip -j` output server-side.

#### `GET /api/audio/bitperfect` → `BitPerfectStatus`

The existing local handler reads `/etc/mpd.conf` + alsa config + parses `aplay -l` output. The Pi endpoint runs the same algorithm against the Pi's `/etc/mpd.conf` / alsa / `aplay -l`. Returning the same JSON shape the Mac handler uses today so the frontend store changes nothing.

#### `GET /api/audio/dsd` → `DsdModeResponse`

Reads `dsd_usb` / `dsd_native` keys from `/etc/mpd.conf` on the Pi. Returns the same envelope the Mac handler returns (success/error fields).

#### `GET /api/audio/mixer` → `MixerModeResponse`

Reads `mixer_type` from `/etc/mpd.conf` on the Pi. Returns `{ enabled: bool, type: "software"|"none" }` etc., matching the Mac handler's existing shape.

### Mac-side client (one shared type)

`internal/transport/socketio/remote_info.go` (NEW, ~140 lines):

```go
type RemoteInfoClient struct {
    baseURL string
    token   string
    client  *http.Client  // 5s timeout, matches RemoteSystemActions
}

func NewRemoteInfoClient(baseURL, token string) *RemoteInfoClient { ... }
func NewRemoteInfoClientWithClient(baseURL, token string, c *http.Client) *RemoteInfoClient { ... }

func (r *RemoteInfoClient) SystemInfo()    (SystemInfo, error)        // GET /api/system/info
func (r *RemoteInfoClient) DeviceInfo()    (device.DeviceInfo, error) // GET /api/system/device
func (r *RemoteInfoClient) NetworkStatus() (netinfo.Status, error)    // GET /api/network/status
func (r *RemoteInfoClient) BitPerfect()    (BitPerfectStatus, error)   // GET /api/audio/bitperfect
func (r *RemoteInfoClient) DsdMode()       (DsdModeResponse, error)   // GET /api/audio/dsd
func (r *RemoteInfoClient) MixerMode()     (MixerModeResponse, error) // GET /api/audio/mixer

// Shared helper: builds req, sets X-Auth-Token, decodes JSON, wraps err
func (r *RemoteInfoClient) get(path string, dst any) error
```

For testability the call sites take an interface (`RemoteInfoReader`) rather than the concrete type, so server tests can stub it.

### Env wiring (`cmd/stellar/main.go`)

Same block that already constructs `RemoteSystemActions`:

```go
remoteURL   := os.Getenv("STELLAR_MOUNT_REMOTE_URL")
remoteToken := os.Getenv("STELLAR_MOUNT_REMOTE_TOKEN")
if remoteURL != "" && remoteToken != "" {
    infoClient := socketio.NewRemoteInfoClient(remoteURL, remoteToken)
    server.UseRemoteInfo(infoClient)
}
```

When `STELLAR_MOUNT_REMOTE_URL` + `_TOKEN` are unset (Linux Pi-resident build), `RemoteInfoClient` is never constructed and all six handlers fall through to their existing local impls. No new env vars are introduced — the M1.D pair is reused.

### Call-site changes (six handlers, all in `internal/transport/socketio/`)

| Handler | Existing local impl | New remote branch |
|---|---|---|
| `getSystemInfo` (server.go:647) | `GetSystemInfo()` | `s.remoteInfo.SystemInfo()` |
| `getDeviceInfo` (volumio_handlers.go:53) | `h.deviceService.GetDeviceInfo()` | `s.remoteInfo.DeviceInfo()` |
| `getNetworkStatus` (server.go:602) | `s.netReporter.GetStatus()` | `s.remoteInfo.NetworkStatus()` |
| `getBitPerfect` (audio_config.go) | reads `/etc/mpd.conf` + alsa + `aplay -l` | `s.remoteInfo.BitPerfect()` |
| `getDsdMode` (audio_config.go) | reads `/etc/mpd.conf` | `s.remoteInfo.DsdMode()` |
| `getMixerMode` (audio_config.go) | reads `/etc/mpd.conf` | `s.remoteInfo.MixerMode()` |

Each handler becomes a two-line `if s.remoteInfo != nil` / `else` branch. No new abstractions, no interface hierarchy on the local-impl side — the existing free functions stay.

### Netinfo watcher disable

`internal/infra/netinfo/netinfo.go` adds one guard: the 30s ticker only starts when no remote-info reader has been wired. On Linux Pi-resident builds the watcher runs as today; on remote Mac/Windows builds the watcher is silent and the frontend pulls `getNetworkStatus` on its own cadence (Settings tab mount + Socket.IO reconnect).

## Data flow

```
Frontend  ─emit('getNetworkStatus')─► Mac socketio.Server handler
                                      ├── if s.remoteInfo != nil:
                                      │     status, err := s.remoteInfo.NetworkStatus()
                                      │     ├── on err → log.Warn + emit zero-value
                                      │     └── on ok  → emit status
                                      └── else: status := s.netReporter.GetStatus(); emit status
```

Identical pattern for the other five handlers. Single round-trip per request, no batching, no fan-out.

## Error handling

| Failure | `RemoteInfoClient` returns | Server emits to frontend | Log |
|---|---|---|---|
| Pi unreachable (timeout / conn refused) | `(zero, err)` after 5s | `pushXxx` with zero-value payload | `Warn` with `path` |
| HTTP 401 (token drift) | `(zero, err)` | same | `Warn` with `path`, `status=401` |
| HTTP 5xx (Pi shell-out failed) | `(zero, err)` | same | `Warn` with `path`, `status=5xx` |
| HTTP 200 + JSON decode fails | `(zero, err)` | same | `Warn` with `path`, `decode_err` |

No retries. No client-side caching. No `pushError` event — Settings already renders "—" for empty fields. Errors are greppable in `~/Library/Logs/stellar-backend.err.log` by `path=/api/...`.

**Pi-side error envelope** (mount-control endpoints): `{ "error": "...", "code": "..." }` with HTTP 500 on shell-out failure, HTTP 401 on missing/wrong token. Matches the existing mount-endpoint envelope.

## Testing

### Mac-side (Go, `testing` + `httptest`)

- **`remote_info_test.go`** — table-driven test exercising all six methods × five failure modes (happy, 401, 500, conn-refused, JSON-decode-fail). ~30 sub-tests, ~180 lines. Assert: returned `err` is non-nil on failures and value is zero-valued.
- **`server_remote_info_test.go`** — for each of the six socket handlers, spin a Server with a stub `RemoteInfoReader`, emit the `get*` event, assert the `push*` payload. Covers both "remote success → push" and "remote error → push zero-value" branches. Uses the same fake-socket pattern as `system_actions_test.go`.
- **Existing local-impl tests** stay unchanged. They run under the `remoteInfo == nil` branch.

### Pi-side (Node, smoke)

- **`Volumio2-UI/scripts/smoke-mount-control-info.sh`** — curls all six new endpoints with the deployed token, validates HTTP 200 + JSON shape via `jq`. Invoked from `verify-cutover.sh` post-deploy. Returns non-zero on any endpoint failure.
- Existing `mount-control-service.js` has no test suite today; this spec **does not** introduce one — adding Jest/Mocha for six handlers is out of proportion. Smoke + manual kiosk verification is the bar.

### End-to-end (manual, kiosk)

1. Open kiosk Settings → System tab. Confirm hostname / UUID / network IP show **Pi** values (not Mac values).
2. Settings → Audio tab. Confirm bit-perfect / DSD / mixer reflect Pi's `/etc/mpd.conf`.
3. `sudo systemctl stop stellar-mount-control` on Pi. Settings tab refresh → all six fields render "—", no UI crash. Restart the service.

## Implementation order (rough — `writing-plans` will refine)

1. **Pi side** — add the six GET handlers to `mount-control-service.js`. Deploy + curl-test each. Add `smoke-mount-control-info.sh`.
2. **Mac side — client** — `remote_info.go` + `remote_info_test.go`. Confirm `go test -race` green.
3. **Mac side — server wiring** — six call-site branches + `server_remote_info_test.go` + watcher disable.
4. **Integration** — set env, restart Mac backend, open kiosk Settings, walk the manual E2E list.
5. **Followups capture** — append M1.E.1 audio-write proxy + OPERATIONS.md restart drift to project note.

## Parked followups

- **M1.E.1 — audio-config write proxy.** `setDsdMode`, `setMixerMode`, `applyBitPerfect` need a write path through mount-control with the same token. Privilege model TBD (mount-control runs `User=root` already, so the `sudo tee /etc/mpd.conf` from `audio_config.go:484` can move server-side without escalation).
- **M1.F — Pi-frontend regressions.** Three items, all via `superpowers:systematic-debugging`:
  1. LCD not updating between tracks.
  2. Right nav-column Refresh button spins forever.
  3. Settings page LCD on/off switch doesn't work.
- **MPD-watcher panic fix.** `internal/infra/mpd/client.go:404` nil-deref when Pi disappears mid-run. Needed because the Mac backend currently dies on every Pi reboot.
- **OPERATIONS.md restart-procedure drift.** Doc says `launchctl kickstart` but LaunchAgent is permanently disabled per the NordVPN-ES filter workaround. Update to document `~/bin/stellar-restart.sh` (shell-spawned).
- **M1.G docs.** Closes M1 — captures the Mac topology, the proxy patterns, the migration path to Windows/Linux.

## Cross-references

- M1.A spec: `docs/superpowers/specs/2026-05-13-m1a-backend-portability-design.md`
- M1.C spec: `docs/superpowers/specs/2026-05-19-m1c-cutover-design.md`
- M1.C plan: `docs/superpowers/plans/2026-05-19-m1c-cutover-plan.md`
- M1.D shipped: backend commits `3374222` + frontend commit (PowerModal event-name change), both unpushed at time of writing.
- `RemoteSystemActions` (M1.D precedent for env wiring + 5s timeout): `internal/transport/socketio/system_actions_remote.go`
- `RemoteController` (M1.A precedent for `Remote*`-per-domain): `internal/infra/lcd/lcd_remote.go`
- `RemoteMounter` / `RemoteDiscoverer` (M1.C precedent for HTTP-bearer Pi RPC): `internal/domain/sources/{mounter,discoverer}_remote.go`
- Memory `feedback_subagent_driven_execution` — expect `/clear` between phases during execution.
- Memory `reference_mpd_watcher_panic_on_target_loss` — Mac stellar dies on Pi reboot until the panic fix lands.
