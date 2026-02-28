package upnp

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	ssdpAddr     = "239.255.255.250:1900"
	ssdpMaxAge   = 1800
	ssdpInterval = 60 * time.Second
)

// ssdpDeviceTypes lists the notification types we announce.
var ssdpDeviceTypes = []string{
	"upnp:rootdevice",
	"urn:schemas-upnp-org:device:MediaRenderer:1",
	"urn:schemas-upnp-org:service:AVTransport:1",
	"urn:schemas-upnp-org:service:RenderingControl:1",
	"urn:schemas-upnp-org:service:ConnectionManager:1",
}

// SSDPAnnouncer sends periodic SSDP NOTIFY messages and responds to M-SEARCH.
type SSDPAnnouncer struct {
	cfg           DeviceConfig
	descURL       string
	conn          *net.UDPConn
	multicastAddr *net.UDPAddr
}

// NewSSDPAnnouncer creates a new SSDP announcer for the given device config.
func NewSSDPAnnouncer(cfg DeviceConfig) *SSDPAnnouncer {
	return &SSDPAnnouncer{
		cfg:     cfg,
		descURL: fmt.Sprintf("http://%s:%d/upnp/description.xml", cfg.LocalIP, cfg.Port),
	}
}

// Start begins the SSDP announcement loop and M-SEARCH listener.
// It blocks until ctx is cancelled, then sends ssdp:byebye.
func (a *SSDPAnnouncer) Start(ctx context.Context) error {
	maddr, err := net.ResolveUDPAddr("udp4", ssdpAddr)
	if err != nil {
		return fmt.Errorf("resolve SSDP multicast addr: %w", err)
	}
	a.multicastAddr = maddr

	// Bind to 0.0.0.0:1900 for receiving M-SEARCH
	conn, err := net.ListenMulticastUDP("udp4", nil, maddr)
	if err != nil {
		// If port 1900 is busy, open a random port for sending only
		log.Warn().Err(err).Msg("Cannot bind to SSDP multicast port 1900, using send-only mode")
		sendConn, err2 := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err2 != nil {
			return fmt.Errorf("open UDP socket: %w", err2)
		}
		a.conn = sendConn
		go a.announceLoop(ctx)
		return nil
	}

	a.conn = conn

	// Send initial alive
	a.sendAlive()

	// Start M-SEARCH listener
	go a.listenMSearch(ctx)

	// Start periodic announcements
	go a.announceLoop(ctx)

	return nil
}

// Stop sends ssdp:byebye and closes the connection.
func (a *SSDPAnnouncer) Stop() {
	a.sendByeBye()
	if a.conn != nil {
		a.conn.Close()
	}
}

// announceLoop sends ssdp:alive periodically and byebye on context cancellation.
func (a *SSDPAnnouncer) announceLoop(ctx context.Context) {
	ticker := time.NewTicker(ssdpInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.sendByeBye()
			return
		case <-ticker.C:
			a.sendAlive()
		}
	}
}

// listenMSearch listens for M-SEARCH requests and responds.
func (a *SSDPAnnouncer) listenMSearch(ctx context.Context) {
	buf := make([]byte, 2048)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		a.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, addr, err := a.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		msg := string(buf[:n])
		if !strings.Contains(msg, "M-SEARCH") {
			continue
		}

		// Check if the search target matches any of our device types
		if a.matchesSearchTarget(msg) {
			a.respondToMSearch(addr)
		}
	}
}

// matchesSearchTarget checks if the M-SEARCH request matches our device.
func (a *SSDPAnnouncer) matchesSearchTarget(msg string) bool {
	if strings.Contains(msg, "ssdp:all") {
		return true
	}
	for _, dt := range ssdpDeviceTypes {
		if strings.Contains(msg, dt) {
			return true
		}
	}
	return false
}

// respondToMSearch sends a unicast M-SEARCH response.
func (a *SSDPAnnouncer) respondToMSearch(addr *net.UDPAddr) {
	usn := "uuid:" + a.cfg.UDN + "::urn:schemas-upnp-org:device:MediaRenderer:1"
	response := "HTTP/1.1 200 OK\r\n" +
		"CACHE-CONTROL: max-age=" + fmt.Sprintf("%d", ssdpMaxAge) + "\r\n" +
		"LOCATION: " + a.descURL + "\r\n" +
		"SERVER: Linux/1.0 UPnP/1.0 Stellar/1.0\r\n" +
		"ST: urn:schemas-upnp-org:device:MediaRenderer:1\r\n" +
		"USN: " + usn + "\r\n" +
		"EXT:\r\n" +
		"\r\n"

	a.conn.WriteToUDP([]byte(response), addr)
	log.Debug().Str("addr", addr.String()).Msg("Responded to M-SEARCH")
}

// sendAlive sends NOTIFY ssdp:alive for all device types.
func (a *SSDPAnnouncer) sendAlive() {
	for _, nt := range ssdpDeviceTypes {
		a.sendNotify(nt, "ssdp:alive")
	}
	// Also announce the device UUID itself
	a.sendNotify("uuid:"+a.cfg.UDN, "ssdp:alive")
	log.Debug().Msg("Sent SSDP alive announcements")
}

// sendByeBye sends NOTIFY ssdp:byebye for all device types.
func (a *SSDPAnnouncer) sendByeBye() {
	for _, nt := range ssdpDeviceTypes {
		a.sendNotify(nt, "ssdp:byebye")
	}
	a.sendNotify("uuid:"+a.cfg.UDN, "ssdp:byebye")
	log.Debug().Msg("Sent SSDP byebye announcements")
}

// sendNotify sends a single SSDP NOTIFY message.
func (a *SSDPAnnouncer) sendNotify(nt, nts string) {
	if a.conn == nil || a.multicastAddr == nil {
		return
	}

	usn := "uuid:" + a.cfg.UDN
	if nt != usn {
		usn = usn + "::" + nt
	}

	msg := "NOTIFY * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"CACHE-CONTROL: max-age=" + fmt.Sprintf("%d", ssdpMaxAge) + "\r\n" +
		"LOCATION: " + a.descURL + "\r\n" +
		"NT: " + nt + "\r\n" +
		"NTS: " + nts + "\r\n" +
		"SERVER: Linux/1.0 UPnP/1.0 Stellar/1.0\r\n" +
		"USN: " + usn + "\r\n" +
		"\r\n"

	_, err := a.conn.WriteToUDP([]byte(msg), a.multicastAddr)
	if err != nil {
		log.Debug().Err(err).Str("nt", nt).Str("nts", nts).Msg("Failed to send SSDP NOTIFY")
	}
}

// BuildNotifyAlive builds a NOTIFY ssdp:alive message for the given notification type.
// Exported for testing.
func BuildNotifyAlive(cfg DeviceConfig, nt string) string {
	descURL := fmt.Sprintf("http://%s:%d/upnp/description.xml", cfg.LocalIP, cfg.Port)
	usn := "uuid:" + cfg.UDN
	if nt != usn {
		usn = usn + "::" + nt
	}
	return "NOTIFY * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"CACHE-CONTROL: max-age=" + fmt.Sprintf("%d", ssdpMaxAge) + "\r\n" +
		"LOCATION: " + descURL + "\r\n" +
		"NT: " + nt + "\r\n" +
		"NTS: ssdp:alive\r\n" +
		"SERVER: Linux/1.0 UPnP/1.0 Stellar/1.0\r\n" +
		"USN: " + usn + "\r\n" +
		"\r\n"
}
