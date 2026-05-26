package socketio

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/airplay"
)

// StartAirplayHeartbeatWatcher periodically calls Session.TickExpire so
// stale sessions (no heartbeat for HeartbeatTimeout) are cleared and
// pushAirplayEnded is broadcast to clients. The watcher ticks every
// tickEvery and returns when ctx is cancelled.
//
// Wiring: cmd/stellar/main.go calls this once during boot, after the
// HTTP server is up. tickEvery = 1s, HeartbeatTimeout (set on the
// Session) = 5s (overridable via STELLAR_AIRPLAY_SESSION_TIMEOUT_MS).
func StartAirplayHeartbeatWatcher(ctx context.Context, session *airplay.Session, emit EmitFunc, tickEvery time.Duration) {
	if tickEvery <= 0 {
		tickEvery = time.Second
	}
	go func() {
		ticker := time.NewTicker(tickEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ended, id := session.TickExpire()
				if ended && emit != nil {
					emit("pushAirplayEnded", map[string]string{"sessionID": id})
					log.Info().Str("sessionID", id).Msg("airplay session expired (heartbeat timeout)")
				}
			}
		}
	}()
}
