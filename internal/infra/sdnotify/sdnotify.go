// Package sdnotify provides a minimal sd_notify client that communicates
// with systemd's socket-activation / notify mechanism via a unixgram
// datagram to the path named by NOTIFY_SOCKET.
//
// When NOTIFY_SOCKET is empty (macOS dev, plain shell invocations, tests
// that do not set it) every function is a safe no-op that returns nil.
//
// No external dependencies are introduced — only the Go standard library
// is used. The implementation follows the sd_notify(3) wire protocol:
// one NUL-terminated or bare newline-separated ASCII datagram per call.
package sdnotify

import (
	"context"
	"net"
	"os"
	"strings"
	"time"
)

// Notify sends a single state string to the systemd notification socket.
// If NOTIFY_SOCKET is unset or empty, the call is a no-op and returns nil.
//
// state should be one of the standard sd_notify fields, e.g. "READY=1",
// "WATCHDOG=1", "STATUS=some text". Multiple fields may be combined with
// newlines: "READY=1\nSTATUS=serving".
func Notify(state string) error {
	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		return nil
	}

	// systemd may use the abstract namespace by prefixing the path with "@".
	// Abstract sockets are identified to the kernel by a NUL byte as the
	// first character of the name.
	if strings.HasPrefix(socketPath, "@") {
		socketPath = "\x00" + socketPath[1:]
	}

	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{
		Name: socketPath,
		Net:  "unixgram",
	})
	if err != nil {
		return err
	}

	_, writeErr := conn.Write([]byte(state))
	// Always attempt close; return the first non-nil error encountered.
	closeErr := conn.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// Ready sends "READY=1" to inform systemd that the process has finished its
// initialisation and is ready to handle requests. Safe no-op when
// NOTIFY_SOCKET is unset.
func Ready() error {
	return Notify("READY=1")
}

// Watchdog sends "WATCHDOG=1" to reset the systemd watchdog timer. Must be
// called at least once per WatchdogSec interval (halving the interval is the
// recommended practice). Safe no-op when NOTIFY_SOCKET is unset.
func Watchdog() error {
	return Notify("WATCHDOG=1")
}

// Status sends "STATUS=<s>" to update the human-readable status string shown
// by `systemctl status`. Safe no-op when NOTIFY_SOCKET is unset.
func Status(s string) error {
	return Notify("STATUS=" + s)
}

// StartWatchdog launches a background goroutine that pings the watchdog every
// interval. The ping is sent only when alive() returns true — if the function
// returns false the goroutine deliberately skips the ping so systemd can
// restart a wedged process after WatchdogSec expires.
//
// The goroutine exits when ctx is cancelled. Callers do not need to hold a
// reference to the goroutine; it is self-contained.
//
// alive should be a cheap, non-blocking function. It is called once per tick
// inside the select loop. A permanently-true closure (func() bool { return true })
// is correct for most uses — the watchdog's purpose is to catch hard deadlocks
// in the main goroutine, not application-level health checks (those belong in
// /ready).
func StartWatchdog(ctx context.Context, interval time.Duration, alive func() bool) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if alive() {
					_ = Watchdog() // best-effort; errors are non-fatal
				}
			}
		}
	}()
}
