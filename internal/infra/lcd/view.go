package lcd

import (
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

// View names a screen the LCD kiosk can show. The values are the frontend's
// `ViewType` strings verbatim (Volumio2-UI src/lib/stores/navigation.ts) —
// they travel over the wire unchanged, so they must not drift from it.
type View string

const (
	ViewPlayer   View = "player"
	ViewLibrary  View = "library"
	ViewQueue    View = "queue"
	ViewSettings View = "settings"
	ViewVuMeter  View = "vu-meter"
)

// ParseView validates a wire string against the known views. Unknown values
// are rejected rather than passed through: the kiosk would silently render
// nothing for a view it doesn't have a branch for.
func ParseView(s string) (View, error) {
	switch View(s) {
	case ViewPlayer, ViewLibrary, ViewQueue, ViewSettings, ViewVuMeter:
		return View(s), nil
	default:
		return "", fmt.Errorf("lcd: unknown view %q", s)
	}
}

// ViewStatus is the `pushLcdView` payload.
//
// PreviousView is what makes "flip to the VU meter and back" work from a
// client that has no history of its own: a remote sends the VU meter to go
// there, and sends PreviousView to come back to whatever the kiosk had been
// showing.
type ViewStatus struct {
	View         View `json:"view"`
	PreviousView View `json:"previousView"`
}

// ViewState tracks which screen the LCD kiosk is showing. It is pure
// coordination state — no hardware is involved, and it exists only so remote
// clients and the kiosk agree on one answer.
//
// The backend is authoritative: the kiosk reports its own local navigation
// back here, so a user tapping NavColumn on the panel itself keeps remotes in
// sync rather than stranding them on a stale view.
type ViewState struct {
	mu       sync.RWMutex
	current  View
	previous View
}

// NewViewState returns a ViewState parked on the player view, which is what
// the kiosk boots into.
func NewViewState() *ViewState {
	return &ViewState{current: ViewPlayer, previous: ViewPlayer}
}

// Status reads the current and previous views.
func (v *ViewState) Status() ViewStatus {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return ViewStatus{View: v.current, PreviousView: v.previous}
}

// Set moves to view, remembering the one being left. The bool reports whether
// anything actually moved; a redundant Set is a no-op that leaves the
// remembered view intact, so the transport layer can skip the broadcast
// instead of bouncing an echo back at the kiosk.
func (v *ViewState) Set(view View) (ViewStatus, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.current == view {
		return ViewStatus{View: v.current, PreviousView: v.previous}, false
	}

	v.previous = v.current
	v.current = view
	return ViewStatus{View: v.current, PreviousView: v.previous}, true
}

// BroadcastView Emits the current view through the supplied Broadcaster.
// Called from the transport layer after every accepted view change.
func BroadcastView(brd Broadcaster, v *ViewState) {
	status := v.Status()
	brd.Emit("pushLcdView", status)
	log.Debug().
		Str("view", string(status.View)).
		Str("previousView", string(status.PreviousView)).
		Msg("Broadcast LCD view")
}
