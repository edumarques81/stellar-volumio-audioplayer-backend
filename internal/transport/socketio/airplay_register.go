package socketio

import (
	"context"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/airplay"
)

// airplayCmdHandler is captured at Server construction time (Use…) and
// looked up by the per-client `airplay:command` Socket.IO listener.
// The session pointer is stored alongside so the on-connect handler can
// hydrate freshly-connected clients with the current AirPlay state —
// without it, a client that reconnects mid-session sits idle until the
// next shairport metadata frame arrives (which may be tens of seconds).
type airplayBundle struct {
	cmd     *AirplayCommandHandler
	session *airplay.Session
}

// UseAirplay wires the AirPlay command handler so per-client connections
// can dispatch `airplay:command` events. Must be called BEFORE the HTTP
// server starts accepting connections (same constraint as SetBioHandlers).
func (s *Server) UseAirplay(session *airplay.Session, commander DACPCommander, resolver DACPResolverInterface) {
	s.airplay = &airplayBundle{
		cmd:     NewAirplayCommandHandler(session, commander, resolver),
		session: session,
	}
}

// pushAirplayStateTo emits the current AirPlay session snapshot to a
// freshly-connected client. No-op when UseAirplay wasn't called or no
// session is currently active. Called by the connection handler so
// reconnecting clients don't need to wait for the next shairport
// metadata frame to discover that AirPlay is playing.
func (s *Server) pushAirplayStateTo(client *socket.Socket) {
	if s.airplay == nil || s.airplay.session == nil {
		return
	}
	snap := s.airplay.session.Snapshot()
	if !snap.IsActive {
		return
	}
	_ = client.Emit("pushAirplayState", snap)
}

// registerAirplayClient is called by setupHandlers' connection callback
// to attach the `airplay:command` listener to a freshly-connected socket.
// No-op when UseAirplay was not invoked.
func (s *Server) registerAirplayClient(client *socket.Socket) {
	if s.airplay == nil || s.airplay.cmd == nil {
		return
	}

	// `getAirplayState` lets a client (typically iOS on app-foreground)
	// re-pull the current session snapshot without forcing a reconnect.
	// Without this, the AirPlay UI can be stale when the iOS app is
	// briefly backgrounded — the socket stays alive through the lifecycle
	// (so reconnectIfNeeded is a no-op) and no fresh pushAirplayState
	// arrives unless shairport happens to emit a metadata frame in that
	// window. The client emits getAirplayState and we reply with a
	// targeted pushAirplayState (or no-op if no active session).
	_ = client.On("getAirplayState", func(_ ...any) {
		s.pushAirplayStateTo(client)
	})

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
