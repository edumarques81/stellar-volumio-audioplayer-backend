package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/fifo"
)

// forwarder POSTs airplay state + heartbeat payloads to the Mac backend.
// Frames go through a bounded channel so a slow Mac can never block the
// pipe-read goroutine — the worst it can do is drop old state updates.
//
// The retry policy mirrors cmd/stellar-spectrum's forwarder: exponential
// backoff 500ms → 10s, reset on first success. Context cancel is honoured
// between attempts and inside sleeps.
type forwarder struct {
	cfg forwarderConfig

	queue   chan postOp
	dropped atomic.Int64

	wg     sync.WaitGroup
	cancel context.CancelFunc

	startOnce sync.Once
}

type postOp struct {
	path string
	body []byte
}

type forwarderConfig struct {
	// baseURL is the Mac endpoint prefix (e.g. http://mac:3000/internal/airplay).
	// The forwarder appends /state or /heartbeat as required.
	baseURL string
	key     string
	client  *http.Client

	initialBackoff time.Duration
	maxBackoff     time.Duration

	queueSize int

	// sleep paces retry backoff. It must honour cancellation — a plain
	// time.Sleep here would hold shutdown for up to maxBackoff — and
	// returns false when ctx was cancelled before the delay elapsed.
	sleep func(context.Context, time.Duration) bool
}

func newForwarder(cfg forwarderConfig) *forwarder {
	if cfg.client == nil {
		cfg.client = &http.Client{Timeout: 2 * time.Second}
	}
	if cfg.initialBackoff <= 0 {
		cfg.initialBackoff = 500 * time.Millisecond
	}
	if cfg.maxBackoff <= 0 {
		cfg.maxBackoff = 10 * time.Second
	}
	if cfg.queueSize <= 0 {
		cfg.queueSize = 4
	}
	if cfg.sleep == nil {
		cfg.sleep = fifo.Sleep
	}
	cfg.baseURL = strings.TrimRight(cfg.baseURL, "/")
	return &forwarder{
		cfg:   cfg,
		queue: make(chan postOp, cfg.queueSize),
	}
}

// PostState enqueues a state payload for delivery to /state. Drops the
// payload when the queue is full (caller intentionally ignores back-
// pressure — the freshest state is always more valuable than a backlog).
func (f *forwarder) PostState(payload map[string]interface{}) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Warn().Err(err).Msg("forwarder: state marshal failed")
		return
	}
	f.enqueue(postOp{path: "/state", body: body})
}

// PostHeartbeat enqueues a heartbeat ping (body-less) for delivery to
// /heartbeat.
func (f *forwarder) PostHeartbeat() {
	f.enqueue(postOp{path: "/heartbeat", body: nil})
}

func (f *forwarder) enqueue(op postOp) {
	select {
	case f.queue <- op:
	default:
		f.dropped.Add(1)
	}
}

func (f *forwarder) start(ctx context.Context) {
	f.startOnce.Do(func() {
		ctx, f.cancel = context.WithCancel(ctx)
		f.wg.Add(1)
		go f.runLoop(ctx)
	})
}

func (f *forwarder) wait() { f.wg.Wait() }

func (f *forwarder) runLoop(ctx context.Context) {
	defer f.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case op := <-f.queue:
			f.deliverWithRetry(ctx, op)
		}
	}
}

func (f *forwarder) deliverWithRetry(ctx context.Context, op postOp) {
	backoff := f.cfg.initialBackoff
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		err := f.postOnce(ctx, op)
		if err == nil {
			return
		}
		log.Debug().Err(err).Str("path", op.path).Dur("delay", backoff).Msg("airplay forward failed; retrying")
		if !f.cfg.sleep(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff, f.cfg.maxBackoff)
	}
}

func (f *forwarder) postOnce(ctx context.Context, op postOp) error {
	url := f.cfg.baseURL + op.path
	var bodyReader io.Reader
	if len(op.body) > 0 {
		bodyReader = bytes.NewReader(op.body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bodyReader)
	if err != nil {
		return err
	}
	if len(op.body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+f.cfg.key)

	resp, err := f.cfg.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("unexpected status %d", resp.StatusCode)
}

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

func (f *forwarder) flushForTest(ctx context.Context) {
	for {
		select {
		case op := <-f.queue:
			f.deliverWithRetry(ctx, op)
		default:
			return
		}
	}
}

func (f *forwarder) droppedFrames() int64 { return f.dropped.Load() }
