package display

import "testing"

func TestParseMode(t *testing.T) {
	tests := []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		{in: "live", want: ModeLive},
		{in: "standby", want: ModeStandby},
		{in: "off", want: ModeOff},
		{in: "", wantErr: true},
		{in: "Off", wantErr: true},
		{in: "dim", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseMode(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMode(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMode(%q) returned %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestModeLit(t *testing.T) {
	tests := []struct {
		mode Mode
		want bool
	}{
		{ModeLive, true},
		{ModeStandby, true}, // standby is painted, not switched
		{ModeOff, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.Lit(); got != tt.want {
				t.Fatalf("%q.Lit() = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}
