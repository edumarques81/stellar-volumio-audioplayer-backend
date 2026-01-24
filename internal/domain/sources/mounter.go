package sources

// Mounter defines the interface for mount operations.
// This allows for mocking in tests and different implementations.
type Mounter interface {
	// Mount mounts a NAS share and updates the share's Mounted status.
	Mount(share *NasShare) error

	// Unmount unmounts a filesystem at the given mount point.
	Unmount(mountPoint string) error

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
