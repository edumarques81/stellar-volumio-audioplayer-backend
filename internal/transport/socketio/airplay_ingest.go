package socketio

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/airplay"
)

// airplayIngestPayload is the wire format the Pi-side stellar-airplay
// daemon posts to /internal/airplay/state. All fields are optional; the
// daemon sends a delta-merge — empty fields preserve prior session
// state. A payload with {ended: true} ends the session.
type airplayIngestPayload struct {
	Ended           bool   `json:"ended,omitempty"`
	Title           string `json:"title,omitempty"`
	Artist          string `json:"artist,omitempty"`
	Album           string `json:"album,omitempty"`
	Sender          string `json:"sender,omitempty"`
	CoverDataURL    string `json:"coverDataURL,omitempty"`
	ActiveRemote    string `json:"activeRemote,omitempty"`
	DACPID          string `json:"dacpID,omitempty"`
	SeekSeconds     int    `json:"seekSeconds,omitempty"`
	DurationSeconds int    `json:"durationSeconds,omitempty"`
	SampleRate      int    `json:"sampleRate,omitempty"`
	BitDepth        int    `json:"bitDepth,omitempty"`

	SessionBegan bool `json:"sessionBegan,omitempty"`
	Paused       bool `json:"paused,omitempty"`
	Resumed      bool `json:"resumed,omitempty"`
}

// AirplayIngestHandler accepts metadata posts from the Pi-side daemon,
// applies them to the supplied Session, and re-broadcasts the resulting
// snapshot via the EmitFunc. Authentication is a constant-time bearer
// token match — an empty key returns 503 on every request so an unset
// env never accepts unauthenticated payloads.
type AirplayIngestHandler struct {
	key     string
	session *airplay.Session
	emit    EmitFunc
}

// NewAirplayIngestHandler builds an ingest handler. Pass an empty key to
// disable the endpoint (503 every request).
func NewAirplayIngestHandler(key string, session *airplay.Session, emit EmitFunc) *AirplayIngestHandler {
	return &AirplayIngestHandler{key: key, session: session, emit: emit}
}

// ServeHTTP implements http.Handler.
func (h *AirplayIngestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.key == "" {
		http.Error(w, "airplay ingest disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !bearerAuthOK(r, h.key) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Cover-art JPEGs run 300-500 KiB; cap at 1 MiB to leave headroom.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}
	var p airplayIngestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if p.Ended {
		id, ended := h.session.End()
		if ended && h.emit != nil {
			h.emit("pushAirplayEnded", map[string]string{"sessionID": id})
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Translate the wire flags (sessionBegan / paused / resumed) into the
	// session's PlayState tristate. Order matters when multiple flags are
	// asserted simultaneously: pbeg implies playing → resumed → paused.
	playState := airplay.PlayStateUnknown
	if p.SessionBegan || p.Resumed {
		playState = airplay.PlayStatePlaying
	}
	if p.Paused {
		playState = airplay.PlayStatePaused
	}

	h.session.Update(airplay.Frame{
		Title:           p.Title,
		Artist:          p.Artist,
		Album:           p.Album,
		Sender:          p.Sender,
		CoverDataURL:    p.CoverDataURL,
		ActiveRemote:    p.ActiveRemote,
		DACPID:          p.DACPID,
		SeekSeconds:     p.SeekSeconds,
		DurationSeconds: p.DurationSeconds,
		SampleRate:      p.SampleRate,
		BitDepth:        p.BitDepth,
		PlayState:       playState,
		SessionBegan:    p.SessionBegan,
		Paused:          p.Paused,
		Resumed:         p.Resumed,
	})

	snap := h.session.Snapshot()
	log.Info().
		Int("body_bytes", len(body)).
		Str("title", snap.Title).
		Str("artist", snap.Artist).
		Bool("isActive", snap.IsActive).
		Bool("hasEmit", h.emit != nil).
		Bool("hasCover", snap.CoverDataURL != "").
		Str("sessionID", snap.SessionID).
		Msg("airplay ingest: about to emit pushAirplayState")
	if h.emit != nil {
		h.emit("pushAirplayState", snap)
		log.Info().Str("sessionID", snap.SessionID).Msg("airplay ingest: emit returned")
	}
	w.WriteHeader(http.StatusNoContent)
}

// AirplayHeartbeatHandler accepts heartbeat pings from the Pi daemon
// to refresh the session's deadline without otherwise updating any
// fields. Returns 204 on success; 401 on bad/missing auth; 503 when
// disabled (empty key).
type AirplayHeartbeatHandler struct {
	key     string
	session *airplay.Session
}

// NewAirplayHeartbeatHandler builds a heartbeat handler.
func NewAirplayHeartbeatHandler(key string, session *airplay.Session) *AirplayHeartbeatHandler {
	return &AirplayHeartbeatHandler{key: key, session: session}
}

// ServeHTTP implements http.Handler.
func (h *AirplayHeartbeatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.key == "" {
		http.Error(w, "airplay heartbeat disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !bearerAuthOK(r, h.key) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	h.session.Heartbeat()
	w.WriteHeader(http.StatusNoContent)
}

// bearerAuthOK validates the Authorization header against `key` using
// constant-time comparison.
func bearerAuthOK(r *http.Request, key string) bool {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := strings.TrimPrefix(header, prefix)
	if len(got) != len(key) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(key)) == 1
}

// AirplayIngestHandler wires the ingest endpoint to the Server's emit
// pipeline (mirrors SpectrumIngestHandler).
func (s *Server) AirplayIngestHandler(key string, session *airplay.Session) *AirplayIngestHandler {
	return NewAirplayIngestHandler(key, session, s.Emit)
}

// AirplayHeartbeatHandler wires the heartbeat endpoint.
func (s *Server) AirplayHeartbeatHandler(key string, session *airplay.Session) *AirplayHeartbeatHandler {
	return NewAirplayHeartbeatHandler(key, session)
}

