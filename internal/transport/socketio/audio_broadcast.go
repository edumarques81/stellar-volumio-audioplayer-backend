// Audio-status broadcast to connected clients.
package socketio

import (
	"github.com/rs/zerolog/log"
)

// BroadcastAudioStatus sends audio status to all connected clients.
func (s *Server) BroadcastAudioStatus() {
	status := s.audioController.GetStatus()
	s.io.Emit("pushAudioStatus", status)
	log.Debug().Bool("locked", status.Locked).Interface("format", status.Format).Msg("Broadcast audio status")
}
