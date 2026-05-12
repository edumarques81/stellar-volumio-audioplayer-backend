package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRoundTripper lets a test script a deterministic sequence of HTTP
// responses without actually hitting the network. Each Roundtrip pops one
// outcome from the front; once the script is exhausted, returns the last
// outcome forever (so the steady-state can be either "always 204" or
// "always fail").
type fakeRoundTripper struct {
	mu       sync.Mutex
	script   []roundTripOutcome
	calls    atomic.Int32
	lastAuth string
}

type roundTripOutcome struct {
	status int
	err    error
}

func (f *fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastAuth = r.Header.Get("Authorization")

	var outcome roundTripOutcome
	if len(f.script) > 0 {
		outcome = f.script[0]
		if len(f.script) > 1 {
			f.script = f.script[1:]
		}
	}
	if outcome.err != nil {
		return nil, outcome.err
	}
	return &http.Response{
		StatusCode: outcome.status,
		Body:       http.NoBody,
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func TestForwarderSendsBearerToken(t *testing.T) {
	rt := &fakeRoundTripper{script: []roundTripOutcome{{status: http.StatusNoContent}}}
	f := newForwarder(forwarderConfig{
		url:    "http://mac.local:3000/internal/spectrum",
		key:    "secret-token-xyz",
		client: &http.Client{Transport: rt},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	f.BroadcastToAll("pushSpectrum", map[string]interface{}{"binsL": []float64{0.1}})
	// drain the queue
	f.flushForTest(ctx)

	if got, want := rt.calls.Load(), int32(1); got != want {
		t.Fatalf("expected %d calls, got %d", want, got)
	}
	if rt.lastAuth != "Bearer secret-token-xyz" {
		t.Errorf("expected bearer header, got %q", rt.lastAuth)
	}
}

func TestForwarderRetriesWithBoundedBackoff(t *testing.T) {
	// First three attempts fail, fourth succeeds. We capture the backoff
	// sequence the forwarder computes — it must double each step and cap
	// at maxBackoff. We use a tiny initial+max so the test stays fast.
	rt := &fakeRoundTripper{script: []roundTripOutcome{
		{err: errors.New("dial: connection refused")},
		{err: errors.New("dial: connection refused")},
		{err: errors.New("dial: connection refused")},
		{status: http.StatusNoContent},
	}}

	var observed []time.Duration
	f := newForwarder(forwarderConfig{
		url:            "http://mac.local:3000/internal/spectrum",
		key:            "k",
		client:         &http.Client{Transport: rt},
		initialBackoff: 1 * time.Millisecond,
		maxBackoff:     8 * time.Millisecond,
		sleep: func(d time.Duration) {
			observed = append(observed, d)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	f.BroadcastToAll("pushSpectrum", map[string]interface{}{})
	f.flushForTest(ctx)

	if got := rt.calls.Load(); got != 4 {
		t.Fatalf("expected 4 attempts, got %d", got)
	}
	if len(observed) != 3 {
		t.Fatalf("expected 3 sleeps between 4 attempts, got %d (%v)", len(observed), observed)
	}
	// Backoff schedule: 1ms, 2ms, 4ms (next would be 8ms cap)
	expected := []time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}
	for i, want := range expected {
		if observed[i] != want {
			t.Errorf("sleep[%d] = %v, want %v", i, observed[i], want)
		}
	}
}

func TestForwarderBackoffCapsAtMax(t *testing.T) {
	// Five failures, then success — backoff should cap at maxBackoff.
	rt := &fakeRoundTripper{script: []roundTripOutcome{
		{err: errors.New("x")},
		{err: errors.New("x")},
		{err: errors.New("x")},
		{err: errors.New("x")},
		{err: errors.New("x")},
		{status: http.StatusNoContent},
	}}

	var observed []time.Duration
	f := newForwarder(forwarderConfig{
		url:            "http://mac.local:3000/internal/spectrum",
		key:            "k",
		client:         &http.Client{Transport: rt},
		initialBackoff: 1 * time.Millisecond,
		maxBackoff:     4 * time.Millisecond,
		sleep: func(d time.Duration) {
			observed = append(observed, d)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	f.BroadcastToAll("pushSpectrum", map[string]interface{}{})
	f.flushForTest(ctx)

	// Expect 5 sleeps; backoff: 1, 2, 4, 4, 4
	expected := []time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond, 4 * time.Millisecond, 4 * time.Millisecond}
	if len(observed) != len(expected) {
		t.Fatalf("expected %d sleeps, got %d (%v)", len(expected), len(observed), observed)
	}
	for i, want := range expected {
		if observed[i] != want {
			t.Errorf("sleep[%d] = %v, want %v", i, observed[i], want)
		}
	}
}

func TestForwarderDropsFrameOnCtxCancel(t *testing.T) {
	// The transport fails forever; cancelling the ctx must unblock the
	// retry loop and drop the in-flight frame without panicking.
	rt := &fakeRoundTripper{script: []roundTripOutcome{
		{err: errors.New("permanent")},
	}}

	sleepCalled := make(chan struct{}, 16)
	f := newForwarder(forwarderConfig{
		url:            "http://mac.local:3000/internal/spectrum",
		key:            "k",
		client:         &http.Client{Transport: rt},
		initialBackoff: 50 * time.Millisecond,
		maxBackoff:     50 * time.Millisecond,
		sleep: func(d time.Duration) {
			select {
			case sleepCalled <- struct{}{}:
			default:
			}
			time.Sleep(d)
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// wait until at least one retry sleep happens, then cancel
		<-sleepCalled
		cancel()
	}()

	f.start(ctx)
	f.BroadcastToAll("pushSpectrum", map[string]interface{}{})

	done := make(chan struct{})
	go func() {
		f.wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("forwarder did not exit within 3s of ctx cancel")
	}
}

func TestForwarderDropsOnQueueFull(t *testing.T) {
	// Block the worker on a hanging transport so the queue can fill up,
	// then push more frames than fit; old frames must drop without panic.
	gate := make(chan struct{})
	rt := &hangingRoundTripper{gate: gate}

	f := newForwarder(forwarderConfig{
		url:       "http://mac.local:3000/internal/spectrum",
		key:       "k",
		client:    &http.Client{Transport: rt},
		queueSize: 2,
		sleep:     func(d time.Duration) {},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.start(ctx)
	defer func() {
		close(gate) // release the worker before wait
		cancel()
		f.wait()
	}()

	// Pump 20 frames; queue=2 means at most 2 should be retained, the
	// rest dropped silently. The test only asserts we don't panic and
	// that the drop counter increments.
	for i := 0; i < 20; i++ {
		f.BroadcastToAll("pushSpectrum", map[string]interface{}{"i": i})
	}

	// Give the queue a moment to settle.
	time.Sleep(20 * time.Millisecond)

	if dropped := f.droppedForTest(); dropped == 0 {
		t.Errorf("expected drops > 0 with queueSize=2 and 20 pushes, got %d", dropped)
	}
}

// hangingRoundTripper blocks every RoundTrip until gate is closed, then
// returns an error so the retry loop can churn.
type hangingRoundTripper struct {
	gate chan struct{}
}

func (h *hangingRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	<-h.gate
	return nil, errors.New("gate closed")
}
