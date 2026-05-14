// Package lcd controls the LCD display attached to the host (real impl on
// Linux; stubs on darwin/windows where no LCD is attached to the backend
// host). The transport layer drives this package via Controller for power
// reads/writes and BroadcastStatus for push notifications, so it does not
// need to know about LCD internals.
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

// NewPlatform returns the Controller implementation for the current build
// platform (build-tag-selected).
func NewPlatform() Controller { return newPlatform() }
