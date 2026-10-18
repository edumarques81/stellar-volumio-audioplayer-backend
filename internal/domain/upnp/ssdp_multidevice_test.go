package upnp

import (
	"strings"
	"testing"
)

func testDeviceConfig() DeviceConfig {
	return DeviceConfig{
		FriendlyName: "Stellar Audio",
		UDN:          "renderer-uuid",
		Port:         9180,
		LocalIP:      "192.168.86.25",
	}
}

func TestMediaServerAdvert(t *testing.T) {
	ad := MediaServerAdvert(MediaServerConfig{UDN: "server-uuid", BackendPort: 3000})

	if ad.UDN != "server-uuid" {
		t.Errorf("UDN = %q", ad.UDN)
	}
	if ad.DescPath != MediaServerDescPath {
		t.Errorf("DescPath = %q, want %q", ad.DescPath, MediaServerDescPath)
	}
	if ad.DeviceType != "urn:schemas-upnp-org:device:MediaServer:1" {
		t.Errorf("DeviceType = %q", ad.DeviceType)
	}

	// A root device that does not announce upnp:rootdevice is invisible to
	// the clients that search for that ST specifically.
	want := map[string]bool{
		"upnp:rootdevice": false,
		"urn:schemas-upnp-org:device:MediaServer:1":        false,
		"urn:schemas-upnp-org:service:ContentDirectory:1":  false,
		"urn:schemas-upnp-org:service:ConnectionManager:1": false,
	}
	for _, nt := range ad.NotifyTypes {
		if _, ok := want[nt]; ok {
			want[nt] = true
		}
	}
	for nt, found := range want {
		if !found {
			t.Errorf("MediaServer advert does not announce %s", nt)
		}
	}
}

func TestAnnouncerAdvertisesBothDevices(t *testing.T) {
	cfg := testDeviceConfig()
	a := NewSSDPAnnouncer(cfg)
	a.AddAdvert(MediaServerAdvert(MediaServerConfig{UDN: "server-uuid", BackendPort: 3000}))

	if len(a.adverts) != 2 {
		t.Fatalf("got %d adverts, want 2", len(a.adverts))
	}

	// The two devices must point clients at *different* description
	// documents, or whichever is fetched second wins and one role disappears.
	renderer := a.adverts[0].descURL(cfg.LocalIP, cfg.Port)
	server := a.adverts[1].descURL(cfg.LocalIP, cfg.Port)
	if renderer == server {
		t.Fatalf("both adverts share LOCATION %q", renderer)
	}
	if renderer != "http://192.168.86.25:9180/upnp/description.xml" {
		t.Errorf("renderer LOCATION = %q", renderer)
	}
	if server != "http://192.168.86.25:9180"+MediaServerDescPath {
		t.Errorf("server LOCATION = %q", server)
	}

	// Distinct UDNs are load-bearing: a control point keys its device cache
	// on the UDN, so one UUID announced as both roles makes it keep only one.
	if a.adverts[0].UDN == a.adverts[1].UDN {
		t.Error("renderer and server share a UDN")
	}
}

func TestMatchesSearchTargetCoversBothDevices(t *testing.T) {
	a := NewSSDPAnnouncer(testDeviceConfig())
	a.AddAdvert(MediaServerAdvert(MediaServerConfig{UDN: "server-uuid"}))

	tests := []struct {
		name  string
		st    string
		match bool
	}{
		{"ssdp:all", "ssdp:all", true},
		{"renderer device", "urn:schemas-upnp-org:device:MediaRenderer:1", true},
		{"server device", "urn:schemas-upnp-org:device:MediaServer:1", true},
		{"content directory", "urn:schemas-upnp-org:service:ContentDirectory:1", true},
		{"av transport", "urn:schemas-upnp-org:service:AVTransport:1", true},
		{"server uuid", "uuid:server-uuid", true},
		{"renderer uuid", "uuid:renderer-uuid", true},
		{"unrelated", "urn:schemas-upnp-org:device:InternetGatewayDevice:1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := "M-SEARCH * HTTP/1.1\r\nST: " + tt.st + "\r\n"
			if got := a.matchesSearchTarget(msg); got != tt.match {
				t.Errorf("matchesSearchTarget(%q) = %v, want %v", tt.st, got, tt.match)
			}
		})
	}
}

func TestZeroValueAnnouncerStillMatchesRendererSearches(t *testing.T) {
	// soap_test.go and ssdp_test.go construct &SSDPAnnouncer{} directly.
	// Falling back to the renderer's type list keeps that usable.
	a := &SSDPAnnouncer{}
	if !a.matchesSearchTarget("M-SEARCH * HTTP/1.1\r\nST: urn:schemas-upnp-org:device:MediaRenderer:1\r\n") {
		t.Error("zero-value announcer does not match a renderer search")
	}
}

func TestUSNFor(t *testing.T) {
	tests := []struct {
		udn, nt, want string
	}{
		{"abc", "upnp:rootdevice", "uuid:abc::upnp:rootdevice"},
		{"abc", "urn:schemas-upnp-org:device:MediaServer:1", "uuid:abc::urn:schemas-upnp-org:device:MediaServer:1"},
		// When the NT *is* the UUID there is no "::" suffix.
		{"abc", "uuid:abc", "uuid:abc"},
	}
	for _, tt := range tests {
		if got := usnFor(tt.udn, tt.nt); got != tt.want {
			t.Errorf("usnFor(%q, %q) = %q, want %q", tt.udn, tt.nt, got, tt.want)
		}
	}
}

func TestBuildNotifyAliveForMediaServer(t *testing.T) {
	cfg := testDeviceConfig()
	ad := MediaServerAdvert(MediaServerConfig{UDN: "server-uuid"})
	msg := BuildNotifyAliveFor(cfg, ad, "urn:schemas-upnp-org:device:MediaServer:1")

	required := []string{
		"NOTIFY * HTTP/1.1",
		"LOCATION: http://192.168.86.25:9180" + MediaServerDescPath,
		"NT: urn:schemas-upnp-org:device:MediaServer:1",
		"NTS: ssdp:alive",
		"USN: uuid:server-uuid::urn:schemas-upnp-org:device:MediaServer:1",
	}
	for _, h := range required {
		if !strings.Contains(msg, h) {
			t.Errorf("MediaServer NOTIFY missing %q in:\n%s", h, msg)
		}
	}
	// The renderer's UUID must not leak into the server's announcement.
	if strings.Contains(msg, "renderer-uuid") {
		t.Errorf("MediaServer NOTIFY carries the renderer UDN:\n%s", msg)
	}
}
