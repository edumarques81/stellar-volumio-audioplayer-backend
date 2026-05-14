//go:build linux

package netinfo

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// linuxReporter is the canonical Linux Reporter — body moved verbatim from
// internal/transport/socketio/network.go (was: GetNetworkStatus).
type linuxReporter struct{}

func newPlatform() Reporter { return &linuxReporter{} }

func (r *linuxReporter) GetStatus() Status {
	return getStatusFromSysClassNet()
}

// getStatusFromSysClassNet — verbatim move of socketio.GetNetworkStatus body,
// with only the NetworkStatus → Status type rename. Reads /sys/class/net for
// carrier/operstate, then ip + iwgetid + /proc/net/wireless for details.
func getStatusFromSysClassNet() Status {
	status := Status{
		Type:     "none",
		Signal:   0,
		Strength: 0,
	}

	for _, iface := range []string{"eth0", "end0"} {
		carrierPath := "/sys/class/net/" + iface + "/carrier"
		if data, err := os.ReadFile(carrierPath); err == nil {
			if strings.TrimSpace(string(data)) == "1" {
				status.Type = "ethernet"
				status.IP = getIPAddress(iface)
				status.Signal = 100
				status.Strength = 3
				return status
			}
		}
	}

	for _, iface := range []string{"wlan0", "wlan1"} {
		operstatePath := "/sys/class/net/" + iface + "/operstate"
		if data, err := os.ReadFile(operstatePath); err == nil {
			if strings.TrimSpace(string(data)) == "up" {
				status.Type = "wifi"
				status.IP = getIPAddress(iface)
				status.SSID, status.Signal = getWifiInfo(iface)
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
	}
	return status
}

func getIPAddress(iface string) string {
	out, err := exec.Command("ip", "-4", "addr", "show", iface).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return strings.Split(parts[1], "/")[0]
			}
		}
	}
	return ""
}

func getWifiInfo(iface string) (string, int) {
	ssid := ""
	signal := 0
	out, err := exec.Command("iwgetid", iface, "-r").Output()
	if err == nil {
		ssid = strings.TrimSpace(string(out))
	}
	file, err := os.Open("/proc/net/wireless")
	if err != nil {
		return ssid, signal
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, iface) {
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				linkQuality := strings.TrimSuffix(fields[2], ".")
				if q, err := strconv.Atoi(linkQuality); err == nil {
					if q >= 0 && q <= 70 {
						signal = (q * 100) / 70
					} else if q > 70 && q <= 100 {
						signal = q
					}
				}
				if signal == 0 && len(fields) >= 4 {
					sigLevel := strings.TrimSuffix(fields[3], ".")
					if dbm, err := strconv.Atoi(sigLevel); err == nil {
						if dbm < 0 {
							signal = 2 * (dbm + 100)
							if signal < 0 {
								signal = 0
							}
							if signal > 100 {
								signal = 100
							}
						}
					}
				}
			}
			break
		}
	}
	return ssid, signal
}

// startWatcher polls every 30s and Emits pushNetworkStatus on change.
// Moved verbatim from socketio.Server.StartNetworkWatcher — the change-detect
// + 30s ticker pattern is preserved exactly. Initial-state emit is preserved
// (first GetStatus result is emitted before the polling loop starts).
func startWatcher(ctx context.Context, r Reporter, brd Broadcaster) {
	go func() {
		log.Info().Msg("Network watcher started")
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
					log.Debug().
						Str("oldType", last.Type).
						Str("newType", current.Type).
						Str("oldIP", last.IP).
						Str("newIP", current.IP).
						Msg("Network status changed")
					last = current
					brd.Emit("pushNetworkStatus", current)
				}
			}
		}
	}()
}
