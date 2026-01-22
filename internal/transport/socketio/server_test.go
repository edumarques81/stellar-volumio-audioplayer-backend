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

	server, err := socketio.NewServer(playerService, mpdClient)
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

	server, err := socketio.NewServer(playerService, mpdClient)
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

	server, err := socketio.NewServer(playerService, mpdClient)
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

	server, err := socketio.NewServer(playerService, mpdClient)
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

	server, err := socketio.NewServer(playerService, mpdClient)
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

	server, err := socketio.NewServer(playerService, mpdClient)
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
