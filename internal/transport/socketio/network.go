// Package socketio provides the Socket.io server for client communication.
package socketio

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

// NetworkStatus represents the current network connection status.
type NetworkStatus struct {
	Type     string `json:"type"`     // "wifi", "ethernet", "none"
	SSID     string `json:"ssid"`     // WiFi network name (if wifi)
	Signal   int    `json:"signal"`   // WiFi signal strength 0-100 (if wifi)
	IP       string `json:"ip"`       // IP address
	Strength int    `json:"strength"` // Signal strength level 0-3 (for icon)
}

// GetNetworkStatus returns the current network connection status.
func GetNetworkStatus() NetworkStatus {
	status := NetworkStatus{
		Type:     "none",
		Signal:   0,
		Strength: 0,
	}

	// Check ethernet first (usually eth0 or end0 on newer Pi)
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

	// Check WiFi (usually wlan0)
	for _, iface := range []string{"wlan0", "wlan1"} {
		operstatePath := "/sys/class/net/" + iface + "/operstate"
		if data, err := os.ReadFile(operstatePath); err == nil {
			if strings.TrimSpace(string(data)) == "up" {
				status.Type = "wifi"
				status.IP = getIPAddress(iface)
				status.SSID, status.Signal = getWifiInfo(iface)
				// Convert signal to strength level (0-3)
				switch {
				case status.Signal >= 70:
					status.Strength = 3 // Full signal
				case status.Signal >= 50:
					status.Strength = 2 // Medium
				case status.Signal >= 30:
					status.Strength = 1 // Weak
				default:
					status.Strength = 0 // Very weak
				}
				return status
			}
		}
	}

	return status
}

// getIPAddress returns the IP address for a given interface.
func getIPAddress(iface string) string {
	out, err := exec.Command("ip", "-4", "addr", "show", iface).Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ip := strings.Split(parts[1], "/")[0]
				return ip
			}
		}
	}
	return ""
}

// getWifiInfo returns SSID and signal strength (0-100) for a WiFi interface.
func getWifiInfo(iface string) (string, int) {
	ssid := ""
	signal := 0

	// Get SSID using iwgetid
	out, err := exec.Command("iwgetid", iface, "-r").Output()
	if err == nil {
		ssid = strings.TrimSpace(string(out))
	}

	// Get signal from /proc/net/wireless
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
					// Link quality can be 0-70 (iwconfig format) or 0-100 (percentage)
					// Check for 0-70 range first (more common in /proc/net/wireless)
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

// BroadcastNetworkStatus sends network status to all connected clients.
func (s *Server) BroadcastNetworkStatus() {
	status := GetNetworkStatus()
	s.io.Emit("pushNetworkStatus", status)
	log.Debug().Str("type", status.Type).Str("ip", status.IP).Int("strength", status.Strength).Msg("Broadcast network status")
}

// StartNetworkWatcher periodically checks network status and broadcasts changes.
func (s *Server) StartNetworkWatcher(ctx context.Context) {
	go func() {
		log.Info().Msg("Network watcher started")
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		// Get initial status
		s.lastNetwork = GetNetworkStatus()

		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("Network watcher stopped")
				return
			case <-ticker.C:
				current := GetNetworkStatus()
				// Only broadcast if status changed
				if current.Type != s.lastNetwork.Type ||
					current.IP != s.lastNetwork.IP ||
					current.SSID != s.lastNetwork.SSID ||
					current.Strength != s.lastNetwork.Strength {
					log.Debug().
						Str("oldType", s.lastNetwork.Type).
						Str("newType", current.Type).
						Str("oldIP", s.lastNetwork.IP).
						Str("newIP", current.IP).
						Msg("Network status changed")
					s.lastNetwork = current
					s.BroadcastNetworkStatus()
				}
			}
		}
	}()
}
