package socketio

import (
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library"
	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"
)

// CacheHandlers contains Socket.IO handlers for cache operations.
type CacheHandlers struct {
	cachedService *library.CachedService
	server        *Server
}

// NewCacheHandlers creates a new CacheHandlers instance.
func NewCacheHandlers(cachedService *library.CachedService, server *Server) *CacheHandlers {
	return &CacheHandlers{
		cachedService: cachedService,
		server:        server,
	}
}

// RegisterHandlers registers all cache-related Socket.IO handlers.
func (h *CacheHandlers) RegisterHandlers(client *socket.Socket) {
	// Cache status
	client.On("library:cache:status", func(args ...interface{}) {
		h.handleGetCacheStatus(client)
	})

	// Cache rebuild
	client.On("library:cache:rebuild", func(args ...interface{}) {
		h.handleRebuildCache(client)
	})
}

// CacheStatusResponse represents the cache status response.
type CacheStatusResponse struct {
	LastUpdated    string `json:"lastUpdated"`
	AlbumCount     int    `json:"albumCount"`
	ArtistCount    int    `json:"artistCount"`
	TrackCount     int    `json:"trackCount"`
	ArtworkCached  int    `json:"artworkCached"`
	ArtworkMissing int    `json:"artworkMissing"`
	RadioCount     int    `json:"radioCount"`
	IsBuilding     bool   `json:"isBuilding"`
	BuildProgress  int    `json:"buildProgress"`
	SchemaVersion  string `json:"schemaVersion"`
}

// handleGetCacheStatus handles the library:cache:status event.
func (h *CacheHandlers) handleGetCacheStatus(client *socket.Socket) {
	log.Debug().Msg("Received library:cache:status")

	stats, err := h.cachedService.GetCacheStatus()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get cache status")
		client.Emit("pushLibraryCacheStatus", CacheStatusResponse{})
		return
	}

	resp := CacheStatusResponse{
		AlbumCount:     stats.AlbumCount,
		ArtistCount:    stats.ArtistCount,
		TrackCount:     stats.TrackCount,
		ArtworkCached:  stats.ArtworkCount,
		ArtworkMissing: stats.ArtworkMissing,
		RadioCount:     stats.RadioCount,
		IsBuilding:     stats.IsBuilding,
		BuildProgress:  stats.BuildProgress,
		SchemaVersion:  stats.SchemaVersion,
	}

	if !stats.LastUpdated.IsZero() {
		resp.LastUpdated = stats.LastUpdated.Format("2006-01-02T15:04:05Z07:00")
	}

	log.Debug().
		Int("albums", resp.AlbumCount).
		Int("artists", resp.ArtistCount).
		Bool("building", resp.IsBuilding).
		Msg("Sending pushLibraryCacheStatus")

	client.Emit("pushLibraryCacheStatus", resp)
}

// CacheUpdatedEvent represents the cache updated event payload.
type CacheUpdatedEvent struct {
	Timestamp      string `json:"timestamp"`
	AlbumCount     int    `json:"albumCount"`
	ArtistCount    int    `json:"artistCount"`
	TrackCount     int    `json:"trackCount"`
	UpdateDuration int    `json:"updateDuration"` // milliseconds
}

// handleRebuildCache handles the library:cache:rebuild event.
func (h *CacheHandlers) handleRebuildCache(client *socket.Socket) {
	log.Info().Msg("Received library:cache:rebuild - starting rebuild")

	// Start rebuild in background
	go func() {
		err := h.cachedService.RebuildCache()
		if err != nil {
			log.Error().Err(err).Msg("Cache rebuild failed")
			return
		}

		// Get updated stats
		stats, err := h.cachedService.GetCacheStatus()
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get cache status after rebuild")
			return
		}

		// Broadcast cache updated event to all clients
		event := CacheUpdatedEvent{
			Timestamp:  stats.LastUpdated.Format("2006-01-02T15:04:05Z07:00"),
			AlbumCount: stats.AlbumCount,
			ArtistCount: stats.ArtistCount,
			TrackCount: stats.TrackCount,
		}

		if h.server != nil && h.server.io != nil {
			h.server.io.Emit("library:cache:updated", event)
			log.Info().
				Int("albums", event.AlbumCount).
				Int("artists", event.ArtistCount).
				Msg("Cache rebuild complete, broadcasted update")
		}
	}()

	// Immediately respond with current status
	h.handleGetCacheStatus(client)
}
