package lcd

import (
	"sync"
	"testing"
)

func TestParseView(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    View
		wantErr bool
	}{
		{"player", "player", ViewPlayer, false},
		{"library", "library", ViewLibrary, false},
		{"queue", "queue", ViewQueue, false},
		{"settings", "settings", ViewSettings, false},
		{"vu meter", "vu-meter", ViewVuMeter, false},
		{"unknown view rejected", "spectrum", "", true},
		{"empty rejected", "", "", true},
		{"case sensitive", "Player", "", true},
		{"underscore variant rejected", "vu_meter", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseView(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseView(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseView(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestViewStateDefaultsToPlayer(t *testing.T) {
	v := NewViewState()
	got := v.Status()
	if got.View != ViewPlayer {
		t.Errorf("View = %q, want %q", got.View, ViewPlayer)
	}
	if got.PreviousView != ViewPlayer {
		t.Errorf("PreviousView = %q, want %q", got.PreviousView, ViewPlayer)
	}
}

func TestViewStateSet(t *testing.T) {
	tests := []struct {
		name         string
		sequence     []View
		wantView     View
		wantPrevious View
	}{
		{
			name:         "single change remembers the view it left",
			sequence:     []View{ViewVuMeter},
			wantView:     ViewVuMeter,
			wantPrevious: ViewPlayer,
		},
		{
			name:         "flipping back restores the remembered view",
			sequence:     []View{ViewVuMeter, ViewPlayer},
			wantView:     ViewPlayer,
			wantPrevious: ViewVuMeter,
		},
		{
			name:         "previous tracks the immediately preceding view only",
			sequence:     []View{ViewLibrary, ViewQueue, ViewVuMeter},
			wantView:     ViewVuMeter,
			wantPrevious: ViewQueue,
		},
		{
			name:         "vu meter reached from library returns to library",
			sequence:     []View{ViewLibrary, ViewVuMeter},
			wantView:     ViewVuMeter,
			wantPrevious: ViewLibrary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewViewState()
			for _, view := range tt.sequence {
				v.Set(view)
			}
			got := v.Status()
			if got.View != tt.wantView {
				t.Errorf("View = %q, want %q", got.View, tt.wantView)
			}
			if got.PreviousView != tt.wantPrevious {
				t.Errorf("PreviousView = %q, want %q", got.PreviousView, tt.wantPrevious)
			}
		})
	}
}

// Set reports whether anything actually moved, so the transport layer can
// skip a broadcast when the kiosk echoes back a view we already hold. Without
// this the kiosk's own report would bounce back and re-trigger its listener.
func TestViewStateSetReportsChange(t *testing.T) {
	v := NewViewState()

	if _, changed := v.Set(ViewVuMeter); !changed {
		t.Error("Set(vu-meter) on a player-state reported no change")
	}
	if _, changed := v.Set(ViewVuMeter); changed {
		t.Error("Set(vu-meter) twice reported a change the second time")
	}
	if _, changed := v.Set(ViewPlayer); !changed {
		t.Error("Set(player) after vu-meter reported no change")
	}
}

// A redundant Set must not corrupt the remembered view — otherwise a duplicate
// report from the kiosk would make "back" mean "stay put".
func TestViewStateRedundantSetPreservesPrevious(t *testing.T) {
	v := NewViewState()
	v.Set(ViewLibrary)
	v.Set(ViewVuMeter)
	v.Set(ViewVuMeter)

	got := v.Status()
	if got.PreviousView != ViewLibrary {
		t.Errorf("PreviousView = %q, want %q", got.PreviousView, ViewLibrary)
	}
}

func TestViewStateConcurrentAccess(t *testing.T) {
	v := NewViewState()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); v.Set(ViewVuMeter) }()
		go func() { defer wg.Done(); _ = v.Status() }()
	}
	wg.Wait()
}
