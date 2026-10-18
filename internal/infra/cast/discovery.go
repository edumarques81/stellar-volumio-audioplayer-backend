package cast

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/rs/zerolog/log"
)

// castService is the mDNS service Cast devices and speaker groups advertise.
// Groups look identical to single devices on the wire, which is why multiroom
// needs no extra protocol work if Nest speakers ever arrive.
const castService = "_googlecast._tcp"

// defaultCastPort is where every Cast device listens. The mDNS record carries a
// port and we use it, but a record missing one is still worth trying here.
const defaultCastPort = 8009

// Device is one discovered Cast endpoint.
type Device struct {
	// ID is the device's stable UUID from its mDNS TXT record. It survives
	// DHCP lease changes and renames, which is why it is what clients select
	// by rather than the address.
	ID string
	// Name is the user-visible name set in the Google Home app.
	Name string
	// Model is the hardware model string, e.g. "Google Nest Hub".
	Model string
	// Addr is "host:port", ready to hand to Dial.
	Addr string
	// IsGroup reports a speaker group rather than a single device.
	IsGroup bool
}

// Discoverer browses for Cast devices and caches what it finds.
//
// It is a cache rather than a live subscription because browsing is a
// multicast round trip that takes seconds, and an output picker needs an answer
// now. Callers get the last result immediately and a refresh runs underneath.
type Discoverer struct {
	mu      sync.RWMutex
	devices map[string]Device
	lastRun time.Time

	// browse is the mDNS lookup, injectable so tests do not need a network.
	browse func(ctx context.Context) ([]Device, error)

	refreshing sync.Mutex
}

// NewDiscoverer returns a Discoverer backed by real mDNS browsing.
func NewDiscoverer() *Discoverer {
	d := &Discoverer{devices: make(map[string]Device)}
	d.browse = browseCastDevices
	return d
}

// Devices returns the cached device list, sorted by name so a picker does not
// reorder itself between refreshes.
func (d *Discoverer) Devices() []Device {
	d.mu.RLock()
	defer d.mu.RUnlock()

	out := make([]Device, 0, len(d.devices))
	for _, dev := range d.devices {
		out = append(out, dev)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Lookup returns a cached device by ID.
func (d *Discoverer) Lookup(id string) (Device, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	dev, ok := d.devices[id]
	return dev, ok
}

// LastRun reports when the cache was last refreshed. Zero means never.
func (d *Discoverer) LastRun() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastRun
}

// Refresh browses the network and replaces the cache.
//
// A device that has gone quiet is dropped rather than retained: a picker
// offering an unplugged speaker is worse than a picker that is one browse
// behind. Concurrent calls collapse onto the one in flight.
func (d *Discoverer) Refresh(ctx context.Context) ([]Device, error) {
	d.refreshing.Lock()
	defer d.refreshing.Unlock()

	found, err := d.browse(ctx)
	if err != nil {
		return d.Devices(), err
	}

	next := make(map[string]Device, len(found))
	for _, dev := range found {
		next[dev.ID] = dev
	}

	d.mu.Lock()
	d.devices = next
	d.lastRun = time.Now()
	d.mu.Unlock()

	return d.Devices(), nil
}

// browseCastDevices runs one mDNS browse and returns what answered before ctx
// expires.
func browseCastDevices(ctx context.Context) ([]Device, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("cast: creating an mDNS resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry, 32)
	var (
		wg   sync.WaitGroup
		seen []Device
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for entry := range entries {
			if dev, ok := deviceFromEntry(entry); ok {
				seen = append(seen, dev)
			}
		}
	}()

	if err := resolver.Browse(ctx, castService, "local.", entries); err != nil {
		return nil, fmt.Errorf("cast: browsing %s: %w", castService, err)
	}

	<-ctx.Done()
	wg.Wait()
	return seen, nil
}

// deviceFromEntry turns an mDNS answer into a Device, or reports false when the
// record is unusable.
func deviceFromEntry(entry *zeroconf.ServiceEntry) (Device, bool) {
	if entry == nil {
		return Device{}, false
	}

	txt := parseTXT(entry.Text)

	id := txt["id"]
	if id == "" {
		// Without the UUID there is nothing stable to key a selection on.
		log.Debug().Str("instance", entry.Instance).Msg("cast: mDNS record has no id, skipped")
		return Device{}, false
	}

	addr := pickAddr(entry)
	if addr == "" {
		log.Debug().Str("instance", entry.Instance).Msg("cast: mDNS record has no address, skipped")
		return Device{}, false
	}

	port := entry.Port
	if port == 0 {
		port = defaultCastPort
	}

	name := txt["fn"]
	if name == "" {
		name = entry.Instance
	}

	// TXT "md" is the model; a speaker group reports "Google Cast Group".
	model := txt["md"]

	return Device{
		ID:      id,
		Name:    name,
		Model:   model,
		Addr:    net.JoinHostPort(addr, fmt.Sprint(port)),
		IsGroup: strings.Contains(strings.ToLower(model), "group"),
	}, true
}

// pickAddr prefers IPv4.
//
// Cast works over IPv6, but the stream URLs we hand the device are composed
// from the backend's own IPv4 address, so a device reached over v6 would be
// told to fetch from an address it may not route to.
func pickAddr(entry *zeroconf.ServiceEntry) string {
	for _, ip := range entry.AddrIPv4 {
		if ip != nil && !ip.IsUnspecified() {
			return ip.String()
		}
	}
	for _, ip := range entry.AddrIPv6 {
		if ip != nil && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() {
			return ip.String()
		}
	}
	return ""
}

func parseTXT(records []string) map[string]string {
	out := make(map[string]string, len(records))
	for _, rec := range records {
		key, value, found := strings.Cut(rec, "=")
		if !found {
			continue
		}
		out[strings.ToLower(key)] = value
	}
	return out
}
