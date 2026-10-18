package cast

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// ErrConnClosed is returned by every operation once the connection is gone.
var ErrConnClosed = errors.New("cast: connection closed")

// ErrTimeout means the receiver never answered a request we expected a reply
// to. The device is still connected; the request is what failed.
var ErrTimeout = errors.New("cast: timed out waiting for a reply")

// requestTimeout bounds a single request/response exchange. A LAUNCH on a cold
// device is the slowest thing we send and comes back well inside this.
const requestTimeout = 10 * time.Second

// heartbeatInterval is the sender-side PING cadence. Google's own senders use
// 5s; the receiver drops a connection it has not heard from in roughly 10s.
const heartbeatInterval = 5 * time.Second

// missedHeartbeatLimit is how many unanswered PINGs we tolerate before
// declaring the device gone. Three at 5s means a dead device is noticed in
// about fifteen seconds rather than hanging a cast session indefinitely.
const missedHeartbeatLimit = 3

// dialer opens a transport to a Cast device. It is a field rather than a direct
// tls.Dial call so tests can drive the protocol over a pipe.
type dialer func(addr string) (net.Conn, error)

// dialTLS is the production dialer.
//
// InsecureSkipVerify is not a shortcut: Cast devices present a certificate
// chained to Google's own device CA with a CN that is neither the IP nor the
// mDNS hostname we reach them on, so no hostname verification can succeed. The
// protocol's own security model assumes a trusted LAN, which is the same
// assumption /stream and the UPnP endpoints already make.
func dialTLS(addr string) (net.Conn, error) {
	return tls.DialWithDialer(
		&net.Dialer{Timeout: 5 * time.Second},
		"tcp", addr,
		&tls.Config{InsecureSkipVerify: true}, //nolint:gosec // see above
	)
}

// conn is one framed, authenticated Cast channel to a device.
//
// It owns a single read goroutine. Replies to requests we sent are routed back
// to the caller by requestId; everything else — the receiver's own PINGs and
// the unsolicited status broadcasts that are the only reliable source of
// playback state — goes to onFrame.
type conn struct {
	net net.Conn

	writeMu sync.Mutex
	nextReq atomic.Int64

	pendingMu sync.Mutex
	pending   map[int]chan frame

	onFrame func(frame)

	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error

	missedPings atomic.Int32
}

func newConn(nc net.Conn, onFrame func(frame)) *conn {
	c := &conn{
		net:     nc,
		pending: make(map[int]chan frame),
		onFrame: onFrame,
		closed:  make(chan struct{}),
	}
	go c.readLoop()
	go c.heartbeatLoop()
	return c
}

// Closed returns a channel closed when the connection dies, for whatever
// reason — a close from our side, a dropped TCP connection, or a missed
// heartbeat.
func (c *conn) Closed() <-chan struct{} { return c.closed }

// Err reports why the connection closed. It is only meaningful after Closed()
// has fired.
func (c *conn) Err() error {
	select {
	case <-c.closed:
		return c.closeErr
	default:
		return nil
	}
}

func (c *conn) Close() error {
	c.shutdown(nil)
	return nil
}

func (c *conn) shutdown(cause error) {
	c.closeOnce.Do(func() {
		c.closeErr = cause
		close(c.closed)
		if err := c.net.Close(); err != nil {
			log.Debug().Err(err).Msg("cast: closing the device connection")
		}

		// Unblock everyone waiting on a reply that will never arrive.
		c.pendingMu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.pendingMu.Unlock()
	})
}

// send writes one frame. It takes no reply.
func (c *conn) send(destination, namespace string, payload any) error {
	select {
	case <-c.closed:
		return ErrConnClosed
	default:
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cast: marshalling a %s payload: %w", namespace, err)
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.net.Write(encodeFrame(frame{
		SourceID:      defaultSenderID,
		DestinationID: destination,
		Namespace:     namespace,
		Payload:       string(body),
	})); err != nil {
		c.shutdown(err)
		return fmt.Errorf("cast: writing a %s frame: %w", namespace, err)
	}
	return nil
}

// nextRequestID allocates a requestId.
//
// Cast treats requestId 0 as "no reply expected", so the counter starts at 1.
func (c *conn) nextRequestID() int {
	return int(c.nextReq.Add(1))
}

// request sends a payload and waits for the frame carrying the same requestId.
//
// setRequestID is called with the allocated id so the caller can stamp it into
// its own payload struct — the field name and position differ between payloads,
// so the conn cannot do it generically.
func (c *conn) request(destination, namespace string, id int, payload any) (frame, error) {
	reply := make(chan frame, 1)

	c.pendingMu.Lock()
	c.pending[id] = reply
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	if err := c.send(destination, namespace, payload); err != nil {
		return frame{}, err
	}

	select {
	case f, ok := <-reply:
		if !ok {
			return frame{}, ErrConnClosed
		}
		return f, nil
	case <-time.After(requestTimeout):
		return frame{}, fmt.Errorf("%w: %s request %d", ErrTimeout, namespace, id)
	case <-c.closed:
		return frame{}, ErrConnClosed
	}
}

func (c *conn) readLoop() {
	for {
		f, err := readFrame(c.net)
		if err != nil {
			if errors.Is(err, io.EOF) {
				c.shutdown(nil)
			} else {
				c.shutdown(err)
			}
			return
		}
		c.dispatch(f)
	}
}

// dispatch routes one inbound frame.
//
// Heartbeats are answered here rather than handed upward: a PONG is a protocol
// obligation, not an event, and routing it through the caller would make a slow
// consumer look like a dead device.
func (c *conn) dispatch(f frame) {
	var env envelope
	if err := json.Unmarshal([]byte(f.Payload), &env); err != nil {
		log.Debug().Str("namespace", f.Namespace).Msg("cast: unparseable payload, ignored")
		return
	}

	if f.Namespace == nsHeartbeat {
		switch env.Type {
		case typePing:
			if err := c.send(platformReceiverID, nsHeartbeat, envelope{Type: typePong}); err != nil {
				log.Debug().Err(err).Msg("cast: answering a device PING")
			}
		case typePong:
			c.missedPings.Store(0)
		}
		return
	}

	if env.RequestID != 0 {
		c.pendingMu.Lock()
		reply, waiting := c.pending[env.RequestID]
		if waiting {
			delete(c.pending, env.RequestID)
		}
		c.pendingMu.Unlock()

		if waiting {
			reply <- f
			return
		}
		// A reply whose caller already timed out. Fall through: a
		// RECEIVER_STATUS or MEDIA_STATUS is still worth observing.
	}

	if c.onFrame != nil {
		c.onFrame(f)
	}
}

// heartbeatLoop keeps the channel alive and notices a device that has stopped
// answering. Cast gives us no other liveness signal: a Nest Hub that loses
// power leaves the TCP connection open from our side.
func (c *conn) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.closed:
			return
		case <-ticker.C:
			if c.missedPings.Load() >= missedHeartbeatLimit {
				c.shutdown(fmt.Errorf("cast: device stopped answering heartbeats"))
				return
			}
			c.missedPings.Add(1)
			if err := c.send(platformReceiverID, nsHeartbeat, envelope{Type: typePing}); err != nil {
				return // send already shut the connection down
			}
		}
	}
}
