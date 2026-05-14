//go:build darwin

package netinfo

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// execCommand is a package-level indirection so tests can substitute canned
// command output. Same pattern as internal/domain/sources and internal/infra/paths.
var execCommand = exec.CommandContext

type darwinReporter struct{}

func newPlatform() Reporter { return &darwinReporter{} }

func (r *darwinReporter) GetStatus() Status {
	status := Status{Type: "none"}

	ports := listHardwarePorts()
	// Prefer ethernet if any wired port is up.
	for _, p := range ports {
		if !strings.Contains(strings.ToLower(p.port), "wi-fi") {
			if ip := ipForDevice(p.device); ip != "" {
				status.Type = "ethernet"
				status.IP = ip
				status.Signal = 100
				status.Strength = 3
				return status
			}
		}
	}

	// Otherwise try the first wi-fi port that has an IP.
	for _, p := range ports {
		if strings.Contains(strings.ToLower(p.port), "wi-fi") {
			ip := ipForDevice(p.device)
			if ip == "" {
				continue
			}
			ssid, rssi := wifiInfo(p.device)
			status.Type = "wifi"
			status.IP = ip
			status.SSID = ssid
			status.Signal = rssiToSignal(rssi)
			switch {
			case status.Signal >= 70:
				status.Strength = 3
			case status.Signal >= 50:
				status.Strength = 2
			case status.Signal >= 30:
				status.Strength = 1
			default:
				status.Strength = 0
			}
			return status
		}
	}
	return status
}

type hwPort struct {
	port   string
	device string
}

// listHardwarePorts parses `networksetup -listallhardwareports` output:
//
//	Hardware Port: Ethernet
//	Device: en0
//	Ethernet Address: ...
//
//	Hardware Port: Wi-Fi
//	Device: en1
//
// Empty lines separate entries.
func listHardwarePorts() []hwPort {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := execCommand(ctx, "/usr/sbin/networksetup", "-listallhardwareports").Output()
	if err != nil {
		log.Debug().Err(err).Msg("networksetup failed")
		return nil
	}
	var ports []hwPort
	var current hwPort
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Hardware Port:"):
			current = hwPort{port: strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port:"))}
		case strings.HasPrefix(line, "Device:"):
			current.device = strings.TrimSpace(strings.TrimPrefix(line, "Device:"))
			if current.port != "" && current.device != "" {
				ports = append(ports, current)
			}
			current = hwPort{}
		}
	}
	return ports
}

// ipForDevice runs `ifconfig <dev>` and returns the first IPv4 address.
func ipForDevice(dev string) string {
	if dev == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := execCommand(ctx, "/sbin/ifconfig", dev).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// Match "inet 192.168.1.5 netmask ..." — but NOT "inet6 fe80::..."
		if strings.HasPrefix(line, "inet ") && !strings.HasPrefix(line, "inet6 ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}

// wifiInfo returns (SSID, RSSI in dBm) for a wi-fi device. Uses the legacy
// airport command — present on macOS 10.7 through 14.x, removed in 15.x.
// When absent or failing, returns empty SSID and zero RSSI; the caller's
// rssiToSignal converts a zero RSSI to a zero Signal value.
func wifiInfo(dev string) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := execCommand(ctx,
		"/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport",
		dev, "-I").Output()
	if err != nil {
		return "", 0
	}
	ssid := ""
	rssi := 0
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SSID:") {
			ssid = strings.TrimSpace(strings.TrimPrefix(line, "SSID:"))
		}
		if strings.HasPrefix(line, "agrCtlRSSI:") {
			vs := strings.TrimSpace(strings.TrimPrefix(line, "agrCtlRSSI:"))
			rssi = parseIntForgiving(vs)
		}
	}
	return ssid, rssi
}

// parseIntForgiving parses a leading signed integer from s, stopping at the
// first non-digit. Returns 0 on parse failure. Mirrors the dBm-parsing
// behaviour from the existing socketio/main.go impls (they use strconv.Atoi
// but on RSSI strings that may have trailing whitespace or fractional
// suffixes; forgiving parse is safer for our airport-output use case).
func parseIntForgiving(s string) int {
	n, neg, i := 0, false, 0
	if len(s) > 0 && s[0] == '-' {
		neg = true
		i = 1
	}
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		n = -n
	}
	return n
}

// rssiToSignal maps RSSI in dBm to a 0-100 percentage. -100 dBm → 0,
// -50 dBm → 100. Mirrors the linux dBm-to-signal scaling.
func rssiToSignal(rssi int) int {
	if rssi == 0 {
		return 0
	}
	pct := 2 * (rssi + 100)
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// startWatcher runs the same 30s polling loop as Linux.
func startWatcher(ctx context.Context, r Reporter, brd Broadcaster) {
	go func() {
		log.Info().Msg("Network watcher started (darwin)")
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		last := r.GetStatus()
		brd.Emit("pushNetworkStatus", last)
		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("Network watcher stopped")
				return
			case <-ticker.C:
				current := r.GetStatus()
				if current.Type != last.Type ||
					current.IP != last.IP ||
					current.SSID != last.SSID ||
					current.Strength != last.Strength {
					last = current
					brd.Emit("pushNetworkStatus", current)
				}
			}
		}
	}()
}
