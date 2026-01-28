// Package device provides device identity management for Volumio integration.
package device

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewService_GeneratesUUID(t *testing.T) {
	// Create a temp directory for testing
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "device.json")

	// Create service - should generate a new UUID
	svc, err := NewService(configPath)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	info := svc.GetDeviceInfo()

	// UUID should not be empty
	if info.UUID == "" {
		t.Error("UUID should not be empty")
	}

	// UUID should be valid format (36 chars with dashes)
	if len(info.UUID) != 36 {
		t.Errorf("UUID should be 36 characters, got %d: %s", len(info.UUID), info.UUID)
	}

	// Name should have a default
	if info.Name == "" {
		t.Error("Name should not be empty")
	}
}

func TestNewService_PersistsUUID(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "device.json")

	// Create first service
	svc1, err := NewService(configPath)
	if err != nil {
		t.Fatalf("NewService (1) failed: %v", err)
	}
	uuid1 := svc1.GetDeviceInfo().UUID

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("Config file should have been created")
	}

	// Create second service - should load existing UUID
	svc2, err := NewService(configPath)
	if err != nil {
		t.Fatalf("NewService (2) failed: %v", err)
	}
	uuid2 := svc2.GetDeviceInfo().UUID

	// UUIDs should match
	if uuid1 != uuid2 {
		t.Errorf("UUID should persist across restarts: %s != %s", uuid1, uuid2)
	}
}

func TestNewService_LoadsExistingUUID(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "device.json")

	// Create a config file with a known UUID
	knownUUID := "550e8400-e29b-41d4-a716-446655440000"
	configContent := `{"uuid":"` + knownUUID + `","name":"TestDevice"}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Create service - should load existing UUID
	svc, err := NewService(configPath)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	info := svc.GetDeviceInfo()
	if info.UUID != knownUUID {
		t.Errorf("Should load existing UUID: got %s, want %s", info.UUID, knownUUID)
	}
	if info.Name != "TestDevice" {
		t.Errorf("Should load existing name: got %s, want TestDevice", info.Name)
	}
}

func TestGetDeviceInfo_ReturnsCorrectFormat(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "device.json")

	svc, err := NewService(configPath)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	info := svc.GetDeviceInfo()

	// Check required fields
	if info.UUID == "" {
		t.Error("UUID is required")
	}
	if info.Name == "" {
		t.Error("Name is required")
	}
	if info.Type != "device" {
		t.Errorf("Type should be 'device', got %s", info.Type)
	}
	if info.ServiceName != "Volumio" {
		t.Errorf("ServiceName should be 'Volumio', got %s", info.ServiceName)
	}
}

func TestGetMultiRoomDevice_ReturnsSelfInList(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "device.json")

	svc, err := NewService(configPath)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Create a mock player state
	state := map[string]interface{}{
		"status":  "play",
		"volume":  75,
		"artist":  "Test Artist",
		"title":   "Test Track",
		"albumart": "/albumart?path=test.flac",
	}

	device := svc.GetMultiRoomDevice(state)

	// Verify device format matches Volumio expectations
	if device["id"] != svc.GetDeviceInfo().UUID {
		t.Error("Device ID should match UUID")
	}
	if device["name"] != svc.GetDeviceInfo().Name {
		t.Error("Device name should match")
	}
	if device["type"] != "device" {
		t.Error("Type should be 'device'")
	}
	if device["isSelf"] != true {
		t.Error("isSelf should be true")
	}
	if device["volumeAvailable"] != true {
		t.Error("volumeAvailable should be true")
	}

	// Verify state is included
	deviceState, ok := device["state"].(map[string]interface{})
	if !ok {
		t.Fatal("state should be a map")
	}
	if deviceState["status"] != "play" {
		t.Error("State should include playback status")
	}
	if deviceState["volume"] != 75 {
		t.Error("State should include volume")
	}
}

func TestSetDeviceName(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "device.json")

	svc, err := NewService(configPath)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Change device name
	newName := "My Custom Player"
	if err := svc.SetDeviceName(newName); err != nil {
		t.Fatalf("SetDeviceName failed: %v", err)
	}

	// Verify name changed
	if svc.GetDeviceInfo().Name != newName {
		t.Errorf("Name should be updated: got %s, want %s", svc.GetDeviceInfo().Name, newName)
	}

	// Verify persisted
	svc2, err := NewService(configPath)
	if err != nil {
		t.Fatalf("NewService (2) failed: %v", err)
	}
	if svc2.GetDeviceInfo().Name != newName {
		t.Error("Name should persist")
	}
}
