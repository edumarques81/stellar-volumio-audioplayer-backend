//go:build darwin

package sources

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// discoverTimeout is the per-tool budget applied to dns-sd / smbutil view.
// Matches the Linux value. It is a var (not const) so tests can shrink it
// without spawning real long-running processes.
var discoverTimeout = 6 * time.Second

// execCommand is the package-level indirection over exec.CommandContext so
// tests can substitute a stub that simulates a hanging tool without spawning
// real binaries. Not exported.
var execCommand = exec.CommandContext

// errDarwinDiscoverTimeout is returned (currently unused externally) when the
// underlying tool exceeded discoverTimeout. Distinct from "tool missing" so
// callers can tell users discovery genuinely timed out vs. silently degraded.
var errDarwinDiscoverTimeout = errors.New("discovery timed out")

// DarwinDiscoverer implements Discoverer using macOS tools.
type DarwinDiscoverer struct{}

// NewDarwinDiscoverer creates a new macOS NAS discoverer.
func NewDarwinDiscoverer() *DarwinDiscoverer { return &DarwinDiscoverer{} }

// DiscoverDevices uses dns-sd to browse for _smb._tcp services on the LAN.
// dns-sd produces an incremental stream that never terminates by itself; we
// cap with a context deadline so it doesn't block indefinitely.
//
// Output shape (relevant rows start with "Add"):
//
//	 1:39:42.456  Add        2   4 local.   _smb._tcp.           nas-music
//	 1:39:42.789  Add        2   4 local.   _smb._tcp.           NAS_Backup
func (d *DarwinDiscoverer) DiscoverDevices(ctx context.Context) ([]NasDevice, error) {
	log.Info().Msg("Starting NAS discovery (darwin)...")

	cmdCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	cmd := execCommand(cmdCtx, "/usr/bin/dns-sd", "-B", "_smb._tcp.")
	out, err := cmd.Output()
	if err != nil && !errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		// dns-sd never exits cleanly without a deadline-kill; treat anything
		// other than DeadlineExceeded as a "tool missing or broken" case.
		log.Debug().Err(err).Msg("dns-sd failed (may not be installed)")
		return nil, nil
	}

	devices := make([]NasDevice, 0)
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		// We want rows shaped like:
		//   <timestamp>  Add  <flags>  <if>  <domain>  <service>  <instance>
		// i.e. at least 7 fields and the 2nd token == "Add".
		if len(fields) < 7 {
			continue
		}
		if fields[1] != "Add" {
			continue
		}
		name := fields[len(fields)-1]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		devices = append(devices, NasDevice{
			Name: name,
			// dns-sd -B does not resolve IPs; dns-sd -L would, but smbutil
			// view accepts hostnames so we leave IP empty and provide a
			// .local hostname for downstream resolution.
			IP:       "",
			Hostname: name + ".local",
		})
	}
	log.Info().Int("count", len(devices)).Msg("NAS discovery complete (darwin)")
	return devices, nil
}

// BrowseShares uses smbutil view to list shares on a host — the macOS
// equivalent of Linux's smbclient -L.
//
// Output shape:
//
//	Share                                 Type    Comments
//	-------------------------------
//	Music                                 Disk
//	IPC$                                  IPC     Remote IPC
func (d *DarwinDiscoverer) BrowseShares(ctx context.Context, host, username, password string) ([]ShareInfo, error) {
	log.Info().Str("host", host).Msg("Browsing NAS shares (darwin)...")

	var url string
	switch {
	case username != "" && password != "":
		url = fmt.Sprintf("//%s:%s@%s", username, password, host)
	case username != "":
		url = fmt.Sprintf("//%s@%s", username, host)
	default:
		url = fmt.Sprintf("//%s", host)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	cmd := execCommand(cmdCtx, "/usr/bin/smbutil", "view", url)
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(out)
		if strings.Contains(outStr, "Authentication") || strings.Contains(outStr, "Permission denied") {
			return nil, &ShareBrowseError{Code: "AUTH_REQUIRED", Message: "authentication required"}
		}
		if strings.Contains(outStr, "No route to host") || strings.Contains(outStr, "Connection refused") {
			return nil, &ShareBrowseError{Code: "HOST_UNREACHABLE", Message: "host unreachable: " + host}
		}
		return nil, &ShareBrowseError{Code: "BROWSE_FAILED", Message: "failed to browse shares: " + err.Error()}
	}

	shares := make([]ShareInfo, 0)
	inList := false
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "---") {
			inList = true
			continue
		}
		if !inList {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			inList = false
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		shareType := strings.ToLower(parts[1])
		if name == "IPC$" || name == "ADMIN$" || name == "C$" {
			continue
		}
		comment := ""
		if len(parts) > 2 {
			comment = strings.TrimSpace(strings.Join(parts[2:], " "))
		}
		shares = append(shares, ShareInfo{
			Name:     name,
			Type:     shareType,
			Comment:  comment,
			Writable: shareType == "disk",
		})
	}
	log.Info().Int("count", len(shares)).Str("host", host).Msg("Share browse complete (darwin)")
	return shares, nil
}
