// Audio-configuration wire types shared by the playback-options, bit-perfect,
// mixer and DSD handlers. Kept in one file so a client-visible JSON shape is
// never changed without seeing its siblings.
package socketio

// BitPerfectStatus represents the result of a bit-perfect configuration check.
type BitPerfectStatus struct {
	Status   string   `json:"status"`   // "ok", "warning", "error"
	Issues   []string `json:"issues"`   // Critical issues preventing bit-perfect
	Warnings []string `json:"warnings"` // Non-critical warnings
	Config   []string `json:"config"`   // Current configuration details
}

// PlaybackOption represents an audio output option.
type PlaybackOption struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

// PlaybackAttribute represents an attribute in playback options.
type PlaybackAttribute struct {
	Name    string           `json:"name"`
	Type    string           `json:"type"`
	Value   string           `json:"value"`
	Options []PlaybackOption `json:"options,omitempty"`
}

// PlaybackOptionsSection represents a section in playback options.
type PlaybackOptionsSection struct {
	ID         string              `json:"id"`
	Name       string              `json:"name,omitempty"`
	Attributes []PlaybackAttribute `json:"attributes"`
}

// PlaybackOptionsResponse represents the playback options response.
type PlaybackOptionsResponse struct {
	Options     []PlaybackOptionsSection `json:"options"`
	SystemCards []string                 `json:"systemCards"`
}

// DsdModeResponse represents the DSD playback mode.
type DsdModeResponse struct {
	Mode    string `json:"mode"`    // "native" or "dop"
	Success bool   `json:"success"` // true if operation succeeded (for setDsdMode)
	Error   string `json:"error"`   // error message if failed
}

// MixerModeResponse represents the mixer configuration.
type MixerModeResponse struct {
	Enabled bool   `json:"enabled"` // true if software mixer enabled
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ApplyBitPerfectResponse represents the result of applying all bit-perfect settings.
type ApplyBitPerfectResponse struct {
	Success bool     `json:"success"`
	Applied []string `json:"applied"` // Settings that were changed
	Errors  []string `json:"errors"`  // Any errors encountered
}
