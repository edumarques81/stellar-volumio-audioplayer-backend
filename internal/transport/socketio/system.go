// Package socketio provides the Socket.io server for client communication.
package socketio

import (
	"os"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/paths"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/version"
)

// SystemInfo represents basic system information.
type SystemInfo struct {
	ID            string `json:"id"`            // Unique device ID
	Host          string `json:"host"`          // Hostname
	Name          string `json:"name"`          // Display name
	Type          string `json:"type"`          // Device type
	ServiceName   string `json:"serviceName"`   // Service name for mDNS
	SystemVersion string `json:"systemversion"` // System version
	BuildDate     string `json:"builddate"`     // Build date
	Variant       string `json:"variant"`       // System variant
	Hardware      string `json:"hardware"`      // Hardware model
}

// GetSystemInfo returns basic system information.
func GetSystemInfo() SystemInfo {
	info := SystemInfo{
		Type:          "audio_player",
		ServiceName:   "stellar",
		SystemVersion: version.GetInfo().Version,
		BuildDate:     version.GetInfo().BuildTime,
		Variant:       "stellar-pi",
		Hardware:      "Raspberry Pi",
	}

	// Get hostname
	if hostname, err := os.Hostname(); err == nil {
		info.Host = hostname
		info.Name = hostname
		info.ID = hostname
	}

	if hw := paths.SystemHardware(); hw != "" {
		info.Hardware = hw
	}

	return info
}
