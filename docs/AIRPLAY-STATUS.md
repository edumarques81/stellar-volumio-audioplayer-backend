# AirPlay integration — state of play (2026-05-26)

End-of-session snapshot. Most of the metadata pipeline works; audio path
contention with MPD and the AirPlay→MPD round-trip are still broken.
Next session should pick up from here.

---

## What works ✅

1. **Pi daemon → Mac backend → Socket.IO broadcast** is solid.
   - Per-session **stable sessionID** maintained across track changes
     (was minting a new ID per `pbeg` and racing iOS's
     sessionID-matched `pushAirplayEnded` filter).
   - `pend` no longer killed as session-end (was firing at every track
     boundary). It now maps to `paused: true`.
   - DACP resolver streams `dns-sd -L` output and returns on first
     match (~1 ms) — was burning the full 3 s command budget.
   - Mac re-emits `pushAirplayState` to a freshly-connecting client
     (and via a new `getAirplayState` event the iOS app calls on
     foreground/`NowPlayingView.onAppear`).
   - iOS `AirplayStore` correctly receives + renders title / artist /
     album / 240 KB cover / progress.
   - Shairport pre/post hook scripts + env-file mode now tracked in
     `deploy/`. Post-hook env reads work after `chgrp shairport-sync +
     mode 640` on `/etc/stellar-airplay/env`.

## What is still broken ❌

### 1. MPD vs shairport-sync DAC contention
MPD holds `plughw:CARD=U20SU6,DEV=0` open across `mpc pause` (default
behaviour for fast resume). When the user tries to AirPlay while MPD
is in any state other than fully stopped, shairport opens the network
connection, fails to acquire the PCM, and tears the AirPlay session
down within ~2 s. Hook log signature: `session START` followed by
`session END (post-hook)` in the same second.

Mitigation applied (unverified live): `deploy/stellar-airplay-pre.sh`
now does `mpc stop` (not `mpc pause`) + a 300 ms sleep. Was deployed
to the Pi as `/usr/local/bin/stellar-airplay-pre.sh` and committed at
`8123681`.

Open question: this only fixes the AirPlay-takeover direction. The
reverse direction (AirPlay → MPD) is unrelated and broken — see below.

### 2. Switch from AirPlay back to normal mode is broken
After the AirPlay session ends and the user wants to resume MPD
playback (NAS / local files), the user reports:
- A "weird noise" comes out of the DAC.
- MPD doesn't resume — UI stays in AirPlay state (control lost).
- The user has to manually `mpc stop` from the terminal to recover.

Expected behaviour: post-hook ends the AirPlay session cleanly,
shairport releases the DAC, the iOS / LCD UI returns to MPD mode, and
the user can hit play on MPD without artefacts.

Hypotheses (untested):
- shairport may leave the DAC in an undefined state on session end
  (sample rate / format mismatch with what MPD wants to send next).
- The iOS app's AirplayStore may not clear when `pushAirplayEnded`
  arrives if its sessionID-match filter rejects the event (e.g. the
  ID drifted between the daemon's last `pbeg` mint and the post-hook
  curl POST).
- Pi post-hook posts `{ended: true}` directly to the Mac — that path
  doesn't include the current sessionID, and the Mac's `End()` call
  uses whatever sessionID is in the session. If iPhone-side races
  produced a different ID between START and END, the filter cuts both
  sides out.

### 3. DACP control buttons (play/pause/next/prev) — never confirmed working
The resolver timeout was fixed (`ea3300b`) and a success log added
(`682ba65`). But during the most recent live test, the user never
witnessed a successful DACP round-trip because the AirPlay sessions
kept dying within 2 s of starting (issue 1 above). Need to retest
once issue 1 is verified stable.

The handler returns `"session not yet controllable (waiting for
Active-Remote)"` when `CanControl` is false — and `CanControl` is
gated on the session having both `ActiveRemote` and `DACPID`. Both
should land in the first 1–2 ingest POSTs from the daemon. Worth
confirming via the Mac log next time.

### 4. LCD kiosk Volumio2-UI parity — unverified in this cycle
The Volumio2-UI agent shipped the LCD-side AirPlay integration
(commits `6ebf66b0`, `72128bdf`, `7dd4d3ab`, `f95d6ddd`) but the
mid-session debugging cycles never returned to verify the LCD swaps
to AirPlay mode reliably. The user reported once that the LCD got the
album art but no title — likely a transient state during one of the
"session dies in 2s" failures, not a Volumio2-UI bug.

---

## Architectural concerns to revisit

### A. Audio engine routing
There's no central "audio engine router" deciding who owns the DAC.
MPD assumes exclusive ownership; shairport assumes it can take over
when an AirPlay sender connects. The pre/post hook pattern is a
workaround, not a design.

Options:
- **ALSA `dmix`** — software mixer that lets multiple PCM clients
  share the device. Trades bit-perfect playback for compatibility.
- **PipeWire / PulseAudio** — same trade-off; routes audio at a
  higher level.
- **Explicit handoff state machine** in the Go backend — track
  current audio engine (MPD / AirPlay / UPnP), forbid simultaneous
  ownership, mediate transitions.

For Stellar's bit-perfect mandate, option C is the right answer
long-term. Short-term the hook-based handoff is acceptable if
issue 2 is fixed.

### B. iOS state after AirPlay ends
The iOS app should:
1. Receive `pushAirplayEnded` with the active sessionID.
2. Clear `AirplayStore.state`.
3. NowPlayingView falls back to MPD state via `airplayStore.state.isActive
   ? airplayBranch : mpdBranch`.
4. User taps play in iOS → emits `play` (MPD) → MPD resumes.

If step 1 or 2 fails, iOS stays stuck on the AirPlay branch — which
matches the user's "control lost" report. Worth instrumenting next
session.

### C. SessionID coherence between daemon and post-hook
The post-hook curls the Mac with `{ended: true}` (no sessionID). The
Mac's `Session.End()` returns whatever the current sessionID is.
That's broadcast as `pushAirplayEnded {sessionID: <whatever>}`.

If the iPhone-side AirPlay TCP closed and re-opened quickly (which
happens during some sender lifecycle events), the daemon may have
already minted a new sessionID via a fresh `pbeg`. The post-hook then
ends the WRONG session. Need to think about whether the post-hook
should include the sessionID it intends to end — or whether `End()`
should always be force-clear regardless.

---

## Concrete next-session checklist

1. **Verify the pre-hook MPD-release fix** with MPD actively playing,
   then user toggles AirPlay → confirm session lives, audio plays,
   no `session END (post-hook)` within 5 s. Watch `/var/log/stellar-airplay.log`.
2. **Confirm `dacp dispatch ok`** appears in `~/Library/Logs/stellar-backend.err.log`
   when the user taps play/pause/next/prev from the Stellar iOS app
   during an active session. Verify the iPhone actually obeys.
3. **Diagnose issue 2 (AirPlay → MPD switch)** — instrument
   `pushAirplayEnded` reception on the iOS side; capture the
   sessionID mismatch (if any) between the AirplayStore's stored ID
   and the post-hook's ended-event ID. Likely fix: ensure the
   post-hook reads the current sessionID from somewhere (env file?
   `/tmp/stellar-airplay-session.txt` written by daemon?) and posts
   it explicitly.
4. **Audio artefact on switch-back** — capture `aplay`-level state
   during the transition: is the PCM in a bad state when MPD
   re-opens it? May need `mpc stop` + sleep + `mpc play` in the
   post-hook or in iOS.
5. **End-to-end verify Volumio2-UI LCD parity** once the iOS side is
   stable.

---

## Useful diagnostic commands

```bash
# Mac stellar log
tail -f ~/Library/Logs/stellar-backend.err.log

# Pi shairport log
ssh eduardo@192.168.86.25 'sudo journalctl -u shairport-sync -f'

# Pi daemon log
ssh eduardo@192.168.86.25 'sudo journalctl -u stellar-airplay -f'

# Hook log (lifecycle events)
ssh eduardo@192.168.86.25 'sudo tail -f /var/log/stellar-airplay.log'

# DAC state
ssh eduardo@192.168.86.25 'for f in /proc/asound/card*/pcm0p/sub0/status; do sudo cat "$f" | head -1; done'

# What process owns the DAC
ssh eduardo@192.168.86.25 'sudo fuser -v /dev/snd/*'

# Socket tap (capture pushAirplayState broadcasts to all clients)
node -e "import('/Users/eduardomarques/workspace/stellar-streamer/Volumio2-UI/node_modules/socket.io-client/build/esm/index.js').then(({io}) => {
  const s = io('http://localhost:3000', { transports: ['websocket'] });
  s.onAny((e, ...a) => { if (e.includes('irplay')) console.log(new Date().toISOString(), e, JSON.stringify(a[0]||{}).slice(0,200)); });
  setTimeout(() => process.exit(0), 30000);
})"
```

---

## Files touched this debugging cycle

Backend (`stellar-volumio-audioplayer-backend`):
- `cmd/stellar-airplay/bundler.go` — pend → paused
- `cmd/stellar-airplay/bundler_test.go` — updated test
- `cmd/stellar-airplay/main.go` — reverted heartbeat-gating
- `internal/domain/airplay/session.go` — no sessionID churn per pbeg
- `internal/domain/airplay/session_test.go` — pin behaviour
- `internal/domain/airplay/dacp_resolver.go` — stream-parse dns-sd
- `internal/transport/socketio/airplay_register.go` — getAirplayState handler + pushAirplayStateTo
- `internal/transport/socketio/airplay_ingest.go` — INFO log per emit
- `internal/transport/socketio/airplay_command.go` — INFO log on dispatch success
- `internal/transport/socketio/server.go` — call pushAirplayStateTo on connect
- `deploy/stellar-airplay-pre.sh` — new, mpc stop + sleep
- `deploy/stellar-airplay-post.sh` — new, curls Mac on true session-end
- `deploy/install-stellar-airplay.sh` — wires the hook scripts

iOS (`stellar-ios`):
- `StellarVolumiO/App/StellarApp.swift` — requestAirplayState on didBecomeActive
- `StellarVolumiO/Services/SocketService.swift` — requestAirplayState method
- `StellarVolumiO/Views/NowPlaying/NowPlayingView.swift` — onAppear refresh

Pi (system, applied in-place):
- `/etc/stellar-airplay/env` — chgrp shairport-sync, mode 640
- `/etc/shairport-sync.conf` — commented out `mixer_control_name` (we
  ignore volume control, so opening the mixer caused spurious
  failures on the SU-6 DAC)
- `/usr/local/bin/stellar-airplay-pre.sh` — installed (mpc stop variant)
- `/usr/local/bin/stellar-airplay-post.sh` — installed (curl ended:true)
