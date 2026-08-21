package socketio

import (
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/lcd"
)

func TestParseLcdSetView(t *testing.T) {
	tests := []struct {
		name     string
		args     []any
		wantView lcd.View
		wantWake bool
		wantErr  bool
	}{
		{
			name:     "bare string from the kiosk reporting its own navigation",
			args:     []any{"vu-meter"},
			wantView: lcd.ViewVuMeter,
			wantWake: false,
		},
		{
			name:     "object without wake defaults to not waking",
			args:     []any{map[string]any{"view": "player"}},
			wantView: lcd.ViewPlayer,
			wantWake: false,
		},
		{
			name:     "object with wake true",
			args:     []any{map[string]any{"view": "vu-meter", "wake": true}},
			wantView: lcd.ViewVuMeter,
			wantWake: true,
		},
		{
			name:     "object with wake false",
			args:     []any{map[string]any{"view": "library", "wake": false}},
			wantView: lcd.ViewLibrary,
			wantWake: false,
		},
		{
			name:     "extra args after the payload are ignored",
			args:     []any{"queue", "ack-callback"},
			wantView: lcd.ViewQueue,
			wantWake: false,
		},
		{name: "no payload", args: []any{}, wantErr: true},
		{name: "unknown view rejected", args: []any{"spectrum"}, wantErr: true},
		{name: "unknown view in object rejected", args: []any{map[string]any{"view": "spectrum"}}, wantErr: true},
		{name: "object missing view field", args: []any{map[string]any{"wake": true}}, wantErr: true},
		{name: "view field wrong type", args: []any{map[string]any{"view": 7}}, wantErr: true},
		{name: "payload wrong type", args: []any{42}, wantErr: true},
		{name: "nil payload", args: []any{nil}, wantErr: true},
		{
			// A non-bool wake must not be coerced into a wake — silently
			// powering the panel on from a malformed payload is worse than
			// ignoring the field.
			name:     "non-bool wake is ignored, not coerced",
			args:     []any{map[string]any{"view": "vu-meter", "wake": "yes"}},
			wantView: lcd.ViewVuMeter,
			wantWake: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view, wake, err := parseLcdSetView(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLcdSetView() err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if view != tt.wantView {
				t.Errorf("view = %q, want %q", view, tt.wantView)
			}
			if wake != tt.wantWake {
				t.Errorf("wake = %v, want %v", wake, tt.wantWake)
			}
		})
	}
}
