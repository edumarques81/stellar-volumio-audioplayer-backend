package display

import "time"

// Conditions is everything the two schedule rules are allowed to look at.
// Keeping them pure is deliberate: the rules are the part that is hard to
// observe in production (they fire at 22:00 and 05:00, on a headless box in
// a listening room) and easy to get wrong around midnight.
type Conditions struct {
	Now time.Time
	// Mode the panel is in right now.
	Mode Mode
	// Playing is MPD's (or AirPlay's) truth, not a guess from the UI.
	Playing bool
	// LastActivity is the later of: the last touch reported by the kiosk,
	// the last mode change, and the last moment playback was running.
	LastActivity time.Time
	// AutoRestoredOn is the date an auto-restore last fired, "2006-01-02",
	// so a manual off at 05:05 is not immediately undone by the schedule.
	AutoRestoredOn string
}

// minutesOfDay is the local time of day in minutes since midnight.
func minutesOfDay(t time.Time) int { return t.Hour()*60 + t.Minute() }

// inNightWindow reports whether now falls in [start, end) with wraparound,
// which is the only shape that matters here: the window runs from the
// auto-off time in the evening to the auto-restore time next morning.
//
// A degenerate window (start == end) is treated as "never", not "always" --
// a schedule that cannot be left would turn the panel off permanently.
func inNightWindow(now, start, end int) bool {
	if start == end {
		return false
	}
	if start < end {
		return now >= start && now < end
	}
	return now >= start || now < end
}

// ShouldAutoOff reports whether the panel should turn itself fully off.
//
// All four conditions must hold: the schedule is enabled, the panel is not
// already off, nothing is playing, and nothing has happened for the
// configured idle interval -- inside the night window. Playback is checked
// separately from LastActivity because a long album legitimately produces no
// touches at all.
func ShouldAutoOff(c Conditions, s Settings) bool {
	if !s.AutoOffEnabled || c.Mode == ModeOff || c.Playing {
		return false
	}

	start, ok := parseClock(s.AutoOffAt)
	if !ok {
		return false
	}
	end, ok := parseClock(s.AutoRestoreAt)
	if !ok {
		// With no restore time there is no window end; treat the schedule as
		// running until midnight rather than inventing one.
		end = 0
	}
	if !inNightWindow(minutesOfDay(c.Now), start, end) {
		return false
	}

	idle := time.Duration(s.AutoOffIdleMinutes) * time.Minute
	return !c.LastActivity.After(c.Now.Add(-idle))
}

// ShouldAutoRestore reports whether a panel that is off should come back.
//
// It fires once per calendar day, within a grace window after the configured
// time, so a backend that was asleep or restarting at exactly 05:00 still
// restores the screen rather than leaving it dark until someone notices.
func ShouldAutoRestore(c Conditions, s Settings) bool {
	if !s.AutoRestoreEnabled || c.Mode != ModeOff {
		return false
	}
	at, ok := parseClock(s.AutoRestoreAt)
	if !ok {
		return false
	}
	if c.AutoRestoredOn == c.Now.Format("2006-01-02") {
		return false
	}

	now := minutesOfDay(c.Now)
	return now >= at && now < at+restoreGraceMinutes
}

// restoreGraceMinutes is how long after the configured time an auto-restore
// may still fire. Wide enough to survive a slow boot, narrow enough that it
// cannot undo a deliberate off made later in the morning.
const restoreGraceMinutes = 30
