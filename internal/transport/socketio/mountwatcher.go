package socketio

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// StartMountWatcher periodically checks for unmounted NAS shares and attempts to remount them.
// Follows the same pattern as StartNetworkWatcher.
func (s *Server) StartMountWatcher(ctx context.Context) {
	if s.sourcesService == nil {
		log.Debug().Msg("Mount watcher not started: sources service not available")
		return
	}

	go func() {
		log.Info().Msg("Mount watcher started (60s interval)")
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("Mount watcher stopped")
				return
			case <-ticker.C:
				unmounted := s.sourcesService.GetUnmountedShares()
				if len(unmounted) == 0 {
					continue
				}

				log.Info().Int("unmounted", len(unmounted)).Msg("Mount watcher detected unmounted shares")
				mounted := s.sourcesService.RemountUnmountedShares()

				if mounted > 0 {
					log.Info().Int("remounted", mounted).Msg("Mount watcher remounted shares")

					// Trigger MPD database update
					if _, err := s.mpdClient.Update(""); err != nil {
						log.Warn().Err(err).Msg("Mount watcher: MPD update failed")
					}

					// Broadcast updated share list to all clients
					shares, err := s.sourcesService.ListNasShares()
					if err == nil {
						s.io.Emit("pushListNasShares", shares)
					}
				}
			}
		}
	}()
}
