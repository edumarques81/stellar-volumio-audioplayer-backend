# M1.A — Backend Portability Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add cross-platform abstractions (`internal/infra/{paths,lcd,netinfo}` + `internal/domain/sources` refactor) so `stellar-backend` builds and runs on darwin/arm64 and compiles for windows/amd64, unblocking M1.C's Pi→Mac cutover.

**Architecture:** Build-tag-selected platform implementations behind interfaces. Linux gets real impls (extracted from `internal/transport/socketio/{lcd,network}.go`, `cmd/stellar/main.go:528-668`, and existing `internal/domain/sources/linux_*.go`). Darwin gets real impls for netinfo + sources, stub for LCD. Windows gets pure stubs everywhere. New `Broadcaster` interface lets infra packages emit Socket.IO events without importing `zishang520`. Netinfo dedup is split across two commits (3a "move, keep both impls" + 3b "dedup to canonical") for bisect cleanliness.

**Tech Stack:** Go 1.25, build tags (`//go:build linux|darwin|windows`), `modernc.org/sqlite` (pure-Go, supports all three targets), existing exec-fake test pattern (`var execCommand = exec.CommandContext`).

**Spec reference:** `docs/superpowers/specs/2026-05-13-m1a-backend-portability-design.md`

**Repo:** All work happens in `stellar-volumio-audioplayer-backend/` on `main`.

---

## Commit Group 1 — `internal/infra/paths` package

Pure additive package that centralises `/data/stellar`, `/mnt/NAS`, `/mnt/USB`, `/proc/mounts`, `/proc/cpuinfo` references behind a portable API. Foundation for everything that follows.

### Task 1.1: Define `paths` package interface

**Files:**
- Create: `internal/infra/paths/paths.go`
- Create: `internal/infra/paths/interface_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/infra/paths/interface_test.go
package paths

import (
	"errors"
	"testing"
)

func TestDataDirNonEmpty(t *testing.T) {
	if got := DataDir(); got == "" {
		t.Errorf("DataDir() = %q, want non-empty", got)
	}
}

func TestCacheDirNonEmpty(t *testing.T) {
	if got := CacheDir(); got == "" {
		t.Errorf("CacheDir() = %q, want non-empty", got)
	}
}

func TestNasMountBaseNonEmpty(t *testing.T) {
	if got := NasMountBase(); got == "" {
		t.Errorf("NasMountBase() = %q, want non-empty", got)
	}
}

func TestUsbMountBaseNonEmpty(t *testing.T) {
	if got := UsbMountBase(); got == "" {
		t.Errorf("UsbMountBase() = %q, want non-empty", got)
	}
}

func TestErrUnsupportedIdentity(t *testing.T) {
	// Sentinel must be comparable via errors.Is across the package.
	wrapped := errors.New("wrap: " + ErrUnsupported.Error())
	if errors.Is(wrapped, ErrUnsupported) {
		t.Error("plain errors.New wrap should not satisfy errors.Is for sentinel")
	}
	if !errors.Is(ErrUnsupported, ErrUnsupported) {
		t.Error("ErrUnsupported should satisfy errors.Is against itself")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd stellar-volumio-audioplayer-backend
go test ./internal/infra/paths/
```

Expected: FAIL — `paths` package does not exist.

- [ ] **Step 3: Implement the interface file**

```go
// internal/infra/paths/paths.go
// Package paths centralises platform-specific filesystem locations and mount
// enumeration so the rest of the codebase can stay platform-agnostic.
package paths

import "errors"

// ErrUnsupported is returned by helpers that have no meaningful behaviour on
// the current platform (e.g., mount enumeration on Windows).
var ErrUnsupported = errors.New("paths: not supported on this platform")

// Mount describes one row of the host's mount table.
type Mount struct {
	Source     string // e.g. "//nas/Music" or "192.168.1.10:/export/music"
	MountPoint string // absolute path
	FSType     string // "cifs", "nfs", "apfs", "ntfs", "auto"
}

// DataDir returns the canonical data directory for stellar config and SQLite.
// Linux: /data/stellar. Darwin: $HOME/Library/Application Support/stellar.
// Windows: %LOCALAPPDATA%\stellar.
func DataDir() string { return dataDir() }

// CacheDir returns the canonical cache directory for derived data.
// Linux: /data/stellar/cache. Darwin: $HOME/Library/Caches/stellar.
// Windows: %LOCALAPPDATA%\stellar\cache.
func CacheDir() string { return cacheDir() }

// NasMountBase returns the base directory under which NAS shares are mounted.
// Linux: /mnt/NAS. Darwin: /Volumes/stellar-nas. Windows: stub (ErrUnsupported intent).
func NasMountBase() string { return nasMountBase() }

// UsbMountBase returns the base directory under which USB drives appear.
// Linux: /mnt/USB. Darwin: /Volumes. Windows: stub.
func UsbMountBase() string { return usbMountBase() }

// ListMounts returns the current host mount table, parsed into Mount records.
// Linux reads /proc/mounts; Darwin parses `mount(8)` output; Windows returns
// (nil, ErrUnsupported).
func ListMounts() ([]Mount, error) { return listMounts() }

// SystemHardware returns the hardware/model string for this host (best-effort).
// Linux parses /proc/cpuinfo "Model:" line; Darwin uses `sysctl hw.model`;
// Windows returns "Windows host".
func SystemHardware() string { return systemHardware() }
```

- [ ] **Step 4: Run test (still failing — no platform impl yet)**

```bash
go test ./internal/infra/paths/
```

Expected: FAIL with `undefined: dataDir` (build-tag-selected helpers don't exist yet).

- [ ] **Step 5: Commit shape only (skip — combined with Task 1.2 commit)**

This task leaves the tree non-buildable on purpose; Task 1.2 lands the linux impl that closes the build.

---

### Task 1.2: Implement Linux `paths` (real impl)

**Files:**
- Create: `internal/infra/paths/paths_linux.go`
- Create: `internal/infra/paths/paths_linux_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/infra/paths/paths_linux_test.go
//go:build linux

package paths

import (
	"strings"
	"testing"
)

func TestLinuxDataDir(t *testing.T) {
	if got := dataDir(); got != "/data/stellar" {
		t.Errorf("dataDir() = %q, want /data/stellar", got)
	}
}

func TestLinuxNasMountBase(t *testing.T) {
	if got := nasMountBase(); got != "/mnt/NAS" {
		t.Errorf("nasMountBase() = %q, want /mnt/NAS", got)
	}
}

func TestLinuxUsbMountBase(t *testing.T) {
	if got := usbMountBase(); got != "/mnt/USB" {
		t.Errorf("usbMountBase() = %q, want /mnt/USB", got)
	}
}

func TestLinuxListMountsParses(t *testing.T) {
	// We can't depend on a specific mount existing, but /proc/mounts is
	// guaranteed non-empty on a running Linux system. Assert at least one row
	// with a non-empty mount point.
	mounts, err := listMounts()
	if err != nil {
		t.Fatalf("listMounts() error = %v", err)
	}
	if len(mounts) == 0 {
		t.Fatal("listMounts() returned 0 rows on Linux")
	}
	hasRoot := false
	for _, m := range mounts {
		if m.MountPoint == "/" {
			hasRoot = true
		}
		if m.MountPoint == "" {
			t.Errorf("mount with empty MountPoint: %+v", m)
		}
	}
	if !hasRoot {
		t.Error("expected at least one mount with MountPoint=/")
	}
}

func TestLinuxSystemHardware(t *testing.T) {
	// On any Linux box, /proc/cpuinfo exists. On a Pi the "Model:" line
	// surfaces "Raspberry Pi ..."; on a CI VM it may be empty. Either is OK;
	// we only assert non-empty on Pi and that the function doesn't panic.
	got := systemHardware()
	if strings.Contains(got, "\x00") {
		t.Errorf("systemHardware() = %q contains NUL", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/infra/paths/
```

Expected: FAIL — `dataDir` and friends still undefined.

- [ ] **Step 3: Implement Linux paths**

```go
// internal/infra/paths/paths_linux.go
//go:build linux

package paths

import (
	"bufio"
	"os"
	"strings"
)

func dataDir() string      { return "/data/stellar" }
func cacheDir() string     { return "/data/stellar/cache" }
func nasMountBase() string { return "/mnt/NAS" }
func usbMountBase() string { return "/mnt/USB" }

func listMounts() ([]Mount, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var mounts []Mount
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		mounts = append(mounts, Mount{
			Source:     fields[0],
			MountPoint: fields[1],
			FSType:     fields[2],
		})
	}
	if err := scanner.Err(); err != nil {
		return mounts, err
	}
	return mounts, nil
}

func systemHardware() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Model") {
			if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/infra/paths/
```

Expected: PASS — all 4 cross-platform tests + 4 linux tests green.

- [ ] **Step 5: Defer commit to Task 1.5 (full commit group lands together)**

---

### Task 1.3: Implement Darwin `paths` (real impl)

**Files:**
- Create: `internal/infra/paths/paths_darwin.go`
- Create: `internal/infra/paths/paths_darwin_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/infra/paths/paths_darwin_test.go
//go:build darwin

package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinDataDirUnderHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	want := filepath.Join(home, "Library", "Application Support", "stellar")
	if got := dataDir(); got != want {
		t.Errorf("dataDir() = %q, want %q", got, want)
	}
}

func TestDarwinCacheDirUnderHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	want := filepath.Join(home, "Library", "Caches", "stellar")
	if got := cacheDir(); got != want {
		t.Errorf("cacheDir() = %q, want %q", got, want)
	}
}

func TestDarwinNasMountBase(t *testing.T) {
	if got := nasMountBase(); got != "/Volumes/stellar-nas" {
		t.Errorf("nasMountBase() = %q, want /Volumes/stellar-nas", got)
	}
}

func TestDarwinUsbMountBase(t *testing.T) {
	if got := usbMountBase(); got != "/Volumes" {
		t.Errorf("usbMountBase() = %q, want /Volumes", got)
	}
}

func TestDarwinListMountsParsesMountOutput(t *testing.T) {
	// Substitute a fake exec.Command that returns canned mount(8) output.
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = fakeExecCommand(t, `/dev/disk1s1 on / (apfs, local, journaled)
/dev/disk1s4 on /System/Volumes/VM (apfs, local, noexec)
//user@nas.local/Music on /Volumes/stellar-nas/Music (smbfs, nodev, nosuid)`)

	mounts, err := listMounts()
	if err != nil {
		t.Fatalf("listMounts() error = %v", err)
	}
	if len(mounts) != 3 {
		t.Fatalf("len(mounts) = %d, want 3", len(mounts))
	}
	if !strings.Contains(mounts[2].FSType, "smbfs") {
		t.Errorf("mounts[2].FSType = %q, want contains smbfs", mounts[2].FSType)
	}
	if mounts[2].MountPoint != "/Volumes/stellar-nas/Music" {
		t.Errorf("mounts[2].MountPoint = %q, want /Volumes/stellar-nas/Music", mounts[2].MountPoint)
	}
}

func TestDarwinSystemHardwareViaSysctl(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = fakeExecCommand(t, "Macmini9,1\n")

	if got := systemHardware(); got != "Macmini9,1" {
		t.Errorf("systemHardware() = %q, want Macmini9,1", got)
	}
}
```

The `fakeExecCommand` helper lives in a shared test file added next step.

- [ ] **Step 2: Add shared darwin test helper**

```go
// internal/infra/paths/exec_fake_darwin_test.go
//go:build darwin

package paths

import (
	"context"
	"os/exec"
	"testing"
)

// fakeExecCommand returns a CommandContext substitute that runs `echo` with
// the canned output, ignoring the requested command. Used to feed canned
// `mount` / `sysctl` output to listMounts / systemHardware without touching
// the host's real mount table.
func fakeExecCommand(t *testing.T, stdout string) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	t.Helper()
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "echo", "-n", stdout)
	}
}
```

- [ ] **Step 3: Implement Darwin paths**

```go
// internal/infra/paths/paths_darwin.go
//go:build darwin

package paths

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// execCommand is a package-level indirection so tests can substitute canned
// `mount` / `sysctl` output. Same pattern as internal/domain/sources.
var execCommand = exec.CommandContext

func dataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "stellar")
}

func cacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Caches", "stellar")
}

func nasMountBase() string { return "/Volumes/stellar-nas" }
func usbMountBase() string { return "/Volumes" }

// listMounts parses `mount(8)` output. Format per row:
//   <source> on <mountpoint> (<fstype>, <opt>, ...)
// Example:
//   /dev/disk1s1 on / (apfs, local, journaled)
//   //user@nas/Music on /Volumes/Music (smbfs, nodev)
func listMounts() ([]Mount, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := execCommand(ctx, "/sbin/mount").Output()
	if err != nil {
		return nil, err
	}

	var mounts []Mount
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Split on " on " first to get source / rest.
		onIdx := strings.Index(line, " on ")
		if onIdx < 0 {
			continue
		}
		source := line[:onIdx]
		rest := line[onIdx+4:]
		// rest is "<mountpoint> (<fstype>, ...)"
		parenIdx := strings.LastIndex(rest, " (")
		if parenIdx < 0 {
			continue
		}
		mountPoint := rest[:parenIdx]
		opts := strings.TrimSuffix(rest[parenIdx+2:], ")")
		fsType := strings.SplitN(opts, ",", 2)[0]
		mounts = append(mounts, Mount{
			Source:     source,
			MountPoint: mountPoint,
			FSType:     fsType,
		})
	}
	return mounts, nil
}

func systemHardware() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := execCommand(ctx, "/usr/sbin/sysctl", "-n", "hw.model").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

- [ ] **Step 4: Run tests on macOS**

```bash
go test ./internal/infra/paths/
```

Expected: PASS — 4 cross-platform + 5 darwin tests green.

- [ ] **Step 5: Defer commit to Task 1.5**

---

### Task 1.4: Implement Windows `paths` (stub)

**Files:**
- Create: `internal/infra/paths/paths_windows.go`

No tests for windows in this phase; `go vet` + `go build` validate the stub.

- [ ] **Step 1: Implement Windows stub**

```go
// internal/infra/paths/paths_windows.go
//go:build windows

package paths

import (
	"os"
	"path/filepath"
)

func dataDir() string {
	if appdata := os.Getenv("LOCALAPPDATA"); appdata != "" {
		return filepath.Join(appdata, "stellar")
	}
	return filepath.Join(os.TempDir(), "stellar")
}

func cacheDir() string { return filepath.Join(dataDir(), "cache") }

func nasMountBase() string { return "" }
func usbMountBase() string { return "" }

func listMounts() ([]Mount, error)     { return nil, ErrUnsupported }
func systemHardware() string           { return "Windows host" }
```

- [ ] **Step 2: Verify cross-compile**

```bash
GOOS=windows GOARCH=amd64 go vet ./internal/infra/paths/
```

Expected: clean (exit 0).

- [ ] **Step 3: Defer commit to Task 1.5**

---

### Task 1.5: Replace callsites + commit Group 1

**Files:**
- Modify: `internal/domain/sources/service.go:18-21` (drop `NasMountBase`/`UsbMountBase` consts)
- Modify: `internal/domain/sources/service.go` (replace `NasMountBase` reads with `paths.NasMountBase()`)
- Modify: `internal/domain/localmusic/classifier.go:157` (replace `os.Open("/proc/mounts")` with `paths.ListMounts()`)
- Modify: `cmd/stellar/main.go:97` (`/data/stellar` → `paths.DataDir()`)
- Modify: `cmd/stellar/main.go:161` (same)

- [ ] **Step 1: Capture pre-change Pi behaviour**

```bash
ssh stellar.lan "curl -s http://localhost:3000/api/v1/network" > /tmp/m1a-pre-network.json
ssh stellar.lan "curl -s http://localhost:3000/api/v1/version" > /tmp/m1a-pre-version.json
cat /tmp/m1a-pre-network.json
```

Expected: JSON payloads captured.

- [ ] **Step 2: Edit `internal/domain/sources/service.go`**

Drop lines 18-21 (`NasMountBase`, `UsbMountBase` consts) and replace every `NasMountBase` / `UsbMountBase` read with `paths.NasMountBase()` / `paths.UsbMountBase()`. Add import for `paths`:

```go
import (
	// ... existing imports ...
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/paths"
)

// (old const block removed)

// Example replacement at service.go:109:
mountPoint := filepath.Join(paths.NasMountBase(), sanitizeName(req.Name))
```

Apply the same `paths.NasMountBase()` substitution at lines 109, 192, 224, 257, 317, 400, 551, 596 of `service.go`.

- [ ] **Step 3: Edit `internal/domain/sources/service_test.go:1159`**

Replace `NasMountBase` reference with `paths.NasMountBase()`. Add `paths` import.

- [ ] **Step 4: Edit `internal/domain/localmusic/classifier.go`**

At line 137-160, replace direct `os.Open("/proc/mounts")` with `paths.ListMounts()`. The function `loadMounts` becomes:

```go
import (
	// ... existing ...
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/paths"
)

func (c *Classifier) loadMounts() []mountEntry {
	mounts, err := paths.ListMounts()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to read mounts, assuming all paths are local")
		return nil
	}
	entries := make([]mountEntry, 0, len(mounts))
	for _, m := range mounts {
		entries = append(entries, mountEntry{
			source:     m.Source,
			mountPoint: m.MountPoint,
			fsType:     m.FSType,
		})
	}
	return entries
}
```

Read `classifier.go` lines 137-200 first to preserve the existing `mountEntry` struct fields exactly.

- [ ] **Step 5: Edit `cmd/stellar/main.go`**

Replace lines 97 and 161:

```go
// Line 97 (was: sourcesConfigPath := filepath.Join("/data/stellar", "sources.json"))
sourcesConfigPath := filepath.Join(paths.DataDir(), "sources.json")

// Line 161 (was: localMusicDataDir := filepath.Join("/data/stellar"))
localMusicDataDir := paths.DataDir()
```

Add `paths` import at the top of main.go:

```go
"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/paths"
```

- [ ] **Step 6: Verify host build + all tests**

```bash
go build ./...
go test ./...
```

Expected: build clean, all existing tests pass (plus the new `paths` tests).

- [ ] **Step 7: Verify Pi cross-compile**

```bash
make build
```

Expected: `bin/stellar-arm64` produced.

- [ ] **Step 8: Commit**

```bash
git add internal/infra/paths/ \
        internal/domain/sources/service.go internal/domain/sources/service_test.go \
        internal/domain/localmusic/classifier.go \
        cmd/stellar/main.go
git commit -m "$(cat <<'EOF'
refactor(infra): introduce internal/infra/paths package

Centralise /data/stellar, /mnt/NAS, /mnt/USB, /proc/mounts, /proc/cpuinfo
reads behind a build-tag-selected portable API. Replaces hardcoded Linux
paths in cmd/stellar/main.go, internal/domain/sources/service.go, and
internal/domain/localmusic/classifier.go with paths.DataDir(),
paths.NasMountBase(), paths.UsbMountBase(), paths.ListMounts(), and
paths.SystemHardware(). Darwin impl returns macOS-conventional paths and
parses mount(8) output; Windows impl returns LOCALAPPDATA-anchored
defaults and ErrUnsupported for listMounts.

First commit of M1.A backend portability layer. No behaviour change on
Linux. Spec: docs/superpowers/specs/2026-05-13-m1a-backend-portability-design.md
EOF
)"
```

---

## Commit Group 2 — `internal/infra/lcd` package + Makefile cross-compile targets

Moves LCD control out of the transport layer into an infra package with build-tag-selected platform impls. Adds `build-darwin` and `build-windows` Makefile targets — the latter validates the entire abstraction by producing a real Windows binary, not just `go vet`.

### Task 2.1: Define `lcd` package interface

**Files:**
- Create: `internal/infra/lcd/lcd.go`
- Create: `internal/infra/lcd/interface_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/infra/lcd/interface_test.go
package lcd

import (
	"errors"
	"testing"
)

func TestStatusZeroValue(t *testing.T) {
	var s Status
	if s.IsOn {
		t.Error("Status zero-value IsOn should be false")
	}
}

func TestErrUnsupportedSentinel(t *testing.T) {
	if !errors.Is(ErrUnsupported, ErrUnsupported) {
		t.Error("ErrUnsupported should satisfy errors.Is against itself")
	}
}

func TestNewPlatformReturnsNonNil(t *testing.T) {
	c := NewPlatform()
	if c == nil {
		t.Fatal("NewPlatform() returned nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/infra/lcd/
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement the interface file**

```go
// internal/infra/lcd/lcd.go
// Package lcd controls the LCD display attached to the host (real impl on
// Linux; stubs on darwin/windows where no LCD is attached to the backend
// host). Handler registration is exposed via RegisterHandlers so the
// transport layer does not need to know about LCD internals.
package lcd

import "errors"

// ErrUnsupported is returned by platforms that have no LCD hardware.
var ErrUnsupported = errors.New("lcd: not supported on this platform")

// Status describes the LCD's current power state.
type Status struct {
	IsOn bool `json:"isOn"`
}

// Controller is the platform-agnostic LCD power interface.
type Controller interface {
	// Status reads the LCD's current power state.
	Status() (Status, error)
	// Set turns the LCD on or off. Returns ErrUnsupported on hostless platforms.
	Set(on bool) error
}

// Broadcaster lets the lcd package emit Socket.IO events without importing
// the transport package. The transport layer implements this interface.
type Broadcaster interface {
	Emit(event string, payload any)
}

// HandlerRegistrar lets the lcd package register request/response handlers
// (Socket.IO acknowledgement-style) without importing the transport package.
type HandlerRegistrar interface {
	OnRequest(event string, handler func(args ...any) any)
}

// NewPlatform returns the Controller implementation for the current build
// platform (build-tag-selected).
func NewPlatform() Controller { return newPlatform() }
```

- [ ] **Step 4: Run test — still failing without platform impl**

```bash
go test ./internal/infra/lcd/
```

Expected: FAIL with `undefined: newPlatform`.

- [ ] **Step 5: Defer commit to Task 2.5**

---

### Task 2.2: Implement Linux `lcd` (move from `socketio/lcd.go`)

**Files:**
- Create: `internal/infra/lcd/lcd_linux.go` (body moved from `internal/transport/socketio/lcd.go`)
- Create: `internal/infra/lcd/lcd_linux_test.go`

- [ ] **Step 1: Create `lcd_linux.go` with the moved body**

Open `internal/transport/socketio/lcd.go` and copy the *content* (not the `Server.BroadcastLCDStatus` method at the bottom — that becomes part of `handlers.go` in Task 2.4). The new file:

```go
// internal/infra/lcd/lcd_linux.go
//go:build linux

package lcd

import (
	"os"
	"os/exec"
	"strings"

	"github.com/rs/zerolog/log"
)

// linuxController is the production Linux LCD controller. It falls back
// through backlight sysfs → DRM DPMS → vcgencmd → wlr-randr → xrandr/xset.
type linuxController struct{}

func newPlatform() Controller { return &linuxController{} }

func (c *linuxController) Status() (Status, error) {
	return getLCDStatus(), nil
}

func (c *linuxController) Set(on bool) error {
	return setLCDPower(on)
}

// --- The rest of this file is a verbatim move of the body of
//     internal/transport/socketio/lcd.go MINUS the `Server.BroadcastLCDStatus`
//     method at the bottom (lines 357-362 of the original), AND with:
//       - top-level type `LCDStatus` renamed to a local helper that returns
//         the new package's `Status` struct
//       - exported top-level funcs `GetLCDStatus` / `SetLCDPower` lowercased
//         to package-private `getLCDStatus` / `setLCDPower`
//     Helper funcs (isWaylandSession, getWaylandEnv, getDRMDisplayPath,
//     getLCDStatusWayland, getBacklightPath, setLCDPowerBacklight,
//     setLCDPowerDPMS, setLCDPowerWayland) move unchanged. ---

// getLCDStatus returns the current LCD display status. (was: GetLCDStatus)
func getLCDStatus() Status {
	status := Status{IsOn: true}

	blPath := getBacklightPath()
	if blPath != "" {
		data, err := os.ReadFile(blPath)
		if err == nil {
			val := strings.TrimSpace(string(data))
			if val == "1" {
				status.IsOn = false
			}
			log.Debug().Str("bl_power", val).Bool("isOn", status.IsOn).Msg("LCD status from backlight sysfs")
			return status
		}
	}

	if isWaylandSession() {
		if wlStatus, ok := getLCDStatusWayland(); ok {
			return wlStatus
		}
		log.Debug().Msg("Wayland detected but wlr-randr failed, trying fallbacks")
	}

	drmPath := getDRMDisplayPath()
	if drmPath != "" {
		data, err := os.ReadFile(drmPath)
		if err == nil {
			dpmsState := strings.TrimSpace(string(data))
			if dpmsState == "Off" || dpmsState == "Standby" || dpmsState == "Suspend" {
				status.IsOn = false
			}
			return status
		}
		log.Debug().Err(err).Str("path", drmPath).Msg("Failed to read DRM DPMS")
	}

	out, err := exec.Command("vcgencmd", "display_power").Output()
	if err != nil {
		log.Debug().Err(err).Msg("Failed to get LCD status via vcgencmd")
		return status
	}

	output := strings.TrimSpace(string(out))
	if strings.Contains(output, "=0") {
		status.IsOn = false
	}
	return status
}

// setLCDPower turns the LCD display on or off. (was: SetLCDPower)
func setLCDPower(on bool) error {
	if err := setLCDPowerBacklight(on); err == nil {
		return nil
	}
	log.Debug().Msg("Backlight sysfs not available, trying DRM DPMS")

	if err := setLCDPowerDPMS(on); err == nil {
		return nil
	}
	log.Debug().Msg("DRM DPMS failed, trying other methods")

	value := "0"
	if on {
		value = "1"
	}
	cmd := exec.Command("vcgencmd", "display_power", value)
	if err := cmd.Run(); err == nil {
		log.Info().Bool("on", on).Msg("LCD power changed via vcgencmd")
		return nil
	}
	log.Debug().Msg("vcgencmd failed, trying Wayland methods")

	if isWaylandSession() {
		if err := setLCDPowerWayland(on); err == nil {
			return nil
		}
		log.Debug().Msg("Wayland wlr-randr failed")
	}

	drmPath := getDRMDisplayPath()
	if drmPath != "" {
		display := "HDMI-A-1"
		mode := "off"
		if on {
			mode = "on"
		}
		dpmsMode := "Off"
		if on {
			dpmsMode = "On"
		}
		cmd := exec.Command("xrandr", "--output", display, "--set", "DPMS", dpmsMode)
		cmd.Env = append(os.Environ(), "DISPLAY=:0")
		if err := cmd.Run(); err != nil {
			cmd = exec.Command("xset", "dpms", "force", mode)
			cmd.Env = append(os.Environ(), "DISPLAY=:0")
			if err := cmd.Run(); err != nil {
				log.Debug().Err(err).Bool("on", on).Msg("X11 methods failed")
			} else {
				log.Info().Bool("on", on).Msg("LCD power changed via xset")
				return nil
			}
		} else {
			log.Info().Bool("on", on).Msg("LCD power changed via xrandr")
			return nil
		}
	}

	log.Error().Bool("on", on).Msg("All LCD power methods failed")
	return os.ErrNotExist
}

// --- helper functions (unchanged from original socketio/lcd.go) ---

func isWaylandSession() bool {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return true
	}
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		return true
	}
	if _, err := exec.LookPath("wlr-randr"); err == nil {
		cmd := exec.Command("wlr-randr")
		cmd.Env = getWaylandEnv()
		if err := cmd.Run(); err == nil {
			return true
		}
	}
	return false
}

func getWaylandEnv() []string {
	env := os.Environ()
	hasXdgRuntime := false
	hasWaylandDisplay := false
	for _, e := range env {
		if strings.HasPrefix(e, "XDG_RUNTIME_DIR=") {
			hasXdgRuntime = true
		}
		if strings.HasPrefix(e, "WAYLAND_DISPLAY=") {
			hasWaylandDisplay = true
		}
	}
	if !hasXdgRuntime {
		env = append(env, "XDG_RUNTIME_DIR=/run/user/1000")
	}
	if !hasWaylandDisplay {
		env = append(env, "WAYLAND_DISPLAY=wayland-0")
	}
	return env
}

func getDRMDisplayPath() string {
	paths := []string{
		"/sys/class/drm/card1-HDMI-A-1/dpms",
		"/sys/class/drm/card0-HDMI-A-1/dpms",
		"/sys/class/drm/card1-HDMI-A-2/dpms",
		"/sys/class/drm/card0-HDMI-A-2/dpms",
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func getLCDStatusWayland() (Status, bool) {
	status := Status{IsOn: true}
	cmd := exec.Command("wlr-randr")
	cmd.Env = getWaylandEnv()
	out, err := cmd.Output()
	if err != nil {
		log.Debug().Err(err).Msg("wlr-randr failed")
		return status, false
	}
	output := string(out)
	lines := strings.Split(output, "\n")
	inHDMI := false
	for _, line := range lines {
		if strings.HasPrefix(line, "HDMI-A-1") {
			inHDMI = true
			continue
		}
		if inHDMI && strings.Contains(line, "Enabled:") {
			if strings.Contains(line, "no") {
				status.IsOn = false
			}
			log.Debug().Bool("isOn", status.IsOn).Msg("LCD status from wlr-randr")
			return status, true
		}
		if inHDMI && !strings.HasPrefix(line, " ") && line != "" {
			break
		}
	}
	if inHDMI {
		return status, true
	}
	return status, false
}

func getBacklightPath() string {
	candidates := []string{
		"/sys/class/backlight/rpi_backlight/bl_power",
		"/sys/class/backlight/10-0045/bl_power",
		"/sys/class/backlight/4-0045/bl_power",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func setLCDPowerBacklight(on bool) error {
	blPath := getBacklightPath()
	if blPath == "" {
		return os.ErrNotExist
	}
	value := "1"
	if on {
		value = "0"
	}
	err := os.WriteFile(blPath, []byte(value), 0644)
	if err != nil {
		cmd := exec.Command("sudo", "sh", "-c", "echo "+value+" > "+blPath)
		output, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			log.Error().Err(cmdErr).Str("output", string(output)).Bool("on", on).Msg("Backlight write failed")
			return cmdErr
		}
	}
	log.Info().Bool("on", on).Str("path", blPath).Msg("LCD power changed via backlight sysfs (touch stays active)")
	return nil
}

func setLCDPowerDPMS(on bool) error {
	drmPath := getDRMDisplayPath()
	if drmPath == "" {
		return os.ErrNotExist
	}
	value := "Off"
	if on {
		value = "On"
	}
	err := os.WriteFile(drmPath, []byte(value), 0644)
	if err != nil {
		cmd := exec.Command("sudo", "sh", "-c", "echo "+value+" > "+drmPath)
		output, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			log.Error().Err(cmdErr).Str("output", string(output)).Bool("on", on).Msg("DPMS write failed")
			return cmdErr
		}
	}
	log.Info().Bool("on", on).Str("path", drmPath).Msg("LCD power changed via DRM DPMS")
	return nil
}

func setLCDPowerWayland(on bool) error {
	mode := "--off"
	if on {
		mode = "--on"
	}
	cmd := exec.Command("wlr-randr", "--output", "HDMI-A-1", mode)
	cmd.Env = getWaylandEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("output", string(output)).Bool("on", on).Msg("wlr-randr failed")
		return err
	}
	log.Info().Bool("on", on).Msg("LCD power changed via wlr-randr (Wayland)")
	return nil
}
```

- [ ] **Step 2: Add a smoke test that exercises Status() on Linux**

```go
// internal/infra/lcd/lcd_linux_test.go
//go:build linux

package lcd

import "testing"

func TestLinuxControllerStatus(t *testing.T) {
	c := newPlatform()
	status, err := c.Status()
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	// On any Linux host, Status returns IsOn=true by default when no LCD
	// is detected (the function defaults to true and only flips to false
	// when a known backlight/DPMS path indicates so).
	_ = status
}
```

- [ ] **Step 3: Run tests on Pi-targeted build**

```bash
GOOS=linux GOARCH=arm64 go vet ./internal/infra/lcd/
go test ./internal/infra/lcd/
```

Expected: vet clean, host test passes (assuming developer is on darwin, the linux test file is excluded by build tag — that's fine).

- [ ] **Step 4: Defer commit to Task 2.5**

---

### Task 2.3: Implement Darwin + Windows `lcd` stubs

**Files:**
- Create: `internal/infra/lcd/lcd_darwin.go`
- Create: `internal/infra/lcd/lcd_windows.go`
- Create: `internal/infra/lcd/lcd_darwin_test.go`

- [ ] **Step 1: Write the failing darwin test**

```go
// internal/infra/lcd/lcd_darwin_test.go
//go:build darwin

package lcd

import (
	"errors"
	"testing"
)

func TestDarwinStubReturnsOn(t *testing.T) {
	c := newPlatform()
	status, err := c.Status()
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.IsOn {
		t.Error("darwin stub Status should report IsOn=true (no LCD attached, default-on)")
	}
}

func TestDarwinStubSetReturnsUnsupported(t *testing.T) {
	c := newPlatform()
	if err := c.Set(false); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Set(false) error = %v, want ErrUnsupported", err)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

```bash
go test ./internal/infra/lcd/
```

Expected: FAIL — `newPlatform` undefined on darwin.

- [ ] **Step 3: Implement darwin + windows stubs**

```go
// internal/infra/lcd/lcd_darwin.go
//go:build darwin

package lcd

type darwinController struct{}

func newPlatform() Controller                { return &darwinController{} }
func (c *darwinController) Status() (Status, error) { return Status{IsOn: true}, nil }
func (c *darwinController) Set(on bool) error       { return ErrUnsupported }
```

```go
// internal/infra/lcd/lcd_windows.go
//go:build windows

package lcd

type windowsController struct{}

func newPlatform() Controller                  { return &windowsController{} }
func (c *windowsController) Status() (Status, error) { return Status{IsOn: true}, nil }
func (c *windowsController) Set(on bool) error       { return ErrUnsupported }
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/infra/lcd/
GOOS=windows GOARCH=amd64 go vet ./internal/infra/lcd/
```

Expected: darwin tests pass; windows vet clean.

- [ ] **Step 5: Defer commit to Task 2.5**

---

### Task 2.4: Add `lcd` handlers + Makefile targets + wire main.go + delete `socketio/lcd.go`

**Files:**
- Create: `internal/infra/lcd/handlers.go`
- Modify: `internal/transport/socketio/server.go` (add `Broadcaster`/`HandlerRegistrar` adapters; remove `BroadcastLCDStatus` method; remove inline LCD handlers at lines 274, 585-604)
- Delete: `internal/transport/socketio/lcd.go`
- Modify: `cmd/stellar/main.go` (call `lcd.RegisterHandlers`)
- Modify: `Makefile` (add `build-darwin` + `build-windows` targets)

- [ ] **Step 1: Implement `internal/infra/lcd/handlers.go`**

```go
// internal/infra/lcd/handlers.go
package lcd

import "github.com/rs/zerolog/log"

// RegisterHandlers wires the LCD package into the transport layer.
// The transport package provides reg (request/response handler registrar) and
// brd (event broadcaster). c is the platform-selected Controller.
//
// Registered Socket.IO request handlers:
//   - getLcdStatus   → returns Status as JSON
//   - lcdOff         → calls Set(false), broadcasts pushLcdStatus
//   - lcdOn          → calls Set(true), broadcasts pushLcdStatus
//
// Registered emit on broadcast: pushLcdStatus
func RegisterHandlers(reg HandlerRegistrar, brd Broadcaster, c Controller) {
	reg.OnRequest("getLcdStatus", func(args ...any) any {
		status, _ := c.Status()
		return status
	})

	reg.OnRequest("lcdOff", func(args ...any) any {
		if err := c.Set(false); err != nil {
			log.Error().Err(err).Msg("LCD off failed")
			return map[string]any{"success": false, "error": err.Error()}
		}
		BroadcastStatus(brd, c)
		return map[string]any{"success": true}
	})

	reg.OnRequest("lcdOn", func(args ...any) any {
		if err := c.Set(true); err != nil {
			log.Error().Err(err).Msg("LCD on failed")
			return map[string]any{"success": false, "error": err.Error()}
		}
		BroadcastStatus(brd, c)
		return map[string]any{"success": true}
	})
}

// BroadcastStatus reads the LCD status and emits it via the Broadcaster.
// Exposed for callers (e.g., the connection-open handler in the transport
// layer) that want to push current status without going through a request.
func BroadcastStatus(brd Broadcaster, c Controller) {
	status, _ := c.Status()
	brd.Emit("pushLcdStatus", status)
	log.Debug().Bool("isOn", status.IsOn).Msg("Broadcast LCD status")
}
```

- [ ] **Step 2: Add adapters + remove old LCD wiring in `internal/transport/socketio/server.go`**

Read `server.go` first to find the exact connection-open handler (around line 272-274) and the LCD request handlers (around line 585-604). Then:

(a) Add at the bottom of `server.go` (or in a new tiny file `server_adapters.go` if you prefer) the two adapter types:

```go
// Adapter satisfying lcd.Broadcaster / netinfo.Broadcaster / sources.Broadcaster.
// Routes Emit calls to the Socket.IO server's global emit.
type serverBroadcaster struct{ s *Server }

func (b serverBroadcaster) Emit(event string, payload any) {
	b.s.io.Emit(event, payload)
}

// Adapter satisfying lcd.HandlerRegistrar / netinfo.HandlerRegistrar.
// Routes OnRequest calls to the Socket.IO server's per-client request handlers.
type serverRegistrar struct{ s *Server }

func (r serverRegistrar) OnRequest(event string, handler func(args ...any) any) {
	r.s.registerRequestHandler(event, handler)
}

// Broadcaster returns a Broadcaster bound to this server.
func (s *Server) Broadcaster() serverBroadcaster { return serverBroadcaster{s} }

// Registrar returns a HandlerRegistrar bound to this server.
func (s *Server) Registrar() serverRegistrar { return serverRegistrar{s} }
```

You will need to expose or add `Server.registerRequestHandler(event, handler)`. Read the existing handler registration shape in `server.go` to find the matching primitive — most likely the existing per-event handler block where `getLcdStatus`/`lcdOff`/`lcdOn` are registered today. Factor that mechanism into a single `registerRequestHandler` method if it doesn't already exist.

(b) Delete from `server.go`:
- The inline LCD-related handlers in the `getLcdStatus`/`lcdOff`/`lcdOn` block (around lines 585-604)
- The `client.Emit("pushLcdStatus", GetLCDStatus())` line in the connect handler (around line 274)
- Any other direct references to `GetLCDStatus`/`SetLCDPower`/`BroadcastLCDStatus`

The connect-handler emit becomes wired via `lcd.BroadcastStatus(s.Broadcaster(), s.lcdController)` instead — see Step 3.

(c) Add a field to the `Server` struct:

```go
type Server struct {
	// ... existing fields ...
	lcdController lcd.Controller
}
```

- [ ] **Step 3: Wire `lcd` from `cmd/stellar/main.go`**

After the `socketServer` is created (search for `socketio.NewServer` to find the existing call site), and after `paths.DataDir()` is in scope:

```go
import (
	// ... existing ...
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/lcd"
)

// Right after socketServer is constructed:
lcdController := lcd.NewPlatform()
socketServer.SetLCDController(lcdController) // see step 2c — add this setter
lcd.RegisterHandlers(socketServer.Registrar(), socketServer.Broadcaster(), lcdController)
```

Also update the connection-open handler in `server.go` to call:

```go
lcd.BroadcastStatus(s.Broadcaster(), s.lcdController)
```

at the spot that previously did `client.Emit("pushLcdStatus", GetLCDStatus())`.

- [ ] **Step 4: Delete `internal/transport/socketio/lcd.go`**

```bash
git rm internal/transport/socketio/lcd.go
```

- [ ] **Step 5: Add Makefile targets**

Open the existing `Makefile` and add after the `build-local` target:

```makefile
# Cross-compilation for macOS (Darwin ARM64) — M1.A target
DARWIN_BINARY := $(BIN_DIR)/stellar-darwin-arm64

## build-darwin: Cross-compile for macOS (ARM64) — M1.A portability target
build-darwin:
	@echo "Cross-compiling for macOS (Darwin ARM64)..."
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=arm64 \
		$(GO) build -ldflags "$(LDFLAGS)" -o $(DARWIN_BINARY) ./$(CMD_DIR)
	@echo "Binary built: $(DARWIN_BINARY)"

# Cross-compilation for Windows (AMD64) — M1.A target. Stubs only on Windows;
# this target exists to catch linker errors and missing-impl gaps that go vet
# alone misses.
WINDOWS_BINARY := $(BIN_DIR)/stellar-windows-amd64.exe

## build-windows: Cross-compile for Windows (AMD64) — M1.A portability target
build-windows:
	@echo "Cross-compiling for Windows (AMD64)..."
	@mkdir -p $(BIN_DIR)
	GOOS=windows GOARCH=amd64 \
		$(GO) build -ldflags "$(LDFLAGS)" -o $(WINDOWS_BINARY) ./$(CMD_DIR)
	@echo "Binary built: $(WINDOWS_BINARY)"
```

Also append `build-darwin build-windows` to the `.PHONY` list at the top.

- [ ] **Step 6: Verify all three builds compile**

```bash
make build           # Pi ARM64
make build-darwin    # macOS ARM64
make build-windows   # Windows AMD64
go test ./...
```

Expected: all three binaries produced under `bin/`; all tests green.

- [ ] **Step 7: Commit**

```bash
git add internal/infra/lcd/ internal/transport/socketio/server.go cmd/stellar/main.go Makefile
git rm internal/transport/socketio/lcd.go
git commit -m "$(cat <<'EOF'
refactor(lcd): extract LCD control into internal/infra/lcd

Move 362 lines of LCD logic out of internal/transport/socketio/lcd.go into
internal/infra/lcd with build-tag-selected impls: real Linux (wlr-randr +
DPMS + backlight sysfs + vcgencmd + xrandr fallback chain unchanged), darwin
stub (Status returns IsOn=true, Set returns ErrUnsupported), windows stub
(same shape as darwin).

Introduce Broadcaster + HandlerRegistrar interfaces so the lcd package can
emit Socket.IO events and register request handlers without importing the
zishang520 transport library. Server.Broadcaster() / Server.Registrar()
adapters in the socketio package fulfil those interfaces.

Add Makefile targets build-darwin (cross-compile darwin/arm64) and
build-windows (cross-compile windows/amd64). Windows is a stub-only target
this phase, but producing a real binary catches linker / missing-impl
errors that go vet alone misses.

Second commit of M1.A. Spec: docs/superpowers/specs/2026-05-13-m1a-backend-portability-design.md
EOF
)"
```

---

## Commit Group 3a — `internal/infra/netinfo` move (keep both impls)

Creates the new package shape and moves both existing Linux network impls into it *unchanged*. Routes the REST `/api/v1/network` handler through one impl and Socket.IO `pushNetworkStatus` through the other (preserving current behaviour). Captures pre-change JSON payloads as test fixtures so Commit 3b's dedup is byte-equality verifiable.

### Task 3a.1: Define `netinfo` package + capture fixtures

**Files:**
- Create: `internal/infra/netinfo/netinfo.go`
- Create: `internal/infra/netinfo/interface_test.go`
- Create: `internal/infra/netinfo/fixture_test.go`
- Create: `internal/infra/netinfo/testdata/network_pi_pre.json` (from live Pi capture in Step 1)

- [ ] **Step 1: Capture live Pi payloads**

```bash
mkdir -p internal/infra/netinfo/testdata
ssh stellar.lan "curl -s http://localhost:3000/api/v1/network" > internal/infra/netinfo/testdata/rest_pre.json
# Capture the Socket.IO emit shape via a one-shot node client (template — adjust to repo's existing helper):
node -e "
const io = require('socket.io-client');
const s = io('http://stellar.lan:3000');
s.on('pushNetworkStatus', p => { console.log(JSON.stringify(p)); s.close(); process.exit(0); });
setTimeout(() => { console.error('timeout'); process.exit(1); }, 5000);
" > internal/infra/netinfo/testdata/push_pre.json
cat internal/infra/netinfo/testdata/rest_pre.json internal/infra/netinfo/testdata/push_pre.json
```

Expected: two JSON files with NetworkStatus-shaped payloads (fields: type, ssid, signal, ip, strength). They may differ slightly between the two impls; that's the entire reason for the dedup commit.

- [ ] **Step 2: Define the interface**

```go
// internal/infra/netinfo/netinfo.go
// Package netinfo reports the host's current network connection state and
// broadcasts changes over Socket.IO. Real impls on linux + darwin; stub on
// windows.
package netinfo

import (
	"context"
	"errors"
)

// ErrUnsupported is returned by platforms that cannot enumerate network state.
var ErrUnsupported = errors.New("netinfo: not supported on this platform")

// Status describes the host's primary network interface state.
//
// Field semantics are wire-stable: the JSON shape MUST match the legacy
// socketio package's NetworkStatus and the legacy main.go NetworkStatus,
// because frontend clients depend on it.
type Status struct {
	Type     string `json:"type"`     // "wifi" | "ethernet" | "none"
	SSID     string `json:"ssid"`     // wifi network name (empty for ethernet/none)
	Signal   int    `json:"signal"`   // wifi signal 0-100 (100 for ethernet)
	IP       string `json:"ip"`       // primary IPv4 address
	Strength int    `json:"strength"` // signal strength level 0-3 (for icon)
}

// Reporter is the platform-agnostic network-state reader.
type Reporter interface {
	GetStatus() Status
}

// Broadcaster + HandlerRegistrar — same shape as the lcd package.
type Broadcaster interface {
	Emit(event string, payload any)
}

type HandlerRegistrar interface {
	OnRequest(event string, handler func(args ...any) any)
}

// NewPlatform returns the Reporter implementation for the current platform.
func NewPlatform() Reporter { return newPlatform() }

// StartWatcher runs a periodic poll (every 30s) and Emits "pushNetworkStatus"
// when the status changes. Blocks until ctx is canceled. The transport layer
// is expected to call this in a goroutine.
func StartWatcher(ctx context.Context, r Reporter, brd Broadcaster) {
	startWatcher(ctx, r, brd)
}
```

- [ ] **Step 3: Add cross-platform interface test**

```go
// internal/infra/netinfo/interface_test.go
package netinfo

import (
	"errors"
	"testing"
)

func TestStatusZeroValue(t *testing.T) {
	var s Status
	if s.Type != "" || s.Signal != 0 || s.Strength != 0 {
		t.Errorf("Status zero value not all-zero: %+v", s)
	}
}

func TestErrUnsupportedSentinel(t *testing.T) {
	if !errors.Is(ErrUnsupported, ErrUnsupported) {
		t.Error("ErrUnsupported should satisfy errors.Is against itself")
	}
}

func TestNewPlatformReturnsNonNil(t *testing.T) {
	r := NewPlatform()
	if r == nil {
		t.Fatal("NewPlatform() returned nil")
	}
}
```

- [ ] **Step 4: Add fixture-byte-equality test scaffold**

```go
// internal/infra/netinfo/fixture_test.go
package netinfo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// readFixture loads testdata/<name> and returns it as a Status. Fails the
// test if the file is missing or malformed.
func readFixture(t *testing.T, name string) Status {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal fixture %q: %v", name, err)
	}
	return s
}

// TestFixturesLoad asserts the captured pre-state JSON files exist and
// unmarshal cleanly into Status. Commit 3b will add a separate test that
// asserts the canonical impl produces byte-equal output for the same
// host conditions.
func TestFixturesLoad(t *testing.T) {
	rest := readFixture(t, "rest_pre.json")
	push := readFixture(t, "push_pre.json")
	if rest.Type == "" {
		t.Error("rest fixture has empty Type")
	}
	if push.Type == "" {
		t.Error("push fixture has empty Type")
	}
}
```

- [ ] **Step 5: Defer commit to Task 3a.5**

---

### Task 3a.2: Move `socketio/network.go` impl into `netinfo_linux.go`

**Files:**
- Create: `internal/infra/netinfo/netinfo_linux.go` (verbatim move of `socketio/network.go` body — see step 1)

- [ ] **Step 1: Create `netinfo_linux.go`**

```go
// internal/infra/netinfo/netinfo_linux.go
//go:build linux

package netinfo

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// linuxReporter is the canonical Linux Reporter (was: socketio.GetNetworkStatus).
type linuxReporter struct{}

func newPlatform() Reporter { return &linuxReporter{} }

func (r *linuxReporter) GetStatus() Status {
	return getStatusFromSysClassNet()
}

// getStatusFromSysClassNet is the body of socketio/network.go:GetNetworkStatus
// moved verbatim, with the only change being NetworkStatus→Status type rename.
func getStatusFromSysClassNet() Status {
	status := Status{
		Type:     "none",
		Signal:   0,
		Strength: 0,
	}

	for _, iface := range []string{"eth0", "end0"} {
		carrierPath := "/sys/class/net/" + iface + "/carrier"
		if data, err := os.ReadFile(carrierPath); err == nil {
			if strings.TrimSpace(string(data)) == "1" {
				status.Type = "ethernet"
				status.IP = getIPAddress(iface)
				status.Signal = 100
				status.Strength = 3
				return status
			}
		}
	}

	for _, iface := range []string{"wlan0", "wlan1"} {
		operstatePath := "/sys/class/net/" + iface + "/operstate"
		if data, err := os.ReadFile(operstatePath); err == nil {
			if strings.TrimSpace(string(data)) == "up" {
				status.Type = "wifi"
				status.IP = getIPAddress(iface)
				status.SSID, status.Signal = getWifiInfo(iface)
				switch {
				case status.Signal >= 70:
					status.Strength = 3
				case status.Signal >= 50:
					status.Strength = 2
				case status.Signal >= 30:
					status.Strength = 1
				default:
					status.Strength = 0
				}
				return status
			}
		}
	}
	return status
}

func getIPAddress(iface string) string {
	out, err := exec.Command("ip", "-4", "addr", "show", iface).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return strings.Split(parts[1], "/")[0]
			}
		}
	}
	return ""
}

func getWifiInfo(iface string) (string, int) {
	ssid := ""
	signal := 0
	out, err := exec.Command("iwgetid", iface, "-r").Output()
	if err == nil {
		ssid = strings.TrimSpace(string(out))
	}
	file, err := os.Open("/proc/net/wireless")
	if err != nil {
		return ssid, signal
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, iface) {
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				linkQuality := strings.TrimSuffix(fields[2], ".")
				if q, err := strconv.Atoi(linkQuality); err == nil {
					if q >= 0 && q <= 70 {
						signal = (q * 100) / 70
					} else if q > 70 && q <= 100 {
						signal = q
					}
				}
				if signal == 0 && len(fields) >= 4 {
					sigLevel := strings.TrimSuffix(fields[3], ".")
					if dbm, err := strconv.Atoi(sigLevel); err == nil {
						if dbm < 0 {
							signal = 2 * (dbm + 100)
							if signal < 0 {
								signal = 0
							}
							if signal > 100 {
								signal = 100
							}
						}
					}
				}
			}
			break
		}
	}
	return ssid, signal
}

// startWatcher polls every 30s and Emits pushNetworkStatus on change.
// Moved verbatim from socketio.Server.StartNetworkWatcher (the change-detect
// + 30s ticker pattern is preserved exactly).
func startWatcher(ctx context.Context, r Reporter, brd Broadcaster) {
	go func() {
		log.Info().Msg("Network watcher started")
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		last := r.GetStatus()
		brd.Emit("pushNetworkStatus", last)
		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("Network watcher stopped")
				return
			case <-ticker.C:
				current := r.GetStatus()
				if current.Type != last.Type ||
					current.IP != last.IP ||
					current.SSID != last.SSID ||
					current.Strength != last.Strength {
					log.Debug().
						Str("oldType", last.Type).
						Str("newType", current.Type).
						Str("oldIP", last.IP).
						Str("newIP", current.IP).
						Msg("Network status changed")
					last = current
					brd.Emit("pushNetworkStatus", current)
				}
			}
		}
	}()
}
```

- [ ] **Step 2: Verify build (file is not yet wired)**

```bash
GOOS=linux go build ./internal/infra/netinfo/
```

Expected: build clean (package compiles in isolation).

- [ ] **Step 3: Defer commit to Task 3a.5**

---

### Task 3a.3: Move `main.go:528-668` impl into `netinfo_legacy_linux.go`

**Files:**
- Create: `internal/infra/netinfo/netinfo_legacy_linux.go`
- Modify: `cmd/stellar/main.go` (delete lines 528-668 NetworkStatus + getNetworkStatus + getIPAddress + getWifiInfo; line 406 callsite will be re-wired in Task 3a.5)

- [ ] **Step 1: Create `netinfo_legacy_linux.go`**

The legacy impl from `main.go` is functionally near-identical to the socketio impl but with subtle wifi-signal scaling differences (compare main.go:636-643 vs network.go:122-126). Keep it as a separate file so 3b can pick the canonical impl with full visibility into both.

```go
// internal/infra/netinfo/netinfo_legacy_linux.go
//go:build linux

package netinfo

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// legacyGetStatus is a verbatim move of cmd/stellar/main.go:538-584
// (getNetworkStatus). It exists in this file ONLY for the duration of
// Commit Group 3a — Commit 3b deletes it after the canonical impl proves
// byte-equivalent. DO NOT add new callers; route through linuxReporter
// (in netinfo_linux.go) instead.
func legacyGetStatus() Status {
	status := Status{
		Type:     "none",
		Signal:   0,
		Strength: 0,
	}

	for _, iface := range []string{"eth0", "end0"} {
		carrierPath := "/sys/class/net/" + iface + "/carrier"
		if data, err := os.ReadFile(carrierPath); err == nil {
			if strings.TrimSpace(string(data)) == "1" {
				status.Type = "ethernet"
				status.IP = legacyGetIPAddress(iface)
				status.Signal = 100
				status.Strength = 3
				return status
			}
		}
	}

	for _, iface := range []string{"wlan0", "wlan1"} {
		operstatePath := "/sys/class/net/" + iface + "/operstate"
		if data, err := os.ReadFile(operstatePath); err == nil {
			if strings.TrimSpace(string(data)) == "up" {
				status.Type = "wifi"
				status.IP = legacyGetIPAddress(iface)
				status.SSID, status.Signal = legacyGetWifiInfo(iface)
				switch {
				case status.Signal >= 70:
					status.Strength = 3
				case status.Signal >= 50:
					status.Strength = 2
				case status.Signal >= 30:
					status.Strength = 1
				default:
					status.Strength = 0
				}
				return status
			}
		}
	}
	return status
}

func legacyGetIPAddress(iface string) string {
	out, err := exec.Command("ip", "-4", "addr", "show", iface).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return strings.Split(parts[1], "/")[0]
			}
		}
	}
	return ""
}

// legacyGetWifiInfo differs subtly from network.go's getWifiInfo: the
// quality-vs-percentage branch order is reversed (main.go checked 0-100
// first; socketio/network.go checked 0-70 first). This is precisely the
// drift that the dedup commit will resolve.
func legacyGetWifiInfo(iface string) (string, int) {
	ssid := ""
	signal := 0
	out, err := exec.Command("iwgetid", iface, "-r").Output()
	if err == nil {
		ssid = strings.TrimSpace(string(out))
	}
	file, err := os.Open("/proc/net/wireless")
	if err != nil {
		return ssid, signal
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, iface) {
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				linkQuality := strings.TrimSuffix(fields[2], ".")
				if q, err := strconv.Atoi(linkQuality); err == nil {
					if q >= 0 && q <= 100 {
						signal = q
					} else if q >= 0 && q <= 70 {
						signal = (q * 100) / 70
					}
				}
				if signal == 0 && len(fields) >= 4 {
					sigLevel := strings.TrimSuffix(fields[3], ".")
					if dbm, err := strconv.Atoi(sigLevel); err == nil {
						if dbm < 0 {
							signal = 2 * (dbm + 100)
							if signal < 0 {
								signal = 0
							}
							if signal > 100 {
								signal = 100
							}
						}
					}
				}
			}
			break
		}
	}
	return ssid, signal
}
```

- [ ] **Step 2: Delete duplicate code from `cmd/stellar/main.go`**

Read `main.go` lines 520-720 to confirm the boundaries. Delete lines 528-668 (the `NetworkStatus` type + `getNetworkStatus` + `getIPAddress` + `getWifiInfo` definitions). Also remove the `bufio` import from main.go's import block if it becomes unused after the delete.

The line-406 callsite to `getNetworkStatus()` becomes broken at this point — Task 3a.5 re-wires it to call into the `netinfo` package.

- [ ] **Step 3: Defer commit to Task 3a.5**

---

### Task 3a.4: Implement darwin + windows `netinfo`

**Files:**
- Create: `internal/infra/netinfo/netinfo_darwin.go`
- Create: `internal/infra/netinfo/netinfo_windows.go`
- Create: `internal/infra/netinfo/netinfo_darwin_test.go`

- [ ] **Step 1: Write the failing darwin test**

```go
// internal/infra/netinfo/netinfo_darwin_test.go
//go:build darwin

package netinfo

import (
	"context"
	"os/exec"
	"testing"
)

// fakeExecCommand returns a CommandContext substitute that emits canned
// stdout regardless of the requested command. The caller passes a map so
// different command names can produce different output.
func fakeExecCommand(t *testing.T, byCommand map[string]string) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	t.Helper()
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		out, ok := byCommand[name]
		if !ok {
			out = ""
		}
		return exec.CommandContext(ctx, "echo", "-n", out)
	}
}

func TestDarwinEthernet(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = fakeExecCommand(t, map[string]string{
		"/usr/sbin/networksetup": "Hardware Port: Ethernet\nDevice: en0\nEthernet Address: aa:bb\n\nHardware Port: Wi-Fi\nDevice: en1\n",
		"/sbin/ifconfig": `en0: flags=8863<UP,BROADCAST,RUNNING> mtu 1500
	inet 192.168.86.221 netmask 0xffffff00 broadcast 192.168.86.255
	status: active`,
	})

	r := newPlatform()
	got := r.GetStatus()
	if got.Type != "ethernet" {
		t.Errorf("Type = %q, want ethernet", got.Type)
	}
	if got.IP != "192.168.86.221" {
		t.Errorf("IP = %q, want 192.168.86.221", got.IP)
	}
	if got.Strength != 3 {
		t.Errorf("Strength = %d, want 3", got.Strength)
	}
}

func TestDarwinWifi(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = fakeExecCommand(t, map[string]string{
		"/usr/sbin/networksetup": "Hardware Port: Wi-Fi\nDevice: en0\n",
		"/sbin/ifconfig": `en0: flags=8863<UP,BROADCAST,RUNNING> mtu 1500
	inet 10.0.0.5 netmask 0xffffff00
	status: active`,
		"/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport": ` agrCtlRSSI: -55
 SSID: HomeNet
 lastTxRate: 433`,
	})

	r := newPlatform()
	got := r.GetStatus()
	if got.Type != "wifi" {
		t.Errorf("Type = %q, want wifi", got.Type)
	}
	if got.SSID != "HomeNet" {
		t.Errorf("SSID = %q, want HomeNet", got.SSID)
	}
	if got.IP != "10.0.0.5" {
		t.Errorf("IP = %q, want 10.0.0.5", got.IP)
	}
	if got.Strength < 2 {
		t.Errorf("Strength = %d, want >=2 for -55 RSSI", got.Strength)
	}
}

func TestDarwinNone(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = fakeExecCommand(t, map[string]string{})

	r := newPlatform()
	got := r.GetStatus()
	if got.Type != "none" {
		t.Errorf("Type = %q, want none", got.Type)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/infra/netinfo/
```

Expected: FAIL — `newPlatform` undefined for darwin.

- [ ] **Step 3: Implement darwin netinfo**

```go
// internal/infra/netinfo/netinfo_darwin.go
//go:build darwin

package netinfo

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// execCommand is a package-level indirection so tests can substitute canned
// command output.
var execCommand = exec.CommandContext

type darwinReporter struct{}

func newPlatform() Reporter { return &darwinReporter{} }

func (r *darwinReporter) GetStatus() Status {
	status := Status{Type: "none"}

	ports := listHardwarePorts()
	// Prefer ethernet if any wired port is up.
	for _, p := range ports {
		if !strings.Contains(strings.ToLower(p.port), "wi-fi") {
			if ip := ipForDevice(p.device); ip != "" {
				status.Type = "ethernet"
				status.IP = ip
				status.Signal = 100
				status.Strength = 3
				return status
			}
		}
	}

	// Otherwise try the first wi-fi port that has an IP.
	for _, p := range ports {
		if strings.Contains(strings.ToLower(p.port), "wi-fi") {
			ip := ipForDevice(p.device)
			if ip == "" {
				continue
			}
			ssid, rssi := wifiInfo(p.device)
			status.Type = "wifi"
			status.IP = ip
			status.SSID = ssid
			status.Signal = rssiToSignal(rssi)
			switch {
			case status.Signal >= 70:
				status.Strength = 3
			case status.Signal >= 50:
				status.Strength = 2
			case status.Signal >= 30:
				status.Strength = 1
			default:
				status.Strength = 0
			}
			return status
		}
	}
	return status
}

type hwPort struct {
	port   string
	device string
}

// listHardwarePorts parses `networksetup -listallhardwareports` output:
//
//   Hardware Port: Ethernet
//   Device: en0
//   Ethernet Address: ...
//
//   Hardware Port: Wi-Fi
//   Device: en1
func listHardwarePorts() []hwPort {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := execCommand(ctx, "/usr/sbin/networksetup", "-listallhardwareports").Output()
	if err != nil {
		log.Debug().Err(err).Msg("networksetup failed")
		return nil
	}
	var ports []hwPort
	var current hwPort
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Hardware Port:"):
			current = hwPort{port: strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port:"))}
		case strings.HasPrefix(line, "Device:"):
			current.device = strings.TrimSpace(strings.TrimPrefix(line, "Device:"))
			ports = append(ports, current)
			current = hwPort{}
		}
	}
	return ports
}

// ipForDevice runs `ifconfig <dev>` and returns the first `inet` line's IPv4.
func ipForDevice(dev string) string {
	if dev == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := execCommand(ctx, "/sbin/ifconfig", dev).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") && !strings.HasPrefix(line, "inet6 ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}

// wifiInfo returns (SSID, RSSI in dBm) for a wi-fi device. Uses the legacy
// airport command — present on macOS 10.7+, removed in 14+. When absent
// returns empty SSID and 0.
func wifiInfo(dev string) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := execCommand(ctx, "/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport", dev, "-I").Output()
	if err != nil {
		return "", 0
	}
	ssid := ""
	rssi := 0
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SSID:") {
			ssid = strings.TrimSpace(strings.TrimPrefix(line, "SSID:"))
		}
		if strings.HasPrefix(line, "agrCtlRSSI:") {
			vs := strings.TrimSpace(strings.TrimPrefix(line, "agrCtlRSSI:"))
			fmtAtoi(&rssi, vs)
		}
	}
	return ssid, rssi
}

func fmtAtoi(dst *int, s string) {
	n := 0
	neg := false
	i := 0
	if len(s) > 0 && s[0] == '-' {
		neg = true
		i = 1
	}
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		n = -n
	}
	*dst = n
}

// rssiToSignal maps RSSI in dBm to 0-100. -100 dBm → 0, -50 dBm → 100.
func rssiToSignal(rssi int) int {
	if rssi == 0 {
		return 0
	}
	pct := 2 * (rssi + 100)
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// startWatcher runs the same 30s polling loop as Linux.
func startWatcher(ctx context.Context, r Reporter, brd Broadcaster) {
	go func() {
		log.Info().Msg("Network watcher started (darwin)")
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		last := r.GetStatus()
		brd.Emit("pushNetworkStatus", last)
		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("Network watcher stopped")
				return
			case <-ticker.C:
				current := r.GetStatus()
				if current.Type != last.Type ||
					current.IP != last.IP ||
					current.SSID != last.SSID ||
					current.Strength != last.Strength {
					last = current
					brd.Emit("pushNetworkStatus", current)
				}
			}
		}
	}()
}
```

- [ ] **Step 4: Implement Windows stub**

```go
// internal/infra/netinfo/netinfo_windows.go
//go:build windows

package netinfo

import "context"

type windowsReporter struct{}

func newPlatform() Reporter         { return &windowsReporter{} }
func (r *windowsReporter) GetStatus() Status { return Status{Type: "none"} }

func startWatcher(ctx context.Context, r Reporter, brd Broadcaster) {
	// No-op on Windows for M1.A.
}
```

- [ ] **Step 5: Run all platform tests**

```bash
go test ./internal/infra/netinfo/
GOOS=windows GOARCH=amd64 go vet ./internal/infra/netinfo/
```

Expected: PASS on host darwin (3 darwin tests + cross-platform interface tests); windows vet clean.

- [ ] **Step 6: Defer commit to Task 3a.5**

---

### Task 3a.5: Wire `netinfo` handlers (REST→legacy, push→canonical) + commit 3a

**Files:**
- Create: `internal/infra/netinfo/handlers.go`
- Modify: `internal/transport/socketio/server.go` (delete inline pushNetworkStatus emit at ~line 272; delete getNetworkStatus reference at ~line 578)
- Delete: `internal/transport/socketio/network.go`
- Modify: `cmd/stellar/main.go` (rewire `/api/v1/network` REST handler at line 405-411; call `netinfo.RegisterHandlers` + `netinfo.StartWatcher`)

- [ ] **Step 1: Implement `internal/infra/netinfo/handlers.go`**

```go
// internal/infra/netinfo/handlers.go
package netinfo

import "github.com/rs/zerolog/log"

// RegisterHandlers wires the netinfo package into the transport layer.
//
// Registered Socket.IO request handler:
//   - getNetworkStatus → returns Status as JSON
//
// The pushNetworkStatus event is emitted by StartWatcher (periodic) and on
// initial client connect (via BroadcastStatus, called from the transport
// connect-handler).
func RegisterHandlers(reg HandlerRegistrar, brd Broadcaster, r Reporter) {
	reg.OnRequest("getNetworkStatus", func(args ...any) any {
		return r.GetStatus()
	})
}

// BroadcastStatus reads current status and emits pushNetworkStatus. Called
// from the connection-open handler in the transport layer.
func BroadcastStatus(brd Broadcaster, r Reporter) {
	status := r.GetStatus()
	brd.Emit("pushNetworkStatus", status)
	log.Debug().
		Str("type", status.Type).
		Str("ip", status.IP).
		Int("strength", status.Strength).
		Msg("Broadcast network status")
}
```

- [ ] **Step 2: Remove inline network code from `internal/transport/socketio/server.go`**

Read `server.go` around lines 270-280 (connect handler), 575-585 (getNetworkStatus inline handler), and any reference to `s.lastNetwork` / `Server.StartNetworkWatcher` / `Server.BroadcastNetworkStatus`. Delete:

- The line ~272 `client.Emit("pushNetworkStatus", GetNetworkStatus())` → replace with `netinfo.BroadcastStatus(s.Broadcaster(), s.netReporter)`
- The line ~578 inline `getNetworkStatus` handler block → removed entirely (replaced by `netinfo.RegisterHandlers`)
- The `lastNetwork NetworkStatus` field on `Server` struct → removed
- The local `NetworkStatus` type reference → removed

Add a field `netReporter netinfo.Reporter` to the `Server` struct and a setter `SetNetReporter`:

```go
type Server struct {
	// ... existing fields ...
	netReporter netinfo.Reporter
}

func (s *Server) SetNetReporter(r netinfo.Reporter) { s.netReporter = r }
```

- [ ] **Step 3: Delete `internal/transport/socketio/network.go`**

```bash
git rm internal/transport/socketio/network.go
```

- [ ] **Step 4: Rewire `cmd/stellar/main.go`**

Find the existing `/api/v1/network` handler at line 405:

```go
// OLD (delete):
mux.HandleFunc("/api/v1/network", func(w http.ResponseWriter, r *http.Request) {
    status := getNetworkStatus()
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(status)
})
```

Replace with (note: this commit deliberately keeps `legacyGetStatus` in the picture for `/api/v1/network` while the watcher uses the canonical `linuxReporter` — fixture diff proves dedup safety in Commit 3b):

```go
// NEW:
import (
	// ...
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/netinfo"
)

// ... in main() after socketServer construction:
netReporter := netinfo.NewPlatform()
socketServer.SetNetReporter(netReporter)
netinfo.RegisterHandlers(socketServer.Registrar(), socketServer.Broadcaster(), netReporter)

// REST: temporarily route through legacy impl on linux (deleted in Commit 3b)
mux.HandleFunc("/api/v1/network", func(w http.ResponseWriter, r *http.Request) {
    status := legacyStatusForREST() // see step 5
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(status)
})

// Watcher uses the canonical reporter:
netinfo.StartWatcher(ctx, netReporter, socketServer.Broadcaster())
```

Also replace the existing `socketServer.StartNetworkWatcher(ctx)` call at line 230 with the new `netinfo.StartWatcher` line above.

- [ ] **Step 5: Add the temporary REST shim in `cmd/stellar/main.go`**

Since `legacyGetStatus` lives in `internal/infra/netinfo` and is package-private, expose it via a temporary public helper used only by main during Commit Group 3a. Add to a new file `internal/infra/netinfo/legacy_export_linux.go`:

```go
// internal/infra/netinfo/legacy_export_linux.go
//go:build linux

package netinfo

// LegacyStatusForREST is a temporary export of legacyGetStatus that lets the
// /api/v1/network REST handler keep its original behaviour while the
// canonical Reporter is wired up for the pushNetworkStatus Socket.IO emit.
//
// REMOVED in Commit 3b (netinfo dedup). DO NOT add new callers.
func LegacyStatusForREST() Status { return legacyGetStatus() }
```

For non-linux platforms, REST `/api/v1/network` can call the canonical Reporter directly. Update `main.go`:

```go
import (
	"runtime"
	// ...
)

mux.HandleFunc("/api/v1/network", func(w http.ResponseWriter, r *http.Request) {
    var status netinfo.Status
    if runtime.GOOS == "linux" {
        status = netinfo.LegacyStatusForREST()
    } else {
        status = netReporter.GetStatus()
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(status)
})
```

- [ ] **Step 6: Verify all three builds + tests**

```bash
make build           # Pi ARM64
make build-darwin
make build-windows
go test ./...
```

Expected: all green.

- [ ] **Step 7: Post-deploy fixture verification on Pi**

```bash
scp bin/stellar-arm64 stellar.lan:~/stellar-backend/stellar.new
ssh stellar.lan "sudo systemctl stop stellar-backend && mv ~/stellar-backend/stellar.new ~/stellar-backend/stellar && sudo systemctl start stellar-backend"
sleep 5
ssh stellar.lan "curl -s http://localhost:3000/api/v1/network" > /tmp/m1a-post-rest-3a.json
diff /tmp/m1a-pre-network.json /tmp/m1a-post-rest-3a.json
```

Expected: `diff` produces 0 differences (REST routes through `legacyGetStatus`, which is the verbatim move of `main.go:538` body — byte-identical).

If the Socket.IO `pushNetworkStatus` payload differs from the fixture, that proves the drift exists between the two old impls. Note the diff in the commit message; Commit 3b will resolve it.

- [ ] **Step 8: Commit**

```bash
git add internal/infra/netinfo/ internal/transport/socketio/server.go cmd/stellar/main.go
git rm internal/transport/socketio/network.go
git commit -m "$(cat <<'EOF'
refactor(netinfo): move both Linux network impls into internal/infra/netinfo (keep both)

Create internal/infra/netinfo with the package interface (Reporter,
Broadcaster, HandlerRegistrar, Status, ErrUnsupported, StartWatcher) and
move the two existing Linux impls into the package unchanged:

  * netinfo_linux.go      — verbatim move of socketio/network.go body
                            (canonical impl going forward)
  * netinfo_legacy_linux.go — verbatim move of cmd/stellar/main.go:528-668
                            (legacyGetStatus — temporary, dropped in Commit 3b)

Add darwin impl (real, using networksetup + ifconfig + airport) and
windows stub (returns Status{Type: "none"}).

Wire transport layer:
  - REST /api/v1/network: continues to call legacyGetStatus on linux,
    canonical impl on other platforms (linux behaviour identical to today)
  - Socket.IO pushNetworkStatus: served by canonical Reporter via
    StartWatcher (deliberately switched here so byte-diff between push
    fixture and rest fixture surfaces the drift before Commit 3b deletes
    the legacy impl)

Capture pre-change JSON fixtures under
internal/infra/netinfo/testdata/{rest_pre,push_pre}.json. Commit 3b's
dedup-byte-equality test runs against these.

Delete internal/transport/socketio/network.go (193 lines). Delete
cmd/stellar/main.go:528-668 duplicate NetworkStatus / getNetworkStatus
/ getIPAddress / getWifiInfo.

Third commit of M1.A (Commit 3a of the netinfo split). Spec:
docs/superpowers/specs/2026-05-13-m1a-backend-portability-design.md
EOF
)"
```

---

## Commit Group 3b — netinfo dedup to canonical impl

Selects the canonical Linux impl (the former `socketio/network.go` body, now in `netinfo_linux.go`), routes both REST and Socket.IO callsites through it, and deletes `netinfo_legacy_linux.go` + the `LegacyStatusForREST` export. Fixture byte-equality test added here proves the canonical impl produces the same wire shape that legacy produced.

### Task 3b.1: Decide canonical impl + add byte-equality fixture test

**Files:**
- Modify: `internal/infra/netinfo/fixture_test.go` (add canonical-vs-fixture equality check)

- [ ] **Step 1: Confirm canonical choice**

The canonical impl is `linuxReporter.GetStatus()` (the former socketio/network.go body — already in `netinfo_linux.go`). Reason: socketio version is the live in-production impl that frontend pushNetworkStatus listeners have been consuming since v1.x; main.go's was a duplicate accidentally created during early dev. We preserve the live behaviour and discard the divergent legacy.

- [ ] **Step 2: Append canonical-equality test**

```go
// internal/infra/netinfo/fixture_test.go (append to existing file)

//go:build linux

import (
	// existing imports
)

// TestCanonicalMatchesRESTFixture asserts that the canonical Reporter on
// the current host produces a Status that, when marshalled, byte-equals
// the pre-change REST fixture captured in Commit 3a. This is the gate
// for safely deleting netinfo_legacy_linux.go.
//
// NOTE: this test is meaningful only when run on the same Pi host whose
// state was captured into rest_pre.json. CI on non-Pi hosts should not
// execute this test — gated by a host-match check.
func TestCanonicalMatchesRESTFixture(t *testing.T) {
	if os.Getenv("STELLAR_NETINFO_FIXTURE_TEST") != "1" {
		t.Skip("set STELLAR_NETINFO_FIXTURE_TEST=1 to run on the captured host")
	}
	want := readFixture(t, "rest_pre.json")
	got := NewPlatform().GetStatus()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("canonical vs fixture mismatch:\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
}
```

- [ ] **Step 3: Run fixture test on the Pi (post-deploy)**

```bash
ssh stellar.lan "cd ~/stellar-backend-src && STELLAR_NETINFO_FIXTURE_TEST=1 go test ./internal/infra/netinfo/ -run TestCanonicalMatchesRESTFixture -v"
```

Expected: PASS — the canonical reporter produces the same wire shape as the pre-change REST endpoint did. If FAIL, investigate the per-field diff before proceeding.

(If the Pi doesn't have a Go toolchain installed, run the test cross-compiled against a captured `/sys/class/net/...` snapshot instead — leave that path as a fallback only if SSH-with-go fails.)

- [ ] **Step 4: Defer commit to Task 3b.2**

---

### Task 3b.2: Delete legacy + route REST through canonical + commit 3b

**Files:**
- Delete: `internal/infra/netinfo/netinfo_legacy_linux.go`
- Delete: `internal/infra/netinfo/legacy_export_linux.go`
- Modify: `cmd/stellar/main.go` (remove `runtime.GOOS == "linux"` branch; always call `netReporter.GetStatus()`)

- [ ] **Step 1: Delete legacy files**

```bash
git rm internal/infra/netinfo/netinfo_legacy_linux.go internal/infra/netinfo/legacy_export_linux.go
```

- [ ] **Step 2: Simplify `cmd/stellar/main.go`**

```go
// REPLACE the /api/v1/network handler from Commit 3a Step 5 with:
mux.HandleFunc("/api/v1/network", func(w http.ResponseWriter, r *http.Request) {
    status := netReporter.GetStatus()
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(status)
})
```

Also remove the unused `runtime` import if it was only added for this branch.

- [ ] **Step 3: Verify build + fixture test on Pi**

```bash
make build
scp bin/stellar-arm64 stellar.lan:~/stellar-backend/stellar.new
ssh stellar.lan "sudo systemctl stop stellar-backend && mv ~/stellar-backend/stellar.new ~/stellar-backend/stellar && sudo systemctl start stellar-backend"
sleep 5
ssh stellar.lan "curl -s http://localhost:3000/api/v1/network" > /tmp/m1a-post-rest-3b.json
diff /tmp/m1a-pre-network.json /tmp/m1a-post-rest-3b.json
```

Expected: `diff` produces 0 differences — canonical impl matches the pre-change REST shape byte-for-byte.

- [ ] **Step 4: Verify pushNetworkStatus matches the push fixture**

```bash
node -e "
const io = require('socket.io-client');
const s = io('http://stellar.lan:3000');
s.on('pushNetworkStatus', p => { console.log(JSON.stringify(p)); s.close(); process.exit(0); });
setTimeout(() => { console.error('timeout'); process.exit(1); }, 5000);
" > /tmp/m1a-post-push-3b.json
diff internal/infra/netinfo/testdata/push_pre.json /tmp/m1a-post-push-3b.json
```

Expected: byte-equal — pushNetworkStatus already went through the canonical impl in Commit 3a, so 3b should not change it.

- [ ] **Step 5: Run full test suite + all three builds**

```bash
go test ./...
make build
make build-darwin
make build-windows
```

Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add cmd/stellar/main.go internal/infra/netinfo/fixture_test.go
git rm internal/infra/netinfo/netinfo_legacy_linux.go internal/infra/netinfo/legacy_export_linux.go
git commit -m "$(cat <<'EOF'
refactor(netinfo): dedup to canonical impl

Delete netinfo_legacy_linux.go (the former main.go:528-668 body) and the
temporary LegacyStatusForREST export. REST /api/v1/network now routes
through the canonical Reporter — the same impl that has served
pushNetworkStatus since Commit 3a.

Byte-equality verified against the rest_pre.json fixture captured in
Commit 3a — TestCanonicalMatchesRESTFixture passes on the Pi with
STELLAR_NETINFO_FIXTURE_TEST=1. Live REST diff produced 0 differences
between pre-3a and post-3b.

Bisect-clean if a network-status regression appears: Commit 3a is the
"move" commit, Commit 3b is the "dedup" commit. Behavioural drift
between the two old impls (wifi-quality scaling branch order) is now
gone — the canonical (former socketio/network.go) version is the single
source of truth.

Fourth commit of M1.A (Commit 3b of the netinfo split). Spec:
docs/superpowers/specs/2026-05-13-m1a-backend-portability-design.md
EOF
)"
```

---

## Commit Group 4 — `internal/domain/sources` rename + darwin impls

Renames the existing Linux sources files to the `_linux.go` build-tag convention, adds darwin counterparts (real impls using `mount_smbfs` / `mount_nfs` / `dns-sd` / `smbutil`), and adds windows stubs. Introduces the `NewPlatformMounter` / `NewPlatformDiscoverer` constructors.

### Task 4.1: Rename Linux sources files with `git mv`

**Files:**
- Rename: `internal/domain/sources/linux_mounter.go` → `internal/domain/sources/mounter_linux.go`
- Rename: `internal/domain/sources/linux_mounter_test.go` → `internal/domain/sources/mounter_linux_test.go`
- Rename: `internal/domain/sources/linux_discoverer.go` → `internal/domain/sources/discoverer_linux.go`
- Rename: `internal/domain/sources/linux_discoverer_test.go` → `internal/domain/sources/discoverer_linux_test.go`
- Modify: each renamed file gets `//go:build linux` added at the top

- [ ] **Step 1: Rename with `git mv` (preserves blame history)**

```bash
git mv internal/domain/sources/linux_mounter.go      internal/domain/sources/mounter_linux.go
git mv internal/domain/sources/linux_mounter_test.go internal/domain/sources/mounter_linux_test.go
git mv internal/domain/sources/linux_discoverer.go      internal/domain/sources/discoverer_linux.go
git mv internal/domain/sources/linux_discoverer_test.go internal/domain/sources/discoverer_linux_test.go
```

- [ ] **Step 2: Add `//go:build linux` build tag to each renamed file**

For each of the 4 renamed files, prepend:

```go
//go:build linux

package sources
```

(remove the existing `package sources` line if it's bare, since the build tag must be the very first non-comment line).

- [ ] **Step 3: Verify Pi build + tests still pass**

```bash
make build
go test ./internal/domain/sources/
```

Expected: all green — the rename plus build tag is a no-op on Linux.

- [ ] **Step 4: Defer commit to Task 4.5**

---

### Task 4.2: Add platform-selector files (`platform_*.go`)

**Files:**
- Create: `internal/domain/sources/platform_linux.go`
- Create: `internal/domain/sources/platform_darwin.go`
- Create: `internal/domain/sources/platform_windows.go`
- Modify: `internal/domain/sources/mounter.go` (no changes, but verify the interface stays exported)
- Modify: `internal/domain/sources/discoverer.go` (same)

- [ ] **Step 1: Implement linux selector**

```go
// internal/domain/sources/platform_linux.go
//go:build linux

package sources

// NewPlatformMounter returns the Linux mount controller.
func NewPlatformMounter() Mounter { return NewLinuxMounter() }

// NewPlatformDiscoverer returns the Linux NAS discoverer.
func NewPlatformDiscoverer() Discoverer { return NewLinuxDiscoverer() }
```

- [ ] **Step 2: Implement darwin selector (stub for now; real impls in Tasks 4.3 + 4.4)**

```go
// internal/domain/sources/platform_darwin.go
//go:build darwin

package sources

// NewPlatformMounter returns the macOS mount controller.
func NewPlatformMounter() Mounter { return NewDarwinMounter() }

// NewPlatformDiscoverer returns the macOS NAS discoverer.
func NewPlatformDiscoverer() Discoverer { return NewDarwinDiscoverer() }
```

- [ ] **Step 3: Implement windows selector (stubs)**

```go
// internal/domain/sources/platform_windows.go
//go:build windows

package sources

import (
	"context"
	"errors"
)

var ErrUnsupported = errors.New("sources: not supported on this platform")

// windowsMounter is a no-op mounter for builds where NAS mounting is not
// supported. All methods return ErrUnsupported.
type windowsMounter struct{}

func NewPlatformMounter() Mounter { return &windowsMounter{} }

func (windowsMounter) Mount(ctx context.Context, share *NasShare) error      { return ErrUnsupported }
func (windowsMounter) Unmount(ctx context.Context, mountPoint string) error  { return ErrUnsupported }
func (windowsMounter) IsMounted(mountPoint string) bool                      { return false }
func (windowsMounter) CreateMountPoint(path string) error                    { return ErrUnsupported }
func (windowsMounter) RemoveMountPoint(path string) error                    { return ErrUnsupported }
func (windowsMounter) CreateSymlink(source, target string) error             { return ErrUnsupported }
func (windowsMounter) RemoveSymlink(path string) error                       { return ErrUnsupported }

type windowsDiscoverer struct{}

func NewPlatformDiscoverer() Discoverer { return &windowsDiscoverer{} }

func (windowsDiscoverer) DiscoverDevices(ctx context.Context) ([]NasDevice, error) {
	return nil, ErrUnsupported
}
func (windowsDiscoverer) BrowseShares(ctx context.Context, host, username, password string) ([]ShareInfo, error) {
	return nil, ErrUnsupported
}
```

- [ ] **Step 4: Verify cross-compile of all three**

```bash
make build           # linux
make build-darwin    # selector references NewDarwinMounter — will fail until Task 4.3/4.4 land
make build-windows
```

Expected: linux build + windows build clean. Darwin build will fail with `undefined: NewDarwinMounter` — that's intended; Tasks 4.3 and 4.4 close that loop.

- [ ] **Step 5: Defer commit to Task 4.5**

---

### Task 4.3: Implement Darwin mounter

**Files:**
- Create: `internal/domain/sources/mounter_darwin.go`
- Create: `internal/domain/sources/mounter_darwin_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/domain/sources/mounter_darwin_test.go
//go:build darwin

package sources

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// fakeMountCommand captures the args it was called with for assertion.
type capturingExec struct {
	calls [][]string
	stdout string
	err    error
}

func (c *capturingExec) cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	c.calls = append(c.calls, append([]string{name}, args...))
	return exec.CommandContext(ctx, "echo", "-n", c.stdout)
}

func TestDarwinMounterMountCifsBuildsExpectedCommand(t *testing.T) {
	cap := &capturingExec{}
	orig := mountCommand
	t.Cleanup(func() { mountCommand = orig })
	mountCommand = cap.cmd

	m := NewDarwinMounter()
	share := &NasShare{
		IP:         "192.168.1.10",
		Path:       "Music",
		MountPoint: "/Volumes/stellar-nas/Music",
		FSType:     "cifs",
		Username:   "alice",
		Password:   "s3cret",
	}

	if err := m.Mount(context.Background(), share); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	if len(cap.calls) != 1 {
		t.Fatalf("expected 1 mount call, got %d", len(cap.calls))
	}
	want := []string{"/sbin/mount_smbfs", "//alice:s3cret@192.168.1.10/Music", "/Volumes/stellar-nas/Music"}
	got := cap.calls[0]
	if !equalArgs(got, want) {
		t.Errorf("mount call args:\n  got:  %v\n  want: %v", got, want)
	}
	if !share.Mounted {
		t.Error("share.Mounted should be true after successful Mount")
	}
}

func TestDarwinMounterMountNfsBuildsExpectedCommand(t *testing.T) {
	cap := &capturingExec{}
	orig := mountCommand
	t.Cleanup(func() { mountCommand = orig })
	mountCommand = cap.cmd

	m := NewDarwinMounter()
	share := &NasShare{
		IP:         "192.168.1.10",
		Path:       "/export/music",
		MountPoint: "/Volumes/stellar-nas/Music",
		FSType:     "nfs",
	}
	if err := m.Mount(context.Background(), share); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	want := []string{"/sbin/mount_nfs", "192.168.1.10:/export/music", "/Volumes/stellar-nas/Music"}
	if !equalArgs(cap.calls[0], want) {
		t.Errorf("nfs mount call args:\n  got:  %v\n  want: %v", cap.calls[0], want)
	}
}

func TestDarwinMounterIsMountedReadsMountTable(t *testing.T) {
	cap := &capturingExec{
		stdout: strings.Join([]string{
			"/dev/disk1s1 on / (apfs, local, journaled)",
			"//user@host/share on /Volumes/stellar-nas/Music (smbfs)",
		}, "\n"),
	}
	orig := mountCommand
	t.Cleanup(func() { mountCommand = orig })
	mountCommand = cap.cmd

	m := NewDarwinMounter()
	if !m.IsMounted("/Volumes/stellar-nas/Music") {
		t.Error("IsMounted should return true for mounted SMB share")
	}
	if m.IsMounted("/Volumes/not-mounted") {
		t.Error("IsMounted should return false for non-mounted path")
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run test — expected to fail**

```bash
go test ./internal/domain/sources/ -run TestDarwinMounter
```

Expected: FAIL — `NewDarwinMounter` undefined.

- [ ] **Step 3: Implement Darwin mounter**

```go
// internal/domain/sources/mounter_darwin.go
//go:build darwin

package sources

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// mountCommand is the package-level indirection over exec.CommandContext so
// tests can substitute a stub. Matches the pattern used in mounter_linux.go.
var mountCommand = exec.CommandContext

// DarwinMounter implements Mounter using macOS mount commands.
type DarwinMounter struct{}

// NewDarwinMounter creates a new macOS mounter.
func NewDarwinMounter() *DarwinMounter { return &DarwinMounter{} }

func (m *DarwinMounter) Mount(ctx context.Context, share *NasShare) error {
	switch share.FSType {
	case "cifs", "smbfs":
		return m.mountCifs(ctx, share)
	case "nfs":
		return m.mountNfs(ctx, share)
	default:
		return fmt.Errorf("unsupported filesystem type: %s", share.FSType)
	}
}

// mountCifs uses mount_smbfs with URL-style auth:
//   /sbin/mount_smbfs //user:pass@host/share /mount/point
// If username is empty the URL collapses to //host/share (anonymous).
func (m *DarwinMounter) mountCifs(ctx context.Context, share *NasShare) error {
	var url string
	switch {
	case share.Username != "" && share.Password != "":
		url = fmt.Sprintf("//%s:%s@%s/%s", share.Username, share.Password, share.IP, share.Path)
	case share.Username != "":
		url = fmt.Sprintf("//%s@%s/%s", share.Username, share.IP, share.Path)
	default:
		url = fmt.Sprintf("//%s/%s", share.IP, share.Path)
	}

	cmd := mountCommand(ctx, "/sbin/mount_smbfs", url, share.MountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Warn().Str("url", redactURL(url)).Msg("SMB mount timed out")
			return fmt.Errorf("mount timed out: %w", ctx.Err())
		}
		log.Error().
			Err(err).
			Str("url", redactURL(url)).
			Str("mountPoint", share.MountPoint).
			Str("output", string(output)).
			Msg("SMB mount failed")
		return fmt.Errorf("mount failed: %s", string(output))
	}
	log.Info().
		Str("url", redactURL(url)).
		Str("mountPoint", share.MountPoint).
		Msg("SMB share mounted")
	share.Mounted = true
	return nil
}

// mountNfs uses mount_nfs:
//   /sbin/mount_nfs <host>:<path> /mount/point
func (m *DarwinMounter) mountNfs(ctx context.Context, share *NasShare) error {
	source := fmt.Sprintf("%s:%s", share.IP, share.Path)
	cmd := mountCommand(ctx, "/sbin/mount_nfs", source, share.MountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Warn().Str("source", source).Msg("NFS mount timed out")
			return fmt.Errorf("mount timed out: %w", ctx.Err())
		}
		log.Error().
			Err(err).
			Str("source", source).
			Str("mountPoint", share.MountPoint).
			Str("output", string(output)).
			Msg("NFS mount failed")
		return fmt.Errorf("mount failed: %s", string(output))
	}
	log.Info().Str("source", source).Str("mountPoint", share.MountPoint).Msg("NFS share mounted")
	share.Mounted = true
	return nil
}

func (m *DarwinMounter) Unmount(ctx context.Context, mountPoint string) error {
	cmd := mountCommand(ctx, "/sbin/umount", mountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("unmount timed out: %w", ctx.Err())
		}
		return fmt.Errorf("unmount failed: %s", string(output))
	}
	log.Info().Str("mountPoint", mountPoint).Msg("Filesystem unmounted")
	return nil
}

// IsMounted parses `mount(8)` output. We don't reuse paths.ListMounts here
// because the sources package shouldn't take a dependency on paths for
// internal mount-state lookups — the format is simple enough to parse
// inline. (paths.ListMounts is for cross-cutting consumers like classifier.)
func (m *DarwinMounter) IsMounted(mountPoint string) bool {
	cmd := mountCommand(context.Background(), "/sbin/mount")
	out, err := cmd.Output()
	if err != nil {
		log.Error().Err(err).Msg("Failed to run mount(8)")
		return false
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		// Format: "<src> on <mountpoint> (...)"
		onIdx := strings.Index(line, " on ")
		if onIdx < 0 {
			continue
		}
		rest := line[onIdx+4:]
		parenIdx := strings.LastIndex(rest, " (")
		if parenIdx < 0 {
			continue
		}
		if rest[:parenIdx] == mountPoint {
			return true
		}
	}
	return false
}

func (m *DarwinMounter) CreateMountPoint(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create parent dir: %w", err)
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}
	return nil
}

func (m *DarwinMounter) RemoveMountPoint(path string) error {
	return os.Remove(path)
}

func (m *DarwinMounter) CreateSymlink(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("failed to create symlink parent: %w", err)
	}
	os.Remove(target) // ignore error — non-existence is fine
	if err := os.Symlink(source, target); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}
	log.Info().Str("source", source).Str("target", target).Msg("Symlink created")
	return nil
}

func (m *DarwinMounter) RemoveSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("not a symlink: %s", path)
	}
	return os.Remove(path)
}

// redactURL masks the password segment of an SMB URL for logging:
//   //user:pass@host/share → //user:***@host/share
func redactURL(u string) string {
	at := strings.LastIndex(u, "@")
	colon := strings.Index(u, ":")
	if at < 0 || colon < 0 || colon >= at {
		return u
	}
	// keep "//user:" then mask up to "@"
	return u[:colon+1] + "***" + u[at:]
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/domain/sources/
```

Expected: PASS — 3 new darwin tests + existing linux tests (cross-tag-skipped on darwin host) + existing cross-tag service tests.

- [ ] **Step 5: Defer commit to Task 4.5**

---

### Task 4.4: Implement Darwin discoverer

**Files:**
- Create: `internal/domain/sources/discoverer_darwin.go`
- Create: `internal/domain/sources/discoverer_darwin_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/domain/sources/discoverer_darwin_test.go
//go:build darwin

package sources

import (
	"context"
	"os/exec"
	"testing"
)

func TestDarwinDiscovererParsesDNSSDOutput(t *testing.T) {
	cap := &capturingExec{
		stdout: `Browsing for _smb._tcp
DATE: ---Mon 12 May 2026---
 1:39:42.123  ...STARTING...
Timestamp     A/R    Flags  if Domain   Service Type         Instance Name
 1:39:42.456  Add        2   4 local.   _smb._tcp.           nas-music
 1:39:42.789  Add        2   4 local.   _smb._tcp.           NAS_Backup
`,
	}
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = cap.cmd

	// Need a second stub for `smbutil view` calls per host — but if
	// DiscoverDevices doesn't call smbutil view itself, this is enough.
	d := NewDarwinDiscoverer()
	devices, err := d.DiscoverDevices(context.Background())
	if err != nil {
		t.Fatalf("DiscoverDevices() error = %v", err)
	}
	if len(devices) < 2 {
		t.Fatalf("expected at least 2 devices, got %d", len(devices))
	}
	names := map[string]bool{}
	for _, dev := range devices {
		names[dev.Name] = true
	}
	if !names["nas-music"] {
		t.Error("expected device named 'nas-music'")
	}
	if !names["NAS_Backup"] {
		t.Error("expected device named 'NAS_Backup'")
	}
}

func TestDarwinDiscovererBrowseSharesParsesSmbutilView(t *testing.T) {
	cap := &capturingExec{
		stdout: `Share                                 Type    Comments
-------------------------------
Music                                 Disk    
Videos                                Disk    Video collection
IPC$                                  IPC     Remote IPC
`,
	}
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = cap.cmd

	d := NewDarwinDiscoverer()
	shares, err := d.BrowseShares(context.Background(), "nas-music", "", "")
	if err != nil {
		t.Fatalf("BrowseShares() error = %v", err)
	}
	if len(shares) != 2 {
		t.Fatalf("expected 2 shares (IPC$ filtered), got %d", len(shares))
	}
	if shares[0].Name != "Music" || shares[1].Name != "Videos" {
		t.Errorf("got shares %v, want Music + Videos", shares)
	}
}

// capturingExec from mounter_darwin_test.go (same package) is reused here.
// If go test complains about duplicate symbols, move both helpers into a
// shared *_test.go file. The simplest fix: rename one to avoid collision.
var _ = (*exec.Cmd)(nil) // keep import for IDE
```

- [ ] **Step 2: Run test — verify failure**

```bash
go test ./internal/domain/sources/ -run TestDarwinDiscoverer
```

Expected: FAIL — `NewDarwinDiscoverer` undefined.

- [ ] **Step 3: Implement Darwin discoverer**

```go
// internal/domain/sources/discoverer_darwin.go
//go:build darwin

package sources

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// discoverTimeout is the per-tool budget. Matches the Linux value.
var discoverTimeout = 6 * time.Second

// execCommand is the package-level indirection for tests.
var execCommand = exec.CommandContext

var errDarwinDiscoverTimeout = errors.New("discovery timed out")

// DarwinDiscoverer implements Discoverer using macOS tools.
type DarwinDiscoverer struct{}

// NewDarwinDiscoverer creates a new macOS NAS discoverer.
func NewDarwinDiscoverer() *DarwinDiscoverer { return &DarwinDiscoverer{} }

// DiscoverDevices uses dns-sd to browse for _smb._tcp services on the LAN.
// dns-sd produces an incremental stream; we cap with a context deadline so
// it doesn't block indefinitely.
//
// Output shape (relevant rows start with "Add"):
//   1:39:42.456  Add        2   4 local.   _smb._tcp.           nas-music
//   1:39:42.789  Add        2   4 local.   _smb._tcp.           NAS_Backup
func (d *DarwinDiscoverer) DiscoverDevices(ctx context.Context) ([]NasDevice, error) {
	log.Info().Msg("Starting NAS discovery (darwin)...")

	cmdCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	cmd := execCommand(cmdCtx, "/usr/bin/dns-sd", "-B", "_smb._tcp.")
	out, err := cmd.Output()
	if err != nil && !errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		// dns-sd never exits cleanly without a deadline-kill; treat anything
		// other than DeadlineExceeded as a "tool missing or broken" case.
		log.Debug().Err(err).Msg("dns-sd failed (may not be installed)")
		return nil, nil
	}

	var devices []NasDevice
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		// We want rows where fields[1] == "Add" and the last token is the
		// instance name.
		if len(fields) < 7 {
			continue
		}
		if fields[1] != "Add" {
			continue
		}
		name := fields[len(fields)-1]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		devices = append(devices, NasDevice{
			Name:     name,
			IP:       "", // dns-sd -B does not resolve IPs; dns-sd -L would, but smbutil view accepts hostnames
			Hostname: name + ".local",
		})
	}
	log.Info().Int("count", len(devices)).Msg("NAS discovery complete (darwin)")
	return devices, nil
}

// BrowseShares uses smbutil view to list shares on a host. The macOS
// equivalent of Linux's smbclient -L.
//
// Output shape:
//   Share                                 Type    Comments
//   -------------------------------
//   Music                                 Disk
//   IPC$                                  IPC     Remote IPC
func (d *DarwinDiscoverer) BrowseShares(ctx context.Context, host, username, password string) ([]ShareInfo, error) {
	log.Info().Str("host", host).Msg("Browsing NAS shares (darwin)...")

	var url string
	switch {
	case username != "" && password != "":
		url = fmt.Sprintf("//%s:%s@%s", username, password, host)
	case username != "":
		url = fmt.Sprintf("//%s@%s", username, host)
	default:
		url = fmt.Sprintf("//%s", host)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	cmd := execCommand(cmdCtx, "/usr/bin/smbutil", "view", url)
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(out)
		if strings.Contains(outStr, "Authentication") || strings.Contains(outStr, "Permission denied") {
			return nil, &ShareBrowseError{Code: "AUTH_REQUIRED", Message: "authentication required"}
		}
		if strings.Contains(outStr, "No route to host") || strings.Contains(outStr, "Connection refused") {
			return nil, &ShareBrowseError{Code: "HOST_UNREACHABLE", Message: "host unreachable: " + host}
		}
		return nil, &ShareBrowseError{Code: "BROWSE_FAILED", Message: "failed to browse shares: " + err.Error()}
	}

	var shares []ShareInfo
	inList := false
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "---") {
			inList = true
			continue
		}
		if !inList {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			inList = false
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		shareType := strings.ToLower(parts[1])
		if name == "IPC$" || name == "ADMIN$" || name == "C$" {
			continue
		}
		comment := ""
		if len(parts) > 2 {
			comment = strings.TrimSpace(strings.Join(parts[2:], " "))
		}
		shares = append(shares, ShareInfo{
			Name:     name,
			Type:     shareType,
			Comment:  comment,
			Writable: shareType == "disk",
		})
	}
	log.Info().Int("count", len(shares)).Str("host", host).Msg("Share browse complete (darwin)")
	return shares, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/domain/sources/
```

Expected: PASS — all darwin tests + existing linux tests skipped (cross-tag).

- [ ] **Step 5: Defer commit to Task 4.5**

---

### Task 4.5: Verify all three builds + commit Group 4

**Files:** none new

- [ ] **Step 1: Verify all three platform builds**

```bash
make build           # Pi ARM64
make build-darwin    # macOS ARM64 (now closes the loop with NewDarwinMounter/Discoverer)
make build-windows   # Windows AMD64 (stubs)
go test ./...
```

Expected: 3 binaries produced; all tests green.

- [ ] **Step 2: Smoke test darwin mounter against a real share (optional)**

```bash
# If you have a local SMB share at smb://user@host/share, this is the
# first proof that the darwin impl actually works end-to-end.
./bin/stellar-darwin-arm64 -port=3001 -mpd-host=localhost -static="" &
# In another shell, hit /api/v1/sources or the equivalent and try a mount.
# Optional — skip if no test share is available.
kill %1
```

- [ ] **Step 3: Commit**

```bash
git add internal/domain/sources/
git commit -m "$(cat <<'EOF'
refactor(sources): add darwin impls + rename Linux files to _linux.go convention

Rename:
  internal/domain/sources/linux_mounter{,_test}.go → mounter_linux{,_test}.go
  internal/domain/sources/linux_discoverer{,_test}.go → discoverer_linux{,_test}.go
Each renamed file gains a //go:build linux tag. Renames use git mv to
preserve blame history.

Add darwin impls:
  mounter_darwin.go    — uses /sbin/mount_smbfs and /sbin/mount_nfs with
                         URL-style auth (//user:pass@host/share); password
                         redacted in logs
  discoverer_darwin.go — uses /usr/bin/dns-sd -B _smb._tcp. for service
                         browse and /usr/bin/smbutil view for share listing

Add platform selectors:
  platform_linux.go    — NewPlatformMounter → NewLinuxMounter
  platform_darwin.go   — NewPlatformMounter → NewDarwinMounter
  platform_windows.go  — pure stubs returning ErrUnsupported

mount_smbfs and dns-sd timeouts mitigated via context deadlines (6s for
discovery; per-call ctx for mounts) — same pattern as the Linux impls.

Fifth commit of M1.A. Spec:
docs/superpowers/specs/2026-05-13-m1a-backend-portability-design.md
EOF
)"
```

---

## Commit Group 5 — Final wiring, system.go portability, falsifiable success gate, docs

Switches `main.go` from `NewLinuxMounter()`/`NewLinuxDiscoverer()` to `NewPlatformMounter()`/`NewPlatformDiscoverer()`. Updates `socketio/system.go` to use `paths.SystemHardware()`. Runs the falsifiable success-criteria gates. Updates `docs/ARCHITECTURE.md`.

### Task 5.1: Switch main.go to platform selectors + update system.go

**Files:**
- Modify: `cmd/stellar/main.go:98` (`sources.NewLinuxMounter()` → `sources.NewPlatformMounter()`)
- Modify: `cmd/stellar/main.go:104` (`sources.NewLinuxDiscoverer()` → `sources.NewPlatformDiscoverer()`)
- Modify: `internal/transport/socketio/system.go` (use `paths.SystemHardware()`)

- [ ] **Step 1: Edit `cmd/stellar/main.go`**

```go
// Line 98: was
sourcesService, err := sources.NewService(sourcesConfigPath, sources.NewLinuxMounter())
// → now
sourcesService, err := sources.NewService(sourcesConfigPath, sources.NewPlatformMounter())

// Line 104 (inside the sourcesService non-nil branch): was
sourcesService.SetDiscoverer(sources.NewLinuxDiscoverer())
// → now
sourcesService.SetDiscoverer(sources.NewPlatformDiscoverer())
```

- [ ] **Step 2: Edit `internal/transport/socketio/system.go`**

Replace lines 42-54 (the `/proc/cpuinfo` read) with a call to `paths.SystemHardware()`:

```go
package socketio

import (
	"os"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/paths"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/version"
)

// SystemInfo represents basic system information.
type SystemInfo struct {
	ID            string `json:"id"`
	Host          string `json:"host"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	ServiceName   string `json:"serviceName"`
	SystemVersion string `json:"systemversion"`
	BuildDate     string `json:"builddate"`
	Variant       string `json:"variant"`
	Hardware      string `json:"hardware"`
}

// GetSystemInfo returns basic system information.
func GetSystemInfo() SystemInfo {
	info := SystemInfo{
		Type:          "audio_player",
		ServiceName:   "stellar",
		SystemVersion: version.GetInfo().Version,
		BuildDate:     version.GetInfo().BuildTime,
		Variant:       "stellar-pi",
		Hardware:      "Raspberry Pi",
	}
	if hostname, err := os.Hostname(); err == nil {
		info.Host = hostname
		info.Name = hostname
		info.ID = hostname
	}
	if hw := paths.SystemHardware(); hw != "" {
		info.Hardware = hw
	}
	return info
}
```

- [ ] **Step 3: Verify builds + tests**

```bash
go test ./...
make build
make build-darwin
make build-windows
```

Expected: all green; 3 binaries produced.

- [ ] **Step 4: Defer commit to Task 5.3**

---

### Task 5.2: Run falsifiable success gates

**Files:** none modified — this task only runs verification commands.

- [ ] **Step 1: Gate 1 — Pi binary identical-ish size**

```bash
make build
ls -la bin/stellar-arm64
```

Expected: binary size within ±5% of the pre-M1.A binary (eyeball check against the most recent backed-up `stellar-arm64`; nothing major has been added other than abstractions, so size should be approximately equal).

- [ ] **Step 2: Gate 2 — Darwin binary builds**

```bash
make build-darwin
file bin/stellar-darwin-arm64
```

Expected: Mach-O 64-bit executable arm64.

- [ ] **Step 3: Gate 3 — Windows binary builds**

```bash
make build-windows
file bin/stellar-windows-amd64.exe
```

Expected: PE32+ executable (console) x86-64.

- [ ] **Step 4: Gate 4 — Tests green on macOS**

```bash
GOOS=darwin go test ./...
```

Expected: PASS.

- [ ] **Step 5: Gate 5 — No Linux-only paths in darwin binary**

```bash
strings bin/stellar-darwin-arm64 | grep -E '/(proc|sys|mnt)/|/etc/(network|resolv)' | head -20
```

Expected: 0 matches (no output). If matches appear, they're a regression — find the leaking impl and fix it before declaring done.

- [ ] **Step 6: Gate 6 — No Linux-only symbols in darwin binary**

```bash
nm bin/stellar-darwin-arm64 2>/dev/null | grep -E 'wlr_randr|nmcli|mount\.cifs' | head -10
```

Expected: 0 matches.

- [ ] **Step 7: Gate 7 — Pi backend behavioural verification (re-deploy)**

```bash
scp bin/stellar-arm64 stellar.lan:~/stellar-backend/stellar.new
ssh stellar.lan "sudo systemctl stop stellar-backend && mv ~/stellar-backend/stellar.new ~/stellar-backend/stellar && sudo systemctl start stellar-backend"
sleep 5
# LCD power tile
ssh stellar.lan "curl -s -X POST http://localhost:3000/api/v1/lcd/off 2>/dev/null || echo 'socketio-only'"
# Network status
ssh stellar.lan "curl -s http://localhost:3000/api/v1/network" | python3 -m json.tool
# NAS browse (no actual mount, just discovery — frontend would call this)
ssh stellar.lan "journalctl -u stellar-backend --since '1 minute ago' | tail -30"
```

Expected: LCD power tile still works on the LCD (visible flicker — verify via LCD or via `pushLcdStatus` event), `/api/v1/network` returns the current Pi network state, journal shows no panic or new error spam.

- [ ] **Step 8: Gate 8 — No transport leakage in infra packages**

```bash
grep -rn "zishang520\|transport/socketio" internal/infra/
```

Expected: 0 matches. If anything turns up, fix the import direction before commit.

- [ ] **Step 9: Defer commit to Task 5.3** (no file changes in this task — gates are read-only verification)

---

### Task 5.3: Update `docs/ARCHITECTURE.md` + final commit

**Files:**
- Modify: `docs/ARCHITECTURE.md` (add a "Cross-platform abstraction layer" section)

- [ ] **Step 1: Read existing ARCHITECTURE.md**

```bash
ls docs/ARCHITECTURE.md && wc -l docs/ARCHITECTURE.md
```

- [ ] **Step 2: Append a new section**

After the existing "MPD as source of truth" section (or at the bottom if no obvious anchor), append:

```markdown
## Cross-platform abstraction layer (M1.A)

The backend builds for `linux/arm64` (Pi production), `darwin/arm64` (Mac
interim host), and `windows/amd64` (long-term Plan B host, stubs only).
Platform-specific logic is concentrated in three infra packages and one
domain-package refactor:

| Package | Role | Linux | Darwin | Windows |
|---|---|---|---|---|
| `internal/infra/paths` | Filesystem layout + mount enumeration + system identity | `/data/stellar`, `/mnt/{NAS,USB}`, parses `/proc/mounts` and `/proc/cpuinfo` | `~/Library/Application Support/stellar`, `/Volumes/stellar-nas`, parses `mount(8)` + `sysctl hw.model` | `%LOCALAPPDATA%\stellar`, `ErrUnsupported` for mounts |
| `internal/infra/lcd` | LCD power control + status broadcast | wlr-randr + DPMS + backlight sysfs + vcgencmd + xrandr fallback chain | stub: `Status` always reports `IsOn=true`, `Set` returns `ErrUnsupported` | same as darwin |
| `internal/infra/netinfo` | Network state read + `pushNetworkStatus` broadcast | reads `/sys/class/net/*`, `iwgetid`, `/proc/net/wireless` | `networksetup -listallhardwareports` + `/sbin/ifconfig` + `airport -I` | stub: returns `Status{Type: "none"}` |
| `internal/domain/sources` | NAS share mount + LAN discovery | `mount -t {cifs,nfs}`, `nmblookup`, `avahi-browse`, `smbclient -L` | `/sbin/mount_smbfs`, `/sbin/mount_nfs`, `/usr/bin/dns-sd -B _smb._tcp.`, `/usr/bin/smbutil view` | pure stubs returning `ErrUnsupported` |

**Selection pattern:** each package exposes a `NewPlatform()` constructor
selected by `//go:build linux|darwin|windows` tags. The transport layer
(`internal/transport/socketio/server.go`) implements the `Broadcaster` and
`HandlerRegistrar` interfaces that infra packages depend on, so
`internal/infra/*` never imports `github.com/zishang520/socket.io/v3`.

**Build targets:**
- `make build` — Linux ARM64 (Pi, default)
- `make build-darwin` — Darwin ARM64 (Mac interim host for M1.C cutover)
- `make build-windows` — Windows AMD64 (Plan B; stubs only)

**Falsifiable gate:**

```
strings bin/stellar-darwin-arm64 | grep -E '/(proc|sys|mnt)/|/etc/(network|resolv)'
```

Must return zero matches — proves the darwin binary contains no leaked
Linux-only paths.
```

- [ ] **Step 3: Verify final build + commit**

```bash
make build
make build-darwin
make build-windows
go test ./...
```

Expected: all green.

```bash
git add docs/ARCHITECTURE.md cmd/stellar/main.go internal/transport/socketio/system.go
git commit -m "$(cat <<'EOF'
refactor(m1a): final wiring, system.go portability, docs

  * cmd/stellar/main.go: switch sources.NewLinuxMounter/Discoverer to
    sources.NewPlatformMounter/Discoverer — closes the loop on platform
    selection for NAS sources
  * internal/transport/socketio/system.go: replace direct /proc/cpuinfo
    read with paths.SystemHardware() — last Linux-only path in the
    transport layer

All falsifiable success criteria gated by `strings`/`nm`:
  * strings bin/stellar-darwin-arm64 | grep '/(proc|sys|mnt)/|/etc/(network|resolv)'
    → 0 matches
  * nm bin/stellar-darwin-arm64 | grep 'wlr_randr|nmcli|mount.cifs'
    → 0 matches
  * make build / build-darwin / build-windows all green
  * No grep of internal/infra/ matches zishang520 or transport/socketio
  * Pi backend redeployed: LCD power tile + pushNetworkStatus + NAS
    browse all still work

Add docs/ARCHITECTURE.md "Cross-platform abstraction layer" section
documenting the package table, selection pattern, build targets, and
falsifiable gate.

Final commit of M1.A backend portability layer. Spec:
docs/superpowers/specs/2026-05-13-m1a-backend-portability-design.md
EOF
)"
```

---

## Self-review

After all six commit groups land, run:

- [ ] **Spec coverage:** Walk through `docs/superpowers/specs/2026-05-13-m1a-backend-portability-design.md` decisions 1-10. Each maps to:
  - Decision 1 (Mac impl scope): Tasks 1.3 + 2.3 + 3a.4 + 4.3 + 4.4
  - Decision 2 (paths package): Commit Group 1
  - Decision 3 (build tags + NewPlatform): Tasks 1.2-1.4, 2.2-2.3, 3a.2-3a.4, 4.1-4.2
  - Decision 4 (Approach 2 package shape): Tasks 2.4 + 3a.5
  - Decision 5 (Broadcaster interface): Tasks 2.1 + 2.4 + 3a.1 + 3a.5
  - Decision 6 (Windows stubs everywhere): Tasks 1.4 + 2.3 + 3a.4 + 4.2
  - Decision 7 (netinfo dedup, split into 3a/3b): Commit Groups 3a + 3b
  - Decision 8 (per-platform tests): all `*_linux_test.go` / `*_darwin_test.go` files added throughout
  - Decision 9 (Makefile build-darwin AND build-windows): Task 2.4 Step 5
  - Decision 10 (falsifiable success gate widened to `/etc/(network|resolv)`, Windows now buildable, netinfo byte-equality): Task 5.2 Steps 5-7 + Task 3b.1

- [ ] **Placeholder scan:** Search this plan for "TBD", "TODO", "implement later", "Similar to Task". Expected: 0 matches.

- [ ] **Type consistency:**
  - `lcd.Controller.Status()` returns `(Status, error)` everywhere ✓
  - `netinfo.Reporter.GetStatus()` returns `Status` (no error) everywhere ✓
  - `Broadcaster.Emit(event string, payload any)` consistent across lcd / netinfo ✓
  - `HandlerRegistrar.OnRequest(event string, handler func(args ...any) any)` consistent ✓
  - `sources.NewPlatformMounter() Mounter` and `NewPlatformDiscoverer() Discoverer` consistent across `platform_{linux,darwin,windows}.go` ✓

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-14-m1a-backend-portability-plan.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Heavy phase across 6 commits; per-task review surfaces drift early.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
