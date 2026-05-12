package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// forwarder satisfies spectrum.SocketEmitter (BroadcastToAll) by POSTing
// each frame to the Mac backend's /internal/spectrum endpoint. Frames go
// through a bounded channel so a slow / unreachable backend can never
// block the FFT compute goroutine — the worst it can do is drop old
// frames.
//
// Retry policy mirrors the MPD idle watcher at internal/infra/mpd/client.go:
// exponential backoff 500ms → 10s on transient failures, reset to initial
// on first success. ctx cancel is honoured between attempts and inside
// the sleep step.
type forwarder struct {
	cfg forwarderConfig

	queue   chan []byte
	dropped atomic.Int64

	wg     sync.WaitGroup
	cancel context.CancelFunc

	startOnce sync.Once
}

type forwarderConfig struct {
	url    string
	key    string
	client *http.Client

	initialBackoff time.Duration
	maxBackoff     time.Duration

	queueSize int

	// sleep is exposed so tests can capture the backoff sequence without
	// actually waiting. nil → real time.Sleep.
	sleep func(time.Duration)
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
		cfg.sleep = time.Sleep
	}
	return &forwarder{
		cfg:   cfg,
		queue: make(chan []byte, cfg.queueSize),
	}
}

// BroadcastToAll satisfies spectrum.SocketEmitter. Drops the frame if the
// queue is full — this is intentional; a backed-up downstream means the
// Mac is slow or unreachable and the freshest frame is more valuable than
// a stale backlog.
func (f *forwarder) BroadcastToAll(event string, data interface{}) {
	if event != "pushSpectrum" {
		// We only forward pushSpectrum; anything else is a programming
		// bug. Log once at debug level so it surfaces in dev.
		log.Debug().Str("event", event).Msg("forwarder ignoring non-pushSpectrum event")
		return
	}

	body, err := json.Marshal(data)
	if err != nil {
		log.Warn().Err(err).Msg("forwarder: marshal failed")
		return
	}

	select {
	case f.queue <- body:
	default:
		f.dropped.Add(1)
	}
}

// start launches the worker goroutine. Safe to call multiple times; only
// the first call has effect.
func (f *forwarder) start(ctx context.Context) {
	f.startOnce.Do(func() {
		ctx, f.cancel = context.WithCancel(ctx)
		f.wg.Add(1)
		go f.runLoop(ctx)
	})
}

// wait blocks until the worker goroutine has exited.
func (f *forwarder) wait() {
	f.wg.Wait()
}

func (f *forwarder) runLoop(ctx context.Context) {
	defer f.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case body := <-f.queue:
			f.deliverWithRetry(ctx, body)
		}
	}
}

// deliverWithRetry attempts the POST repeatedly until it succeeds or ctx
// is cancelled. Backoff doubles each failure, capped at maxBackoff.
func (f *forwarder) deliverWithRetry(ctx context.Context, body []byte) {
	backoff := f.cfg.initialBackoff
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		err := f.postOnce(ctx, body)
		if err == nil {
			return
		}

		log.Debug().Err(err).Dur("delay", backoff).Msg("spectrum forward failed; retrying")
		f.cfg.sleep(backoff)
		backoff = nextBackoff(backoff, f.cfg.maxBackoff)
	}
}

func (f *forwarder) postOnce(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.cfg.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
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

// nextBackoff doubles current, capped at max. Same shape as
// internal/infra/mpd/client.go's nextBackoff helper.
func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

// flushForTest drains the queue synchronously by invoking the run loop
// once per queued frame. Intended for test use only — the production
// path runs the loop in a goroutine via start().
func (f *forwarder) flushForTest(ctx context.Context) {
	for {
		select {
		case body := <-f.queue:
			f.deliverWithRetry(ctx, body)
		default:
			return
		}
	}
}

// droppedFrames returns the dropped-frame counter. Used at shutdown to
// surface backlog statistics and by tests to assert drop behaviour.
func (f *forwarder) droppedFrames() int64 {
	return f.dropped.Load()
}

// droppedForTest is a test-only alias retained so the existing
// queue-full assertion keeps reading as "for-test" intent.
func (f *forwarder) droppedForTest() int64 {
	return f.droppedFrames()
}
