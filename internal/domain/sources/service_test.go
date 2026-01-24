package sources

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestNewService(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	s, err := NewService(configPath, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	if s == nil {
		t.Fatal("NewService returned nil")
	}

	if s.configPath != configPath {
		t.Errorf("configPath = %q, want %q", s.configPath, configPath)
	}
}

func TestService_AddNasShare(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	// Create mock mount functions for testing
	s, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	req := AddNasShareRequest{
		Name:     "TestShare",
		IP:       "192.168.1.100",
		Path:     "Music",
		FSType:   "cifs",
		Username: "user",
		Password: "pass",
	}

	result, err := s.AddNasShare(req)
	if err != nil {
		t.Fatalf("AddNasShare failed: %v", err)
	}

	if !result.Success {
		t.Errorf("AddNasShare returned success=false: %s", result.Error)
	}

	// Verify share is in the list
	shares, err := s.ListNasShares()
	if err != nil {
		t.Fatalf("ListNasShares failed: %v", err)
	}

	if len(shares) != 1 {
		t.Fatalf("ListNasShares returned %d shares, want 1", len(shares))
	}

	share := shares[0]
	if share.Name != "TestShare" {
		t.Errorf("share.Name = %q, want %q", share.Name, "TestShare")
	}
	if share.IP != "192.168.1.100" {
		t.Errorf("share.IP = %q, want %q", share.IP, "192.168.1.100")
	}
	if share.Path != "Music" {
		t.Errorf("share.Path = %q, want %q", share.Path, "Music")
	}
}

func TestService_ListNasShares_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	s, err := NewService(configPath, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	shares, err := s.ListNasShares()
	if err != nil {
		t.Fatalf("ListNasShares failed: %v", err)
	}

	if len(shares) != 0 {
		t.Errorf("ListNasShares returned %d shares, want 0", len(shares))
	}
}

func TestService_DeleteNasShare(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	s, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Add a share first
	req := AddNasShareRequest{
		Name:   "ToDelete",
		IP:     "192.168.1.100",
		Path:   "Music",
		FSType: "cifs",
	}

	result, err := s.AddNasShare(req)
	if err != nil {
		t.Fatalf("AddNasShare failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("AddNasShare returned success=false: %s", result.Error)
	}

	// Get the share ID
	shares, _ := s.ListNasShares()
	if len(shares) == 0 {
		t.Fatal("No shares found after add")
	}
	shareID := shares[0].ID

	// Delete the share
	result, err = s.DeleteNasShare(shareID)
	if err != nil {
		t.Fatalf("DeleteNasShare failed: %v", err)
	}

	if !result.Success {
		t.Errorf("DeleteNasShare returned success=false: %s", result.Error)
	}

	// Verify share is gone
	shares, _ = s.ListNasShares()
	if len(shares) != 0 {
		t.Errorf("ListNasShares returned %d shares after delete, want 0", len(shares))
	}
}

func TestService_DeleteNasShare_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	s, err := NewService(configPath, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	result, err := s.DeleteNasShare("nonexistent-id")
	if err != nil {
		t.Fatalf("DeleteNasShare failed: %v", err)
	}

	if result.Success {
		t.Error("DeleteNasShare returned success=true for nonexistent share")
	}
}

func TestService_ConfigPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	// Create service and add share
	s1, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	req := AddNasShareRequest{
		Name:   "Persistent",
		IP:     "192.168.1.200",
		Path:   "Audio",
		FSType: "cifs",
	}

	_, err = s1.AddNasShare(req)
	if err != nil {
		t.Fatalf("AddNasShare failed: %v", err)
	}

	// Create new service instance - should load config
	s2, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService (reload) failed: %v", err)
	}

	shares, err := s2.ListNasShares()
	if err != nil {
		t.Fatalf("ListNasShares failed: %v", err)
	}

	if len(shares) != 1 {
		t.Fatalf("ListNasShares returned %d shares after reload, want 1", len(shares))
	}

	if shares[0].Name != "Persistent" {
		t.Errorf("share.Name = %q, want %q", shares[0].Name, "Persistent")
	}
}

func TestService_AddNasShare_Validation(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	s, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	tests := []struct {
		name    string
		req     AddNasShareRequest
		wantErr bool
	}{
		{
			name:    "empty name",
			req:     AddNasShareRequest{IP: "192.168.1.1", Path: "Music", FSType: "cifs"},
			wantErr: true,
		},
		{
			name:    "empty IP",
			req:     AddNasShareRequest{Name: "Test", Path: "Music", FSType: "cifs"},
			wantErr: true,
		},
		{
			name:    "empty path",
			req:     AddNasShareRequest{Name: "Test", IP: "192.168.1.1", FSType: "cifs"},
			wantErr: true,
		},
		{
			name:    "invalid fstype",
			req:     AddNasShareRequest{Name: "Test", IP: "192.168.1.1", Path: "Music", FSType: "invalid"},
			wantErr: true,
		},
		{
			name:    "valid cifs",
			req:     AddNasShareRequest{Name: "Test", IP: "192.168.1.1", Path: "Music", FSType: "cifs"},
			wantErr: false,
		},
		{
			name:    "valid nfs",
			req:     AddNasShareRequest{Name: "Test2", IP: "192.168.1.2", Path: "/export/music", FSType: "nfs"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := s.AddNasShare(tt.req)
			if err != nil {
				t.Fatalf("AddNasShare returned error: %v", err)
			}

			if tt.wantErr && result.Success {
				t.Error("AddNasShare succeeded, want failure")
			}
			if !tt.wantErr && !result.Success {
				t.Errorf("AddNasShare failed: %s", result.Error)
			}
		})
	}
}

// MockMounter implements Mounter interface for testing
type MockMounter struct {
	MountCalled   bool
	UnmountCalled bool
	MountError    error
	UnmountError  error
	IsMountedVal  bool
	MountedPaths  map[string]bool
}

func NewMockMounter() *MockMounter {
	return &MockMounter{
		MountedPaths: make(map[string]bool),
	}
}

func (m *MockMounter) Mount(share *NasShare) error {
	m.MountCalled = true
	if m.MountError != nil {
		return m.MountError
	}
	share.Mounted = true
	m.MountedPaths[share.MountPoint] = true
	return nil
}

func (m *MockMounter) Unmount(mountPoint string) error {
	m.UnmountCalled = true
	if m.UnmountError != nil {
		return m.UnmountError
	}
	delete(m.MountedPaths, mountPoint)
	return nil
}

func (m *MockMounter) IsMounted(mountPoint string) bool {
	if m.IsMountedVal {
		return true
	}
	return m.MountedPaths[mountPoint]
}

func (m *MockMounter) CreateMountPoint(path string) error {
	// Mock - don't actually create directories
	return nil
}

func (m *MockMounter) RemoveMountPoint(path string) error {
	// Mock - don't actually remove directories
	return nil
}

func (m *MockMounter) CreateSymlink(source, target string) error {
	return nil // Mock - don't actually create symlink
}

func (m *MockMounter) RemoveSymlink(path string) error {
	return nil // Mock
}

// ============================================================
// Phase 2: NAS Discovery Tests
// ============================================================

func TestService_DiscoverNasDevices(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	mockDiscoverer := &MockDiscoverer{
		Devices: []NasDevice{
			{Name: "NAS1", IP: "192.168.1.10", Hostname: "nas1.local"},
			{Name: "NAS2", IP: "192.168.1.20", Hostname: "nas2.local"},
		},
	}

	s, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	s.SetDiscoverer(mockDiscoverer)

	result, err := s.DiscoverNasDevices()
	if err != nil {
		t.Fatalf("DiscoverNasDevices failed: %v", err)
	}

	if len(result.Devices) != 2 {
		t.Errorf("DiscoverNasDevices returned %d devices, want 2", len(result.Devices))
	}

	if result.Devices[0].IP != "192.168.1.10" {
		t.Errorf("Device[0].IP = %q, want %q", result.Devices[0].IP, "192.168.1.10")
	}
}

func TestService_DiscoverNasDevices_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	mockDiscoverer := &MockDiscoverer{
		Devices: []NasDevice{},
	}

	s, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	s.SetDiscoverer(mockDiscoverer)

	result, err := s.DiscoverNasDevices()
	if err != nil {
		t.Fatalf("DiscoverNasDevices failed: %v", err)
	}

	if len(result.Devices) != 0 {
		t.Errorf("DiscoverNasDevices returned %d devices, want 0", len(result.Devices))
	}
}

func TestService_DiscoverNasDevices_NoDiscoverer(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	s, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	// No discoverer set

	result, err := s.DiscoverNasDevices()
	if err != nil {
		t.Fatalf("DiscoverNasDevices failed: %v", err)
	}

	if result.Error == "" {
		t.Error("Expected error when discoverer is not set")
	}
}

func TestService_BrowseNasShares(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	mockDiscoverer := &MockDiscoverer{
		Shares: map[string][]ShareInfo{
			"192.168.1.10": {
				{Name: "Music", Type: "disk", Comment: "Music library"},
				{Name: "Videos", Type: "disk", Comment: "Video files"},
			},
		},
	}

	s, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	s.SetDiscoverer(mockDiscoverer)

	result, err := s.BrowseNasShares("192.168.1.10", "", "")
	if err != nil {
		t.Fatalf("BrowseNasShares failed: %v", err)
	}

	if len(result.Shares) != 2 {
		t.Errorf("BrowseNasShares returned %d shares, want 2", len(result.Shares))
	}

	if result.Shares[0].Name != "Music" {
		t.Errorf("Shares[0].Name = %q, want %q", result.Shares[0].Name, "Music")
	}
}

func TestService_BrowseNasShares_WithCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	mockDiscoverer := &MockDiscoverer{
		Shares: map[string][]ShareInfo{
			"192.168.1.10": {
				{Name: "Private", Type: "disk", Comment: "Private share"},
			},
		},
		RequireAuth: true,
	}

	s, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	s.SetDiscoverer(mockDiscoverer)

	// Without credentials - should fail
	result, err := s.BrowseNasShares("192.168.1.10", "", "")
	if err != nil {
		t.Fatalf("BrowseNasShares failed: %v", err)
	}
	if result.Error == "" {
		t.Error("Expected error without credentials")
	}

	// With credentials - should succeed
	result, err = s.BrowseNasShares("192.168.1.10", "user", "pass")
	if err != nil {
		t.Fatalf("BrowseNasShares with auth failed: %v", err)
	}
	if result.Error != "" {
		t.Errorf("BrowseNasShares with auth returned error: %s", result.Error)
	}
	if len(result.Shares) != 1 {
		t.Errorf("BrowseNasShares returned %d shares, want 1", len(result.Shares))
	}
}

func TestService_BrowseNasShares_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "sources.json")

	mockDiscoverer := &MockDiscoverer{
		Shares: map[string][]ShareInfo{},
	}

	s, err := NewService(configPath, NewMockMounter())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	s.SetDiscoverer(mockDiscoverer)

	result, err := s.BrowseNasShares("192.168.1.99", "", "")
	if err != nil {
		t.Fatalf("BrowseNasShares failed: %v", err)
	}

	if result.Error == "" {
		t.Error("Expected error for unreachable host")
	}
}

// MockDiscoverer implements Discoverer interface for testing
type MockDiscoverer struct {
	Devices     []NasDevice
	Shares      map[string][]ShareInfo
	RequireAuth bool
	Error       error
}

func (m *MockDiscoverer) DiscoverDevices() ([]NasDevice, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return m.Devices, nil
}

func (m *MockDiscoverer) BrowseShares(host, username, password string) ([]ShareInfo, error) {
	if m.Error != nil {
		return nil, m.Error
	}

	if m.RequireAuth && (username == "" || password == "") {
		return nil, fmt.Errorf("authentication required")
	}

	shares, ok := m.Shares[host]
	if !ok {
		return nil, fmt.Errorf("host not found: %s", host)
	}

	return shares, nil
}
