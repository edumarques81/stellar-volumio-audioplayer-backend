//go:build windows

package sources

import (
	"context"
	"errors"
)

// ErrUnsupported is returned by the Windows stub Mounter and Discoverer for
// every operation. NAS mounting and discovery are not implemented for the
// Windows build target — it exists only as the long-term Plan B host so the
// rest of the binary links cleanly on windows/amd64.
var ErrUnsupported = errors.New("sources: not supported on this platform")

// windowsMounter is a no-op Mounter implementation for the Windows build.
// All methods return ErrUnsupported.
type windowsMounter struct{}

// NewPlatformMounter returns the Windows stub mount controller.
func NewPlatformMounter() Mounter { return &windowsMounter{} }

func (windowsMounter) Mount(ctx context.Context, share *NasShare) error     { return ErrUnsupported }
func (windowsMounter) Unmount(ctx context.Context, mountPoint string) error { return ErrUnsupported }
func (windowsMounter) IsMounted(mountPoint string) bool                     { return false }
func (windowsMounter) CreateMountPoint(path string) error                   { return ErrUnsupported }
func (windowsMounter) RemoveMountPoint(path string) error                   { return ErrUnsupported }
func (windowsMounter) CreateSymlink(source, target string) error            { return ErrUnsupported }
func (windowsMounter) RemoveSymlink(path string) error                      { return ErrUnsupported }

// windowsDiscoverer is a no-op Discoverer implementation for the Windows
// build. All methods return ErrUnsupported.
type windowsDiscoverer struct{}

// NewPlatformDiscoverer returns the Windows stub NAS discoverer.
func NewPlatformDiscoverer() Discoverer { return &windowsDiscoverer{} }

func (windowsDiscoverer) DiscoverDevices(ctx context.Context) ([]NasDevice, error) {
	return nil, ErrUnsupported
}

func (windowsDiscoverer) BrowseShares(ctx context.Context, host, username, password string) ([]ShareInfo, error) {
	return nil, ErrUnsupported
}
