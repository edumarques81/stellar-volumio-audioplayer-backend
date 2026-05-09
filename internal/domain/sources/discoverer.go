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
