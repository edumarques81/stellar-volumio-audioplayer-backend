// Package paths centralises platform-specific filesystem locations and mount
// enumeration so the rest of the codebase can stay platform-agnostic.
package paths

import "errors"

// ErrUnsupported is returned by helpers that have no meaningful behaviour on
// the current platform (e.g., mount enumeration on Windows).
var ErrUnsupported = errors.New("paths: not supported on this platform")

// Mount describes one row of the host's mount table.
type Mount struct {
	Source     string // e.g. "//nas/Music" or "192.168.1.10:/export/music"
	MountPoint string // absolute path
	FSType     string // "cifs", "nfs", "apfs", "ntfs", "auto"
}

// DataDir returns the canonical data directory for stellar config and SQLite.
// Linux: /data/stellar. Darwin: $HOME/Library/Application Support/stellar.
// Windows: %LOCALAPPDATA%\stellar.
func DataDir() string { return dataDir() }

// CacheDir returns the canonical cache directory for derived data.
// Linux: /data/stellar/cache. Darwin: $HOME/Library/Caches/stellar.
// Windows: %LOCALAPPDATA%\stellar\cache.
func CacheDir() string { return cacheDir() }

// NasMountBase returns the base directory under which NAS shares are mounted.
// Linux: /mnt/NAS. Darwin: /Volumes/stellar-nas. Windows: stub (ErrUnsupported intent).
func NasMountBase() string { return nasMountBase() }

// UsbMountBase returns the base directory under which USB drives appear.
// Linux: /mnt/USB. Darwin: /Volumes. Windows: stub.
func UsbMountBase() string { return usbMountBase() }

// ListMounts returns the current host mount table, parsed into Mount records.
// Linux reads /proc/mounts; Darwin parses `mount(8)` output; Windows returns
// (nil, ErrUnsupported).
func ListMounts() ([]Mount, error) { return listMounts() }

// SystemHardware returns the hardware/model string for this host (best-effort).
// Linux parses /proc/cpuinfo "Model:" line; Darwin uses `sysctl hw.model`;
// Windows returns "Windows host".
func SystemHardware() string { return systemHardware() }
