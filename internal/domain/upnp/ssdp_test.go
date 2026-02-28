package upnp

import (
	"strings"
	"testing"
)

func TestBuildNotifyAlive(t *testing.T) {
	cfg := DeviceConfig{
		FriendlyName: "Stellar Audio",
		UDN:          "12345678-1234-1234-1234-123456789abc",
		Port:         9180,
		LocalIP:      "192.168.1.10",
	}

	msg := BuildNotifyAlive(cfg, "urn:schemas-upnp-org:device:MediaRenderer:1")

	// Validate required SSDP headers
	requiredHeaders := []string{
		"NOTIFY * HTTP/1.1",
		"HOST: 239.255.255.250:1900",
		"CACHE-CONTROL: max-age=",
		"LOCATION: http://192.168.1.10:9180/upnp/description.xml",
		"NT: urn:schemas-upnp-org:device:MediaRenderer:1",
		"NTS: ssdp:alive",
		"USN: uuid:12345678-1234-1234-1234-123456789abc::urn:schemas-upnp-org:device:MediaRenderer:1",
		"SERVER: ",
	}

	for _, h := range requiredHeaders {
		if !strings.Contains(msg, h) {
			t.Errorf("SSDP NOTIFY missing %q in:\n%s", h, msg)
		}
	}

	// Validate CRLF line endings
	lines := strings.Split(msg, "\r\n")
	if len(lines) < 8 {
		t.Errorf("expected at least 8 lines, got %d", len(lines))
	}

	// Must end with blank line (empty string after final \r\n)
	if lines[len(lines)-1] != "" {
		t.Errorf("SSDP message must end with blank line")
	}
}

func TestBuildNotifyAliveUUIDOnly(t *testing.T) {
	cfg := DeviceConfig{
		UDN:     "test-uuid",
		Port:    9180,
		LocalIP: "10.0.0.1",
	}

	msg := BuildNotifyAlive(cfg, "uuid:test-uuid")

	// When NT matches the UUID, USN should NOT have "::" suffix
	if !strings.Contains(msg, "USN: uuid:test-uuid\r\n") {
		t.Errorf("USN should be plain UUID when NT matches UUID:\n%s", msg)
	}
}

func TestSSDPDeviceTypes(t *testing.T) {
	// Ensure we announce all required types for Audirvana discovery
	required := map[string]bool{
		"upnp:rootdevice":                                       false,
		"urn:schemas-upnp-org:device:MediaRenderer:1":           false,
		"urn:schemas-upnp-org:service:AVTransport:1":            false,
		"urn:schemas-upnp-org:service:RenderingControl:1":       false,
		"urn:schemas-upnp-org:service:ConnectionManager:1":      false,
	}

	for _, dt := range ssdpDeviceTypes {
		if _, ok := required[dt]; ok {
			required[dt] = true
		}
	}

	for dt, found := range required {
		if !found {
			t.Errorf("missing required SSDP device type: %s", dt)
		}
	}
}

func TestMatchesSearchTarget(t *testing.T) {
	a := &SSDPAnnouncer{}

	tests := []struct {
		name  string
		msg   string
		match bool
	}{
		{"ssdp:all", "M-SEARCH * HTTP/1.1\r\nST: ssdp:all\r\n", true},
		{"MediaRenderer", "M-SEARCH * HTTP/1.1\r\nST: urn:schemas-upnp-org:device:MediaRenderer:1\r\n", true},
		{"AVTransport", "M-SEARCH * HTTP/1.1\r\nST: urn:schemas-upnp-org:service:AVTransport:1\r\n", true},
		{"unrelated", "M-SEARCH * HTTP/1.1\r\nST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.matchesSearchTarget(tt.msg)
			if got != tt.match {
				t.Errorf("matchesSearchTarget() = %v, want %v", got, tt.match)
			}
		})
	}
}
