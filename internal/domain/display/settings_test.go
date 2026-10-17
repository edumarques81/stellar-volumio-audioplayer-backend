package display

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseClock(t *testing.T) {
	tests := []struct {
		in     string
		want   int
		wantOk bool
	}{
		{in: "00:00", want: 0, wantOk: true},
		{in: "22:00", want: 1320, wantOk: true},
		{in: "05:30", want: 330, wantOk: true},
		{in: " 05:30 ", want: 330, wantOk: true},
		{in: "5:30", want: 330, wantOk: true},
		{in: "24:00"},
		{in: "22:60"},
		{in: "22"},
		{in: "22:00:00"},
		{in: "ten"},
		{in: ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := parseClock(tt.in)
			if ok != tt.wantOk {
				t.Fatalf("parseClock(%q) ok = %v, want %v", tt.in, ok, tt.wantOk)
			}
			if ok && got != tt.want {
				t.Fatalf("parseClock(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestSettingsNormalize(t *testing.T) {
	d := DefaultSettings()

	tests := []struct {
		name string
		in   Settings
		want Settings
	}{
		{
			name: "valid settings pass through",
			in:   Settings{AutoOffEnabled: true, AutoOffAt: "23:30", AutoOffIdleMinutes: 90, AutoRestoreEnabled: true, AutoRestoreAt: "06:15"},
			want: Settings{AutoOffEnabled: true, AutoOffAt: "23:30", AutoOffIdleMinutes: 90, AutoRestoreEnabled: true, AutoRestoreAt: "06:15"},
		},
		{
			name: "a bad time falls back to the default for that field only",
			in:   Settings{AutoOffEnabled: true, AutoOffAt: "bedtime", AutoOffIdleMinutes: 90, AutoRestoreAt: "06:15"},
			want: Settings{AutoOffEnabled: true, AutoOffAt: d.AutoOffAt, AutoOffIdleMinutes: 90, AutoRestoreAt: "06:15"},
		},
		{
			name: "zero idle minutes is repaired, not honoured",
			in:   Settings{AutoOffAt: "22:00", AutoOffIdleMinutes: 0, AutoRestoreAt: "05:00"},
			want: Settings{AutoOffAt: "22:00", AutoOffIdleMinutes: d.AutoOffIdleMinutes, AutoRestoreAt: "05:00"},
		},
		{
			name: "absurd idle minutes are repaired",
			in:   Settings{AutoOffAt: "22:00", AutoOffIdleMinutes: 99999, AutoRestoreAt: "05:00"},
			want: Settings{AutoOffAt: "22:00", AutoOffIdleMinutes: d.AutoOffIdleMinutes, AutoRestoreAt: "05:00"},
		},
		{
			name: "an empty struct becomes a usable schedule",
			in:   Settings{},
			want: Settings{AutoOffAt: d.AutoOffAt, AutoOffIdleMinutes: d.AutoOffIdleMinutes, AutoRestoreAt: d.AutoRestoreAt},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Normalize(); got != tt.want {
				t.Fatalf("Normalize() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadSettingsMissingFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "display.json")
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings() returned %v", err)
	}
	if got != DefaultSettings() {
		t.Fatalf("LoadSettings() = %+v, want defaults", got)
	}
}

func TestLoadSettingsCorruptFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "display.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Reporting the error matters: silently rewriting a user's schedule
	// because it failed to parse would lose it for good.
	if _, err := LoadSettings(path); err == nil {
		t.Fatal("LoadSettings() on a corrupt file returned no error")
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "display.json")
	want := Settings{
		AutoOffEnabled:     true,
		AutoOffAt:          "21:45",
		AutoOffIdleMinutes: 30,
		AutoRestoreEnabled: false,
		AutoRestoreAt:      "07:00",
	}
	if err := SaveSettings(path, want); err != nil {
		t.Fatalf("SaveSettings() returned %v", err)
	}

	got, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings() returned %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}

	// The file is what a human may end up editing; keep the wire names.
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}
	for _, key := range []string{"autoOffEnabled", "autoOffAt", "autoOffIdleMinutes", "autoRestoreEnabled", "autoRestoreAt"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("saved file is missing %q: %s", key, data)
		}
	}
}
