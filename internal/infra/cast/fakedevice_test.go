package cast

import (
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeDevice speaks enough of the Cast protocol over an in-memory pipe to drive
// the client through a real exchange: framing, the connection handshake,
// heartbeats, LAUNCH, LOAD and the transport commands.
//
// It exists because the alternative — asserting on marshalled payloads — would
// not catch the mistakes that actually bite here: addressing the media
// namespace before CONNECTing to the app transport, or correlating a reply to
// the wrong requestId.
type fakeDevice struct {
	t *testing.T

	net net.Conn

	mu       sync.Mutex
	received []frame
	// appID is the app the device reports as running; empty means idle.
	appID       string
	sessionID   string
	transportID string
	mediaID     int
	playerState string
	volume      float64
	// loadVerdict decides how the device answers a LOAD: "", "LOAD_FAILED",
	// or "empty" for the acknowledged-but-no-session case a declined format
	// produces.
	loadVerdict string
	connectedTo map[string]bool
	closed      bool
	loadedMedia mediaInfo
	idleReason  string
}

// newFakeDevice returns the device and a client already connected to it.
func newFakeDevice(t *testing.T, opts ...func(*fakeDevice)) (*fakeDevice, *Client) {
	t.Helper()

	devSide, clientSide := net.Pipe()
	d := &fakeDevice{
		t:           t,
		net:         devSide,
		transportID: "transport-7",
		volume:      0.4,
		connectedTo: map[string]bool{},
	}
	for _, opt := range opts {
		opt(d)
	}
	go d.serve()

	client, err := Dial("fake:8009", WithDialer(func(string) (net.Conn, error) {
		return clientSide, nil
	}))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		d.mu.Lock()
		d.closed = true
		d.mu.Unlock()
		_ = devSide.Close()
	})
	return d, client
}

func (d *fakeDevice) serve() {
	for {
		f, err := readFrame(d.net)
		if err != nil {
			return
		}

		d.mu.Lock()
		if d.closed {
			d.mu.Unlock()
			return
		}
		d.received = append(d.received, f)
		d.mu.Unlock()

		d.handle(f)
	}
}

func (d *fakeDevice) handle(f frame) {
	var env envelope
	if err := json.Unmarshal([]byte(f.Payload), &env); err != nil {
		return
	}

	switch f.Namespace {
	case nsConnection:
		d.mu.Lock()
		if env.Type == typeConnect {
			d.connectedTo[f.DestinationID] = true
		}
		d.mu.Unlock()
		return

	case nsHeartbeat:
		if env.Type == typePing {
			d.reply(f.SourceID, nsHeartbeat, envelope{Type: typePong})
		}
		return

	case nsReceiver:
		switch env.Type {
		case typeGetStatus:
			d.sendReceiverStatus(f.SourceID, env.RequestID)
		case typeLaunch:
			d.mu.Lock()
			d.appID = DefaultMediaReceiverAppID
			d.sessionID = "session-1"
			d.mu.Unlock()
			d.sendReceiverStatus(f.SourceID, env.RequestID)
		case typeSetVolume:
			var p setVolumePayload
			_ = json.Unmarshal([]byte(f.Payload), &p)
			d.mu.Lock()
			if p.Volume.Level != nil {
				d.volume = *p.Volume.Level
			}
			d.mu.Unlock()
			d.sendReceiverStatus(f.SourceID, env.RequestID)
		case typeStop:
			d.mu.Lock()
			d.appID, d.sessionID, d.mediaID, d.playerState = "", "", 0, ""
			d.mu.Unlock()
			d.sendReceiverStatus(f.SourceID, env.RequestID)
		}
		return

	case nsMedia:
		// A real receiver ignores the media namespace until the sender has
		// CONNECTed to the app transport. Reproducing that is the point of
		// this fake.
		d.mu.Lock()
		connected := d.connectedTo[d.transportID]
		d.mu.Unlock()
		if !connected {
			d.t.Errorf("media command %q sent before CONNECT to %s", env.Type, d.transportID)
			return
		}

		switch env.Type {
		case typeLoad:
			d.handleLoad(f, env)
		case typePlay:
			d.setStateAndReply(f.SourceID, env.RequestID, "PLAYING")
		case typePause:
			d.setStateAndReply(f.SourceID, env.RequestID, "PAUSED")
		case typeStop:
			// Measured against a Nest Hub on firmware 3.80.546557: STOP keeps
			// the mediaSessionId and reports IDLE/CANCELLED rather than
			// dropping to an empty status array.
			d.mu.Lock()
			d.playerState = "IDLE"
			d.idleReason = "CANCELLED"
			d.mu.Unlock()
			d.sendMediaStatus(f.SourceID, env.RequestID, 0)
		case typeSeek:
			var p seekPayload
			_ = json.Unmarshal([]byte(f.Payload), &p)
			d.sendMediaStatus(f.SourceID, env.RequestID, p.CurrentTime)
		case typeGetStatus:
			d.sendMediaStatus(f.SourceID, env.RequestID, 0)
		}
	}
}

func (d *fakeDevice) handleLoad(f frame, env envelope) {
	d.mu.Lock()
	verdict := d.loadVerdict
	d.mu.Unlock()

	switch verdict {
	case "LOAD_FAILED":
		d.replyWithID(f.SourceID, nsMedia, env.RequestID, errorPayload{
			Type:   typeLoadFailed,
			Reason: "UNSUPPORTED_MEDIA",
		})
		return
	case "empty":
		d.replyWithID(f.SourceID, nsMedia, env.RequestID, mediaStatusPayload{Type: typeMediaState})
		return
	}

	var p loadPayload
	_ = json.Unmarshal([]byte(f.Payload), &p)

	d.mu.Lock()
	d.mediaID = 42
	d.playerState = "PLAYING"
	d.loadedMedia = p.Media
	d.mu.Unlock()

	d.sendMediaStatus(f.SourceID, env.RequestID, 0)
}

func (d *fakeDevice) setStateAndReply(dest string, requestID int, state string) {
	d.mu.Lock()
	d.playerState = state
	d.idleReason = ""
	d.mu.Unlock()
	d.sendMediaStatus(dest, requestID, 0)
}

func (d *fakeDevice) sendReceiverStatus(dest string, requestID int) {
	d.mu.Lock()
	var p receiverStatusPayload
	p.Type = typeReceiverState
	p.Status.Volume.Level = d.volume
	if d.appID != "" {
		p.Status.Applications = append(p.Status.Applications, struct {
			AppID        string `json:"appId"`
			DisplayName  string `json:"displayName"`
			SessionID    string `json:"sessionId"`
			StatusText   string `json:"statusText"`
			TransportID  string `json:"transportId"`
			IsIdleScreen bool   `json:"isIdleScreen"`
		}{
			AppID:       d.appID,
			DisplayName: "Default Media Receiver",
			SessionID:   d.sessionID,
			TransportID: d.transportID,
		})
	}
	d.mu.Unlock()

	d.replyWithID(dest, nsReceiver, requestID, p)
}

func (d *fakeDevice) sendMediaStatus(dest string, requestID int, currentTime float64) {
	d.mu.Lock()
	var p mediaStatusPayload
	p.Type = typeMediaState
	if d.mediaID != 0 {
		p.Status = []mediaStatus{{
			MediaSessionID: d.mediaID,
			PlayerState:    d.playerState,
			CurrentTime:    currentTime,
			IdleReason:     d.idleReason,
			Media:          d.loadedMedia,
		}}
	}
	d.mu.Unlock()

	d.replyWithID(dest, nsMedia, requestID, p)
}

func (d *fakeDevice) reply(dest, namespace string, payload any) {
	d.replyWithID(dest, namespace, 0, payload)
}

// replyWithID writes a payload with requestID spliced in, which is how a real
// receiver correlates a reply.
func (d *fakeDevice) replyWithID(dest, namespace string, requestID int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		d.t.Errorf("fakeDevice marshal: %v", err)
		return
	}
	if requestID != 0 {
		var generic map[string]any
		if err := json.Unmarshal(body, &generic); err == nil {
			generic["requestId"] = requestID
			if remarshalled, err := json.Marshal(generic); err == nil {
				body = remarshalled
			}
		}
	}

	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return
	}

	if _, err := d.net.Write(encodeFrame(frame{
		SourceID:      dest,
		DestinationID: defaultSenderID,
		Namespace:     namespace,
		Payload:       string(body),
	})); err != nil {
		return
	}
}

// waitForConnect blocks until the device has recorded a CONNECT to dest.
//
// net.Pipe's Write returns once the bytes are consumed, which is before the
// device goroutine has appended the frame to its log — so an assertion about a
// fire-and-forget send has to poll rather than read immediately.
func (d *fakeDevice) waitForConnect(dest string) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		ok := d.connectedTo[dest]
		d.mu.Unlock()
		if ok {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// sent returns every frame the device received on a namespace.
func (d *fakeDevice) sent(namespace string) []frame {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []frame
	for _, f := range d.received {
		if f.Namespace == namespace {
			out = append(out, f)
		}
	}
	return out
}

// lastLoad returns the media block of the most recent LOAD the device received.
func (d *fakeDevice) lastLoad() (loadPayload, bool) {
	for _, f := range d.sent(nsMedia) {
		var env envelope
		if err := json.Unmarshal([]byte(f.Payload), &env); err != nil || env.Type != typeLoad {
			continue
		}
		var p loadPayload
		if err := json.Unmarshal([]byte(f.Payload), &p); err == nil {
			return p, true
		}
	}
	return loadPayload{}, false
}
