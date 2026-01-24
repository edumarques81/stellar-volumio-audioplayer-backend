// Package streaming provides types and interfaces for streaming service integrations.
package streaming

// StreamingSource represents a music streaming service that can be browsed.
type StreamingSource struct {
	Name       string `json:"name"`
	URI        string `json:"uri"`
	PluginType string `json:"plugin_type"`
	PluginName string `json:"plugin_name"`
	AlbumArt   string `json:"albumart"`
	Icon       string `json:"icon,omitempty"`
}

// BrowseItem represents a single item in a browse list (track, album, artist, playlist, folder).
type BrowseItem struct {
	Service     string `json:"service"`
	Type        string `json:"type"` // folder, song, album, artist, playlist
	Title       string `json:"title"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	AlbumArt    string `json:"albumart,omitempty"`
	URI         string `json:"uri"`
	Icon        string `json:"icon,omitempty"`        // Font Awesome icon class
	Duration    int    `json:"duration,omitempty"`    // Duration in seconds
	TrackNumber int    `json:"tracknumber,omitempty"` // Track number within album
	Year        int    `json:"year,omitempty"`        // Release year
	Genre       string `json:"genre,omitempty"`       // Genre
	Quality     string `json:"quality,omitempty"`     // Audio quality info (e.g., "24/192")
}

// BrowseList represents a list of browse items with a title.
type BrowseList struct {
	Title             string       `json:"title,omitempty"`
	Icon              string       `json:"icon,omitempty"`
	AvailableListView []string     `json:"availableListViews,omitempty"`
	Items             []BrowseItem `json:"items"`
}

// Navigation represents the navigation state for browse results.
type Navigation struct {
	Lists    []BrowseList `json:"lists,omitempty"`
	Info     *BrowseInfo  `json:"info,omitempty"`
	PrevURI  string       `json:"prev,omitempty"`
	IsSearch bool         `json:"isSearchResult,omitempty"`
}

// BrowseInfo provides context information about the current browse location.
type BrowseInfo struct {
	URI      string `json:"uri"`
	Title    string `json:"title,omitempty"`
	Service  string `json:"service,omitempty"`
	Type     string `json:"type,omitempty"`
	AlbumArt string `json:"albumart,omitempty"`
	Artist   string `json:"artist,omitempty"`
	Album    string `json:"album,omitempty"`
	Duration int    `json:"duration,omitempty"` // Total duration for albums/playlists
	Year     int    `json:"year,omitempty"`
}

// BrowseResult is the complete response for a browse request.
type BrowseResult struct {
	Navigation Navigation `json:"navigation"`
}

// SearchResult contains search results across multiple categories.
type SearchResult struct {
	Title  string       `json:"title"`
	Items  []BrowseItem `json:"items"`
	Count  int          `json:"count"`
	Offset int          `json:"offset,omitempty"`
	Limit  int          `json:"limit,omitempty"`
}

// StreamingCredentials holds login credentials for a streaming service.
type StreamingCredentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// StreamingStatus represents the current status of a streaming service.
type StreamingStatus struct {
	LoggedIn     bool   `json:"loggedIn"`
	Email        string `json:"email,omitempty"`
	Subscription string `json:"subscription,omitempty"` // e.g., "studio", "sublime", "hifi"
	Country      string `json:"country,omitempty"`
	Error        string `json:"error,omitempty"`
}

// LoginResult is the response for a login attempt.
type LoginResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Status  *StreamingStatus `json:"status,omitempty"`
}

// TrackStreamInfo contains information needed to play a track.
type TrackStreamInfo struct {
	URL         string `json:"url"`
	Format      string `json:"format,omitempty"`      // e.g., "flac", "mp3"
	BitDepth    int    `json:"bitDepth,omitempty"`    // e.g., 16, 24
	SampleRate  int    `json:"sampleRate,omitempty"`  // e.g., 44100, 96000, 192000
	Duration    int    `json:"duration,omitempty"`    // Duration in seconds
	MimeType    string `json:"mimeType,omitempty"`    // e.g., "audio/flac"
}

// AudioFormat represents available audio quality formats.
type AudioFormat int

const (
	// FormatMP3320 is MP3 at 320kbps CBR.
	FormatMP3320 AudioFormat = iota
	// FormatFLAC16 is 16-bit FLAC (CD quality).
	FormatFLAC16
	// FormatFLAC24_96 is 24-bit FLAC up to 96kHz (Hi-Res).
	FormatFLAC24_96
	// FormatFLAC24_192 is 24-bit FLAC up to 192kHz (Hi-Res).
	FormatFLAC24_192
)

// String returns the human-readable name of the format.
func (f AudioFormat) String() string {
	switch f {
	case FormatMP3320:
		return "MP3 320kbps"
	case FormatFLAC16:
		return "FLAC 16-bit"
	case FormatFLAC24_96:
		return "FLAC 24-bit/96kHz"
	case FormatFLAC24_192:
		return "FLAC 24-bit/192kHz"
	default:
		return "Unknown"
	}
}

// StreamingService defines the interface that all streaming services must implement.
type StreamingService interface {
	// Name returns the service name (e.g., "qobuz", "tidal").
	Name() string

	// GetBrowseSource returns the browse source entry for this service.
	// Returns nil if the service is not available (e.g., not logged in).
	GetBrowseSource() *StreamingSource

	// IsLoggedIn returns true if the user is authenticated.
	IsLoggedIn() bool

	// GetStatus returns the current status of the service.
	GetStatus() *StreamingStatus

	// Login authenticates the user with the service.
	Login(email, password string) (*LoginResult, error)

	// Logout clears the user session.
	Logout() error

	// HandleBrowseURI handles a browse request for this service.
	HandleBrowseURI(uri string) (*BrowseResult, error)

	// Search searches for content across the service.
	Search(query string, limit int) (*BrowseResult, error)

	// GetStreamURL returns the streaming URL for a track.
	GetStreamURL(trackID string) (*TrackStreamInfo, error)
}

// Config holds configuration for streaming services.
type Config struct {
	Qobuz *ServiceConfig `json:"qobuz,omitempty"`
	Tidal *ServiceConfig `json:"tidal,omitempty"`
}

// ServiceConfig holds configuration for a single streaming service.
type ServiceConfig struct {
	Email             string `json:"email,omitempty"`
	EncryptedPassword string `json:"encrypted_password,omitempty"`
	AuthToken         string `json:"auth_token,omitempty"`
}
