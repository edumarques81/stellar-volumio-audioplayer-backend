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

// NasDevice represents a discovered NAS device on the network.
type NasDevice struct {
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname,omitempty"`
}

// ShareInfo represents an available share on a NAS device.
type ShareInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "disk", "printer", "ipc"
	Comment  string `json:"comment,omitempty"`
	Writable bool   `json:"writable"`
}

// DiscoverResult represents the result of NAS discovery.
type DiscoverResult struct {
	Devices []NasDevice `json:"devices"`
	Error   string      `json:"error,omitempty"`
}

// BrowseSharesResult represents the result of browsing NAS shares.
type BrowseSharesResult struct {
	Shares []ShareInfo `json:"shares"`
	Error  string      `json:"error,omitempty"`
}

// NOTE: UsbDrive types will be added in Phase 3
// when USB detection is implemented.

// SourceResult represents the result of a source operation.
type SourceResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// MountResult represents the result of mounting a single share.
type MountResult struct {
	ShareID   string `json:"shareId"`
	ShareName string `json:"shareName"`
	Success   bool   `json:"success"`
	Mounted   bool   `json:"mounted"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
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
	FSType            string `json:"fsType"`
	Username          string `json:"username,omitempty"`
	EncryptedPassword string `json:"password,omitempty"` // Stored encrypted
	Options           string `json:"options,omitempty"`
}
