package player_test

import (
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
)

func TestNewState(t *testing.T) {
	state := player.NewState()

	if state.Status != player.StatusStop {
		t.Errorf("expected status %q, got %q", player.StatusStop, state.Status)
	}

	if state.Volume != 100 {
		t.Errorf("expected volume 100, got %d", state.Volume)
	}

	if state.Random != false {
		t.Error("expected random to be false")
	}

	if state.Repeat != false {
		t.Error("expected repeat to be false")
	}
}

func TestStatePlay(t *testing.T) {
	state := player.NewState()
	state.Play()

	if state.Status != player.StatusPlay {
		t.Errorf("expected status %q, got %q", player.StatusPlay, state.Status)
	}
}

func TestStatePause(t *testing.T) {
	state := player.NewState()
	state.Play()
	state.Pause()

	if state.Status != player.StatusPause {
		t.Errorf("expected status %q, got %q", player.StatusPause, state.Status)
	}
}

func TestStateStop(t *testing.T) {
	state := player.NewState()
	state.Play()
	state.Stop()

	if state.Status != player.StatusStop {
		t.Errorf("expected status %q, got %q", player.StatusStop, state.Status)
	}

	if state.Seek != 0 {
		t.Errorf("expected seek to be 0, got %d", state.Seek)
	}
}

func TestStateSetVolume(t *testing.T) {
	tests := []struct {
		name     string
		volume   int
		expected int
	}{
		{"normal volume", 50, 50},
		{"max volume", 100, 100},
		{"min volume", 0, 0},
		{"over max", 150, 100},
		{"under min", -10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := player.NewState()
			state.SetVolume(tt.volume)

			if state.Volume != tt.expected {
				t.Errorf("expected volume %d, got %d", tt.expected, state.Volume)
			}
		})
	}
}

func TestStateToggleMute(t *testing.T) {
	state := player.NewState()
	state.SetVolume(50)

	// Mute
	state.ToggleMute()
	if !state.Mute {
		t.Error("expected mute to be true")
	}

	// Unmute
	state.ToggleMute()
	if state.Mute {
		t.Error("expected mute to be false")
	}
}

func TestStateToggleRandom(t *testing.T) {
	state := player.NewState()

	state.ToggleRandom()
	if !state.Random {
		t.Error("expected random to be true")
	}

	state.ToggleRandom()
	if state.Random {
		t.Error("expected random to be false")
	}
}

func TestStateSetRepeat(t *testing.T) {
	state := player.NewState()

	// No repeat -> repeat all
	state.SetRepeat(true, false)
	if !state.Repeat || state.RepeatSingle {
		t.Error("expected repeat all mode")
	}

	// Repeat all -> repeat single
	state.SetRepeat(true, true)
	if !state.Repeat || !state.RepeatSingle {
		t.Error("expected repeat single mode")
	}

	// Repeat single -> no repeat
	state.SetRepeat(false, false)
	if state.Repeat || state.RepeatSingle {
		t.Error("expected no repeat mode")
	}
}

func TestStateUpdateTrack(t *testing.T) {
	state := player.NewState()

	track := player.TrackInfo{
		Title:      "Something Heavy",
		Artist:     "Jacob Collier",
		Album:      "The Light For Days",
		AlbumArt:   "/albumart/123",
		URI:        "file:///music/track.flac",
		Duration:   245,
		TrackType:  "flac",
		SampleRate: "96000",
		BitDepth:   "24",
		Service:    "mpd",
	}

	state.UpdateTrack(track)

	if state.Title != track.Title {
		t.Errorf("expected title %q, got %q", track.Title, state.Title)
	}
	if state.Artist != track.Artist {
		t.Errorf("expected artist %q, got %q", track.Artist, state.Artist)
	}
	if state.Duration != track.Duration {
		t.Errorf("expected duration %d, got %d", track.Duration, state.Duration)
	}
	if state.SampleRate != track.SampleRate {
		t.Errorf("expected sample rate %q, got %q", track.SampleRate, state.SampleRate)
	}
}

func TestStateToJSON(t *testing.T) {
	state := player.NewState()
	state.Play()
	state.UpdateTrack(player.TrackInfo{
		Title:  "Test Track",
		Artist: "Test Artist",
	})

	json := state.ToJSON()

	if json["status"] != player.StatusPlay {
		t.Errorf("expected status %q in JSON, got %v", player.StatusPlay, json["status"])
	}
	if json["title"] != "Test Track" {
		t.Errorf("expected title %q in JSON, got %v", "Test Track", json["title"])
	}
}
