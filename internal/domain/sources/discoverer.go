package sources

// Discoverer defines the interface for NAS device discovery.
type Discoverer interface {
	// DiscoverDevices finds NAS devices on the local network.
	DiscoverDevices() ([]NasDevice, error)

	// BrowseShares lists available shares on a NAS host.
	BrowseShares(host, username, password string) ([]ShareInfo, error)
}
