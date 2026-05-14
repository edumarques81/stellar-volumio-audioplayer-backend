package netinfo

import "github.com/rs/zerolog/log"

// BroadcastStatus reads the reporter's current Status and emits a
// pushNetworkStatus event through the supplied Broadcaster. Called from
// the transport layer's connect handler so newly-connected clients get
// the current network state immediately (separately from the 30s
// watcher's periodic broadcasts).
func BroadcastStatus(brd Broadcaster, r Reporter) {
	status := r.GetStatus()
	brd.Emit("pushNetworkStatus", status)
	log.Debug().
		Str("type", status.Type).
		Str("ip", status.IP).
		Int("strength", status.Strength).
		Msg("Broadcast network status")
}
