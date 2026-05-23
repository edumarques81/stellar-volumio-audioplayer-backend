package socketio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/device"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/netinfo"
)

// piStub returns a httptest server that responds with the given fixtures
// keyed by request path. It also records the X-Auth-Token of the last
// request so tests can verify the header.
func piStub(t *testing.T, fixtures map[string]any) (*httptest.Server, *string) {
	t.Helper()
	var lastToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastToken = r.Header.Get("X-Auth-Token")
		body, ok := fixtures[r.URL.Path]
		if !ok {
			http.Error(w, "no fixture", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &lastToken
}

func TestRemoteInfoClient_HappyPaths(t *testing.T) {
	// Fixtures use the ACTUAL struct shapes (not the plan's drafted shapes,
	// which had wrong field names for BitPerfectStatus and MixerModeResponse).
	fixtures := map[string]any{
		"/api/system/info": SystemInfo{
			ID: "stellar.local", Host: "stellar.local", Name: "stellar.local",
			Type: "audio_player", ServiceName: "stellar",
			Hardware: "Raspberry Pi 5 Model B Rev 1.0", Variant: "stellar-pi",
		},
		"/api/system/device":    device.DeviceInfo{UUID: "abc-123", Name: "stellar.local"},
		"/api/network/status":   netinfo.Status{Type: "ethernet", IP: "192.168.86.25", Strength: 100},
		"/api/audio/bitperfect": BitPerfectStatus{
			Status:   "ok",
			Issues:   []string{},
			Warnings: []string{},
			Config:   []string{"MPD: Direct hardware output: hw:2,0 (good)"},
		},
		"/api/audio/dsd":   DsdModeResponse{Mode: "native", Success: true},
		"/api/audio/mixer": MixerModeResponse{Enabled: false, Success: true},
	}
	srv, lastToken := piStub(t, fixtures)
	client := NewRemoteInfoClientWithClient(srv.URL, "test-token", &http.Client{Timeout: 2 * time.Second})

	t.Run("SystemInfo", func(t *testing.T) {
		got, err := client.SystemInfo()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.Host != "stellar.local" {
			t.Errorf("Host = %q, want stellar.local", got.Host)
		}
		if *lastToken != "test-token" {
			t.Errorf("X-Auth-Token = %q, want test-token", *lastToken)
		}
	})

	t.Run("DeviceInfo", func(t *testing.T) {
		got, err := client.DeviceInfo()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.UUID != "abc-123" {
			t.Errorf("UUID = %q, want abc-123", got.UUID)
		}
	})

	t.Run("NetworkStatus", func(t *testing.T) {
		got, err := client.NetworkStatus()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.IP != "192.168.86.25" {
			t.Errorf("IP = %q, want 192.168.86.25", got.IP)
		}
	})

	t.Run("BitPerfect", func(t *testing.T) {
		got, err := client.BitPerfect()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.Status != "ok" {
			t.Errorf("Status = %q, want ok", got.Status)
		}
		if len(got.Config) != 1 {
			t.Errorf("Config len = %d, want 1", len(got.Config))
		}
	})

	t.Run("DsdMode", func(t *testing.T) {
		got, err := client.DsdMode()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.Mode != "native" {
			t.Errorf("Mode = %q, want native", got.Mode)
		}
	})

	t.Run("MixerMode", func(t *testing.T) {
		got, err := client.MixerMode()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.Enabled {
			t.Errorf("Enabled = true, want false")
		}
	})
}
