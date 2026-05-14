//go:build darwin

package sources

// NewPlatformMounter returns the macOS mount controller.
func NewPlatformMounter() Mounter { return NewDarwinMounter() }

// NewPlatformDiscoverer returns the macOS NAS discoverer.
func NewPlatformDiscoverer() Discoverer { return NewDarwinDiscoverer() }
