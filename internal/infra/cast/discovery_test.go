package cast

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/grandcat/zeroconf"
)

func TestDeviceFromEntry(t *testing.T) {
	tests := []struct {
		name  string
		entry *zeroconf.ServiceEntry
		want  Device
		ok    bool
	}{
		{
			name:  "a Nest Hub",
			entry: entryFor("Google-Nest-Hub-abc", 8009, []string{"192.168.86.33"}, "id=abc123", "fn=Living Room Display", "md=Google Nest Hub"),
			want: Device{
				ID:    "abc123",
				Name:  "Living Room Display",
				Model: "Google Nest Hub",
				Addr:  "192.168.86.33:8009",
			},
			ok: true,
		},
		{
			// A speaker group answers mDNS exactly like a device, so
			// multiroom needs no extra protocol — only this flag, so a picker
			// can label it.
			name:  "a speaker group",
			entry: entryFor("Google-Cast-Group-xyz", 8009, []string{"192.168.86.40"}, "id=grp1", "fn=Whole House", "md=Google Cast Group"),
			want: Device{
				ID:      "grp1",
				Name:    "Whole House",
				Model:   "Google Cast Group",
				Addr:    "192.168.86.40:8009",
				IsGroup: true,
			},
			ok: true,
		},
		{
			name:  "TXT keys in upper case",
			entry: entryFor("Device", 8009, []string{"192.168.86.50"}, "ID=upper", "FN=Kitchen", "MD=Nest Mini"),
			want:  Device{ID: "upper", Name: "Kitchen", Model: "Nest Mini", Addr: "192.168.86.50:8009"},
			ok:    true,
		},
		{
			// Without the UUID there is nothing stable for a client to select
			// by — an address changes with the DHCP lease.
			name:  "no id in TXT",
			entry: entryFor("Nameless", 8009, []string{"192.168.86.51"}, "fn=Mystery"),
			ok:    false,
		},
		{
			name:  "no address",
			entry: entryFor("Unreachable", 8009, nil, "id=noaddr", "fn=Ghost"),
			ok:    false,
		},
		{
			name:  "missing port falls back to 8009",
			entry: entryFor("NoPort", 0, []string{"192.168.86.52"}, "id=noport", "fn=Study"),
			want:  Device{ID: "noport", Name: "Study", Addr: "192.168.86.52:8009"},
			ok:    true,
		},
		{
			// A record with no friendly name still beats hiding the device.
			name:  "falls back to the instance name",
			entry: entryFor("Hallway-Speaker", 8009, []string{"192.168.86.53"}, "id=inst"),
			want:  Device{ID: "inst", Name: "Hallway-Speaker", Addr: "192.168.86.53:8009"},
			ok:    true,
		},
		{
			name:  "malformed TXT entries are skipped, not fatal",
			entry: entryFor("Odd", 8009, []string{"192.168.86.54"}, "noequalssign", "id=odd", "fn=Odd One"),
			want:  Device{ID: "odd", Name: "Odd One", Addr: "192.168.86.54:8009"},
			ok:    true,
		},
		{name: "nil entry", entry: nil, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := deviceFromEntry(tt.entry)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("device = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPickAddrPrefersIPv4(t *testing.T) {
	// Stream URLs are composed from the backend's IPv4 address, so a device
	// reached over v6 would be told to fetch from somewhere it may not route.
	entry := &zeroconf.ServiceEntry{
		AddrIPv4: []net.IP{net.ParseIP("192.168.86.33")},
		AddrIPv6: []net.IP{net.ParseIP("fe80::1")},
	}
	if got := pickAddr(entry); got != "192.168.86.33" {
		t.Errorf("pickAddr = %q, want the IPv4 address", got)
	}
}

func TestPickAddrSkipsLinkLocalIPv6(t *testing.T) {
	// A link-local v6 address needs a zone index we do not carry, so it is
	// unusable as a dial target.
	entry := &zeroconf.ServiceEntry{AddrIPv6: []net.IP{net.ParseIP("fe80::1")}}
	if got := pickAddr(entry); got != "" {
		t.Errorf("pickAddr = %q, want empty", got)
	}
}

func TestDiscovererCachesAndSortsByName(t *testing.T) {
	d := &Discoverer{devices: map[string]Device{}}
	d.browse = func(context.Context) ([]Device, error) {
		return []Device{
			{ID: "c", Name: "Study"},
			{ID: "a", Name: "Bedroom"},
			{ID: "b", Name: "Living Room Display"},
		}, nil
	}

	if got := d.Devices(); len(got) != 0 {
		t.Errorf("fresh discoverer returned %d devices", len(got))
	}
	if !d.LastRun().IsZero() {
		t.Error("LastRun should be zero before the first refresh")
	}

	got, err := d.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// A picker that reorders itself between refreshes is unusable.
	wantOrder := []string{"Bedroom", "Living Room Display", "Study"}
	if len(got) != len(wantOrder) {
		t.Fatalf("got %d devices, want %d", len(got), len(wantOrder))
	}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("device %d = %q, want %q", i, got[i].Name, name)
		}
	}
	if d.LastRun().IsZero() {
		t.Error("LastRun not set after a refresh")
	}
}

func TestRefreshDropsDevicesThatStopAnswering(t *testing.T) {
	// A picker offering an unplugged speaker is worse than one that is a
	// browse behind.
	d := &Discoverer{devices: map[string]Device{}}
	round := 0
	d.browse = func(context.Context) ([]Device, error) {
		round++
		if round == 1 {
			return []Device{{ID: "a", Name: "Hub"}, {ID: "b", Name: "Gone"}}, nil
		}
		return []Device{{ID: "a", Name: "Hub"}}, nil
	}

	if _, err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	if _, err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}

	if _, ok := d.Lookup("b"); ok {
		t.Error("a device that stopped answering is still offered")
	}
	if _, ok := d.Lookup("a"); !ok {
		t.Error("a device that kept answering was dropped")
	}
}

func TestRefreshKeepsTheCacheWhenBrowsingFails(t *testing.T) {
	// An mDNS failure is usually transient. Emptying the picker on one bad
	// browse would make casting look broken.
	d := &Discoverer{devices: map[string]Device{}}
	d.browse = func(context.Context) ([]Device, error) {
		return []Device{{ID: "a", Name: "Hub"}}, nil
	}
	if _, err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	d.browse = func(context.Context) ([]Device, error) {
		return nil, errors.New("resolver unavailable")
	}
	got, err := d.Refresh(context.Background())
	if err == nil {
		t.Fatal("Refresh hid a browse failure")
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("cache lost on failure: %+v", got)
	}
}

func TestRefreshIsSafeUnderConcurrency(t *testing.T) {
	d := &Discoverer{devices: map[string]Device{}}
	d.browse = func(context.Context) ([]Device, error) {
		time.Sleep(5 * time.Millisecond)
		return []Device{{ID: "a", Name: "Hub"}}, nil
	}

	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			if _, err := d.Refresh(context.Background()); err != nil {
				t.Errorf("Refresh: %v", err)
			}
			_ = d.Devices()
			_, _ = d.Lookup("a")
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}

	if len(d.Devices()) != 1 {
		t.Errorf("devices = %+v", d.Devices())
	}
}

func TestNewDiscovererUsesRealBrowsing(t *testing.T) {
	d := NewDiscoverer()
	if d.browse == nil {
		t.Fatal("NewDiscoverer left browse unset")
	}
	if len(d.Devices()) != 0 {
		t.Error("NewDiscoverer returned a non-empty cache")
	}
}

// entryFor builds an mDNS answer the way zeroconf hands them to us.
func entryFor(instance string, port int, ipv4 []string, txt ...string) *zeroconf.ServiceEntry {
	entry := &zeroconf.ServiceEntry{
		ServiceRecord: zeroconf.ServiceRecord{Instance: instance},
		Port:          port,
		Text:          txt,
	}
	for _, ip := range ipv4 {
		entry.AddrIPv4 = append(entry.AddrIPv4, net.ParseIP(ip))
	}
	return entry
}
