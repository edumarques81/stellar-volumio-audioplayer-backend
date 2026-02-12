package socketio

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncerRapidPlayerEventsCollapseToOne(t *testing.T) {
	var stateCalls int32
	var queueCalls int32

	d := NewBroadcastDebouncer(50*time.Millisecond,
		func() { atomic.AddInt32(&stateCalls, 1) },
		func() { atomic.AddInt32(&queueCalls, 1) },
	)
	defer d.Stop()

	// Fire 10 rapid player events
	for i := 0; i < 10; i++ {
		d.Trigger("player")
	}

	// Wait for debounce window to elapse
	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt32(&stateCalls); got != 1 {
		t.Errorf("expected 1 state callback, got %d", got)
	}
	if got := atomic.LoadInt32(&queueCalls); got != 0 {
		t.Errorf("expected 0 queue callbacks, got %d", got)
	}
}

func TestDebouncerRapidMixerEventsCollapseToOne(t *testing.T) {
	var stateCalls int32

	d := NewBroadcastDebouncer(50*time.Millisecond,
		func() { atomic.AddInt32(&stateCalls, 1) },
		func() {},
	)
	defer d.Stop()

	// Simulate rapid volume knob events
	for i := 0; i < 20; i++ {
		d.Trigger("mixer")
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for debounce window
	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt32(&stateCalls); got != 1 {
		t.Errorf("expected 1 state callback for rapid mixer events, got %d", got)
	}
}

func TestDebouncerPlaylistTriggersBothStateAndQueue(t *testing.T) {
	var stateCalls int32
	var queueCalls int32

	d := NewBroadcastDebouncer(50*time.Millisecond,
		func() { atomic.AddInt32(&stateCalls, 1) },
		func() { atomic.AddInt32(&queueCalls, 1) },
	)
	defer d.Stop()

	d.Trigger("playlist")

	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt32(&stateCalls); got != 1 {
		t.Errorf("expected 1 state callback for playlist event, got %d", got)
	}
	if got := atomic.LoadInt32(&queueCalls); got != 1 {
		t.Errorf("expected 1 queue callback for playlist event, got %d", got)
	}
}

func TestDebouncerMixedEventsWithinWindow(t *testing.T) {
	var stateCalls int32
	var queueCalls int32

	d := NewBroadcastDebouncer(50*time.Millisecond,
		func() { atomic.AddInt32(&stateCalls, 1) },
		func() { atomic.AddInt32(&queueCalls, 1) },
	)
	defer d.Stop()

	// Mix of player, mixer, and playlist events within the window
	d.Trigger("player")
	d.Trigger("mixer")
	d.Trigger("playlist")
	d.Trigger("options")

	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt32(&stateCalls); got != 1 {
		t.Errorf("expected 1 state callback for mixed events, got %d", got)
	}
	if got := atomic.LoadInt32(&queueCalls); got != 1 {
		t.Errorf("expected 1 queue callback for mixed events, got %d", got)
	}
}

func TestDebouncerSeparateWindowsFireIndependently(t *testing.T) {
	var stateCalls int32

	d := NewBroadcastDebouncer(50*time.Millisecond,
		func() { atomic.AddInt32(&stateCalls, 1) },
		func() {},
	)
	defer d.Stop()

	// First burst
	d.Trigger("player")
	time.Sleep(100 * time.Millisecond) // Wait for first flush

	// Second burst (separate window)
	d.Trigger("player")
	time.Sleep(100 * time.Millisecond) // Wait for second flush

	if got := atomic.LoadInt32(&stateCalls); got != 2 {
		t.Errorf("expected 2 state callbacks for separate windows, got %d", got)
	}
}

func TestDebouncerStopPreventsCallbacks(t *testing.T) {
	var stateCalls int32

	d := NewBroadcastDebouncer(50*time.Millisecond,
		func() { atomic.AddInt32(&stateCalls, 1) },
		func() {},
	)

	d.Trigger("player")
	d.Stop()

	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt32(&stateCalls); got != 0 {
		t.Errorf("expected 0 state callbacks after stop, got %d", got)
	}
}

func TestDebouncerTriggerAfterStopIsIgnored(t *testing.T) {
	var stateCalls int32

	d := NewBroadcastDebouncer(50*time.Millisecond,
		func() { atomic.AddInt32(&stateCalls, 1) },
		func() {},
	)

	d.Stop()
	d.Trigger("player")

	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt32(&stateCalls); got != 0 {
		t.Errorf("expected 0 state callbacks after stop+trigger, got %d", got)
	}
}
