package socketio

import (
	"testing"
)

func TestConnectionLimiterLocalhostAlwaysAllowed(t *testing.T) {
	cl := NewConnectionLimiter(1)

	// Multiple localhost connections should all be allowed
	for i := 0; i < 10; i++ {
		allowed, evicted := cl.TryAdd("local-"+string(rune('a'+i)), "127.0.0.1")
		if !allowed {
			t.Errorf("localhost connection %d should be allowed", i)
		}
		if evicted != "" {
			t.Errorf("localhost connection %d should not evict anyone, got %s", i, evicted)
		}
	}
}

func TestConnectionLimiterIPv6LocalhostAllowed(t *testing.T) {
	cl := NewConnectionLimiter(1)

	allowed, evicted := cl.TryAdd("ipv6-local", "::1")
	if !allowed {
		t.Error("IPv6 localhost should be allowed")
	}
	if evicted != "" {
		t.Errorf("IPv6 localhost should not evict anyone, got %s", evicted)
	}
}

func TestConnectionLimiterFirstExternalAllowed(t *testing.T) {
	cl := NewConnectionLimiter(1)

	allowed, evicted := cl.TryAdd("ext-1", "192.168.1.100")
	if !allowed {
		t.Error("first external connection should be allowed")
	}
	if evicted != "" {
		t.Errorf("first external should not evict anyone, got %s", evicted)
	}
}

func TestConnectionLimiterSecondExternalEvictsOldest(t *testing.T) {
	cl := NewConnectionLimiter(1)

	// First external
	cl.TryAdd("ext-1", "192.168.1.100")

	// Second external should evict first
	allowed, evicted := cl.TryAdd("ext-2", "192.168.1.101")
	if !allowed {
		t.Error("second external connection should be allowed")
	}
	if evicted != "ext-1" {
		t.Errorf("expected eviction of ext-1, got %q", evicted)
	}
}

func TestConnectionLimiterLocalConnectionsUnlimited(t *testing.T) {
	cl := NewConnectionLimiter(1)

	// Fill external slot
	cl.TryAdd("ext-1", "192.168.1.100")

	// Local connections should not be affected by external limit
	allowed, evicted := cl.TryAdd("local-1", "127.0.0.1")
	if !allowed {
		t.Error("local should be allowed even with external limit reached")
	}
	if evicted != "" {
		t.Errorf("local connection should not evict anyone, got %s", evicted)
	}
}

func TestConnectionLimiterRemoveFreesSlot(t *testing.T) {
	cl := NewConnectionLimiter(1)

	// Fill external slot
	cl.TryAdd("ext-1", "192.168.1.100")

	// Remove it
	cl.Remove("ext-1")

	// New external should be allowed without eviction
	allowed, evicted := cl.TryAdd("ext-2", "192.168.1.101")
	if !allowed {
		t.Error("external should be allowed after removal")
	}
	if evicted != "" {
		t.Errorf("should not evict after removal freed a slot, got %s", evicted)
	}
}

func TestConnectionLimiterEvictedIDReturned(t *testing.T) {
	cl := NewConnectionLimiter(1)

	cl.TryAdd("first", "10.0.0.1")
	_, evicted := cl.TryAdd("second", "10.0.0.2")

	if evicted != "first" {
		t.Errorf("expected evicted ID 'first', got %q", evicted)
	}

	// Third connection should evict second
	_, evicted = cl.TryAdd("third", "10.0.0.3")
	if evicted != "second" {
		t.Errorf("expected evicted ID 'second', got %q", evicted)
	}
}

func TestConnectionLimiterDuplicateAddIsIdempotent(t *testing.T) {
	cl := NewConnectionLimiter(1)

	cl.TryAdd("ext-1", "192.168.1.100")

	// Adding same ID again should not evict or error
	allowed, evicted := cl.TryAdd("ext-1", "192.168.1.100")
	if !allowed {
		t.Error("duplicate add should be allowed")
	}
	if evicted != "" {
		t.Errorf("duplicate add should not evict, got %s", evicted)
	}
}

func TestConnectionLimiterRemoveNonexistent(t *testing.T) {
	cl := NewConnectionLimiter(1)

	// Should not panic
	cl.Remove("nonexistent")
}

func TestIsLocalIP(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"192.168.1.100", false},
		{"10.0.0.1", false},
		{"0.0.0.0", false},
	}

	for _, tc := range tests {
		if got := isLocalIP(tc.ip); got != tc.expected {
			t.Errorf("isLocalIP(%q) = %v, want %v", tc.ip, got, tc.expected)
		}
	}
}
