package socketio

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/airplay"
)

// DACPCommander is the minimal contract the airplay:command handler
// needs from airplay.DACPClient — declared as an interface so tests can
// inject a fake without touching the network.
type DACPCommander interface {
	Play(ctx context.Context, baseURL, activeRemote string) error
	Pause(ctx context.Context, baseURL, activeRemote string) error
	PlayPause(ctx context.Context, baseURL, activeRemote string) error
	Next(ctx context.Context, baseURL, activeRemote string) error
	Prev(ctx context.Context, baseURL, activeRemote string) error
}

// DACPResolverInterface is the minimal contract for the Bonjour-backed
// DACPResolver. Tests provide a fake; cmd/stellar wires the real one.
type DACPResolverInterface interface {
	Resolve(ctx context.Context, dacpID string) (string, error)
	Invalidate(dacpID string)
}

// AirplayCommandResponse is the ack-style response shape returned to
// the Socket.IO caller (and also used as a plain JSON ack via HTTP in
// the iOS / Volumio2-UI consumers).
type AirplayCommandResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// AirplayCommandHandler dispatches a {cmd} payload to the right
// airplay.DACPClient call, looking up the per-session ActiveRemote
// token and the Bonjour-resolved DACP base URL on demand.
type AirplayCommandHandler struct {
	session  *airplay.Session
	commander DACPCommander
	resolver DACPResolverInterface

	commandTimeout time.Duration
}

// NewAirplayCommandHandler builds a command handler with a 3s overall
// timeout (which envelopes Resolve + the DACP request).
func NewAirplayCommandHandler(session *airplay.Session, commander DACPCommander, resolver DACPResolverInterface) *AirplayCommandHandler {
	return &AirplayCommandHandler{
		session:        session,
		commander:      commander,
		resolver:       resolver,
		commandTimeout: 3 * time.Second,
	}
}

// Handle dispatches the supplied payload (the Socket.IO event body).
// The payload is the raw map socket.io hands to event listeners; the
// handler tolerates missing/invalid types and returns an error
// AirplayCommandResponse rather than panicking.
func (h *AirplayCommandHandler) Handle(ctx context.Context, payload map[string]interface{}) AirplayCommandResponse {
	cmd, _ := payload["cmd"].(string)
	if cmd == "" {
		return AirplayCommandResponse{OK: false, Error: "missing cmd"}
	}

	snap := h.session.Snapshot()
	if !snap.IsActive {
		return AirplayCommandResponse{OK: false, Error: "no active airplay session"}
	}
	if !snap.CanControl {
		return AirplayCommandResponse{OK: false, Error: "session not yet controllable (waiting for Active-Remote)"}
	}

	ctx, cancel := context.WithTimeout(ctx, h.commandTimeout)
	defer cancel()

	baseURL, err := h.resolver.Resolve(ctx, snap.DACPID)
	if err != nil {
		log.Warn().Err(err).Str("dacpID", snap.DACPID).Msg("dacp resolver failed")
		return AirplayCommandResponse{OK: false, Error: "dacp resolver: " + err.Error()}
	}

	dispatch, err := h.lookup(cmd)
	if err != nil {
		return AirplayCommandResponse{OK: false, Error: err.Error()}
	}

	if err := dispatch(ctx, baseURL, snap.ActiveRemote); err != nil {
		// Drop the cached endpoint so a stale host:port doesn't trap us
		// in repeated failures — next command re-resolves.
		h.resolver.Invalidate(snap.DACPID)
		log.Warn().Err(err).Str("cmd", cmd).Msg("dacp dispatch failed")
		return AirplayCommandResponse{OK: false, Error: err.Error()}
	}
	return AirplayCommandResponse{OK: true}
}

func (h *AirplayCommandHandler) lookup(cmd string) (func(ctx context.Context, baseURL, token string) error, error) {
	switch cmd {
	case "play":
		return h.commander.Play, nil
	case "pause":
		return h.commander.Pause, nil
	case "toggle", "playpause":
		return h.commander.PlayPause, nil
	case "next":
		return h.commander.Next, nil
	case "prev":
		return h.commander.Prev, nil
	default:
		return nil, errors.New("unknown cmd: " + cmd)
	}
}
