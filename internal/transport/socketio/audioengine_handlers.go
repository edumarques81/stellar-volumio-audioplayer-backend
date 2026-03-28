package socketio

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"
)

// AudioEngineState represents the current audio engine state.
type AudioEngineState struct {
	Active           string `json:"active"`           // "mpd" or "audirvana"
	MPDRunning       bool   `json:"mpdRunning"`
	AudirvanaRunning bool   `json:"audirvanaRunning"`
}

// GetAudioEngineState checks which audio engine is currently active.
func (s *Server) GetAudioEngineState() AudioEngineState {
	mpdRunning := isServiceActive("mpd")
	audirvanaRunning := isServiceActive("audirvanaStudio")

	active := "mpd" // default
	if audirvanaRunning {
		active = "audirvana"
	}

	return AudioEngineState{
		Active:           active,
		MPDRunning:       mpdRunning,
		AudirvanaRunning: audirvanaRunning,
	}
}

// pushAudioEngineState sends the current audio engine state to a single client.
func (s *Server) pushAudioEngineState(client *socket.Socket) {
	state := s.GetAudioEngineState()
	log.Debug().
		Str("active", state.Active).
		Bool("mpd", state.MPDRunning).
		Bool("audirvana", state.AudirvanaRunning).
		Msg("Pushing audio engine state")
	client.Emit("pushAudioEngineState", state)
}

// BroadcastAudioEngineState sends the current audio engine state to all clients.
func (s *Server) BroadcastAudioEngineState() {
	state := s.GetAudioEngineState()
	s.io.Emit("pushAudioEngineState", state)
}

// StartAudirvanaPoller starts a background poller that broadcasts Audirvana
// now-playing state every 2 seconds when Audirvana is the active engine.
//
// TODO: Audirvana Studio on Linux does not expose a REST API, DBus interface,
// or state file for current track info. Possible future approaches:
// - Parse ~/.local/share/audirvana/audirvana_studio.log for track changes
// - Monitor ALSA/PulseAudio output to detect playback
// - Use the Audirvana mDNS/DAAP protocol (port discovered via avahi-browse)
// For now, this emits a stub state so the frontend knows Audirvana is playing.
func (s *Server) StartAudirvanaPoller(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		wasActive := false

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				state := s.GetAudioEngineState()
				isActive := state.AudirvanaRunning

				if isActive {
					// Audirvana is running — broadcast now-playing stub
					s.io.Emit("pushState", map[string]interface{}{
						"status":   "play",
						"service":  "audirvana",
						"title":    "Audirvana Active",
						"artist":   "",
						"album":    "",
						"duration": 0,
						"seek":     0,
						"volume":   0,
					})

					if !wasActive {
						log.Info().Msg("Audirvana became active, broadcasting state")
						s.BroadcastAudioEngineState()
					}
				} else if wasActive {
					// Audirvana just stopped — broadcast engine change back to MPD
					log.Info().Msg("Audirvana became inactive, switching to MPD state")
					s.BroadcastAudioEngineState()
					s.BroadcastState()
				}

				wasActive = isActive
			}
		}
	}()

	log.Info().Msg("Audirvana now-playing poller started (2s interval)")
}

// isServiceActive checks if a systemd service is currently active,
// falling back to pgrep process detection when no systemd unit exists.
func isServiceActive(serviceName string) bool {
	// Try systemctl first (works when installed as a service)
	out, err := exec.Command("systemctl", "is-active", serviceName).Output()
	if err == nil && strings.TrimSpace(string(out)) == "active" {
		return true
	}

	// Fallback: check if the process is running by name via pgrep
	// This handles Audirvana running as a bare process without a systemd unit.
	pgrepOut, pgrepErr := exec.Command("pgrep", "-f", serviceName).Output()
	return pgrepErr == nil && len(strings.TrimSpace(string(pgrepOut))) > 0
}
