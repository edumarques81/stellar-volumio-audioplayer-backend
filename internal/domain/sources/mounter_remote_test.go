package sources

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type fakeMountRT struct {
	mu       sync.Mutex
	script   []mountOutcome
	calls    atomic.Int32
	lastAuth string
	lastURL  string
	lastBody string
	lastMethod string
}

type mountOutcome struct {
	status int
	body   string
	err    error
}

func (f *fakeMountRT) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastAuth = r.Header.Get("X-Auth-Token")
	f.lastURL = r.URL.String()
	f.lastMethod = r.Method
	if r.Body != nil {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		f.lastBody = string(buf[:n])
	}
	var o mountOutcome
	if len(f.script) > 0 {
		o = f.script[0]
		if len(f.script) > 1 {
			f.script = f.script[1:]
		}
	}
	if o.err != nil {
		return nil, o.err
	}
	return &http.Response{
		StatusCode: o.status,
		Body:       http.NoBody,
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func newRM(rt *fakeMountRT) *RemoteMounter {
	return NewRemoteMounterWithClient("http://pi.local:8082", "tok", &http.Client{Transport: rt})
}

func TestRemoteMounter_Mount_CIFS(t *testing.T) {
	rt := &fakeMountRT{script: []mountOutcome{{status: 200}}}
	rm := newRM(rt)
	share := &NasShare{IP: "192.168.86.26", Path: "Music", FSType: "cifs",
		Username: "user", Password: "pw", MountPoint: "/mnt/NAS/music"}
	if err := rm.Mount(context.Background(), share); err != nil {
		t.Fatalf("Mount err: %v", err)
	}
	if !share.Mounted {
		t.Error("share.Mounted should be true after success")
	}
	if rt.lastMethod != "POST" || !strings.HasSuffix(strings.SplitN(rt.lastURL, "?", 2)[0], "/api/mount") {
		t.Errorf("wrong METHOD/URL: %s %s", rt.lastMethod, rt.lastURL)
	}
	if !strings.Contains(rt.lastBody, `"ip":"192.168.86.26"`) ||
		!strings.Contains(rt.lastBody, `"share":"Music"`) ||
		!strings.Contains(rt.lastBody, `"fstype":"cifs"`) ||
		!strings.Contains(rt.lastBody, `"mountpoint":"/mnt/NAS/music"`) ||
		!strings.Contains(rt.lastBody, `"username":"user"`) {
		t.Errorf("body missing expected fields: %q", rt.lastBody)
	}
	if rt.lastAuth != "tok" {
		t.Errorf("wrong auth header: %q", rt.lastAuth)
	}
}

func TestRemoteMounter_Mount_Failure(t *testing.T) {
	rt := &fakeMountRT{script: []mountOutcome{{status: 500}}}
	rm := newRM(rt)
	share := &NasShare{IP: "x", Path: "y", FSType: "cifs", MountPoint: "/mnt/z"}
	if err := rm.Mount(context.Background(), share); err == nil {
		t.Fatal("expected error on 500")
	}
	if share.Mounted {
		t.Error("share.Mounted should remain false after failure")
	}
}

func TestRemoteMounter_Unmount(t *testing.T) {
	rt := &fakeMountRT{script: []mountOutcome{{status: 200}}}
	rm := newRM(rt)
	if err := rm.Unmount(context.Background(), "/mnt/NAS/music"); err != nil {
		t.Fatalf("Unmount err: %v", err)
	}
	if !strings.Contains(rt.lastBody, `"mountpoint":"/mnt/NAS/music"`) {
		t.Errorf("body missing mountpoint: %q", rt.lastBody)
	}
}

func TestRemoteMounter_IsMounted(t *testing.T) {
	rt := &fakeMountRT{script: []mountOutcome{{status: 200}}}
	rm := newRM(rt)
	// IsMounted returns false here because fake body is empty — we just
	// assert the request goes to the right URL.
	_ = rm.IsMounted("/mnt/NAS/music")
	if !strings.Contains(rt.lastURL, "/api/mount/is-mounted") {
		t.Errorf("wrong URL: %q", rt.lastURL)
	}
	if !strings.Contains(rt.lastURL, "path=%2Fmnt%2FNAS%2Fmusic") {
		t.Errorf("path query missing/escaped wrong: %q", rt.lastURL)
	}
}

func TestRemoteMounter_CreateMountPoint(t *testing.T) {
	rt := &fakeMountRT{script: []mountOutcome{{status: 200}}}
	rm := newRM(rt)
	if err := rm.CreateMountPoint("/mnt/NAS/foo"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.HasSuffix(rt.lastURL, "/api/mount/mountpoint") || rt.lastMethod != "POST" {
		t.Errorf("wrong POST URL: %s %s", rt.lastMethod, rt.lastURL)
	}
	if !strings.Contains(rt.lastBody, `"path":"/mnt/NAS/foo"`) {
		t.Errorf("body missing path: %q", rt.lastBody)
	}
}

func TestRemoteMounter_RemoveMountPoint(t *testing.T) {
	rt := &fakeMountRT{script: []mountOutcome{{status: 200}}}
	rm := newRM(rt)
	if err := rm.RemoveMountPoint("/mnt/NAS/foo"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if rt.lastMethod != "DELETE" || !strings.Contains(rt.lastURL, "/api/mount/mountpoint") {
		t.Errorf("wrong DELETE URL: %s %s", rt.lastMethod, rt.lastURL)
	}
}

func TestRemoteMounter_CreateSymlink(t *testing.T) {
	rt := &fakeMountRT{script: []mountOutcome{{status: 200}}}
	rm := newRM(rt)
	if err := rm.CreateSymlink("/mnt/NAS/foo", "/var/lib/mpd/music/foo"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(rt.lastBody, `"source":"/mnt/NAS/foo"`) ||
		!strings.Contains(rt.lastBody, `"target":"/var/lib/mpd/music/foo"`) {
		t.Errorf("body missing source/target: %q", rt.lastBody)
	}
}

func TestRemoteMounter_RemoveSymlink(t *testing.T) {
	rt := &fakeMountRT{script: []mountOutcome{{status: 200}}}
	rm := newRM(rt)
	if err := rm.RemoveSymlink("/var/lib/mpd/music/foo"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if rt.lastMethod != "DELETE" {
		t.Errorf("wrong method: %s", rt.lastMethod)
	}
}

func TestRemoteMounter_TransportErrorWrapped(t *testing.T) {
	rt := &fakeMountRT{script: []mountOutcome{{err: errors.New("conn refused")}}}
	rm := newRM(rt)
	err := rm.Mount(context.Background(), &NasShare{IP: "x", Path: "y", FSType: "cifs", MountPoint: "/m"})
	if err == nil || !strings.Contains(err.Error(), "conn refused") {
		t.Errorf("expected wrapped transport err, got: %v", err)
	}
}

func TestRemoteMounter_AuthError(t *testing.T) {
	rt := &fakeMountRT{script: []mountOutcome{{status: 401}}}
	rm := newRM(rt)
	err := rm.Mount(context.Background(), &NasShare{IP: "x", Path: "y", FSType: "cifs", MountPoint: "/m"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in err, got: %v", err)
	}
}
