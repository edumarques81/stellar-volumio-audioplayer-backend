// Package device provides device identity management for Volumio integration.
// This enables Volumio Connect apps to discover and identify this device on the network.
package device

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// DeviceInfo contains the device identity information.
type DeviceInfo struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	Type        string `json:"type"`        // Always "device" for audio players
	ServiceName string `json:"serviceName"` // "Volumio" for mDNS compatibility
}

// Service manages device identity and provides Volumio-compatible device information.
type Service struct {
	mu         sync.RWMutex
	configPath string
	info       DeviceInfo
}

// persistedConfig is the format stored on disk.
type persistedConfig struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// NewService creates a new device service.
// It loads existing configuration from configPath or generates new identity if none exists.
func NewService(configPath string) (*Service, error) {
	svc := &Service{
		configPath: configPath,
		info: DeviceInfo{
			Type:        "device",
			ServiceName: "Volumio",
		},
	}

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Try to load existing config
	if err := svc.loadConfig(); err != nil {
		log.Debug().Err(err).Msg("No existing device config, generating new identity")
		// Generate new identity
		svc.info.UUID = uuid.New().String()
		svc.info.Name = getDefaultDeviceName()

		// Persist immediately
		if err := svc.saveConfig(); err != nil {
			return nil, fmt.Errorf("failed to save device config: %w", err)
		}
	}

	log.Info().
		Str("uuid", svc.info.UUID).
		Str("name", svc.info.Name).
		Msg("Device identity initialized")

	return svc, nil
}

// loadConfig loads device configuration from disk.
func (s *Service) loadConfig() error {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return err
	}

	var cfg persistedConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("invalid config format: %w", err)
	}

	if cfg.UUID == "" {
		return fmt.Errorf("config missing UUID")
	}

	s.info.UUID = cfg.UUID
	s.info.Name = cfg.Name

	if s.info.Name == "" {
		s.info.Name = getDefaultDeviceName()
	}

	return nil
}

// saveConfig persists device configuration to disk.
func (s *Service) saveConfig() error {
	cfg := persistedConfig{
		UUID: s.info.UUID,
		Name: s.info.Name,
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.configPath, data, 0644)
}

// GetDeviceInfo returns the current device information.
func (s *Service) GetDeviceInfo() DeviceInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info
}

// GetMultiRoomDevice returns the device formatted for Volumio's pushMultiRoomDevices response.
// The state parameter should contain current player state (status, volume, artist, track, albumart).
func (s *Service) GetMultiRoomDevice(state map[string]interface{}) map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Extract state fields, providing defaults
	status := getStringFromMap(state, "status", "stop")
	volume := getIntFromMap(state, "volume", 100)
	mute := getBoolFromMap(state, "mute", false)
	artist := getStringFromMap(state, "artist", "")
	track := getStringFromMap(state, "title", "")
	albumart := getStringFromMap(state, "albumart", "")

	return map[string]interface{}{
		"id":              s.info.UUID,
		"host":            fmt.Sprintf("http://localhost:%d", 3000), // Will be overridden with actual host
		"name":            s.info.Name,
		"isSelf":          true,
		"type":            "device",
		"volumeAvailable": true,
		"state": map[string]interface{}{
			"status":   status,
			"volume":   volume,
			"mute":     mute,
			"artist":   artist,
			"track":    track,
			"albumart": albumart,
		},
	}
}

// SetDeviceName updates the device name and persists it.
func (s *Service) SetDeviceName(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.info.Name = name
	return s.saveConfig()
}

// GetUUID returns just the device UUID.
func (s *Service) GetUUID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info.UUID
}

// getDefaultDeviceName returns a default device name.
func getDefaultDeviceName() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "Stellar"
	}
	return hostname
}

// Helper functions to safely extract values from map.
func getStringFromMap(m map[string]interface{}, key, defaultVal string) string {
	if m == nil {
		return defaultVal
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultVal
}

func getIntFromMap(m map[string]interface{}, key string, defaultVal int) int {
	if m == nil {
		return defaultVal
	}
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	}
	return defaultVal
}

func getBoolFromMap(m map[string]interface{}, key string, defaultVal bool) bool {
	if m == nil {
		return defaultVal
	}
	if v, ok := m[key].(bool); ok {
		return v
	}
	return defaultVal
}
