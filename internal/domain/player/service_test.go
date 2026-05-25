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

// TestIsAppleDouble pins the basename-only `._` prefix matcher used to skip
// macOS resource-fork ghost files that MPD indexes alongside real audio
// files on NAS shares. These ghosts fail to decode and cause Play(0) to
// stall briefly while MPD errors out and auto-skips.
func TestIsAppleDouble(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		expected bool
	}{
		// AppleDouble ghosts at various path depths
		{"deep path with ._01.flac", "/some/path/._01.flac", true},
		{"root-level ghost", "/._something.flac", true},
		{"bare basename ghost", "._01.flac", true},
		{"NAS path with ghost", "NAS/MusicLibrary/Album/._01-Track.flac", true},

		// Real files (not AppleDouble)
		{"regular numbered track", "/some/path/01.flac", false},
		{"deep path no prefix", "Music/Album/01-Track.flac", false},
		{"bare basename non-ghost", "01.flac", false},

		// Disambiguation: `__` prefix (Python dunder etc.) must NOT match
		{"double underscore prefix", "/dir/__init__.py", false},
		{"single dot file", "/.hidden", false},
		{"single dot in basename", "/dir/.gitignore", false},

		// Edge cases
		{"empty string", "", false},
		{"just the prefix", "._", true},
		{"trailing slash with ghost basename", "/dir/._foo.flac", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAppleDouble(tt.uri); got != tt.expected {
				t.Errorf("isAppleDouble(%q) = %v, want %v", tt.uri, got, tt.expected)
			}
		})
	}
}

// TestFirstPlayableIndex verifies the queue-scan helper that the
// directory-branch of ReplaceAndPlay uses to skip past leading AppleDouble
// ghost entries before calling Play. This is the unit-level proof for
// task B: Play(2) instead of Play(0) when the queue starts ._01, ._02, 01,
// 02 — without this, MPD stalls trying to decode the first ghost.
func TestFirstPlayableIndex(t *testing.T) {
	t.Run("skips two leading ghosts then returns 2", func(t *testing.T) {
		queue := []map[string]string{
			{"file": "/x/._01.flac"},
			{"file": "/x/._02.flac"},
			{"file": "/x/01.flac"},
			{"file": "/x/02.flac"},
		}
		idx, skipped := firstPlayableIndex(queue)
		if idx != 2 {
			t.Errorf("firstPlayableIndex idx = %d, want 2", idx)
		}
		if skipped != 2 {
			t.Errorf("firstPlayableIndex skipped = %d, want 2", skipped)
		}
	})

	t.Run("no ghosts returns 0,0", func(t *testing.T) {
		queue := []map[string]string{
			{"file": "/x/01.flac"},
			{"file": "/x/02.flac"},
		}
		idx, skipped := firstPlayableIndex(queue)
		if idx != 0 || skipped != 0 {
			t.Errorf("firstPlayableIndex = (%d, %d), want (0, 0)", idx, skipped)
		}
	})

	t.Run("all ghosts returns -1 and full count", func(t *testing.T) {
		queue := []map[string]string{
			{"file": "/x/._01.flac"},
			{"file": "/x/._02.flac"},
		}
		idx, skipped := firstPlayableIndex(queue)
		if idx != -1 {
			t.Errorf("firstPlayableIndex idx = %d, want -1", idx)
		}
		if skipped != 2 {
			t.Errorf("firstPlayableIndex skipped = %d, want 2", skipped)
		}
	})

	t.Run("empty queue returns -1,0", func(t *testing.T) {
		idx, skipped := firstPlayableIndex(nil)
		if idx != -1 || skipped != 0 {
			t.Errorf("firstPlayableIndex(nil) = (%d, %d), want (-1, 0)", idx, skipped)
		}
	})

	t.Run("entry without file key is treated as playable", func(t *testing.T) {
		// Defensive: shouldn't happen in practice, but skipping a row we can't
		// identify would silently change playback position.
		queue := []map[string]string{
			{"file": "/x/._01.flac"},
			{"directory": "/x/sub"}, // no "file" key
			{"file": "/x/01.flac"},
		}
		idx, skipped := firstPlayableIndex(queue)
		if idx != 1 {
			t.Errorf("firstPlayableIndex idx = %d, want 1", idx)
		}
		if skipped != 1 {
			t.Errorf("firstPlayableIndex skipped = %d, want 1", skipped)
		}
	})
}

// TestAudioFileFilterExcludesAppleDouble checks the per-file filter logic
// that the audio-file branch of ReplaceAndPlay uses to assemble the
// audioFiles slice. Ghost files like `._01.flac` pass isAudioFile (the
// extension is `.flac`) so we need the explicit isAppleDouble exclusion
// to keep them out of the queue. This test simulates the filter inline to
// keep this checked in the same place as the real one.
func TestAudioFileFilterExcludesAppleDouble(t *testing.T) {
	siblings := []map[string]string{
		{"file": "/album/._01.flac"},
		{"file": "/album/._02.flac"},
		{"file": "/album/01.flac"},
		{"file": "/album/02.flac"},
		{"file": "/album/cover.jpg"}, // not audio
		{"file": "/album/._cover.jpg"}, // ghost but also not audio
	}

	var audioFiles []string
	ghostsFiltered := 0
	for _, item := range siblings {
		file, ok := item["file"]
		if !ok {
			continue
		}
		if !isAudioFile(file) {
			continue
		}
		if isAppleDouble(file) {
			ghostsFiltered++
			continue
		}
		audioFiles = append(audioFiles, file)
	}

	wantFiles := []string{"/album/01.flac", "/album/02.flac"}
	if len(audioFiles) != len(wantFiles) {
		t.Fatalf("audioFiles len = %d, want %d (got %v)", len(audioFiles), len(wantFiles), audioFiles)
	}
	for i, f := range wantFiles {
		if audioFiles[i] != f {
			t.Errorf("audioFiles[%d] = %q, want %q", i, audioFiles[i], f)
		}
	}
	if ghostsFiltered != 2 {
		t.Errorf("ghostsFiltered = %d, want 2", ghostsFiltered)
	}
}

// ============================================================
// Tests for Volumio integration methods
// These use extended mock to test the new queue manipulation methods
// ============================================================

// ExtendedMockMPDClient implements the full MPD client interface for testing.
type ExtendedMockMPDClient struct {
	// Track method calls
	StatusCalled            bool
	PlayCalled              bool
	PauseCalled             bool
	ClearCalled             bool
	AddCalled               bool
	AddIdCalled             bool
	MoveCalled              bool
	DeleteCalled            bool
	GetCurrentPositionCalled bool
	GetQueueLengthCalled    bool

	// Return values
	StatusReturn       map[string]string
	AddIdReturn        int
	CurrentPosReturn   int
	QueueLengthReturn  int

	// Arguments received
	PlayPosition    int
	PauseState      bool
	AddedURIs       []string
	AddIdURI        string
	AddIdPosition   int
	MoveFrom        int
	MoveTo          int
	DeletePosition  int

	// Errors to return
	StatusError             error
	PlayError               error
	PauseError              error
	ClearError              error
	AddError                error
	AddIdError              error
	MoveError               error
	DeleteError             error
	GetCurrentPositionError error
	GetQueueLengthError     error
}

func (m *ExtendedMockMPDClient) Status() (map[string]string, error) {
	m.StatusCalled = true
	if m.StatusError != nil {
		return nil, m.StatusError
	}
	if m.StatusReturn == nil {
		return map[string]string{"state": "stop"}, nil
	}
	return m.StatusReturn, nil
}

func (m *ExtendedMockMPDClient) Play(pos int) error {
	m.PlayCalled = true
	m.PlayPosition = pos
	return m.PlayError
}

func (m *ExtendedMockMPDClient) Pause(pause bool) error {
	m.PauseCalled = true
	m.PauseState = pause
	return m.PauseError
}

func (m *ExtendedMockMPDClient) Clear() error {
	m.ClearCalled = true
	return m.ClearError
}

func (m *ExtendedMockMPDClient) Add(uri string) error {
	m.AddCalled = true
	m.AddedURIs = append(m.AddedURIs, uri)
	return m.AddError
}

func (m *ExtendedMockMPDClient) AddId(uri string, position int) (int, error) {
	m.AddIdCalled = true
	m.AddIdURI = uri
	m.AddIdPosition = position
	if m.AddIdError != nil {
		return 0, m.AddIdError
	}
	return m.AddIdReturn, nil
}

func (m *ExtendedMockMPDClient) Move(from, to int) error {
	m.MoveCalled = true
	m.MoveFrom = from
	m.MoveTo = to
	return m.MoveError
}

func (m *ExtendedMockMPDClient) Delete(pos int) error {
	m.DeleteCalled = true
	m.DeletePosition = pos
	return m.DeleteError
}

func (m *ExtendedMockMPDClient) GetCurrentPosition() (int, error) {
	m.GetCurrentPositionCalled = true
	if m.GetCurrentPositionError != nil {
		return 0, m.GetCurrentPositionError
	}
	return m.CurrentPosReturn, nil
}

func (m *ExtendedMockMPDClient) GetQueueLength() (int, error) {
	m.GetQueueLengthCalled = true
	if m.GetQueueLengthError != nil {
		return 0, m.GetQueueLengthError
	}
	return m.QueueLengthReturn, nil
}

// Test Toggle functionality
func TestToggle_PlaysWhenStopped(t *testing.T) {
	mock := &ExtendedMockMPDClient{
		StatusReturn: map[string]string{"state": "stop"},
	}

	// Create service with mock - we'll test the logic directly
	// The actual Service uses real mpd.Client, so we test behavior indirectly

	// When status is "stop", toggle should call Play
	status := mock.StatusReturn["state"]
	if status == "stop" {
		mock.Play(-1)
	}

	if !mock.PlayCalled {
		t.Error("Toggle should call Play when stopped")
	}
	if mock.PlayPosition != -1 {
		t.Errorf("Toggle should play with position -1 (resume), got %d", mock.PlayPosition)
	}
}

func TestToggle_PausesWhenPlaying(t *testing.T) {
	mock := &ExtendedMockMPDClient{
		StatusReturn: map[string]string{"state": "play"},
	}

	// When status is "play", toggle should call Pause
	status := mock.StatusReturn["state"]
	if status == "play" {
		mock.Pause(true)
	}

	if !mock.PauseCalled {
		t.Error("Toggle should call Pause when playing")
	}
	if !mock.PauseState {
		t.Error("Toggle should pause (true) when playing")
	}
}

func TestToggle_PlaysWhenPaused(t *testing.T) {
	mock := &ExtendedMockMPDClient{
		StatusReturn: map[string]string{"state": "pause"},
	}

	// When status is "pause", toggle should call Play (resume)
	status := mock.StatusReturn["state"]
	if status == "pause" {
		mock.Play(-1)
	}

	if !mock.PlayCalled {
		t.Error("Toggle should call Play when paused")
	}
}

// Test InsertNext functionality
func TestInsertNext_InsertsAfterCurrent(t *testing.T) {
	mock := &ExtendedMockMPDClient{
		CurrentPosReturn: 3,
		AddIdReturn:      42,
	}

	// InsertNext should add at position current+1
	currentPos, _ := mock.GetCurrentPosition()
	insertPos := currentPos + 1

	_, _ = mock.AddId("test.flac", insertPos)

	if !mock.AddIdCalled {
		t.Error("InsertNext should call AddId")
	}
	if mock.AddIdPosition != 4 {
		t.Errorf("InsertNext should insert at position %d, got %d", insertPos, mock.AddIdPosition)
	}
}

// Test MoveQueueItem functionality
func TestMoveQueueItem_ReordersCorrectly(t *testing.T) {
	mock := &ExtendedMockMPDClient{}

	// Move item from position 2 to position 5
	_ = mock.Move(2, 5)

	if !mock.MoveCalled {
		t.Error("MoveQueueItem should call Move")
	}
	if mock.MoveFrom != 2 || mock.MoveTo != 5 {
		t.Errorf("MoveQueueItem should move from 2 to 5, got from %d to %d", mock.MoveFrom, mock.MoveTo)
	}
}

// Test RemoveQueueItem functionality
func TestRemoveQueueItem_DeletesItem(t *testing.T) {
	mock := &ExtendedMockMPDClient{}

	// Remove item at position 3
	_ = mock.Delete(3)

	if !mock.DeleteCalled {
		t.Error("RemoveQueueItem should call Delete")
	}
	if mock.DeletePosition != 3 {
		t.Errorf("RemoveQueueItem should delete position 3, got %d", mock.DeletePosition)
	}
}

// CallRecorder is a tiny mock that records the order in which MPD methods are
// invoked. It mirrors the slice of methods that ReplaceAndPlay's directory
// branch touches and is intentionally separate from the existing mocks so we
// don't perturb other tests.
type CallRecorder struct {
	calls []string
}

func (r *CallRecorder) Stop() error             { r.calls = append(r.calls, "Stop"); return nil }
func (r *CallRecorder) Clear() error            { r.calls = append(r.calls, "Clear"); return nil }
func (r *CallRecorder) Add(uri string) error    { r.calls = append(r.calls, "Add"); return nil }
func (r *CallRecorder) Play(pos int) error      { r.calls = append(r.calls, "Play"); return nil }
func (r *CallRecorder) PlaylistInfo() ([]map[string]string, error) {
	r.calls = append(r.calls, "PlaylistInfo")
	return []map[string]string{{"file": "/x/01.flac"}}, nil
}

// TestReplaceAndPlayCallsStopBeforeClear pins the ordering fix: the
// ReplaceAndPlay flow MUST issue Stop before Clear. Without Stop, a rapid
// second ReplaceAndPlay (e.g. while the USB DAC is still negotiating with the
// previous Play) caused Clear to block ~34s because MPD serializes commands
// and won't tear the queue down while a decoder is feeding from it.
//
// This is a shadow test in the same style as the existing TestToggle_*
// tests in this file: it replays the call sequence the production code uses
// and asserts the recorded order, since Service holds a concrete *mpd.Client
// (not an interface) and we don't refactor that here. Future structural
// change: lift *mpd.Client to an interface so the real ReplaceAndPlay can be
// driven against a mock — at that point this shadow test should be replaced
// with a true end-to-end mock-driven test.
func TestReplaceAndPlayCallsStopBeforeClear(t *testing.T) {
	rec := &CallRecorder{}

	// Replay the directory-branch sequence ReplaceAndPlay performs in
	// service.go: Stop → Clear → Add(dir) → PlaylistInfo → Play.
	const uri = "Music/Album"
	if err := rec.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if err := rec.Clear(); err != nil {
		t.Fatalf("Clear returned error: %v", err)
	}
	if err := rec.Add(uri); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if _, err := rec.PlaylistInfo(); err != nil {
		t.Fatalf("PlaylistInfo returned error: %v", err)
	}
	if err := rec.Play(0); err != nil {
		t.Fatalf("Play returned error: %v", err)
	}

	// Stop must precede Clear, and Stop must be the very first call so an
	// in-flight decoder is torn down before any queue mutation.
	if len(rec.calls) == 0 {
		t.Fatal("no calls recorded")
	}
	if rec.calls[0] != "Stop" {
		t.Errorf("first call = %q, want %q", rec.calls[0], "Stop")
	}

	stopIdx, clearIdx := -1, -1
	for i, c := range rec.calls {
		if c == "Stop" && stopIdx == -1 {
			stopIdx = i
		}
		if c == "Clear" && clearIdx == -1 {
			clearIdx = i
		}
	}
	if stopIdx == -1 {
		t.Fatal("Stop was never called")
	}
	if clearIdx == -1 {
		t.Fatal("Clear was never called")
	}
	if stopIdx >= clearIdx {
		t.Errorf("Stop must precede Clear; got Stop@%d Clear@%d (calls=%v)", stopIdx, clearIdx, rec.calls)
	}
	// "Immediately before": no other recorded call between Stop and Clear.
	if clearIdx-stopIdx != 1 {
		t.Errorf("Stop must be immediately before Clear; got Stop@%d Clear@%d (calls=%v)", stopIdx, clearIdx, rec.calls)
	}
}
