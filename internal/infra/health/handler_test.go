// Package health_test — httptest-based tests for /metrics and /health
// handler wrappers exported from this package.
package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCollector satisfies Collector in tests.
type fakeCollector struct {
	snap Snapshot
}

func (s *fakeCollector) Collect() Snapshot { return s.snap }

// ---------------------------------------------------------------------------
// MetricsHandler
// ---------------------------------------------------------------------------

func TestMetricsHandlerReturns200WithJSON(t *testing.T) {
	t.Parallel()
	c := &fakeCollector{snap: Snapshot{
		ALSA:           ALSASnapshot{Supported: true, State: "RUNNING"},
		Xruns:          0,
		XrunsAvailable: true,
	}}
	h := NewMetricsHandler(c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var snap Snapshot
	if err := json.NewDecoder(rec.Body).Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if snap.ALSA.State != "RUNNING" {
		t.Errorf("alsa.state = %q, want RUNNING", snap.ALSA.State)
	}
}

func TestMetricsHandlerMethodNotAllowed(t *testing.T) {
	t.Parallel()
	c := &fakeCollector{}
	h := NewMetricsHandler(c)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/metrics", nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("method %s: status = %d, want 405", method, rec.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// HealthExtension — adds xruns + alsa to an existing health response body.
// The real /health handler calls mpdClient.Ping() and then writes JSON.
// We test the health-extension helper separately so the MPD layer is not
// needed.
// ---------------------------------------------------------------------------

func TestBuildHealthExtra(t *testing.T) {
	t.Parallel()
	snap := Snapshot{
		Xruns:          3,
		XrunsAvailable: true,
		ALSA: ALSASnapshot{
			Supported: true,
			State:     "RUNNING",
			AvailMax:  4096,
		},
	}
	extra := buildHealthExtra(snap)
	if extra["xruns"] != int64(3) {
		t.Errorf("xruns = %v, want 3", extra["xruns"])
	}
	alsa, ok := extra["alsa"].(map[string]interface{})
	if !ok {
		t.Fatalf("alsa field missing or wrong type: %T %v", extra["alsa"], extra["alsa"])
	}
	if alsa["state"] != "RUNNING" {
		t.Errorf("alsa.state = %v, want RUNNING", alsa["state"])
	}
}

func TestBuildHealthExtraUnavailable(t *testing.T) {
	t.Parallel()
	snap := Snapshot{
		Xruns:          -1,
		XrunsAvailable: false,
		ALSA:           ALSASnapshot{Supported: false},
	}
	extra := buildHealthExtra(snap)
	// xruns should be -1 (sentinel for "not available")
	if extra["xruns"] != int64(-1) {
		t.Errorf("xruns = %v, want -1", extra["xruns"])
	}
	alsa, ok := extra["alsa"].(map[string]interface{})
	if !ok {
		t.Fatalf("alsa field missing: %T", extra["alsa"])
	}
	if alsa["supported"] != false {
		t.Errorf("alsa.supported = %v, want false", alsa["supported"])
	}
}
