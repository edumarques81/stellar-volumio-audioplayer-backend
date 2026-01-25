package audirvana

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// Service provides Audirvana detection and discovery functionality.
type Service struct{}

// NewService creates a new Audirvana service.
func NewService() *Service {
	return &Service{}
}

// GetStatus returns the complete Audirvana status.
func (s *Service) GetStatus() Status {
	status := Status{
		Installed: s.checkInstalled(),
		Service:   s.getServiceStatus(),
		Instances: s.discoverInstances(),
	}
	return status
}

// checkInstalled checks if Audirvana binary is installed.
func (s *Service) checkInstalled() bool {
	_, err := os.Stat(Paths.Binary)
	return err == nil
}

// getServiceStatus returns the systemd service status.
func (s *Service) getServiceStatus() ServiceStatus {
	out, err := exec.Command("systemctl", "status", "audirvanaStudio", "--no-pager").CombinedOutput()
	if err != nil {
		// Command may fail with exit code 3 for inactive services, but output is still valid
		log.Debug().Err(err).Msg("systemctl status returned non-zero exit code")
	}
	return ParseSystemctlStatus(string(out))
}

// discoverInstances uses avahi-browse to find Audirvana instances on the network.
func (s *Service) discoverInstances() []Instance {
	out, err := exec.Command("avahi-browse", "-r", MDNSServiceType, "--terminate").CombinedOutput()
	if err != nil {
		log.Debug().Err(err).Msg("avahi-browse failed")
		return []Instance{}
	}
	return ParseAvahiBrowseOutput(string(out))
}

// StartService starts the Audirvana service.
func (s *Service) StartService() error {
	cmd := exec.Command("sudo", Paths.ServiceScript, "start")
	return cmd.Run()
}

// StopService stops the Audirvana service.
func (s *Service) StopService() error {
	cmd := exec.Command("sudo", Paths.ServiceScript, "stop")
	return cmd.Run()
}

// ParseAvahiBrowseOutput parses the output of avahi-browse -r _audirvana-ap._tcp --terminate.
func ParseAvahiBrowseOutput(output string) []Instance {
	if output == "" || strings.TrimSpace(output) == "" {
		return []Instance{}
	}

	instances := []Instance{}
	lines := strings.Split(output, "\n")

	var currentInstance *Instance

	for _, line := range lines {
		// Resolved service line starts with "="
		// Format: =   eth0 IPv4 stellar                                       _audirvana-ap._tcp   local
		resolvedMatch := regexp.MustCompile(`^=\s+\S+\s+(IPv[46])\s+(\S+)\s+_audirvana-ap\._tcp\s+local`).FindStringSubmatch(line)
		if resolvedMatch != nil {
			// Save previous instance if complete
			if currentInstance != nil && isInstanceComplete(currentInstance) {
				instances = append(instances, *currentInstance)
			}

			currentInstance = &Instance{
				Name: strings.TrimSpace(resolvedMatch[2]),
			}
			continue
		}

		// Parse metadata lines when we have a current instance
		if currentInstance != nil {
			// hostname = [stellar.local]
			hostnameMatch := regexp.MustCompile(`^\s+hostname\s*=\s*\[([^\]]+)\]`).FindStringSubmatch(line)
			if hostnameMatch != nil {
				currentInstance.Hostname = hostnameMatch[1]
				continue
			}

			// address = [192.168.86.34]
			addressMatch := regexp.MustCompile(`^\s+address\s*=\s*\[([^\]]+)\]`).FindStringSubmatch(line)
			if addressMatch != nil {
				currentInstance.Address = addressMatch[1]
				continue
			}

			// port = [39887]
			portMatch := regexp.MustCompile(`^\s+port\s*=\s*\[(\d+)\]`).FindStringSubmatch(line)
			if portMatch != nil {
				port, _ := strconv.Atoi(portMatch[1])
				currentInstance.Port = port
				continue
			}

			// txt = ["protovers=4.1.0" "osversion=Linux" "txtvers=1"]
			txtMatch := regexp.MustCompile(`^\s+txt\s*=\s*\[([^\]]*)\]`).FindStringSubmatch(line)
			if txtMatch != nil {
				txtContent := txtMatch[1]
				currentInstance.ProtocolVersion = extractTxtValue(txtContent, "protovers")
				if currentInstance.ProtocolVersion == "" {
					currentInstance.ProtocolVersion = "unknown"
				}
				currentInstance.OS = extractTxtValue(txtContent, "osversion")
				if currentInstance.OS == "" {
					currentInstance.OS = "unknown"
				}
				continue
			}
		}
	}

	// Don't forget the last instance
	if currentInstance != nil && isInstanceComplete(currentInstance) {
		instances = append(instances, *currentInstance)
	}

	// Deduplicate by name, preferring IPv4 addresses
	return deduplicateInstances(instances)
}

// extractTxtValue extracts a value from TXT record string.
// Input: '"protovers=4.1.0" "osversion=Linux" "txtvers=1"'
// extractTxtValue(input, "protovers") => "4.1.0"
func extractTxtValue(txtContent, key string) string {
	if txtContent == "" {
		return ""
	}
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `=([^"]+)"`)
	match := re.FindStringSubmatch(txtContent)
	if match != nil {
		return match[1]
	}
	return ""
}

// isInstanceComplete checks if an instance has all required fields.
func isInstanceComplete(instance *Instance) bool {
	return instance.Name != "" &&
		instance.Hostname != "" &&
		instance.Address != "" &&
		instance.Port > 0
}

// deduplicateInstances deduplicates instances by name, preferring IPv4 addresses over IPv6.
func deduplicateInstances(instances []Instance) []Instance {
	byName := make(map[string]Instance)

	for _, instance := range instances {
		existing, found := byName[instance.Name]

		if !found {
			byName[instance.Name] = instance
		} else {
			// Prefer IPv4 (doesn't contain ':')
			existingIsIPv4 := !strings.Contains(existing.Address, ":")
			newIsIPv4 := !strings.Contains(instance.Address, ":")

			if newIsIPv4 && !existingIsIPv4 {
				byName[instance.Name] = instance
			}
		}
	}

	result := make([]Instance, 0, len(byName))
	for _, instance := range byName {
		result = append(result, instance)
	}
	return result
}

// ParseSystemctlStatus parses systemctl status audirvanaStudio output.
func ParseSystemctlStatus(output string) ServiceStatus {
	result := ServiceStatus{
		Loaded:  false,
		Enabled: false,
		Active:  false,
		Running: false,
		PID:     0,
	}

	if output == "" || strings.Contains(output, "could not be found") {
		return result
	}

	// Check if loaded
	// Loaded: loaded (/etc/systemd/system/audirvanaStudio.service; enabled; preset: enabled)
	loadedMatch := regexp.MustCompile(`Loaded:\s+loaded\s+\([^;]+;\s*(enabled|disabled)`).FindStringSubmatch(output)
	if loadedMatch != nil {
		result.Loaded = true
		result.Enabled = loadedMatch[1] == "enabled"
	}

	// Check if active
	// Active: active (running) since ...
	// Active: inactive (dead)
	// Active: failed (Result: exit-code)
	activeMatch := regexp.MustCompile(`Active:\s+(active|inactive|failed)\s*\(([^)]+)\)`).FindStringSubmatch(output)
	if activeMatch != nil {
		result.Active = activeMatch[1] == "active"
		result.Running = activeMatch[1] == "active" && activeMatch[2] == "running"
	}

	// Extract PID if running
	// Main PID: 6448 (audirvanaStudio)
	if result.Running {
		pidMatch := regexp.MustCompile(`Main PID:\s+(\d+)`).FindStringSubmatch(output)
		if pidMatch != nil {
			result.PID, _ = strconv.Atoi(pidMatch[1])
		}
	}

	return result
}
