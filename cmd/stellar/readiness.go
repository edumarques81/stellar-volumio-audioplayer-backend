package main

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

// readinessBody is the JSON payload returned by GET /ready.
// Consumers should poll /ready (not /health) before sending traffic.
type readinessBody struct {
	// Ready is true only when both hard gates pass (MPD reachable and cache loaded).
	Ready bool `json:"ready"`

	// MPD is "connected" or "disconnected".
	MPD string `json:"mpd"`

	// Cache holds album count and building flag.
	Cache cacheReadiness `json:"cache"`

	// Airplay holds the current AirPlay state — informational only; does not
	// cause a 503.
	Airplay airplayReadiness `json:"airplay"`
}

// cacheReadiness is the cache sub-object within readinessBody.
type cacheReadiness struct {
	Albums   int  `json:"albums"`
	Building bool `json:"building"`
}

// airplayReadiness is the airplay sub-object within readinessBody.
type airplayReadiness struct {
	Active bool `json:"active"`
}

// computeReadiness evaluates the two hard readiness gates and assembles the
// response body. It is a pure function with no I/O so it can be unit-tested
// directly without standing up an HTTP server.
//
// Gates:
//   - MPD: mpdPingErr == nil.
//   - Cache: cacheStats != nil && AlbumCount > 0 && !IsBuilding.
//     If cacheStats is nil (cache disabled), the cache gate is skipped and
//     readiness depends on MPD alone.
//
// AirPlay state is included in the body as informational metadata; it never
// causes ready=false.
func computeReadiness(mpdPingErr error, cacheStats *cache.CacheStats, airplayActive bool) (ready bool, body readinessBody) {
	body.Airplay = airplayReadiness{Active: airplayActive}

	// Gate 1: MPD connectivity.
	if mpdPingErr != nil {
		body.MPD = "disconnected"
		body.Ready = false
		return false, body
	}
	body.MPD = "connected"

	// Gate 2: Cache readiness (skipped when cache is disabled).
	if cacheStats != nil {
		body.Cache = cacheReadiness{
			Albums:   cacheStats.AlbumCount,
			Building: cacheStats.IsBuilding,
		}
		if cacheStats.AlbumCount == 0 || cacheStats.IsBuilding {
			body.Ready = false
			return false, body
		}
	}

	body.Ready = true
	return true, body
}

// handleReady is the HTTP handler for GET /ready. It calls computeReadiness
// with live values and returns 200 when ready, 503 otherwise.
//
// mpdPing is a function that returns the result of mpdClient.Ping() — this
// indirection makes the handler mockable in tests without importing the MPD
// package here.
func makeReadyHandler(
	mpdPing func() error,
	cacheDB cacheDBer,
	airplayActive func() bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pingErr := mpdPing()

		var stats *cache.CacheStats
		if cacheDB != nil {
			if s, err := cacheDB.GetStats(); err == nil {
				stats = s
			}
		}

		active := airplayActive()
		ready, body := computeReadiness(pingErr, stats, active)
		body.Ready = ready

		httpStatus := http.StatusOK
		if !ready {
			httpStatus = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		if err := json.NewEncoder(w).Encode(body); err != nil {
			log.Warn().Err(err).Msg("/ready: encode error")
		}
	}
}

// cacheDBer is the subset of *cache.DB needed by the /ready handler so tests
// can provide a lightweight stub without opening a real SQLite file.
type cacheDBer interface {
	GetStats() (*cache.CacheStats, error)
}
