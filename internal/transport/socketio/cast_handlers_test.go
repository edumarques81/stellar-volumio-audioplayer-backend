package socketio

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	domaincast "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/cast"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library"
	infracast "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cast"
)

// fakeCastManager records what the handlers asked the session manager to do.
type fakeCastManager struct {
	mu      sync.Mutex
	calls   []string
	devices []infracast.Device

	refreshErr error
	connectErr error
	playErr    error
	cmdErr     error
	skipped    []domaincast.Skipped
	state      domaincast.State

	playedItems []domaincast.Item
	playedStart int
	connectedTo string
	seekTo      int
	volumeTo    int
	mutedTo     bool
}

func (f *fakeCastManager) record(name string) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	f.mu.Unlock()
}

func (f *fakeCastManager) called(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == name {
			return true
		}
	}
	return false
}

func (f *fakeCastManager) Devices() []infracast.Device { f.record("Devices"); return f.devices }

func (f *fakeCastManager) RefreshDevices(context.Context) ([]infracast.Device, error) {
	f.record("RefreshDevices")
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	return f.devices, nil
}

func (f *fakeCastManager) Connect(_ context.Context, deviceID string) error {
	f.record("Connect")
	f.connectedTo = deviceID
	return f.connectErr
}

func (f *fakeCastManager) Disconnect()             { f.record("Disconnect") }
func (f *fakeCastManager) StopCasting() error      { f.record("StopCasting"); return f.cmdErr }
func (f *fakeCastManager) Active() bool            { return f.state.Active }
func (f *fakeCastManager) Resume() error           { f.record("Resume"); return f.cmdErr }
func (f *fakeCastManager) Pause() error            { f.record("Pause"); return f.cmdErr }
func (f *fakeCastManager) TogglePause() error      { f.record("TogglePause"); return f.cmdErr }
func (f *fakeCastManager) Stop() error             { f.record("Stop"); return f.cmdErr }
func (f *fakeCastManager) Next() error             { f.record("Next"); return f.cmdErr }
func (f *fakeCastManager) Previous() error         { f.record("Previous"); return f.cmdErr }
func (f *fakeCastManager) State() domaincast.State { return f.state }

func (f *fakeCastManager) Play(items []domaincast.Item, startIndex int) ([]domaincast.Skipped, error) {
	f.record("Play")
	f.playedItems = items
	f.playedStart = startIndex
	return f.skipped, f.playErr
}

func (f *fakeCastManager) Seek(seconds int) error {
	f.record("Seek")
	f.seekTo = seconds
	return f.cmdErr
}

func (f *fakeCastManager) SetVolume(percent int) error {
	f.record("SetVolume")
	f.volumeTo = percent
	return f.cmdErr
}

func (f *fakeCastManager) SetMuted(muted bool) error {
	f.record("SetMuted")
	f.mutedTo = muted
	return f.cmdErr
}

func (f *fakeCastManager) OnState(fn func(domaincast.State)) { f.record("OnState"); _ = fn }

// castLibrary is a LibraryService that only answers GetAlbumTracks.
type castLibrary struct {
	resp library.AlbumTracksResponse
	req  library.GetAlbumTracksRequest
}

func (l *castLibrary) GetAlbums(library.GetAlbumsRequest) library.AlbumsResponse {
	return library.AlbumsResponse{}
}
func (l *castLibrary) GetArtists(library.GetArtistsRequest) library.ArtistsResponse {
	return library.ArtistsResponse{}
}
func (l *castLibrary) GetArtistAlbums(library.GetArtistAlbumsRequest) library.ArtistAlbumsResponse {
	return library.ArtistAlbumsResponse{}
}
func (l *castLibrary) GetRadioStations(library.GetRadioRequest) library.RadioResponse {
	return library.RadioResponse{}
}

func (l *castLibrary) GetAlbumTracks(req library.GetAlbumTracksRequest) library.AlbumTracksResponse {
	l.req = req
	return l.resp
}

// castTestServer builds a Server with just enough wired for the cast
// handlers: the bundle and a library. It never starts socket.io, so the
// handlers under test are the ones that take a payload and return a response.
func castTestServer(mgr CastManager, lib LibraryService) *Server {
	s := &Server{}
	if mgr != nil {
		s.cast = &castBundle{mgr: mgr, lib: lib}
	}
	return s
}

func twoTrackAlbum() library.AlbumTracksResponse {
	return library.AlbumTracksResponse{
		Album:       "Kind of Blue",
		AlbumArtist: "Miles Davis",
		Tracks: []library.Track{
			{Title: "So What", Artist: "Miles Davis", Album: "Kind of Blue", URI: "USB/Kind of Blue/01.flac", TrackNumber: 1, Disc: 1, Duration: 545, AlbumArt: "/albumart?path=USB%2FKind+of+Blue"},
			{Title: "Freddie Freeloader", Artist: "Miles Davis", Album: "Kind of Blue", URI: "USB/Kind of Blue/02.flac", TrackNumber: 2, Disc: 1, Duration: 574},
		},
	}
}

func TestCastCommandRouting(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]interface{}
		want    string
		wantOK  bool
	}{
		{"play", map[string]interface{}{"cmd": "play"}, "Resume", true},
		{"pause", map[string]interface{}{"cmd": "pause"}, "Pause", true},
		{"toggle", map[string]interface{}{"cmd": "toggle"}, "TogglePause", true},
		{"playpause alias", map[string]interface{}{"cmd": "playpause"}, "TogglePause", true},
		{"stop", map[string]interface{}{"cmd": "stop"}, "Stop", true},
		{"next", map[string]interface{}{"cmd": "next"}, "Next", true},
		{"prev", map[string]interface{}{"cmd": "prev"}, "Previous", true},
		{"previous alias", map[string]interface{}{"cmd": "previous"}, "Previous", true},
		{"seek", map[string]interface{}{"cmd": "seek", "value": float64(90)}, "Seek", true},
		{"volume", map[string]interface{}{"cmd": "volume", "value": float64(35)}, "SetVolume", true},
		{"mute", map[string]interface{}{"cmd": "mute", "value": true}, "SetMuted", true},
		{"unknown", map[string]interface{}{"cmd": "teleport"}, "", false},
		{"missing cmd", map[string]interface{}{}, "", false},
		{"nil payload", nil, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := &fakeCastManager{}
			s := castTestServer(mgr, nil)

			resp := s.castCommand(tt.payload)
			if resp.OK != tt.wantOK {
				t.Errorf("OK = %v, want %v (error %q)", resp.OK, tt.wantOK, resp.Error)
			}
			if !tt.wantOK && resp.Error == "" {
				t.Error("a refused command carried no error")
			}
			if tt.want != "" && !mgr.called(tt.want) {
				t.Errorf("calls = %v, want %s", mgr.calls, tt.want)
			}
			if tt.want == "" && len(mgr.calls) != 0 {
				t.Errorf("a refused command still reached the manager: %v", mgr.calls)
			}
		})
	}
}

func TestCastCommandPassesValuesThrough(t *testing.T) {
	mgr := &fakeCastManager{}
	s := castTestServer(mgr, nil)

	if resp := s.castCommand(map[string]interface{}{"cmd": "seek", "value": float64(123)}); !resp.OK {
		t.Fatalf("seek: %v", resp.Error)
	}
	if mgr.seekTo != 123 {
		t.Errorf("seek = %d, want 123", mgr.seekTo)
	}

	if resp := s.castCommand(map[string]interface{}{"cmd": "volume", "value": float64(42)}); !resp.OK {
		t.Fatalf("volume: %v", resp.Error)
	}
	if mgr.volumeTo != 42 {
		t.Errorf("volume = %d, want 42", mgr.volumeTo)
	}

	if resp := s.castCommand(map[string]interface{}{"cmd": "mute", "value": true}); !resp.OK {
		t.Fatalf("mute: %v", resp.Error)
	}
	if !mgr.mutedTo {
		t.Error("mute = false, want true")
	}
}

func TestCastCommandSurfacesTheManagersError(t *testing.T) {
	// "No session" has to reach the caller: a transport button that silently
	// does nothing is the bug this prevents.
	mgr := &fakeCastManager{cmdErr: domaincast.ErrNotCasting}
	s := castTestServer(mgr, nil)

	resp := s.castCommand(map[string]interface{}{"cmd": "pause"})
	if resp.OK {
		t.Fatal("OK = true despite an error")
	}
	if !strings.Contains(resp.Error, "no session is active") {
		t.Errorf("Error = %q", resp.Error)
	}
}

func TestCastCommandWithoutUseCast(t *testing.T) {
	s := castTestServer(nil, nil)
	resp := s.castCommand(map[string]interface{}{"cmd": "play"})
	if resp.OK || resp.Error == "" {
		t.Errorf("resp = %+v, want a refusal", resp)
	}
	if s.CastIsActive() {
		t.Error("CastIsActive true with no manager")
	}
}

func TestCastPlayAlbumBuildsItemsFromTheLibrary(t *testing.T) {
	mgr := &fakeCastManager{}
	lib := &castLibrary{resp: twoTrackAlbum()}
	s := castTestServer(mgr, lib)

	resp := s.castPlayAlbum(map[string]interface{}{
		"album":       "Kind of Blue",
		"albumArtist": "Miles Davis",
		"uri":         "USB/Kind of Blue",
		"startIndex":  float64(1),
		"sampleRate":  float64(44100),
	})
	if !resp.OK {
		t.Fatalf("playAlbum: %v", resp.Error)
	}

	// The album identity must reach the library unchanged — the URI is what
	// scopes a lookup to one quality version of a duplicated release.
	if lib.req.Album != "Kind of Blue" || lib.req.AlbumArtist != "Miles Davis" || lib.req.URI != "USB/Kind of Blue" {
		t.Errorf("library request = %+v", lib.req)
	}

	if len(mgr.playedItems) != 2 {
		t.Fatalf("played %d items, want 2", len(mgr.playedItems))
	}
	if mgr.playedStart != 1 {
		t.Errorf("startIndex = %d, want 1", mgr.playedStart)
	}
	got := mgr.playedItems[0]
	if got.URI != "USB/Kind of Blue/01.flac" || got.Title != "So What" || got.TrackNumber != 1 || got.DurationSec != 545 {
		t.Errorf("item = %+v", got)
	}
	// AlbumArtist comes from the response, not the per-track Artist: that is
	// the field the receiver renders, and per-track soloist credits would
	// otherwise split one album across several headers on the device.
	if got.AlbumArtist != "Miles Davis" {
		t.Errorf("AlbumArtist = %q", got.AlbumArtist)
	}
	if got.SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want the payload's 44100", got.SampleRate)
	}
}

func TestCastPlayAlbumWithoutASampleRateStillPlays(t *testing.T) {
	// A client that does not send the rate must not be blocked: the policy
	// treats 0 as unknown-and-allowed, and DSD is still caught by extension.
	mgr := &fakeCastManager{}
	s := castTestServer(mgr, &castLibrary{resp: twoTrackAlbum()})

	resp := s.castPlayAlbum(map[string]interface{}{"album": "Kind of Blue"})
	if !resp.OK {
		t.Fatalf("playAlbum: %v", resp.Error)
	}
	if mgr.playedItems[0].SampleRate != 0 {
		t.Errorf("SampleRate = %d, want 0", mgr.playedItems[0].SampleRate)
	}
}

func TestCastPlayAlbumReportsSkippedTracks(t *testing.T) {
	mgr := &fakeCastManager{skipped: []domaincast.Skipped{
		{Item: domaincast.Item{Title: "Blue in Green", URI: "USB/Kind of Blue/03.dsf"}, Reason: "DSD is not supported by Cast receivers"},
	}}
	s := castTestServer(mgr, &castLibrary{resp: twoTrackAlbum()})

	resp := s.castPlayAlbum(map[string]interface{}{"album": "Kind of Blue"})
	if !resp.OK {
		t.Fatalf("playAlbum: %v", resp.Error)
	}
	if len(resp.Skipped) != 1 {
		t.Fatalf("Skipped = %+v, want one entry", resp.Skipped)
	}
	if resp.Skipped[0].Title != "Blue in Green" || resp.Skipped[0].Reason == "" || resp.Skipped[0].URI == "" {
		t.Errorf("skipped entry = %+v", resp.Skipped[0])
	}
}

func TestCastPlayAlbumRefusals(t *testing.T) {
	tests := []struct {
		name    string
		mgr     CastManager
		lib     LibraryService
		payload map[string]interface{}
		wantErr string
	}{
		{
			name:    "no album",
			mgr:     &fakeCastManager{},
			lib:     &castLibrary{resp: twoTrackAlbum()},
			payload: map[string]interface{}{},
			wantErr: "missing album",
		},
		{
			name:    "library error",
			mgr:     &fakeCastManager{},
			lib:     &castLibrary{resp: library.AlbumTracksResponse{Error: "album not found"}},
			payload: map[string]interface{}{"album": "Ghost"},
			wantErr: "album not found",
		},
		{
			name:    "empty album",
			mgr:     &fakeCastManager{},
			lib:     &castLibrary{resp: library.AlbumTracksResponse{Album: "Empty"}},
			payload: map[string]interface{}{"album": "Empty"},
			wantErr: "no tracks",
		},
		{
			name:    "nothing playable",
			mgr:     &fakeCastManager{playErr: domaincast.ErrNothingPlayable},
			lib:     &castLibrary{resp: twoTrackAlbum()},
			payload: map[string]interface{}{"album": "Kind of Blue"},
			wantErr: "can be cast",
		},
		{
			name:    "no library",
			mgr:     &fakeCastManager{},
			lib:     nil,
			payload: map[string]interface{}{"album": "Kind of Blue"},
			wantErr: "library",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := castTestServer(tt.mgr, tt.lib)
			resp := s.castPlayAlbum(tt.payload)
			if resp.OK {
				t.Fatal("OK = true, want a refusal")
			}
			if !strings.Contains(resp.Error, tt.wantErr) {
				t.Errorf("Error = %q, want it to mention %q", resp.Error, tt.wantErr)
			}
		})
	}
}

func TestCastPlayAlbumStillReportsSkipsWhenNothingIsPlayable(t *testing.T) {
	// The whole value of the refusal is the reason, so it must survive the
	// error path too.
	mgr := &fakeCastManager{
		playErr: domaincast.ErrNothingPlayable,
		skipped: []domaincast.Skipped{{Item: domaincast.Item{Title: "A", URI: "x.dsf"}, Reason: "DSD"}},
	}
	s := castTestServer(mgr, &castLibrary{resp: twoTrackAlbum()})

	resp := s.castPlayAlbum(map[string]interface{}{"album": "Kind of Blue"})
	if resp.OK {
		t.Fatal("OK = true")
	}
	if len(resp.Skipped) != 1 {
		t.Errorf("Skipped = %+v, want the reason to survive the error", resp.Skipped)
	}
}

func TestCastDevicesStripTheSocketAddress(t *testing.T) {
	in := []infracast.Device{
		{ID: "abc", Name: "Living Room Display", Model: "Google Nest Hub", Addr: "192.168.86.33:8009"},
		{ID: "grp", Name: "Whole House", IsGroup: true, Addr: "192.168.86.40:8009"},
	}
	out := castDevices(in)

	if len(out) != 2 {
		t.Fatalf("got %d devices, want 2", len(out))
	}
	if out[0].ID != "abc" || out[0].Name != "Living Room Display" || out[0].Model != "Google Nest Hub" {
		t.Errorf("device = %+v", out[0])
	}
	if !out[1].IsGroup {
		t.Error("speaker group lost its IsGroup flag")
	}
}

func TestCastPayloadIgnoresTheAckCallback(t *testing.T) {
	ack := func([]any, error) {}
	body := map[string]interface{}{"cmd": "play"}

	if got := castPayload([]any{body, ack}); got == nil || got["cmd"] != "play" {
		t.Errorf("payload = %+v", got)
	}
	if got := castPayload([]any{ack}); got != nil {
		t.Errorf("payload = %+v, want nil when only an ack was sent", got)
	}
	if got := castPayload(nil); got != nil {
		t.Errorf("payload = %+v, want nil", got)
	}
}

func TestCastAckPrefersTheCallback(t *testing.T) {
	var got []any
	ack := func(args []any, _ error) { got = args }

	castAck(nil, []any{map[string]interface{}{"cmd": "play"}, ack}, castResponse{OK: true})

	if len(got) != 1 {
		t.Fatalf("ack received %d args", len(got))
	}
	resp, ok := got[0].(castResponse)
	if !ok || !resp.OK {
		t.Errorf("ack arg = %+v", got[0])
	}
}

func TestCastLookupDevicesFallsBackToTheCachedList(t *testing.T) {
	// A transient mDNS failure must not empty the output picker.
	mgr := &fakeCastManager{
		refreshErr: errors.New("mdns browse failed"),
		devices:    []infracast.Device{{ID: "abc", Name: "Living Room Display"}},
	}
	s := castTestServer(mgr, nil)

	devices := s.castLookupDevices(context.Background())
	if len(devices) != 1 || devices[0].ID != "abc" {
		t.Errorf("devices = %+v, want the cached device", devices)
	}
	if !mgr.called("Devices") {
		t.Error("the cached list was never consulted")
	}
}

func TestCastLookupDevicesOnASuccessfulBrowse(t *testing.T) {
	mgr := &fakeCastManager{devices: []infracast.Device{{ID: "abc", Name: "Hub"}}}
	s := castTestServer(mgr, nil)

	if devices := s.castLookupDevices(context.Background()); len(devices) != 1 {
		t.Errorf("devices = %+v", devices)
	}
	if mgr.called("Devices") {
		t.Error("a successful browse still fell back to the cache")
	}
}
