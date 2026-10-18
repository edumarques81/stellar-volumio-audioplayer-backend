package cast

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		name            string
		uri             string
		sampleRate      int
		wantPlayability Playability
		wantContentType string
	}{
		// 850 of this library's 1,068 files land here.
		{"CD-rate FLAC", "USB/Album/01.flac", 44100, PlayableDirect, "audio/flac"},
		{"96 kHz FLAC", "USB/Album/01.flac", 96000, PlayableDirect, "audio/flac"},
		// The measured ceiling, which is double Google's published figure.
		{"192 kHz FLAC is exactly at the ceiling", "USB/Album/01.flac", 192000, PlayableDirect, "audio/flac"},
		{"WAV", "USB/Album/01.wav", 176400, PlayableDirect, "audio/wav"},
		{"MP3", "USB/Album/01.mp3", 44100, PlayableDirect, "audio/mpeg"},

		// The 31 files that need processing.
		{"352.8 kHz FLAC", "USB/Album/01.flac", 352800, NeedsResample, "audio/flac"},
		{"384 kHz WAV", "USB/Album/01.wav", 384000, NeedsResample, "audio/wav"},

		// The 187 files that never will.
		{"DSF", "USB/Album/01.dsf", 2822400, Unplayable, ""},
		{"DFF", "USB/Album/01.dff", 2822400, Unplayable, ""},
		{"DSF at a low reported rate is still DSD", "USB/Album/01.dsf", 44100, Unplayable, ""},

		{"unknown extension", "USB/Album/notes.txt", 0, Unplayable, ""},
		{"no extension", "USB/Album/track", 0, Unplayable, ""},
		// /stream serves folder artwork so a DIDL albumArtURI can point at it;
		// it is not something to cast.
		{"folder artwork", "USB/Album/folder.jpg", 0, Unplayable, ""},

		// An album the cache has not measured yet must not be hidden on a
		// guess — the device gets to refuse it with its own reason.
		{"unknown sample rate is allowed through", "USB/Album/01.flac", 0, PlayableDirect, "audio/flac"},

		{"upper-case extension", "USB/Album/01.FLAC", 44100, PlayableDirect, "audio/flac"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.uri, tt.sampleRate)
			if got.Playability != tt.wantPlayability {
				t.Errorf("Playability = %q, want %q (reason %q)", got.Playability, tt.wantPlayability, got.Reason)
			}
			if got.ContentType != tt.wantContentType {
				t.Errorf("ContentType = %q, want %q", got.ContentType, tt.wantContentType)
			}
			if got.Playable() != (tt.wantPlayability == PlayableDirect) {
				t.Errorf("Playable() = %v", got.Playable())
			}
			if tt.wantPlayability != PlayableDirect && got.Reason == "" {
				t.Error("a refusal carried no reason to show the user")
			}
		})
	}
}
