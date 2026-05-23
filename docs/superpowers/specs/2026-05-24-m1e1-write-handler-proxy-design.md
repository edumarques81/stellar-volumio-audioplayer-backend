# M1.E.1 — Write-handler proxy through Pi (audio config writes)

_Date: 2026-05-24 · Spec for the write-handler proxy phase of the linear-blanket initiative_

## Goal

M1.E proxied six READ handlers through the Pi-resident `stellar-mount-control.service` so the Settings page reflects Pi appliance state instead of Mac host state. Three WRITE handlers were deliberately deferred: `setDsdMode`, `setMixerMode`, and `applyBitPerfect`. On the Mac, these handlers currently call `SetDsdMode()` / `SetMixerMode()` / `ApplyBitPerfect()` from `audio_config.go`, which shell out `sudo tee /etc/mpd.conf` and `sudo systemctl restart mpd` — both of which operate on the Mac filesystem and process table, not the Pi's. The writes silently succeed on the Mac but change nothing on the audio appliance.

M1.E.1 closes that gap by adding three POST endpoints to the Pi service, extending `RemoteInfoClient` with matching setter methods, and branching the three Mac socket handlers through the proxy when `STELLAR_MOUNT_REMOTE_URL` + `_TOKEN` are set. Post-write broadcasts are rerouted through the existing M1.E read helpers (`s.dsdMode()`, `s.mixerMode()`, `s.bitPerfect()`) so all connected clients receive Pi-truth after a write.

## Topology recap

Full topology is documented in the M1.E spec (`docs/superpowers/specs/2026-05-23-m1e-read-handler-proxy-design.md`). In summary: the Mac runs the `stellar` backend binary; the Pi runs `stellar-mount-control.service` (Node.js, `:8082`, `X-Auth-Token`). M1.E wired the read side; M1.E.1 wires the write side using the same port, same token, same env vars, and same service — no new systemd units, no new credentials.

The service runs as `User=root Group=root` (confirmed: `/etc/systemd/system/stellar-mount-control.service` line 9-10). This means the Pi-side POST handlers can write `/etc/mpd.conf` and call `systemctl restart mpd` directly — no `sudo` escalation required inside the handler code.

```
setDsdMode (socket)         POST /api/audio/dsd
setMixerMode (socket)  ──►  POST /api/audio/mixer        → Pi mount-control (:8082)
applyBitPerfect (socket)    POST /api/audio/bitperfect/apply
                            (writes /etc/mpd.conf, restarts mpd)
```

## Decisions

### Decision 1 — Pi endpoints: three new POST routes

Three new handlers in `Volumio2-UI/pi-kiosk/mount-control-service.js`, dispatched from the existing router:

| Route | Method | Body | Returns |
|---|---|---|---|
| `/api/audio/dsd` | POST | `{ "mode": "native" \| "dop" }` | `DsdModeResponse` |
| `/api/audio/mixer` | POST | `{ "enabled": true \| false }` | `MixerModeResponse` |
| `/api/audio/bitperfect/apply` | POST | _(no body)_ | `ApplyBitPerfectResponse` |

Auth: the existing `checkAuth(req)` gate already applies to every route. No per-handler auth logic needed. Token: same `/etc/stellar-mount-control/token` as M1.E.

The POST body for `dsd` and `mixer` is parsed via the existing `parseBody(req)` helper already used by mount/unmount routes. `bitperfect/apply` reads no body — any `parseBody` call is omitted.

The handlers run as root (service constraint), so `fs.writeFile('/etc/mpd.conf', ...)` and `exec('systemctl restart mpd', ...)` succeed without escalation. The `sudo` keyword that appears in the Go `writeMPDConfig` function (`audio_config.go:484`) is not needed in the Node handlers.

**Rationale:** Three symmetrical POST routes on the same service and port as the six GET routes from M1.E. No new service, no new port, no new token. Consistent with the M1.E precedent of extending `stellar-mount-control` as the "Pi RPC surface."

### Decision 2 — Mac client: extend `RemoteInfoClient` with setter methods

`RemoteInfoClient` in `internal/transport/socketio/remote_info.go` gains three new methods and the `RemoteInfoReader` interface is renamed and broadened to `RemoteAudioClient`. The concrete struct is also renamed from `RemoteInfoClient` to `RemoteAudioClient`.

The rename is justified because "Info" in `RemoteInfoClient` no longer accurately describes the type after M1.E.1: the type now handles both reads (six GET methods) and writes (three POST methods). Keeping the old name would be misleading. The rename affects:

- `internal/transport/socketio/remote_info.go`: rename type + constructor functions
- `internal/transport/socketio/remote_info_test.go`: rename test references
- `internal/transport/socketio/server_remote_info_test.go`: rename `fakeRemoteInfo` + field
- `internal/transport/socketio/server.go`: rename field type + `UseRemoteInfo` parameter
- `cmd/stellar/main.go`: rename constructor call

The interface (`RemoteInfoReader` → `RemoteAudioClient`) gains three write methods:

```go
type RemoteAudioClient interface {
    // Reads (from M1.E)
    SystemInfo()    (SystemInfo, error)
    DeviceInfo()    (device.DeviceInfo, error)
    NetworkStatus() (netinfo.Status, error)
    BitPerfect()    (BitPerfectStatus, error)
    DsdMode()       (DsdModeResponse, error)
    MixerMode()     (MixerModeResponse, error)

    // Writes (M1.E.1)
    SetDsdMode(mode string)  (DsdModeResponse, error)
    SetMixerMode(enabled bool) (MixerModeResponse, error)
    ApplyBitPerfect()        (ApplyBitPerfectResponse, error)
}
```

The existing `get(path string, dst any)` helper on the struct is joined by a `post(path string, body any, dst any)` helper that JSON-encodes the body, sets `Content-Type: application/json`, and decodes the response — same error-wrapping pattern.

**Rationale:** Extending `RemoteInfoClient` keeps all Pi-RPC wiring in one file and one type. The "Info" name was a narrowing that M1.E accepted as a concession ("mild deviation from the M1.A precedent" per the M1.E spec §Decision 6). M1.E.1 is the right moment to pay that debt: the type now covers the full audio-config RPC surface, so `RemoteAudioClient` is accurate. A second separate `RemoteAudioWriter` struct would duplicate the baseURL/token/transport setup and force callers to carry two distinct references where one suffices.

### Decision 3 — HTTP timeout for write calls: 30 seconds

M1.E's `RemoteInfoClient` uses a 5-second timeout for GET requests (matching `RemoteSystemActions`). Write calls block until `systemctl restart mpd` returns on the Pi. MPD restart time on a Pi 5 with ALSA + USB DAC: 3-8 seconds nominal, up to 15 seconds on a slow first-start or after a config change that forces ALSA re-enumeration.

The write methods use a separate `*http.Client` with a 30-second timeout injected at construction time, distinct from the 5-second read client. The `RemoteAudioClientImpl` struct holds two HTTP clients:

```go
type RemoteAudioClientImpl struct {
    baseURL     string
    token       string
    readClient  *http.Client  // 5s — for GET methods
    writeClient *http.Client  // 30s — for POST methods
}
```

The `NewRemoteAudioClient(baseURL, token string)` constructor populates both with default timeouts. `NewRemoteAudioClientWithClients(baseURL, token string, rc, wc *http.Client)` allows test injection.

**Rationale:** A single 30-second timeout on all calls would degrade Settings-tab load time when the Pi is unreachable (the user waits 30s for each read to fail). A single 5-second timeout on writes would cause false-failure errors when MPD restart takes 6-8 seconds. Two clients, two budgets, one struct.

### Decision 4 — Post-write broadcasts: route through M1.E read helpers

After a successful write on the Pi, the Mac handler must broadcast the new state to all connected clients. The current local-mode pattern (`server.go:752, 775, 788-790`) broadcasts the return value of the write function itself (e.g., `s.io.Emit("pushDsdMode", result)` where `result` came from `SetDsdMode(mode)`). In remote mode that result describes what the Mac handler *thinks* the Pi's new state is — not what the Pi actually committed.

Instead, after a successful POST write, the Mac handler calls the M1.E read helpers to fetch Pi state and emits those values:

```
setDsdMode handler (success path):
  1. POST /api/audio/dsd → DsdModeResponse (write ack)
  2. s.dsdMode()         → DsdModeResponse (fresh Pi read via GET)
  3. s.io.Emit("pushDsdMode", freshState)   // broadcast Pi-truth

setMixerMode handler (success path):
  1. POST /api/audio/mixer → MixerModeResponse (write ack)
  2. s.mixerMode()         → MixerModeResponse (fresh Pi read)
  3. s.io.Emit("pushMixerMode", freshState)

applyBitPerfect handler (success path):
  1. POST /api/audio/bitperfect/apply → ApplyBitPerfectResponse (write ack)
  2. s.bitPerfect()   → BitPerfectStatus  (fresh Pi read)
  3. s.mixerMode()    → MixerModeResponse (fresh Pi read — applyBitPerfect can change mixer)
  4. s.io.Emit("pushBitPerfect",  freshBitPerfect)
  5. s.io.Emit("pushMixerMode",   freshMixerMode)
  6. client.Emit("pushApplyBitPerfect", writeAck)  // unicast to requesting client only
```

On write failure (Pi returns non-200, or `RemoteAudioClient.SetXxx` returns error), the handler emits the write-ack error payload back to the requesting client only (unicast, no broadcast) so the Settings UI can display the failure without corrupting other clients' state.

**Rationale:** The existing local-mode post-write broadcast is correct because the write and the state report both originate from the same process. In remote mode, the write happens on the Pi but the confirmation is constructed on the Mac. Rerouting through the read helpers is the only way to guarantee that the broadcasted state reflects actual Pi state after MPD restart.

### Decision 5 — Failure-mode UX: emit error response to requesting client only

If the Pi POST fails (network error, 401 token drift, HTTP 500, 30-second timeout), the Mac handler:

1. Logs `Warn` with the path, status code, and error (same pattern as M1.E read failures).
2. Emits the write-ack error payload back to the requesting client (unicast) in the same response shape as the local-mode failure path.
3. Does NOT broadcast to all clients — other clients' displayed state is unaffected.
4. Does NOT attempt a follow-up read — if the write failed, Pi state is indeterminate; reading and broadcasting could show stale state as if the write succeeded.

The frontend already handles `success: false` in `DsdModeResponse`, `MixerModeResponse`, and `ApplyBitPerfectResponse` — the Settings UI displays an error state for the affected control. No frontend changes are needed for the failure path.

**Rationale:** Asymmetric error handling (unicast to requester vs. broadcast on success) is the correct model: a write failure is an event for the requesting client's attention, not a state change for all clients. Broadcasting a "write failed" error to all clients could corrupt the other clients' Settings displays.

### Decision 6 — Idempotency: skip MPD restart when config content is unchanged

The Pi POST handlers read `/etc/mpd.conf`, compute the new content, and compare it to the existing content before writing. If `newContent === content` (byte-identical), the handler returns `success: true` without writing the file or restarting MPD.

This applies to all three write handlers:
- `POST /api/audio/dsd`: no-op if the `dop` line already has the requested value.
- `POST /api/audio/mixer`: no-op if `mixer_type` already has the requested value.
- `POST /api/audio/bitperfect/apply`: no-op if all four settings (`mixer_type`, `auto_resample`, `auto_format`, `auto_channels`) are already at their optimal values. The existing `ApplyBitPerfect()` Go function already implements this check (`audio_config.go:713-722`) — the Node port must preserve it.

The response shape on no-op success is identical to a write success: `success: true`. The handler may optionally add an `"unchanged": true` field as a diagnostic hint, but it is not required by the frontend.

**Rationale:** `systemctl restart mpd` causes a brief audio interruption (MPD re-initializes the ALSA device). Duplicate writes from the Settings UI (e.g., the user clicks "Apply" again without changing anything, or two clients send the same write) should not interrupt playback. The read-then-compare approach costs one extra file read per write call, which is negligible. The alternative — always restarting MPD — is user-visible and unnecessary.

### Decision 7 — Explicitly excluded from M1.E.1 scope

The following are not part of M1.E.1 and must not be implemented in this phase:

- Audio device selection (`getPlaybackOptions`, `setPlaybackOption`) — these require ALSA card enumeration and a more complex config surgery. Separate phase.
- NAS share write operations — already handled by M1.B/C via dedicated endpoints.
- Any handler that is not `setDsdMode`, `setMixerMode`, or `applyBitPerfect`.
- The MPD watcher panic fix (`internal/infra/mpd/client.go:404` nil-deref on Pi disappearance) — independent bug, tracked separately.
- Renaming `RemoteInfoReader` → `RemoteAudioClient` in a separate commit from adding the write methods, to keep diffs reviewable.

## API surface

### Pi endpoints (added to `mount-control-service.js`)

All require `X-Auth-Token` header. All return `application/json`. HTTP 200 on success, HTTP 400 on invalid input, HTTP 401 on missing/wrong token, HTTP 500 on execution failure.

#### `POST /api/audio/dsd`

Request body:
```json
{ "mode": "native" | "dop" }
```

Response (`DsdModeResponse` shape):
```json
{ "mode": "native", "success": true }
```

On invalid mode:
```json
{ "mode": "dop", "success": false, "error": "Invalid mode. Must be 'native' or 'dop'" }
```

On file write failure or MPD restart failure:
```json
{ "mode": "native", "success": false, "error": "Failed to write MPD config: ..." }
```

Algorithm: read `/etc/mpd.conf`, locate the `dop` line using the exact string-replacement logic from `SetDsdMode()` in `audio_config.go:519-573`. If newContent equals content (no-op), return success without writing or restarting. Otherwise `fs.writeFile('/etc/mpd.conf', newContent)` then `exec('systemctl restart mpd')`.

#### `POST /api/audio/mixer`

Request body:
```json
{ "enabled": true | false }
```

Response (`MixerModeResponse` shape):
```json
{ "enabled": false, "success": true }
```

Algorithm: read `/etc/mpd.conf`, apply the `mixer_type` regex replacement from `SetMixerMode()` (`audio_config.go:609-653`). No-op check before write. Write file + restart mpd.

#### `POST /api/audio/bitperfect/apply`

Request body: none (empty body or `{}`).

Response (`ApplyBitPerfectResponse` shape):
```json
{
  "success": true,
  "applied": ["mixer_type = bit-perfect", "auto_resample = bit-perfect"],
  "errors": []
}
```

Algorithm: port `ApplyBitPerfect()` (`audio_config.go:655-741`) to Node. The four settings (`mixer_type`, `auto_resample`, `auto_format`, `auto_channels`) are each checked against their current value; those that need updating are patched via regex replacement. If `applied` is empty (all settings already optimal), return success with `applied` containing "already set to optimal" messages — no write, no MPD restart. This matches the Go no-op path exactly (`audio_config.go:712-722`).

### Mac socket events (unchanged names)

| Socket event | Existing call site | New remote branch |
|---|---|---|
| `setDsdMode` | `server.go:743-756` | `s.remoteAudio.SetDsdMode(mode)` |
| `setMixerMode` | `server.go:766-779` | `s.remoteAudio.SetMixerMode(enabled)` |
| `applyBitPerfect` | `server.go:782-791` | `s.remoteAudio.ApplyBitPerfect()` |

Post-write broadcasts (remote mode):
- `setDsdMode` success → `s.io.Emit("pushDsdMode", s.dsdMode())`
- `setMixerMode` success → `s.io.Emit("pushMixerMode", s.mixerMode())`
- `applyBitPerfect` success → `s.io.Emit("pushBitPerfect", s.bitPerfect())` + `s.io.Emit("pushMixerMode", s.mixerMode())` + `client.Emit("pushApplyBitPerfect", writeAck)`

### Go client methods (added to `remote_info.go` → renamed `remote_audio.go`)

```go
// post JSON-encodes body, POSTs to baseURL+path with X-Auth-Token,
// decodes response JSON into dst, wraps errors with the path.
func (r *RemoteAudioClientImpl) post(path string, body any, dst any) error

func (r *RemoteAudioClientImpl) SetDsdMode(mode string) (DsdModeResponse, error)
    // POST /api/audio/dsd   body: {"mode": mode}

func (r *RemoteAudioClientImpl) SetMixerMode(enabled bool) (MixerModeResponse, error)
    // POST /api/audio/mixer body: {"enabled": enabled}

func (r *RemoteAudioClientImpl) ApplyBitPerfect() (ApplyBitPerfectResponse, error)
    // POST /api/audio/bitperfect/apply   body: {}
```

Constructor update:

```go
func NewRemoteAudioClient(baseURL, token string) *RemoteAudioClientImpl {
    return NewRemoteAudioClientWithClients(
        baseURL, token,
        &http.Client{Timeout: 5 * time.Second},   // readClient
        &http.Client{Timeout: 30 * time.Second},  // writeClient
    )
}

func NewRemoteAudioClientWithClients(
    baseURL, token string,
    readClient, writeClient *http.Client,
) *RemoteAudioClientImpl
```

## Error envelopes

### Mac side (handler failure path)

Pattern identical to M1.E reads:

```
log.Warn().Err(err).Str("path", "/api/audio/dsd").Msg("remote SetDsdMode failed")
client.Emit("pushDsdMode", DsdModeResponse{Mode: mode, Success: false, Error: err.Error()})
```

Greppable in `~/Library/Logs/stellar-backend.err.log` by `path=/api/audio/...`.

No `pushError` event. The frontend `success: false` path in the Settings component renders the error inline.

### Pi side (HTTP 500)

Matching the M1.E read error envelope:

```json
{ "success": false, "error": "Failed to write MPD config: open /etc/mpd.conf: permission denied", "code": "dsd_write_failed" }
```

HTTP status 500. Mac client treats all non-200 responses as errors and extracts the body for logging.

### Pi side (HTTP 400 — invalid input)

```json
{ "success": false, "error": "Invalid mode. Must be 'native' or 'dop'", "code": "invalid_input" }
```

HTTP status 400. Mac client treats this as an error and forwards the message to the frontend in the write-ack emit.

## Testing posture

### Pi side (Node, smoke)

Extend `Volumio2-UI/scripts/smoke-mount-control-info.sh` (or add a companion `smoke-mount-control-writes.sh`) with POST probes:

```bash
probe_post() {
  local path="$1" body="$2" expect_key="$3"
  body=$(curl -fsS -m 35 -X POST -H "Content-Type: application/json" \
    -H "X-Auth-Token: ${TOKEN}" -d "$body" "${BASE}${path}") || { echo "FAIL ${path}"; EXIT=1; return; }
  echo "$body" | jq -e ". | has(\"${expect_key}\")" >/dev/null || { echo "FAIL ${path}: missing ${expect_key}"; EXIT=1; return; }
  echo "OK   ${path}"
}

probe_post /api/audio/dsd              '{"mode":"native"}'  "success"
probe_post /api/audio/mixer            '{"enabled":false}'  "success"
probe_post /api/audio/bitperfect/apply '{}'                 "success"
```

Note `-m 35` to allow for MPD restart latency within the 30-second write timeout. The smoke script intentionally sends idempotent values (the current Pi state) to avoid disrupting audio during CI runs.

### Mac side (Go, `testing` + `httptest`)

**`remote_audio_test.go`** (renamed from `remote_info_test.go` or kept as two files):

- Existing 30 happy-path + failure-mode sub-tests from M1.E remain unchanged.
- Add three new happy-path sub-tests for `SetDsdMode`, `SetMixerMode`, `ApplyBitPerfect`.
- Add failure-mode coverage: HTTP 400, HTTP 500, timeout (use `httptest.NewServer` with a slow handler), JSON decode failure for each new method.
- Assert that POST requests hit the correct paths and carry `Content-Type: application/json` + `X-Auth-Token` headers.
- Assert that the write client (30s) is used (not the read client) — inject distinct `*http.Client` instances with traceable transports.

**`server_remote_audio_test.go`** (renaming or extending `server_remote_info_test.go`):

Extend `fakeRemoteAudioClient` with three new function fields:

```go
type fakeRemoteAudioClient struct {
    // ... existing six read fields ...
    setDsdModeFn     func(mode string) (DsdModeResponse, error)
    setMixerModeFn   func(enabled bool) (MixerModeResponse, error)
    applyBitPerfectFn func() (ApplyBitPerfectResponse, error)
}
```

Add branch tests for each write handler, covering:
1. Remote write success → read helper called → broadcast emitted (verify the broadcast payload comes from the read helper, not the write ack).
2. Remote write failure → unicast error to requesting client only → no broadcast.
3. Local fallback when `remoteAudio == nil` → existing `SetDsdMode()` / `SetMixerMode()` / `ApplyBitPerfect()` called.

Use the existing fake-socket pattern from `system_actions_test.go`.

### End-to-end (manual, kiosk + Settings page)

1. Open kiosk Settings → Audio tab. Confirm DSD mode shows Pi's current value.
2. Toggle DSD mode. Confirm the kiosk and a second browser window both update to the new value within 1-2 seconds (after MPD restart).
3. Open a second browser tab. Confirm the second tab shows the updated value (broadcast works).
4. Toggle software mixer on and off. Confirm both tabs update.
5. Click "Apply Bit-Perfect Settings." Confirm `pushBitPerfect` + `pushMixerMode` both broadcast.
6. `sudo systemctl stop stellar-mount-control` on Pi. Attempt a write from Settings → confirm error display in Settings, no crash, no broadcast to other clients.
7. Restart mount-control. Confirm writes work again without restarting Mac backend.

### Socket probe (DevTools, reusing M1.E pattern)

From the browser console (per memory `reference_devtools_socket_tap`):

```javascript
const { socketService } = await import('/src/lib/services/socket.ts');
socketService.on('pushDsdMode', (d) => console.log('pushDsdMode', d));
socketService.emit('setDsdMode', { mode: 'dop' });
// Expect: pushDsdMode {mode: "dop", success: true} within ~10s
socketService.emit('setDsdMode', { mode: 'native' });
// Expect: pushDsdMode {mode: "native", success: true} — restore original state
```

## Implementation order

The plan phase (`writing-plans`) will expand each step into tasks with file paths, line numbers, and verification checks. The high-level order is:

1. **Pi POST endpoints** — add `handleAudioDsdWrite`, `handleAudioMixerWrite`, `handleAudioBitperfectApply` to `mount-control-service.js`. Deploy. Smoke-test with `smoke-mount-control-writes.sh`.
2. **Mac client rename + write methods** — rename `remote_info.go` → `remote_audio.go`, rename types, add `post()` helper, add three `Set*` / `Apply*` methods. Update all reference sites (tests, server, main). `go test -race ./internal/transport/socketio/` must be green.
3. **Server handler branches** — three `if s.remoteAudio != nil` branches in `server.go:743-791`. Update post-write broadcasts to route through read helpers. Update `fakeRemoteAudioClient` in tests. `go test -race ./internal/transport/socketio/` must be green.
4. **Integration smoke** — set env, restart Mac backend, open kiosk Settings, walk the manual E2E list above.
5. **Commit spec + plan artifacts** — this spec doc + the plan doc land as non-code commits.

## Coverage matrix

| Decision | Plan task |
|---|---|
| D1 — Pi POST endpoints (three routes) | Pi: add `handleAudioDsdWrite`, `handleAudioMixerWrite`, `handleAudioBitperfectApply`; deploy; smoke |
| D2 — Mac client rename + write methods | Rename `remote_info.go`; add `post()`; add three write methods; update references; tests |
| D3 — 30s write timeout via separate `writeClient` | `NewRemoteAudioClientWithClients`; inject in tests with distinct clients; assert POST uses `writeClient` |
| D4 — Post-write broadcasts via read helpers | Branch in `server.go:743-791`; test: verify broadcast payload == `s.dsdMode()` not write-ack |
| D5 — Failure-mode unicast only | Test: write failure → unicast error, no `s.io.Emit`; manual E2E step 6 |
| D6 — Idempotency: no-op if unchanged | Pi handler unit test: POST same value twice, assert `systemctl restart mpd` not called (track via flag or mock) |
| D7 — Excluded scope | Code review gate: PR must not include any handler outside the three named ones |

## Cross-references

- M1.E spec (read proxy): `docs/superpowers/specs/2026-05-23-m1e-read-handler-proxy-design.md`
- M1.E plan (read proxy): `docs/superpowers/plans/2026-05-23-m1e-read-handler-proxy-plan.md`
- M1.A spec (portability): `docs/superpowers/specs/2026-05-13-m1a-backend-portability-design.md`
- M1.C spec (cutover): `docs/superpowers/specs/2026-05-19-m1c-cutover-design.md`
- `RemoteSystemActions` (M1.D precedent, 5s timeout): `internal/transport/socketio/system_actions_remote.go`
- `audio_config.go` write functions (Go source of truth for algorithms to port): `internal/transport/socketio/audio_config.go:519-741`
- `server.go` write handler call sites (current local-mode code to branch): `internal/transport/socketio/server.go:743-791`
- `mount-control-service.js` (Pi service, post-M1.E): `Volumio2-UI/pi-kiosk/mount-control-service.js`
- Memory `feedback_subagent_driven_execution` — expect `/clear` between phases during execution.
- Memory `reference_mpd_watcher_panic_on_target_loss` — Mac stellar still dies on Pi reboot; this spec does not address that.
- Memory `reference_stellar_cache_rebuild_wipes_enrichment` — unrelated; cache rebuild is not triggered by MPD config writes.
