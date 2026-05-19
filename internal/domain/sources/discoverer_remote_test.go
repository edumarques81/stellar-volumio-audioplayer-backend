package sources

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type fakeDiscRT struct {
	mu         sync.Mutex
	script     []discOutcome
	calls      atomic.Int32
	lastAuth   string
	lastURL    string
	lastMethod string
}

type discOutcome struct {
	status int
	body   string
	err    error
}

func (f *fakeDiscRT) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastAuth = r.Header.Get("X-Auth-Token")
	f.lastURL = r.URL.String()
	f.lastMethod = r.Method
	var o discOutcome
	if len(f.script) > 0 {
		o = f.script[0]
		if len(f.script) > 1 {
			f.script = f.script[1:]
		}
	}
	if o.err != nil {
		return nil, o.err
	}
	body := io.NopCloser(strings.NewReader(o.body))
	return &http.Response{
		StatusCode: o.status,
		Body:       body,
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func newRD(rt *fakeDiscRT) *RemoteDiscoverer {
	return NewRemoteDiscovererWithClient("http://pi.local:8082", "tok", &http.Client{Transport: rt})
}

func TestRemoteDiscoverer_DiscoverDevices(t *testing.T) {
	body := `{"devices":[{"name":"NAS_Music","ip":"192.168.86.26","hostname":"nas.local"}]}`
	rt := &fakeDiscRT{script: []discOutcome{{status: 200, body: body}}}
	rd := newRD(rt)

	devs, err := rd.DiscoverDevices(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(devs) != 1 || devs[0].Name != "NAS_Music" || devs[0].IP != "192.168.86.26" || devs[0].Hostname != "nas.local" {
		t.Errorf("wrong devices: %+v", devs)
	}
	if !strings.HasSuffix(rt.lastURL, "/api/mount/devices") {
		t.Errorf("wrong URL: %s", rt.lastURL)
	}
}

func TestRemoteDiscoverer_BrowseShares(t *testing.T) {
	body := `{"shares":[{"name":"Music","type":"disk","comment":"main lib","writable":true},{"name":"Photos","type":"disk","comment":"","writable":true}]}`
	rt := &fakeDiscRT{script: []discOutcome{{status: 200, body: body}}}
	rd := newRD(rt)

	shares, err := rd.BrowseShares(context.Background(), "192.168.86.26", "u", "p")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(shares) != 2 || shares[0].Name != "Music" || !shares[0].Writable || shares[1].Name != "Photos" {
		t.Errorf("wrong shares: %+v", shares)
	}
	if !strings.Contains(rt.lastURL, "host=192.168.86.26") ||
		!strings.Contains(rt.lastURL, "username=u") ||
		!strings.Contains(rt.lastURL, "password=p") {
		t.Errorf("query missing params: %s", rt.lastURL)
	}
}

func TestRemoteDiscoverer_BrowseShares_AuthRequired(t *testing.T) {
	body := `{"code":"AUTH_REQUIRED","message":"authentication required"}`
	rt := &fakeDiscRT{script: []discOutcome{{status: 502, body: body}}}
	rd := newRD(rt)

	_, err := rd.BrowseShares(context.Background(), "1.2.3.4", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	var sbe *ShareBrowseError
	if !errors.As(err, &sbe) {
		t.Fatalf("expected *ShareBrowseError, got %T: %v", err, err)
	}
	if sbe.Code != "AUTH_REQUIRED" {
		t.Errorf("wrong code: %q", sbe.Code)
	}
}

func TestRemoteDiscoverer_BrowseShares_HostUnreachable(t *testing.T) {
	body := `{"code":"HOST_UNREACHABLE","message":"host unreachable: 1.2.3.4"}`
	rt := &fakeDiscRT{script: []discOutcome{{status: 502, body: body}}}
	rd := newRD(rt)

	_, err := rd.BrowseShares(context.Background(), "1.2.3.4", "", "")
	var sbe *ShareBrowseError
	if !errors.As(err, &sbe) || sbe.Code != "HOST_UNREACHABLE" {
		t.Errorf("wrong error: %T %v", err, err)
	}
}

func TestRemoteDiscoverer_TransportError(t *testing.T) {
	rt := &fakeDiscRT{script: []discOutcome{{err: errors.New("conn refused")}}}
	rd := newRD(rt)
	_, err := rd.DiscoverDevices(context.Background())
	if err == nil || !strings.Contains(err.Error(), "conn refused") {
		t.Errorf("expected wrapped transport err, got: %v", err)
	}
}
