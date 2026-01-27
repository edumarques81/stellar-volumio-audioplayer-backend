// Package artwork provides artwork resolution and caching for albums and artists.
package artwork

import (
	"errors"
	"time"
)

// ErrNoArtwork is returned when no artwork is found.
var ErrNoArtwork = errors.New("no artwork found")

// CachedArtwork represents artwork metadata stored in the cache.
type CachedArtwork struct {
	ID        string    `json:"id"`        // MD5(albumArtist || album || type)
	AlbumID   string    `json:"albumId"`   // FK to album (null for artist images)
	ArtistID  string    `json:"artistId"`  // FK to artist (null for album art)
	Type      string    `json:"type"`      // 'album', 'artist'
	FilePath  string    `json:"filePath"`  // Local cache path
	Source    string    `json:"source"`    // 'mpd', 'embedded', 'folder', 'musicbrainz', 'lastfm', 'cache', 'placeholder'
	MimeType  string    `json:"mimeType"`  // MIME type
	Width     int       `json:"width"`     // Image width
	Height    int       `json:"height"`    // Image height
	FileSize  int       `json:"fileSize"`  // File size in bytes
	Checksum  string    `json:"checksum"`  // MD5 of image data
	FetchedAt time.Time `json:"fetchedAt"` // When fetched
	ExpiresAt time.Time `json:"expiresAt"` // TTL for web-fetched images
	CreatedAt time.Time `json:"createdAt"` // Cache entry creation
}

// ResolveResult contains the result of artwork resolution.
type ResolveResult struct {
	FilePath string // Path to the artwork file
	Source   string // Source of the artwork: 'cache', 'mpd', 'embedded', 'web', 'placeholder'
	MimeType string // MIME type of the artwork
	Width    int    // Image width (0 if unknown)
	Height   int    // Image height (0 if unknown)
	FileSize int    // File size in bytes
}

// MPDArtworkProvider defines the interface for fetching artwork from MPD.
type MPDArtworkProvider interface {
	// AlbumArt retrieves folder-based album art (cover.jpg, folder.jpg, etc.)
	AlbumArt(uri string) ([]byte, error)
	// ReadPicture retrieves embedded artwork from audio file tags
	ReadPicture(uri string) ([]byte, error)
}

// ArtworkDAO defines the interface for artwork persistence.
type ArtworkDAO interface {
	// GetArtwork retrieves cached artwork metadata by album ID
	GetArtwork(albumID string) (*CachedArtwork, error)
	// SaveArtwork saves artwork metadata to the cache
	SaveArtwork(art *CachedArtwork) error
}
