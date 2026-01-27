package library

import (
	"crypto/md5"
	"encoding/hex"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// AlbumInfo matches the mpd.AlbumInfo type.
type AlbumInfo struct {
	Album       string
	AlbumArtist string
}

// AlbumDetails matches the mpd.AlbumDetails type.
type AlbumDetails struct {
	Album       string
	AlbumArtist string
	TrackCount  int
	FirstTrack  string
	TotalTime   int
}

// MPDClient interface for MPD operations needed by this service.
type MPDClient interface {
	// Album queries
	ListAlbums() ([]AlbumInfo, error)
	ListAlbumsInBase(basePath string) ([]AlbumInfo, error)
	GetAlbumDetails(basePath string) ([]AlbumDetails, error)

	// Artist queries
	ListArtists() ([]string, error)
	FindAlbumsByArtist(artist string) ([]AlbumInfo, error)

	// Track queries
	FindAlbumTracks(album, albumArtist string) ([]map[string]string, error)

	// Playlist/radio queries
	ListPlaylists() ([]string, error)
	ListPlaylistInfo(name string) ([]map[string]string, error)
}

// PathClassifier interface for source classification.
type PathClassifier interface {
	GetSourceType(uri string) SourceType
}

// Service provides library browsing operations.
type Service struct {
	mpd        MPDClient
	classifier PathClassifier
}

// NewService creates a new library service.
func NewService(mpd MPDClient, classifier PathClassifier) *Service {
	return &Service{
		mpd:        mpd,
		classifier: classifier,
	}
}

// GetAlbums returns albums based on the request parameters.
func (s *Service) GetAlbums(req GetAlbumsRequest) AlbumsResponse {
	albums := make([]Album, 0)

	// Determine which base paths to query based on scope
	basePaths := s.getBasePathsForScope(req.Scope)

	// Fetch albums from each base path
	for _, basePath := range basePaths {
		sourceType := s.sourceTypeForBasePath(basePath)
		albumsFromPath := s.getAlbumsFromBasePath(basePath, sourceType, req.Query)
		albums = append(albums, albumsFromPath...)
	}

	// Sort albums
	s.sortAlbums(albums, req.Sort)

	// Apply pagination
	total := len(albums)
	page := req.Page
	limit := req.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > MaxLimit {
		limit = DefaultLimit
	}

	start := (page - 1) * limit
	end := start + limit
	if start > len(albums) {
		start = len(albums)
	}
	if end > len(albums) {
		end = len(albums)
	}

	paginatedAlbums := albums[start:end]
	hasMore := end < total

	return AlbumsResponse{
		Albums: paginatedAlbums,
		Pagination: Pagination{
			Page:    page,
			Limit:   limit,
			Total:   total,
			HasMore: hasMore,
		},
	}
}

// getBasePathsForScope returns the MPD base paths to query for a given scope.
func (s *Service) getBasePathsForScope(scope Scope) []string {
	switch scope {
	case ScopeNAS:
		return []string{"NAS"}
	case ScopeUSB:
		return []string{"USB"}
	case ScopeLocal:
		return []string{"INTERNAL", "USB"}
	case ScopeAll:
		fallthrough
	default:
		return []string{"INTERNAL", "USB", "NAS"}
	}
}

// sourceTypeForBasePath returns the SourceType for a base path.
func (s *Service) sourceTypeForBasePath(basePath string) SourceType {
	switch basePath {
	case "NAS":
		return SourceNAS
	case "USB":
		return SourceUSB
	case "INTERNAL":
		fallthrough
	default:
		return SourceLocal
	}
}

// getAlbumsFromBasePath fetches albums from a specific base path.
func (s *Service) getAlbumsFromBasePath(basePath string, sourceType SourceType, query string) []Album {
	var albums []Album

	albumDetails, err := s.mpd.GetAlbumDetails(basePath)
	if err != nil {
		log.Debug().Err(err).Str("path", basePath).Msg("Failed to get albums from database")
		return albums
	}

	queryLower := strings.ToLower(query)

	for _, details := range albumDetails {
		// Apply query filter if provided
		if query != "" {
			if !strings.Contains(strings.ToLower(details.Album), queryLower) &&
				!strings.Contains(strings.ToLower(details.AlbumArtist), queryLower) {
				continue
			}
		}

		// Generate album ID
		albumID := generateID(details.Album + "\x00" + details.AlbumArtist)

		// Get album art from first track
		albumArt := ""
		if details.FirstTrack != "" {
			albumArt = "/albumart?path=" + details.FirstTrack
		}

		// Get URI from first track directory
		uri := ""
		if details.FirstTrack != "" {
			uri = path.Dir(details.FirstTrack)
		}

		album := Album{
			ID:         albumID,
			Title:      details.Album,
			Artist:     details.AlbumArtist,
			URI:        uri,
			AlbumArt:   albumArt,
			TrackCount: details.TrackCount,
			Source:     sourceType,
		}

		albums = append(albums, album)
	}

	return albums
}

// sortAlbums sorts albums by the specified order.
func (s *Service) sortAlbums(albums []Album, sortOrder SortOrder) {
	switch sortOrder {
	case SortByArtist:
		sort.Slice(albums, func(i, j int) bool {
			if albums[i].Artist == albums[j].Artist {
				return strings.ToLower(albums[i].Title) < strings.ToLower(albums[j].Title)
			}
			return strings.ToLower(albums[i].Artist) < strings.ToLower(albums[j].Artist)
		})
	case SortYear:
		sort.Slice(albums, func(i, j int) bool {
			if albums[i].Year == albums[j].Year {
				return strings.ToLower(albums[i].Title) < strings.ToLower(albums[j].Title)
			}
			return albums[i].Year > albums[j].Year // Descending
		})
	case SortRecentlyAdded:
		sort.Slice(albums, func(i, j int) bool {
			if albums[i].AddedAt.IsZero() && albums[j].AddedAt.IsZero() {
				return strings.ToLower(albums[i].Title) < strings.ToLower(albums[j].Title)
			}
			return albums[i].AddedAt.After(albums[j].AddedAt)
		})
	case SortAlphabetical:
		fallthrough
	default:
		sort.Slice(albums, func(i, j int) bool {
			return strings.ToLower(albums[i].Title) < strings.ToLower(albums[j].Title)
		})
	}
}

// GetArtists returns all artists from the MPD database.
func (s *Service) GetArtists(req GetArtistsRequest) ArtistsResponse {
	var artists []Artist

	artistNames, err := s.mpd.ListArtists()
	if err != nil {
		log.Debug().Err(err).Msg("Failed to list artists")
		return ArtistsResponse{
			Artists:    []Artist{},
			Pagination: Pagination{Page: 1, Limit: DefaultLimit},
		}
	}

	queryLower := strings.ToLower(req.Query)

	for _, name := range artistNames {
		// Skip empty artist names
		if name == "" {
			continue
		}

		// Apply query filter if provided
		if req.Query != "" && !strings.Contains(strings.ToLower(name), queryLower) {
			continue
		}

		// Get album count for this artist
		albumCount := 0
		if albumInfos, err := s.mpd.FindAlbumsByArtist(name); err == nil {
			albumCount = len(albumInfos)
		}

		artist := Artist{
			Name:       name,
			AlbumCount: albumCount,
		}

		artists = append(artists, artist)
	}

	// Sort artists alphabetically
	sort.Slice(artists, func(i, j int) bool {
		return strings.ToLower(artists[i].Name) < strings.ToLower(artists[j].Name)
	})

	// Apply pagination
	total := len(artists)
	page := req.Page
	limit := req.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > MaxLimit {
		limit = DefaultLimit
	}

	start := (page - 1) * limit
	end := start + limit
	if start > len(artists) {
		start = len(artists)
	}
	if end > len(artists) {
		end = len(artists)
	}

	paginatedArtists := artists[start:end]
	hasMore := end < total

	return ArtistsResponse{
		Artists: paginatedArtists,
		Pagination: Pagination{
			Page:    page,
			Limit:   limit,
			Total:   total,
			HasMore: hasMore,
		},
	}
}

// GetArtistAlbums returns albums by a specific artist.
func (s *Service) GetArtistAlbums(req GetArtistAlbumsRequest) ArtistAlbumsResponse {
	var albums []Album

	albumInfos, err := s.mpd.FindAlbumsByArtist(req.Artist)
	if err != nil {
		log.Debug().Err(err).Str("artist", req.Artist).Msg("Failed to find albums by artist")
		return ArtistAlbumsResponse{
			Artist:     req.Artist,
			Albums:     []Album{},
			Pagination: Pagination{Page: 1, Limit: DefaultLimit},
		}
	}

	// For each album, get full details
	// Note: This is a simplified implementation. In production, we would
	// use a more efficient query or cache.
	for _, info := range albumInfos {
		albumID := generateID(info.Album + "\x00" + info.AlbumArtist)

		album := Album{
			ID:     albumID,
			Title:  info.Album,
			Artist: info.AlbumArtist,
			Source: SourceLocal, // Default, would need track info to determine
		}

		albums = append(albums, album)
	}

	// Sort albums
	s.sortAlbums(albums, req.Sort)

	// Apply pagination
	total := len(albums)
	page := req.Page
	limit := req.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > MaxLimit {
		limit = DefaultLimit
	}

	start := (page - 1) * limit
	end := start + limit
	if start > len(albums) {
		start = len(albums)
	}
	if end > len(albums) {
		end = len(albums)
	}

	paginatedAlbums := albums[start:end]
	hasMore := end < total

	return ArtistAlbumsResponse{
		Artist: req.Artist,
		Albums: paginatedAlbums,
		Pagination: Pagination{
			Page:    page,
			Limit:   limit,
			Total:   total,
			HasMore: hasMore,
		},
	}
}

// GetAlbumTracks returns tracks for a specific album.
func (s *Service) GetAlbumTracks(req GetAlbumTracksRequest) AlbumTracksResponse {
	if req.Album == "" {
		return AlbumTracksResponse{
			Error: "album name is required",
		}
	}

	tracks, err := s.mpd.FindAlbumTracks(req.Album, req.AlbumArtist)
	if err != nil {
		log.Debug().Err(err).Str("album", req.Album).Msg("Failed to find album tracks")
		return AlbumTracksResponse{
			Album:       req.Album,
			AlbumArtist: req.AlbumArtist,
			Error:       "failed to get album tracks: " + err.Error(),
		}
	}

	var resultTracks []Track
	totalDuration := 0

	for _, track := range tracks {
		file := track["file"]
		if file == "" {
			continue
		}

		// Parse track number
		trackNum := 0
		if tn := track["Track"]; tn != "" {
			// Track can be "1" or "1/12"
			if idx := strings.Index(tn, "/"); idx > 0 {
				tn = tn[:idx]
			}
			if n, err := strconv.Atoi(tn); err == nil {
				trackNum = n
			}
		}

		// Parse duration
		duration := 0
		if d := track["Time"]; d != "" {
			if n, err := strconv.Atoi(d); err == nil {
				duration = n
			}
		} else if d := track["duration"]; d != "" {
			if f, err := strconv.ParseFloat(d, 64); err == nil {
				duration = int(f)
			}
		}

		totalDuration += duration

		// Get title, fallback to filename
		title := track["Title"]
		if title == "" {
			title = path.Base(file)
			if ext := path.Ext(title); ext != "" {
				title = title[:len(title)-len(ext)]
			}
		}

		// Determine source type from file path
		sourceType := s.classifier.GetSourceType(file)

		resultTrack := Track{
			ID:          generateID(file),
			Title:       title,
			Artist:      track["Artist"],
			Album:       track["Album"],
			URI:         file,
			TrackNumber: trackNum,
			Duration:    duration,
			AlbumArt:    "/albumart?path=" + file,
			Source:      sourceType,
		}

		resultTracks = append(resultTracks, resultTrack)
	}

	// Sort by track number
	sort.Slice(resultTracks, func(i, j int) bool {
		if resultTracks[i].TrackNumber != resultTracks[j].TrackNumber {
			return resultTracks[i].TrackNumber < resultTracks[j].TrackNumber
		}
		return resultTracks[i].Title < resultTracks[j].Title
	})

	return AlbumTracksResponse{
		Album:         req.Album,
		AlbumArtist:   req.AlbumArtist,
		Tracks:        resultTracks,
		TotalDuration: totalDuration,
	}
}

// GetRadioStations returns radio stations from MPD playlists.
// Radio stations are expected to be stored in playlists with "Radio/" prefix.
func (s *Service) GetRadioStations(req GetRadioRequest) RadioResponse {
	var stations []RadioStation

	playlists, err := s.mpd.ListPlaylists()
	if err != nil {
		log.Debug().Err(err).Msg("Failed to list playlists")
		return RadioResponse{
			Stations:   []RadioStation{},
			Pagination: Pagination{Page: 1, Limit: DefaultLimit},
		}
	}

	queryLower := strings.ToLower(req.Query)

	for _, playlist := range playlists {
		// Only include playlists with "Radio/" prefix
		if !strings.HasPrefix(playlist, "Radio/") {
			continue
		}

		// Extract station name from playlist path
		stationName := strings.TrimPrefix(playlist, "Radio/")

		// Apply query filter if provided
		if req.Query != "" && !strings.Contains(strings.ToLower(stationName), queryLower) {
			continue
		}

		// Get stream URL from playlist
		streamURL := ""
		if playlistInfo, err := s.mpd.ListPlaylistInfo(playlist); err == nil && len(playlistInfo) > 0 {
			streamURL = playlistInfo[0]["file"]
		}

		station := RadioStation{
			ID:   generateID(playlist),
			Name: stationName,
			URI:  streamURL,
		}

		stations = append(stations, station)
	}

	// Sort stations alphabetically
	sort.Slice(stations, func(i, j int) bool {
		return strings.ToLower(stations[i].Name) < strings.ToLower(stations[j].Name)
	})

	// Apply pagination
	total := len(stations)
	page := req.Page
	limit := req.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > MaxLimit {
		limit = DefaultLimit
	}

	start := (page - 1) * limit
	end := start + limit
	if start > len(stations) {
		start = len(stations)
	}
	if end > len(stations) {
		end = len(stations)
	}

	paginatedStations := stations[start:end]
	hasMore := end < total

	return RadioResponse{
		Stations: paginatedStations,
		Pagination: Pagination{
			Page:    page,
			Limit:   limit,
			Total:   total,
			HasMore: hasMore,
		},
	}
}

// generateID creates a unique ID from a string.
func generateID(input string) string {
	hash := md5.Sum([]byte(input))
	return hex.EncodeToString(hash[:])
}
