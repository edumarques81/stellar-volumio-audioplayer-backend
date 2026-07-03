package sdnotify_test

import (
	"context"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/sdnotify"
)

// listenUnixgram creates a temporary unixgram listener and returns its path
// plus a channel that receives every datagram as a string.
//
// macOS caps Unix socket paths at 104 bytes, so we create a very short path
// under /tmp directly rather than using t.TempDir() (whose paths can exceed
// the limit in deeply-nested test hierarchies).
func listenUnixgram(t *testing.T) (sockPath string, msgs <-chan string) {
	t.Helper()

	// Create a temp dir with a short name under /tmp.
	dir, err := os.MkdirTemp("/tmp", "sdn")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("cleanup RemoveAll %s: %v", dir, err)
		}
	})

	sockPath = dir + "/n.sock" // short enough for the 104-byte macOS limit

	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sockPath, Net: "unixgram"})
	if err != nil {
		t.Fatalf("listenUnixgram: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Logf("cleanup conn.Close: %v", err)
		}
	})

	ch := make(chan string, 16)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, _, err := conn.ReadFromUnix(buf)
			if err != nil {
				return // connection closed on cleanup
			}
			ch <- string(buf[:n])
		}
	}()

	return sockPath, ch
}

// TestNotifyEmptySocketIsNoop ensures Notify is a no-op when NOTIFY_SOCKET is empty.
// Note: t.Setenv and t.Parallel are mutually exclusive in Go testing.
func TestNotifyEmptySocketIsNoop(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")

	if err := sdnotify.Notify("READY=1"); err != nil {
		t.Errorf("Notify with empty NOTIFY_SOCKET = %v, want nil", err)
	}
}

// TestReadyDelivers asserts Ready() sends exactly "READY=1" to the socket.
func TestReadyDelivers(t *testing.T) {
	sockPath, msgs := listenUnixgram(t)
	t.Setenv("NOTIFY_SOCKET", sockPath)

	if err := sdnotify.Ready(); err != nil {
		t.Fatalf("Ready() error: %v", err)
	}

	select {
	case got := <-msgs:
		if got != "READY=1" {
			t.Errorf("got %q, want %q", got, "READY=1")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Ready() datagram")
	}
}

// TestWatchdogDelivers asserts Watchdog() sends exactly "WATCHDOG=1".
func TestWatchdogDelivers(t *testing.T) {
	sockPath, msgs := listenUnixgram(t)
	t.Setenv("NOTIFY_SOCKET", sockPath)

	if err := sdnotify.Watchdog(); err != nil {
		t.Fatalf("Watchdog() error: %v", err)
	}

	select {
	case got := <-msgs:
		if got != "WATCHDOG=1" {
			t.Errorf("got %q, want %q", got, "WATCHDOG=1")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Watchdog() datagram")
	}
}

// TestStatusDelivers asserts Status(s) sends "STATUS=<s>".
func TestStatusDelivers(t *testing.T) {
	sockPath, msgs := listenUnixgram(t)
	t.Setenv("NOTIFY_SOCKET", sockPath)

	if err := sdnotify.Status("starting up"); err != nil {
		t.Fatalf("Status() error: %v", err)
	}

	select {
	case got := <-msgs:
		want := "STATUS=starting up"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Status() datagram")
	}
}

// TestStartWatchdogPingsWhenAlive verifies that StartWatchdog sends WATCHDOG=1
// when alive() returns true.
func TestStartWatchdogPingsWhenAlive(t *testing.T) {
	sockPath, msgs := listenUnixgram(t)
	t.Setenv("NOTIFY_SOCKET", sockPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var pinged atomic.Bool
	sdnotify.StartWatchdog(ctx, 20*time.Millisecond, func() bool { return true })

	// Wait for at least one ping, with a generous deadline.
	deadline := time.After(500 * time.Millisecond)
	for !pinged.Load() {
		select {
		case got := <-msgs:
			if got == "WATCHDOG=1" {
				pinged.Store(true)
			}
		case <-deadline:
			t.Fatal("timeout waiting for watchdog ping when alive=true")
		}
	}
}

// TestStartWatchdogSkipsWhenDead verifies that StartWatchdog does NOT send
// any datagram when alive() returns false.
func TestStartWatchdogSkipsWhenDead(t *testing.T) {
	sockPath, msgs := listenUnixgram(t)
	t.Setenv("NOTIFY_SOCKET", sockPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sdnotify.StartWatchdog(ctx, 20*time.Millisecond, func() bool { return false })

	// Give the goroutine time to fire several intervals; assert no message arrives.
	select {
	case got := <-msgs:
		t.Errorf("received unexpected datagram %q when alive=false", got)
	case <-time.After(120 * time.Millisecond):
		// no ping received — correct
	}
}

// TestStartWatchdogStopsOnCancel verifies that the goroutine exits when ctx
// is cancelled (no further pings after cancel).
func TestStartWatchdogStopsOnCancel(t *testing.T) {
	sockPath, msgs := listenUnixgram(t)
	t.Setenv("NOTIFY_SOCKET", sockPath)

	ctx, cancel := context.WithCancel(context.Background())

	sdnotify.StartWatchdog(ctx, 20*time.Millisecond, func() bool { return true })

	// Wait for at least one ping.
	select {
	case <-msgs:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no initial ping received")
	}

	// Cancel context and drain any in-flight messages.
	cancel()
	time.Sleep(50 * time.Millisecond)
	for {
		select {
		case <-msgs:
		default:
			goto drained
		}
	}
drained:

	// Now assert no more pings arrive after a couple of intervals.
	select {
	case got := <-msgs:
		t.Errorf("received ping %q after context cancel", got)
	case <-time.After(100 * time.Millisecond):
		// correct — goroutine stopped
	}
}
