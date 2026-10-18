package upnp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubController satisfies PlayerController without touching MPD.
type stubController struct{}

func (stubController) LoadAndPlay(string, TrackMetadata) error { return nil }
func (stubController) Play() error                             { return nil }
func (stubController) Pause() error                            { return nil }
func (stubController) Stop() error                             { return nil }
func (stubController) Seek(int) error                          { return nil }
func (stubController) GetPosition() (int, int, TransportState) { return 0, 0, StateStopped }

func TestEnableMediaServerRequiresUDNPortAndLibrary(t *testing.T) {
	tests := []struct {
		name string
		cfg  MediaServerConfig
		lib  LibraryProvider
		want bool
	}{
		{"complete", MediaServerConfig{UDN: "server", BackendPort: 3000}, &fakeLibrary{}, true},
		{"explicit base URL instead of a port", MediaServerConfig{UDN: "server", BaseURL: "http://pi:3000"}, &fakeLibrary{}, true},
		{"no UDN", MediaServerConfig{BackendPort: 3000}, &fakeLibrary{}, false},
		{"no port and no base URL", MediaServerConfig{UDN: "server"}, &fakeLibrary{}, false},
		{"no library", MediaServerConfig{UDN: "server", BackendPort: 3000}, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService(DeviceConfig{UDN: "renderer", Port: 9180}, stubController{})
			s.EnableMediaServer(tt.cfg, tt.lib)
			if s.serverEnabled != tt.want {
				t.Errorf("serverEnabled = %v, want %v", s.serverEnabled, tt.want)
			}
		})
	}
}

func TestMediaServerSharesTheRendererHTTPMux(t *testing.T) {
	s := NewService(DeviceConfig{UDN: "renderer", Port: 9180, LocalIP: "10.0.0.5"}, stubController{})
	s.EnableMediaServer(MediaServerConfig{
		FriendlyName: "Stellar Library",
		UDN:          "server",
		BackendPort:  3000,
	}, newTestLibrary())
	mux := s.buildMux()

	// Both roles must answer on the one UPnP port: the renderer's endpoints
	// keep working and the server's are added alongside them.
	for _, path := range []string{
		"/upnp/description.xml",
		"/upnp/AVTransport.xml",
		MediaServerDescPath,
		"/upnp/ContentDirectory.xml",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}

func TestMediaServerStreamURLsPointAtTheBackendPortNotTheUPnPPort(t *testing.T) {
	// The UPnP HTTP server serves descriptions and SOAP with a 10s write
	// timeout and no Range support. Media must come from the backend's own
	// /stream endpoint, so a res URL carrying the UPnP port would truncate
	// every transfer that takes longer than ten seconds.
	s := NewService(DeviceConfig{UDN: "renderer", Port: 9180, LocalIP: "10.0.0.5"}, stubController{})
	s.EnableMediaServer(MediaServerConfig{UDN: "server", BackendPort: 3000}, newTestLibrary())
	mux := s.buildMux()

	albumID := EncodeAlbumID(AlbumRef{Title: "Kind of Blue", AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue"})
	req := httptest.NewRequest(http.MethodPost, "/upnp/control/ContentDirectory",
		strings.NewReader(browseBody(albumID, "BrowseDirectChildren", 0, 0)))
	req.Header.Set("SOAPACTION", `"`+contentDirectoryType+`#Browse"`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Browse status = %d:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "http://10.0.0.5:3000/stream/") {
		t.Errorf("res URLs do not target the backend port:\n%s", body)
	}
	if strings.Contains(body, ":9180/stream") {
		t.Errorf("res URLs target the UPnP port:\n%s", body)
	}
}

func TestExplicitBaseURLOverridesTheComposedOne(t *testing.T) {
	s := NewService(DeviceConfig{UDN: "renderer", Port: 9180, LocalIP: "10.0.0.5"}, stubController{})
	s.EnableMediaServer(MediaServerConfig{
		UDN:         "server",
		BackendPort: 3000,
		BaseURL:     "http://stellar.local:3000",
	}, newTestLibrary())
	s.buildMux()

	if s.serverConfig.BaseURL != "http://stellar.local:3000" {
		t.Errorf("BaseURL = %q, want the explicit value to survive", s.serverConfig.BaseURL)
	}
}

func TestCollidingUDNDisablesTheMediaServer(t *testing.T) {
	// Two root devices sharing one UUID makes control points overwrite one
	// description with the other and then fail every action belonging to the
	// role they discarded. Refusing to advertise beats shipping that.
	s := NewService(DeviceConfig{UDN: "same-uuid", Port: 9180, LocalIP: "10.0.0.5"}, stubController{})
	s.EnableMediaServer(MediaServerConfig{UDN: "same-uuid", BackendPort: 3000}, newTestLibrary())

	if !s.serverEnabled {
		t.Fatal("EnableMediaServer rejected the config too early; the collision check belongs in Start")
	}

	mux := s.buildMux()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, MediaServerDescPath, nil))
	if rec.Code == http.StatusOK {
		t.Error("a colliding UDN still registered the MediaServer description")
	}

	// The renderer must keep working — the server is what gets dropped.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/upnp/description.xml", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("renderer description = %d, want 200", rec.Code)
	}
}

func TestRendererWorksWithoutAMediaServer(t *testing.T) {
	// EnableMediaServer is optional; the renderer predates it and must not
	// regress when it is never called.
	s := NewService(DeviceConfig{UDN: "renderer", Port: 9180, LocalIP: "10.0.0.5"}, stubController{})
	mux := s.buildMux()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/upnp/description.xml", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("renderer description = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, MediaServerDescPath, nil))
	if rec.Code == http.StatusOK {
		t.Error("MediaServer description served without EnableMediaServer")
	}
}
