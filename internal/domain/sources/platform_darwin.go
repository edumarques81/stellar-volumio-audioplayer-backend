//go:build darwin

package sources

import (
	"context"
	"errors"
	"os"
)

// ErrUnsupported is returned by the fallback stub when the remote
// stellar-mount-control URL/token are not configured. Matches the existing
// windows stub's sentinel name so callers that errors.Is-check it work
// uniformly across platforms.
var ErrUnsupported = errors.New("sources: remote mount-control not configured")

// NewPlatformMounter returns a RemoteMounter when STELLAR_MOUNT_REMOTE_URL +
// STELLAR_MOUNT_REMOTE_TOKEN are both set, otherwise a no-op stub that
// returns ErrUnsupported. The stub preserves M1.A's guarantee that the
// backend compiles + boots cleanly on a fresh dev Mac with no env file.
//
// The previous DarwinMounter (mount_smbfs / mount_nfs local impls) was
// removed in M1.C — mounting on the Mac is not useful in the cutover
// topology because MPD lives on the Pi.
func NewPlatformMounter() Mounter {
	url := os.Getenv("STELLAR_MOUNT_REMOTE_URL")
	tok := os.Getenv("STELLAR_MOUNT_REMOTE_TOKEN")
	if url == "" || tok == "" {
		return darwinStubMounter{}
	}
	return NewRemoteMounter(url, tok)
}

// NewPlatformDiscoverer returns a RemoteDiscoverer or a no-op stub on the
// same env-driven logic.
func NewPlatformDiscoverer() Discoverer {
	url := os.Getenv("STELLAR_MOUNT_REMOTE_URL")
	tok := os.Getenv("STELLAR_MOUNT_REMOTE_TOKEN")
	if url == "" || tok == "" {
		return darwinStubDiscoverer{}
	}
	return NewRemoteDiscoverer(url, tok)
}

// darwinStubMounter is the no-op fallback returning ErrUnsupported for any
// state-changing op. Read ops return harmless zero values.
type darwinStubMounter struct{}

func (darwinStubMounter) Mount(ctx context.Context, share *NasShare) error     { return ErrUnsupported }
func (darwinStubMounter) Unmount(ctx context.Context, mountPoint string) error { return ErrUnsupported }
func (darwinStubMounter) IsMounted(mountPoint string) bool                     { return false }
func (darwinStubMounter) CreateMountPoint(path string) error                   { return ErrUnsupported }
func (darwinStubMounter) RemoveMountPoint(path string) error                   { return ErrUnsupported }
func (darwinStubMounter) CreateSymlink(source, target string) error            { return ErrUnsupported }
func (darwinStubMounter) RemoveSymlink(path string) error                      { return ErrUnsupported }

type darwinStubDiscoverer struct{}

func (darwinStubDiscoverer) DiscoverDevices(ctx context.Context) ([]NasDevice, error) {
	return []NasDevice{}, nil
}
func (darwinStubDiscoverer) BrowseShares(ctx context.Context, host, username, password string) ([]ShareInfo, error) {
	return nil, ErrUnsupported
}
