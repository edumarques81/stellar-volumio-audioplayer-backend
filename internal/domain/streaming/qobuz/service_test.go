package qobuz

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewService(t *testing.T) {
	// Create a temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "qobuz.json")

	// Test creating a new service
	svc, err := NewService(configPath)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if svc == nil {
		t.Fatal("NewService() returned nil")
	}

	// Verify initial state
	if svc.IsLoggedIn() {
		t.Error("New service should not be logged in")
	}

	if svc.Name() != QobuzServiceName {
		t.Errorf("Name() = %v, want %v", svc.Name(), QobuzServiceName)
	}

	// GetBrowseSource should return nil when not logged in
	if source := svc.GetBrowseSource(); source != nil {
		t.Error("GetBrowseSource() should return nil when not logged in")
	}
}

func TestServiceName(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "qobuz.json")

	svc, _ := NewService(configPath)

	if svc.Name() != "qobuz" {
		t.Errorf("Name() = %v, want qobuz", svc.Name())
	}
}

func TestGetStatus(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "qobuz.json")

	svc, _ := NewService(configPath)

	status := svc.GetStatus()
	if status == nil {
		t.Fatal("GetStatus() returned nil")
	}

	if status.LoggedIn {
		t.Error("Initial status should not be logged in")
	}
}

func TestLogout(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "qobuz.json")

	svc, _ := NewService(configPath)

	// Logout should not error even if not logged in
	err := svc.Logout()
	if err != nil {
		t.Errorf("Logout() error = %v", err)
	}

	// Verify logged out state
	if svc.IsLoggedIn() {
		t.Error("Should not be logged in after logout")
	}
}

func TestHandleBrowseURINotLoggedIn(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "qobuz.json")

	svc, _ := NewService(configPath)

	// Should fail when not logged in
	_, err := svc.HandleBrowseURI("qobuz://")
	if err == nil {
		t.Error("HandleBrowseURI() should fail when not logged in")
	}
}

func TestSearchNotLoggedIn(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "qobuz.json")

	svc, _ := NewService(configPath)

	// Should fail when not logged in
	_, err := svc.Search("test query", 10)
	if err == nil {
		t.Error("Search() should fail when not logged in")
	}
}

func TestGetStreamURLNotLoggedIn(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "qobuz.json")

	svc, _ := NewService(configPath)

	// Should fail when not logged in
	_, err := svc.GetStreamURL("123456")
	if err == nil {
		t.Error("GetStreamURL() should fail when not logged in")
	}
}

func TestConfigPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "qobuz.json")

	// Create service and manually set some config
	svc, _ := NewService(configPath)
	svc.config.Email = "test@example.com"
	svc.config.AppCredentials = &WebPlayerCredentials{
		AppID:     "123456789",
		AppSecret: "abcdef123456",
	}

	// Save config
	err := svc.saveConfig()
	if err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Config file was not created")
	}

	// Create new service and verify config was loaded
	svc2, err := NewService(configPath)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if svc2.config.Email != "test@example.com" {
		t.Errorf("Email not persisted, got %v", svc2.config.Email)
	}

	if svc2.config.AppCredentials == nil {
		t.Fatal("AppCredentials not persisted")
	}

	if svc2.config.AppCredentials.AppID != "123456789" {
		t.Errorf("AppID not persisted, got %v", svc2.config.AppCredentials.AppID)
	}
}

func TestURIParsing(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{"root", "qobuz://", false},
		{"root with slash", "qobuz:///", false},
		{"myalbums", "qobuz://myalbums", false},
		{"myplaylists", "qobuz://myplaylists", false},
		{"featured", "qobuz://featured", false},
		{"album", "qobuz://album/123456", false},
		{"artist", "qobuz://artist/789012", false},
		{"playlist", "qobuz://playlist/345678", false},
		{"unknown", "qobuz://unknown/path", true},
	}

	// Note: These tests require the service to be logged in
	// They are skipped here because we can't easily mock the gobuz API
	// In a real scenario, we'd use interface-based mocking
	t.Skip("URI parsing tests require logged-in service - need mocking")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "qobuz.json")
			svc, _ := NewService(configPath)

			_, err := svc.HandleBrowseURI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("HandleBrowseURI(%q) error = %v, wantErr %v", tt.uri, err, tt.wantErr)
			}
		})
	}
}
