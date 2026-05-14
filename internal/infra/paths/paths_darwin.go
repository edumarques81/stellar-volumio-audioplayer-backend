//go:build darwin

package paths

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// execCommand is a package-level indirection so tests can substitute canned
// `mount` / `sysctl` output. Same pattern as internal/domain/sources.
var execCommand = exec.CommandContext

func dataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "stellar")
}

func cacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Caches", "stellar")
}

func nasMountBase() string { return "/Volumes/stellar-nas" }
func usbMountBase() string { return "/Volumes" }

// listMounts parses `mount(8)` output. Format per row:
//   <source> on <mountpoint> (<fstype>, <opt>, ...)
// Example:
//   /dev/disk1s1 on / (apfs, local, journaled)
//   //user@nas/Music on /Volumes/Music (smbfs, nodev)
func listMounts() ([]Mount, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := execCommand(ctx, "/sbin/mount").Output()
	if err != nil {
		return nil, err
	}
	return parseDarwinMountOutput(string(out)), nil
}

// parseDarwinMountOutput splits mount(8) lines into Mount records. Extracted
// for fixture-driven testing (parallels parseMounts on Linux).
func parseDarwinMountOutput(out string) []Mount {
	var mounts []Mount
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Split on the literal " on " separator (mountpoint paths never contain " on ").
		onIdx := strings.Index(line, " on ")
		if onIdx < 0 {
			continue
		}
		source := line[:onIdx]
		rest := line[onIdx+4:]
		// rest is "<mountpoint> (<fstype>, ...)"
		parenIdx := strings.LastIndex(rest, " (")
		if parenIdx < 0 {
			continue
		}
		mountPoint := rest[:parenIdx]
		opts := strings.TrimSuffix(rest[parenIdx+2:], ")")
		fsType := strings.SplitN(opts, ",", 2)[0]
		mounts = append(mounts, Mount{
			Source:     source,
			MountPoint: mountPoint,
			FSType:     fsType,
		})
	}
	return mounts
}

func systemHardware() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := execCommand(ctx, "/usr/sbin/sysctl", "-n", "hw.model").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
