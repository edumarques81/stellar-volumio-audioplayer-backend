// Package audirvana provides Audirvana Studio detection and discovery.
package audirvana

// Instance represents a discovered Audirvana instance on the network.
type Instance struct {
	Name            string `json:"name"`
	Hostname        string `json:"hostname"`
	Address         string `json:"address"`
	Port            int    `json:"port"`
	ProtocolVersion string `json:"protocol_version"`
	OS              string `json:"os"`
}

// ServiceStatus represents the systemd service status of Audirvana.
type ServiceStatus struct {
	Loaded  bool `json:"loaded"`
	Enabled bool `json:"enabled"`
	Active  bool `json:"active"`
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
}

// Status represents the complete Audirvana status.
type Status struct {
	Installed bool          `json:"installed"`
	Service   ServiceStatus `json:"service"`
	Instances []Instance    `json:"instances"`
	Error     string        `json:"error,omitempty"`
}

// Paths contains the default installation paths for Audirvana Studio.
var Paths = struct {
	Binary        string
	ServiceScript string
	ConfigDir     string
	DataDir       string
	LogFile       string
	SystemdUnit   string
}{
	Binary:        "/opt/audirvana/studio/audirvanaStudio",
	ServiceScript: "/opt/audirvana/studio/setAsService.sh",
	ConfigDir:     "~/.config/audirvana",
	DataDir:       "~/.local/share/audirvana",
	LogFile:       "~/.local/share/audirvana/audirvana_studio.log",
	SystemdUnit:   "/etc/systemd/system/audirvanaStudio.service",
}

// MDNSServiceType is the mDNS service type for Audirvana discovery.
const MDNSServiceType = "_audirvana-ap._tcp"
