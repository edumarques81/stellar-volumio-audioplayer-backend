package localmusic

import (
	"crypto/md5"
	"encoding/hex"
	"path"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"
)

// MPDClient interface for MPD operations needed by this service.
type MPDClient interface {
	ListInfo(uri string) ([]map[string]string, error)
	ListAllInfo(uri string) ([]map[string]string, error)
}

// Service provides local music operations.
type Service struct {
	mpd          MPDClient
	classifier   *PathClassifier
	history      *HistoryStore
	mpdMusicDir  string
}

// NewService creates a new local music service.
func NewService(mpd MPDClient, dataDir string, mpdMusicDir string) *Service {
	classifier := NewPathClassifier(mpdMusicDir)
	history := NewHistoryStore(dataDir, classifier)

	return &Service{
		mpd:         mpd,
		classifier:  classifier,
		history:     history,
		mpdMusicDir: mpdMusicDir,
	}
}

// GetClassifier returns the path classifier for external use.
func (s *Service) GetClassifier() *PathClassifier {
	return s.classifier
}

// GetLocalAlbums returns albums from local sources only (local disk + USB).
func (s *Service) GetLocalAlbums(req GetLocalAlbumsRequest) LocalAlbumsResponse {
	var albums []Album
	filteredOut := 0

	// Get albums from INTERNAL (local disk)
	internalAlbums, internalFiltered := s.getAlbumsFromPath("INTERNAL", SourceLocal, req.Query)
	albums = append(albums, internalAlbums...)
	filteredOut += internalFiltered

	// Get albums from USB
	usbAlbums, usbFiltered := s.getAlbumsFromPath("USB", SourceUSB, req.Query)
	albums = append(albums, usbAlbums...)
	filteredOut += usbFiltered

	// Sort albums
	s.sortAlbums(albums, req.Sort)

	// Apply limit
	if req.Limit > 0 && len(albums) > req.Limit {
		albums = albums[:req.Limit]
	}

	log.Info().
		Int("albumCount", len(albums)).
		Int("filteredOut", filteredOut).
		Str("sort", string(req.Sort)).
		Msg("GetLocalAlbums completed")

	return LocalAlbumsResponse{
		Albums:      albums,
		TotalCount:  len(albums),
		FilteredOut: filteredOut,
	}
}

// getAlbumsFromPath retrieves albums from a specific path prefix.
func (s *Service) getAlbumsFromPath(basePath string, sourceType SourceType, query string) ([]Album, int) {
	var albums []Album
	filteredOut := 0

	// List the base directory
	entries, err := s.mpd.ListInfo(basePath)
	if err != nil {
		log.Debug().Err(err).Str("path", basePath).Msg("Failed to list directory (may not exist)")
		return albums, 0
	}

	// Process each entry - looking for album folders
	for _, entry := range entries {
		dirPath, isDir := entry["directory"]
		if !isDir {
			continue
		}

		// Each subdirectory under INTERNAL or USB is a potential album or artist folder
		// We need to recursively find album folders
		albumsFromDir := s.findAlbumsInDirectory(dirPath, sourceType, query)
		albums = append(albums, albumsFromDir...)
	}

	return albums, filteredOut
}

// findAlbumsInDirectory recursively finds albums in a directory.
func (s *Service) findAlbumsInDirectory(dirPath string, sourceType SourceType, query string) []Album {
	var albums []Album

	entries, err := s.mpd.ListInfo(dirPath)
	if err != nil {
		log.Debug().Err(err).Str("path", dirPath).Msg("Failed to list directory")
		return albums
	}

	hasAudioFiles := false
	var subDirs []string
	var firstTrack map[string]string

	for _, entry := range entries {
		if file, ok := entry["file"]; ok {
			if isAudioFile(file) {
				hasAudioFiles = true
				if firstTrack == nil {
					firstTrack = entry
				}
			}
		} else if dir, ok := entry["directory"]; ok {
			subDirs = append(subDirs, dir)
		}
	}

	// If this directory has audio files, it's an album
	if hasAudioFiles {
		album := s.createAlbumFromDirectory(dirPath, firstTrack, sourceType, entries)

		// Apply query filter if provided
		if query != "" {
			queryLower := strings.ToLower(query)
			if !strings.Contains(strings.ToLower(album.Title), queryLower) &&
				!strings.Contains(strings.ToLower(album.Artist), queryLower) {
				return albums // Skip this album
			}
		}

		albums = append(albums, album)
	}

	// Recursively check subdirectories
	for _, subDir := range subDirs {
		subAlbums := s.findAlbumsInDirectory(subDir, sourceType, query)
		albums = append(albums, subAlbums...)
	}

	return albums
}

// createAlbumFromDirectory creates an Album from a directory with audio files.
func (s *Service) createAlbumFromDirectory(dirPath string, firstTrack map[string]string, sourceType SourceType, entries []map[string]string) Album {
	// Extract album info from first track metadata or directory name
	albumTitle := firstTrack["Album"]
	if albumTitle == "" {
		albumTitle = path.Base(dirPath)
	}

	artist := firstTrack["Artist"]
	if artist == "" {
		artist = firstTrack["AlbumArtist"]
	}
	if artist == "" {
		// Try to extract from parent directory
		parent := path.Dir(dirPath)
		artist = path.Base(parent)
	}

	// Count tracks
	trackCount := 0
	for _, entry := range entries {
		if file, ok := entry["file"]; ok && isAudioFile(file) {
			trackCount++
		}
	}

	// Generate album ID from URI
	albumID := generateID(dirPath)

	return Album{
		ID:         albumID,
		Title:      albumTitle,
		Artist:     artist,
		URI:        dirPath,
		AlbumArt:   "/albumart?path=" + dirPath,
		TrackCount: trackCount,
		Source:     sourceType,
	}
}

// sortAlbums sorts albums by the specified order.
func (s *Service) sortAlbums(albums []Album, sortOrder AlbumSortOrder) {
	switch sortOrder {
	case AlbumSortRecentlyAdded:
		// Sort by AddedAt descending (most recent first)
		// Note: If AddedAt is not populated, fall back to alphabetical
		sort.Slice(albums, func(i, j int) bool {
			if albums[i].AddedAt.IsZero() && albums[j].AddedAt.IsZero() {
				return albums[i].Title < albums[j].Title
			}
			return albums[i].AddedAt.After(albums[j].AddedAt)
		})
	case AlbumSortAlphabetical:
		sort.Slice(albums, func(i, j int) bool {
			return strings.ToLower(albums[i].Title) < strings.ToLower(albums[j].Title)
		})
	case AlbumSortByArtist:
		sort.Slice(albums, func(i, j int) bool {
			if albums[i].Artist == albums[j].Artist {
				return strings.ToLower(albums[i].Title) < strings.ToLower(albums[j].Title)
			}
			return strings.ToLower(albums[i].Artist) < strings.ToLower(albums[j].Artist)
		})
	default:
		// Default to alphabetical
		sort.Slice(albums, func(i, j int) bool {
			return strings.ToLower(albums[i].Title) < strings.ToLower(albums[j].Title)
		})
	}
}

// GetLastPlayedTracks returns the last played tracks from local sources.
func (s *Service) GetLastPlayedTracks(req GetLastPlayedRequest) LastPlayedResponse {
	// Get last played from history, filtered to local-only and manual plays only
	return s.history.GetLastPlayed(req, true, true)
}

// RecordTrackPlay records a track play event.
func (s *Service) RecordTrackPlay(trackURI, title, artist, album, albumArt string, origin PlayOrigin) {
	s.history.RecordPlay(trackURI, title, artist, album, albumArt, origin)
}

// GetSourceType returns the source type for a URI.
func (s *Service) GetSourceType(uri string) SourceType {
	return s.classifier.GetSourceType(uri)
}

// IsLocalSource returns true if the URI is from a local source.
func (s *Service) IsLocalSource(uri string) bool {
	return s.classifier.IsLocalPath(uri)
}

// GetHistoryStats returns playback history statistics.
func (s *Service) GetHistoryStats() map[string]interface{} {
	return s.history.Stats()
}

// ClearHistory clears the playback history.
func (s *Service) ClearHistory() {
	s.history.ClearHistory()
}

// RefreshMountCache refreshes the mount point cache.
func (s *Service) RefreshMountCache() {
	s.classifier.RefreshMountCache()
}

// generateID generates a unique ID from a string.
func generateID(input string) string {
	hash := md5.Sum([]byte(input))
	return hex.EncodeToString(hash[:])
}

// isAudioFile checks if a path is an audio file.
func isAudioFile(filePath string) bool {
	ext := strings.ToLower(path.Ext(filePath))
	audioExtensions := map[string]bool{
		".flac": true, ".mp3": true, ".wav": true, ".aiff": true,
		".aif": true, ".ogg": true, ".m4a": true, ".aac": true,
		".wma": true, ".dsf": true, ".dff": true, ".dsd": true,
		".ape": true, ".wv": true, ".mpc": true, ".opus": true,
		".alac": true,
	}
	return audioExtensions[ext]
}
