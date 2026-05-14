package lcd

import "github.com/rs/zerolog/log"

// BroadcastStatus reads the controller's current Status and Emits a
// pushLcdStatus event through the supplied Broadcaster. Called from the
// transport layer after every LCD power transition.
func BroadcastStatus(brd Broadcaster, c Controller) {
	status, _ := c.Status()
	brd.Emit("pushLcdStatus", status)
	log.Debug().Bool("isOn", status.IsOn).Msg("Broadcast LCD status")
}
