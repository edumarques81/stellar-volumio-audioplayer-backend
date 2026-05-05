package socketio

import (
	"errors"
	"testing"
)

func TestSystemActions_LocalCallerAllowed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, ip string
	}{
		{"ipv4 loopback", "127.0.0.1"},
		{"ipv6 loopback", "::1"},
		{"ipv4 loopback range 127.0.0.5", "127.0.0.5"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calledShutdown, calledReboot bool
			h := NewSystemActionHandlers(SystemActionDeps{
				Shutdown: func() error { calledShutdown = true; return nil },
				Reboot:   func() error { calledReboot = true; return nil },
			})

			if err := h.handleShutdownInternal(tc.ip); err != nil {
				t.Fatalf("expected allow, got %v", err)
			}
			if !calledShutdown {
				t.Fatalf("Shutdown dep was not invoked")
			}
			if calledReboot {
				t.Fatalf("Reboot dep should NOT have been invoked by shutdown")
			}

			calledShutdown = false
			if err := h.handleRebootInternal(tc.ip); err != nil {
				t.Fatalf("expected allow on reboot, got %v", err)
			}
			if !calledReboot {
				t.Fatalf("Reboot dep was not invoked")
			}
			if calledShutdown {
				t.Fatalf("Shutdown dep should NOT have been invoked by reboot")
			}
		})
	}
}

func TestSystemActions_RemoteCallerRefused(t *testing.T) {
	t.Parallel()
	for _, ip := range []string{
		"192.168.1.42",
		"10.0.0.5",
		"203.0.113.7",
		"fe80::1",
		"2001:db8::1",
		"",         // empty (no handshake)
		"not-an-ip", // unparseable
	} {
		ip := ip
		t.Run("ip="+ip, func(t *testing.T) {
			t.Parallel()
			var called bool
			h := NewSystemActionHandlers(SystemActionDeps{
				Shutdown: func() error { called = true; return nil },
				Reboot:   func() error { called = true; return nil },
			})

			err := h.handleShutdownInternal(ip)
			if err == nil {
				t.Fatalf("expected refusal for non-loopback %q", ip)
			}
			if !errors.Is(err, errNonLoopback) {
				t.Fatalf("expected errNonLoopback for %q, got %v", ip, err)
			}
			if msg := clientErrorMessage(err); msg != "unauthorized" {
				t.Fatalf("client-facing message for %q must be %q, got %q", ip, "unauthorized", msg)
			}
			// Internal-facing wrapped error must not leak the IP back to the
			// client; the unwrapped error string is internal-only (used in
			// the warn log). It MAY contain the IP — that's fine for ops.
			if called {
				t.Fatalf("non-loopback %q should not have triggered shutdown dep", ip)
			}

			err = h.handleRebootInternal(ip)
			if err == nil {
				t.Fatalf("expected refusal for non-loopback %q on reboot", ip)
			}
			if !errors.Is(err, errNonLoopback) {
				t.Fatalf("expected errNonLoopback on reboot for %q, got %v", ip, err)
			}
			if msg := clientErrorMessage(err); msg != "unauthorized" {
				t.Fatalf("reboot client-facing message for %q must be %q, got %q", ip, "unauthorized", msg)
			}
			if called {
				t.Fatalf("non-loopback %q should not have triggered reboot dep", ip)
			}
		})
	}
}

// TestClientErrorMessage_PropagatesDepErrors confirms that loopback dep
// failures (e.g. /sbin/shutdown permission denied) still surface the real
// error to the client — only auth refusals are sanitized.
func TestClientErrorMessage_PropagatesDepErrors(t *testing.T) {
	t.Parallel()
	depErr := errors.New("permission denied")
	if msg := clientErrorMessage(depErr); msg != "permission denied" {
		t.Fatalf("dep error must propagate verbatim, got %q", msg)
	}
}

func TestSystemActions_ShutdownErrorPropagates(t *testing.T) {
	t.Parallel()
	h := NewSystemActionHandlers(SystemActionDeps{
		Shutdown: func() error { return errors.New("permission denied") },
		Reboot:   func() error { return nil },
	})
	if err := h.handleShutdownInternal("127.0.0.1"); err == nil {
		t.Fatalf("expected error to propagate")
	}
}

func TestSystemActions_DefaultDepsNotNil(t *testing.T) {
	t.Parallel()
	// When deps are not provided, the handler installs DefaultShutdown/Reboot
	// so production wiring works without explicit deps.
	h := NewSystemActionHandlers(SystemActionDeps{})
	if h.deps.Shutdown == nil {
		t.Fatalf("default Shutdown dep not installed (got nil)")
	}
	if h.deps.Reboot == nil {
		t.Fatalf("default Reboot dep not installed (got nil)")
	}
}

func TestIsLoopback(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"127.0.0.1":   true,
		"127.1.2.3":   true,
		"::1":         true,
		"192.168.1.1": false,
		"10.0.0.1":    false,
		"":            false,
		"garbage":     false,
	}
	for ip, want := range cases {
		if got := isLoopback(ip); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", ip, got, want)
		}
	}
}
