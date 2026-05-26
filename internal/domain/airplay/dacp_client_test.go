package airplay

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// dacpRT is a controllable RoundTripper that records each request and
// returns a scripted outcome.
type dacpRT struct {
	mu       sync.Mutex
	calls    atomic.Int32
	lastURL  string
	lastAuth string
	status   int
	err      error
}

func (d *dacpRT) RoundTrip(r *http.Request) (*http.Response, error) {
	d.calls.Add(1)
	d.mu.Lock()
	d.lastURL = r.URL.String()
	d.lastAuth = r.Header.Get("Active-Remote")
	d.mu.Unlock()
	if d.err != nil {
		return nil, d.err
	}
	status := d.status
	if status == 0 {
		status = http.StatusNoContent
	}
	return &http.Response{
		StatusCode: status,
		Body:       http.NoBody,
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func TestDACPClientSendsPlayCommand(t *testing.T) {
	rt := &dacpRT{}
	c := NewDACPClient(&http.Client{Transport: rt, Timeout: time.Second})
	err := c.Play(context.Background(), "http://192.168.86.30:50556", "3823061215")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if rt.calls.Load() != 1 {
		t.Errorf("expected 1 call, got %d", rt.calls.Load())
	}
	if !strings.HasSuffix(rt.lastURL, "/ctrl-int/1/play") {
		t.Errorf("URL = %q, want suffix /ctrl-int/1/play", rt.lastURL)
	}
	if rt.lastAuth != "3823061215" {
		t.Errorf("Active-Remote = %q, want 3823061215", rt.lastAuth)
	}
}

func TestDACPClientCommands(t *testing.T) {
	cases := []struct {
		name string
		call func(context.Context, *DACPClient) error
		want string
	}{
		{"pause", func(ctx context.Context, c *DACPClient) error { return c.Pause(ctx, "http://h", "t") }, "/ctrl-int/1/pause"},
		{"toggle", func(ctx context.Context, c *DACPClient) error { return c.PlayPause(ctx, "http://h", "t") }, "/ctrl-int/1/playpause"},
		{"next", func(ctx context.Context, c *DACPClient) error { return c.Next(ctx, "http://h", "t") }, "/ctrl-int/1/nextitem"},
		{"prev", func(ctx context.Context, c *DACPClient) error { return c.Prev(ctx, "http://h", "t") }, "/ctrl-int/1/previtem"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := &dacpRT{}
			c := NewDACPClient(&http.Client{Transport: rt, Timeout: time.Second})
			if err := tc.call(context.Background(), c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !strings.HasSuffix(rt.lastURL, tc.want) {
				t.Errorf("%s URL = %q, want suffix %q", tc.name, rt.lastURL, tc.want)
			}
		})
	}
}

func TestDACPClientSurfacesNon2xxStatus(t *testing.T) {
	rt := &dacpRT{status: http.StatusForbidden}
	c := NewDACPClient(&http.Client{Transport: rt, Timeout: time.Second})
	err := c.Play(context.Background(), "http://h", "t")
	if err == nil {
		t.Fatalf("expected error on 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention 403; got %v", err)
	}
}

func TestDACPClientSurfacesTransportError(t *testing.T) {
	rt := &dacpRT{err: errors.New("connection refused")}
	c := NewDACPClient(&http.Client{Transport: rt, Timeout: time.Second})
	err := c.Play(context.Background(), "http://h", "t")
	if err == nil {
		t.Fatalf("expected transport error")
	}
}

func TestDACPClientRejectsEmptyToken(t *testing.T) {
	rt := &dacpRT{}
	c := NewDACPClient(&http.Client{Transport: rt, Timeout: time.Second})
	err := c.Play(context.Background(), "http://h", "")
	if err == nil {
		t.Fatalf("expected error for empty token")
	}
	if rt.calls.Load() != 0 {
		t.Errorf("should not have called transport: %d calls", rt.calls.Load())
	}
}

func TestDACPClientRejectsEmptyBaseURL(t *testing.T) {
	rt := &dacpRT{}
	c := NewDACPClient(&http.Client{Transport: rt, Timeout: time.Second})
	err := c.Play(context.Background(), "", "t")
	if err == nil {
		t.Fatalf("expected error for empty base URL")
	}
}
