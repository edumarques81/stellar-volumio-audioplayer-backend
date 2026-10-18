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

// ssdpDeviceTypes lists the notification types the MediaRenderer announces.
var ssdpDeviceTypes = []string{
	"upnp:rootdevice",
	"urn:schemas-upnp-org:device:MediaRenderer:1",
	"urn:schemas-upnp-org:service:AVTransport:1",
	"urn:schemas-upnp-org:service:RenderingControl:1",
	"urn:schemas-upnp-org:service:ConnectionManager:1",
}

// SSDPAdvert describes one UPnP root device to announce.
//
// Stellar hosts two root devices in one process — a MediaRenderer (so control
// points can push audio *to* the appliance) and a MediaServer (so they can
// browse the library *on* it). They are separate devices with separate UDNs
// and separate description documents, but they share one SSDP socket: binding
// port 1900 twice is needlessly fragile, and one listener answering for both
// is what the protocol expects anyway.
type SSDPAdvert struct {
	// UDN is this device's UUID, without the "uuid:" prefix.
	UDN string
	// DeviceType is the device's primary type URN, used as the ST of an
	// M-SEARCH response when the search was for ssdp:all.
	DeviceType string
	// DescPath is the path of this device's description document.
	DescPath string
	// NotifyTypes are the NT values announced for this device. It must
	// include upnp:rootdevice, the device type, and each service type.
	NotifyTypes []string
}

// descURL builds this advert's absolute LOCATION header value.
func (ad SSDPAdvert) descURL(localIP string, port int) string {
	return fmt.Sprintf("http://%s:%d%s", localIP, port, ad.DescPath)
}

// SSDPAnnouncer sends periodic SSDP NOTIFY messages and responds to M-SEARCH
// on behalf of every advertised device.
type SSDPAnnouncer struct {
	cfg           DeviceConfig
	adverts       []SSDPAdvert
	conn          *net.UDPConn
	multicastAddr *net.UDPAddr
}

// rendererAdvert is the MediaRenderer advert implied by a DeviceConfig.
func rendererAdvert(cfg DeviceConfig) SSDPAdvert {
	return SSDPAdvert{
		UDN:         cfg.UDN,
		DeviceType:  "urn:schemas-upnp-org:device:MediaRenderer:1",
		DescPath:    "/upnp/description.xml",
		NotifyTypes: ssdpDeviceTypes,
	}
}

// NewSSDPAnnouncer creates an announcer for the MediaRenderer alone.
func NewSSDPAnnouncer(cfg DeviceConfig) *SSDPAnnouncer {
	return &SSDPAnnouncer{cfg: cfg, adverts: []SSDPAdvert{rendererAdvert(cfg)}}
}

// AddAdvert registers an additional root device to announce. It must be called
// before Start.
func (a *SSDPAnnouncer) AddAdvert(ad SSDPAdvert) {
	a.adverts = append(a.adverts, ad)
}

// Start begins the SSDP announcement loop and M-SEARCH listener.
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
		if err := a.conn.Close(); err != nil {
			log.Debug().Err(err).Msg("SSDP socket close failed")
		}
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

		if err := a.conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return
		}
		n, addr, err := a.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		msg := string(buf[:n])
		if !strings.Contains(msg, "M-SEARCH") {
			continue
		}

		a.respondToMSearch(addr, msg)
	}
}

// matchesSearchTarget reports whether the M-SEARCH matches any advertised
// device. Retained as the single predicate used to decide "is this for us".
func (a *SSDPAnnouncer) matchesSearchTarget(msg string) bool {
	if strings.Contains(msg, "ssdp:all") {
		return true
	}
	for _, nt := range a.notifyTypes() {
		if strings.Contains(msg, nt) {
			return true
		}
	}
	return false
}

// notifyTypes is the union of every advert's notification types. When no
// adverts are configured it falls back to the renderer's set, so a
// zero-value announcer still answers the searches the renderer cares about.
func (a *SSDPAnnouncer) notifyTypes() []string {
	if len(a.adverts) == 0 {
		return ssdpDeviceTypes
	}
	var out []string
	for _, ad := range a.adverts {
		out = append(out, ad.NotifyTypes...)
		out = append(out, "uuid:"+ad.UDN)
	}
	return out
}

// respondToMSearch sends one unicast response per matching (device, ST) pair.
//
// Responses must echo the ST the client searched for, not our own device type:
// a control point looking for MediaServer:1 discards a reply whose ST says
// MediaRenderer:1, which is exactly how a two-role device ends up visible in
// only one of a client's two lists.
func (a *SSDPAnnouncer) respondToMSearch(addr *net.UDPAddr, msg string) {
	searchAll := strings.Contains(msg, "ssdp:all")

	for _, ad := range a.adverts {
		if searchAll {
			for _, nt := range ad.NotifyTypes {
				a.sendSearchResponse(addr, ad, nt)
			}
			a.sendSearchResponse(addr, ad, "uuid:"+ad.UDN)
			continue
		}
		for _, nt := range ad.NotifyTypes {
			if strings.Contains(msg, nt) {
				a.sendSearchResponse(addr, ad, nt)
			}
		}
		if strings.Contains(msg, "uuid:"+ad.UDN) {
			a.sendSearchResponse(addr, ad, "uuid:"+ad.UDN)
		}
	}
}

// sendSearchResponse sends a single unicast M-SEARCH response.
func (a *SSDPAnnouncer) sendSearchResponse(addr *net.UDPAddr, ad SSDPAdvert, st string) {
	if a.conn == nil {
		return
	}
	response := "HTTP/1.1 200 OK\r\n" +
		"CACHE-CONTROL: max-age=" + fmt.Sprintf("%d", ssdpMaxAge) + "\r\n" +
		"EXT:\r\n" +
		"LOCATION: " + ad.descURL(a.cfg.LocalIP, a.cfg.Port) + "\r\n" +
		"SERVER: Linux/1.0 UPnP/1.0 Stellar/1.0\r\n" +
		"ST: " + st + "\r\n" +
		"USN: " + usnFor(ad.UDN, st) + "\r\n" +
		"\r\n"

	if _, err := a.conn.WriteToUDP([]byte(response), addr); err != nil {
		log.Debug().Err(err).Str("st", st).Msg("Failed to send M-SEARCH response")
		return
	}
	log.Debug().Str("addr", addr.String()).Str("st", st).Msg("Responded to M-SEARCH")
}

// sendAlive sends NOTIFY ssdp:alive for every advertised device and type.
func (a *SSDPAnnouncer) sendAlive() {
	a.notifyAll("ssdp:alive")
	log.Debug().Int("devices", len(a.adverts)).Msg("Sent SSDP alive announcements")
}

// sendByeBye sends NOTIFY ssdp:byebye for every advertised device and type.
func (a *SSDPAnnouncer) sendByeBye() {
	a.notifyAll("ssdp:byebye")
	log.Debug().Int("devices", len(a.adverts)).Msg("Sent SSDP byebye announcements")
}

func (a *SSDPAnnouncer) notifyAll(nts string) {
	for _, ad := range a.adverts {
		for _, nt := range ad.NotifyTypes {
			a.sendNotify(ad, nt, nts)
		}
		// Also announce the device UUID itself
		a.sendNotify(ad, "uuid:"+ad.UDN, nts)
	}
}

// sendNotify sends a single SSDP NOTIFY message.
func (a *SSDPAnnouncer) sendNotify(ad SSDPAdvert, nt, nts string) {
	if a.conn == nil || a.multicastAddr == nil {
		return
	}

	msg := "NOTIFY * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"CACHE-CONTROL: max-age=" + fmt.Sprintf("%d", ssdpMaxAge) + "\r\n" +
		"LOCATION: " + ad.descURL(a.cfg.LocalIP, a.cfg.Port) + "\r\n" +
		"NT: " + nt + "\r\n" +
		"NTS: " + nts + "\r\n" +
		"SERVER: Linux/1.0 UPnP/1.0 Stellar/1.0\r\n" +
		"USN: " + usnFor(ad.UDN, nt) + "\r\n" +
		"\r\n"

	if _, err := a.conn.WriteToUDP([]byte(msg), a.multicastAddr); err != nil {
		log.Debug().Err(err).Str("nt", nt).Str("nts", nts).Msg("Failed to send SSDP NOTIFY")
	}
}

// usnFor builds the USN header for a given UDN and notification type. When the
// type *is* the UUID, the USN is the bare UUID with no "::" suffix.
func usnFor(udn, nt string) string {
	usn := "uuid:" + udn
	if nt == usn {
		return usn
	}
	return usn + "::" + nt
}

// BuildNotifyAlive builds a NOTIFY ssdp:alive message for the given
// notification type. Exported for testing.
func BuildNotifyAlive(cfg DeviceConfig, nt string) string {
	return BuildNotifyAliveFor(cfg, rendererAdvert(cfg), nt)
}

// BuildNotifyAliveFor builds a NOTIFY ssdp:alive message for a specific
// advert. Exported for testing.
func BuildNotifyAliveFor(cfg DeviceConfig, ad SSDPAdvert, nt string) string {
	return "NOTIFY * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"CACHE-CONTROL: max-age=" + fmt.Sprintf("%d", ssdpMaxAge) + "\r\n" +
		"LOCATION: " + ad.descURL(cfg.LocalIP, cfg.Port) + "\r\n" +
		"NT: " + nt + "\r\n" +
		"NTS: ssdp:alive\r\n" +
		"SERVER: Linux/1.0 UPnP/1.0 Stellar/1.0\r\n" +
		"USN: " + usnFor(ad.UDN, nt) + "\r\n" +
		"\r\n"
}

// MediaServerAdvert builds the SSDP advert for the MediaServer root device.
func MediaServerAdvert(cfg MediaServerConfig) SSDPAdvert {
	return SSDPAdvert{
		UDN:         cfg.UDN,
		DeviceType:  "urn:schemas-upnp-org:device:MediaServer:1",
		DescPath:    MediaServerDescPath,
		NotifyTypes: mediaServerNotifyTypes,
	}
}
