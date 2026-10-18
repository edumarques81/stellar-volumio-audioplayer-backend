package main

import (
	"fmt"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/netinfo"
)

// castBaseURL returns the origin a Cast device should fetch media from.
//
// It is resolved on every call rather than once at boot for two reasons: the
// backend starts before the network is reliably up (systemd orders it after
// network-online.target, which is a weaker guarantee than an address being
// assigned), and a DHCP lease change would otherwise leave every subsequent
// LOAD pointing at an address this host no longer owns.
//
// Returning an empty string when there is no address is deliberate — Cast is
// a pull protocol, so an unreachable URL fails on the device as a generic
// load error. The session manager refuses to load without a base URL instead,
// which is a message we can actually explain.
func castBaseURL(reporter netinfo.Reporter, port int) func() string {
	return func() string {
		if reporter == nil || port <= 0 {
			return ""
		}
		ip := reporter.GetStatus().IP
		if ip == "" {
			return ""
		}
		return fmt.Sprintf("http://%s:%d", ip, port)
	}
}
