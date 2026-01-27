package library

import (
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
	"github.com/rs/zerolog/log"
)

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
		albums = append(albums, Album{
			ID:         ca.ID,
			Title:      ca.Title,
			Artist:     ca.AlbumArtist, // Use AlbumArtist from cache
			URI:        ca.URI,
			TrackCount: ca.TrackCount,
			Source:     SourceType(ca.Source),
			Year:       ca.Year,
			AlbumArt:   "", // Will be set by artwork resolver
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
		artists = append(artists, Artist{
			Name:       ca.Name,
			AlbumCount: ca.AlbumCount,
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
