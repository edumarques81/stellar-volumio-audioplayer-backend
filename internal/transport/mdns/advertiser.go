// Package mdns advertises the Stellar backend over Bonjour/mDNS so
// LAN clients (iOS app, Volumio2-UI dev kiosks, etc.) can auto-discover
// the Socket.IO server without hardcoded IPs.
//
// The advertised service is `_stellar._tcp` with TXT records
// `path=/socket.io` and `version=1`. The instance name defaults to the
// machine's hostname (falling back to `Stellar`) so multiple servers on
// the same LAN don't collide.
//
// Start failures are logged by the caller and treated as non-fatal —
// discovery is a convenience, not a hard requirement for the backend to
// serve clients.
package mdns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/grandcat/zeroconf"
)

// sanitiseInstance trims trailing dots and `.local`/`.local.` suffixes from a
// hostname so it's safe to use as the first DNS label of an mDNS PTR record.
// macOS's `os.Hostname()` returns `Foo.local`, which would otherwise produce
// `Foo.local._stellar._tcp.local.` — two `.local` labels — and mDNSResponder
// silently drops the announcement.
func sanitiseInstance(name string) string {
	name = strings.TrimSuffix(name, ".")
	name = strings.TrimSuffix(name, ".local")
	name = strings.TrimSuffix(name, ".")
	return name
}

// pickAdvertisementInterfaces returns the interfaces best suited for mDNS
// advertisement: UP, MULTICAST, non-loopback, non-pointopoint, and bearing a
// global IPv4 address. This filters out NordVPN/Wireguard utun* tunnels,
// awdl0, and other interfaces that don't carry LAN multicast on the user's
// actual subnet. Returning nil from this function tells zeroconf to fall back
// to its "all multicast interfaces" default — useful when the heuristic finds
// nothing (e.g. a host with only tunnel interfaces during early boot).
func pickAdvertisementInterfaces() []net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var picked []net.Interface
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 ||
			ifi.Flags&net.FlagMulticast == 0 ||
			ifi.Flags&net.FlagLoopback != 0 ||
			ifi.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, addrErr := ifi.Addrs()
		if addrErr != nil {
			continue
		}
		for _, a := range addrs {
			ip, _, parseErr := net.ParseCIDR(a.String())
			if parseErr != nil {
				continue
			}
			v4 := ip.To4()
			if v4 == nil || v4.IsLinkLocalUnicast() || v4.IsLoopback() {
				continue
			}
			picked = append(picked, ifi)
			break
		}
	}
	if len(picked) == 0 {
		return nil
	}
	return picked
}

// hostnameFn is a swappable seam so tests can simulate hostname errors.
var hostnameFn = os.Hostname

// serverHandle is the subset of *zeroconf.Server we depend on. Defining
// it as an interface lets unit tests substitute a fake without touching
// the multicast socket layer.
type serverHandle interface {
	Shutdown()
}

// registerFunc abstracts zeroconf.Register so tests can inject a stub.
// The ifaces argument is typed as interface{} to keep the seam minimal —
// the real implementation passes nil (meaning "all multicast interfaces")
// and tests don't exercise iface selection.
type registerFunc func(instance, service, domain string, port int, text []string, ifaces interface{}) (serverHandle, error)

// defaultRegister wraps zeroconf.Register and returns the *zeroconf.Server
// as a serverHandle.
func defaultRegister(instance, service, domain string, port int, text []string, _ interface{}) (serverHandle, error) {
	srv, err := zeroconf.Register(instance, service, domain, port, text, pickAdvertisementInterfaces())
	if err != nil {
		return nil, err
	}
	return srv, nil
}

// Config tunes the advertised mDNS service. Zero values are filled in by
// withDefaults: Service=`_stellar._tcp`, Domain=`local.`, Instance=hostname
// (or `Stellar`), TXT=[`path=/socket.io`, `version=1`]. Port is required.
type Config struct {
	// Instance is the human-readable instance name (e.g. the hostname).
	// Defaults to os.Hostname(), with `Stellar` as a last-resort fallback.
	Instance string

	// Service is the mDNS service type. Defaults to `_stellar._tcp`.
	Service string

	// Domain is the mDNS domain. Defaults to `local.`.
	Domain string

	// Port is the advertised TCP port. Required; zero is an error.
	Port int

	// TXT records published alongside the service. Defaults to the
	// canonical Stellar pair: `path=/socket.io`, `version=1`.
	TXT []string
}

// withDefaults returns a copy of cfg with any unset fields populated.
func (c Config) withDefaults() Config {
	if c.Service == "" {
		c.Service = "_stellar._tcp"
	}
	if c.Domain == "" {
		c.Domain = "local."
	}
	if c.Instance == "" {
		if host, err := hostnameFn(); err == nil && host != "" {
			c.Instance = sanitiseInstance(host)
		}
		if c.Instance == "" {
			c.Instance = "Stellar"
		}
	}
	if len(c.TXT) == 0 {
		c.TXT = []string{"path=/socket.io", "version=1"}
	}
	return c
}

// Advertiser publishes a service on the LAN and unpublishes it on Stop.
// Implementations must be safe for concurrent Start/Stop calls and must
// treat duplicate Start or Stop as a no-op.
type Advertiser interface {
	// Start begins advertising. The ctx argument is reserved for future
	// use (e.g. cancelling a slow startup probe); current implementations
	// return promptly. Start is idempotent: a second call on a running
	// advertiser returns nil without re-registering.
	Start(ctx context.Context) error

	// Stop unregisters the service. Safe to call before Start (no-op) and
	// safe to call multiple times.
	Stop() error

	// IsRunning reports whether the service is currently advertised.
	IsRunning() bool
}

// BonjourAdvertiser is the concrete Advertiser backed by zeroconf.
type BonjourAdvertiser struct {
	cfg      Config
	register registerFunc

	mu     sync.Mutex
	server serverHandle
}

// New builds a BonjourAdvertiser using the real zeroconf backend. It
// returns an error if the config is invalid (e.g. Port == 0).
func New(cfg Config) (*BonjourAdvertiser, error) {
	return newWithRegister(cfg, defaultRegister)
}

// newWithRegister is the test-friendly constructor that accepts an
// injectable registerFunc. It's unexported so callers always go through
// New, but lives in the same package so _test.go can reach it.
func newWithRegister(cfg Config, reg registerFunc) (*BonjourAdvertiser, error) {
	if cfg.Port == 0 {
		return nil, errors.New("mdns: Config.Port is required (cannot be 0)")
	}
	if reg == nil {
		return nil, errors.New("mdns: registerFunc is required")
	}
	return &BonjourAdvertiser{
		cfg:      cfg.withDefaults(),
		register: reg,
	}, nil
}

// Start begins advertising. Safe to call multiple times; a second call
// while running is a no-op.
func (a *BonjourAdvertiser) Start(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.server != nil {
		return nil // already running
	}

	srv, err := a.register(a.cfg.Instance, a.cfg.Service, a.cfg.Domain, a.cfg.Port, a.cfg.TXT, nil)
	if err != nil {
		return fmt.Errorf("mdns: register %q on %s: %w", a.cfg.Instance, a.cfg.Service, err)
	}
	a.server = srv
	return nil
}

// Stop unregisters the service. Safe to call before Start or multiple
// times.
func (a *BonjourAdvertiser) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.server == nil {
		return nil
	}
	a.server.Shutdown()
	a.server = nil
	return nil
}

// IsRunning reports whether the advertiser is currently publishing.
func (a *BonjourAdvertiser) IsRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.server != nil
}
