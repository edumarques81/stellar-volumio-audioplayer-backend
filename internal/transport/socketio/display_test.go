package socketio

import (
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/display"
)

func TestParseLcdSetMode(t *testing.T) {
	tests := []struct {
		name    string
		args    []any
		want    display.Mode
		wantErr bool
	}{
		{name: "bare string", args: []any{"standby"}, want: display.ModeStandby},
		{name: "object", args: []any{map[string]any{"mode": "off"}}, want: display.ModeOff},
		{name: "object with extra fields", args: []any{map[string]any{"mode": "live", "why": "touch"}}, want: display.ModeLive},
		{name: "no payload", args: nil, wantErr: true},
		{name: "unknown mode", args: []any{"dim"}, wantErr: true},
		{name: "object without mode", args: []any{map[string]any{"state": "off"}}, wantErr: true},
		{name: "mode of the wrong type", args: []any{map[string]any{"mode": 3}}, wantErr: true},
		{name: "payload of the wrong type", args: []any{42}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLcdSetMode(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseLcdSetMode(%v) = %q, want error", tt.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLcdSetMode(%v) returned %v", tt.args, err)
			}
			if got != tt.want {
				t.Fatalf("parseLcdSetMode(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseLcdSettingsPatchesOntoCurrent(t *testing.T) {
	current := display.Settings{
		AutoOffEnabled:     true,
		AutoOffAt:          "22:00",
		AutoOffIdleMinutes: 60,
		AutoRestoreEnabled: true,
		AutoRestoreAt:      "05:00",
	}

	tests := []struct {
		name string
		args []any
		want display.Settings
	}{
		{
			name: "a single toggle leaves everything else alone",
			args: []any{map[string]any{"autoOffEnabled": false}},
			want: display.Settings{AutoOffEnabled: false, AutoOffAt: "22:00", AutoOffIdleMinutes: 60, AutoRestoreEnabled: true, AutoRestoreAt: "05:00"},
		},
		{
			name: "numbers arrive from JSON as float64",
			args: []any{map[string]any{"autoOffIdleMinutes": float64(90)}},
			want: display.Settings{AutoOffEnabled: true, AutoOffAt: "22:00", AutoOffIdleMinutes: 90, AutoRestoreEnabled: true, AutoRestoreAt: "05:00"},
		},
		{
			name: "times",
			args: []any{map[string]any{"autoOffAt": "23:15", "autoRestoreAt": "06:30"}},
			want: display.Settings{AutoOffEnabled: true, AutoOffAt: "23:15", AutoOffIdleMinutes: 60, AutoRestoreEnabled: true, AutoRestoreAt: "06:30"},
		},
		{
			name: "fields of the wrong type are ignored, not applied as zero",
			args: []any{map[string]any{"autoOffAt": 2200, "autoRestoreEnabled": "yes"}},
			want: current,
		},
		{
			name: "an empty object is a no-op",
			args: []any{map[string]any{}},
			want: current,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLcdSettings(tt.args, current)
			if err != nil {
				t.Fatalf("parseLcdSettings() returned %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseLcdSettings() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseLcdSettingsRejectsBadPayloads(t *testing.T) {
	current := display.DefaultSettings()
	for _, args := range [][]any{nil, {"off"}, {42}} {
		got, err := parseLcdSettings(args, current)
		if err == nil {
			t.Fatalf("parseLcdSettings(%v) returned no error", args)
		}
		// The current schedule must survive a rejected payload.
		if got != current {
			t.Fatalf("parseLcdSettings(%v) = %+v, want the unchanged current settings", args, got)
		}
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		name   string
		in     any
		want   int
		wantOk bool
	}{
		{name: "float64 from JSON", in: float64(45), want: 45, wantOk: true},
		{name: "int", in: 45, want: 45, wantOk: true},
		{name: "int64", in: int64(45), want: 45, wantOk: true},
		{name: "string is not a number here", in: "45"},
		{name: "nil (absent key)", in: nil},
		{name: "bool", in: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toInt(tt.in)
			if ok != tt.wantOk {
				t.Fatalf("toInt(%v) ok = %v, want %v", tt.in, ok, tt.wantOk)
			}
			if ok && got != tt.want {
				t.Fatalf("toInt(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
