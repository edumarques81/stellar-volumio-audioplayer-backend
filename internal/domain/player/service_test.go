package player

import (
	"testing"
)

func TestIsAudioFile(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		expected bool
	}{
		// Common audio formats
		{"FLAC file", "Music/Album/01-Track.flac", true},
		{"MP3 file", "Music/Album/track.mp3", true},
		{"WAV file", "path/to/file.wav", true},
		{"AIFF file", "file.aiff", true},
		{"AIF file", "file.aif", true},
		{"OGG file", "music.ogg", true},
		{"M4A file", "song.m4a", true},
		{"AAC file", "audio.aac", true},
		{"WMA file", "track.wma", true},

		// High-res formats
		{"DSF file", "NAS/MusicLibrary/Album/01-Track.dsf", true},
		{"DFF file", "music/track.dff", true},
		{"DSD file", "audio.dsd", true},

		// Lossless formats
		{"APE file", "track.ape", true},
		{"WavPack file", "music.wv", true},
		{"Musepack file", "audio.mpc", true},
		{"Opus file", "stream.opus", true},
		{"ALAC file", "track.alac", true},

		// Case insensitivity
		{"Uppercase FLAC", "track.FLAC", true},
		{"Mixed case MP3", "track.Mp3", true},
		{"Mixed case DSF", "track.DsF", true},

		// Non-audio files
		{"Text file", "readme.txt", false},
		{"Image file", "cover.jpg", false},
		{"Playlist file", "playlist.m3u", false},
		{"CUE file", "album.cue", false},
		{"Directory", "Music/Album", false},
		{"No extension", "filename", false},
		{"Hidden file", ".hidden", false},

		// Edge cases
		{"Empty string", "", false},
		{"Just extension", ".flac", true},
		{"Path with dots", "artist.name/album.title/track.flac", true},
		{"Space in name", "01 - Black Coffee .dsf", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAudioFile(tt.uri)
			if result != tt.expected {
				t.Errorf("isAudioFile(%q) = %v, want %v", tt.uri, result, tt.expected)
			}
		})
	}
}

// MockMPDClient implements the MPD client interface for testing.
type MockMPDClient struct {
	ClearCalled    bool
	AddCalled      bool
	PlayCalled     bool
	ListInfoCalled bool

	AddedURIs     []string
	PlayPosition  int
	ListInfoDir   string
	ListInfoItems []map[string]string

	ClearError    error
	AddError      error
	PlayError     error
	ListInfoError error
}

func (m *MockMPDClient) Clear() error {
	m.ClearCalled = true
	return m.ClearError
}

func (m *MockMPDClient) Add(uri string) error {
	m.AddCalled = true
	m.AddedURIs = append(m.AddedURIs, uri)
	return m.AddError
}

func (m *MockMPDClient) Play(pos int) error {
	m.PlayCalled = true
	m.PlayPosition = pos
	return m.PlayError
}

func (m *MockMPDClient) ListInfo(uri string) ([]map[string]string, error) {
	m.ListInfoCalled = true
	m.ListInfoDir = uri
	if m.ListInfoError != nil {
		return nil, m.ListInfoError
	}
	return m.ListInfoItems, nil
}

// Note: Full integration tests for ReplaceAndPlay would require
// a mock MPD client that implements the full interface.
// The isAudioFile helper is tested thoroughly above.
// Integration testing is done on the Raspberry Pi with real MPD.
