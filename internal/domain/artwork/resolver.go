// Package artwork provides artwork resolution and caching for albums and artists.
package artwork

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

// Resolver handles artwork resolution with multi-source fallback.
// Resolution order:
// 1. Check cache (artwork table + file on disk)
// 2. MPD albumart (folder-based: cover.jpg, folder.jpg, etc.)
// 3. MPD readpicture (embedded in audio file tags)
// 4. Web fetch (MusicBrainz/Cover Art Archive) - handled by enrichment subsystem
// 5. Placeholder
type Resolver struct {
	mpd      MPDArtworkProvider
	dao      ArtworkDAO
	cacheDir string
}

// NewResolver creates a new artwork resolver.
func NewResolver(mpd MPDArtworkProvider, dao ArtworkDAO, cacheDir string) *Resolver {
	return &Resolver{
		mpd:      mpd,
		dao:      dao,
		cacheDir: cacheDir,
	}
}

// Resolve attempts to find or fetch artwork for an album.
// Returns a ResolveResult with the artwork path and source.
// If no artwork is found, returns a placeholder result (never returns error for missing art).
func (r *Resolver) Resolve(albumID, trackURI string) (*ResolveResult, error) {
	// 1. Check cache first
	if result := r.checkCache(albumID); result != nil {
		return result, nil
	}

	// 2. Try MPD albumart (folder-based)
	if result, err := r.tryMPDAlbumArt(albumID, trackURI); err == nil && result != nil {
		return result, nil
	}

	// 3. Try MPD readpicture (embedded)
	if result, err := r.tryMPDReadPicture(albumID, trackURI); err == nil && result != nil {
		return result, nil
	}

	// 4. Return placeholder (web enrichment handled separately)
	return r.placeholder(), nil
}

// checkCache checks if artwork exists in cache.
func (r *Resolver) checkCache(albumID string) *ResolveResult {
	if r.dao == nil {
		return nil
	}

	cached, err := r.dao.GetArtwork(albumID)
	if err != nil || cached == nil {
		return nil
	}

	// Verify file still exists on disk
	if cached.FilePath == "" {
		return nil
	}

	info, err := os.Stat(cached.FilePath)
	if err != nil {
		log.Debug().Str("albumID", albumID).Str("path", cached.FilePath).Msg("Cached artwork file missing")
		return nil
	}

	return &ResolveResult{
		FilePath: cached.FilePath,
		Source:   "cache",
		MimeType: cached.MimeType,
		Width:    cached.Width,
		Height:   cached.Height,
		FileSize: int(info.Size()),
	}
}

// tryMPDAlbumArt attempts to fetch folder-based album art from MPD.
func (r *Resolver) tryMPDAlbumArt(albumID, trackURI string) (*ResolveResult, error) {
	if r.mpd == nil {
		return nil, ErrNoArtwork
	}

	data, err := r.mpd.AlbumArt(trackURI)
	if err != nil || len(data) == 0 {
		return nil, ErrNoArtwork
	}

	return r.saveToCache(albumID, data, "mpd")
}

// tryMPDReadPicture attempts to fetch embedded artwork from audio file.
func (r *Resolver) tryMPDReadPicture(albumID, trackURI string) (*ResolveResult, error) {
	if r.mpd == nil {
		return nil, ErrNoArtwork
	}

	data, err := r.mpd.ReadPicture(trackURI)
	if err != nil || len(data) == 0 {
		return nil, ErrNoArtwork
	}

	return r.saveToCache(albumID, data, "embedded")
}

// saveToCache saves artwork data to disk and database.
func (r *Resolver) saveToCache(albumID string, data []byte, source string) (*ResolveResult, error) {
	mimeType := DetectMimeType(data)
	ext := GetExtensionForMime(mimeType)

	// Create cache directory if needed
	albumsDir := filepath.Join(r.cacheDir, "albums")
	if err := os.MkdirAll(albumsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Write file to cache
	filename := albumID + ext
	filePath := filepath.Join(albumsDir, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write artwork file: %w", err)
	}

	log.Debug().
		Str("albumID", albumID).
		Str("source", source).
		Str("path", filePath).
		Int("size", len(data)).
		Msg("Cached artwork")

	// Save metadata to database
	if r.dao != nil {
		checksum := fmt.Sprintf("%x", md5.Sum(data))
		artwork := &CachedArtwork{
			ID:        generateArtworkID(albumID, "album"),
			AlbumID:   albumID,
			Type:      "album",
			FilePath:  filePath,
			Source:    source,
			MimeType:  mimeType,
			FileSize:  len(data),
			Checksum:  checksum,
			FetchedAt: time.Now(),
			CreatedAt: time.Now(),
		}
		if err := r.dao.SaveArtwork(artwork); err != nil {
			log.Warn().Err(err).Str("albumID", albumID).Msg("Failed to save artwork metadata")
		}
	}

	return &ResolveResult{
		FilePath: filePath,
		Source:   source,
		MimeType: mimeType,
		FileSize: len(data),
	}, nil
}

// placeholder returns a placeholder result when no artwork is found.
func (r *Resolver) placeholder() *ResolveResult {
	return &ResolveResult{
		Source: "placeholder",
	}
}

// generateArtworkID creates a unique ID for artwork.
func generateArtworkID(albumID, artType string) string {
	data := albumID + "\x00" + artType
	return fmt.Sprintf("%x", md5.Sum([]byte(data)))
}

// DetectMimeType detects the MIME type from image data magic bytes.
func DetectMimeType(data []byte) string {
	if len(data) < 4 {
		return "application/octet-stream"
	}

	// JPEG: starts with FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}

	// PNG: starts with 89 50 4E 47 0D 0A 1A 0A
	if len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' &&
		data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A {
		return "image/png"
	}

	// GIF: starts with GIF87a or GIF89a
	if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' && data[3] == '8' {
		return "image/gif"
	}

	// WebP: starts with RIFF....WEBP
	if len(data) >= 12 &&
		data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P' {
		return "image/webp"
	}

	return "application/octet-stream"
}

// GetExtensionForMime returns the file extension for a MIME type.
func GetExtensionForMime(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}
