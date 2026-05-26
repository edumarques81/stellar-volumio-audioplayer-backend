package socketio

import (
	"context"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/airplay"
)

// airplayCmdHandler is captured at Server construction time (Use…) and
// looked up by the per-client `airplay:command` Socket.IO listener.
type airplayBundle struct {
	cmd *AirplayCommandHandler
}

// UseAirplay wires the AirPlay command handler so per-client connections
// can dispatch `airplay:command` events. Must be called BEFORE the HTTP
// server starts accepting connections (same constraint as SetBioHandlers).
func (s *Server) UseAirplay(session *airplay.Session, commander DACPCommander, resolver DACPResolverInterface) {
	s.airplay = &airplayBundle{
		cmd: NewAirplayCommandHandler(session, commander, resolver),
	}
}

// registerAirplayClient is called by setupHandlers' connection callback
// to attach the `airplay:command` listener to a freshly-connected socket.
// No-op when UseAirplay was not invoked.
func (s *Server) registerAirplayClient(client *socket.Socket) {
	if s.airplay == nil || s.airplay.cmd == nil {
		return
	}
	cmd := s.airplay.cmd
	// socket.io's client.On returns an error that we don't propagate;
	// matches the pattern used everywhere else in this package.
	_ = client.On("airplay:command", func(args ...any) {
		// Socket.IO emits with an optional trailing ack callback. Find it.
		var payload map[string]interface{}
		var ack func([]any, error)
		for _, a := range args {
			switch v := a.(type) {
			case map[string]interface{}:
				if payload == nil {
					payload = v
				}
			case func([]any, error):
				ack = v
			}
		}
		resp := cmd.Handle(context.Background(), payload)
		if ack != nil {
			ack([]any{resp}, nil)
		} else {
			// No ack callback supplied — surface via a pushAirplayCommandResult event
			// so the caller can still observe success/failure. Emit returns an
			// error we don't propagate (consistent with the rest of this package).
			_ = client.Emit("pushAirplayCommandResult", resp)
		}
		log.Debug().Interface("payload", payload).Bool("ok", resp.OK).Str("error", resp.Error).Msg("airplay:command")
	})
}
