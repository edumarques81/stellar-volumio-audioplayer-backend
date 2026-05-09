package sources

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// discoverTimeout is the per-tool budget applied to nmblookup / avahi-browse.
// Kept slightly under the frontend's 8s discovery budget so the backend is the
// first to give up and the channel is freed. It is a var (not const) so tests
// can shrink it without spawning real long-running processes.
var discoverTimeout = 6 * time.Second

// execCommand is a package-level indirection over exec.CommandContext so tests
// can substitute a stub that simulates a hanging tool without spawning real
// binaries. Not exported.
var execCommand = exec.CommandContext

// errDiscoverTimeout is returned by helpers when their underlying command
// exceeded discoverTimeout. It is distinct from "tool missing" so callers can
// tell users discovery genuinely timed out vs. silently degraded.
var errDiscoverTimeout = errors.New("discovery timed out")

// LinuxDiscoverer implements Discoverer using Linux tools.
type LinuxDiscoverer struct{}

// NewLinuxDiscoverer creates a new Linux-based NAS discoverer.
func NewLinuxDiscoverer() *LinuxDiscoverer {
	return &LinuxDiscoverer{}
}

// DiscoverDevices finds NAS devices on the local network using nmblookup.
// A timeout (errDiscoverTimeout) from any underlying tool bubbles up so the
// service layer can populate DiscoverResult.Error with a clear message.
func (d *LinuxDiscoverer) DiscoverDevices(ctx context.Context) ([]NasDevice, error) {
	log.Info().Msg("Starting NAS discovery...")
	devices := make([]NasDevice, 0)
	seen := make(map[string]bool)
	var firstTimeoutErr error

	// Method 1: Use nmblookup to find SMB servers
	nmbDevices, err := d.discoverViaNmblookup(ctx)
	if err != nil && firstTimeoutErr == nil {
		firstTimeoutErr = err
	}
	for _, device := range nmbDevices {
		if !seen[device.IP] {
			devices = append(devices, device)
			seen[device.IP] = true
		}
	}

	// Method 2: Use avahi-browse for mDNS/Bonjour SMB services
	avahiDevices, err := d.discoverViaAvahi(ctx)
	if err != nil && firstTimeoutErr == nil {
		firstTimeoutErr = err
	}
	for _, device := range avahiDevices {
		if !seen[device.IP] {
			devices = append(devices, device)
			seen[device.IP] = true
		}
	}

	log.Info().Int("count", len(devices)).Msg("NAS discovery complete")
	// If both methods surfaced a timeout AND we found nothing, propagate the
	// timeout so the frontend can show a helpful error. If we got results
	// despite a timeout from one method, prefer returning what we have.
	if firstTimeoutErr != nil && len(devices) == 0 {
		return devices, firstTimeoutErr
	}
	return devices, nil
}

// discoverViaNmblookup uses nmblookup to find SMB servers. Returns
// errDiscoverTimeout if the command exceeded discoverTimeout. A "tool not
// installed" failure is logged at Debug and returns a nil error so missing
// optional tools don't surface as user-visible errors.
func (d *LinuxDiscoverer) discoverViaNmblookup(ctx context.Context) ([]NasDevice, error) {
	devices := make([]NasDevice, 0)

	cmdCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	// Run: nmblookup -S '*'
	// This broadcasts to find all NetBIOS names on the network
	cmd := execCommand(cmdCtx, "nmblookup", "-S", "*")
	output, err := cmd.Output()
	if err != nil {
		// Distinguish timeout from "tool missing": if our derived context's
		// deadline fired, treat as timeout.
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			log.Warn().Msg("nmblookup timed out")
			return devices, errDiscoverTimeout
		}
		log.Debug().Err(err).Msg("nmblookup failed (may not be installed)")
		return devices, nil
	}

	// Parse output like:
	// 192.168.1.10 NAS1<00>
	// Looking up status of 192.168.1.10
	// NAS1           <00> -         B <ACTIVE>
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Look for lines with IP and name
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			ip := parts[0]
			// Check if first field is an IP address
			if net.ParseIP(ip) != nil {
				name := strings.TrimSuffix(parts[1], "<00>")
				name = strings.TrimSuffix(name, "<20>")
				if name != "" && name != "*" {
					devices = append(devices, NasDevice{
						Name:     name,
						IP:       ip,
						Hostname: name,
					})
				}
			}
		}
	}

	return devices, nil
}

// discoverViaAvahi uses avahi-browse to find SMB services. Returns
// errDiscoverTimeout if the command exceeded discoverTimeout. A missing tool
// is treated as benign.
func (d *LinuxDiscoverer) discoverViaAvahi(ctx context.Context) ([]NasDevice, error) {
	devices := make([]NasDevice, 0)

	cmdCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	// Run: avahi-browse -rt _smb._tcp
	// -r = resolve addresses, -t = terminate after scanning
	cmd := execCommand(cmdCtx, "avahi-browse", "-rtp", "_smb._tcp")
	output, err := cmd.Output()
	if err != nil {
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			log.Warn().Msg("avahi-browse timed out")
			return devices, errDiscoverTimeout
		}
		log.Debug().Err(err).Msg("avahi-browse failed (may not be installed)")
		return devices, nil
	}

	// Parse output - avahi-browse -p outputs parseable format:
	// +;eth0;IPv4;NAS;_smb._tcp;local
	// =;eth0;IPv4;NAS;_smb._tcp;local;nas.local;192.168.1.10;445;"..."
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "=") {
			continue
		}

		parts := strings.Split(line, ";")
		if len(parts) < 8 {
			continue
		}

		name := parts[3]
		hostname := parts[6]
		ip := parts[7]

		if ip != "" && name != "" {
			devices = append(devices, NasDevice{
				Name:     name,
				IP:       ip,
				Hostname: hostname,
			})
		}
	}

	return devices, nil
}

// BrowseShares lists available shares on a NAS host using smbclient.
func (d *LinuxDiscoverer) BrowseShares(ctx context.Context, host, username, password string) ([]ShareInfo, error) {
	log.Info().Str("host", host).Msg("Browsing NAS shares...")
	shares := make([]ShareInfo, 0)

	// Build smbclient command
	// smbclient -L //host -N (anonymous) or -U user%pass
	args := []string{"-L", "//" + host}

	if username != "" && password != "" {
		args = append(args, "-U", username+"%"+password)
	} else if username != "" {
		args = append(args, "-U", username, "-N")
	} else {
		args = append(args, "-N") // No password (anonymous/guest)
	}

	// Add timeout (smbclient's own --timeout is in seconds; we keep it for
	// belt-and-suspenders alongside the context deadline below).
	args = append(args, "--timeout=5")

	cmd := execCommand(ctx, "smbclient", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		outputStr := string(output)
		// Check for specific errors
		if strings.Contains(outputStr, "NT_STATUS_ACCESS_DENIED") ||
			strings.Contains(outputStr, "NT_STATUS_LOGON_FAILURE") {
			return nil, &ShareBrowseError{
				Code:    "AUTH_REQUIRED",
				Message: "authentication required",
			}
		}
		if strings.Contains(outputStr, "NT_STATUS_CONNECTION_REFUSED") ||
			strings.Contains(outputStr, "NT_STATUS_HOST_UNREACHABLE") {
			return nil, &ShareBrowseError{
				Code:    "HOST_UNREACHABLE",
				Message: "host unreachable: " + host,
			}
		}
		log.Debug().Err(err).Str("output", outputStr).Msg("smbclient failed")
		return nil, &ShareBrowseError{
			Code:    "BROWSE_FAILED",
			Message: "failed to browse shares: " + err.Error(),
		}
	}

	// Parse smbclient output
	// Format:
	// 	Sharename       Type      Comment
	// 	---------       ----      -------
	// 	Music           Disk      Music files
	// 	Videos          Disk      Video collection
	// 	IPC$            IPC       Remote IPC
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	inShareList := false

	for scanner.Scan() {
		line := scanner.Text()

		// Detect the header separator line
		if strings.HasPrefix(strings.TrimSpace(line), "---") {
			inShareList = true
			continue
		}

		// Skip until we're in the share list
		if !inShareList {
			continue
		}

		// Empty line or end of list
		if strings.TrimSpace(line) == "" {
			inShareList = false
			continue
		}

		// Parse share line
		share := parseShareLine(line)
		if share != nil {
			// Skip special shares
			if share.Name == "IPC$" || share.Name == "ADMIN$" || share.Name == "C$" {
				continue
			}
			shares = append(shares, *share)
		}
	}

	log.Info().Int("count", len(shares)).Str("host", host).Msg("Share browse complete")
	return shares, nil
}

// parseShareLine parses a line from smbclient -L output.
func parseShareLine(line string) *ShareInfo {
	// Line format: "	ShareName       Type      Comment"
	// Fields are separated by multiple spaces
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	// Split by multiple spaces (smbclient uses column alignment)
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil
	}

	name := parts[0]
	shareType := strings.ToLower(parts[1])

	// Get comment (everything after name and type)
	comment := ""
	if len(parts) > 2 {
		// Find where comment starts
		typeIndex := strings.Index(line, parts[1])
		if typeIndex > 0 {
			afterType := line[typeIndex+len(parts[1]):]
			comment = strings.TrimSpace(afterType)
		}
	}

	return &ShareInfo{
		Name:     name,
		Type:     shareType,
		Comment:  comment,
		Writable: shareType == "disk",
	}
}

// ShareBrowseError represents an error during share browsing.
type ShareBrowseError struct {
	Code    string
	Message string
}

func (e *ShareBrowseError) Error() string {
	return e.Message
}
