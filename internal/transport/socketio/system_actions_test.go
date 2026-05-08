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

func TestSystemActions_TrustedRemoteAllowed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, ip string
		trusted  []string
	}{
		{"single ipv4 in /32 spec", "192.168.86.221", []string{"192.168.86.221"}},
		{"single ipv4 in /24 cidr", "192.168.86.221", []string{"192.168.86.0/24"}},
		{"second of multiple specs", "10.0.0.5", []string{"192.168.86.221", "10.0.0.0/8"}},
		{"ipv6 single host", "fe80::42", []string{"fe80::42"}},
		{"ipv6 cidr", "2001:db8::1234", []string{"2001:db8::/32"}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var called bool
			h, err := NewSystemActionHandlersWithTrusted(SystemActionDeps{
				Shutdown: func() error { called = true; return nil },
				Reboot:   func() error { called = true; return nil },
			}, tc.trusted)
			if err != nil {
				t.Fatalf("constructor errored on valid specs %v: %v", tc.trusted, err)
			}
			if err := h.handleShutdownInternal(tc.ip); err != nil {
				t.Fatalf("expected trusted ip %q to be allowed, got %v", tc.ip, err)
			}
			if !called {
				t.Fatalf("Shutdown dep was not invoked for trusted %q", tc.ip)
			}
			called = false
			if err := h.handleRebootInternal(tc.ip); err != nil {
				t.Fatalf("expected trusted ip %q to be allowed for reboot, got %v", tc.ip, err)
			}
			if !called {
				t.Fatalf("Reboot dep was not invoked for trusted %q", tc.ip)
			}
		})
	}
}

func TestSystemActions_NonTrustedRemoteStillRefused(t *testing.T) {
	t.Parallel()
	// With a trust list, non-matching IPs must still be refused (and still
	// surface as "unauthorized" to the client — same security guarantee).
	h, err := NewSystemActionHandlersWithTrusted(SystemActionDeps{
		Shutdown: func() error { return nil },
		Reboot:   func() error { return nil },
	}, []string{"192.168.86.221", "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("unexpected constructor error: %v", err)
	}
	for _, ip := range []string{
		"192.168.86.222", // adjacent host, not in list
		"172.16.0.1",     // outside any range
		"203.0.113.7",    // public
		"",               // empty
		"not-an-ip",      // unparseable
	} {
		ip := ip
		t.Run("refused="+ip, func(t *testing.T) {
			t.Parallel()
			err := h.handleShutdownInternal(ip)
			if err == nil {
				t.Fatalf("expected refusal for %q", ip)
			}
			if !errors.Is(err, errNonLoopback) {
				t.Fatalf("expected errNonLoopback for %q, got %v", ip, err)
			}
			if msg := clientErrorMessage(err); msg != "unauthorized" {
				t.Fatalf("client message for %q must be 'unauthorized', got %q", ip, msg)
			}
		})
	}
}

func TestNewSystemActionHandlersWithTrusted_EmptyListPreservesLoopbackOnly(t *testing.T) {
	t.Parallel()
	// Empty/nil trusted list means loopback-only — same as the legacy
	// constructor. Verify the gate still allows 127.0.0.1 and refuses LAN.
	for _, specs := range [][]string{nil, {}, {""}, {"  "}} {
		h, err := NewSystemActionHandlersWithTrusted(SystemActionDeps{
			Shutdown: func() error { return nil },
			Reboot:   func() error { return nil },
		}, specs)
		if err != nil {
			t.Fatalf("empty spec list %v should not error, got %v", specs, err)
		}
		if err := h.handleShutdownInternal("127.0.0.1"); err != nil {
			t.Fatalf("loopback must remain allowed with specs=%v, got %v", specs, err)
		}
		err = h.handleShutdownInternal("192.168.86.221")
		if err == nil || !errors.Is(err, errNonLoopback) {
			t.Fatalf("LAN ip must be refused with specs=%v, got %v", specs, err)
		}
	}
}

func TestNewSystemActionHandlersWithTrusted_InvalidSpec(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"not-an-ip",
		"999.999.999.999",
		"192.168.86.0/33",   // mask out of range
		"foo/24",
	} {
		bad := bad
		t.Run("bad="+bad, func(t *testing.T) {
			t.Parallel()
			_, err := NewSystemActionHandlersWithTrusted(SystemActionDeps{}, []string{bad})
			if err == nil {
				t.Fatalf("expected error for invalid spec %q", bad)
			}
		})
	}
}

func TestSystemActions_LegacyConstructorUnchanged(t *testing.T) {
	t.Parallel()
	// The legacy NewSystemActionHandlers must still default to loopback-only
	// behavior (no trusted-remote escape hatch). This guards against an
	// accidental change to the legacy path that would silently widen the gate.
	h := NewSystemActionHandlers(SystemActionDeps{
		Shutdown: func() error { return nil },
		Reboot:   func() error { return nil },
	})
	if err := h.handleShutdownInternal("127.0.0.1"); err != nil {
		t.Fatalf("legacy: loopback must be allowed, got %v", err)
	}
	if err := h.handleShutdownInternal("192.168.86.221"); err == nil || !errors.Is(err, errNonLoopback) {
		t.Fatalf("legacy: LAN ip must be refused, got %v", err)
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
