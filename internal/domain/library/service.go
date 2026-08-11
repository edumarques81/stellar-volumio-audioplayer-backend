package library

import (
	"crypto/md5"
	"encoding/hex"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/artistidentity"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/discgroup"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/dupebadge"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/musicfile"
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
	Format      string // Audio format from MPD, e.g. "44100:16:2"
	Genre       string // Album-level genre (first track's Genre tag, normalized)
	Disc        string // MPD Disc tag from a representative track, first-track-wins; "" when absent
}

// MPDClient interface for MPD operations needed by this service.
type MPDClient interface {
	// Album queries
	ListAlbums() ([]AlbumInfo, error)
	ListAlbumsInBase(basePath string) ([]AlbumInfo, error)
	GetAlbumDetails(basePath string) ([]AlbumDetails, error)
	// CountAlbums returns the total number of unique albums in MPD.
	// Used as a pre-count guard in cache builder to detect an empty/unready MPD.
	CountAlbums() (int, error)

	// Artist queries
	ListArtists() ([]string, error)
	FindAlbumsByArtist(artist string) ([]AlbumInfo, error)

	// Track queries
	FindAlbumTracks(album, albumArtist string) ([]map[string]string, error)
	SearchByBase(basePath string) ([]map[string]string, error)

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
	var discs []string

	// Determine which base paths to query based on scope
	basePaths := s.getBasePathsForScope(req.Scope)

	// Fetch albums from each base path. Grouping (discgroup) is inherently
	// basePath-scoped -- a box set's member folders always share one root
	// under a single source -- but duplicate-disambiguation badging
	// (dupebadge) is NOT: live duplicate groups like "The Light For Days"
	// (LOCAL vs USB) and "Djesse Vol. 4 (Deluxe)" span different basePaths.
	// Badges are therefore computed once, below, over the FULL merged list
	// across all queried basePaths -- not per basePath.
	for _, basePath := range basePaths {
		sourceType := s.sourceTypeForBasePath(basePath)
		albumsFromPath, discsFromPath := s.getAlbumsFromBasePath(basePath, sourceType, req.Query)
		albums = append(albums, albumsFromPath...)
		discs = append(discs, discsFromPath...)
	}

	applyDupeBadges(albums, discs)

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

// getAlbumsFromBasePath fetches albums from a specific base path, grouping
// multi-disc box sets (BROWSE-07) via discgroup before converting to Album
// records. Returns the built albums alongside each album's representative
// discgroup.Group.Disc value (aligned by index, not exposed on Album itself)
// so the caller can feed dupebadge.Compute across the FULL merged album list
// once all basePaths have been collected -- see GetAlbums's comment on why
// badging is not done per basePath.
func (s *Service) getAlbumsFromBasePath(basePath string, sourceType SourceType, query string) ([]Album, []string) {
	albumDetails, err := s.mpd.GetAlbumDetails(basePath)
	if err != nil {
		log.Debug().Err(err).Str("path", basePath).Msg("Failed to get albums from database")
		return nil, nil
	}

	groups := discgroup.GroupFolders(foldersFromAlbumDetails(albumDetails))

	queryLower := strings.ToLower(query)

	var albums []Album
	var discs []string
	for _, g := range groups {
		// Apply query filter if provided
		if query != "" {
			if !strings.Contains(strings.ToLower(g.Album), queryLower) &&
				!strings.Contains(strings.ToLower(g.AlbumArtist), queryLower) {
				continue
			}
		}

		albums = append(albums, albumFromGroup(g, sourceType, false))
		discs = append(discs, g.Disc)
	}

	return albums, discs
}

// foldersFromAlbumDetails converts raw per-folder AlbumDetails (one entry
// per (Album, AlbumArtist, Directory) tuple, per mpd.groupAlbumDetails) into
// discgroup.Folder values for discgroup.GroupFolders.
func foldersFromAlbumDetails(albumDetails []AlbumDetails) []discgroup.Folder {
	folders := make([]discgroup.Folder, 0, len(albumDetails))
	for _, details := range albumDetails {
		directory := ""
		if details.FirstTrack != "" {
			directory = path.Dir(details.FirstTrack)
		}
		folders = append(folders, discgroup.Folder{
			Album:       details.Album,
			AlbumArtist: details.AlbumArtist,
			Directory:   directory,
			Disc:        details.Disc,
			FirstTrack:  details.FirstTrack,
			TrackCount:  details.TrackCount,
			TotalTime:   details.TotalTime,
			Format:      details.Format,
			Genre:       details.Genre,
		})
	}
	return folders
}

// albumFromGroup builds an Album from a discgroup.Group. URI is
// group.RootDir -- NOT path.Dir(group.FirstTrack) -- so a grouped box set's
// URI is the common parent directory, making MPD's recursive
// `search base <uri>` (via GetAlbumTracks/SearchByBase) return every disc's
// tracks with no new query logic. DiscCount is only set when the group is
// genuinely a multi-disc box set (>1); otherwise it stays 0 so `omitempty`
// drops it, matching this package's existing "0/unset means ordinary
// single-disc album" convention. includeGenre matches GetArtistAlbums's
// pre-existing behavior of populating Genre, which GetAlbums's album shape
// has never set.
func albumFromGroup(g discgroup.Group, sourceType SourceType, includeGenre bool) Album {
	uri := g.RootDir

	albumID := generateID(g.Album + "\x00" + g.AlbumArtist + "\x00" + uri)

	albumArt := ""
	if g.FirstTrack != "" {
		albumArt = "/albumart?path=" + g.FirstTrack
	}

	var sampleRate, bitDepth int
	if g.Format != "" {
		parts := strings.Split(g.Format, ":")
		if len(parts) >= 2 {
			sampleRate, _ = strconv.Atoi(parts[0])
			bitDepth, _ = strconv.Atoi(parts[1])
		}
	}
	trackType := ""
	if g.FirstTrack != "" {
		if idx := strings.LastIndex(g.FirstTrack, "."); idx >= 0 {
			trackType = strings.ToLower(g.FirstTrack[idx+1:])
		}
	}
	quality := formatQualityLabel(sampleRate, bitDepth, trackType)

	discCount := 0
	if g.DiscCount > 1 {
		discCount = g.DiscCount
	}

	album := Album{
		ID:         albumID,
		Title:      g.Album,
		Artist:     g.AlbumArtist,
		URI:        uri,
		AlbumArt:   albumArt,
		TrackCount: g.TrackCount,
		Source:     sourceType,
		Quality:    quality,
		TrackType:  trackType,
		DiscCount:  discCount,
	}
	if includeGenre {
		album.Genre = g.Genre
	}
	return album
}

// applyDupeBadges computes BROWSE-01/02/03 duplicate-disambiguation badges
// (internal/infra/dupebadge) across the full album list and writes results
// back into each Album.Badge by index. discs must be the same length as
// albums, aligned by index (each album's representative discgroup.Group.Disc
// value, threaded through without being exposed on Album itself). A missing
// or short discs slice degrades to "" per-candidate, which only affects the
// disc precedence tier (quality and source tiers are unaffected).
func applyDupeBadges(albums []Album, discs []string) {
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
			Artist:  a.Artist,
			Quality: a.Quality,
			Disc:    disc,
			Source:  string(a.Source),
		}
	}

	badges := dupebadge.Compute(candidates)
	for i := range albums {
		albums[i].Badge = badges[i]
	}
}

// sortAlbums sorts albums by the specified order.
func (s *Service) sortAlbums(albums []Album, sortOrder SortOrder) {
	switch sortOrder {
	case SortByArtist:
		sort.Slice(albums, func(i, j int) bool {
			ai, aj := strings.ToLower(albums[i].Artist), strings.ToLower(albums[j].Artist)
			if ai != aj {
				return ai < aj
			}
			ti, tj := strings.ToLower(albums[i].Title), strings.ToLower(albums[j].Title)
			if ti != tj {
				return ti < tj
			}
			// Same artist+title: sort by quality descending (higher quality first)
			return albums[i].Quality > albums[j].Quality
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
//
// This is the MPD-direct fallback path (used when the SQLite cache is empty
// or a cache query fails; see internal/infra/cache.Builder.buildArtists for
// the cache-primary path). Raw MPD Artist tag values are collapsed via
// artistidentity.Collapse (ARTIST-01/ARTIST-02) before being surfaced:
// multiple raw variants that collapse to the same canonical performer name
// are merged into one Artist row with summed album counts, and the empty
// raw value MPD's real `list artist` output contains collapses to "" and is
// skipped, producing no row (ARTIST-03). D-06 resolves to collapsing on
// both paths so ARTIST-01/02/03 hold regardless of which one answers a
// given request.
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

	// First pass: collapse each raw name to its canonical form and merge
	// album counts across raw variants that collapse to the same canonical
	// name (sum on collision -- distinct raw variants represent distinct
	// albums' raw tags, not overlapping counts).
	merged := make(map[string]int, len(artistNames))
	for _, name := range artistNames {
		canonical := artistidentity.Collapse(name)
		if canonical == "" {
			continue
		}

		// FindAlbumsByArtist is queried with the RAW name, not the
		// canonical one -- unchanged pre-existing behavior; see this
		// plan's <interfaces> block for the documented, out-of-scope
		// AlbumArtist-vs-Artist tag quirk this does not fix.
		albumCount := 0
		if albumInfos, err := s.mpd.FindAlbumsByArtist(name); err == nil {
			albumCount = len(albumInfos)
		}

		merged[canonical] += albumCount
	}

	// Second pass: apply the query filter against the CANONICAL name and
	// build the response slice.
	for canonical, albumCount := range merged {
		if req.Query != "" && !strings.Contains(strings.ToLower(canonical), queryLower) {
			continue
		}

		artists = append(artists, Artist{
			Name:       canonical,
			AlbumCount: albumCount,
		})
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

// GetArtistAlbums returns albums by a specific artist with fully-populated
// URI / AlbumArt / quality fields. Mirrors GetAlbums's data-flow
// (iterate base paths via GetAlbumDetails) but filters by AlbumArtist so
// every Album record returned is the same shape downstream consumers get
// from `library:albums:list` — which means `playAlbum(album)` works and
// album covers render.
func (s *Service) GetArtistAlbums(req GetArtistAlbumsRequest) ArtistAlbumsResponse {
	albums := make([]Album, 0)
	var discs []string

	// Scope to all sources — artist filtering at this layer is by name only.
	basePaths := s.getBasePathsForScope(ScopeAll)

	for _, basePath := range basePaths {
		sourceType := s.sourceTypeForBasePath(basePath)
		albumDetails, err := s.mpd.GetAlbumDetails(basePath)
		if err != nil {
			log.Debug().Err(err).Str("path", basePath).Msg("Failed to get albums from database")
			continue
		}

		// Group the FULL per-basePath AlbumDetails first, then filter by
		// artist from the grouped output -- grouping never mixes different
		// AlbumArtist values by construction (discgroup clusters by
		// (Album, AlbumArtist)), so filtering after grouping is equivalent
		// to filtering before it, without needing to special-case it.
		groups := discgroup.GroupFolders(foldersFromAlbumDetails(albumDetails))

		for _, g := range groups {
			// Case-insensitive AlbumArtist match — MPD tag casing is unreliable.
			if !strings.EqualFold(g.AlbumArtist, req.Artist) {
				continue
			}

			albums = append(albums, albumFromGroup(g, sourceType, true))
			discs = append(discs, g.Disc)
		}
	}

	// Badging across the full merged list -- see GetAlbums's comment on why
	// this cannot be done per basePath (cross-source duplicates like "The
	// Light For Days" LOCAL vs USB).
	applyDupeBadges(albums, discs)

	s.sortAlbums(albums, req.Sort)

	// Pagination (same shape as before).
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

	return ArtistAlbumsResponse{
		Artist: req.Artist,
		Albums: albums[start:end],
		Pagination: Pagination{
			Page:    page,
			Limit:   limit,
			Total:   total,
			HasMore: end < total,
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

	var tracks []map[string]string
	var err error

	if req.URI != "" {
		// URI provided — search within specific folder (scopes to one quality version)
		tracks, err = s.mpd.SearchByBase(req.URI)
	} else {
		// Fallback: search by album name + artist (may return tracks from multiple folders)
		tracks, err = s.mpd.FindAlbumTracks(req.Album, req.AlbumArtist)
	}
	if err != nil {
		log.Debug().Err(err).Str("album", req.Album).Str("uri", req.URI).Msg("Failed to find album tracks")
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

		// Skip macOS resource fork files (._prefix)
		if musicfile.IsResourceFork(file) {
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
