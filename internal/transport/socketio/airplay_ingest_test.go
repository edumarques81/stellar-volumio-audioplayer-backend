package socketio

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/airplay"
)

func samplePushAirplayState() map[string]interface{} {
	return map[string]interface{}{
		"title":           "One More Time",
		"artist":          "Daft Punk",
		"album":           "Discovery",
		"sender":          "Eduardo's iPhone",
		"coverDataURL":    "data:image/jpeg;base64,/9j/4AAQ",
		"seekSeconds":     20,
		"durationSeconds": 245,
		"activeRemote":    "3823061215",
		"dacpID":          "63B5C32B3D40C25B",
		"sampleRate":      44100,
		"bitDepth":        16,
	}
}

func postAirplayState(t *testing.T, handler http.Handler, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/airplay/state", &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAirplayIngestRejectsMissingAuth(t *testing.T) {
	emitter := &captureEmitter{}
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	handler := NewAirplayIngestHandler("k", session, emitter.emit)

	rec := postAirplayState(t, handler, "", samplePushAirplayState())
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	if emitter.count() != 0 {
		t.Errorf("expected no emit, got %d", emitter.count())
	}
}

func TestAirplayIngestEmitsPushAirplayState(t *testing.T) {
	emitter := &captureEmitter{}
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	handler := NewAirplayIngestHandler("k", session, emitter.emit)

	rec := postAirplayState(t, handler, "k", samplePushAirplayState())
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if emitter.count() != 1 {
		t.Fatalf("expected 1 emit, got %d", emitter.count())
	}
	ev := emitter.events[0]
	if ev.event != "pushAirplayState" {
		t.Errorf("event = %q, want pushAirplayState", ev.event)
	}
	snap, ok := ev.data.(airplay.Snapshot)
	if !ok {
		t.Fatalf("data type = %T, want airplay.Snapshot", ev.data)
	}
	if !snap.IsActive {
		t.Errorf("snap.IsActive should be true")
	}
	if snap.Title != "One More Time" || snap.Artist != "Daft Punk" {
		t.Errorf("snap fields wrong: %+v", snap)
	}
	if !snap.CanControl {
		t.Errorf("CanControl should be true (acre+daid both present)")
	}
	if !snap.IsPlaying {
		t.Errorf("IsPlaying should default to true on first non-paused frame")
	}
	if session.ActiveRemote() != "3823061215" {
		t.Errorf("session ActiveRemote not stored: %q", session.ActiveRemote())
	}
}

func TestAirplayIngestPropagatesIsPlayingFalseOnPause(t *testing.T) {
	emitter := &captureEmitter{}
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	handler := NewAirplayIngestHandler("k", session, emitter.emit)

	// Establish a session.
	_ = postAirplayState(t, handler, "k", samplePushAirplayState())
	emitter.events = nil

	// Pause.
	rec := postAirplayState(t, handler, "k", map[string]interface{}{"paused": true})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if emitter.count() != 1 {
		t.Fatalf("expected 1 emit, got %d", emitter.count())
	}
	snap, ok := emitter.events[0].data.(airplay.Snapshot)
	if !ok {
		t.Fatalf("data type = %T", emitter.events[0].data)
	}
	if !snap.IsActive {
		t.Errorf("paused → IsActive should remain true")
	}
	if snap.IsPlaying {
		t.Errorf("paused → IsPlaying should be false")
	}
}

func TestAirplayIngestEndsSessionOnEnded(t *testing.T) {
	emitter := &captureEmitter{}
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	handler := NewAirplayIngestHandler("k", session, emitter.emit)

	// Start a session
	_ = postAirplayState(t, handler, "k", samplePushAirplayState())
	emitter.events = nil

	// Send {"ended": true}
	rec := postAirplayState(t, handler, "k", map[string]interface{}{"ended": true})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	// Two emits: the canonical sessionID-matched pushAirplayEnded, plus a
	// terminal pushAirplayState{isActive:false} so a client that missed the
	// ended event still flips out of AirPlay on the next snapshot.
	if emitter.count() != 2 {
		t.Fatalf("expected 2 emits (pushAirplayEnded + pushAirplayState), got %d", emitter.count())
	}
	if emitter.events[0].event != "pushAirplayEnded" {
		t.Errorf("event[0] = %q, want pushAirplayEnded", emitter.events[0].event)
	}
	if emitter.events[1].event != "pushAirplayState" {
		t.Errorf("event[1] = %q, want pushAirplayState", emitter.events[1].event)
	}
	termSnap, ok := emitter.events[1].data.(airplay.Snapshot)
	if !ok {
		t.Fatalf("event[1] data type = %T, want airplay.Snapshot", emitter.events[1].data)
	}
	if termSnap.IsActive {
		t.Errorf("terminal pushAirplayState should carry isActive=false")
	}
	if session.Snapshot().IsActive {
		t.Errorf("session should be inactive after Ended")
	}
}

func TestAirplayIngestRejectsGet(t *testing.T) {
	emitter := &captureEmitter{}
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	handler := NewAirplayIngestHandler("k", session, emitter.emit)

	req := httptest.NewRequest(http.MethodGet, "/internal/airplay/state", nil)
	req.Header.Set("Authorization", "Bearer k")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestAirplayIngestRejectsMalformedJSON(t *testing.T) {
	emitter := &captureEmitter{}
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	handler := NewAirplayIngestHandler("k", session, emitter.emit)

	req := httptest.NewRequest(http.MethodPost, "/internal/airplay/state", bytes.NewBufferString("not json"))
	req.Header.Set("Authorization", "Bearer k")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAirplayIngestDisabledWhenKeyEmpty(t *testing.T) {
	emitter := &captureEmitter{}
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	handler := NewAirplayIngestHandler("", session, emitter.emit)

	rec := postAirplayState(t, handler, "anything", samplePushAirplayState())
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

// TestAirplayHeartbeatRoutesToSessionHeartbeat covers POST /internal/airplay/heartbeat.
// The handler should call Session.Heartbeat and return 204 with no emit.
func TestAirplayHeartbeatRoutesToSessionHeartbeat(t *testing.T) {
	emitter := &captureEmitter{}
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: 50 * time.Millisecond})
	handler := NewAirplayHeartbeatHandler("k", session)

	// Start a session via direct Update so we don't depend on the ingest handler.
	session.Update(airplay.Frame{Title: "x"})
	time.Sleep(30 * time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/internal/airplay/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer k")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if emitter.count() != 0 {
		t.Errorf("heartbeat should not emit; got %d", emitter.count())
	}

	// After heartbeat, the session should still be alive past the original timeout
	// (heartbeat at +30ms moves the deadline to +80ms; we check at +60ms).
	time.Sleep(30 * time.Millisecond)
	if !session.Snapshot().IsActive {
		t.Errorf("session should still be active after heartbeat refresh")
	}
}

func TestAirplayHeartbeatRejectsMissingAuth(t *testing.T) {
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	handler := NewAirplayHeartbeatHandler("k", session)

	req := httptest.NewRequest(http.MethodPost, "/internal/airplay/heartbeat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
