package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRT struct {
	mu     sync.Mutex
	calls  atomic.Int32
	last   *http.Request
	script []rtOutcome
}

type rtOutcome struct {
	status int
	err    error
}

func (f *fakeRT) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.last = r
	var out rtOutcome
	if len(f.script) > 0 {
		out = f.script[0]
		if len(f.script) > 1 {
			f.script = f.script[1:]
		}
	} else {
		out = rtOutcome{status: http.StatusNoContent}
	}
	f.mu.Unlock()
	if out.err != nil {
		return nil, out.err
	}
	return &http.Response{
		StatusCode: out.status,
		Body:       http.NoBody,
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func TestForwarderPostsStateWithBearerAndJSON(t *testing.T) {
	rt := &fakeRT{}
	f := newForwarder(forwarderConfig{
		baseURL: "http://mac:3000/internal/airplay",
		key:     "secret",
		client:  &http.Client{Transport: rt},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	f.PostState(map[string]interface{}{"title": "x"})
	f.flushForTest(ctx)

	if rt.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", rt.calls.Load())
	}
	if !strings.HasSuffix(rt.last.URL.Path, "/internal/airplay/state") {
		t.Errorf("URL path = %q", rt.last.URL.Path)
	}
	if rt.last.Header.Get("Authorization") != "Bearer secret" {
		t.Errorf("missing bearer header: %q", rt.last.Header.Get("Authorization"))
	}
	if rt.last.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q", rt.last.Header.Get("Content-Type"))
	}
}

func TestForwarderHeartbeatHitsHeartbeatPath(t *testing.T) {
	rt := &fakeRT{}
	f := newForwarder(forwarderConfig{
		baseURL: "http://mac:3000/internal/airplay",
		key:     "k",
		client:  &http.Client{Transport: rt},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	f.PostHeartbeat()
	f.flushForTest(ctx)

	if rt.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", rt.calls.Load())
	}
	if !strings.HasSuffix(rt.last.URL.Path, "/internal/airplay/heartbeat") {
		t.Errorf("URL path = %q", rt.last.URL.Path)
	}
}

func TestForwarderEndPostsEndedTrue(t *testing.T) {
	rt := &fakeRT{}
	f := newForwarder(forwarderConfig{
		baseURL: "http://mac:3000/internal/airplay",
		key:     "k",
		client:  &http.Client{Transport: rt},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	f.PostState(map[string]interface{}{"ended": true})
	f.flushForTest(ctx)

	// Read the body off the recorded request.
	if rt.last == nil {
		t.Fatalf("no request recorded")
	}
	body := make([]byte, 256)
	n, _ := rt.last.Body.Read(body)
	var decoded map[string]interface{}
	_ = json.Unmarshal(body[:n], &decoded)
	if v, _ := decoded["ended"].(bool); !v {
		t.Errorf("body should contain ended:true; got %v", decoded)
	}
}

func TestForwarderRetriesOnFailure(t *testing.T) {
	rt := &fakeRT{script: []rtOutcome{
		{err: errors.New("dial: connection refused")},
		{err: errors.New("dial: connection refused")},
		{status: http.StatusNoContent},
	}}

	var observed []time.Duration
	f := newForwarder(forwarderConfig{
		baseURL:        "http://mac",
		key:            "k",
		client:         &http.Client{Transport: rt},
		initialBackoff: 1 * time.Millisecond,
		maxBackoff:     4 * time.Millisecond,
		sleep: func(_ context.Context, d time.Duration) bool {
			observed = append(observed, d)
			return true
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	f.PostState(map[string]interface{}{"title": "x"})
	f.flushForTest(ctx)

	if rt.calls.Load() != 3 {
		t.Errorf("expected 3 calls (2 fails + 1 success), got %d", rt.calls.Load())
	}
	if len(observed) < 2 {
		t.Errorf("expected at least 2 backoff sleeps, got %d", len(observed))
	}
}

func TestForwarderDropsWhenQueueFull(t *testing.T) {
	rt := &fakeRT{script: []rtOutcome{{err: errors.New("hang")}}}
	f := newForwarder(forwarderConfig{
		baseURL:        "http://mac",
		key:            "k",
		client:         &http.Client{Transport: rt},
		initialBackoff: time.Hour, // ensure retries park
		queueSize:      2,
		sleep:          func(context.Context, time.Duration) bool { return true },
	})

	// Don't drain; push 5 payloads. Queue is 2 → 3 must be dropped.
	for i := 0; i < 5; i++ {
		f.PostState(map[string]interface{}{"i": i})
	}
	if f.dropped.Load() < 1 {
		t.Errorf("expected dropped > 0 with queue=2 and 5 unprocessed pushes; got %d", f.dropped.Load())
	}
}
