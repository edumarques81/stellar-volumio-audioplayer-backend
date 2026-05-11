package sources

import "context"

// Mounter defines the interface for mount operations.
// This allows for mocking in tests and different implementations.
//
// Mount and Unmount accept a context.Context so callers can bound the
// kernel-level mount/umount syscalls. Without this, `sudo mount -t nfs`
// against a dead host blocks the handler goroutine indefinitely (the live
// failure mode observed when a user points NFS at a Windows share).
type Mounter interface {
	// Mount mounts a NAS share and updates the share's Mounted status.
	// Honors ctx — when the context fires, the underlying mount process
	// is killed and an error wrapping ctx.Err() is returned.
	Mount(ctx context.Context, share *NasShare) error

	// Unmount unmounts a filesystem at the given mount point.
	// Honors ctx for the same reasons as Mount.
	Unmount(ctx context.Context, mountPoint string) error

	// IsMounted checks if a mount point is currently mounted.
	IsMounted(mountPoint string) bool

	// CreateMountPoint creates the directory for mounting.
	CreateMountPoint(path string) error

	// RemoveMountPoint removes an empty mount point directory.
	RemoveMountPoint(path string) error

	// CreateSymlink creates a symlink from source to target.
	CreateSymlink(source, target string) error

	// RemoveSymlink removes a symlink.
	RemoveSymlink(path string) error
}
