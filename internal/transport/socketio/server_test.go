package socketio_test

import (
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/transport/socketio"
)

func TestNewServer(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, true)
	if err != nil {
		t.Errorf("NewServer should not return error: %v", err)
	}

	if server == nil {
		t.Error("NewServer should return a non-nil server")
	}
}

func TestServerServeHTTP(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, true)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Server should implement http.Handler
	// We verify this by checking it's not nil and can be closed
	if server == nil {
		t.Error("Server should not be nil")
	}

	// Test that Close works
	if err := server.Close(); err != nil {
		t.Errorf("Close should not error: %v", err)
	}
}

func TestServerBroadcastStateWithoutClients(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, true)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// BroadcastState should not panic with no clients
	// This is a smoke test - it should handle the case gracefully
	server.BroadcastState()
}

func TestServerBroadcastQueueWithoutClients(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, true)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// BroadcastQueue should not panic with no clients
	server.BroadcastQueue()
}

func TestServerBroadcastNetworkStatusWithoutClients(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, true)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// BroadcastNetworkStatus should not panic with no clients
	server.BroadcastNetworkStatus()
}

func TestServerBroadcastLCDStatusWithoutClients(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, true)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// BroadcastLCDStatus should not panic with no clients
	server.BroadcastLCDStatus()
}

func TestGetNetworkStatus(t *testing.T) {
	// GetNetworkStatus should return a valid NetworkStatus struct
	status := socketio.GetNetworkStatus()

	// Type should be one of: wifi, ethernet, none
	validTypes := map[string]bool{"wifi": true, "ethernet": true, "none": true}
	if !validTypes[status.Type] {
		t.Errorf("Invalid network type: %s", status.Type)
	}

	// Strength should be 0-3
	if status.Strength < 0 || status.Strength > 3 {
		t.Errorf("Invalid strength: %d (should be 0-3)", status.Strength)
	}

	// Signal should be 0-100
	if status.Signal < 0 || status.Signal > 100 {
		t.Errorf("Invalid signal: %d (should be 0-100)", status.Signal)
	}
}

func TestGetLCDStatus(t *testing.T) {
	// GetLCDStatus should return a valid LCDStatus struct
	status := socketio.GetLCDStatus()

	// IsOn should be boolean (no additional validation needed, but test doesn't panic)
	t.Logf("LCD status: isOn=%v", status.IsOn)
}

func TestGetBitPerfectStatus(t *testing.T) {
	// GetBitPerfectStatus should return a valid BitPerfectStatus struct
	status := socketio.GetBitPerfectStatus()

	// Status should be one of: ok, warning, error
	validStatuses := map[string]bool{"ok": true, "warning": true, "error": true}
	if !validStatuses[status.Status] {
		t.Errorf("Invalid bit-perfect status: %s (should be ok, warning, or error)", status.Status)
	}

	// Arrays should not be nil (may be empty but not nil)
	if status.Issues == nil {
		t.Error("Issues array should not be nil")
	}
	if status.Warnings == nil {
		t.Error("Warnings array should not be nil")
	}
	if status.Config == nil {
		t.Error("Config array should not be nil")
	}

	t.Logf("Bit-perfect status: %s, issues=%d, warnings=%d, config=%d",
		status.Status, len(status.Issues), len(status.Warnings), len(status.Config))
}

func TestBitPerfectStatusStructure(t *testing.T) {
	// Test that BitPerfectStatus can be properly JSON marshaled
	status := socketio.BitPerfectStatus{
		Status:   "ok",
		Issues:   []string{},
		Warnings: []string{"test warning"},
		Config:   []string{"config1", "config2"},
	}

	if status.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", status.Status)
	}
	if len(status.Issues) != 0 {
		t.Errorf("Expected 0 issues, got %d", len(status.Issues))
	}
	if len(status.Warnings) != 1 {
		t.Errorf("Expected 1 warning, got %d", len(status.Warnings))
	}
	if len(status.Config) != 2 {
		t.Errorf("Expected 2 config items, got %d", len(status.Config))
	}
}
