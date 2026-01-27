// Package cache provides a SQLite-based caching layer for library metadata.
package cache

import "time"

// CachedAlbum represents an album stored in the cache.
type CachedAlbum struct {
	ID            string    `json:"id"`             // MD5(albumArtist || album)
	Title         string    `json:"title"`          // Album title
	AlbumArtist   string    `json:"albumArtist"`    // Album artist
	URI           string    `json:"uri"`            // Directory path for playback
	TrackCount    int       `json:"trackCount"`     // Number of tracks
	TotalDuration int       `json:"totalDuration"`  // Total duration in seconds
	Source        string    `json:"source"`         // 'local', 'usb', 'nas', 'streaming'
	Year          int       `json:"year,omitempty"` // Release year
	AddedAt       time.Time `json:"addedAt"`        // When added to library
	LastPlayed    time.Time `json:"lastPlayed"`     // Last played timestamp
	ArtworkID     string    `json:"artworkId"`      // FK to artwork cache
	CreatedAt     time.Time `json:"createdAt"`      // Cache entry creation
	UpdatedAt     time.Time `json:"updatedAt"`      // Cache entry update
}

// CachedArtist represents an artist stored in the cache.
type CachedArtist struct {
	ID         string    `json:"id"`         // MD5(name)
	Name       string    `json:"name"`       // Artist name
	AlbumCount int       `json:"albumCount"` // Number of albums
	TrackCount int       `json:"trackCount"` // Number of tracks
	ArtworkID  string    `json:"artworkId"`  // FK to artwork (artist image)
	CreatedAt  time.Time `json:"createdAt"`  // Cache entry creation
	UpdatedAt  time.Time `json:"updatedAt"`  // Cache entry update
}

// CachedTrack represents a track stored in the cache.
type CachedTrack struct {
	ID          string    `json:"id"`                    // MD5(uri)
	AlbumID     string    `json:"albumId"`               // FK to album
	Title       string    `json:"title"`                 // Track title
	Artist      string    `json:"artist"`                // Track artist
	URI         string    `json:"uri"`                   // File path
	TrackNumber int       `json:"trackNumber"`           // Track number in album
	DiscNumber  int       `json:"discNumber,omitempty"`  // Disc number (default 1)
	Duration    int       `json:"duration"`              // Duration in seconds
	Source      string    `json:"source"`                // 'local', 'usb', 'nas'
	CreatedAt   time.Time `json:"createdAt"`             // Cache entry creation
}

// CachedArtwork represents artwork metadata in the cache.
type CachedArtwork struct {
	ID        string    `json:"id"`               // MD5(albumArtist || album || type)
	AlbumID   string    `json:"albumId"`          // FK to album (null for artist images)
	ArtistID  string    `json:"artistId"`         // FK to artist (null for album art)
	Type      string    `json:"type"`             // 'album', 'artist'
	FilePath  string    `json:"filePath"`         // Local cache path
	Source    string    `json:"source"`           // 'mpd', 'embedded', 'folder', 'musicbrainz', 'lastfm'
	MimeType  string    `json:"mimeType"`         // MIME type
	Width     int       `json:"width"`            // Image width
	Height    int       `json:"height"`           // Image height
	FileSize  int       `json:"fileSize"`         // File size in bytes
	Checksum  string    `json:"checksum"`         // MD5 of image data
	FetchedAt time.Time `json:"fetchedAt"`        // When fetched
	ExpiresAt time.Time `json:"expiresAt"`        // TTL for web-fetched images
	CreatedAt time.Time `json:"createdAt"`        // Cache entry creation
}

// CachedRadioStation represents a radio station in the cache.
type CachedRadioStation struct {
	ID        string    `json:"id"`        // Station ID
	Name      string    `json:"name"`      // Station name
	URI       string    `json:"uri"`       // Stream URL
	Icon      string    `json:"icon"`      // Icon URL
	Genre     string    `json:"genre"`     // Genre
	CreatedAt time.Time `json:"createdAt"` // Cache entry creation
	UpdatedAt time.Time `json:"updatedAt"` // Cache entry update
}

// CacheMeta holds cache metadata (schema version, last update, etc.)
type CacheMeta struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CacheStats provides statistics about the cache.
type CacheStats struct {
	AlbumCount    int       `json:"albumCount"`
	ArtistCount   int       `json:"artistCount"`
	TrackCount    int       `json:"trackCount"`
	ArtworkCount  int       `json:"artworkCount"`
	ArtworkMissing int      `json:"artworkMissing"`
	RadioCount    int       `json:"radioCount"`
	SchemaVersion string    `json:"schemaVersion"`
	LastFullBuild time.Time `json:"lastFullBuild"`
	LastUpdated   time.Time `json:"lastUpdated"`
	IsBuilding    bool      `json:"isBuilding"`
	BuildProgress int       `json:"buildProgress"` // 0-100
}

// AlbumFilter defines filters for album queries.
type AlbumFilter struct {
	Scope  string // 'all', 'nas', 'local', 'usb'
	Query  string // Search term
	Artist string // Filter by artist
}

// SortOrder defines how results should be sorted.
type SortOrder string

const (
	SortAlphabetical   SortOrder = "alphabetical"
	SortByArtist       SortOrder = "by_artist"
	SortRecentlyAdded  SortOrder = "recently_added"
	SortYear           SortOrder = "year"
)

// Pagination defines pagination parameters.
type Pagination struct {
	Page    int
	Limit   int
	Offset  int // Calculated from Page and Limit
}

// NewPagination creates a new pagination with defaults.
func NewPagination(page, limit int) Pagination {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return Pagination{
		Page:   page,
		Limit:  limit,
		Offset: (page - 1) * limit,
	}
}
