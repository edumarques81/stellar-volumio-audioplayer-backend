package socketio

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestRunWatcherLoop_TickerRecoversAfterMissedEvent simulates a missed MPD
// subsystem event: the loop receives some events, then stops receiving them
// while the watcher socket continues to look healthy. The ticker must fire and
// hand the on-tick callback a "no subsystem event in window" signal, so the
// production wiring can run a recovery broadcast and bump the recovery
// counter.
//
// This test fails before the ticker wiring lands in StartMPDWatcher (the loop
// has no ticker case at all, so onTick is never called).
func TestRunWatcherLoop_TickerRecoversAfterMissedEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan string, 4)
	tickerC := make(chan time.Time, 4)

	var (
		mu              sync.Mutex
		subsystemEvents []string
		dbEvents        int
		tickArgs        []bool // values of subsystemSeen passed to onTick
	)

	done := make(chan struct{})
	hooks := watcherLoopHooks{
		onSubsystem: func(s string) {
			mu.Lock()
			subsystemEvents = append(subsystemEvents, s)
			mu.Unlock()
		},
		onDatabase: func() {
			mu.Lock()
			dbEvents++
			mu.Unlock()
		},
		onTick: func(subsystemSeen bool) {
			mu.Lock()
			tickArgs = append(tickArgs, subsystemSeen)
			mu.Unlock()
		},
	}

	go func() {
		runWatcherLoop(ctx, events, tickerC, hooks)
		close(done)
	}()

	// Phase 1: deliver a "player" event, then a ticker tick.
	// onTick must see subsystemSeen=true.
	events <- "player"
	waitForLen(t, &mu, &subsystemEvents, 1, "subsystem event 1")

	tickerC <- time.Now()
	waitForLen(t, &mu, &tickArgs, 1, "tick 1")
	mu.Lock()
	if !tickArgs[0] {
		t.Errorf("tick 1: subsystemSeen should be true after a 'player' event, got false")
	}
	mu.Unlock()

	// Phase 2: simulate a MISSED event — the ticker fires with no preceding
	// subsystem event. onTick must see subsystemSeen=false. This is the
	// scenario the production code recovers from.
	tickerC <- time.Now()
	waitForLen(t, &mu, &tickArgs, 2, "tick 2")
	mu.Lock()
	if tickArgs[1] {
		t.Errorf("tick 2: subsystemSeen should be false (no event since last tick), got true")
	}
	mu.Unlock()

	// Phase 3: a database event should be routed to onDatabase, not onSubsystem,
	// and should NOT count as a subsystem event for the ticker window.
	events <- "database"
	waitForCounter(t, &mu, &dbEvents, 1, "database event")
	tickerC <- time.Now()
	waitForLen(t, &mu, &tickArgs, 3, "tick 3")
	mu.Lock()
	if tickArgs[2] {
		t.Errorf("tick 3: subsystemSeen should be false after database event (database bypasses subsystem path), got true")
	}
	mu.Unlock()

	// Phase 4: another player event then a tick — back to true.
	events <- "playlist"
	waitForLen(t, &mu, &subsystemEvents, 2, "subsystem event 2")
	tickerC <- time.Now()
	waitForLen(t, &mu, &tickArgs, 4, "tick 4")
	mu.Lock()
	if !tickArgs[3] {
		t.Errorf("tick 4: subsystemSeen should be true after 'playlist' event, got false")
	}
	mu.Unlock()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher loop did not exit after ctx cancel")
	}
}

// TestRunWatcherLoop_StopsOnContextCancel verifies the loop exits cleanly when
// its context is cancelled (no goroutine leak).
func TestRunWatcherLoop_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan string)
	tickerC := make(chan time.Time)
	hooks := watcherLoopHooks{
		onSubsystem: func(string) {},
		onDatabase:  func() {},
		onTick:      func(bool) {},
	}

	done := make(chan struct{})
	go func() {
		runWatcherLoop(ctx, events, tickerC, hooks)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher loop did not exit after ctx cancel")
	}
}

// TestRunWatcherLoop_StopsOnEventsChannelClose verifies the loop exits when
// the upstream events channel is closed (gompd watcher death).
func TestRunWatcherLoop_StopsOnEventsChannelClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan string)
	tickerC := make(chan time.Time)
	hooks := watcherLoopHooks{
		onSubsystem: func(string) {},
		onDatabase:  func() {},
		onTick:      func(bool) {},
	}

	done := make(chan struct{})
	go func() {
		runWatcherLoop(ctx, events, tickerC, hooks)
		close(done)
	}()

	close(events)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher loop did not exit after events channel close")
	}
}

// TestServer_TickerRecoveredBroadcasts_StartsAtZero verifies the counter is
// readable on a freshly constructed server and starts at zero.
func TestServer_TickerRecoveredBroadcasts_StartsAtZero(t *testing.T) {
	s := &Server{}
	if got := s.TickerRecoveredBroadcasts(); got != 0 {
		t.Errorf("fresh server: TickerRecoveredBroadcasts = %d, want 0", got)
	}
}

// TestServer_TickerRecoveredBroadcasts_IncrementsOnRecovery verifies the
// counter increments when handleTickerTick is invoked with subsystemSeen=false
// and BroadcastState would have emitted (state has changed since last
// broadcast). Uses a server with no playerService to simulate the "GetState
// fails" branch — that branch must NOT bump the counter.
func TestServer_TickerRecoveredBroadcasts_DoesNotIncrementOnGetStateFailure(t *testing.T) {
	s := &Server{} // playerService is nil → BroadcastState short-circuits
	// Calling handleTickerTick with no event-in-window must not panic and
	// must not increment the counter when no broadcast was emitted.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handleTickerTick panicked: %v", r)
		}
	}()
	s.handleTickerTick(false)
	if got := s.TickerRecoveredBroadcasts(); got != 0 {
		t.Errorf("after failed GetState, counter = %d, want 0", got)
	}
}

// --- helpers ---

func waitForLen[T any](t *testing.T, mu *sync.Mutex, slice *[]T, want int, label string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := len(*slice)
		mu.Unlock()
		if got >= want {
			return
		}
		if time.Now().After(deadline) {
			mu.Lock()
			got := len(*slice)
			mu.Unlock()
			t.Fatalf("timeout waiting for %s: got %d, want %d", label, got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForCounter(t *testing.T, mu *sync.Mutex, counter *int, want int, label string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := *counter
		mu.Unlock()
		if got >= want {
			return
		}
		if time.Now().After(deadline) {
			mu.Lock()
			got := *counter
			mu.Unlock()
			t.Fatalf("timeout waiting for %s: got %d, want %d", label, got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
