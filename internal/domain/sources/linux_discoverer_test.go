package sources

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// stubHangingCommand returns an exec.Cmd that runs `sleep` long enough to
// exceed the caller's context deadline. It uses real CommandContext so the
// process is actually killed when the ctx fires, mirroring production
// behavior (rather than asserting on a fake Cmd).
func stubHangingCommand(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "sleep", "30")
}

// withInjected swaps execCommand and discoverTimeout for the duration of the
// test, restoring originals via t.Cleanup.
func withInjected(t *testing.T, runner func(ctx context.Context, name string, arg ...string) *exec.Cmd, timeout time.Duration) {
	t.Helper()
	origExec := execCommand
	origTimeout := discoverTimeout
	execCommand = runner
	discoverTimeout = timeout
	t.Cleanup(func() {
		execCommand = origExec
		discoverTimeout = origTimeout
	})
}

// TestLinuxDiscoverer_DiscoverDevices_Timeout asserts that when both
// nmblookup and avahi-browse hang past discoverTimeout, DiscoverDevices
// surfaces a "discovery timed out" error rather than silently returning an
// empty slice (which would be indistinguishable from "no NAS on the network").
func TestLinuxDiscoverer_DiscoverDevices_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX `sleep`")
	}

	// 50ms timeout is plenty to detect the deadline path; sleep 30 will be
	// killed long before it would naturally end.
	withInjected(t, stubHangingCommand, 50*time.Millisecond)

	d := NewLinuxDiscoverer()

	start := time.Now()
	devices, err := d.DiscoverDevices(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected timeout error, got nil (devices=%v)", devices)
	}
	if !errors.Is(err, errDiscoverTimeout) {
		t.Fatalf("expected errDiscoverTimeout, got %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("expected zero devices on timeout, got %d", len(devices))
	}
	// Both helpers run sequentially with 50ms budget each, so total should be
	// well under 1s. If we exceeded that, the deadline plumbing isn't working.
	if elapsed > time.Second {
		t.Errorf("DiscoverDevices took %v, expected to give up well under 1s", elapsed)
	}
}

// TestLinuxDiscoverer_DiscoverDevices_ToolMissing asserts that the legacy
// "tool not installed" path is preserved: if exec returns a non-timeout
// error (e.g., binary not found), we log Debug and return no error so a
// missing optional tool doesn't surface as a user-visible failure.
func TestLinuxDiscoverer_DiscoverDevices_ToolMissing(t *testing.T) {
	missingRunner := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		// Point exec at a path that does not exist; cmd.Output() will fail
		// with an exec.Error, NOT a context deadline exceeded.
		return exec.CommandContext(ctx, "/nonexistent/binary/__definitely_missing__")
	}
	withInjected(t, missingRunner, 5*time.Second)

	d := NewLinuxDiscoverer()
	devices, err := d.DiscoverDevices(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for missing tool, got %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("expected zero devices when tool is missing, got %d", len(devices))
	}
}

// TestService_DiscoverNasDevices_TimeoutSurfacesAsError verifies the full
// service-layer wiring: a discoverer timeout becomes a non-empty
// DiscoverResult.Error string the frontend can render.
func TestService_DiscoverNasDevices_TimeoutSurfacesAsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX `sleep`")
	}
	withInjected(t, stubHangingCommand, 50*time.Millisecond)

	tmpDir := t.TempDir()
	configPath := tmpDir + "/sources.json"
	s, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	s.SetDiscoverer(NewLinuxDiscoverer())

	result, err := s.DiscoverNasDevices(context.Background())
	if err != nil {
		t.Fatalf("DiscoverNasDevices returned unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected DiscoverResult.Error to be populated on timeout")
	}
	if !strings.Contains(strings.ToLower(result.Error), "timed out") {
		t.Errorf("expected DiscoverResult.Error to mention timeout, got %q", result.Error)
	}
	if len(result.Devices) != 0 {
		t.Errorf("expected zero devices on timeout, got %d", len(result.Devices))
	}
}
