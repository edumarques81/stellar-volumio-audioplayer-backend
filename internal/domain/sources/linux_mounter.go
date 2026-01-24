package sources

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// LinuxMounter implements the Mounter interface using Linux mount commands.
type LinuxMounter struct{}

// NewLinuxMounter creates a new Linux mounter.
func NewLinuxMounter() *LinuxMounter {
	return &LinuxMounter{}
}

// Mount mounts a NAS share using the appropriate protocol.
func (m *LinuxMounter) Mount(share *NasShare) error {
	switch share.FSType {
	case "cifs":
		return m.mountCifs(share)
	case "nfs":
		return m.mountNfs(share)
	default:
		return fmt.Errorf("unsupported filesystem type: %s", share.FSType)
	}
}

// mountCifs mounts a CIFS/SMB share.
func (m *LinuxMounter) mountCifs(share *NasShare) error {
	// Build the source path: //IP/SharePath
	source := fmt.Sprintf("//%s/%s", share.IP, share.Path)

	// Build mount options
	opts := []string{
		"ro",
		"dir_mode=0777",
		"file_mode=0666",
		"iocharset=utf8",
		"noauto",
		"soft",
	}

	// Add credentials if provided
	if share.Username != "" {
		opts = append(opts, fmt.Sprintf("username=%s", share.Username))
		if share.Password != "" {
			opts = append(opts, fmt.Sprintf("password=%s", share.Password))
		}
	} else {
		opts = append(opts, "guest")
	}

	// Add custom options if provided
	if share.Options != "" {
		opts = append(opts, share.Options)
	}

	optStr := strings.Join(opts, ",")

	// Execute mount command with sudo
	cmd := exec.Command("sudo", "mount", "-t", "cifs", "-o", optStr, source, share.MountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().
			Err(err).
			Str("source", source).
			Str("mountPoint", share.MountPoint).
			Str("output", string(output)).
			Msg("CIFS mount failed")
		return fmt.Errorf("mount failed: %s", string(output))
	}

	log.Info().
		Str("source", source).
		Str("mountPoint", share.MountPoint).
		Msg("CIFS share mounted")

	share.Mounted = true
	return nil
}

// mountNfs mounts an NFS share.
func (m *LinuxMounter) mountNfs(share *NasShare) error {
	// Build the source path: IP:/path
	source := fmt.Sprintf("%s:%s", share.IP, share.Path)

	// Build mount options
	opts := []string{
		"ro",
		"soft",
		"noauto",
	}

	// Add custom options if provided
	if share.Options != "" {
		opts = append(opts, share.Options)
	}

	optStr := strings.Join(opts, ",")

	// Execute mount command with sudo
	cmd := exec.Command("sudo", "mount", "-t", "nfs", "-o", optStr, source, share.MountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().
			Err(err).
			Str("source", source).
			Str("mountPoint", share.MountPoint).
			Str("output", string(output)).
			Msg("NFS mount failed")
		return fmt.Errorf("mount failed: %s", string(output))
	}

	log.Info().
		Str("source", source).
		Str("mountPoint", share.MountPoint).
		Msg("NFS share mounted")

	share.Mounted = true
	return nil
}

// Unmount unmounts a filesystem.
func (m *LinuxMounter) Unmount(mountPoint string) error {
	cmd := exec.Command("sudo", "umount", mountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().
			Err(err).
			Str("mountPoint", mountPoint).
			Str("output", string(output)).
			Msg("Unmount failed")
		return fmt.Errorf("unmount failed: %s", string(output))
	}

	log.Info().
		Str("mountPoint", mountPoint).
		Msg("Filesystem unmounted")

	return nil
}

// IsMounted checks if a path is a mount point.
func (m *LinuxMounter) IsMounted(mountPoint string) bool {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		log.Error().Err(err).Msg("Failed to open /proc/mounts")
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[1] == mountPoint {
			return true
		}
	}

	return false
}

// CreateMountPoint creates the directory for mounting.
func (m *LinuxMounter) CreateMountPoint(path string) error {
	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		// Try with sudo
		cmd := exec.Command("sudo", "mkdir", "-p", filepath.Dir(path))
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create parent dir: %s", string(output))
		}
	}

	// Create mount point
	if err := os.MkdirAll(path, 0755); err != nil {
		cmd := exec.Command("sudo", "mkdir", "-p", path)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create mount point: %s", string(output))
		}
	}

	return nil
}

// RemoveMountPoint removes an empty mount point directory.
func (m *LinuxMounter) RemoveMountPoint(path string) error {
	if err := os.Remove(path); err != nil {
		// Try with sudo
		cmd := exec.Command("sudo", "rmdir", path)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to remove mount point: %s", string(output))
		}
	}
	return nil
}

// CreateSymlink creates a symlink from source to target.
func (m *LinuxMounter) CreateSymlink(source, target string) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		cmd := exec.Command("sudo", "mkdir", "-p", filepath.Dir(target))
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create symlink parent: %s", string(output))
		}
	}

	// Remove existing symlink if present
	os.Remove(target)

	// Create symlink
	if err := os.Symlink(source, target); err != nil {
		cmd := exec.Command("sudo", "ln", "-sf", source, target)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create symlink: %s", string(output))
		}
	}

	log.Info().
		Str("source", source).
		Str("target", target).
		Msg("Symlink created")

	return nil
}

// RemoveSymlink removes a symlink.
func (m *LinuxMounter) RemoveSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Already gone
		}
		return err
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("not a symlink: %s", path)
	}

	if err := os.Remove(path); err != nil {
		cmd := exec.Command("sudo", "rm", path)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to remove symlink: %s", string(output))
		}
	}

	return nil
}
