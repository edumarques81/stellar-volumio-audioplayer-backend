//go:build darwin

package sources

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// mountCommand is the package-level indirection over exec.CommandContext so
// tests can substitute a stub that simulates a hanging mount/umount without
// invoking the real binaries. Matches the pattern used in mounter_linux.go.
//
// Production behaviour: when the supplied ctx fires, the wrapped *exec.Cmd is
// killed via SIGKILL by the os/exec package, freeing the handler goroutine.
var mountCommand = exec.CommandContext

// DarwinMounter implements the Mounter interface using macOS mount commands.
// SMB shares use /sbin/mount_smbfs with URL-style auth; NFS shares use
// /sbin/mount_nfs.
type DarwinMounter struct{}

// NewDarwinMounter creates a new macOS mounter.
func NewDarwinMounter() *DarwinMounter { return &DarwinMounter{} }

// Mount mounts a NAS share using the appropriate macOS mount helper. The ctx
// bounds the underlying mount syscall — when it fires the mount process is
// killed and an error wrapping ctx.Err() is returned.
func (m *DarwinMounter) Mount(ctx context.Context, share *NasShare) error {
	switch share.FSType {
	case "cifs", "smbfs":
		return m.mountCifs(ctx, share)
	case "nfs":
		return m.mountNfs(ctx, share)
	default:
		return fmt.Errorf("unsupported filesystem type: %s", share.FSType)
	}
}

// mountCifs uses /sbin/mount_smbfs with URL-style auth:
//
//	/sbin/mount_smbfs //user:pass@host/share /mount/point
//
// If username is empty the URL collapses to //host/share (anonymous).
func (m *DarwinMounter) mountCifs(ctx context.Context, share *NasShare) error {
	var url string
	switch {
	case share.Username != "" && share.Password != "":
		url = fmt.Sprintf("//%s:%s@%s/%s", share.Username, share.Password, share.IP, share.Path)
	case share.Username != "":
		url = fmt.Sprintf("//%s@%s/%s", share.Username, share.IP, share.Path)
	default:
		url = fmt.Sprintf("//%s/%s", share.IP, share.Path)
	}

	cmd := mountCommand(ctx, "/sbin/mount_smbfs", url, share.MountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Warn().Str("url", redactURL(url)).Msg("SMB mount timed out")
			return fmt.Errorf("mount timed out: %w", ctx.Err())
		}
		log.Error().
			Err(err).
			Str("url", redactURL(url)).
			Str("mountPoint", share.MountPoint).
			Str("output", string(output)).
			Msg("SMB mount failed")
		return fmt.Errorf("mount failed: %s", string(output))
	}
	log.Info().
		Str("url", redactURL(url)).
		Str("mountPoint", share.MountPoint).
		Msg("SMB share mounted")
	share.Mounted = true
	return nil
}

// mountNfs uses /sbin/mount_nfs:
//
//	/sbin/mount_nfs <host>:<path> /mount/point
func (m *DarwinMounter) mountNfs(ctx context.Context, share *NasShare) error {
	source := fmt.Sprintf("%s:%s", share.IP, share.Path)
	cmd := mountCommand(ctx, "/sbin/mount_nfs", source, share.MountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Warn().Str("source", source).Msg("NFS mount timed out")
			return fmt.Errorf("mount timed out: %w", ctx.Err())
		}
		log.Error().
			Err(err).
			Str("source", source).
			Str("mountPoint", share.MountPoint).
			Str("output", string(output)).
			Msg("NFS mount failed")
		return fmt.Errorf("mount failed: %s", string(output))
	}
	log.Info().Str("source", source).Str("mountPoint", share.MountPoint).Msg("NFS share mounted")
	share.Mounted = true
	return nil
}

// Unmount unmounts a filesystem at the given mount point. The ctx bounds the
// underlying umount call.
func (m *DarwinMounter) Unmount(ctx context.Context, mountPoint string) error {
	cmd := mountCommand(ctx, "/sbin/umount", mountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("unmount timed out: %w", ctx.Err())
		}
		return fmt.Errorf("unmount failed: %s", string(output))
	}
	log.Info().Str("mountPoint", mountPoint).Msg("Filesystem unmounted")
	return nil
}

// IsMounted parses `mount(8)` output to check whether the given mount point
// is currently mounted. macOS does not expose a /proc/mounts equivalent, so
// we shell out to /sbin/mount and parse its
//
//	<source> on <mountpoint> (<fstype>, ...)
//
// line format. Kept local to this package rather than calling paths.ListMounts
// to avoid pulling the paths dependency into the sources domain.
func (m *DarwinMounter) IsMounted(mountPoint string) bool {
	cmd := mountCommand(context.Background(), "/sbin/mount")
	out, err := cmd.Output()
	if err != nil {
		log.Error().Err(err).Msg("Failed to run mount(8)")
		return false
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		// Format: "<src> on <mountpoint> (...)"
		onIdx := strings.Index(line, " on ")
		if onIdx < 0 {
			continue
		}
		rest := line[onIdx+4:]
		parenIdx := strings.LastIndex(rest, " (")
		if parenIdx < 0 {
			continue
		}
		if rest[:parenIdx] == mountPoint {
			return true
		}
	}
	return false
}

// CreateMountPoint creates the mount-point directory and any necessary
// parents. macOS doesn't require sudo for ~/Volumes-style paths, so this is
// a plain os.MkdirAll (no sudo fallback like the Linux variant).
func (m *DarwinMounter) CreateMountPoint(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create parent dir: %w", err)
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}
	return nil
}

// RemoveMountPoint removes an empty mount-point directory.
func (m *DarwinMounter) RemoveMountPoint(path string) error {
	return os.Remove(path)
}

// CreateSymlink creates a symlink from source to target, replacing any
// existing target.
func (m *DarwinMounter) CreateSymlink(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("failed to create symlink parent: %w", err)
	}
	_ = os.Remove(target) // non-existence is fine
	if err := os.Symlink(source, target); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}
	log.Info().Str("source", source).Str("target", target).Msg("Symlink created")
	return nil
}

// RemoveSymlink removes a symlink. Returns nil if the path does not exist;
// returns an error if the path exists but is not a symlink.
func (m *DarwinMounter) RemoveSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("not a symlink: %s", path)
	}
	return os.Remove(path)
}

// redactURL masks the password segment of an SMB URL for logging:
//
//	//user:pass@host/share → //user:***@host/share
//
// Leaves unmasked URLs alone (no colon-then-at pattern present).
func redactURL(u string) string {
	at := strings.LastIndex(u, "@")
	colon := strings.Index(u, ":")
	if at < 0 || colon < 0 || colon >= at {
		return u
	}
	// keep "//user:" then mask up to "@"
	return u[:colon+1] + "***" + u[at:]
}
