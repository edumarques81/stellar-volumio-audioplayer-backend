package sources

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const (
	// NasMountBase is the base directory for NAS mounts.
	NasMountBase = "/mnt/NAS"
	// UsbMountBase is the base directory for USB mounts.
	UsbMountBase = "/mnt/USB"
	// MpdMusicDir is the MPD music directory.
	MpdMusicDir = "/var/lib/mpd/music"
)

// Service manages music sources (NAS and USB).
type Service struct {
	config     *Config
	configPath string
	mounter    Mounter
	discoverer Discoverer
	mu         sync.RWMutex
}

// NewService creates a new sources service.
func NewService(configPath string, mounter Mounter) (*Service, error) {
	s := &Service{
		configPath: configPath,
		mounter:    mounter,
		config: &Config{
			NasShares: make(map[string]*NasShareConfig),
		},
	}

	// Load existing config if it exists
	if err := s.loadConfig(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return s, nil
}

// loadConfig loads the configuration from disk.
func (s *Service) loadConfig() error {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	if config.NasShares == nil {
		config.NasShares = make(map[string]*NasShareConfig)
	}

	s.config = &config
	return nil
}

// saveConfig saves the configuration to disk.
func (s *Service) saveConfig() error {
	// Ensure directory exists
	dir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(s.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(s.configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// AddNasShare adds and mounts a new NAS share.
func (s *Service) AddNasShare(req AddNasShareRequest) (*SourceResult, error) {
	// Validate request
	if err := validateAddNasShareRequest(req); err != nil {
		return &SourceResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate ID and mount point
	id := uuid.New().String()
	mountPoint := filepath.Join(NasMountBase, sanitizeName(req.Name))

	// Create share config
	shareConfig := &NasShareConfig{
		ID:       id,
		Name:     req.Name,
		IP:       req.IP,
		Path:     req.Path,
		FSType:   req.FSType,
		Username: req.Username,
		Options:  req.Options,
	}

	// Encrypt password if provided
	if req.Password != "" {
		shareConfig.EncryptedPassword = encryptPassword(req.Password)
	}

	// Create share for mounting
	share := &NasShare{
		ID:         id,
		Name:       req.Name,
		IP:         req.IP,
		Path:       req.Path,
		FSType:     req.FSType,
		Username:   req.Username,
		Password:   req.Password,
		Options:    req.Options,
		MountPoint: mountPoint,
		Mounted:    false,
	}

	// Mount the share if mounter is available
	if s.mounter != nil {
		if err := s.mounter.CreateMountPoint(mountPoint); err != nil {
			return &SourceResult{
				Success: false,
				Error:   fmt.Sprintf("failed to create mount point: %v", err),
			}, nil
		}

		if err := s.mounter.Mount(share); err != nil {
			// Clean up mount point on failure
			s.mounter.RemoveMountPoint(mountPoint)
			return &SourceResult{
				Success: false,
				Error:   fmt.Sprintf("failed to mount share: %v", err),
			}, nil
		}

		// Create symlink in MPD music directory
		symlinkPath := filepath.Join(MpdMusicDir, "NAS", sanitizeName(req.Name))
		if err := s.mounter.CreateSymlink(mountPoint, symlinkPath); err != nil {
			// Log but don't fail - symlink is not critical
		}
	}

	// Save to config
	s.config.NasShares[id] = shareConfig

	if err := s.saveConfig(); err != nil {
		return &SourceResult{
			Success: false,
			Error:   fmt.Sprintf("failed to save config: %v", err),
		}, nil
	}

	return &SourceResult{
		Success: true,
		Message: fmt.Sprintf("NAS share '%s' added successfully", req.Name),
	}, nil
}

// ListNasShares returns all configured NAS shares.
func (s *Service) ListNasShares() ([]NasShare, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	shares := make([]NasShare, 0, len(s.config.NasShares))

	for _, cfg := range s.config.NasShares {
		mountPoint := filepath.Join(NasMountBase, sanitizeName(cfg.Name))
		mounted := false
		if s.mounter != nil {
			mounted = s.mounter.IsMounted(mountPoint)
		}

		shares = append(shares, NasShare{
			ID:         cfg.ID,
			Name:       cfg.Name,
			IP:         cfg.IP,
			Path:       cfg.Path,
			FSType:     cfg.FSType,
			Username:   cfg.Username,
			Options:    cfg.Options,
			MountPoint: mountPoint,
			Mounted:    mounted,
		})
	}

	return shares, nil
}

// GetNasShareInfo returns information about a specific NAS share.
func (s *Service) GetNasShareInfo(id string) (*NasShare, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg, exists := s.config.NasShares[id]
	if !exists {
		return nil, fmt.Errorf("share not found: %s", id)
	}

	mountPoint := filepath.Join(NasMountBase, sanitizeName(cfg.Name))
	mounted := false
	if s.mounter != nil {
		mounted = s.mounter.IsMounted(mountPoint)
	}

	return &NasShare{
		ID:         cfg.ID,
		Name:       cfg.Name,
		IP:         cfg.IP,
		Path:       cfg.Path,
		FSType:     cfg.FSType,
		Username:   cfg.Username,
		Options:    cfg.Options,
		MountPoint: mountPoint,
		Mounted:    mounted,
	}, nil
}

// DeleteNasShare unmounts and removes a NAS share.
func (s *Service) DeleteNasShare(id string) (*SourceResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, exists := s.config.NasShares[id]
	if !exists {
		return &SourceResult{
			Success: false,
			Error:   "share not found",
		}, nil
	}

	mountPoint := filepath.Join(NasMountBase, sanitizeName(cfg.Name))

	// Unmount if mounted
	if s.mounter != nil && s.mounter.IsMounted(mountPoint) {
		if err := s.mounter.Unmount(mountPoint); err != nil {
			return &SourceResult{
				Success: false,
				Error:   fmt.Sprintf("failed to unmount: %v", err),
			}, nil
		}
	}

	// Remove symlink
	if s.mounter != nil {
		symlinkPath := filepath.Join(MpdMusicDir, "NAS", sanitizeName(cfg.Name))
		s.mounter.RemoveSymlink(symlinkPath)
	}

	// Remove mount point
	if s.mounter != nil {
		s.mounter.RemoveMountPoint(mountPoint)
	}

	// Remove from config
	delete(s.config.NasShares, id)

	if err := s.saveConfig(); err != nil {
		return &SourceResult{
			Success: false,
			Error:   fmt.Sprintf("failed to save config: %v", err),
		}, nil
	}

	return &SourceResult{
		Success: true,
		Message: fmt.Sprintf("NAS share '%s' removed successfully", cfg.Name),
	}, nil
}

// MountNasShare mounts an existing NAS share.
func (s *Service) MountNasShare(id string) (*SourceResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, exists := s.config.NasShares[id]
	if !exists {
		return &SourceResult{
			Success: false,
			Error:   "share not found",
		}, nil
	}

	if s.mounter == nil {
		return &SourceResult{
			Success: false,
			Error:   "mounter not available",
		}, nil
	}

	mountPoint := filepath.Join(NasMountBase, sanitizeName(cfg.Name))

	// Check if already mounted
	if s.mounter.IsMounted(mountPoint) {
		return &SourceResult{
			Success: true,
			Message: "share is already mounted",
		}, nil
	}

	// Create share for mounting
	share := &NasShare{
		ID:         cfg.ID,
		Name:       cfg.Name,
		IP:         cfg.IP,
		Path:       cfg.Path,
		FSType:     cfg.FSType,
		Username:   cfg.Username,
		Password:   decryptPassword(cfg.EncryptedPassword),
		Options:    cfg.Options,
		MountPoint: mountPoint,
	}

	// Ensure mount point exists
	if err := s.mounter.CreateMountPoint(mountPoint); err != nil {
		return &SourceResult{
			Success: false,
			Error:   fmt.Sprintf("failed to create mount point: %v", err),
		}, nil
	}

	// Mount
	if err := s.mounter.Mount(share); err != nil {
		return &SourceResult{
			Success: false,
			Error:   fmt.Sprintf("failed to mount: %v", err),
		}, nil
	}

	return &SourceResult{
		Success: true,
		Message: fmt.Sprintf("NAS share '%s' mounted successfully", cfg.Name),
	}, nil
}

// UnmountNasShare unmounts a NAS share.
func (s *Service) UnmountNasShare(id string) (*SourceResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, exists := s.config.NasShares[id]
	if !exists {
		return &SourceResult{
			Success: false,
			Error:   "share not found",
		}, nil
	}

	if s.mounter == nil {
		return &SourceResult{
			Success: false,
			Error:   "mounter not available",
		}, nil
	}

	mountPoint := filepath.Join(NasMountBase, sanitizeName(cfg.Name))

	if !s.mounter.IsMounted(mountPoint) {
		return &SourceResult{
			Success: true,
			Message: "share is not mounted",
		}, nil
	}

	if err := s.mounter.Unmount(mountPoint); err != nil {
		return &SourceResult{
			Success: false,
			Error:   fmt.Sprintf("failed to unmount: %v", err),
		}, nil
	}

	return &SourceResult{
		Success: true,
		Message: fmt.Sprintf("NAS share '%s' unmounted successfully", cfg.Name),
	}, nil
}

// validateAddNasShareRequest validates an add NAS share request.
func validateAddNasShareRequest(req AddNasShareRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(req.IP) == "" {
		return fmt.Errorf("IP address is required")
	}
	if strings.TrimSpace(req.Path) == "" {
		return fmt.Errorf("path is required")
	}
	if req.FSType != "cifs" && req.FSType != "nfs" {
		return fmt.Errorf("invalid filesystem type: must be 'cifs' or 'nfs'")
	}
	return nil
}

// sanitizeName sanitizes a name for use in file paths.
func sanitizeName(name string) string {
	// Replace unsafe characters with underscores
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "..", "_")
	return name
}

// encryptPassword encrypts a password for storage.
// TODO: Implement proper encryption with device-specific key.
func encryptPassword(password string) string {
	// For now, just base64 encode - will implement proper encryption later
	return password // Placeholder
}

// decryptPassword decrypts a stored password.
func decryptPassword(encrypted string) string {
	return encrypted // Placeholder
}

// SetDiscoverer sets the NAS discoverer for the service.
func (s *Service) SetDiscoverer(d Discoverer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discoverer = d
}

// DiscoverNasDevices finds NAS devices on the local network.
func (s *Service) DiscoverNasDevices() (*DiscoverResult, error) {
	s.mu.RLock()
	discoverer := s.discoverer
	s.mu.RUnlock()

	if discoverer == nil {
		return &DiscoverResult{
			Devices: []NasDevice{},
			Error:   "discoverer not configured",
		}, nil
	}

	devices, err := discoverer.DiscoverDevices()
	if err != nil {
		return &DiscoverResult{
			Devices: []NasDevice{},
			Error:   err.Error(),
		}, nil
	}

	return &DiscoverResult{
		Devices: devices,
	}, nil
}

// MountAllShares attempts to mount all configured NAS shares.
// Returns a summary of mount results for each share.
func (s *Service) MountAllShares() []MountResult {
	s.mu.RLock()
	shares := make([]*NasShareConfig, 0, len(s.config.NasShares))
	for _, cfg := range s.config.NasShares {
		shares = append(shares, cfg)
	}
	s.mu.RUnlock()

	results := make([]MountResult, 0, len(shares))

	for _, cfg := range shares {
		result := MountResult{
			ShareID:   cfg.ID,
			ShareName: cfg.Name,
		}

		mountPoint := filepath.Join(NasMountBase, sanitizeName(cfg.Name))

		// Check if already mounted
		if s.mounter != nil && s.mounter.IsMounted(mountPoint) {
			result.Success = true
			result.Message = "already mounted"
			result.Mounted = true
			results = append(results, result)
			continue
		}

		// Try to mount
		mountResult, err := s.MountNasShare(cfg.ID)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
		} else if mountResult != nil {
			result.Success = mountResult.Success
			result.Message = mountResult.Message
			result.Error = mountResult.Error
			result.Mounted = mountResult.Success
		}

		results = append(results, result)
	}

	return results
}

// BrowseNasShares lists available shares on a NAS host.
func (s *Service) BrowseNasShares(host, username, password string) (*BrowseSharesResult, error) {
	s.mu.RLock()
	discoverer := s.discoverer
	s.mu.RUnlock()

	if discoverer == nil {
		return &BrowseSharesResult{
			Shares: []ShareInfo{},
			Error:  "discoverer not configured",
		}, nil
	}

	shares, err := discoverer.BrowseShares(host, username, password)
	if err != nil {
		return &BrowseSharesResult{
			Shares: []ShareInfo{},
			Error:  err.Error(),
		}, nil
	}

	return &BrowseSharesResult{
		Shares: shares,
	}, nil
}
