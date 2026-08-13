package socketio

import (
	"testing"
	"time"
)

// playingState builds a steady "playing" state map differing only in seek.
func playingState(seekMs int) map[string]interface{} {
	return map[string]interface{}{
		"status":   "play",
		"position": 0,
		"title":    "Symphonic Dances, Op. 45: No. 1",
		"artist":   "Eiji Oue & The Minnesota Orchestra",
		"album":    "Symphonic Dances",
		"volume":   50,
		"duration": 715,
		"random":   false,
		"repeat":   false,
		"seek":     seekMs,
	}
}

// fixedClockServer returns a Server whose clock is driven by the returned
// pointer, so tests can advance time without sleeping.
func fixedClockServer(t *testing.T) (*Server, *time.Time) {
	t.Helper()
	now := time.Date(2026, 8, 14, 5, 0, 0, 0, time.UTC)
	s := &Server{}
	s.nowFn = func() time.Time { return now }
	return s, &now
}

func TestIsStateSame_SeekAdvancingWithWallClock_ReturnsTrue(t *testing.T) {
	// Steady playback: MPD's position advances in step with real time, which
	// is exactly what clients dead-reckon. No broadcast needed — this is the
	// debounce behaviour the seek exclusion was introduced for.
	s, now := fixedClockServer(t)
	s.saveLastState(playingState(60_000))

	*now = now.Add(1500 * time.Millisecond)

	if !s.isStateSame(playingState(61_500)) {
		t.Error("seek advancing in step with wall clock should not force a broadcast")
	}
}

func TestIsStateSame_TrackRestarted_ReturnsFalse(t *testing.T) {
	// The reported bug: restarting the current track rewinds MPD to 0 while
	// status/title/position/duration all stay identical. Clients keep counting
	// from their old position forever unless this forces a broadcast.
	s, now := fixedClockServer(t)
	s.saveLastState(playingState(242_000))

	*now = now.Add(500 * time.Millisecond)

	if s.isStateSame(playingState(0)) {
		t.Error("restarting the current track must force a state broadcast")
	}
}

func TestIsStateSame_SeekJumpedForward_ReturnsFalse(t *testing.T) {
	// Scrubbing forward from another client (iPhone) — same track, same
	// status, so only the seek discontinuity reveals it.
	s, now := fixedClockServer(t)
	s.saveLastState(playingState(60_000))

	*now = now.Add(500 * time.Millisecond)

	if s.isStateSame(playingState(400_000)) {
		t.Error("a forward seek jump must force a state broadcast")
	}
}

func TestIsStateSame_SeekDriftWithinTolerance_ReturnsTrue(t *testing.T) {
	// Sub-tolerance jitter between MPD's clock and the watcher tick must not
	// generate broadcast churn.
	s, now := fixedClockServer(t)
	s.saveLastState(playingState(60_000))

	*now = now.Add(1 * time.Second)

	if !s.isStateSame(playingState(61_900)) {
		t.Error("900ms of jitter is within tolerance and should not broadcast")
	}
}

func TestIsStateSame_PlayingAndStale_ReturnsFalse(t *testing.T) {
	// Even with a perfectly predictable seek, clients get a fresh anchor at
	// least every seekMaxStaleInterval so client-side timer throttling cannot
	// accumulate unbounded drift.
	s, now := fixedClockServer(t)
	s.saveLastState(playingState(60_000))

	*now = now.Add(seekMaxStaleInterval + time.Second)

	if s.isStateSame(playingState(60_000 + int((seekMaxStaleInterval + time.Second).Milliseconds()))) {
		t.Error("a playing client must be re-anchored at least every seekMaxStaleInterval")
	}
}

func TestIsStateSame_PausedAndStale_ReturnsTrue(t *testing.T) {
	// Paused clients are not dead-reckoning, so there is nothing to correct;
	// the staleness heartbeat must not fire and spam broadcasts while idle.
	s, now := fixedClockServer(t)
	paused := playingState(60_000)
	paused["status"] = "pause"
	s.saveLastState(paused)

	*now = now.Add(10 * seekMaxStaleInterval)

	if !s.isStateSame(paused) {
		t.Error("a paused, unchanged state must not broadcast on the staleness heartbeat")
	}
}

func TestIsStateSame_PausedThenSeeked_ReturnsFalse(t *testing.T) {
	// Scrubbing while paused moves seek with no wall-clock advance at all.
	s, now := fixedClockServer(t)
	paused := playingState(60_000)
	paused["status"] = "pause"
	s.saveLastState(paused)

	*now = now.Add(200 * time.Millisecond)

	scrubbed := playingState(300_000)
	scrubbed["status"] = "pause"

	if s.isStateSame(scrubbed) {
		t.Error("seeking while paused must force a state broadcast")
	}
}

func TestSeekMillis(t *testing.T) {
	// GetState writes an int, but the map is untyped and JSON round-trips
	// produce float64 — both must be understood.
	tests := []struct {
		name  string
		value interface{}
		want  int
		ok    bool
	}{
		{"int", 1234, 1234, true},
		{"int64", int64(1234), 1234, true},
		{"float64", float64(1234), 1234, true},
		{"missing", nil, 0, false},
		{"wrong type", "1234", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := map[string]interface{}{}
			if tt.value != nil {
				state["seek"] = tt.value
			}
			got, ok := seekMillis(state)
			if ok != tt.ok || got != tt.want {
				t.Errorf("seekMillis() = (%d, %v), want (%d, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}
