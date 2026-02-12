package socketio

import (
	"sync"
	"time"
)

// BroadcastDebouncer collapses rapid MPD subsystem events into batched broadcasts.
// Multiple subsystem changes within the debounce window result in a single
// broadcast for each affected type (state and/or queue).
type BroadcastDebouncer struct {
	window        time.Duration
	stateCallback func()
	queueCallback func()

	mu           sync.Mutex
	pendingState bool
	pendingQueue bool
	timer        *time.Timer
	stopped      bool
}

// NewBroadcastDebouncer creates a debouncer with the given window duration.
// stateCallback is called when player/mixer/options events need broadcasting.
// queueCallback is called when playlist events need broadcasting.
func NewBroadcastDebouncer(window time.Duration, stateCallback, queueCallback func()) *BroadcastDebouncer {
	return &BroadcastDebouncer{
		window:        window,
		stateCallback: stateCallback,
		queueCallback: queueCallback,
	}
}

// Trigger records that the given MPD subsystem has changed.
// The actual broadcast callbacks are deferred until the debounce window elapses
// without further triggers.
func (d *BroadcastDebouncer) Trigger(subsystem string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stopped {
		return
	}

	switch subsystem {
	case "player", "mixer", "options":
		d.pendingState = true
	case "playlist":
		d.pendingState = true
		d.pendingQueue = true
	}

	// Reset the timer
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.window, d.flush)
}

// flush fires callbacks for any pending flags and resets them.
func (d *BroadcastDebouncer) flush() {
	d.mu.Lock()
	doState := d.pendingState
	doQueue := d.pendingQueue
	d.pendingState = false
	d.pendingQueue = false
	d.mu.Unlock()

	if doState && d.stateCallback != nil {
		d.stateCallback()
	}
	if doQueue && d.queueCallback != nil {
		d.queueCallback()
	}
}

// Stop prevents any further callbacks from firing.
func (d *BroadcastDebouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.stopped = true
	if d.timer != nil {
		d.timer.Stop()
	}
	d.pendingState = false
	d.pendingQueue = false
}
