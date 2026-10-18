package cast

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

// ErrNoMediaSession means a transport command arrived with nothing loaded. The
// caller has to LOAD before it can PLAY.
var ErrNoMediaSession = errors.New("cast: no media session on the device")

// ErrNoApp means the media namespace was addressed before an app was launched.
var ErrNoApp = errors.New("cast: no receiver app running")

// Status is a flattened snapshot of what the device reports about itself.
//
// It is assembled from RECEIVER_STATUS and MEDIA_STATUS frames as they arrive,
// never from a cached echo of what we asked for: the receiver is the authority
// on its own state, and a sender that trusts its own last command drifts the
// first time a phone in the same room presses pause.
type Status struct {
	// Receiver half.
	AppID        string
	DisplayName  string
	SessionID    string
	StatusText   string
	TransportID  string
	IsIdleScreen bool
	Volume       float64
	Muted        bool

	// Media half. HasMedia is false once there is nothing loaded to act on —
	// see isTerminalIdle for why that is not simply "the status array was
	// empty".
	HasMedia       bool
	MediaSessionID int
	PlayerState    string
	CurrentTime    float64
	Duration       float64
	IdleReason     string
	ContentID      string
}

// Playing reports whether audio is actually coming out of the device.
func (s Status) Playing() bool { return s.PlayerState == "PLAYING" }

// Media is one item to hand the receiver. The receiver fetches URL itself over
// HTTP — Cast is a pull protocol — so URL must be reachable from the device,
// not from us.
type Media struct {
	URL         string
	ContentType string
	Duration    float64
	Metadata    *Metadata
}

// Option configures a Client at dial time.
type Option func(*Client)

// WithDialer replaces the TLS dialer. Tests use it to drive a real protocol
// exchange over an in-memory pipe.
func WithDialer(d dialer) Option {
	return func(c *Client) { c.dial = d }
}

// WithStatusHandler registers a callback fired on every status change the
// device reports. It runs on the read goroutine, so it must not block.
func WithStatusHandler(f func(Status)) Option {
	return func(c *Client) { c.onStatus = f }
}

// Client is a Cast sender bound to one device.
type Client struct {
	addr string
	dial dialer

	conn *conn

	mu     sync.RWMutex
	status Status

	onStatus func(Status)
}

// Dial opens a Cast channel to addr ("host:port", conventionally port 8009),
// completes the connection handshake and reads the device's initial status.
func Dial(addr string, opts ...Option) (*Client, error) {
	c := &Client{addr: addr, dial: dialTLS}
	for _, opt := range opts {
		opt(c)
	}

	nc, err := c.dial(addr)
	if err != nil {
		return nil, fmt.Errorf("cast: dialling %s: %w", addr, err)
	}
	c.conn = newConn(nc, c.handleFrame)

	// The platform receiver ignores everything until a CONNECT arrives on the
	// connection namespace.
	if err := c.conn.send(platformReceiverID, nsConnection, connectPayload{
		Type:   typeConnect,
		Origin: map[string]any{},
	}); err != nil {
		_ = c.conn.Close()
		return nil, err
	}

	if err := c.RefreshReceiverStatus(); err != nil {
		_ = c.conn.Close()
		return nil, err
	}
	return c, nil
}

// Addr is the device address this client is bound to.
func (c *Client) Addr() string { return c.addr }

// Closed fires when the channel to the device is gone.
func (c *Client) Closed() <-chan struct{} { return c.conn.Closed() }

// Err reports why the channel closed, if it closed abnormally.
func (c *Client) Err() error { return c.conn.Err() }

// Close sends a tidy CLOSE and tears the channel down. It deliberately does
// not stop playback: a sender disconnecting is not an instruction to the
// device to go quiet, and treating it as one would stop the music every time
// the backend restarts.
func (c *Client) Close() error {
	if err := c.conn.send(platformReceiverID, nsConnection, envelope{Type: typeClose}); err != nil {
		log.Debug().Err(err).Msg("cast: sending CLOSE")
	}
	return c.conn.Close()
}

// Status returns the latest snapshot.
func (c *Client) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// RefreshReceiverStatus asks the platform receiver what app is running and at
// what volume.
func (c *Client) RefreshReceiverStatus() error {
	id := c.conn.nextRequestID()
	f, err := c.conn.request(platformReceiverID, nsReceiver, id, requestPayload{
		Type:      typeGetStatus,
		RequestID: id,
	})
	if err != nil {
		return err
	}
	c.applyFrame(f)
	return nil
}

// RefreshMediaStatus asks the running app about the current media session.
func (c *Client) RefreshMediaStatus() error {
	transport, _, err := c.session()
	if err != nil {
		return err
	}
	id := c.conn.nextRequestID()
	f, err := c.conn.request(transport, nsMedia, id, requestPayload{
		Type:      typeGetStatus,
		RequestID: id,
	})
	if err != nil {
		return err
	}
	c.applyFrame(f)
	return nil
}

// LaunchDefaultMediaReceiver starts Google's stock media player, unless it is
// already running. It then CONNECTs to the app's transport, which the media
// namespace cannot be addressed without.
func (c *Client) LaunchDefaultMediaReceiver() error {
	if c.Status().AppID == DefaultMediaReceiverAppID {
		return c.connectToTransport()
	}

	id := c.conn.nextRequestID()
	f, err := c.conn.request(platformReceiverID, nsReceiver, id, launchPayload{
		Type:      typeLaunch,
		RequestID: id,
		AppID:     DefaultMediaReceiverAppID,
	})
	if err != nil {
		return err
	}
	c.applyFrame(f)

	if got := c.Status().AppID; got != DefaultMediaReceiverAppID {
		return fmt.Errorf("cast: LAUNCH left appId %q running", got)
	}
	return c.connectToTransport()
}

func (c *Client) connectToTransport() error {
	transport := c.Status().TransportID
	if transport == "" {
		return ErrNoApp
	}
	return c.conn.send(transport, nsConnection, connectPayload{
		Type:   typeConnect,
		Origin: map[string]any{},
	})
}

// Load hands the receiver a URL to fetch and play.
//
// The reply is the first MEDIA_STATUS of the new session, which is also where
// the mediaSessionId every later transport command needs comes from. A
// LOAD_FAILED or LOAD_CANCELLED comes back on the same requestId and is
// surfaced as an error rather than leaving the caller waiting.
func (c *Client) Load(m Media) error {
	if err := c.LaunchDefaultMediaReceiver(); err != nil {
		return err
	}
	transport := c.Status().TransportID
	if transport == "" {
		return ErrNoApp
	}

	id := c.conn.nextRequestID()
	f, err := c.conn.request(transport, nsMedia, id, loadPayload{
		Type:        typeLoad,
		RequestID:   id,
		Autoplay:    true,
		CurrentTime: 0,
		Media: mediaInfo{
			ContentID:   m.URL,
			ContentType: m.ContentType,
			StreamType:  "BUFFERED",
			Duration:    m.Duration,
			Metadata:    m.Metadata,
		},
	})
	if err != nil {
		return err
	}

	var env envelope
	if err := json.Unmarshal([]byte(f.Payload), &env); err == nil {
		switch env.Type {
		case typeLoadFailed, typeLoadCancelled, typeInvalidReq:
			var perr errorPayload
			_ = json.Unmarshal([]byte(f.Payload), &perr)
			if perr.Reason != "" {
				return fmt.Errorf("cast: %s (%s)", env.Type, perr.Reason)
			}
			return fmt.Errorf("cast: %s", env.Type)
		}
	}

	c.applyFrame(f)
	if !c.Status().HasMedia {
		// The device acknowledged the LOAD without opening a session. In
		// practice this is an unsupported format the receiver declined
		// without a LOAD_FAILED.
		return fmt.Errorf("cast: device accepted LOAD but reported no media session")
	}
	return nil
}

// Play resumes a paused session.
func (c *Client) Play() error { return c.mediaCommand(typePlay) }

// Pause pauses the session.
func (c *Client) Pause() error { return c.mediaCommand(typePause) }

// StopMedia ends the media session, leaving the receiver app running.
func (c *Client) StopMedia() error { return c.mediaCommand(typeStop) }

func (c *Client) mediaCommand(cmdType string) error {
	transport, session, err := c.session()
	if err != nil {
		return err
	}
	id := c.conn.nextRequestID()
	f, err := c.conn.request(transport, nsMedia, id, mediaRequestPayload{
		Type:           cmdType,
		RequestID:      id,
		MediaSessionID: session,
	})
	if err != nil {
		return err
	}
	c.applyFrame(f)
	return nil
}

// Seek jumps to an absolute offset in seconds.
func (c *Client) Seek(seconds float64) error {
	transport, session, err := c.session()
	if err != nil {
		return err
	}
	if seconds < 0 {
		seconds = 0
	}
	id := c.conn.nextRequestID()
	f, err := c.conn.request(transport, nsMedia, id, seekPayload{
		Type:           typeSeek,
		RequestID:      id,
		MediaSessionID: session,
		CurrentTime:    seconds,
	})
	if err != nil {
		return err
	}
	c.applyFrame(f)
	return nil
}

// SetVolume sets the device's own volume, clamped to the 0..1 the receiver
// accepts. This is the Cast device's volume, not the Singxer's — nothing here
// touches the DAC path.
func (c *Client) SetVolume(level float64) error {
	switch {
	case level < 0:
		level = 0
	case level > 1:
		level = 1
	}
	id := c.conn.nextRequestID()
	f, err := c.conn.request(platformReceiverID, nsReceiver, id, setVolumePayload{
		Type:      typeSetVolume,
		RequestID: id,
		Volume:    volumeTarget{Level: &level},
	})
	if err != nil {
		return err
	}
	c.applyFrame(f)
	return nil
}

// SetMuted mutes or unmutes the device without disturbing its volume level.
func (c *Client) SetMuted(muted bool) error {
	id := c.conn.nextRequestID()
	f, err := c.conn.request(platformReceiverID, nsReceiver, id, setVolumePayload{
		Type:      typeSetVolume,
		RequestID: id,
		Volume:    volumeTarget{Muted: &muted},
	})
	if err != nil {
		return err
	}
	c.applyFrame(f)
	return nil
}

// QuitApp stops the receiver app entirely, returning the device to its idle
// screen. This is what "stop casting" means to a user.
func (c *Client) QuitApp() error {
	session := c.Status().SessionID
	if session == "" {
		return nil // nothing running; already where the caller wants to be
	}
	id := c.conn.nextRequestID()
	f, err := c.conn.request(platformReceiverID, nsReceiver, id, stopSessionPayload{
		Type:      typeStop,
		RequestID: id,
		SessionID: session,
	})
	if err != nil {
		return err
	}
	c.applyFrame(f)
	return nil
}

// session returns the transport and media session ids a media command needs.
func (c *Client) session() (string, int, error) {
	s := c.Status()
	if s.TransportID == "" {
		return "", 0, ErrNoApp
	}
	if !s.HasMedia || s.MediaSessionID == 0 {
		return "", 0, ErrNoMediaSession
	}
	return s.TransportID, s.MediaSessionID, nil
}

// handleFrame receives the unsolicited frames: the status broadcasts the device
// sends whenever anything changes, including changes made by other senders.
func (c *Client) handleFrame(f frame) {
	if c.applyFrame(f) && c.onStatus != nil {
		c.onStatus(c.Status())
	}
}

// applyFrame folds a status frame into the snapshot and reports whether
// anything changed.
func (c *Client) applyFrame(f frame) bool {
	var env envelope
	if err := json.Unmarshal([]byte(f.Payload), &env); err != nil {
		return false
	}

	c.mu.Lock()
	before := c.status
	switch env.Type {
	case typeReceiverState:
		c.applyReceiverStatus(f.Payload)
	case typeMediaState:
		c.applyMediaStatus(f.Payload)
	}
	changed := c.status != before
	c.mu.Unlock()

	return changed
}

// applyReceiverStatus must be called with the lock held.
func (c *Client) applyReceiverStatus(payload string) {
	var p receiverStatusPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return
	}

	c.status.Volume = p.Status.Volume.Level
	c.status.Muted = p.Status.Volume.Muted

	if len(p.Status.Applications) == 0 {
		// No app running. Clear the app half and the media half with it —
		// a media session cannot outlive the app that owned it.
		c.status.AppID = ""
		c.status.DisplayName = ""
		c.status.SessionID = ""
		c.status.StatusText = ""
		c.status.TransportID = ""
		c.status.IsIdleScreen = false
		c.clearMedia()
		return
	}

	app := p.Status.Applications[0]
	c.status.AppID = app.AppID
	c.status.DisplayName = app.DisplayName
	c.status.SessionID = app.SessionID
	c.status.StatusText = app.StatusText
	c.status.TransportID = app.TransportID
	c.status.IsIdleScreen = app.IsIdleScreen
}

// applyMediaStatus must be called with the lock held.
func (c *Client) applyMediaStatus(payload string) {
	var p mediaStatusPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return
	}

	// An empty status array is the device saying "there is no media session".
	// Keeping the previous media here is the documented way to end up showing
	// a track that stopped playing minutes ago.
	if len(p.Status) == 0 {
		c.clearMedia()
		return
	}

	s := p.Status[0]

	// A real Nest Hub does not drop the session on STOP: it answers with the
	// same mediaSessionId, playerState IDLE and an idleReason of CANCELLED.
	// Measured against firmware 3.80.546557 — trusting the empty-array case
	// alone leaves HasMedia true for a session that is over, and the next
	// transport command then goes to a session the device has discarded.
	//
	// IDLE *without* a reason is the opposite case: it is the pre-roll state
	// between LOAD and the first buffer, where the session is real and about
	// to start.
	if isTerminalIdle(s.PlayerState, s.IdleReason) {
		c.clearMedia()
		c.status.PlayerState = s.PlayerState
		c.status.IdleReason = s.IdleReason
		return
	}

	c.status.HasMedia = true
	c.status.MediaSessionID = s.MediaSessionID
	c.status.PlayerState = s.PlayerState
	c.status.CurrentTime = s.CurrentTime
	c.status.IdleReason = s.IdleReason

	// A MEDIA_STATUS sent for a transport change carries no media block, so
	// only overwrite what the frame actually reports.
	if s.Media.ContentID != "" {
		c.status.ContentID = s.Media.ContentID
	}
	if s.Media.Duration != 0 {
		c.status.Duration = s.Media.Duration
	}
}

// isTerminalIdle reports whether a status frame describes a media session that
// is over. The Cast receiver defines exactly four idle reasons and all of them
// end the current item: FINISHED (played out), CANCELLED (stopped),
// INTERRUPTED (replaced by another LOAD) and ERROR.
func isTerminalIdle(playerState, idleReason string) bool {
	return playerState == "IDLE" && idleReason != ""
}

// IdleFinished reports that the last session ended because the track played to
// the end, as opposed to being stopped or replaced. It is what a queue would
// advance on.
func (s Status) IdleFinished() bool {
	return s.PlayerState == "IDLE" && s.IdleReason == "FINISHED"
}

func (c *Client) clearMedia() {
	c.status.HasMedia = false
	c.status.MediaSessionID = 0
	c.status.PlayerState = ""
	c.status.CurrentTime = 0
	c.status.Duration = 0
	c.status.IdleReason = ""
	c.status.ContentID = ""
}

// compile-time assertion that the production dialer fits the seam the tests
// substitute.
var _ dialer = dialTLS
