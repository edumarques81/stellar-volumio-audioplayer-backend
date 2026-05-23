package socketio

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestRemoteAudioClientImpl_HappyPaths(t *testing.T) {
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
	client := NewRemoteAudioClientWithClient(srv.URL, "test-token", &http.Client{Timeout: 2 * time.Second})

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

func TestRemoteAudioClientImpl_FailureModes(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(t *testing.T) (string, string) // returns (baseURL, token)
		wantErrIs string                              // substring match on err.Error()
	}{
		{
			name: "HTTP 401 unauthorized",
			setup: func(t *testing.T) (string, string) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
				}))
				t.Cleanup(srv.Close)
				return srv.URL, "wrong-token"
			},
			wantErrIs: "HTTP 401",
		},
		{
			name: "HTTP 500 Pi shell-out failed",
			setup: func(t *testing.T) (string, string) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "shell failed", http.StatusInternalServerError)
				}))
				t.Cleanup(srv.Close)
				return srv.URL, "token"
			},
			wantErrIs: "HTTP 500",
		},
		{
			name: "connection refused",
			setup: func(t *testing.T) (string, string) {
				// Bind a port, immediately close it, then return its URL.
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
				url := srv.URL
				srv.Close()
				return url, "token"
			},
			wantErrIs: "", // any error (connection refused message varies by OS)
		},
		{
			name: "JSON decode failure",
			setup: func(t *testing.T) (string, string) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("not json"))
				}))
				t.Cleanup(srv.Close)
				return srv.URL, "token"
			},
			wantErrIs: "decode",
		},
	}

	methods := []struct {
		name string
		call func(*RemoteAudioClientImpl) error
	}{
		{"SystemInfo", func(c *RemoteAudioClientImpl) error { _, e := c.SystemInfo(); return e }},
		{"DeviceInfo", func(c *RemoteAudioClientImpl) error { _, e := c.DeviceInfo(); return e }},
		{"NetworkStatus", func(c *RemoteAudioClientImpl) error { _, e := c.NetworkStatus(); return e }},
		{"BitPerfect", func(c *RemoteAudioClientImpl) error { _, e := c.BitPerfect(); return e }},
		{"DsdMode", func(c *RemoteAudioClientImpl) error { _, e := c.DsdMode(); return e }},
		{"MixerMode", func(c *RemoteAudioClientImpl) error { _, e := c.MixerMode(); return e }},
	}

	for _, tc := range cases {
		for _, m := range methods {
			tc, m := tc, m
			t.Run(tc.name+"/"+m.name, func(t *testing.T) {
				baseURL, token := tc.setup(t)
				client := NewRemoteAudioClientWithClient(baseURL, token, &http.Client{Timeout: 2 * time.Second})
				err := m.call(client)
				if err == nil {
					t.Fatalf("want error, got nil")
				}
				if tc.wantErrIs != "" && !strings.Contains(err.Error(), tc.wantErrIs) {
					t.Errorf("err = %q, want substring %q", err.Error(), tc.wantErrIs)
				}
			})
		}
	}
}

func TestRemoteAudioClient_WriteHappyPaths(t *testing.T) {
	// Fixtures for the 3 POST endpoints.
	var (
		gotDsdBody    map[string]any
		gotMixerBody  map[string]any
		gotApplyBody  string
		lastPostToken string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		lastPostToken = r.Header.Get("X-Auth-Token")
		switch r.URL.Path {
		case "/api/audio/dsd":
			_ = json.NewDecoder(r.Body).Decode(&gotDsdBody)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(DsdModeResponse{Mode: gotDsdBody["mode"].(string), Success: true})
		case "/api/audio/mixer":
			_ = json.NewDecoder(r.Body).Decode(&gotMixerBody)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(MixerModeResponse{Enabled: gotMixerBody["enabled"].(bool), Success: true})
		case "/api/audio/bitperfect/apply":
			buf, _ := io.ReadAll(r.Body)
			gotApplyBody = string(buf)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ApplyBitPerfectResponse{Success: true, Applied: []string{"mixer_type = bit-perfect"}, Errors: []string{}})
		default:
			http.Error(w, "no route", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	client := NewRemoteAudioClientWithClient(srv.URL, "write-token", &http.Client{Timeout: 2 * time.Second})

	t.Run("SetDsdMode", func(t *testing.T) {
		resp, err := client.SetDsdMode("dop")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if resp.Mode != "dop" {
			t.Errorf("Mode = %q, want dop", resp.Mode)
		}
		if !resp.Success {
			t.Errorf("Success = false, want true")
		}
		if gotDsdBody["mode"] != "dop" {
			t.Errorf("body mode = %v, want dop", gotDsdBody["mode"])
		}
		if lastPostToken != "write-token" {
			t.Errorf("token = %q, want write-token", lastPostToken)
		}
	})
	t.Run("SetMixerMode", func(t *testing.T) {
		resp, err := client.SetMixerMode(true)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if !resp.Enabled {
			t.Errorf("Enabled = false, want true")
		}
		if gotMixerBody["enabled"] != true {
			t.Errorf("body enabled = %v, want true", gotMixerBody["enabled"])
		}
	})
	t.Run("ApplyBitPerfect", func(t *testing.T) {
		resp, err := client.ApplyBitPerfect()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if !resp.Success {
			t.Errorf("Success = false, want true")
		}
		if len(resp.Applied) != 1 {
			t.Errorf("Applied len = %d, want 1", len(resp.Applied))
		}
		if gotApplyBody != "" {
			t.Errorf("body = %q, want empty (no body for apply)", gotApplyBody)
		}
	})
}
