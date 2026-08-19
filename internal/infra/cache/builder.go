// Package cache provides a SQLite-based caching layer for library metadata.
package cache

import (
	"crypto/md5"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/artistidentity"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/discgroup"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/dupebadge"
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
	// LastModified is the newest file mtime in the album -- MPD 0.23's only
	// "when did this arrive" signal (`added` is 0.24+). Zero when unknown.
	LastModified time.Time
	Format       string // Audio format from MPD, e.g. "44100:16:2"
	Genre        string // Album-level genre (first track's Genre tag, normalized)
	Disc         string // MPD Disc tag from a representative track, first-track-wins; "" when absent
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
//
// Grouping (discgroup.GroupFolders, BROWSE-07) runs per basePath -- a box
// set's member disc-folders always share one common root directory under
// one source by construction. Badging (dupebadge.Compute, BROWSE-01/02/03)
// runs ONCE over the FULL cross-basePath set, after every basePath's groups
// have been collected, mirroring Service.GetAlbums's badging-scope decision
// (see 03-03-SUMMARY.md key-decisions): duplicates can span basePaths (e.g.
// "The Light For Days": LOCAL vs USB), so per-basePath badging would miss
// them.
func (b *Builder) buildAlbums() error {
	tx, err := b.db.BeginTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Collected across ALL basePaths before any insert, so badging can see
	// the full merged set. discs is aligned by index with cachedAlbums --
	// each entry is that album's representative discgroup.Group.Disc value
	// (only meaningful when DiscCount<=1), fed to dupebadge's disc tier.
	var cachedAlbums []*CachedAlbum
	var discs []string

	for _, basePath := range b.basePaths {
		albums, err := b.provider.GetAlbumDetails(basePath)
		if err != nil {
			log.Warn().Err(err).Str("basePath", basePath).Msg("Failed to get albums for base path")
			continue
		}

		folders := make([]discgroup.Folder, 0, len(albums))
		for _, album := range albums {
			if album.Album == "" {
				continue
			}
			folders = append(folders, discgroup.Folder{
				Album:        album.Album,
				AlbumArtist:  album.AlbumArtist,
				Directory:    filepath.Dir(album.FirstTrack),
				Disc:         album.Disc,
				FirstTrack:   album.FirstTrack,
				TrackCount:   album.TrackCount,
				TotalTime:    album.TotalTime,
				Format:       album.Format,
				Genre:        album.Genre,
				Year:         album.Year,
				LastModified: album.LastModified,
			})
		}

		groups := discgroup.GroupFolders(folders)

		for _, g := range groups {
			// Get source type from the representative first track path
			source := b.classifier.GetSourceType(g.FirstTrack)

			// URI is the group's RootDir -- for a merged box set this is the
			// common parent directory (NOT filepath.Dir(g.FirstTrack), which
			// would point at disc 1's own subfolder), so MPD's recursive
			// `search base <uri>` returns every disc's tracks with no new
			// query logic. On an ungrouped group, RootDir equals the
			// original folder's own Directory (unchanged existing behavior).
			uri := g.RootDir

			// Generate album ID including URI so different quality versions are separate
			albumID := generateAlbumID(g.AlbumArtist, g.Album, uri)

			// Parse audio format (e.g. "44100:16:2" → sampleRate, bitDepth)
			var sampleRate, bitDepth int
			if g.Format != "" {
				parts := strings.Split(g.Format, ":")
				if len(parts) >= 2 {
					sampleRate, _ = strconv.Atoi(parts[0])
					bitDepth, _ = strconv.Atoi(parts[1])
				}
			}

			// Detect track type from the representative first track's file extension
			trackType := ""
			if g.FirstTrack != "" {
				if idx := strings.LastIndex(g.FirstTrack, "."); idx >= 0 {
					trackType = strings.ToLower(g.FirstTrack[idx+1:])
				}
			}

			// DiscCount is only set when the group is genuinely a multi-disc
			// box set (>1); an ungrouped discgroup.Group's own convention is
			// DiscCount=1, which must map to CachedAlbum.DiscCount=0 so the
			// "0/unset = ordinary single-disc album" contract Service.GetAlbums'
			// albumFromGroup (service.go) already established holds through
			// the cache path too -- this plan's objective is byte-for-byte
			// parity between the two paths.
			discCount := 0
			if g.DiscCount > 1 {
				discCount = g.DiscCount
			}

			cachedAlbum := &CachedAlbum{
				ID:            albumID,
				Title:         g.Album,
				AlbumArtist:   g.AlbumArtist,
				URI:           uri,
				FirstTrack:    g.FirstTrack,
				TrackCount:    g.TrackCount,
				TotalDuration: g.TotalTime,
				Source:        source,
				SampleRate:    sampleRate,
				BitDepth:      bitDepth,
				TrackType:     trackType,
				Genre:         g.Genre,
				DiscCount:     discCount,
				Year:          g.Year,
				// The newest file mtime in the album, not time.Now(): a
				// FullBuild does Clear() then repopulate, which defeats the
				// DAO's `added_at = COALESCE(albums.added_at, ?)` preservation
				// and would otherwise stamp every album with the same rebuild
				// timestamp, making sort=recently_added arbitrary. Caveat:
				// stellar-ingest copies with `cp -r --preserve=timestamps`, so
				// an old rip landing today keeps its old mtime -- still
				// strictly better than every album sharing one timestamp.
				// Falls back to now when MPD reported no usable Last-Modified.
				// Normalised to UTC because albums.added_at is a TEXT column
				// and `ORDER BY added_at DESC` is therefore a LEXICAL compare:
				// a local-zone fallback ("... 20:06:22+10:00") would sort above
				// an earlier-but-UTC mtime ("... 12:00:00+00:00"). Uniform
				// offsets make the string order the chronological order.
				AddedAt: firstNonZeroTime(g.LastModified, time.Now()).UTC(),
			}

			cachedAlbums = append(cachedAlbums, cachedAlbum)
			discs = append(discs, g.Disc)
		}
	}

	// Compute BROWSE-01/02/03 duplicate-disambiguation badges across the
	// FULL cross-basePath set, before any insert -- see this function's doc
	// comment.
	applyDupeBadges(cachedAlbums, discs)

	albumCount := 0
	for _, cachedAlbum := range cachedAlbums {
		if err := b.dao.InsertAlbumTx(tx, cachedAlbum); err != nil {
			log.Warn().Err(err).Str("album", cachedAlbum.Title).Msg("Failed to insert album")
			continue
		}

		albumCount++
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
//
// Raw MPD Artist tag values are collapsed via artistidentity.Collapse
// (ARTIST-01/ARTIST-02) before insertion: multiple raw credit-string
// variants that name the same real performer (e.g. 15 distinct "Luciano
// Pavarotti, <ensemble>" values) collapse to one canonical name, and their
// album counts are summed rather than the last-processed variant winning
// (Go map iteration order is randomized, so a naive overwrite would be
// silently non-deterministic). The empty raw value MPD's real `list artist`
// output contains collapses to "" and is skipped, producing no row
// (ARTIST-03).
func (b *Builder) buildArtists() error {
	artistCounts, err := b.provider.GetArtistsWithAlbumCounts()
	if err != nil {
		return fmt.Errorf("failed to get artist counts: %w", err)
	}

	collapsed := make(map[string]int, len(artistCounts))
	for artistName, albumCount := range artistCounts {
		canonical := artistidentity.Collapse(artistName)
		if canonical == "" {
			continue
		}
		collapsed[canonical] += albumCount
	}

	tx, err := b.db.BeginTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for artistName, albumCount := range collapsed {
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

	log.Debug().Int("count", len(collapsed)).Msg("Artists cached")
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

// applyDupeBadges computes BROWSE-01/02/03 duplicate-disambiguation badges
// (internal/infra/dupebadge) across the full album list and writes results
// back into each CachedAlbum.Badge by index. discs must be the same length
// as albums, aligned by index (each album's representative
// discgroup.Group.Disc value). Mirrors
// internal/domain/library.applyDupeBadges (service.go) -- see
// 03-03-SUMMARY.md key-decisions for why badging must run over the FULL
// merged set, not per basePath.
func applyDupeBadges(albums []*CachedAlbum, discs []string) {
	if len(albums) == 0 {
		return
	}

	candidates := make([]dupebadge.Candidate, len(albums))
	for i, a := range albums {
		disc := ""
		if i < len(discs) {
			disc = discs[i]
		}
		candidates[i] = dupebadge.Candidate{
			Title:   a.Title,
			Artist:  a.AlbumArtist,
			Quality: formatQualityLabel(a.SampleRate, a.BitDepth, a.TrackType),
			Disc:    disc,
			Source:  a.Source,
		}
	}

	badges := dupebadge.Compute(candidates)
	for i := range albums {
		albums[i].Badge = badges[i]
	}
}

// formatQualityLabel mirrors internal/domain/library.formatQualityLabel
// (cached_service.go) byte-for-byte. It must be duplicated here rather than
// imported: internal/infra packages must not import internal/domain
// packages (see discgroup's and dupebadge's package docs for the two
// verified facts justifying this layering rule), and
// dupebadge.Candidate.Quality expects the same already-formatted label
// string the MPD-direct path (service.go) computes.
func formatQualityLabel(sampleRate, bitDepth int, trackType string) string {
	if sampleRate == 0 && bitDepth == 0 && trackType == "" {
		return ""
	}

	tt := strings.ToUpper(trackType)

	if trackType == "dsf" || trackType == "dff" || trackType == "dsd" {
		switch {
		case sampleRate >= 11289600 || sampleRate == 176400:
			return "DSD256"
		case sampleRate >= 5644800 || sampleRate == 88200:
			return "DSD128"
		case sampleRate >= 2822400:
			return "DSD64"
		default:
			return "DSD"
		}
	}

	var parts []string
	if sampleRate > 0 {
		if sampleRate%1000 == 0 {
			parts = append(parts, fmt.Sprintf("%dkHz", sampleRate/1000))
		} else {
			parts = append(parts, fmt.Sprintf("%.1fkHz", float64(sampleRate)/1000))
		}
	}
	if bitDepth > 0 {
		parts = append(parts, fmt.Sprintf("%dbit", bitDepth))
	}

	label := strings.Join(parts, "/")
	if tt != "" && label != "" {
		label += " " + tt
	} else if tt != "" {
		label = tt
	}
	return label
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

// firstNonZeroTime returns ts when it is set, otherwise fallback. Used for
// AddedAt so an album MPD reported no Last-Modified for still gets a
// timestamp rather than sorting as the epoch.
func firstNonZeroTime(ts, fallback time.Time) time.Time {
	if ts.IsZero() {
		return fallback
	}
	return ts
}
