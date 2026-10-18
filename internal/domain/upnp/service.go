package upnp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Service is the top-level UPnP AV Renderer service.
// It manages the HTTP server (device description + SOAP control),
// the SSDP multicast announcer, and bridges playback commands
// to the PlayerController (MPD).
type Service struct {
	config     DeviceConfig
	controller PlayerController
	httpServer *http.Server
	ssdp       *SSDPAnnouncer
	state      TransportState
	current    TrackMetadata
	mu         sync.RWMutex
	cancel     context.CancelFunc

	// MediaServer half, present only when EnableMediaServer was called.
	serverConfig  MediaServerConfig
	library       LibraryProvider
	serverEnabled bool
}

// EnableMediaServer adds a UPnP MediaServer (ContentDirectory) root device
// alongside the renderer, exposing the music library to DLNA control points.
//
// It must be called before Start. The MediaServer shares this service's HTTP
// port and SSDP socket but is advertised as its own root device with its own
// UDN, because a UPnP description document declares exactly one deviceType.
func (s *Service) EnableMediaServer(cfg MediaServerConfig, lib LibraryProvider) {
	s.serverConfig = cfg
	s.library = lib
	s.serverEnabled = cfg.UDN != "" && lib != nil && (cfg.BaseURL != "" || cfg.BackendPort > 0)
	if !s.serverEnabled {
		log.Warn().Msg("UPnP MediaServer not enabled: a UDN, a backend port and a library provider are all required")
	}
}

// NewService creates a new UPnP service with the given config and player controller.
func NewService(cfg DeviceConfig, ctrl PlayerController) *Service {
	return &Service{
		config:     cfg,
		controller: ctrl,
		state:      StateStopped,
	}
}

// Start begins serving UPnP device description, SOAP controls, and SSDP announcements.
// It auto-detects the local IP if not set in the config.
func (s *Service) Start() error {
	if s.config.LocalIP == "" {
		ip, err := detectLocalIP()
		if err != nil {
			return fmt.Errorf("detect local IP: %w", err)
		}
		s.config.LocalIP = ip
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	mux := s.buildMux()

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start HTTP server
	go func() {
		log.Info().
			Str("addr", s.httpServer.Addr).
			Str("ip", s.config.LocalIP).
			Str("name", s.config.FriendlyName).
			Msg("UPnP AV Renderer HTTP server starting")
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Error().Err(err).Msg("UPnP HTTP server error")
		}
	}()

	// Start SSDP announcer
	s.ssdp = NewSSDPAnnouncer(s.config)
	if s.serverEnabled {
		s.ssdp.AddAdvert(MediaServerAdvert(s.serverConfig))
		log.Info().
			Str("udn", s.serverConfig.UDN).
			Str("name", s.serverConfig.FriendlyName).
			Str("baseURL", s.serverConfig.BaseURL).
			Msg("UPnP MediaServer (ContentDirectory) enabled")
	}
	if err := s.ssdp.Start(ctx); err != nil {
		log.Warn().Err(err).Msg("SSDP announcer failed to start — UPnP discovery disabled")
	} else {
		log.Info().
			Str("udn", s.config.UDN).
			Str("name", s.config.FriendlyName).
			Msg("SSDP announcer started")
	}

	return nil
}

// buildMux registers every HTTP endpoint both roles serve on the UPnP port.
//
// It also resolves the MediaServer's BaseURL and enforces the UDN-collision
// rule, so it must run after LocalIP has been detected. Split out of Start so
// the registration and URL composition can be tested without binding a port
// or opening an SSDP socket.
func (s *Service) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	RegisterDeviceHandlers(mux, s.config)
	NewSOAPHandler(s).RegisterHandlers(mux)

	if !s.serverEnabled {
		return mux
	}

	if s.serverConfig.UDN == s.config.UDN {
		// Two root devices sharing one UUID makes control points overwrite
		// one description with the other and then fail every action
		// belonging to the role they discarded. Refuse to advertise the
		// server rather than ship a half-working pair; the renderer, which
		// predates it, keeps working.
		log.Error().Str("udn", s.config.UDN).
			Msg("UPnP MediaServer UDN collides with the renderer UDN — MediaServer disabled")
		s.serverEnabled = false
		return mux
	}

	// The media URLs point at the main backend, so the origin can only be
	// composed once the local IP is known. An explicit BaseURL (tests,
	// unusual topologies) wins.
	if s.serverConfig.BaseURL == "" {
		s.serverConfig.BaseURL = fmt.Sprintf("http://%s:%d", s.config.LocalIP, s.serverConfig.BackendPort)
	}
	RegisterMediaServerHandlers(mux, s.serverConfig)
	NewContentDirectory(s.serverConfig, s.library).RegisterHandlers(mux)
	return mux
}

// Stop gracefully shuts down the HTTP server and sends SSDP byebye.
func (s *Service) Stop() {
	log.Info().Msg("Stopping UPnP AV Renderer")

	if s.cancel != nil {
		s.cancel()
	}

	if s.ssdp != nil {
		s.ssdp.Stop()
	}

	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			log.Debug().Err(err).Msg("UPnP HTTP server shutdown returned an error")
		}
	}
}

// detectLocalIP returns the first non-loopback IPv4 address found.
func detectLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}
	return "127.0.0.1", nil
}
