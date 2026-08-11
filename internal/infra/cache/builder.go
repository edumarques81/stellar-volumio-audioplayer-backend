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
	// CountAlbums returns the total number of unique albums known to MPD.
	// Used as a pre-count guard in FullBuild to avoid wiping the cache when
	// MPD has not yet scanned its music directory (e.g. boot-before-MPD-ready).
	CountAlbums() (int, error)
	// CountUntagged returns the number of real (non-resource-fork) songs in
	// basePath with no Album tag -- DATA-02's skipped/untagged signal.
	CountUntagged(basePath string) (int, error)
}

// AlbumDetailsData represents album data from MPD.
type AlbumDetailsData struct {
	Album       string
	AlbumArtist string
	TrackCount  int
	FirstTrack  string
	TotalTime   int
	Year        int
	Format      string // Audio format from MPD, e.g. "44100:16:2"
	Genre       string // Album-level genre (first track's Genre tag, normalized)
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
// Before clearing the existing cache it probes MPD with CountAlbums. If MPD
// returns 0 albums — or returns an error (e.g. MPD not yet ready after reboot)
// — the rebuild is skipped entirely and the existing cache is preserved. This
// prevents the boot-before-MPD-ready race from wiping a valid library cache.
func (b *Builder) FullBuild() error {
	startTime := time.Now()
	log.Info().Msg("Starting full cache build from MPD")

	// Pre-count guard: probe MPD before touching the cache.
	count, err := b.provider.CountAlbums()
	if err != nil {
		log.Warn().Err(err).Msg("MPD CountAlbums failed; skipping rebuild to preserve existing cache")
		return nil
	}
	if count == 0 {
		log.Warn().Msg("MPD reports 0 albums; skipping rebuild to preserve existing cache")
		return nil
	}

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

	// Persist the skipped/untagged count (DATA-02). Non-fatal: matches the
	// buildRadioStations pattern below -- a failure here must not fail the
	// whole rebuild.
	if err := b.buildSkippedCount(); err != nil {
		log.Warn().Err(err).Msg("Failed to build skipped count (non-fatal)")
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

			// Get source type from first track path
			source := b.classifier.GetSourceType(album.FirstTrack)

			// Get directory URI for playback
			uri := filepath.Dir(album.FirstTrack)

			// Generate album ID including URI so different quality versions are separate
			albumID := generateAlbumID(album.AlbumArtist, album.Album, uri)

			// Parse audio format (e.g. "44100:16:2" → sampleRate, bitDepth)
			var sampleRate, bitDepth int
			if album.Format != "" {
				parts := strings.Split(album.Format, ":")
				if len(parts) >= 2 {
					sampleRate, _ = strconv.Atoi(parts[0])
					bitDepth, _ = strconv.Atoi(parts[1])
				}
			}

			// Detect track type from first track's file extension
			trackType := ""
			if album.FirstTrack != "" {
				if idx := strings.LastIndex(album.FirstTrack, "."); idx >= 0 {
					trackType = strings.ToLower(album.FirstTrack[idx+1:])
				}
			}

			cachedAlbum := &CachedAlbum{
				ID:            albumID,
				Title:         album.Album,
				AlbumArtist:   album.AlbumArtist,
				URI:           uri,
				FirstTrack:    album.FirstTrack,
				TrackCount:    album.TrackCount,
				TotalDuration: album.TotalTime,
				Source:        source,
				Year:          album.Year,
				SampleRate:    sampleRate,
				BitDepth:      bitDepth,
				TrackType:     trackType,
				Genre:         album.Genre,
				AddedAt:       time.Now(), // Would be better to get from file mtime
			}

			if err := b.dao.InsertAlbumTx(tx, cachedAlbum); err != nil {
				log.Warn().Err(err).Str("album", album.Album).Msg("Failed to insert album")
				continue
			}

			albumCount++
		}
	}

	// Re-link albums to their existing artwork rows. Clear() preserves the
	// artwork table by design (enrichment is slow + rate-limited), but the
	// albums row is wiped + re-inserted from MPD with no artwork_id. Artwork
	// row IDs follow the deterministic `<album_id>_artwork` convention (set
	// by the album save path in internal/infra/enrichment/coordinator.go), so
	// a single batched UPDATE restores the FK without re-fetching.
	//
	// Mirrors the buildArtists relinker (lines ~292 below); without it, every
	// cache rebuild forced enrichment to re-fetch all album art from
	// MusicBrainz/CAA at 1 req/sec — slow + wasteful (regression 2026-05-27).
	now := time.Now().Format(time.RFC3339)
	if relinkResult, err := tx.Exec(`
		UPDATE albums
		SET artwork_id = albums.id || '_artwork',
		    updated_at = ?
		WHERE (artwork_id IS NULL OR artwork_id = '')
		  AND EXISTS (
		      SELECT 1 FROM artwork
		      WHERE artwork.id   = albums.id || '_artwork'
		        AND artwork.type = 'album'
		  )
	`, now); err != nil {
		log.Warn().Err(err).Msg("Failed to relink album artwork; continuing")
	} else if relinked, _ := relinkResult.RowsAffected(); relinked > 0 {
		log.Info().Int64("count", relinked).Msg("Relinked albums to preserved artwork rows")
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit albums: %w", err)
	}

	log.Debug().Int("count", albumCount).Msg("Albums cached")
	return nil
}

// buildSkippedCount sums CountUntagged across every configured basePath and
// persists the total via cache_meta ("skipped_count"), read back by
// DB.GetStats() as CacheStats.SkippedCount -- DATA-02's skipped/untagged
// observability signal. Per-basePath errors are logged and skipped, matching
// buildAlbums' existing log.Warn().Err(err)...continue style, so one bad
// basePath does not block the total for the others.
func (b *Builder) buildSkippedCount() error {
	total := 0
	for _, basePath := range b.basePaths {
		count, err := b.provider.CountUntagged(basePath)
		if err != nil {
			log.Warn().Err(err).Str("basePath", basePath).Msg("Failed to count untagged songs for base path")
			continue
		}
		total += count
	}

	if err := b.db.setMeta("skipped_count", strconv.Itoa(total)); err != nil {
		return fmt.Errorf("failed to persist skipped_count: %w", err)
	}

	log.Debug().Int("skipped", total).Msg("Skipped/untagged count cached")
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

	// Re-link artists to their existing artwork rows. Clear() preserves the
	// artwork table by design, but the artists row is wiped + re-inserted
	// from MPD which has no knowledge of pre-existing enrichment. Artwork
	// row IDs follow a deterministic pattern (`<artist_id>_artwork`), so a
	// single batched UPDATE restores the FK without re-downloading anything.
	now := time.Now().Format(time.RFC3339)
	if relinkResult, err := tx.Exec(`
		UPDATE artists
		SET artwork_id = artists.id || '_artwork',
		    updated_at = ?
		WHERE (artwork_id IS NULL OR artwork_id = '')
		  AND EXISTS (
		      SELECT 1 FROM artwork
		      WHERE artwork.id   = artists.id || '_artwork'
		        AND artwork.type = 'artist'
		  )
	`, now); err != nil {
		log.Warn().Err(err).Msg("Failed to relink artist artwork; continuing")
	} else if relinked, _ := relinkResult.RowsAffected(); relinked > 0 {
		log.Info().Int64("count", relinked).Msg("Relinked artists to preserved artwork rows")
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit artists: %w", err)
	}

	log.Debug().Int("count", len(artistCounts)).Msg("Artists cached")
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

func generateAlbumID(albumArtist, album, uri string) string {
	data := albumArtist + "\x00" + album + "\x00" + uri
	return fmt.Sprintf("%x", md5.Sum([]byte(data)))
}

func generateArtistID(name string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(name)))
}

func generateTrackID(uri string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(uri)))
}
