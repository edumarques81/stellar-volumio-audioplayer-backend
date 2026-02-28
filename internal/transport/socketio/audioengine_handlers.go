package socketio

import (
	"os/exec"
	"strings"

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

// isServiceActive checks if a systemd service is currently active.
func isServiceActive(serviceName string) bool {
	out, err := exec.Command("systemctl", "is-active", serviceName).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "active"
}
