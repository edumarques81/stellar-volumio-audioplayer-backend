//go:build linux

package sources

// NewPlatformMounter returns the Linux mount controller.
func NewPlatformMounter() Mounter { return NewLinuxMounter() }

// NewPlatformDiscoverer returns the Linux NAS discoverer.
func NewPlatformDiscoverer() Discoverer { return NewLinuxDiscoverer() }
