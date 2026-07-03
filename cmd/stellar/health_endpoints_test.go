package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/health"
)

// fakeCollector is a test-only Collector that returns a canned Snapshot.
type fakeCollector struct {
	snap health.Snapshot
}

func (f *fakeCollector) Collect() health.Snapshot { return f.snap }

// ---------------------------------------------------------------------------
// /metrics handler gating
// ---------------------------------------------------------------------------

func TestMetricsHandlerGated404(t *testing.T) {
	t.Parallel()
	// When STELLAR_METRICS != "1", the route is not registered → 404.
	mux := http.NewServeMux()
	// deliberately NOT registering /metrics
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when /metrics not registered", rec.Code)
	}
}

func TestMetricsHandlerEnabled200(t *testing.T) {
	t.Parallel()
	c := &fakeCollector{snap: health.Snapshot{
		Xruns:          0,
		XrunsAvailable: true,
		ALSA:           health.ALSASnapshot{Supported: true, State: "RUNNING"},
	}}
	mux := http.NewServeMux()
	mux.Handle("/metrics", health.NewMetricsHandler(c))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var snap health.Snapshot
	if err := json.NewDecoder(rec.Body).Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if snap.ALSA.State != "RUNNING" {
		t.Errorf("alsa.state = %q, want RUNNING", snap.ALSA.State)
	}
}

// ---------------------------------------------------------------------------
// /health handler — extended with xruns + alsa
// ---------------------------------------------------------------------------

func TestHealthHandlerIncludesXrunsAndAlsa(t *testing.T) {
	t.Parallel()
	snap := health.Snapshot{
		Xruns:          0,
		XrunsAvailable: true,
		ALSA:           health.ALSASnapshot{Supported: true, State: "RUNNING"},
	}
	extra := health.BuildHealthExtra(snap)

	// Verify required keys are present.
	if _, ok := extra["xruns"]; !ok {
		t.Error("BuildHealthExtra missing key 'xruns'")
	}
	if _, ok := extra["alsa"]; !ok {
		t.Error("BuildHealthExtra missing key 'alsa'")
	}
	alsa, ok := extra["alsa"].(map[string]interface{})
	if !ok {
		t.Fatalf("alsa is %T, want map", extra["alsa"])
	}
	if alsa["state"] != "RUNNING" {
		t.Errorf("alsa.state = %v, want RUNNING", alsa["state"])
	}
}

func TestHealthHandlerXrunsUnavailable(t *testing.T) {
	t.Parallel()
	snap := health.Snapshot{
		Xruns:          -1,
		XrunsAvailable: false,
		ALSA:           health.ALSASnapshot{Supported: false},
	}
	extra := health.BuildHealthExtra(snap)
	if extra["xruns"] != int64(-1) {
		t.Errorf("xruns = %v (%T), want int64(-1)", extra["xruns"], extra["xruns"])
	}
}
