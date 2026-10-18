package cast

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	infracast "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cast"
)

// stubDevice stands in for the protocol client. The protocol itself is covered
// in internal/infra/cast against a fake receiver and a real Nest Hub; what
// matters here is the queue, the policy filter and the published state.
type stubDevice struct {
	mu     sync.Mutex
	status infracast.Status
	loads  []infracast.Media
	calls  []string
	closed chan struct{}
	err    error

	loadErr error
}

func newStubDevice() *stubDevice {
	return &stubDevice{closed: make(chan struct{})}
}

func (d *stubDevice) Status() infracast.Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status
}

func (d *stubDevice) setStatus(s infracast.Status) {
	d.mu.Lock()
	d.status = s
	d.mu.Unlock()
}

func (d *stubDevice) record(name string) {
	d.mu.Lock()
	d.calls = append(d.calls, name)
	d.mu.Unlock()
}

func (d *stubDevice) Load(m infracast.Media) error {
	d.record("Load")
	if d.loadErr != nil {
		return d.loadErr
	}
	d.mu.Lock()
	d.loads = append(d.loads, m)
	d.status.HasMedia = true
	d.status.PlayerState = "PLAYING"
	d.status.ContentID = m.URL
	d.status.Duration = m.Duration
	d.status.IdleReason = ""
	d.mu.Unlock()
	return nil
}

func (d *stubDevice) Play() error {
	d.record("Play")
	d.mu.Lock()
	d.status.PlayerState = "PLAYING"
	d.mu.Unlock()
	return nil
}

func (d *stubDevice) Pause() error {
	d.record("Pause")
	d.mu.Lock()
	d.status.PlayerState = "PAUSED"
	d.mu.Unlock()
	return nil
}

func (d *stubDevice) StopMedia() error {
	d.record("StopMedia")
	d.mu.Lock()
	d.status.PlayerState = "IDLE"
	d.status.IdleReason = "CANCELLED"
	d.status.HasMedia = false
	d.mu.Unlock()
	return nil
}

func (d *stubDevice) Seek(seconds float64) error {
	d.record("Seek")
	d.mu.Lock()
	d.status.CurrentTime = seconds
	d.mu.Unlock()
	return nil
}

func (d *stubDevice) SetVolume(level float64) error {
	d.record("SetVolume")
	d.mu.Lock()
	d.status.Volume = level
	d.mu.Unlock()
	return nil
}

func (d *stubDevice) SetMuted(muted bool) error {
	d.record("SetMuted")
	d.mu.Lock()
	d.status.Muted = muted
	d.mu.Unlock()
	return nil
}

func (d *stubDevice) QuitApp() error {
	d.record("QuitApp")
	return nil
}

func (d *stubDevice) Close() error {
	d.record("Close")
	select {
	case <-d.closed:
	default:
		close(d.closed)
	}
	return nil
}

func (d *stubDevice) Closed() <-chan struct{} { return d.closed }
func (d *stubDevice) Err() error              { return d.err }

func (d *stubDevice) lastLoad() (infracast.Media, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.loads) == 0 {
		return infracast.Media{}, false
	}
	return d.loads[len(d.loads)-1], true
}

func (d *stubDevice) loadCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.loads)
}

const testBase = "http://192.168.86.25:3000"

// newTestManager returns a Manager already connected to a stub device.
func newTestManager(t *testing.T) (*Manager, *stubDevice) {
	t.Helper()

	dev := newStubDevice()
	m := &Manager{
		discoverer: infracast.NewDiscoverer(),
		baseURL:    func() string { return testBase },
		dial: func(string, func(infracast.Status)) (deviceClient, error) {
			return dev, nil
		},
	}
	if err := m.connectTo(infracast.Device{ID: "hub", Name: "Living Room Display", Addr: "192.168.86.33:8009"}); err != nil {
		t.Fatalf("connectTo: %v", err)
	}
	t.Cleanup(m.Disconnect)
	return m, dev
}

func flacItem(n int, title string) Item {
	return Item{
		URI:         "USB/Kind of Blue/0" + string(rune('0'+n)) + ".flac",
		Title:       title,
		Artist:      "Miles Davis",
		Album:       "Kind of Blue",
		AlbumArtist: "Miles Davis",
		TrackNumber: n,
		DurationSec: 300 + n,
		SampleRate:  44100,
		ArtPath:     "/albumart?path=USB%2FKind+of+Blue%2F01.flac",
	}
}

func TestTransportCommandsNeedASession(t *testing.T) {
	m := &Manager{discoverer: infracast.NewDiscoverer(), baseURL: func() string { return testBase }}

	tests := []struct {
		name string
		call func() error
	}{
		{"resume", m.Resume},
		{"pause", m.Pause},
		{"toggle", m.TogglePause},
		{"stop", m.Stop},
		{"next", m.Next},
		{"previous", m.Previous},
		{"seek", func() error { return m.Seek(10) }},
		{"volume", func() error { return m.SetVolume(50) }},
		{"mute", func() error { return m.SetMuted(true) }},
		{"stop casting", m.StopCasting},
		{"play", func() error { _, err := m.Play([]Item{flacItem(1, "So What")}, 0); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, ErrNotCasting) {
				t.Errorf("err = %v, want ErrNotCasting", err)
			}
		})
	}
}

func TestConnectToAnUnknownDeviceFails(t *testing.T) {
	m := NewManager(func() string { return testBase })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := m.Connect(ctx, "not-a-device"); !errors.Is(err, ErrNoDevice) {
		t.Errorf("err = %v, want ErrNoDevice", err)
	}
}

func TestPlayLoadsTheFirstTrackWithFullMetadata(t *testing.T) {
	m, dev := newTestManager(t)

	skipped, err := m.Play([]Item{flacItem(1, "So What"), flacItem(2, "Freddie Freeloader")}, 0)
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %+v, want none", skipped)
	}

	media, ok := dev.lastLoad()
	if !ok {
		t.Fatal("nothing was loaded")
	}
	if media.ContentType != "audio/flac" {
		t.Errorf("ContentType = %q", media.ContentType)
	}
	// The URL must target the backend's stream endpoint with each segment
	// escaped — the device fetches it itself.
	if !strings.HasPrefix(media.URL, testBase+"/stream/USB/Kind%20of%20Blue/") {
		t.Errorf("URL = %q", media.URL)
	}
	if media.Metadata == nil {
		t.Fatal("no metadata sent")
	}
	md := media.Metadata
	if md.MetadataType != infracast.MetadataTypeMusicTrack {
		t.Errorf("metadataType = %d, want music track", md.MetadataType)
	}
	if md.Title != "So What" || md.AlbumName != "Kind of Blue" || md.AlbumArtist != "Miles Davis" || md.TrackNumber != 1 {
		t.Errorf("metadata = %+v", md)
	}
	if len(md.Images) != 1 || !strings.HasPrefix(md.Images[0].URL, testBase+"/albumart?path=") {
		t.Errorf("artwork = %+v", md.Images)
	}
}

func TestPlayReportsSkippedTracksRatherThanHidingThem(t *testing.T) {
	// A mixed album is common here: 17.5% of the library is DSD. Dropping
	// those silently would make the album look shorter than it is.
	m, dev := newTestManager(t)

	dsd := flacItem(2, "Blue in Green")
	dsd.URI = "USB/Kind of Blue/02.dsf"
	dsd.SampleRate = 2822400

	hires := flacItem(3, "All Blues")
	hires.SampleRate = 352800

	skipped, err := m.Play([]Item{flacItem(1, "So What"), dsd, hires}, 0)
	if err != nil {
		t.Fatalf("Play: %v", err)
	}

	if len(skipped) != 2 {
		t.Fatalf("skipped %d tracks, want 2: %+v", len(skipped), skipped)
	}
	for _, s := range skipped {
		if s.Reason == "" {
			t.Errorf("skipped %q with no reason", s.Item.Title)
		}
	}
	if !strings.Contains(skipped[0].Reason, "DSD") {
		t.Errorf("DSD reason = %q", skipped[0].Reason)
	}
	if !strings.Contains(skipped[1].Reason, "192") {
		t.Errorf("high-rate reason = %q", skipped[1].Reason)
	}

	if got := m.State().QueueLength; got != 1 {
		t.Errorf("QueueLength = %d, want 1", got)
	}
	if dev.loadCount() != 1 {
		t.Errorf("loads = %d, want 1", dev.loadCount())
	}
}

func TestPlayFailsWhenNothingIsPlayable(t *testing.T) {
	m, dev := newTestManager(t)

	dsd := flacItem(1, "Track")
	dsd.URI = "USB/Album/01.dsf"

	skipped, err := m.Play([]Item{dsd}, 0)
	if !errors.Is(err, ErrNothingPlayable) {
		t.Fatalf("err = %v, want ErrNothingPlayable", err)
	}
	// The reasons still come back, so the caller can say why.
	if len(skipped) != 1 {
		t.Errorf("skipped = %+v, want the refused track reported", skipped)
	}
	if dev.loadCount() != 0 {
		t.Error("loaded a track the policy refused")
	}
}

func TestPlayStartIndexIsTranslatedOntoTheFilteredQueue(t *testing.T) {
	tests := []struct {
		name       string
		startIndex int
		wantTitle  string
	}{
		// Tracks 2 and 3 of the caller's list are DSD.
		{"first track", 0, "one"},
		{"an unplayable track starts at the next playable one", 1, "four"},
		{"the second unplayable track also moves forward", 2, "four"},
		{"a playable track after the gap", 3, "four"},
		{"last track", 4, "five"},
		// Out-of-range input must not panic or load nothing.
		{"index past the end falls back to the first playable", 99, "one"},
		{"negative index", -5, "one"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, dev := newTestManager(t)

			items := []Item{flacItem(1, "one"), flacItem(2, "two"), flacItem(3, "three"), flacItem(4, "four"), flacItem(5, "five")}
			items[1].URI = "USB/Album/02.dsf"
			items[2].URI = "USB/Album/03.dsf"

			if _, err := m.Play(items, tt.startIndex); err != nil {
				t.Fatalf("Play: %v", err)
			}

			media, ok := dev.lastLoad()
			if !ok {
				t.Fatal("nothing loaded")
			}
			if got := m.State().Title; got != tt.wantTitle {
				t.Errorf("playing %q, want %q (url %q)", got, tt.wantTitle, media.URL)
			}
		})
	}
}

func TestNextAndPreviousWalkTheQueue(t *testing.T) {
	m, dev := newTestManager(t)
	items := []Item{flacItem(1, "one"), flacItem(2, "two"), flacItem(3, "three")}
	if _, err := m.Play(items, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}

	if err := m.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got := m.State().Title; got != "two" {
		t.Errorf("after Next = %q, want two", got)
	}

	if err := m.Previous(); err != nil {
		t.Fatalf("Previous: %v", err)
	}
	if got := m.State().Title; got != "one" {
		t.Errorf("after Previous = %q, want one", got)
	}

	// Walking off either end is a no-op, not an error: a transport button
	// that reports a failure at the end of an album is wrong.
	if err := m.Previous(); err != nil {
		t.Errorf("Previous at the start: %v", err)
	}
	if got := m.State().QueueIndex; got != 0 {
		t.Errorf("QueueIndex = %d, want 0", got)
	}

	loadsBefore := dev.loadCount()
	for i := 0; i < 5; i++ {
		if err := m.Next(); err != nil {
			t.Fatalf("Next: %v", err)
		}
	}
	if got := m.State().QueueIndex; got != 2 {
		t.Errorf("QueueIndex = %d, want 2 (the last track)", got)
	}
	// Two real advances, then three no-ops.
	if got := dev.loadCount() - loadsBefore; got != 2 {
		t.Errorf("%d loads past the end of the queue, want 2", got)
	}
}

func TestATrackPlayingOutAdvancesTheQueue(t *testing.T) {
	m, dev := newTestManager(t)
	if _, err := m.Play([]Item{flacItem(1, "one"), flacItem(2, "two")}, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}

	// Only FINISHED advances: CANCELLED is our own Stop and INTERRUPTED is the
	// LOAD of the next track, so treating either as an advance skips tracks.
	dev.setStatus(infracast.Status{PlayerState: "IDLE", IdleReason: "FINISHED"})
	m.onDeviceStatus(m.generation)

	if got := m.State().Title; got != "two" {
		t.Errorf("playing %q, want two", got)
	}
}

func TestACancelledTrackDoesNotAdvanceTheQueue(t *testing.T) {
	m, dev := newTestManager(t)
	if _, err := m.Play([]Item{flacItem(1, "one"), flacItem(2, "two")}, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}

	for _, reason := range []string{"CANCELLED", "INTERRUPTED", "ERROR"} {
		t.Run(reason, func(t *testing.T) {
			dev.setStatus(infracast.Status{PlayerState: "IDLE", IdleReason: reason})
			m.onDeviceStatus(m.generation)

			if got := m.State().QueueIndex; got != 0 {
				t.Errorf("%s advanced the queue to %d", reason, got)
			}
		})
	}
}

func TestStateReflectsTheDeviceNotTheLastCommand(t *testing.T) {
	// Another phone in the same room pressing pause arrives as a status frame.
	// A sender that trusts its own last command drifts the first time that
	// happens.
	m, dev := newTestManager(t)
	if _, err := m.Play([]Item{flacItem(1, "So What")}, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if got := m.State().Status; got != "play" {
		t.Fatalf("Status = %q, want play", got)
	}

	dev.setStatus(infracast.Status{HasMedia: true, PlayerState: "PAUSED", CurrentTime: 42.5, Volume: 0.25})
	if got := m.State(); got.Status != "pause" || got.Seek != 42500 || got.Volume != 25 {
		t.Errorf("state = %+v", got)
	}
}

func TestStatusWord(t *testing.T) {
	tests := []struct {
		name   string
		status infracast.Status
		want   string
	}{
		{"playing", infracast.Status{PlayerState: "PLAYING", HasMedia: true}, "play"},
		// Buffering reads as play: it is what the user asked for and what is
		// about to happen, and showing "stopped" mid-buffer flickers the UI on
		// every track change.
		{"buffering", infracast.Status{PlayerState: "BUFFERING", HasMedia: true}, "play"},
		{"paused", infracast.Status{PlayerState: "PAUSED", HasMedia: true}, "pause"},
		// Pre-roll: the measured window between LOAD and the first buffer is
		// three frames on a Nest Hub. Reporting "stop" there sent the UI back
		// to stopped straight after every play and every track change.
		{"pre-roll", infracast.Status{PlayerState: "IDLE", HasMedia: true}, "play"},
		{"finished", infracast.Status{PlayerState: "IDLE", IdleReason: "FINISHED"}, "stop"},
		{"cancelled", infracast.Status{PlayerState: "IDLE", IdleReason: "CANCELLED"}, "stop"},
		// No session at all — the receiver app is up but nothing is loaded.
		{"idle with no media", infracast.Status{PlayerState: "IDLE"}, "stop"},
		{"empty", infracast.Status{}, "stop"},
		{"unknown state", infracast.Status{PlayerState: "SOMETHING_NEW", HasMedia: true}, "stop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statusWord(tt.status); got != tt.want {
				t.Errorf("statusWord(%+v) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestStateIsInactiveWithNoSession(t *testing.T) {
	m := NewManager(func() string { return testBase })
	s := m.State()
	if s.Active || s.Status != "stop" {
		t.Errorf("state = %+v, want inactive and stopped", s)
	}
	if m.Active() {
		t.Error("Active() true with no session")
	}
}

func TestDeviceDurationWinsOverTheLibraryTag(t *testing.T) {
	// The device's duration comes from its decoder; ours comes from a tag.
	m, dev := newTestManager(t)
	item := flacItem(1, "So What")
	item.DurationSec = 300
	if _, err := m.Play([]Item{item}, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}

	dev.setStatus(infracast.Status{HasMedia: true, PlayerState: "PLAYING", Duration: 332.36})
	if got := m.State().Duration; got != 332 {
		t.Errorf("Duration = %d, want the device's 332", got)
	}
}

func TestSetVolumeScalesToTheDeviceRange(t *testing.T) {
	tests := []struct {
		percent int
		want    float64
	}{
		{0, 0}, {50, 0.5}, {100, 1},
	}
	for _, tt := range tests {
		m, dev := newTestManager(t)
		if err := m.SetVolume(tt.percent); err != nil {
			t.Fatalf("SetVolume: %v", err)
		}
		if got := dev.Status().Volume; got != tt.want {
			t.Errorf("SetVolume(%d) sent %v, want %v", tt.percent, got, tt.want)
		}
	}
}

func TestTogglePauseFollowsTheDevicesState(t *testing.T) {
	m, dev := newTestManager(t)
	if _, err := m.Play([]Item{flacItem(1, "one")}, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}

	if err := m.TogglePause(); err != nil {
		t.Fatalf("TogglePause: %v", err)
	}
	if got := dev.Status().PlayerState; got != "PAUSED" {
		t.Errorf("first toggle left %q, want PAUSED", got)
	}

	if err := m.TogglePause(); err != nil {
		t.Fatalf("TogglePause: %v", err)
	}
	if got := dev.Status().PlayerState; got != "PLAYING" {
		t.Errorf("second toggle left %q, want PLAYING", got)
	}
}

func TestStopCastingQuitsTheAppAndEndsTheSession(t *testing.T) {
	m, dev := newTestManager(t)
	if _, err := m.Play([]Item{flacItem(1, "one")}, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}

	if err := m.StopCasting(); err != nil {
		t.Fatalf("StopCasting: %v", err)
	}

	dev.mu.Lock()
	calls := strings.Join(dev.calls, ",")
	dev.mu.Unlock()
	if !strings.Contains(calls, "QuitApp") {
		t.Errorf("calls = %q, want a QuitApp", calls)
	}
	if m.Active() {
		t.Error("session still active after StopCasting")
	}
	if got := m.State().Active; got {
		t.Error("state still reports an active session")
	}
}

func TestDisconnectLeavesTheDevicePlaying(t *testing.T) {
	// A backend restart must not silence a device somebody is listening to,
	// so disconnecting is not an instruction to stop.
	m, dev := newTestManager(t)
	if _, err := m.Play([]Item{flacItem(1, "one")}, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}

	m.Disconnect()

	dev.mu.Lock()
	calls := strings.Join(dev.calls, ",")
	dev.mu.Unlock()
	if strings.Contains(calls, "QuitApp") || strings.Contains(calls, "StopMedia") {
		t.Errorf("Disconnect stopped playback: %q", calls)
	}
	if m.Active() {
		t.Error("session still active after Disconnect")
	}
}

func TestConnectReplacesAnExistingSession(t *testing.T) {
	first := newStubDevice()
	second := newStubDevice()

	round := 0
	m := &Manager{
		discoverer: infracast.NewDiscoverer(),
		baseURL:    func() string { return testBase },
		dial: func(string, func(infracast.Status)) (deviceClient, error) {
			round++
			if round == 1 {
				return first, nil
			}
			return second, nil
		},
	}
	t.Cleanup(m.Disconnect)

	if err := m.connectTo(infracast.Device{ID: "a", Name: "Hub"}); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	if err := m.connectTo(infracast.Device{ID: "b", Name: "Kitchen"}); err != nil {
		t.Fatalf("second connect: %v", err)
	}

	// The first connection must be closed, not left open alongside the second.
	select {
	case <-first.Closed():
	case <-time.After(time.Second):
		t.Error("the replaced session was left open")
	}
	if got := m.State().DeviceName; got != "Kitchen" {
		t.Errorf("DeviceName = %q, want Kitchen", got)
	}
}

func TestLosingTheDeviceEndsTheSession(t *testing.T) {
	// A client must not be left holding transport controls for an unplugged
	// speaker.
	m, dev := newTestManager(t)
	if _, err := m.Play([]Item{flacItem(1, "one")}, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}

	states := make(chan State, 8)
	m.OnState(func(s State) { states <- s })

	dev.err = errors.New("device stopped answering heartbeats")
	close(dev.closed)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case s := <-states:
			if !s.Active {
				if m.Active() {
					t.Error("Active() still true after the device went away")
				}
				return
			}
		case <-deadline:
			t.Fatal("no inactive state published after the device went away")
		}
	}
}

func TestAStaleStatusFrameCannotResurrectAReplacedSession(t *testing.T) {
	// A late frame from a connection that has been replaced must not write
	// over the current session's state.
	m, dev := newTestManager(t)
	if _, err := m.Play([]Item{flacItem(1, "one"), flacItem(2, "two")}, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}
	staleGen := m.generation

	m.Disconnect()

	dev.setStatus(infracast.Status{PlayerState: "IDLE", IdleReason: "FINISHED"})
	m.onDeviceStatus(staleGen)

	if m.Active() {
		t.Error("a stale frame reopened the session")
	}
}

func TestLoadWithoutABackendAddressFails(t *testing.T) {
	// Stream URLs are composed from the backend's own address. Without one
	// there is nothing to hand the device, and loading anyway would produce a
	// device trying to fetch from an empty host.
	dev := newStubDevice()
	m := &Manager{
		discoverer: infracast.NewDiscoverer(),
		baseURL:    func() string { return "" },
		dial: func(string, func(infracast.Status)) (deviceClient, error) {
			return dev, nil
		},
	}
	if err := m.connectTo(infracast.Device{ID: "a", Name: "Hub"}); err != nil {
		t.Fatalf("connectTo: %v", err)
	}
	t.Cleanup(m.Disconnect)

	if _, err := m.Play([]Item{flacItem(1, "one")}, 0); err == nil {
		t.Fatal("Play succeeded with no backend address")
	}
	if dev.loadCount() != 0 {
		t.Error("a track was loaded with no stream URL")
	}
}

func TestLoadFailureLeavesNoPhantomSession(t *testing.T) {
	m, dev := newTestManager(t)
	dev.loadErr = errors.New("LOAD_FAILED (UNSUPPORTED_MEDIA)")

	if _, err := m.Play([]Item{flacItem(1, "one")}, 0); err == nil {
		t.Fatal("Play hid a LOAD failure")
	}
	if got := m.State().Status; got == "play" {
		t.Error("state reports playing after a failed load")
	}
}

func TestStateDuringALoadReportsTheTrackTheUserAsked(t *testing.T) {
	// A Nest Hub answers a track change by first reporting the OUTGOING
	// track's terminal IDLE frame, which clears the media snapshot. Measured
	// live, that window is ~2s and three status frames — long enough for the
	// transport UI to show "stopped" on every single track change.
	m, dev := newTestManager(t)
	if _, err := m.Play([]Item{flacItem(1, "one"), flacItem(2, "two")}, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}

	// Reproduce the mid-load frame: the device has dropped the old session
	// and has not yet reported the new one.
	m.setLoading(true)
	dev.setStatus(infracast.Status{PlayerState: "IDLE", IdleReason: "INTERRUPTED", CurrentTime: 180})

	got := m.State()
	if got.Status != "play" {
		t.Errorf("Status = %q mid-load, want play", got.Status)
	}
	// The device's position belongs to the track being left behind.
	if got.Seek != 0 {
		t.Errorf("Seek = %d mid-load, want 0", got.Seek)
	}

	m.setLoading(false)
	if got := m.State().Status; got != "stop" {
		t.Errorf("Status = %q once the load window closed, want stop", got)
	}
}

func TestTheLoadWindowClosesEvenWhenTheLoadFails(t *testing.T) {
	// Otherwise a device that refuses the media would be reported as playing
	// for the rest of the session.
	m, dev := newTestManager(t)
	dev.loadErr = errors.New("LOAD_FAILED (UNSUPPORTED_MEDIA)")

	if _, err := m.Play([]Item{flacItem(1, "one")}, 0); err == nil {
		t.Fatal("Play hid a LOAD failure")
	}

	m.mu.Lock()
	loading := m.loading
	m.mu.Unlock()
	if loading {
		t.Error("the load window was left open after a failed LOAD")
	}
}
