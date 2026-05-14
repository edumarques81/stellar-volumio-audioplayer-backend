//go:build linux

package netinfo

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// legacyGetStatus is a verbatim move of cmd/stellar/main.go:getNetworkStatus.
// It exists in this file ONLY for the duration of Commit Group 3a — Commit
// 3b deletes it after the canonical impl proves byte-equivalent.
// DO NOT add new callers; route through linuxReporter (in netinfo_linux.go)
// for any new consumer.
func legacyGetStatus() Status {
	status := Status{
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
				status.IP = legacyGetIPAddress(iface)
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
				status.IP = legacyGetIPAddress(iface)
				status.SSID, status.Signal = legacyGetWifiInfo(iface)
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

// legacyGetIPAddress is a verbatim move of main.go:getIPAddress.
func legacyGetIPAddress(iface string) string {
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
				// Remove CIDR notation
				ip := strings.Split(parts[1], "/")[0]
				return ip
			}
		}
	}
	return ""
}

// legacyGetWifiInfo is a verbatim move of main.go:getWifiInfo.
//
// IMPORTANT: this impl differs subtly from the canonical socketio version
// (now netinfo_linux.go:getWifiInfo) in the wifi-quality scaling branch
// order:
//   - canonical: `q >= 0 && q <= 70` first, then `q > 70 && q <= 100`
//   - legacy:    `q >= 0 && q <= 100` first, then `q >= 0 && q <= 70`
//
// The legacy branch order is what main.go:getWifiInfo currently does. Commit
// 3b's dedup deletes this file after byte-equality is verified against the
// captured testdata/rest_pre.json (the REST handler routes through legacy
// today, so the fixture WAS produced by this branch order; the byte-equality
// gate proves canonical produces the same wire shape).
func legacyGetWifiInfo(iface string) (string, int) {
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
				// Signal level is in field 3 (link quality) or field 4 (signal level)
				// Format is usually: interface: status link level noise
				// Signal level is typically in dBm (negative) or link quality (0-100)
				linkQuality := strings.TrimSuffix(fields[2], ".")
				if q, err := strconv.Atoi(linkQuality); err == nil {
					// If it's a percentage (0-100), use directly
					if q >= 0 && q <= 100 {
						signal = q
					} else if q >= 0 && q <= 70 {
						// It's likely link quality out of 70
						signal = (q * 100) / 70
					}
				}

				// Also try signal level in dBm (field 3)
				if signal == 0 && len(fields) >= 4 {
					sigLevel := strings.TrimSuffix(fields[3], ".")
					if dbm, err := strconv.Atoi(sigLevel); err == nil {
						// Convert dBm to percentage (-100 dBm = 0%, -50 dBm = 100%)
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
