// Package sources provides music source management (NAS and USB).
package sources

// NasShare represents a configured NAS share.
type NasShare struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IP         string `json:"ip"`
	Path       string `json:"path"`
	FSType     string `json:"fstype"` // "cifs" or "nfs"
	Username   string `json:"username,omitempty"`
	Password   string `json:"-"` // Never serialized to JSON responses
	Options    string `json:"options,omitempty"`
	Mounted    bool   `json:"mounted"`
	MountPoint string `json:"mountPoint"`
}

// NOTE: NasDevice, ShareInfo, and UsbDrive types will be added in Phase 2/3
// when NAS discovery and USB detection are implemented.

// SourceResult represents the result of a source operation.
type SourceResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// AddNasShareRequest represents a request to add a NAS share.
type AddNasShareRequest struct {
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Path     string `json:"path"`
	FSType   string `json:"fstype"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Options  string `json:"options,omitempty"`
}

// Config represents the persistent configuration for music sources.
type Config struct {
	NasShares map[string]*NasShareConfig `json:"nasShares"`
}

// NasShareConfig is the persistent configuration for a NAS share.
type NasShareConfig struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	IP                string `json:"ip"`
	Path              string `json:"path"`
	FSType            string `json:"fstype"`
	Username          string `json:"username,omitempty"`
	EncryptedPassword string `json:"password,omitempty"` // Encrypted
	Options           string `json:"options,omitempty"`
}
