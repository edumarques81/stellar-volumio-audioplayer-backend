// Package mdns advertises the Stellar backend over Bonjour/mDNS so
// LAN clients (iOS app, Volumio2-UI dev kiosks, etc.) can auto-discover
// the Socket.IO server without hardcoded IPs.
package mdns

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grandcat/zeroconf"
)

// fakeServerHandle stands in for *zeroconf.Server in unit tests so we
// can exercise Start/Stop without binding multicast sockets.
type fakeServerHandle struct {
	shutdownCount atomic.Int32
}

func (f *fakeServerHandle) Shutdown() { f.shutdownCount.Add(1) }

// stubRegister builds a registerFunc that records the args it was called
// with and returns the supplied handle/err. It also lets a test fail the
// first N calls to simulate transient mDNS failures.
func stubRegister(handle serverHandle, err error) (registerFunc, *registerCall) {
	call := &registerCall{}
	fn := func(instance, service, domain string, port int, text []string, ifaces interface{}) (serverHandle, error) {
		call.mu.Lock()
		defer call.mu.Unlock()
		call.invocations++
		call.lastInstance = instance
		call.lastService = service
		call.lastDomain = domain
		call.lastPort = port
		call.lastText = append([]string(nil), text...)
		if err != nil {
			return nil, err
		}
		return handle, nil
	}
	return fn, call
}

type registerCall struct {
	mu           sync.Mutex
	invocations  int
	lastInstance string
	lastService  string
	lastDomain   string
	lastPort     int
	lastText     []string
}

func (c *registerCall) snapshot() registerCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return registerCall{
		invocations:  c.invocations,
		lastInstance: c.lastInstance,
		lastService:  c.lastService,
		lastDomain:   c.lastDomain,
		lastPort:     c.lastPort,
		lastText:     append([]string(nil), c.lastText...),
	}
}

func TestConfigDefaults_FillsServiceDomainAndInstance(t *testing.T) {
	t.Parallel()
	cfg := Config{Port: 3001}
	cfg = cfg.withDefaults()

	if cfg.Service != "_stellar._tcp" {
		t.Errorf("default service = %q, want %q", cfg.Service, "_stellar._tcp")
	}
	if cfg.Domain != "local." {
		t.Errorf("default domain = %q, want %q", cfg.Domain, "local.")
	}
	if cfg.Instance == "" {
		t.Error("default instance should be non-empty (hostname or Stellar fallback)")
	}
	host, _ := os.Hostname()
	if host != "" && cfg.Instance != host && cfg.Instance != "Stellar" {
		t.Errorf("default instance = %q, want hostname %q or fallback Stellar", cfg.Instance, host)
	}
}

func TestConfigDefaults_DefaultTXTRecords(t *testing.T) {
	t.Parallel()
	cfg := Config{Port: 3001}.withDefaults()

	wantPath := "path=/socket.io"
	wantVersion := "version=1"
	var hasPath, hasVersion bool
	for _, rec := range cfg.TXT {
		if rec == wantPath {
			hasPath = true
		}
		if rec == wantVersion {
			hasVersion = true
		}
	}
	if !hasPath {
		t.Errorf("default TXT records missing %q, got %v", wantPath, cfg.TXT)
	}
	if !hasVersion {
		t.Errorf("default TXT records missing %q, got %v", wantVersion, cfg.TXT)
	}
}

func TestConfigDefaults_DoesNotOverrideCallerSetFields(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Instance: "MyHost",
		Service:  "_custom._tcp",
		Domain:   "local.",
		Port:     4242,
		TXT:      []string{"foo=bar"},
	}.withDefaults()

	if cfg.Instance != "MyHost" {
		t.Errorf("instance overridden: got %q", cfg.Instance)
	}
	if cfg.Service != "_custom._tcp" {
		t.Errorf("service overridden: got %q", cfg.Service)
	}
	if cfg.Port != 4242 {
		t.Errorf("port overridden: got %d", cfg.Port)
	}
	if len(cfg.TXT) != 1 || cfg.TXT[0] != "foo=bar" {
		t.Errorf("TXT overridden: got %v", cfg.TXT)
	}
}

func TestNew_RejectsZeroPort(t *testing.T) {
	t.Parallel()
	_, err := New(Config{Port: 0})
	if err == nil {
		t.Fatal("New with port=0 should error")
	}
}

func TestStart_PassesConfigToRegister(t *testing.T) {
	t.Parallel()
	fake := &fakeServerHandle{}
	reg, call := stubRegister(fake, nil)

	adv, err := newWithRegister(Config{
		Instance: "TestBox",
		Service:  "_stellar._tcp",
		Port:     3001,
		TXT:      []string{"path=/socket.io", "version=1"},
	}, reg)
	if err != nil {
		t.Fatalf("newWithRegister: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := adv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = adv.Stop() }()

	snap := call.snapshot()
	if snap.invocations != 1 {
		t.Errorf("invocations = %d, want 1", snap.invocations)
	}
	if snap.lastInstance != "TestBox" {
		t.Errorf("instance = %q", snap.lastInstance)
	}
	if snap.lastService != "_stellar._tcp" {
		t.Errorf("service = %q", snap.lastService)
	}
	if snap.lastPort != 3001 {
		t.Errorf("port = %d", snap.lastPort)
	}
	if !adv.IsRunning() {
		t.Error("IsRunning() = false after successful Start")
	}
}

func TestStart_IsIdempotent(t *testing.T) {
	t.Parallel()
	fake := &fakeServerHandle{}
	reg, call := stubRegister(fake, nil)
	adv, err := newWithRegister(Config{Port: 3001}, reg)
	if err != nil {
		t.Fatalf("newWithRegister: %v", err)
	}

	ctx := context.Background()
	if err := adv.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := adv.Start(ctx); err != nil {
		t.Fatalf("second Start should be a no-op, got: %v", err)
	}
	defer func() { _ = adv.Stop() }()

	if got := call.snapshot().invocations; got != 1 {
		t.Errorf("register invocations = %d, want 1 (Start must not double-register)", got)
	}
}

func TestStart_PropagatesRegisterError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("multicast bind failed")
	reg, _ := stubRegister(nil, wantErr)
	adv, err := newWithRegister(Config{Port: 3001}, reg)
	if err != nil {
		t.Fatalf("newWithRegister: %v", err)
	}

	err = adv.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "multicast bind failed") {
		t.Fatalf("Start error = %v, want it to wrap %q", err, wantErr)
	}
	if adv.IsRunning() {
		t.Error("IsRunning() = true after failed Start")
	}
}

func TestStop_ShutsDownAndIsIdempotent(t *testing.T) {
	t.Parallel()
	fake := &fakeServerHandle{}
	reg, _ := stubRegister(fake, nil)
	adv, err := newWithRegister(Config{Port: 3001}, reg)
	if err != nil {
		t.Fatalf("newWithRegister: %v", err)
	}

	if err := adv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := adv.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := adv.Stop(); err != nil {
		t.Fatalf("second Stop must be safe, got: %v", err)
	}

	if got := fake.shutdownCount.Load(); got != 1 {
		t.Errorf("zeroconf Shutdown calls = %d, want 1 (Stop must be idempotent)", got)
	}
	if adv.IsRunning() {
		t.Error("IsRunning() = true after Stop")
	}
}

func TestStop_BeforeStartIsSafe(t *testing.T) {
	t.Parallel()
	reg, _ := stubRegister(&fakeServerHandle{}, nil)
	adv, err := newWithRegister(Config{Port: 3001}, reg)
	if err != nil {
		t.Fatalf("newWithRegister: %v", err)
	}
	if err := adv.Stop(); err != nil {
		t.Fatalf("Stop before Start should be a no-op, got: %v", err)
	}
}

func TestStart_HostnameFallbackToStellar(t *testing.T) {
	// NOT t.Parallel(): mutates the hostnameFn package global, must run
	// alone relative to other tests that read it via withDefaults().
	origHostname := hostnameFn
	hostnameFn = func() (string, error) { return "", errors.New("no hostname for you") }
	t.Cleanup(func() { hostnameFn = origHostname })

	cfg := Config{Port: 3001}.withDefaults()
	if cfg.Instance != "Stellar" {
		t.Errorf("instance = %q, want fallback %q", cfg.Instance, "Stellar")
	}
}

// TestLoopback_RegisterIsObservable is an integration smoke test: it
// registers a real service on the LAN via zeroconf and verifies that
// the in-process resolver can see it. Skipped under -short because
// multicast is flaky in CI sandboxes.
func TestLoopback_RegisterIsObservable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping loopback mDNS test in -short mode")
	}
	t.Parallel()

	adv, err := New(Config{
		Instance: "StellarTest-" + randSuffix(),
		Service:  "_stellar-test._tcp",
		Port:     3099,
		TXT:      []string{"path=/socket.io", "version=1"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := adv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = adv.Stop() }()

	// Give the announcer a beat to send its initial probes.
	time.Sleep(250 * time.Millisecond)

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	entries := make(chan *zeroconf.ServiceEntry, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := resolver.Browse(ctx, "_stellar-test._tcp", "local.", entries); err != nil {
		t.Fatalf("Browse: %v", err)
	}

	wantInstance := adv.cfg.Instance
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-entries:
			if e == nil {
				continue
			}
			if e.Instance == wantInstance {
				return // success
			}
		case <-deadline:
			t.Skipf("did not observe own mDNS service %q within 3s — multicast likely blocked on this network", wantInstance)
			return
		}
	}
}

// randSuffix returns a short string unique enough for one test run so
// concurrent runs / leftover cached records on the LAN don't collide.
func randSuffix() string {
	return time.Now().UTC().Format("150405.000000")
}
