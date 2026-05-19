//go:build windows

package sources

import (
	"context"
	"errors"
	"os"
)

// ErrUnsupported is returned by the windows stub Mounter and Discoverer for
// every operation when the remote mount-control URL/token are not configured.
var ErrUnsupported = errors.New("sources: remote mount-control not configured")

// NewPlatformMounter returns a RemoteMounter when STELLAR_MOUNT_REMOTE_URL +
// STELLAR_MOUNT_REMOTE_TOKEN are both set, otherwise the windows stub. Same
// env-driven pattern as darwin for parity when the long-term Plan B Windows
// host comes online.
func NewPlatformMounter() Mounter {
	url := os.Getenv("STELLAR_MOUNT_REMOTE_URL")
	tok := os.Getenv("STELLAR_MOUNT_REMOTE_TOKEN")
	if url == "" || tok == "" {
		return windowsMounter{}
	}
	return NewRemoteMounter(url, tok)
}

// NewPlatformDiscoverer returns a RemoteDiscoverer or the windows stub.
func NewPlatformDiscoverer() Discoverer {
	url := os.Getenv("STELLAR_MOUNT_REMOTE_URL")
	tok := os.Getenv("STELLAR_MOUNT_REMOTE_TOKEN")
	if url == "" || tok == "" {
		return windowsDiscoverer{}
	}
	return NewRemoteDiscoverer(url, tok)
}

// windowsMounter is a no-op Mounter for the Windows build.
type windowsMounter struct{}

func (windowsMounter) Mount(ctx context.Context, share *NasShare) error     { return ErrUnsupported }
func (windowsMounter) Unmount(ctx context.Context, mountPoint string) error { return ErrUnsupported }
func (windowsMounter) IsMounted(mountPoint string) bool                     { return false }
func (windowsMounter) CreateMountPoint(path string) error                   { return ErrUnsupported }
func (windowsMounter) RemoveMountPoint(path string) error                   { return ErrUnsupported }
func (windowsMounter) CreateSymlink(source, target string) error            { return ErrUnsupported }
func (windowsMounter) RemoveSymlink(path string) error                      { return ErrUnsupported }

// windowsDiscoverer is a no-op Discoverer for the Windows build.
type windowsDiscoverer struct{}

func (windowsDiscoverer) DiscoverDevices(ctx context.Context) ([]NasDevice, error) {
	return []NasDevice{}, nil
}
func (windowsDiscoverer) BrowseShares(ctx context.Context, host, username, password string) ([]ShareInfo, error) {
	return nil, ErrUnsupported
}
