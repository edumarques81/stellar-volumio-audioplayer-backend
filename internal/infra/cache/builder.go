// Package cache provides a SQLite-based caching layer for library metadata.
package cache

import (
	"crypto/md5"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// MPDDataProvider defines the interface for fetching data from MPD.
type MPDDataProvider interface {
	// GetAlbumDetails returns album details for a base path
	GetAlbumDetails(basePath string) ([]AlbumDetailsData, error)
	// GetArtistsWithAlbumCounts returns artists with their album counts
	GetArtistsWithAlbumCounts() (map[string]int, error)
	// FindAlbumTracks returns tracks for an album
	FindAlbumTracks(album, albumArtist string) ([]TrackData, error)
	// ListPlaylists returns all playlists
	ListPlaylists() ([]string, error)
	// ListPlaylistInfo returns playlist contents
	ListPlaylistInfo(name string) ([]TrackData, error)
}

// AlbumDetailsData represents album data from MPD.
type AlbumDetailsData struct {
	Album       string
	AlbumArtist string
	TrackCount  int
	FirstTrack  string
	TotalTime   int
	Year        int
}

// TrackData represents track data from MPD.
type TrackData struct {
	File        string
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Track       string
	Disc        string
	Duration    string
	Time        string
	Date        string
}

// PathClassifier classifies file paths into source types.
type PathClassifier interface {
	GetSourceType(uri string) string
}

// DefaultPathClassifier provides default path classification logic.
type DefaultPathClassifier struct {
	NASPaths   []string // Paths that indicate NAS storage
	USBPaths   []string // Paths that indicate USB storage
	LocalPaths []string // Paths that indicate local storage
}

// NewDefaultPathClassifier creates a path classifier with default patterns.
func NewDefaultPathClassifier() *DefaultPathClassifier {
	return &DefaultPathClassifier{
		NASPaths:   []string{"NAS", "nas", "network", "smb", "nfs", "cifs"},
		USBPaths:   []string{"USB", "usb", "media", "mnt"},
		LocalPaths: []string{"INTERNAL", "internal", "local", "data"},
	}
}

// GetSourceType determines the source type from a file path.
func (c *DefaultPathClassifier) GetSourceType(uri string) string {
	uriLower := strings.ToLower(uri)

	// Check NAS paths first (more specific)
	for _, pattern := range c.NASPaths {
		if strings.Contains(uriLower, strings.ToLower(pattern)) {
			return "nas"
		}
	}

	// Check USB paths
	for _, pattern := range c.USBPaths {
		if strings.Contains(uriLower, strings.ToLower(pattern)) {
			return "usb"
		}
	}

	// Default to local
	return "local"
}

// Builder handles building and updating the cache from MPD.
type Builder struct {
	db         *DB
	dao        *DAO
	provider   MPDDataProvider
	classifier PathClassifier
	basePaths  []string // Base paths to scan (e.g., ["INTERNAL", "USB", "NAS"])
}

// NewBuilder creates a new cache builder.
func NewBuilder(db *DB, provider MPDDataProvider, classifier PathClassifier) *Builder {
	if classifier == nil {
		classifier = NewDefaultPathClassifier()
	}
	return &Builder{
		db:         db,
		dao:        NewDAO(db),
		provider:   provider,
		classifier: classifier,
		basePaths:  []string{"INTERNAL", "USB", "NAS"}, // Default base paths
	}
}

// SetBasePaths sets the base paths to scan.
func (b *Builder) SetBasePaths(paths []string) {
	b.basePaths = paths
}

// FullBuild performs a complete cache rebuild from MPD.
func (b *Builder) FullBuild() error {
	startTime := time.Now()
	log.Info().Msg("Starting full cache build from MPD")

	b.db.SetBuildingState(true, 0)
	defer b.db.SetBuildingState(false, 100)

	// Clear existing cache
	if err := b.db.Clear(); err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	// Build albums
	b.db.SetBuildingState(true, 10)
	if err := b.buildAlbums(); err != nil {
		return fmt.Errorf("failed to build albums: %w", err)
	}

	// Build artists
	b.db.SetBuildingState(true, 50)
	if err := b.buildArtists(); err != nil {
		return fmt.Errorf("failed to build artists: %w", err)
	}

	// Build radio stations
	b.db.SetBuildingState(true, 80)
	if err := b.buildRadioStations(); err != nil {
		log.Warn().Err(err).Msg("Failed to build radio stations (non-fatal)")
	}

	// Mark build complete
	b.db.SetBuildingState(true, 95)
	if err := b.db.MarkBuildComplete(); err != nil {
		return fmt.Errorf("failed to mark build complete: %w", err)
	}

	duration := time.Since(startTime)

	// Log stats
	stats, _ := b.db.GetStats()
	log.Info().
		Int("albums", stats.AlbumCount).
		Int("artists", stats.ArtistCount).
		Int("tracks", stats.TrackCount).
		Dur("duration", duration).
		Msg("Cache build complete")

	return nil
}

// buildAlbums builds the albums cache from MPD.
func (b *Builder) buildAlbums() error {
	tx, err := b.db.BeginTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	albumCount := 0

	for _, basePath := range b.basePaths {
		albums, err := b.provider.GetAlbumDetails(basePath)
		if err != nil {
			log.Warn().Err(err).Str("basePath", basePath).Msg("Failed to get albums for base path")
			continue
		}

		for _, album := range albums {
			if album.Album == "" {
				continue
			}

			// Generate album ID
			albumID := generateAlbumID(album.AlbumArtist, album.Album)

			// Get source type from first track path
			source := b.classifier.GetSourceType(album.FirstTrack)

			// Get directory URI for playback
			uri := filepath.Dir(album.FirstTrack)

			cachedAlbum := &CachedAlbum{
				ID:            albumID,
				Title:         album.Album,
				AlbumArtist:   album.AlbumArtist,
				URI:           uri,
				TrackCount:    album.TrackCount,
				TotalDuration: album.TotalTime,
				Source:        source,
				Year:          album.Year,
				AddedAt:       time.Now(), // Would be better to get from file mtime
			}

			if err := b.dao.InsertAlbumTx(tx, cachedAlbum); err != nil {
				log.Warn().Err(err).Str("album", album.Album).Msg("Failed to insert album")
				continue
			}

			albumCount++
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit albums: %w", err)
	}

	log.Debug().Int("count", albumCount).Msg("Albums cached")
	return nil
}

// buildArtists builds the artists cache from MPD.
func (b *Builder) buildArtists() error {
	artistCounts, err := b.provider.GetArtistsWithAlbumCounts()
	if err != nil {
		return fmt.Errorf("failed to get artist counts: %w", err)
	}

	tx, err := b.db.BeginTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for artistName, albumCount := range artistCounts {
		if artistName == "" {
			continue
		}

		artistID := generateArtistID(artistName)

		artist := &CachedArtist{
			ID:         artistID,
			Name:       artistName,
			AlbumCount: albumCount,
		}

		if err := b.dao.InsertArtistTx(tx, artist); err != nil {
			log.Warn().Err(err).Str("artist", artistName).Msg("Failed to insert artist")
			continue
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit artists: %w", err)
	}

	log.Debug().Int("count", len(artistCounts)).Msg("Artists cached")
	return nil
}

// buildRadioStations builds the radio stations cache from MPD playlists.
func (b *Builder) buildRadioStations() error {
	playlists, err := b.provider.ListPlaylists()
	if err != nil {
		return fmt.Errorf("failed to list playlists: %w", err)
	}

	tx, err := b.db.BeginTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	radioCount := 0

	for _, playlist := range playlists {
		// Only process playlists that look like radio stations
		if !strings.HasPrefix(playlist, "Radio/") && !strings.HasPrefix(strings.ToLower(playlist), "radio") {
			continue
		}

		info, err := b.provider.ListPlaylistInfo(playlist)
		if err != nil {
			log.Warn().Err(err).Str("playlist", playlist).Msg("Failed to get playlist info")
			continue
		}

		if len(info) == 0 {
			continue
		}

		// Use first track as the stream URL
		uri := info[0].File
		name := strings.TrimPrefix(playlist, "Radio/")
		if name == "" {
			name = playlist
		}

		stationID := generateRadioID(name, uri)

		station := &CachedRadioStation{
			ID:   stationID,
			Name: name,
			URI:  uri,
		}

		if err := b.dao.InsertRadioStation(station); err != nil {
			log.Warn().Err(err).Str("station", name).Msg("Failed to insert radio station")
			continue
		}

		radioCount++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit radio stations: %w", err)
	}

	log.Debug().Int("count", radioCount).Msg("Radio stations cached")
	return nil
}

// BuildAlbumTracks builds the track cache for a specific album.
// This is called on-demand when tracks are requested.
func (b *Builder) BuildAlbumTracks(albumID, album, albumArtist string) error {
	tracks, err := b.provider.FindAlbumTracks(album, albumArtist)
	if err != nil {
		return fmt.Errorf("failed to get album tracks: %w", err)
	}

	tx, err := b.db.BeginTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, track := range tracks {
		trackID := generateTrackID(track.File)
		source := b.classifier.GetSourceType(track.File)

		trackNumber, _ := strconv.Atoi(track.Track)
		discNumber := 1
		if track.Disc != "" {
			discNumber, _ = strconv.Atoi(track.Disc)
		}

		duration := 0
		if track.Duration != "" {
			if d, err := strconv.ParseFloat(track.Duration, 64); err == nil {
				duration = int(d)
			}
		} else if track.Time != "" {
			duration, _ = strconv.Atoi(track.Time)
		}

		cachedTrack := &CachedTrack{
			ID:          trackID,
			AlbumID:     albumID,
			Title:       track.Title,
			Artist:      track.Artist,
			URI:         track.File,
			TrackNumber: trackNumber,
			DiscNumber:  discNumber,
			Duration:    duration,
			Source:      source,
		}

		if err := b.dao.InsertTrackTx(tx, cachedTrack); err != nil {
			log.Warn().Err(err).Str("track", track.Title).Msg("Failed to insert track")
			continue
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit tracks: %w", err)
	}

	return nil
}

// Helper functions for generating IDs

func generateAlbumID(albumArtist, album string) string {
	data := albumArtist + "\x00" + album
	return fmt.Sprintf("%x", md5.Sum([]byte(data)))
}

func generateArtistID(name string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(name)))
}

func generateTrackID(uri string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(uri)))
}

func generateRadioID(name, uri string) string {
	data := name + "\x00" + uri
	return fmt.Sprintf("%x", md5.Sum([]byte(data)))
}
