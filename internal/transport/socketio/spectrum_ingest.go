package socketio

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// EmitFunc is the callback invoked by the spectrum ingest handler once an
// inbound payload has been authenticated and parsed. The Server's Emit
// method satisfies this signature; tests inject a capturing stand-in.
type EmitFunc func(event string, data interface{})

// SpectrumIngestHandler is the HTTP handler that accepts spectrum payloads
// from the Pi-side stellar-spectrum daemon and re-broadcasts them via the
// supplied EmitFunc. Authentication is a shared bearer token; an empty
// key disables the endpoint entirely (returns 503) to avoid an
// open-relay misconfig.
type SpectrumIngestHandler struct {
	key  string
	emit EmitFunc
}

// NewSpectrumIngestHandler returns a handler that authenticates inbound
// spectrum payloads with `key` (compared in constant time). If key is
// empty, the handler returns 503 for every request so an unset env
// doesn't accidentally accept unauthenticated payloads.
func NewSpectrumIngestHandler(key string, emit EmitFunc) *SpectrumIngestHandler {
	return &SpectrumIngestHandler{key: key, emit: emit}
}

// ServeHTTP implements http.Handler.
func (h *SpectrumIngestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.key == "" {
		http.Error(w, "spectrum ingest disabled", http.StatusServiceUnavailable)
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.authOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Cap body size at 256 KiB — a 64-bin L+R float64 payload is ~2 KiB,
	// so anything larger is malformed or hostile.
	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024))
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if h.emit != nil {
		h.emit("pushSpectrum", payload)
	}

	w.WriteHeader(http.StatusNoContent)
}

// authOK validates the Authorization header against the configured key
// using constant-time comparison.
func (h *SpectrumIngestHandler) authOK(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := strings.TrimPrefix(header, prefix)
	if len(got) != len(h.key) {
		// constant-time compare still safe with mismatched lengths but
		// short-circuit makes the intent clear.
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(h.key)) == 1
}

// SpectrumIngestHandler wires the ingest endpoint to the Server's
// emit pipeline. Callers (cmd/stellar/main.go) attach the returned
// handler to their mux at the desired path (default `/internal/spectrum`).
func (s *Server) SpectrumIngestHandler(key string) *SpectrumIngestHandler {
	return NewSpectrumIngestHandler(key, s.Emit)
}

// logSpectrumIngestConfig emits an info-level startup line so operators can
// see whether the ingest endpoint came up enabled. Kept separate from
// NewSpectrumIngestHandler so unit tests stay quiet.
func logSpectrumIngestConfig(enabled bool, route string) {
	if enabled {
		log.Info().Str("route", route).Msg("Spectrum ingest endpoint enabled")
	} else {
		log.Info().Msg("Spectrum ingest endpoint disabled (STELLAR_SPECTRUM_KEY unset)")
	}
}
