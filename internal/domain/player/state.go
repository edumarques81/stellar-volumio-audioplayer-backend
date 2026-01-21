// Package player provides the core player domain logic for audio playback control.
package player

import "sync"

// Status constants for player state
const (
	StatusPlay  = "play"
	StatusPause = "pause"
	StatusStop  = "stop"
)

// TrackInfo contains metadata about the currently playing track.
type TrackInfo struct {
	Title      string
	Artist     string
	Album      string
	AlbumArt   string
	URI        string
	Duration   int    // Duration in seconds
	TrackType  string // flac, mp3, dsf, etc.
	SampleRate string // e.g., "96000", "192000"
	BitDepth   string // e.g., "16", "24", "32"
	BitRate    string // For lossy formats
	Service    string // mpd, tidal, qobuz, etc.
}

// State represents the current player state.
// It is safe for concurrent access.
type State struct {
	mu sync.RWMutex

	// Playback state
	Status   string
	Position int // Current position in queue
	Seek     int // Current seek position in milliseconds

	// Track info
	Title      string
	Artist     string
	Album      string
	AlbumArt   string
	URI        string
	Duration   int
	TrackType  string
	SampleRate string
	BitDepth   string
	BitRate    string
	Service    string

	// Playback options
	Random       bool
	Repeat       bool
	RepeatSingle bool

	// Volume
	Volume int
	Mute   bool

	// Stream info (for internet radio, etc.)
	Stream string

	// Bit-perfect mode indicator
	BitPerfect bool
}

// NewState creates a new player state with default values.
func NewState() *State {
	return &State{
		Status: StatusStop,
		Volume: 100,
	}
}

// Play sets the player status to playing.
func (s *State) Play() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = StatusPlay
}

// Pause sets the player status to paused.
func (s *State) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = StatusPause
}

// Stop sets the player status to stopped and resets seek position.
func (s *State) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = StatusStop
	s.Seek = 0
}

// SetVolume sets the volume level (0-100).
func (s *State) SetVolume(volume int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if volume < 0 {
		volume = 0
	} else if volume > 100 {
		volume = 100
	}
	s.Volume = volume
}

// ToggleMute toggles the mute state.
func (s *State) ToggleMute() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Mute = !s.Mute
}

// ToggleRandom toggles the shuffle/random state.
func (s *State) ToggleRandom() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Random = !s.Random
}

// SetRepeat sets the repeat mode.
func (s *State) SetRepeat(repeat, repeatSingle bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Repeat = repeat
	s.RepeatSingle = repeatSingle
}

// UpdateTrack updates the current track information.
func (s *State) UpdateTrack(track TrackInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Title = track.Title
	s.Artist = track.Artist
	s.Album = track.Album
	s.AlbumArt = track.AlbumArt
	s.URI = track.URI
	s.Duration = track.Duration
	s.TrackType = track.TrackType
	s.SampleRate = track.SampleRate
	s.BitDepth = track.BitDepth
	s.BitRate = track.BitRate
	s.Service = track.Service
}

// UpdateSeek updates the current seek position.
func (s *State) UpdateSeek(seek int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Seek = seek
}

// UpdatePosition updates the current queue position.
func (s *State) UpdatePosition(position int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Position = position
}

// ToJSON returns the state as a map suitable for JSON serialization.
// This matches the Volumio pushState format.
func (s *State) ToJSON() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"status":       s.Status,
		"position":     s.Position,
		"seek":         s.Seek,
		"title":        s.Title,
		"artist":       s.Artist,
		"album":        s.Album,
		"albumart":     s.AlbumArt,
		"uri":          s.URI,
		"duration":     s.Duration,
		"trackType":    s.TrackType,
		"samplerate":   s.SampleRate,
		"bitdepth":     s.BitDepth,
		"bitrate":      s.BitRate,
		"service":      s.Service,
		"random":       s.Random,
		"repeat":       s.Repeat,
		"repeatSingle": s.RepeatSingle,
		"volume":       s.Volume,
		"mute":         s.Mute,
		"stream":       s.Stream,
		"bitperfect":   s.BitPerfect,
	}
}

// Clone returns a copy of the current state.
func (s *State) Clone() *State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &State{
		Status:       s.Status,
		Position:     s.Position,
		Seek:         s.Seek,
		Title:        s.Title,
		Artist:       s.Artist,
		Album:        s.Album,
		AlbumArt:     s.AlbumArt,
		URI:          s.URI,
		Duration:     s.Duration,
		TrackType:    s.TrackType,
		SampleRate:   s.SampleRate,
		BitDepth:     s.BitDepth,
		BitRate:      s.BitRate,
		Service:      s.Service,
		Random:       s.Random,
		Repeat:       s.Repeat,
		RepeatSingle: s.RepeatSingle,
		Volume:       s.Volume,
		Mute:         s.Mute,
		Stream:       s.Stream,
		BitPerfect:   s.BitPerfect,
	}
}
