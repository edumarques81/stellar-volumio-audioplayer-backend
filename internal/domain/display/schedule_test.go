package display

import (
	"testing"
	"time"
)

func at(hh, mm int) time.Time { return atOn(18, hh, mm) }

// atOn pins a local time on a given October 2026 day, so tests that cross
// midnight can say so instead of silently going backwards.
func atOn(day, hh, mm int) time.Time {
	return time.Date(2026, 10, day, hh, mm, 0, 0, time.Local)
}

func TestInNightWindow(t *testing.T) {
	tests := []struct {
		name            string
		now, start, end int
		want            bool
	}{
		{"inside a wrapping window, before midnight", 23 * 60, 22 * 60, 5 * 60, true},
		{"inside a wrapping window, after midnight", 2 * 60, 22 * 60, 5 * 60, true},
		{"at the start boundary is inside", 22 * 60, 22 * 60, 5 * 60, true},
		{"at the end boundary is outside", 5 * 60, 22 * 60, 5 * 60, false},
		{"mid-afternoon is outside", 15 * 60, 22 * 60, 5 * 60, false},
		{"non-wrapping window", 13 * 60, 12 * 60, 14 * 60, true},
		{"degenerate window is never, not always", 13 * 60, 12 * 60, 12 * 60, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inNightWindow(tt.now, tt.start, tt.end); got != tt.want {
				t.Fatalf("inNightWindow(%d,%d,%d) = %v, want %v", tt.now, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestShouldAutoOff(t *testing.T) {
	settings := DefaultSettings() // off at 22:00 after 60 idle, restore 05:00

	tests := []struct {
		name string
		c    Conditions
		s    Settings
		want bool
	}{
		{
			name: "late evening, idle long enough",
			c:    Conditions{Now: at(23, 0), Mode: ModeStandby, LastActivity: at(21, 30)},
			s:    settings,
			want: true,
		},
		{
			name: "after midnight is still the same night",
			c:    Conditions{Now: at(2, 0), Mode: ModeLive, LastActivity: at(0, 30)},
			s:    settings,
			want: true,
		},
		{
			name: "before the window, however idle",
			c:    Conditions{Now: at(15, 0), Mode: ModeLive, LastActivity: at(8, 0)},
			s:    settings,
			want: false,
		},
		{
			name: "in the window but recently touched",
			c:    Conditions{Now: at(23, 0), Mode: ModeLive, LastActivity: at(22, 55)},
			s:    settings,
			want: false,
		},
		{
			name: "playing music never blacks the panel",
			c:    Conditions{Now: at(23, 0), Mode: ModeLive, Playing: true, LastActivity: at(18, 0)},
			s:    settings,
			want: false,
		},
		{
			name: "already off",
			c:    Conditions{Now: at(23, 0), Mode: ModeOff, LastActivity: at(18, 0)},
			s:    settings,
			want: false,
		},
		{
			name: "schedule disabled",
			c:    Conditions{Now: at(23, 0), Mode: ModeLive, LastActivity: at(18, 0)},
			s:    Settings{AutoOffEnabled: false, AutoOffAt: "22:00", AutoOffIdleMinutes: 60, AutoRestoreAt: "05:00"},
			want: false,
		},
		{
			name: "exactly at the idle boundary counts as idle",
			c:    Conditions{Now: at(23, 0), Mode: ModeLive, LastActivity: at(22, 0)},
			s:    settings,
			want: true,
		},
		{
			name: "unparsable off time cannot fire",
			c:    Conditions{Now: at(23, 0), Mode: ModeLive, LastActivity: at(18, 0)},
			s:    Settings{AutoOffEnabled: true, AutoOffAt: "later", AutoOffIdleMinutes: 60, AutoRestoreAt: "05:00"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldAutoOff(tt.c, tt.s); got != tt.want {
				t.Fatalf("ShouldAutoOff() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldAutoRestore(t *testing.T) {
	settings := DefaultSettings()

	tests := []struct {
		name string
		c    Conditions
		s    Settings
		want bool
	}{
		{
			name: "at the restore time with the panel off",
			c:    Conditions{Now: at(5, 0), Mode: ModeOff},
			s:    settings,
			want: true,
		},
		{
			name: "inside the grace window, so a slow boot still restores",
			c:    Conditions{Now: at(5, 20), Mode: ModeOff},
			s:    settings,
			want: true,
		},
		{
			name: "past the grace window",
			c:    Conditions{Now: at(6, 0), Mode: ModeOff},
			s:    settings,
			want: false,
		},
		{
			name: "before the restore time",
			c:    Conditions{Now: at(4, 59), Mode: ModeOff},
			s:    settings,
			want: false,
		},
		{
			name: "panel is not off",
			c:    Conditions{Now: at(5, 0), Mode: ModeStandby},
			s:    settings,
			want: false,
		},
		{
			name: "already restored today, so a manual off is not undone",
			c:    Conditions{Now: at(5, 10), Mode: ModeOff, AutoRestoredOn: "2026-10-18"},
			s:    settings,
			want: false,
		},
		{
			name: "restored yesterday",
			c:    Conditions{Now: at(5, 10), Mode: ModeOff, AutoRestoredOn: "2026-10-17"},
			s:    settings,
			want: true,
		},
		{
			name: "schedule disabled",
			c:    Conditions{Now: at(5, 0), Mode: ModeOff},
			s:    Settings{AutoRestoreEnabled: false, AutoRestoreAt: "05:00"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldAutoRestore(tt.c, tt.s); got != tt.want {
				t.Fatalf("ShouldAutoRestore() = %v, want %v", got, tt.want)
			}
		})
	}
}
