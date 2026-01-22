package audio_test

import (
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/audio"
)

func TestNewController(t *testing.T) {
	t.Run("creates controller with bit-perfect enabled", func(t *testing.T) {
		ctrl := audio.NewController(true)
		status := ctrl.GetStatus()

		if status.Locked {
			t.Error("expected locked to be false initially")
		}
		if status.Format != nil {
			t.Error("expected format to be nil initially")
		}
	})

	t.Run("creates controller with bit-perfect disabled", func(t *testing.T) {
		ctrl := audio.NewController(false)
		status := ctrl.GetStatus()

		if status.Locked {
			t.Error("expected locked to be false initially")
		}
	})
}

func TestUpdateFromMPDStatus(t *testing.T) {
	tests := []struct {
		name           string
		mpdState       string
		audio          string
		bitPerfect     bool
		expectLocked   bool
		expectFormat   bool
		expectSR       int
		expectBD       int
		expectCh       int
		expectFmtType  string
		expectBP       bool
		expectChanged  bool
	}{
		{
			name:          "playing PCM 44.1kHz/16-bit",
			mpdState:      "play",
			audio:         "44100:16:2",
			bitPerfect:    true,
			expectLocked:  true,
			expectFormat:  true,
			expectSR:      44100,
			expectBD:      16,
			expectCh:      2,
			expectFmtType: "PCM",
			expectBP:      true,
			expectChanged: true,
		},
		{
			name:          "playing PCM 192kHz/24-bit",
			mpdState:      "play",
			audio:         "192000:24:2",
			bitPerfect:    true,
			expectLocked:  true,
			expectFormat:  true,
			expectSR:      192000,
			expectBD:      24,
			expectCh:      2,
			expectFmtType: "PCM",
			expectBP:      true,
			expectChanged: true,
		},
		{
			name:          "playing DSD64",
			mpdState:      "play",
			audio:         "2822400:1:2",
			bitPerfect:    true,
			expectLocked:  true,
			expectFormat:  true,
			expectSR:      2822400,
			expectBD:      1,
			expectCh:      2,
			expectFmtType: "DSD64",
			expectBP:      true,
			expectChanged: true,
		},
		{
			name:          "playing DSD128",
			mpdState:      "play",
			audio:         "5644800:1:2",
			bitPerfect:    true,
			expectLocked:  true,
			expectFormat:  true,
			expectSR:      5644800,
			expectBD:      1,
			expectCh:      2,
			expectFmtType: "DSD128",
			expectBP:      true,
			expectChanged: true,
		},
		{
			name:          "playing DSD256",
			mpdState:      "play",
			audio:         "11289600:1:2",
			bitPerfect:    true,
			expectLocked:  true,
			expectFormat:  true,
			expectSR:      11289600,
			expectBD:      1,
			expectCh:      2,
			expectFmtType: "DSD256",
			expectBP:      true,
			expectChanged: true,
		},
		{
			name:          "playing DSD512",
			mpdState:      "play",
			audio:         "22579200:1:2",
			bitPerfect:    true,
			expectLocked:  true,
			expectFormat:  true,
			expectSR:      22579200,
			expectBD:      1,
			expectCh:      2,
			expectFmtType: "DSD512",
			expectBP:      true,
			expectChanged: true,
		},
		{
			name:          "paused should not lock",
			mpdState:      "pause",
			audio:         "96000:24:2",
			bitPerfect:    true,
			expectLocked:  false,
			expectFormat:  true,
			expectSR:      96000,
			expectBD:      24,
			expectCh:      2,
			expectFmtType: "PCM",
			expectBP:      true,
			expectChanged: true,
		},
		{
			name:          "stopped should not lock",
			mpdState:      "stop",
			audio:         "",
			bitPerfect:    true,
			expectLocked:  false,
			expectFormat:  false,
			expectChanged: false, // No change from initial state (locked=false, format=nil)
		},
		{
			name:          "bit-perfect disabled",
			mpdState:      "play",
			audio:         "48000:24:2",
			bitPerfect:    false,
			expectLocked:  true,
			expectFormat:  true,
			expectSR:      48000,
			expectBD:      24,
			expectCh:      2,
			expectFmtType: "PCM",
			expectBP:      false,
			expectChanged: true,
		},
		{
			name:          "mono channel",
			mpdState:      "play",
			audio:         "44100:16:1",
			bitPerfect:    true,
			expectLocked:  true,
			expectFormat:  true,
			expectSR:      44100,
			expectBD:      16,
			expectCh:      1,
			expectFmtType: "PCM",
			expectBP:      true,
			expectChanged: true,
		},
		{
			name:          "invalid audio format",
			mpdState:      "play",
			audio:         "invalid",
			bitPerfect:    true,
			expectLocked:  true,
			expectFormat:  false,
			expectChanged: true,
		},
		{
			name:          "empty audio format while playing",
			mpdState:      "play",
			audio:         "",
			bitPerfect:    true,
			expectLocked:  true,
			expectFormat:  false,
			expectChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := audio.NewController(tt.bitPerfect)
			changed := ctrl.UpdateFromMPDStatus(tt.mpdState, tt.audio)

			status := ctrl.GetStatus()

			if status.Locked != tt.expectLocked {
				t.Errorf("expected locked=%v, got %v", tt.expectLocked, status.Locked)
			}

			if tt.expectFormat {
				if status.Format == nil {
					t.Fatal("expected format to be set, got nil")
				}
				if status.Format.SampleRate != tt.expectSR {
					t.Errorf("expected sampleRate=%d, got %d", tt.expectSR, status.Format.SampleRate)
				}
				if status.Format.BitDepth != tt.expectBD {
					t.Errorf("expected bitDepth=%d, got %d", tt.expectBD, status.Format.BitDepth)
				}
				if status.Format.Channels != tt.expectCh {
					t.Errorf("expected channels=%d, got %d", tt.expectCh, status.Format.Channels)
				}
				if status.Format.Format != tt.expectFmtType {
					t.Errorf("expected format=%q, got %q", tt.expectFmtType, status.Format.Format)
				}
				if status.Format.IsBitPerfect != tt.expectBP {
					t.Errorf("expected isBitPerfect=%v, got %v", tt.expectBP, status.Format.IsBitPerfect)
				}
			} else {
				if status.Format != nil {
					t.Errorf("expected format to be nil, got %+v", status.Format)
				}
			}

			if changed != tt.expectChanged {
				t.Errorf("expected changed=%v, got %v", tt.expectChanged, changed)
			}
		})
	}
}

func TestOnPlaybackStartStop(t *testing.T) {
	t.Run("OnPlaybackStart locks device", func(t *testing.T) {
		ctrl := audio.NewController(true)

		ctrl.OnPlaybackStart()
		status := ctrl.GetStatus()

		if !status.Locked {
			t.Error("expected locked to be true after OnPlaybackStart")
		}
	})

	t.Run("OnPlaybackStop unlocks device", func(t *testing.T) {
		ctrl := audio.NewController(true)

		ctrl.OnPlaybackStart()
		ctrl.OnPlaybackStop()
		status := ctrl.GetStatus()

		if status.Locked {
			t.Error("expected locked to be false after OnPlaybackStop")
		}
	})
}

func TestNoChangeDetection(t *testing.T) {
	ctrl := audio.NewController(true)

	// First update should change
	changed1 := ctrl.UpdateFromMPDStatus("play", "44100:16:2")
	if !changed1 {
		t.Error("expected first update to report changed")
	}

	// Same state should not change
	changed2 := ctrl.UpdateFromMPDStatus("play", "44100:16:2")
	if changed2 {
		t.Error("expected same state to not report changed")
	}

	// Different state should change
	changed3 := ctrl.UpdateFromMPDStatus("pause", "44100:16:2")
	if !changed3 {
		t.Error("expected state change to report changed")
	}

	// Different format should change
	ctrl.UpdateFromMPDStatus("play", "44100:16:2")
	changed4 := ctrl.UpdateFromMPDStatus("play", "96000:24:2")
	if !changed4 {
		t.Error("expected format change to report changed")
	}
}

func TestFormatSampleRate(t *testing.T) {
	tests := []struct {
		sampleRate int
		expected   string
	}{
		{44100, "44.1kHz"},
		{48000, "48kHz"},
		{96000, "96kHz"},
		{192000, "192kHz"},
		{384000, "384kHz"},
		{2822400, "DSD64"},
		{5644800, "DSD128"},
		{11289600, "DSD256"},
		{22579200, "DSD512"},
		{8000, "8kHz"},
		{500, "500Hz"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := audio.FormatSampleRate(tt.sampleRate)
			if result != tt.expected {
				t.Errorf("FormatSampleRate(%d) = %q, want %q", tt.sampleRate, result, tt.expected)
			}
		})
	}
}

func TestFormatBitDepth(t *testing.T) {
	tests := []struct {
		bitDepth int
		expected string
	}{
		{16, "16-bit"},
		{24, "24-bit"},
		{32, "32-bit"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := audio.FormatBitDepth(tt.bitDepth)
			if result != tt.expected {
				t.Errorf("FormatBitDepth(%d) = %q, want %q", tt.bitDepth, result, tt.expected)
			}
		})
	}
}

func TestConcurrentAccess(t *testing.T) {
	ctrl := audio.NewController(true)

	// Test concurrent read/write access
	done := make(chan bool, 10)

	// Writers
	for i := 0; i < 5; i++ {
		go func(i int) {
			for j := 0; j < 100; j++ {
				if j%2 == 0 {
					ctrl.UpdateFromMPDStatus("play", "44100:16:2")
				} else {
					ctrl.UpdateFromMPDStatus("pause", "96000:24:2")
				}
			}
			done <- true
		}(i)
	}

	// Readers
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = ctrl.GetStatus()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// If we get here without panic/deadlock, the test passes
}
