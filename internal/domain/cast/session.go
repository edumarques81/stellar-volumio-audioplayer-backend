package cast

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/filestream"
	infracast "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cast"
)

// Errors a caller is expected to surface to the user.
var (
	// ErrNoDevice means the requested device is not in the discovery cache.
	ErrNoDevice = errors.New("cast: no such device")
	// ErrNotCasting means a transport command arrived with no session open.
	ErrNotCasting = errors.New("cast: no session is active")
	// ErrNothingPlayable means every track handed to Play was refused by the
	// format policy.
	ErrNothingPlayable = errors.New("cast: none of those tracks can be cast")
)

// discoveryTimeout bounds one mDNS browse. Cast devices answer in well under a
// second; the rest of the window is for a device that is slow to wake.
const discoveryTimeout = 3 * time.Second

// Item is one resolved library track, ready to cast.
//
// The caller builds these — this package deliberately knows nothing about the
// library cache, the same separation internal/domain/upnp keeps.
type Item struct {
	URI         string // library-relative path, e.g. "USB/Kind of Blue/01.flac"
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	TrackNumber int
	DiscNumber  int
	DurationSec int
	SampleRate  int    // Hz; 0 when the library has not measured it
	ArtPath     string // the library's own /albumart?path=… value, already escaped
}

// State is the snapshot published to clients.
//
// Status uses the same play/pause/stop vocabulary as the MPD-backed player
// state, so a client can render a cast session with the controls it already
// has. That is the take-over model: a cast session replaces the transport view
// rather than appearing as a second zone.
type State struct {
	Active      bool   `json:"active"`
	DeviceID    string `json:"deviceId,omitempty"`
	DeviceName  string `json:"deviceName,omitempty"`
	Status      string `json:"status"`
	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	AlbumArt    string `json:"albumart,omitempty"`
	URI         string `json:"uri,omitempty"`
	Seek        int    `json:"seek"`     // milliseconds, matching pushState
	Duration    int    `json:"duration"` // seconds, matching pushState
	Volume      int    `json:"volume"`   // 0..100
	Mute        bool   `json:"mute"`
	QueueIndex  int    `json:"queueIndex"`
	QueueLength int    `json:"queueLength"`
}

// Manager owns the single Cast session the appliance maintains.
//
// One session at a time is deliberate: the appliance has one transport UI, and
// the take-over model means a second simultaneous session would have nowhere to
// be displayed. Speaker groups cover the multiroom case without needing two.
type Manager struct {
	discoverer *infracast.Discoverer

	// baseURL supplies the origin a Cast device should fetch from. It is a
	// function because the backend's own address is only known after startup.
	baseURL func() string

	// dial is the seam tests replace. It returns the interface rather than
	// the concrete client so the session's queue and state logic can be tested
	// without a protocol exchange — the protocol itself is covered in
	// internal/infra/cast against a fake receiver and a real Nest Hub.
	dial func(addr string, onStatus func(infracast.Status)) (deviceClient, error)

	mu     sync.Mutex
	client deviceClient
	device infracast.Device
	queue  []Item
	index  int
	// generation invalidates the watcher of a session that has been replaced,
	// so a late status frame from an old connection cannot resurrect it.
	generation int

	onState func(State)
}

// deviceClient is the slice of the Cast protocol client a session needs.
// *infracast.Client satisfies it.
type deviceClient interface {
	Status() infracast.Status
	Load(infracast.Media) error
	Play() error
	Pause() error
	StopMedia() error
	Seek(seconds float64) error
	SetVolume(level float64) error
	SetMuted(muted bool) error
	QuitApp() error
	Close() error
	Closed() <-chan struct{}
	Err() error
}

// NewManager returns a Manager that discovers devices over mDNS.
func NewManager(baseURL func() string) *Manager {
	return &Manager{
		discoverer: infracast.NewDiscoverer(),
		baseURL:    baseURL,
		dial: func(addr string, onStatus func(infracast.Status)) (deviceClient, error) {
			return infracast.Dial(addr, infracast.WithStatusHandler(onStatus))
		},
	}
}

var _ deviceClient = (*infracast.Client)(nil)

// OnState registers the broadcast callback. It must be set before any session
// is opened and is called from the Cast read goroutine, so it must not block.
func (m *Manager) OnState(f func(State)) {
	m.mu.Lock()
	m.onState = f
	m.mu.Unlock()
}

// Devices returns the cached device list.
func (m *Manager) Devices() []infracast.Device { return m.discoverer.Devices() }

// RefreshDevices browses the network and returns what answered.
func (m *Manager) RefreshDevices(ctx context.Context) ([]infracast.Device, error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	return m.discoverer.Refresh(ctx)
}

// Active reports whether a session is open.
func (m *Manager) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.client != nil
}

// Connect opens a session to a device, replacing any session already open.
//
// Discovery is retried once when the device is not in the cache: a client may
// know a device id from a previous run, and making it browse first would turn
// every reconnect into a two-step dance.
func (m *Manager) Connect(ctx context.Context, deviceID string) error {
	device, ok := m.discoverer.Lookup(deviceID)
	if !ok {
		if _, err := m.RefreshDevices(ctx); err != nil {
			log.Debug().Err(err).Msg("cast: refreshing devices for an unknown id")
		}
		device, ok = m.discoverer.Lookup(deviceID)
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoDevice, deviceID)
	}
	return m.connectTo(device)
}

func (m *Manager) connectTo(device infracast.Device) error {
	// Close an existing session before opening the next, so two connections
	// are never live at once.
	m.Disconnect()

	m.mu.Lock()
	m.generation++
	gen := m.generation
	m.mu.Unlock()

	client, err := m.dial(device.Addr, func(infracast.Status) {
		m.onDeviceStatus(gen)
	})
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.client = client
	m.device = device
	m.queue = nil
	m.index = 0
	m.mu.Unlock()

	go m.watch(gen, client)

	m.broadcast()
	log.Info().Str("device", device.Name).Str("addr", device.Addr).Msg("Cast session opened")
	return nil
}

// Disconnect ends the session, leaving the device's receiver app running.
//
// Stopping playback is a separate decision — StopCasting is the one that puts
// the device back on its idle screen — so that a backend restart does not
// silence a device somebody is listening to.
func (m *Manager) Disconnect() {
	m.mu.Lock()
	client := m.client
	m.client = nil
	m.device = infracast.Device{}
	m.queue = nil
	m.index = 0
	m.generation++
	m.mu.Unlock()

	if client == nil {
		return
	}
	if err := client.Close(); err != nil {
		log.Debug().Err(err).Msg("cast: closing a session")
	}
	m.broadcast()
}

// StopCasting stops playback, returns the device to its idle screen and ends
// the session. This is what "stop casting" means to a person.
func (m *Manager) StopCasting() error {
	client, err := m.active()
	if err != nil {
		return err
	}
	if err := client.QuitApp(); err != nil {
		log.Debug().Err(err).Msg("cast: quitting the receiver app")
	}
	m.Disconnect()
	return nil
}

// Play casts a list of tracks, starting at startIndex.
//
// Tracks the format policy refuses are dropped and reported in the returned
// skip list rather than silently omitted, so a caller can tell the user that
// the DSD half of a mixed album is not coming.
func (m *Manager) Play(items []Item, startIndex int) ([]Skipped, error) {
	client, err := m.active()
	if err != nil {
		return nil, err
	}

	playable := make([]Item, 0, len(items))
	var skipped []Skipped
	// startIndex refers to the caller's list, so it has to be translated onto
	// the filtered one — otherwise asking for track 5 of an album whose first
	// four are DSD starts in the wrong place. When the chosen track is itself
	// unplayable the session starts at the next playable one: handing back an
	// earlier track than the one tapped is more confusing than skipping
	// forward.
	start := -1
	for i, item := range items {
		verdict := Classify(item.URI, item.SampleRate)
		if !verdict.Playable() {
			skipped = append(skipped, Skipped{Item: item, Reason: verdict.Reason})
			continue
		}
		if start == -1 && i >= startIndex {
			start = len(playable)
		}
		playable = append(playable, item)
	}
	if start == -1 {
		// Every playable track precedes the chosen one.
		start = 0
	}

	if len(playable) == 0 {
		return skipped, ErrNothingPlayable
	}

	m.mu.Lock()
	m.queue = playable
	m.index = start
	item := playable[start]
	m.mu.Unlock()

	if err := m.load(client, item); err != nil {
		return skipped, err
	}
	m.broadcast()
	return skipped, nil
}

// Skipped records one track the policy refused, with a reason fit to show.
type Skipped struct {
	Item   Item
	Reason string
}

func (m *Manager) load(client deviceClient, item Item) error {
	base := ""
	if m.baseURL != nil {
		base = m.baseURL()
	}
	if base == "" {
		return errors.New("cast: the backend address is unknown, so no stream URL can be built")
	}

	verdict := Classify(item.URI, item.SampleRate)
	if !verdict.Playable() {
		return fmt.Errorf("cast: %s: %s", item.Title, verdict.Reason)
	}

	return client.Load(infracast.Media{
		URL:         filestream.StreamURL(base, item.URI),
		ContentType: verdict.ContentType,
		Duration:    float64(item.DurationSec),
		Metadata:    metadataFor(item, base),
	})
}

// metadataFor builds the MusicTrackMediaMetadata the receiver renders. The
// generic metadata type drops album name, album artist and track number, which
// is most of what makes a cast album look like an album.
func metadataFor(item Item, base string) *infracast.Metadata {
	md := &infracast.Metadata{
		MetadataType: infracast.MetadataTypeMusicTrack,
		Title:        item.Title,
		AlbumName:    item.Album,
		Artist:       item.Artist,
		AlbumArtist:  item.AlbumArtist,
		TrackNumber:  item.TrackNumber,
		DiscNumber:   item.DiscNumber,
	}
	if art := filestream.AssetURL(base, item.ArtPath); art != "" {
		md.Images = []infracast.Image{{URL: art}}
	}
	return md
}

// Resume, Pause, TogglePause, Stop, Seek and SetVolume are the transport
// surface. Each is a thin pass-through: the device remains the authority on its
// own state and the resulting status frame is what updates clients.

func (m *Manager) Resume() error {
	client, err := m.active()
	if err != nil {
		return err
	}
	if err := client.Play(); err != nil {
		return err
	}
	m.broadcast()
	return nil
}

func (m *Manager) Pause() error {
	client, err := m.active()
	if err != nil {
		return err
	}
	if err := client.Pause(); err != nil {
		return err
	}
	m.broadcast()
	return nil
}

// TogglePause flips play and pause based on what the device reports, not on
// what we last sent.
func (m *Manager) TogglePause() error {
	client, err := m.active()
	if err != nil {
		return err
	}
	if client.Status().Playing() {
		return m.Pause()
	}
	return m.Resume()
}

// Stop ends playback but keeps the session, so the next Play does not have to
// relaunch the receiver app.
func (m *Manager) Stop() error {
	client, err := m.active()
	if err != nil {
		return err
	}
	if err := client.StopMedia(); err != nil {
		return err
	}
	m.broadcast()
	return nil
}

// Seek moves to an absolute position in seconds.
func (m *Manager) Seek(seconds int) error {
	client, err := m.active()
	if err != nil {
		return err
	}
	if err := client.Seek(float64(seconds)); err != nil {
		return err
	}
	m.broadcast()
	return nil
}

// SetVolume sets the Cast device's volume on a 0..100 scale.
//
// This is the remote device's own volume. Nothing here touches the Singxer or
// MPD — the DAC path stays at mixer_type "none".
func (m *Manager) SetVolume(percent int) error {
	client, err := m.active()
	if err != nil {
		return err
	}
	if err := client.SetVolume(float64(percent) / 100); err != nil {
		return err
	}
	m.broadcast()
	return nil
}

// SetMuted mutes the Cast device without changing its volume level.
func (m *Manager) SetMuted(muted bool) error {
	client, err := m.active()
	if err != nil {
		return err
	}
	if err := client.SetMuted(muted); err != nil {
		return err
	}
	m.broadcast()
	return nil
}

// Next advances the queue. It reports ErrNotCasting when nothing is loaded and
// simply stops at the end rather than erroring, which is what a transport
// button should do.
func (m *Manager) Next() error { return m.step(1) }

// Previous goes back one track.
func (m *Manager) Previous() error { return m.step(-1) }

func (m *Manager) step(delta int) error {
	client, err := m.active()
	if err != nil {
		return err
	}

	m.mu.Lock()
	next := m.index + delta
	if next < 0 || next >= len(m.queue) {
		m.mu.Unlock()
		m.broadcast()
		return nil
	}
	m.index = next
	item := m.queue[next]
	m.mu.Unlock()

	if err := m.load(client, item); err != nil {
		return err
	}
	m.broadcast()
	return nil
}

// State returns the snapshot a client should render.
func (m *Manager) State() State {
	m.mu.Lock()
	client := m.client
	device := m.device
	queueLen := len(m.queue)
	index := m.index
	var current Item
	if index >= 0 && index < queueLen {
		current = m.queue[index]
	}
	base := ""
	if m.baseURL != nil {
		base = m.baseURL()
	}
	m.mu.Unlock()

	if client == nil {
		return State{Status: "stop"}
	}

	s := client.Status()
	state := State{
		Active:      true,
		DeviceID:    device.ID,
		DeviceName:  device.Name,
		Status:      statusWord(s),
		Volume:      int(s.Volume*100 + 0.5),
		Mute:        s.Muted,
		Seek:        int(s.CurrentTime * 1000),
		QueueIndex:  index,
		QueueLength: queueLen,
	}

	// Track identity comes from our own queue rather than the device: the
	// receiver echoes back only what we sent it, and the library already has
	// the authoritative tags.
	if current.URI != "" {
		state.Title = current.Title
		state.Artist = current.Artist
		state.Album = current.Album
		state.URI = current.URI
		state.AlbumArt = filestream.AssetURL(base, current.ArtPath)
		state.Duration = current.DurationSec
	}
	// The device's own duration is better than the library's when it has one:
	// it comes from the decoder rather than a tag.
	if s.Duration > 0 {
		state.Duration = int(s.Duration)
	}
	return state
}

// statusWord maps the Cast player states onto the play/pause/stop vocabulary
// the rest of the system uses. BUFFERING reads as play because that is what the
// user asked for and what is about to happen; showing "stopped" mid-buffer
// makes the UI flicker on every track change.
func statusWord(s infracast.Status) string {
	switch s.PlayerState {
	case "PLAYING", "BUFFERING":
		return "play"
	case "PAUSED":
		return "pause"
	default:
		return "stop"
	}
}

// active returns the live client or an error.
func (m *Manager) active() (deviceClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client == nil {
		return nil, ErrNotCasting
	}
	return m.client, nil
}

// onDeviceStatus handles an unsolicited status frame: it advances the queue
// when a track plays out, and republishes state either way.
func (m *Manager) onDeviceStatus(gen int) {
	m.mu.Lock()
	stale := gen != m.generation
	client := m.client
	m.mu.Unlock()
	if stale || client == nil {
		return
	}

	// A track that played to the end is the signal to advance. Only FINISHED
	// counts: CANCELLED is our own Stop, and INTERRUPTED is the LOAD of the
	// next track, so treating either as an advance would skip tracks.
	if client.Status().IdleFinished() {
		if err := m.step(1); err != nil {
			log.Debug().Err(err).Msg("cast: advancing the queue")
		}
		return
	}
	m.broadcast()
}

// watch tears the session down when the device goes away, so a client is not
// left holding transport controls for an unplugged speaker.
func (m *Manager) watch(gen int, client deviceClient) {
	<-client.Closed()

	m.mu.Lock()
	stale := gen != m.generation
	if !stale {
		m.client = nil
		m.device = infracast.Device{}
		m.queue = nil
		m.index = 0
	}
	m.mu.Unlock()

	if stale {
		return
	}
	if err := client.Err(); err != nil {
		log.Warn().Err(err).Msg("Cast session lost")
	} else {
		log.Info().Msg("Cast session closed")
	}
	m.broadcast()
}

func (m *Manager) broadcast() {
	m.mu.Lock()
	f := m.onState
	m.mu.Unlock()
	if f == nil {
		return
	}
	f(m.State())
}
