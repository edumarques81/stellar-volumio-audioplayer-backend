//go:build linux

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

// stubHangingMountCommand returns an exec.Cmd that runs `sleep` long enough to
// exceed the caller's context deadline. It uses real CommandContext so the
// process is actually killed by the kernel when ctx fires — exactly what we
// rely on in production to unblock the handler goroutine when `mount -t nfs`
// hangs against a dead share.
func stubHangingMountCommand(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "sleep", "30")
}

// withInjectedMountRunner swaps mountCommand for the duration of a test,
// restoring the original via t.Cleanup. Mirrors withInjected in the
// discoverer tests.
func withInjectedMountRunner(t *testing.T, runner func(ctx context.Context, name string, arg ...string) *exec.Cmd) {
	t.Helper()
	orig := mountCommand
	mountCommand = runner
	t.Cleanup(func() {
		mountCommand = orig
	})
}

// TestLinuxMounter_Mount_ContextTimeout asserts that Mount honours a short
// context deadline: when the underlying `sudo mount` process hangs (the live
// failure mode for "mount NFS share that is actually an SMB host"), the
// goroutine returns within ~the deadline rather than blocking forever.
func TestLinuxMounter_Mount_ContextTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX `sleep`")
	}

	withInjectedMountRunner(t, stubHangingMountCommand)

	m := NewLinuxMounter()
	share := &NasShare{
		ID:         "test",
		Name:       "Test",
		IP:         "192.168.1.99",
		Path:       "Music",
		FSType:     "nfs",
		MountPoint: "/tmp/stellar-mount-test",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := m.Mount(ctx, share)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected timeout error from Mount, got nil")
	}
	// We just need the context-deadline-exceeded condition surfaced.
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("expected ctx.Err to be DeadlineExceeded, got %v", ctx.Err())
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Mount took %v to honor 100ms ctx deadline (expected <500ms)", elapsed)
	}
}

// TestLinuxMounter_Unmount_ContextTimeout asserts the same context-honouring
// behaviour for Unmount. `sudo umount` can also hang on dead shares.
func TestLinuxMounter_Unmount_ContextTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX `sleep`")
	}

	withInjectedMountRunner(t, stubHangingMountCommand)

	m := NewLinuxMounter()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := m.Unmount(ctx, "/tmp/stellar-mount-test")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected timeout error from Unmount, got nil")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("expected ctx.Err to be DeadlineExceeded, got %v", ctx.Err())
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Unmount took %v to honor 100ms ctx deadline (expected <500ms)", elapsed)
	}
}

// TestService_AddNasShare_ContextPropagation verifies that the service layer
// propagates a short caller context into the mounter and surfaces a clean
// SourceResult with a timeout-flavoured error rather than blocking forever.
// Uses MockMounter that respects the context to simulate the production
// linuxMounter's exec.CommandContext behaviour.
func TestService_AddNasShare_ContextPropagation(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/sources.json"

	mounter := NewMockMounter()
	// Simulate a hung mount: respect ctx.Done(), return ctx.Err if it fires
	// before MountDelay completes.
	mounter.MountDelay = 5 * time.Second
	mounter.MountError = nil

	s, err := NewService(configPath, mounter)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := AddNasShareRequest{
		Name:   "HungShare",
		IP:     "192.168.1.99",
		Path:   "Music",
		FSType: "nfs",
	}

	start := time.Now()
	result, err := s.AddNasShare(ctx, req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AddNasShare returned hard error: %v", err)
	}
	if result == nil {
		t.Fatal("AddNasShare returned nil result")
	}
	if result.Success {
		t.Errorf("AddNasShare succeeded; want failure under deadline")
	}
	if !strings.Contains(strings.ToLower(result.Error), "context") && !strings.Contains(strings.ToLower(result.Error), "deadline") && !strings.Contains(strings.ToLower(result.Error), "timed out") {
		t.Errorf("expected timeout-flavoured error, got %q", result.Error)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("AddNasShare took %v under 50ms ctx (expected to honor deadline)", elapsed)
	}
}
