package airplay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DACPResolver discovers the host:port of an AirPlay sender's DACP HTTP
// endpoint via Bonjour. The instance name is `iTunes_Ctrl_<DACP_ID>`
// under the service type `_dacp._tcp`. The sender publishes this after
// the AirPlay session establishes; the daemon also reports the DACP-ID
// in every ssnc/daid metadata chunk.
//
// Platform shellouts:
//   - darwin: `dns-sd -L iTunes_Ctrl_<id> _dacp._tcp local`
//   - linux:  `avahi-browse -r _dacp._tcp --terminate`
//
// Both are run with a hard 3s timeout — the iPhone is on the same LAN
// and a working sender resolves in well under 500ms.
type DACPResolver struct {
	mu    sync.RWMutex
	cache map[string]dacpEndpoint // keyed by DACP-ID

	resolveTimeout time.Duration

	// resolveFn defaults to platform-dispatch but is overridable for
	// tests.
	resolveFn func(ctx context.Context, dacpID string) (host string, port int, ok bool)
}

type dacpEndpoint struct {
	host string
	port int
}

// NewDACPResolver returns a Resolver with a 3s shellout timeout.
func NewDACPResolver() *DACPResolver {
	r := &DACPResolver{
		cache:          make(map[string]dacpEndpoint),
		resolveTimeout: 3 * time.Second,
	}
	r.resolveFn = r.platformResolve
	return r
}

// Resolve returns the base URL ("http://host:port") for the supplied
// DACP-ID. Cached lookups are returned immediately; cache misses trigger
// a fresh Bonjour shellout.
func (r *DACPResolver) Resolve(ctx context.Context, dacpID string) (string, error) {
	if dacpID == "" {
		return "", errors.New("dacp resolver: empty DACP-ID")
	}
	r.mu.RLock()
	ep, ok := r.cache[dacpID]
	r.mu.RUnlock()
	if ok {
		return formatURL(ep.host, ep.port), nil
	}

	resolveCtx, cancel := context.WithTimeout(ctx, r.resolveTimeout)
	defer cancel()

	host, port, ok := r.resolveFn(resolveCtx, dacpID)
	if !ok {
		return "", fmt.Errorf("dacp resolver: instance iTunes_Ctrl_%s not found", dacpID)
	}

	r.mu.Lock()
	r.cache[dacpID] = dacpEndpoint{host: host, port: port}
	r.mu.Unlock()

	return formatURL(host, port), nil
}

// Invalidate drops the cached endpoint for the supplied DACP-ID. The
// command handler calls this after a transport error so the next
// command re-resolves from scratch.
func (r *DACPResolver) Invalidate(dacpID string) {
	r.mu.Lock()
	delete(r.cache, dacpID)
	r.mu.Unlock()
}

func (r *DACPResolver) platformResolve(ctx context.Context, dacpID string) (string, int, bool) {
	switch runtime.GOOS {
	case "darwin":
		return resolveViaDNSSD(ctx, dacpID)
	case "linux":
		return resolveViaAvahi(ctx, dacpID)
	default:
		return "", 0, false
	}
}

func resolveViaDNSSD(ctx context.Context, dacpID string) (string, int, bool) {
	instance := "iTunes_Ctrl_" + dacpID
	// `dns-sd -L` does not terminate on its own; we read it for a short
	// window and parse the first "can be reached at" line. The outer
	// context timeout enforces the cap.
	cmd := exec.CommandContext(ctx, "dns-sd", "-L", instance, "_dacp._tcp", "local")
	out, _ := cmd.CombinedOutput() // err is always context-cancel here; output is what matters
	return ParseDNSSDResolveOutput(string(out))
}

func resolveViaAvahi(ctx context.Context, dacpID string) (string, int, bool) {
	cmd := exec.CommandContext(ctx, "avahi-browse", "-r", "_dacp._tcp", "--terminate")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", 0, false
	}
	return ParseAvahiResolveOutput(string(out), dacpID)
}

// dnssdResolveRegex matches the "can be reached at HOSTNAME:PORT" line
// emitted by `dns-sd -L`.
var dnssdResolveRegex = regexp.MustCompile(`can be reached at\s+(\S+?):(\d+)`)

// ParseDNSSDResolveOutput extracts (host, port) from the output of
// `dns-sd -L iTunes_Ctrl_<id> _dacp._tcp local`. Returns ok=false if no
// resolve line is present.
func ParseDNSSDResolveOutput(out string) (string, int, bool) {
	m := dnssdResolveRegex.FindStringSubmatch(out)
	if m == nil {
		return "", 0, false
	}
	host := strings.TrimSpace(m[1])
	port, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return host, port, true
}

// avahiInstanceRegex matches a resolved-service line from `avahi-browse
// -r _dacp._tcp --terminate`.
var avahiInstanceRegex = regexp.MustCompile(`^=\s+\S+\s+IPv[46]\s+iTunes_Ctrl_(\S+)\s+_dacp\._tcp\s+local`)
var avahiAddressRegex = regexp.MustCompile(`^\s+address\s*=\s*\[([^\]]+)\]`)
var avahiPortRegex = regexp.MustCompile(`^\s+port\s*=\s*\[(\d+)\]`)

// ParseAvahiResolveOutput extracts (host, port) for the supplied DACP-ID
// from the output of `avahi-browse -r _dacp._tcp --terminate`. The
// stream contains every published service on the LAN; we filter to the
// instance whose name is iTunes_Ctrl_<dacpID>.
func ParseAvahiResolveOutput(out, dacpID string) (string, int, bool) {
	lines := strings.Split(out, "\n")

	var match bool
	var host string
	var port int

	for _, line := range lines {
		if m := avahiInstanceRegex.FindStringSubmatch(line); m != nil {
			// Encountered a new instance — commit the previous if it
			// matched and was complete, otherwise reset.
			if match && host != "" && port > 0 {
				return host, port, true
			}
			match = strings.EqualFold(strings.TrimSpace(m[1]), dacpID)
			host = ""
			port = 0
			continue
		}
		if !match {
			continue
		}
		if m := avahiAddressRegex.FindStringSubmatch(line); m != nil {
			host = strings.TrimSpace(m[1])
			continue
		}
		if m := avahiPortRegex.FindStringSubmatch(line); m != nil {
			if p, err := strconv.Atoi(m[1]); err == nil {
				port = p
			}
		}
	}
	if match && host != "" && port > 0 {
		return host, port, true
	}
	return "", 0, false
}

func formatURL(host string, port int) string {
	host = strings.TrimRight(host, ".")
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}
