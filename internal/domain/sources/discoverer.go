package sources

import "context"

// Discoverer defines the interface for NAS device discovery.
type Discoverer interface {
	// DiscoverDevices finds NAS devices on the local network.
	// The context controls the overall budget for discovery; implementations
	// must apply their own per-tool timeouts derived from it (typically a few
	// seconds shorter than any caller-side timeout).
	DiscoverDevices(ctx context.Context) ([]NasDevice, error)

	// BrowseShares lists available shares on a NAS host.
	// The context controls the budget for the browse operation.
	BrowseShares(ctx context.Context, host, username, password string) ([]ShareInfo, error)
}

// ShareBrowseError is a structured error returned by Discoverer.BrowseShares
// implementations. Code is a stable token (AUTH_REQUIRED / HOST_UNREACHABLE
// / BROWSE_FAILED) that callers can switch on to render the right UI; Message
// is the human-readable text. Declared here (rather than in the per-platform
// _linux.go / _darwin.go files) so callers that need to type-assert do not
// have to be themselves build-tagged.
type ShareBrowseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ShareBrowseError) Error() string {
	return e.Message
}
