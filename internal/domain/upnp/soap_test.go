package upnp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockController implements PlayerController for testing.
type mockController struct {
	lastURI      string
	lastMeta     TrackMetadata
	lastAction   string
	lastSeekSec  int
	positionSec  int
	durationSec  int
	currentState TransportState
	err          error
}

func (m *mockController) LoadAndPlay(uri string, meta TrackMetadata) error {
	m.lastAction = "LoadAndPlay"
	m.lastURI = uri
	m.lastMeta = meta
	return m.err
}

func (m *mockController) Play() error {
	m.lastAction = "Play"
	return m.err
}

func (m *mockController) Pause() error {
	m.lastAction = "Pause"
	return m.err
}

func (m *mockController) Stop() error {
	m.lastAction = "Stop"
	return m.err
}

func (m *mockController) Seek(seconds int) error {
	m.lastAction = "Seek"
	m.lastSeekSec = seconds
	return m.err
}

func (m *mockController) GetPosition() (int, int, TransportState) {
	return m.positionSec, m.durationSec, m.currentState
}

func TestExtractSOAPAction(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{`"urn:schemas-upnp-org:service:AVTransport:1#Play"`, "Play"},
		{`"urn:schemas-upnp-org:service:AVTransport:1#SetAVTransportURI"`, "SetAVTransportURI"},
		{`urn:schemas-upnp-org:service:AVTransport:1#Stop`, "Stop"},
		{"", ""},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("POST", "/", nil)
		r.Header.Set("SOAPACTION", tt.header)
		got := extractSOAPAction(r)
		if got != tt.want {
			t.Errorf("extractSOAPAction(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestExtractXMLElement(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		element string
		want    string
	}{
		{
			name:    "simple element",
			raw:     `<CurrentURI>http://example.com/track.flac</CurrentURI>`,
			element: "CurrentURI",
			want:    "http://example.com/track.flac",
		},
		{
			name:    "element with attributes",
			raw:     `<CurrentURI attr="val">http://example.com/track.flac</CurrentURI>`,
			element: "CurrentURI",
			want:    "http://example.com/track.flac",
		},
		{
			name:    "missing element",
			raw:     `<Other>value</Other>`,
			element: "CurrentURI",
			want:    "",
		},
		{
			name:    "self-closing element",
			raw:     `<CurrentURI/>`,
			element: "CurrentURI",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractXMLElement(tt.raw, tt.element)
			if got != tt.want {
				t.Errorf("extractXMLElement() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTimeToSeconds(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"0:06:50", 410},
		{"0:06:50.000", 410},
		{"1:30:00", 5400},
		{"0:00:30", 30},
		{"5:00", 300},
		{"30", 30},
		{"", 0},
	}
	for _, tt := range tests {
		got := parseTimeToSeconds(tt.input)
		if got != tt.want {
			t.Errorf("parseTimeToSeconds(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestFormatSeconds(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0:00:00"},
		{30, "0:00:30"},
		{410, "0:06:50"},
		{5400, "1:30:00"},
		{-5, "0:00:00"},
	}
	for _, tt := range tests {
		got := formatSeconds(tt.input)
		if got != tt.want {
			t.Errorf("formatSeconds(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSOAPHandlerPlay(t *testing.T) {
	ctrl := &mockController{currentState: StateStopped}
	svc := &Service{
		config:     DeviceConfig{UDN: "test-uuid"},
		controller: ctrl,
		state:      StateStopped,
	}
	handler := NewSOAPHandler(svc)

	mux := http.NewServeMux()
	handler.RegisterHandlers(mux)

	body := `<?xml version="1.0"?>
	<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
	  <s:Body>
	    <u:Play xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">
	      <InstanceID>0</InstanceID><Speed>1</Speed>
	    </u:Play>
	  </s:Body>
	</s:Envelope>`

	req := httptest.NewRequest("POST", "/upnp/control/AVTransport", strings.NewReader(body))
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:AVTransport:1#Play"`)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ctrl.lastAction != "Play" {
		t.Errorf("expected Play action, got %q", ctrl.lastAction)
	}
	if !strings.Contains(rec.Body.String(), "PlayResponse") {
		t.Errorf("response missing PlayResponse: %s", rec.Body.String())
	}
}

func TestSOAPHandlerGetTransportInfo(t *testing.T) {
	ctrl := &mockController{currentState: StatePlaying, positionSec: 30, durationSec: 300}
	svc := &Service{
		config:     DeviceConfig{UDN: "test-uuid"},
		controller: ctrl,
		state:      StatePlaying,
	}
	handler := NewSOAPHandler(svc)

	mux := http.NewServeMux()
	handler.RegisterHandlers(mux)

	body := `<?xml version="1.0"?>
	<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
	  <s:Body>
	    <u:GetTransportInfo xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">
	      <InstanceID>0</InstanceID>
	    </u:GetTransportInfo>
	  </s:Body>
	</s:Envelope>`

	req := httptest.NewRequest("POST", "/upnp/control/AVTransport", strings.NewReader(body))
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:AVTransport:1#GetTransportInfo"`)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	respBody := rec.Body.String()
	if !strings.Contains(respBody, "<CurrentTransportState>PLAYING</CurrentTransportState>") {
		t.Errorf("response missing PLAYING state: %s", respBody)
	}
	if !strings.Contains(respBody, "<CurrentTransportStatus>OK</CurrentTransportStatus>") {
		t.Errorf("response missing OK status: %s", respBody)
	}
}

func TestSOAPHandlerGetPositionInfo(t *testing.T) {
	ctrl := &mockController{currentState: StatePlaying, positionSec: 130, durationSec: 410}
	svc := &Service{
		config:     DeviceConfig{UDN: "test-uuid"},
		controller: ctrl,
		state:      StatePlaying,
		current: TrackMetadata{
			URI: "http://example.com/track.flac",
		},
	}
	handler := NewSOAPHandler(svc)

	mux := http.NewServeMux()
	handler.RegisterHandlers(mux)

	req := httptest.NewRequest("POST", "/upnp/control/AVTransport", strings.NewReader(`<dummy/>`))
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:AVTransport:1#GetPositionInfo"`)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	respBody := rec.Body.String()
	if !strings.Contains(respBody, "<RelTime>0:02:10</RelTime>") {
		t.Errorf("expected RelTime 0:02:10, got: %s", respBody)
	}
	if !strings.Contains(respBody, "<TrackDuration>0:06:50</TrackDuration>") {
		t.Errorf("expected TrackDuration 0:06:50, got: %s", respBody)
	}
}

func TestSOAPHandlerGetProtocolInfo(t *testing.T) {
	ctrl := &mockController{}
	svc := &Service{
		config:     DeviceConfig{UDN: "test-uuid"},
		controller: ctrl,
	}
	handler := NewSOAPHandler(svc)

	mux := http.NewServeMux()
	handler.RegisterHandlers(mux)

	req := httptest.NewRequest("POST", "/upnp/control/ConnectionManager", nil)
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ConnectionManager:1#GetProtocolInfo"`)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "audio/flac") {
		t.Errorf("expected audio/flac in protocol info: %s", rec.Body.String())
	}
}

func TestDecodeXMLEntities(t *testing.T) {
	input := "&lt;DIDL-Lite&gt;&amp;test&lt;/DIDL-Lite&gt;"
	want := "<DIDL-Lite>&test</DIDL-Lite>"
	got := decodeXMLEntities(input)
	if got != want {
		t.Errorf("decodeXMLEntities() = %q, want %q", got, want)
	}
}
