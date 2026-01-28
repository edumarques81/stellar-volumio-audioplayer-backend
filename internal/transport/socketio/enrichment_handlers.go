package socketio

import (
	"context"
	"database/sql"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/enrichment"
)

// EnrichmentHandlers manages artwork enrichment from web sources.
type EnrichmentHandlers struct {
	coordinator *enrichment.Coordinator
	jobStore    *enrichment.SQLiteJobStore
	worker      *enrichment.Worker
	server      *Server
	ctx         context.Context
	cancel      context.CancelFunc
}

// EnrichmentConfig holds configuration for the enrichment system.
type EnrichmentConfig struct {
	CacheDir string
	DB       *sql.DB
	CacheDAO *cache.DAO
}

// NewEnrichmentHandlers creates a new enrichment handlers instance.
// Returns nil if the database is not available.
func NewEnrichmentHandlers(cfg EnrichmentConfig, server *Server) *EnrichmentHandlers {
	if cfg.DB == nil {
		log.Warn().Msg("Enrichment handlers disabled: no database available")
		return nil
	}

	// Create job store
	jobStore, err := enrichment.NewSQLiteJobStore(cfg.DB)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create enrichment job store")
		return nil
	}

	// Create MusicBrainz client
	mbClient := enrichment.NewMusicBrainzClient()

	// Create Cover Art Archive client
	caaClient := enrichment.NewCAAClient()

	// Create album provider adapter
	var albumProvider enrichment.AlbumProvider
	if cfg.CacheDAO != nil {
		albumProvider = &cacheDAOAlbumProvider{dao: cfg.CacheDAO}
	}

	// Create coordinator
	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		cacheDir = os.ExpandEnv("$HOME/stellar-backend/data/cache")
	}
	coordinator := enrichment.NewCoordinator(mbClient, caaClient, jobStore, albumProvider, cacheDir)

	// Create worker with save function
	worker := enrichment.NewWorker(caaClient, jobStore,
		enrichment.WithSaveFunc(coordinator.CreateSaveFunc()),
		enrichment.WithBatchSize(5),
	)

	ctx, cancel := context.WithCancel(context.Background())

	return &EnrichmentHandlers{
		coordinator: coordinator,
		jobStore:    jobStore,
		worker:      worker,
		server:      server,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// RegisterHandlers registers enrichment-related Socket.IO handlers.
func (h *EnrichmentHandlers) RegisterHandlers(client *socket.Socket) {
	client.On("enrichment:status", func(args ...interface{}) {
		h.handleGetStatus(client)
	})

	client.On("enrichment:queue", func(args ...interface{}) {
		h.handleQueueMissing(client)
	})
}

// Initialize starts the enrichment worker. Call this after server creation.
func (h *EnrichmentHandlers) Initialize() {
	if h.worker == nil {
		return
	}
	go h.worker.Start(h.ctx)
	log.Info().Msg("Enrichment worker started")
}

// QueueMissingArtwork triggers the coordinator to queue missing artwork jobs.
// This should be called after cache builds complete.
func (h *EnrichmentHandlers) QueueMissingArtwork() {
	if h.coordinator == nil {
		return
	}
	go func() {
		if err := h.coordinator.QueueMissingArtwork(h.ctx); err != nil {
			log.Warn().Err(err).Msg("Failed to queue missing artwork for enrichment")
		}
	}()
}

// Close stops the enrichment worker.
func (h *EnrichmentHandlers) Close() {
	if h.cancel != nil {
		h.cancel()
	}
}

// EnrichmentStatusResponse represents the enrichment status response.
type EnrichmentStatusResponse struct {
	WorkerRunning bool `json:"workerRunning"`
	Pending       int  `json:"pending"`
	Running       int  `json:"running"`
	Completed     int  `json:"completed"`
	Failed        int  `json:"failed"`
	QueueRunning  bool `json:"queueRunning"`
}

func (h *EnrichmentHandlers) handleGetStatus(client *socket.Socket) {
	log.Debug().Msg("Received enrichment:status")
	client.Emit("pushEnrichmentStatus", h.getStatus())
}

func (h *EnrichmentHandlers) handleQueueMissing(client *socket.Socket) {
	log.Info().Msg("Received enrichment:queue - queuing missing artwork")

	if h.coordinator == nil {
		client.Emit("pushEnrichmentQueueResult", map[string]interface{}{
			"success": false,
			"error":   "enrichment coordinator not available",
		})
		return
	}

	go func() {
		if err := h.coordinator.QueueMissingArtwork(h.ctx); err != nil {
			log.Error().Err(err).Msg("Failed to queue missing artwork")
			return
		}
		if h.server != nil && h.server.io != nil {
			h.server.io.Emit("pushEnrichmentStatus", h.getStatus())
		}
	}()

	client.Emit("pushEnrichmentQueueResult", map[string]interface{}{
		"success": true,
		"message": "Enrichment queue processing started",
	})
}

func (h *EnrichmentHandlers) getStatus() EnrichmentStatusResponse {
	resp := EnrichmentStatusResponse{}
	if h.worker != nil {
		resp.WorkerRunning = h.worker.IsRunning()
	}
	if h.jobStore != nil {
		pending, running, completed, failed, _ := h.jobStore.GetStats()
		resp.Pending = pending
		resp.Running = running
		resp.Completed = completed
		resp.Failed = failed
	}
	if h.coordinator != nil {
		resp.QueueRunning = h.coordinator.IsRunning()
	}
	return resp
}

// cacheDAOAlbumProvider adapts cache.DAO to enrichment.AlbumProvider.
type cacheDAOAlbumProvider struct {
	dao *cache.DAO
}

func (p *cacheDAOAlbumProvider) GetAlbumsWithoutArtwork() ([]enrichment.Album, error) {
	albums, err := p.dao.GetAlbumsWithoutArtwork()
	if err != nil {
		return nil, err
	}
	result := make([]enrichment.Album, len(albums))
	for i, a := range albums {
		result[i] = enrichment.Album{
			ID:          a.ID,
			Title:       a.Title,
			AlbumArtist: a.AlbumArtist,
			FirstTrack:  a.FirstTrack,
			HasArtwork:  a.HasArtwork,
		}
	}
	return result, nil
}

func (p *cacheDAOAlbumProvider) UpdateAlbumArtwork(albumID, artworkID string) error {
	return p.dao.UpdateAlbumArtwork(albumID, artworkID)
}
