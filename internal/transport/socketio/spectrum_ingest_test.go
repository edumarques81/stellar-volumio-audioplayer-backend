package socketio

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// captureEmitter records the events the ingest handler asks the server to
// broadcast. The real Server emits via the underlying socket.io stack;
// here we only care that ingest → re-broadcast happens with the right
// payload, so we substitute a callback.
type captureEmitter struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	event string
	data  interface{}
}

func (c *captureEmitter) emit(event string, data interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, capturedEvent{event: event, data: data})
}

func (c *captureEmitter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func samplePayload() map[string]interface{} {
	return map[string]interface{}{
		"binsL":      []float64{0.1, 0.2, 0.3},
		"binsR":      []float64{0.4, 0.5, 0.6},
		"peakL":      0.7,
		"peakR":      0.8,
		"rmsL":       0.3,
		"rmsR":       0.4,
		"sampleRate": 44100,
		"ts":         int64(1234567890),
	}
}

func postPayload(t *testing.T, handler http.Handler, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/internal/spectrum", &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestSpectrumIngestRejectsMissingAuth(t *testing.T) {
	emitter := &captureEmitter{}
	handler := NewSpectrumIngestHandler("test-key-123", emitter.emit)

	rec := postPayload(t, handler, "", samplePayload())

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without bearer, got %d", rec.Code)
	}
	if emitter.count() != 0 {
		t.Errorf("expected no emit on unauthorized request, got %d", emitter.count())
	}
}

func TestSpectrumIngestRejectsWrongAuth(t *testing.T) {
	emitter := &captureEmitter{}
	handler := NewSpectrumIngestHandler("test-key-123", emitter.emit)

	rec := postPayload(t, handler, "wrong-key", samplePayload())

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong bearer, got %d", rec.Code)
	}
	if emitter.count() != 0 {
		t.Errorf("expected no emit on wrong key, got %d", emitter.count())
	}
}

func TestSpectrumIngestAcceptsValidPayload(t *testing.T) {
	emitter := &captureEmitter{}
	handler := NewSpectrumIngestHandler("test-key-123", emitter.emit)

	rec := postPayload(t, handler, "test-key-123", samplePayload())

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204 No Content on success, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if emitter.count() != 1 {
		t.Fatalf("expected exactly one emit, got %d", emitter.count())
	}
	if emitter.events[0].event != "pushSpectrum" {
		t.Errorf("expected event=pushSpectrum, got %q", emitter.events[0].event)
	}
}

func TestSpectrumIngestRejectsGet(t *testing.T) {
	emitter := &captureEmitter{}
	handler := NewSpectrumIngestHandler("test-key-123", emitter.emit)

	req := httptest.NewRequest(http.MethodGet, "/internal/spectrum", nil)
	req.Header.Set("Authorization", "Bearer test-key-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on GET, got %d", rec.Code)
	}
}

func TestSpectrumIngestRejectsMalformedJSON(t *testing.T) {
	emitter := &captureEmitter{}
	handler := NewSpectrumIngestHandler("test-key-123", emitter.emit)

	req := httptest.NewRequest(http.MethodPost, "/internal/spectrum", bytes.NewBufferString("not json"))
	req.Header.Set("Authorization", "Bearer test-key-123")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on malformed json, got %d", rec.Code)
	}
	if emitter.count() != 0 {
		t.Errorf("expected no emit on malformed json, got %d", emitter.count())
	}
}

func TestSpectrumIngestDisabledWhenKeyEmpty(t *testing.T) {
	emitter := &captureEmitter{}
	// Empty key explicitly disables ingest — protects against accidental
	// open-relay misconfig when STELLAR_SPECTRUM_KEY is unset on the Mac.
	handler := NewSpectrumIngestHandler("", emitter.emit)

	rec := postPayload(t, handler, "anything", samplePayload())

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when ingest disabled, got %d", rec.Code)
	}
	if emitter.count() != 0 {
		t.Errorf("expected no emit when disabled, got %d", emitter.count())
	}
}
