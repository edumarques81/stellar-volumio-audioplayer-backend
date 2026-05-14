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

// Broadcaster matches the shape used by internal/infra/lcd — the transport
// layer's serverBroadcaster type implements both.
type Broadcaster interface {
	Emit(event string, payload any)
}

// NewPlatform returns the Reporter implementation for the current platform.
func NewPlatform() Reporter { return newPlatform() }

// StartWatcher runs a periodic poll (every 30s) and Emits "pushNetworkStatus"
// when the status changes. Blocks until ctx is canceled. The transport layer
// is expected to call this in a goroutine.
func StartWatcher(ctx context.Context, r Reporter, brd Broadcaster) {
	startWatcher(ctx, r, brd)
}
