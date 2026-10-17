// Package display owns what the LCD panel is currently doing and when it
// changes that by itself.
//
// There are three states, not two, because on this panel they are physically
// different things:
//
//	live    — the kiosk's normal UI, panel lit.
//	standby — the panel is still lit and the touch digitizer still works; the
//	          kiosk paints a dimmed clock face. This is the only "off" that
//	          can be undone by touching the screen.
//	off     — the HDMI output is really stopped. The panel answers that by
//	          holding its own touch controller in reset, so it can only be
//	          brought back from a remote client or by the auto-restore
//	          schedule below. Nothing host-side can prevent that: the ILITEK
//	          digitizer is bus-powered from the Pi's own root hub, so VBUS
//	          never drops and the panel does this to itself.
//
// The backend is authoritative so the kiosk and the iPhone cannot disagree
// about which of the three is current.
package display

import "fmt"

// Mode is one of the three display states.
type Mode string

const (
	// ModeLive is the normal kiosk UI.
	ModeLive Mode = "live"
	// ModeStandby is the lit-but-dimmed clock face; touch still wakes it.
	ModeStandby Mode = "standby"
	// ModeOff really stops the display signal; touch dies with it.
	ModeOff Mode = "off"
)

// ParseMode validates a wire string. Unknown values are rejected rather than
// defaulted: guessing here would mean turning the panel off (or failing to)
// on a typo.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeLive, ModeStandby, ModeOff:
		return Mode(s), nil
	default:
		return "", fmt.Errorf("display: unknown mode %q", s)
	}
}

// Lit reports whether the mode needs the display signal on.
func (m Mode) Lit() bool { return m != ModeOff }

// Status is the `pushLcdStatus` payload.
//
// IsOn is kept alongside Mode for the older clients that only ever knew a
// boolean. It is the hardware truth, read back from the controller, not a
// restatement of Mode — if a `wlr-randr` call fails, those two disagree and
// that is exactly what a client should see.
type Status struct {
	Mode Mode `json:"mode"`
	IsOn bool `json:"isOn"`
}
