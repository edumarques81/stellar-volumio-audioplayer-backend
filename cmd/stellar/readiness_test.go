package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

// ---------------------------------------------------------------------------
// computeReadiness — pure unit tests, no HTTP server required
// ---------------------------------------------------------------------------

func TestComputeReadinessMPDDown503(t *testing.T) {
	t.Parallel()

	stats := &cache.CacheStats{AlbumCount: 10, IsBuilding: false}
	ready, body := computeReadiness(errors.New("connection refused"), stats, false)

	if ready {
		t.Error("expected ready=false when MPD is down")
	}
	if body.MPD != "disconnected" {
		t.Errorf("mpd = %q, want disconnected", body.MPD)
	}
}

func TestComputeReadinessCacheBuilding503(t *testing.T) {
	t.Parallel()

	stats := &cache.CacheStats{AlbumCount: 0, IsBuilding: true}
	ready, body := computeReadiness(nil, stats, false)

	if ready {
		t.Error("expected ready=false when cache is building")
	}
	if !body.Cache.Building {
		t.Errorf("cache.building = %v, want true", body.Cache.Building)
	}
	if body.Cache.Albums != 0 {
		t.Errorf("albums = %d, want 0", body.Cache.Albums)
	}
}

func TestComputeReadinessCacheEmptyNotBuilding503(t *testing.T) {
	t.Parallel()

	// AlbumCount=0, IsBuilding=false → cache not ready (empty)
	stats := &cache.CacheStats{AlbumCount: 0, IsBuilding: false}
	ready, body := computeReadiness(nil, stats, false)

	if ready {
		t.Error("expected ready=false when cache is empty and not building")
	}
	if body.MPD != "connected" {
		t.Errorf("mpd = %q, want connected", body.MPD)
	}
}

func TestComputeReadinessAllGoodReturns200(t *testing.T) {
	t.Parallel()

	stats := &cache.CacheStats{AlbumCount: 55, IsBuilding: false}
	ready, body := computeReadiness(nil, stats, false)

	if !ready {
		t.Error("expected ready=true when all gates pass")
	}
	if body.MPD != "connected" {
		t.Errorf("mpd = %q, want connected", body.MPD)
	}
	if body.Cache.Albums != 55 {
		t.Errorf("albums = %d, want 55", body.Cache.Albums)
	}
}

func TestComputeReadinessAirplayActiveDoesNotCause503(t *testing.T) {
	t.Parallel()

	// AirPlay active should NOT gate readiness (informational only)
	stats := &cache.CacheStats{AlbumCount: 10, IsBuilding: false}
	ready, body := computeReadiness(nil, stats, true)

	if !ready {
		t.Error("expected ready=true — airplay=active must not cause 503")
	}
	if !body.Airplay.Active {
		t.Error("expected airplay.active=true in response body")
	}
}

func TestComputeReadinessCacheNilIsNotGating(t *testing.T) {
	t.Parallel()

	// When cache is disabled (nil stats), MPD-OK alone is sufficient for ready=true.
	ready, body := computeReadiness(nil, nil, false)

	if !ready {
		t.Error("expected ready=true when cache is disabled (nil stats)")
	}
	if body.Cache.Albums != 0 {
		t.Errorf("albums = %d, want 0 for nil cache stats", body.Cache.Albums)
	}
}

// ---------------------------------------------------------------------------
// /ready HTTP handler — integration via httptest using production makeReadyHandler
// ---------------------------------------------------------------------------

// fakeCacheDB is a minimal cacheDBer stub for tests.
type fakeCacheDB struct{ stats *cache.CacheStats }

func (f *fakeCacheDB) GetStats() (*cache.CacheStats, error) {
	if f == nil || f.stats == nil {
		return nil, nil
	}
	return f.stats, nil
}

// buildReadyHandler constructs the production /ready handler with canned values.
func buildReadyHandler(mpdPingErr error, stats *cache.CacheStats, airplayActive bool) http.HandlerFunc {
	var db cacheDBer
	if stats != nil {
		db = &fakeCacheDB{stats: stats}
	}
	return makeReadyHandler(
		func() error { return mpdPingErr },
		db,
		func() bool { return airplayActive },
	)
}

func TestReadyHandlerMPDDown503(t *testing.T) {
	t.Parallel()

	stats := &cache.CacheStats{AlbumCount: 10, IsBuilding: false}
	h := buildReadyHandler(errors.New("dial tcp: connection refused"), stats, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	h(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	var resp readinessBody
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Ready {
		t.Error("ready=true in body, want false")
	}
	if resp.MPD != "disconnected" {
		t.Errorf("mpd = %q, want disconnected", resp.MPD)
	}
}

func TestReadyHandlerCacheBuilding503(t *testing.T) {
	t.Parallel()

	stats := &cache.CacheStats{AlbumCount: 0, IsBuilding: true}
	h := buildReadyHandler(nil, stats, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	h(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	var resp readinessBody
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Cache.Building != true {
		t.Errorf("cache.building = %v, want true", resp.Cache.Building)
	}
}

func TestReadyHandlerCacheEmptyNotBuilding503(t *testing.T) {
	t.Parallel()

	stats := &cache.CacheStats{AlbumCount: 0, IsBuilding: false}
	h := buildReadyHandler(nil, stats, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	h(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestReadyHandlerAllGood200(t *testing.T) {
	t.Parallel()

	stats := &cache.CacheStats{AlbumCount: 55, IsBuilding: false}
	h := buildReadyHandler(nil, stats, false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	var resp readinessBody
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Ready {
		t.Error("ready=false in body, want true")
	}
	if resp.Cache.Albums != 55 {
		t.Errorf("cache.albums = %d, want 55", resp.Cache.Albums)
	}
	if resp.MPD != "connected" {
		t.Errorf("mpd = %q, want connected", resp.MPD)
	}
}

func TestReadyHandlerAirplayActiveIsInformationalOnly(t *testing.T) {
	t.Parallel()

	stats := &cache.CacheStats{AlbumCount: 10, IsBuilding: false}
	h := buildReadyHandler(nil, stats, true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	h(rec, req)

	// Must still be 200 — airplay active must NOT cause 503.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 when airplay is active", rec.Code)
	}
	var resp readinessBody
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Airplay.Active {
		t.Error("airplay.active=false in body, want true")
	}
}
