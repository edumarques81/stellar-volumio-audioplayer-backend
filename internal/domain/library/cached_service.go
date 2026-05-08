package library

import (
	"fmt"
	"strings"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
	"github.com/rs/zerolog/log"
)

// formatQualityLabel creates a human-readable quality label from audio parameters.
func formatQualityLabel(sampleRate, bitDepth int, trackType string) string {
	if sampleRate == 0 && bitDepth == 0 && trackType == "" {
		return ""
	}

	tt := strings.ToUpper(trackType)

	// DSD formats — MPD reports PCM-equivalent rate for DSF/DFF (e.g. 44100:24:2),
	// not the native DSD rate. We can infer DSD multiplier from the PCM rate:
	// DSD64 = 2.8MHz → PCM equiv 44100, DSD128 = 5.6MHz → 88200, DSD256 = 11.2MHz → 176400
	// Also check for native DSD rates and the "f" bit depth indicator (352800:f:2).
	if trackType == "dsf" || trackType == "dff" || trackType == "dsd" {
		// MPD 0.23 doesn't report Format for DSD files, so sampleRate may be 0.
		// When available, infer DSD multiplier from native or PCM-equivalent rate.
		switch {
		case sampleRate >= 11289600 || sampleRate == 176400:
			return "DSD256"
		case sampleRate >= 5644800 || sampleRate == 88200:
			return "DSD128"
		case sampleRate >= 2822400:
			return "DSD64"
		default:
			// No rate info from MPD — just label as DSD
			return "DSD"
		}
	}

	// PCM formats
	var parts []string
	if sampleRate > 0 {
		if sampleRate%1000 == 0 {
			parts = append(parts, fmt.Sprintf("%dkHz", sampleRate/1000))
		} else {
			// e.g. 44100 → "44.1kHz"
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

// CachedService wraps the library Service with cache support.
type CachedService struct {
	*Service
	cacheDB      *cache.DB
	cacheDAO     *cache.DAO
	cacheBuilder *cache.Builder
	cacheEnabled bool
}

// NewCachedService creates a new cached library service.
func NewCachedService(mpd MPDClient, classifier PathClassifier, cacheDB *cache.DB) *CachedService {
	baseService := NewService(mpd, classifier)

	if cacheDB == nil {
		return &CachedService{
			Service:      baseService,
			cacheEnabled: false,
		}
	}

	// Create cache DAO and builder
	dao := cache.NewDAO(cacheDB)
	builder := cache.NewBuilder(cacheDB, &mpdDataProviderAdapter{mpd: mpd}, nil)

	return &CachedService{
		Service:      baseService,
		cacheDB:      cacheDB,
		cacheDAO:     dao,
		cacheBuilder: builder,
		cacheEnabled: true,
	}
}

// GetAlbums returns albums, checking cache first.
func (s *CachedService) GetAlbums(req GetAlbumsRequest) AlbumsResponse {
	if !s.cacheEnabled || s.cacheDB == nil {
		return s.Service.GetAlbums(req)
	}

	// Check cache stats - if empty, fall back to MPD
	stats, err := s.cacheDB.GetStats()
	if err != nil || stats.AlbumCount == 0 {
		log.Debug().Msg("Cache empty, falling back to MPD")
		return s.Service.GetAlbums(req)
	}

	// Build filter from request
	filter := cache.AlbumFilter{
		Query: req.Query,
	}

	switch req.Scope {
	case ScopeNAS:
		filter.Scope = "nas"
	case ScopeUSB:
		filter.Scope = "usb"
	case ScopeLocal:
		filter.Scope = "local"
	default:
		filter.Scope = "all"
	}

	// Map sort order
	var sortOrder cache.SortOrder
	switch req.Sort {
	case SortByArtist:
		sortOrder = cache.SortByArtist
	case SortRecentlyAdded:
		sortOrder = cache.SortRecentlyAdded
	case SortYear:
		sortOrder = cache.SortYear
	default:
		sortOrder = cache.SortAlphabetical
	}

	// Query from cache
	pag := cache.NewPagination(req.Page, req.Limit)
	cachedAlbums, total, err := s.cacheDAO.QueryAlbums(filter, sortOrder, pag)
	if err != nil {
		log.Warn().Err(err).Msg("Cache query failed, falling back to MPD")
		return s.Service.GetAlbums(req)
	}

	// Convert cached albums to response format
	albums := make([]Album, 0, len(cachedAlbums))
	for _, ca := range cachedAlbums {
		// Generate album art URL from first track path (same as base Service)
		albumArt := ""
		if ca.FirstTrack != "" {
			albumArt = "/albumart?path=" + ca.FirstTrack
		}

		quality := formatQualityLabel(ca.SampleRate, ca.BitDepth, ca.TrackType)

		albums = append(albums, Album{
			ID:         ca.ID,
			Title:      ca.Title,
			Artist:     ca.AlbumArtist,
			URI:        ca.URI,
			TrackCount: ca.TrackCount,
			Source:     SourceType(ca.Source),
			Year:       ca.Year,
			AlbumArt:   albumArt,
			Quality:    quality,
			TrackType:  ca.TrackType,
			Genre:      ca.Genre,
		})
	}

	hasMore := pag.Offset+len(albums) < total

	log.Debug().
		Int("fromCache", len(albums)).
		Int("total", total).
		Msg("Albums served from cache")

	return AlbumsResponse{
		Albums: albums,
		Pagination: Pagination{
			Page:    req.Page,
			Limit:   req.Limit,
			Total:   total,
			HasMore: hasMore,
		},
	}
}

// GetArtists returns artists, checking cache first.
func (s *CachedService) GetArtists(req GetArtistsRequest) ArtistsResponse {
	if !s.cacheEnabled || s.cacheDB == nil {
		return s.Service.GetArtists(req)
	}

	// Check cache stats
	stats, err := s.cacheDB.GetStats()
	if err != nil || stats.ArtistCount == 0 {
		log.Debug().Msg("Cache empty, falling back to MPD")
		return s.Service.GetArtists(req)
	}

	// Query from cache
	pag := cache.NewPagination(req.Page, req.Limit)
	cachedArtists, total, err := s.cacheDAO.QueryArtists(req.Query, pag)
	if err != nil {
		log.Warn().Err(err).Msg("Cache query failed, falling back to MPD")
		return s.Service.GetArtists(req)
	}

	// Convert cached artists to response format
	artists := make([]Artist, 0, len(cachedArtists))
	for _, ca := range cachedArtists {
		// Generate artist art URL if artwork exists
		artistArt := ""
		if ca.ArtworkID != "" {
			// Check if it's a URL or file reference by looking up the artwork
			artwork, _ := s.cacheDAO.GetArtworkByArtist(ca.ID)
			if artwork != nil && artwork.FilePath != "" {
				if strings.HasPrefix(artwork.FilePath, "http") {
					// It's an external URL (Deezer hotlink)
					artistArt = artwork.FilePath
				} else {
					// Local file - use endpoint
					artistArt = "/artistart?id=" + ca.ID
				}
			}
		}

		artists = append(artists, Artist{
			Name:       ca.Name,
			AlbumCount: ca.AlbumCount,
			AlbumArt:   artistArt,
		})
	}

	hasMore := pag.Offset+len(artists) < total

	log.Debug().
		Int("fromCache", len(artists)).
		Int("total", total).
		Msg("Artists served from cache")

	return ArtistsResponse{
		Artists: artists,
		Pagination: Pagination{
			Page:    req.Page,
			Limit:   req.Limit,
			Total:   total,
			HasMore: hasMore,
		},
	}
}

// GetRadioStations returns radio stations, checking cache first.
func (s *CachedService) GetRadioStations(req GetRadioRequest) RadioResponse {
	if !s.cacheEnabled || s.cacheDB == nil {
		return s.Service.GetRadioStations(req)
	}

	// Check cache stats
	stats, err := s.cacheDB.GetStats()
	if err != nil || stats.RadioCount == 0 {
		log.Debug().Msg("Radio cache empty, falling back to MPD")
		return s.Service.GetRadioStations(req)
	}

	// Query from cache
	pag := cache.NewPagination(req.Page, req.Limit)
	cachedStations, total, err := s.cacheDAO.QueryRadioStations(req.Query, pag)
	if err != nil {
		log.Warn().Err(err).Msg("Radio cache query failed, falling back to MPD")
		return s.Service.GetRadioStations(req)
	}

	// Convert cached stations to response format
	stations := make([]RadioStation, 0, len(cachedStations))
	for _, cs := range cachedStations {
		stations = append(stations, RadioStation{
			ID:    cs.ID,
			Name:  cs.Name,
			URI:   cs.URI,
			Icon:  cs.Icon,
			Genre: cs.Genre,
		})
	}

	hasMore := pag.Offset+len(stations) < total

	log.Debug().
		Int("fromCache", len(stations)).
		Int("total", total).
		Msg("Radio stations served from cache")

	return RadioResponse{
		Stations: stations,
		Pagination: Pagination{
			Page:    req.Page,
			Limit:   req.Limit,
			Total:   total,
			HasMore: hasMore,
		},
	}
}

// RebuildCache triggers a full cache rebuild.
func (s *CachedService) RebuildCache() error {
	if !s.cacheEnabled || s.cacheBuilder == nil {
		return nil
	}

	log.Info().Msg("Starting cache rebuild")
	return s.cacheBuilder.FullBuild()
}

// GetCacheStatus returns cache statistics.
func (s *CachedService) GetCacheStatus() (*cache.CacheStats, error) {
	if !s.cacheEnabled || s.cacheDB == nil {
		return &cache.CacheStats{}, nil
	}
	return s.cacheDB.GetStats()
}

// IsCacheEnabled returns whether caching is enabled.
func (s *CachedService) IsCacheEnabled() bool {
	return s.cacheEnabled
}

// mpdDataProviderAdapter adapts the library MPDClient to cache.MPDDataProvider.
type mpdDataProviderAdapter struct {
	mpd MPDClient
}

func (a *mpdDataProviderAdapter) GetAlbumDetails(basePath string) ([]cache.AlbumDetailsData, error) {
	details, err := a.mpd.GetAlbumDetails(basePath)
	if err != nil {
		return nil, err
	}

	result := make([]cache.AlbumDetailsData, 0, len(details))
	for _, d := range details {
		result = append(result, cache.AlbumDetailsData{
			Album:       d.Album,
			AlbumArtist: d.AlbumArtist,
			TrackCount:  d.TrackCount,
			FirstTrack:  d.FirstTrack,
			TotalTime:   d.TotalTime,
			Format:      d.Format,
		})
	}
	return result, nil
}

func (a *mpdDataProviderAdapter) GetArtistsWithAlbumCounts() (map[string]int, error) {
	artists, err := a.mpd.ListArtists()
	if err != nil {
		return nil, err
	}

	result := make(map[string]int, len(artists))
	for _, artist := range artists {
		albums, err := a.mpd.FindAlbumsByArtist(artist)
		if err != nil {
			continue
		}
		result[artist] = len(albums)
	}
	return result, nil
}

func (a *mpdDataProviderAdapter) FindAlbumTracks(album, albumArtist string) ([]cache.TrackData, error) {
	tracks, err := a.mpd.FindAlbumTracks(album, albumArtist)
	if err != nil {
		return nil, err
	}

	result := make([]cache.TrackData, 0, len(tracks))
	for _, t := range tracks {
		result = append(result, cache.TrackData{
			File:        t["file"],
			Title:       t["Title"],
			Artist:      t["Artist"],
			Album:       t["Album"],
			AlbumArtist: t["AlbumArtist"],
			Track:       t["Track"],
			Disc:        t["Disc"],
			Duration:    t["duration"],
			Time:        t["Time"],
			Date:        t["Date"],
		})
	}
	return result, nil
}

func (a *mpdDataProviderAdapter) ListPlaylists() ([]string, error) {
	return a.mpd.ListPlaylists()
}

func (a *mpdDataProviderAdapter) ListPlaylistInfo(name string) ([]cache.TrackData, error) {
	tracks, err := a.mpd.ListPlaylistInfo(name)
	if err != nil {
		return nil, err
	}

	result := make([]cache.TrackData, 0, len(tracks))
	for _, t := range tracks {
		result = append(result, cache.TrackData{
			File:  t["file"],
			Title: t["Title"],
		})
	}
	return result, nil
}
