package sources

import (
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
