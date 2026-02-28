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

	// Build HTTP mux
	mux := http.NewServeMux()
	RegisterDeviceHandlers(mux, s.config)
	NewSOAPHandler(s).RegisterHandlers(mux)

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
		s.httpServer.Shutdown(ctx)
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
