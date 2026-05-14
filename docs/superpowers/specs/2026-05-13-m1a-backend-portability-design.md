# M1.A — Backend portability layer (design)

**Date:** 2026-05-13
**Initiative:** `we-are-going-to-linear-blanket` (M1)
**Parent plan:** `~/.claude/plans/we-are-going-to-linear-blanket.md` §M1.A
**Predecessors:**
- M1.B SHIPPED: `cmd/stellar-spectrum` daemon + per-channel L/R FFT live on the Pi, pushing `pushSpectrum` at 20 FPS over Socket.IO.
- M1.E SHIPPED: VU meter frontend consumes L/R bands live.
- `internal/domain/sources/{mounter.go, discoverer.go}` interfaces already exist; only Linux concrete impls live today.

## Why this work (load-bearing)

The driving goal is **freeing the Raspberry Pi 5 to play music with no audio hiccups**. The Pi currently runs the full backend (Socket.IO server, SQLite cache builds, artwork resolution, enrichment HTTP calls, bio LLM calls, mount supervisor) alongside MPD and a Chromium kiosk. CPU spikes from any of those non-realtime workloads occasionally preempt MPD's audio thread or contend with PipeWire, producing playback glitches.

M1.A is the *enabler* phase. It does NOT itself reduce Pi load; it removes the Linux/Pi-specific coupling that currently forces the backend to live on the Pi. **M1.C** (cutover) is where the load actually moves off the Pi onto a Mac host. M1.A's quality bar therefore is: "after M1.A, the binary cleanly builds for darwin/arm64 with zero `/proc`, `/sys`, `/mnt`, `wlr-randr`, `nmcli`, or `mount(8)` references reachable on Mac". When M1.C flips the topology, that guarantee is what makes the Pi-load reduction real.

The Mac is the *interim* host. Plan B's long-term destination is a Windows mini-PC, so the abstraction must also accept a Windows compile target — even if Windows impls are stubs for M1.

## User-locked decisions (this brainstorm)

1. **Mac impl scope per subsystem:**
   - **LCD** — stub (`ErrUnsupported`). LCD control stays on the Pi via `lcd-control.service` (per master plan §M1.C). Mac has no LCD attached.
   - **Netinfo** — real macOS impl using `networksetup` / `ipconfig getifaddr` / `ifconfig`. Mac backend can answer `/api/v1/network` truthfully and broadcast `pushNetworkStatus`.
   - **Sources** — real macOS impls using `mount_smbfs` / `mount_nfs` for the mounter and `dns-sd -B _smb._tcp.` / `smbutil view` for the discoverer. Gives M1.C a working NAS-browse path without a second phase of work.
2. **Cross-cutting Linux paths/constants:** New `internal/infra/paths` package with build tags. Centralises data dir, mount bases, and a portable `ListMounts()` helper. Replaces every hardcoded `/data/stellar`, `/mnt/NAS`, `/mnt/USB`, `/proc/mounts` reference outside the named LCD/netinfo/sources scope.
3. **Platform-selection pattern:** Uniform `//go:build linux|darwin|windows` build tags + `pkg.NewPlatform()` constructors. Existing `internal/domain/sources/linux_mounter.go` and `linux_discoverer.go` are renamed to `mounter_linux.go` and `discoverer_linux.go` for consistency and gain `//go:build linux`.
4. **Package shape:** Approach 2 — Socket.IO handlers move out of `internal/transport/socketio/{lcd.go, network.go}` into the new infra packages' `handlers.go` files. The socketio package becomes a thinner transport router that exposes a `HandlerRegistrar` and a `Broadcaster`.
5. **Event emission across the seam:** Each infra package defines a small `Broadcaster` interface (`Emit(event string, payload any)`). The socketio package implements it. Infra packages emit `pushLCDStatus`, `pushNetworkStatus`, etc., without importing `zishang520`.
6. **Windows scope:** Pure stubs everywhere this phase. `*_windows.go` files exist solely so `GOOS=windows go build` succeeds; all functions return `ErrUnsupported` or zero values. No `netsh`/`wmic` work in M1.A.
7. **Netinfo deduplication:** The two existing Linux network impls (one in `cmd/stellar/main.go:528-708`, one in `internal/transport/socketio/network.go`) collapse into a single canonical Linux impl inside `internal/infra/netinfo`. Both the REST `/api/v1/network` handler and the Socket.IO `pushNetworkStatus` event delegate to that one impl.
8. **Test strategy:** Per-platform tests live in `*_linux_test.go` / `*_darwin_test.go` (build-tagged) and only run on their target OS. A shared `interface_test.go` (no tag) covers cross-platform contracts that don't need OS facilities (e.g., `LCDStatus` zero-value behaviour, paths package returning non-empty strings, error type sentinel checks).
9. **Makefile:** Add `build-darwin` (cross-compile darwin/arm64) AND `build-windows` (cross-compile windows/amd64) this phase. The existing `build` (linux/arm64 Pi) and `build-local` (host) targets stay unchanged. Pure-Go `modernc.org/sqlite` already supports both targets, so producing a real Windows binary is near-free and catches linker / missing-impl issues that `go vet` alone misses.
10. **Falsifiable success gate:** `strings bin/stellar-darwin-arm64 | grep -E '/(proc|sys|mnt)/|/etc/(network|resolv)'` returns zero matches; `nm bin/stellar-darwin-arm64 | grep -E 'wlr_randr|nmcli|mount\.cifs'` returns zero matches. The Mac binary is structurally Pi-free. (`/dev` and `/run` deliberately excluded from the strings grep — too noisy because of stdlib `/dev/null`-style references.)

## Out of scope (explicit)

- **The cutover itself.** Mac host setup, MPD-remote-host wiring (`-mpd-host` already exists), backend systemd disable, kiosk URL flip — all M1.C.
- **Bio LLM relocation.** The bio pipeline already takes `ANTHROPIC_API_KEY` via env; when the backend runs on Mac it just inherits the Mac shell's env. No code change needed in M1.A.
- **Cache library rebuild.** The SQLite cache and its build pipeline are already platform-agnostic Go. They follow the backend wherever it runs. M1.D is a verification phase only; M1.A does nothing here.
- **Chromium kiosk CPU.** The kiosk runs on the Pi against a Mac-served frontend (`?layout=` URL param is dead per memory). Kiosk CPU is M1.F (Pi OS audit) territory, not M1.A.
- **LCD remote-control plumbing.** When the Mac backend wants to turn the Pi LCD off, it'll go through `lcd-control.service` on the Pi (decided in master plan §M1.C). M1.A only ships the Mac-side stub that returns `ErrUnsupported`; the RPC path is M1.C's job.
- **Audirvana service.** Already Mac-friendly. Don't touch.
- **Top-level tracked binaries.** `stellar-arm64`, `stellar-arm64-cgo`, `stellar-arm64-nocgo` at the repo root are stale tracked binaries unrelated to M1.A. Cleanup is parked.

## Architecture

### Package layout

```
internal/infra/lcd/                     NEW
  lcd.go                                Interface + LCDStatus + ErrUnsupported sentinel + Broadcaster iface
  lcd_linux.go        //go:build linux  All wlr-randr / DPMS / backlight logic from socketio/lcd.go
  lcd_darwin.go       //go:build darwin Stub: Status() returns {IsOn: true}; Set() returns ErrUnsupported
  lcd_windows.go      //go:build windows Same stub
  handlers.go                            RegisterHandlers(reg HandlerRegistrar, brd Broadcaster, svc LCDController)
  lcd_linux_test.go   //go:build linux
  lcd_darwin_test.go  //go:build darwin
  interface_test.go                     Cross-platform LCDStatus zero-value & ErrUnsupported sentinel tests

internal/infra/netinfo/                 NEW
  netinfo.go                            Interface (GetStatus() NetworkStatus) + NetworkStatus + Broadcaster
  netinfo_linux.go    //go:build linux  Consolidated single impl (was main.go:528-708 + socketio/network.go)
  netinfo_darwin.go   //go:build darwin Real impl: networksetup -listallhardwareports, ipconfig getifaddr, ifconfig
  netinfo_windows.go  //go:build windows Stub returning {Type: "none"}
  handlers.go                            Registers Socket.IO pushNetworkStatus + REST /api/v1/network
  *_test.go

internal/infra/paths/                   NEW
  paths.go                              Public API: DataDir(), CacheDir(), NasMountBase(), UsbMountBase(), ListMounts() []Mount
  paths_linux.go      //go:build linux  /data/stellar, /mnt/NAS, /mnt/USB, /proc/mounts parser
  paths_darwin.go     //go:build darwin ~/Library/Application Support/stellar, /Volumes, mount(8) parser
  paths_windows.go    //go:build windows %LOCALAPPDATA%\stellar, stub mount listing
  *_test.go                             Per-platform tests; cross-platform contract test asserts non-empty + idempotent

internal/domain/sources/                EXISTING — refactored
  mounter.go                            (unchanged) interface
  discoverer.go                         (unchanged) interface
  service.go                            Replaces hardcoded /mnt/NAS and /mnt/USB with paths.NasMountBase() / paths.UsbMountBase()
  mounter_linux.go    //go:build linux  RENAMED from linux_mounter.go + add build tag
  mounter_darwin.go   //go:build darwin NEW — mount_smbfs / mount_nfs with same exec.CommandContext 6s budget
  discoverer_linux.go //go:build linux  RENAMED from linux_discoverer.go + add build tag
  discoverer_darwin.go //go:build darwin NEW — dns-sd -B _smb._tcp / smbutil view
  platform_linux.go   //go:build linux  NewPlatformMounter() / NewPlatformDiscoverer()
  platform_darwin.go  //go:build darwin Same constructor names; darwin types
  platform_windows.go //go:build windows Returns ErrUnsupported impls
  *_test.go                             Renamed alongside their impl files
```

### Files deleted

```
internal/transport/socketio/lcd.go            -362 lines (logic in internal/infra/lcd)
internal/transport/socketio/network.go        -193 lines (logic in internal/infra/netinfo)
cmd/stellar/main.go:528-708 (network helpers) ~180 lines deleted; main.go shrinks materially
```

### Files edited (callsite-only)

```
cmd/stellar/main.go                            Wire lcd.NewPlatform(), netinfo.NewPlatform(), sources.NewPlatformMounter/Discoverer,
                                               paths.DataDir(). Remove inlined networking code. Pass Broadcaster + HandlerRegistrar
                                               into each pkg's RegisterHandlers().
internal/transport/socketio/server.go          Expose HandlerRegistrar + Broadcaster on the Server struct so RegisterHandlers
                                               from infra packages can wire in. Drop direct lcd.go / network.go references.
internal/domain/sources/service.go             Replace const NasMountBase / UsbMountBase reads with paths.* helpers.
internal/domain/localmusic/classifier.go       Replace direct os.Open("/proc/mounts") with paths.ListMounts().
internal/transport/socketio/system.go          Minor: /proc/cpuinfo read moves to paths.SystemInfo() with darwin stub returning
                                               sysctl-derived values (already a 57-line file; ~10 lines change).
Makefile                                       Add build-darwin target. Verify build-local + build still work.
```

### Dependency direction (load-bearing invariant)

```
  cmd/stellar (main.go)
        │ wires constructors + Broadcaster + HandlerRegistrar
        ▼
  internal/transport/socketio (Server)
        │ implements HandlerRegistrar + Broadcaster
        ▼
  internal/infra/{lcd,netinfo,sources}/handlers.go
        │ registers callbacks via HandlerRegistrar; emits via Broadcaster
        ▼
  internal/infra/{lcd,netinfo,sources} (platform interfaces + impls)
        │
        ▼
  internal/infra/paths
```

**Invariant: `internal/infra/*` never imports `internal/transport/socketio` or `github.com/zishang520/socket.io/v3`.** This is the test that "transport-agnostic infra" actually held.

### Interface shapes (concrete)

```go
// internal/infra/lcd/lcd.go
package lcd

import "errors"

var ErrUnsupported = errors.New("lcd: not supported on this platform")

type Status struct {
    IsOn bool `json:"isOn"`
}

type Controller interface {
    Status() (Status, error)
    Set(on bool) error
}

type Broadcaster interface {
    Emit(event string, payload any)
}

type HandlerRegistrar interface {
    OnRequest(event string, handler func(args ...any) any)
}

func NewPlatform() Controller   // build-tag-selected per platform
func RegisterHandlers(reg HandlerRegistrar, brd Broadcaster, c Controller)
```

`netinfo`, `sources` packages follow the same shape: an interface, `ErrUnsupported`, `NewPlatform()`, `RegisterHandlers`.

## Migration & sequencing

Implementation lands as **six sequential commits**. Each commit leaves the binary buildable and tests green on Linux. The netinfo work is split across two commits so that if a behavioural-drift regression appears, bisect cleanly isolates "move" from "dedup."

1. **Commit 1 — `internal/infra/paths`.** Pure additive package. Replace callsites in `main.go`, `sources/service.go`, `localmusic/classifier.go`. No behavior change on Pi. Concrete test: existing Pi backend still parses `/proc/mounts` correctly via `paths.ListMounts()`.
2. **Commit 2 — `internal/infra/lcd`.** Move `socketio/lcd.go` body into `internal/infra/lcd/lcd_linux.go`. Add darwin/windows stubs. Add `handlers.go`. Delete `socketio/lcd.go`. Add `build-darwin` AND `build-windows` Makefile targets. Concrete test: `make build` (Pi cross-compile), `make build-darwin`, and `make build-windows` all succeed; LCD power tile still works on the Pi.
3. **Commit 3a — `internal/infra/netinfo` move (keep both impls).** Create the new package shape. Move `socketio/network.go` Linux logic into `internal/infra/netinfo/netinfo_linux.go` *unchanged*. Move `main.go:528-708` helpers into a sibling file inside the same package, *also unchanged* — call it `netinfo_legacy_linux.go` and keep its functions exported only to the package. Add darwin + windows. Both old callsites (REST `/api/v1/network` and Socket.IO `pushNetworkStatus`) now route through the new package but each still calls *its own* original impl. Capture pre-change JSON payloads as test fixtures (`netinfo_fixture_test.go`) — assert both impls produce equal output for the current Pi state. Concrete test: payload bytes from REST and pushNetworkStatus match the fixtures byte-for-byte after deploy.
4. **Commit 3b — netinfo dedup to canonical impl.** Delete `netinfo_legacy_linux.go`. Route both callsites at the canonical `netinfo_linux.go` impl. Fixture test from 3a still passes. Concrete test: bisect-safe — if any drift appears, the regression is provably inside this commit, not the move.
5. **Commit 4 — `internal/domain/sources` rename + darwin impls.** Rename existing Linux files, add darwin counterparts, add `platform_*.go` selector files. Concrete test: existing `sources` Go tests still pass on Linux; new darwin tests pass on macOS; `sources.NewPlatformMounter()` returns a `LinuxMounter` on Pi.
6. **Commit 5 — final wiring + falsifiable success gate.** `main.go` switches its `sources.NewLinuxMounter()` calls to `sources.NewPlatformMounter()`. Run the success-gate grep against `bin/stellar-darwin-arm64` and confirm `bin/stellar-windows-amd64.exe` builds. Document new flags / behavior in `docs/ARCHITECTURE.md`.

Each commit is independently revertable. If any reviewer pass surfaces a problem after a commit, the next commit can absorb the fix rather than amending.

## Testing strategy

| Layer | Where | Runs on | What it covers |
|---|---|---|---|
| Cross-platform interface contracts | `*/interface_test.go` (no build tag) | All platforms | Zero-value behaviour, ErrUnsupported sentinel identity, constructor returns non-nil |
| Linux impl tests | `*_linux_test.go` (`//go:build linux`) | Linux only | wlr-randr parsing, /sys/class/net carrier reads, /proc/mounts parser, mount.cifs command shape |
| Darwin impl tests | `*_darwin_test.go` (`//go:build darwin`) | macOS only | networksetup output parsing, mount_smbfs command shape, dns-sd discovery shape; uses exec stubs via PATH-prepended fakes (`PATH=t/testdata:$PATH`) to avoid hitting the real network |
| Compile gates | `make build` (Pi) + `make build-darwin` (Mac) | CI + dev | Binary builds for both targets |
| Falsifiable success gate | `strings`/`nm` grep against `bin/stellar-darwin-arm64` | After commit 5 | Mac binary contains no `/proc /sys /mnt wlr_randr nmcli mount.cifs` strings |

**Test-fake convention for exec-based impls:** Each darwin impl exposes a `var execCommand = exec.CommandContext` package var so tests can substitute a fake that writes to stdout. Pattern already used in `linux_mounter_test.go`; carry it forward.

## Risks & mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Build tag conflict — same symbol defined in both `_linux.go` and `_darwin.go` causes ambiguous-build-on-CI | Low | Verify each new file has exactly one `//go:build` tag and matching `_<goos>.go` suffix; Go's build tooling enforces uniqueness |
| Behavioural drift between netinfo deduped impl and old socketio/main.go versions | Medium | Side-by-side smoke: spin Pi backend pre-deploy, capture `/api/v1/network` + `pushNetworkStatus` JSON; after Commit 3 deploy, capture again. Compare byte-by-byte. |
| Darwin sources impl assumes `mount_smbfs` is in `$PATH` but it's actually in `/sbin` on macOS | High | Hardcode `/sbin/mount_smbfs` / `/usr/sbin/scutil` etc. in the darwin impl, OR call `exec.LookPath` first and fall back to `/sbin`. Tested via the PATH-prepended fake convention |
| `mount_smbfs` requires the share to be pre-mounted in keychain or interactive credentials prompt | Medium | Use `mount_smbfs //user:pass@host/share /Volumes/Foo` URL-style auth; document in README that the daemon needs SMB creds in its env (already true on Linux) |
| `dns-sd` blocks indefinitely without timeout | High | Wrap in `exec.CommandContext` with 6 s budget — same pattern shipped in `linux_mounter.go` per the Plan §M1.A "Reuse" note |
| Renaming `linux_mounter.go` → `mounter_linux.go` breaks `git blame` continuity | Low | Use `git mv` (single rename op preserves history); add a one-line comment in the new file referencing the rename commit for searchability |
| Mac binary works but Pi binary regresses due to Linux build-tag mistake | Medium | CI/local run `make build` (Pi ARM64) after every commit, run `go test ./...` with `GOOS=linux` |
| User has to type `make build-darwin` to test M1.A on the Mac but doesn't realise that requires the musl toolchain isn't needed | Low | Document in commit 2's message: `build-darwin` uses native Go cross-compile (no CGO; SQLite via pure-Go modernc.org/sqlite already supports darwin/arm64) |

## Falsifiable success criteria (final gates)

Before declaring M1.A done:

1. `make build` (Pi ARM64) succeeds; binary identical-ish size to current (within 5%).
2. `make build-darwin` succeeds; produces `bin/stellar-darwin-arm64`.
3. `make build-windows` succeeds; produces `bin/stellar-windows-amd64.exe` (Windows impls are stubs; this gate exists to catch linker errors and missing-symbol leaks that `go vet` alone misses).
4. `go test ./...` passes on macOS with `GOOS=darwin`.
5. `strings bin/stellar-darwin-arm64 | grep -E '/(proc|sys|mnt)/|/etc/(network|resolv)'` → 0 matches. (`/dev` and `/run` deliberately excluded — too noisy because of stdlib `/dev/null`-style references.)
6. `nm bin/stellar-darwin-arm64 | grep -E 'wlr_randr|nmcli|mount\.cifs'` → 0 matches.
7. Pi backend redeployed from the new tree: existing LCD power tile, network status broadcast, and NAS browse all still work.
8. No grep of `internal/infra/` matches `zishang520` or `transport/socketio`.
9. Netinfo dedup verification (Commit 3b): REST `/api/v1/network` and Socket.IO `pushNetworkStatus` payloads match the Commit-3a fixtures byte-for-byte on a redeployed Pi backend.

## Non-goals reminder

This phase does **not** measurably reduce Pi load yet — that's M1.C's job. M1.A's value is purely *enablement*: it makes the Pi-load-reduction win in M1.C a topology change rather than a code change.
