package enrichment

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Worker processes enrichment jobs in the background
type Worker struct {
	provider       ArtworkProvider
	fanartClient   *FanartClient
	deezerClient   *DeezerClient
	artistProvider ArtistProvider
	store          JobStore
	config         WorkerConfig
	mu             sync.Mutex
	running        bool
	stopCh         chan struct{}
}

// WorkerOption is a functional option for configuring the worker
type WorkerOption func(*Worker)

// WithBatchSize sets the number of jobs to process per batch
func WithBatchSize(size int) WorkerOption {
	return func(w *Worker) {
		w.config.BatchSize = size
	}
}

// WithWorkerInterval sets the interval between batch processing
func WithWorkerInterval(interval time.Duration) WorkerOption {
	return func(w *Worker) {
		w.config.Interval = interval
	}
}

// WithMaxRetries sets the maximum number of retries for failed jobs
func WithMaxRetries(max int) WorkerOption {
	return func(w *Worker) {
		w.config.MaxRetries = max
	}
}

// WithSaveFunc sets the callback function for saving fetched artwork
func WithSaveFunc(fn SaveFunc) WorkerOption {
	return func(w *Worker) {
		w.config.SaveFunc = fn
	}
}

// WithSaveFuncArtist sets the callback function for saving fetched artist artwork
func WithSaveFuncArtist(fn SaveFuncArtist) WorkerOption {
	return func(w *Worker) {
		w.config.SaveFuncArtist = fn
	}
}

// WithFanartClient sets the Fanart.tv client for artist images
func WithWorkerFanartClient(fc *FanartClient) WorkerOption {
	return func(w *Worker) {
		w.fanartClient = fc
	}
}

// WithDeezerClient sets the Deezer client for artist images (fallback)
func WithWorkerDeezerClient(dc *DeezerClient) WorkerOption {
	return func(w *Worker) {
		w.deezerClient = dc
	}
}

// WithArtistProvider sets the artist provider for fallback to album artwork
func WithWorkerArtistProvider(ap ArtistProvider) WorkerOption {
	return func(w *Worker) {
		w.artistProvider = ap
	}
}

// NewWorker creates a new enrichment worker
func NewWorker(provider ArtworkProvider, store JobStore, opts ...WorkerOption) *Worker {
	w := &Worker{
		provider: provider,
		store:    store,
		config:   DefaultWorkerConfig(),
		stopCh:   make(chan struct{}),
	}

	for _, opt := range opts {
		opt(w)
	}

	return w
}

// Start begins processing jobs in the background
func (w *Worker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.mu.Unlock()

	log.Info().
		Int("batchSize", w.config.BatchSize).
		Dur("interval", w.config.Interval).
		Msg("Enrichment worker started")

	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()

	// Process immediately on start
	w.processBatch(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Enrichment worker stopping (context cancelled)")
			w.mu.Lock()
			w.running = false
			w.mu.Unlock()
			return
		case <-w.stopCh:
			log.Info().Msg("Enrichment worker stopping (stop requested)")
			w.mu.Lock()
			w.running = false
			w.mu.Unlock()
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		close(w.stopCh)
	}
}

// IsRunning returns whether the worker is currently running
func (w *Worker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// processBatch processes a batch of pending jobs
func (w *Worker) processBatch(ctx context.Context) {
	jobs, err := w.store.GetPendingJobs(w.config.BatchSize)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get pending jobs")
		return
	}

	if len(jobs) == 0 {
		return
	}

	log.Debug().Int("count", len(jobs)).Msg("Processing enrichment jobs")

	for _, job := range jobs {
		select {
		case <-ctx.Done():
			return
		default:
			w.processJob(ctx, job)
		}
	}
}

// processJob processes a single enrichment job
func (w *Worker) processJob(ctx context.Context, job *EnrichmentJob) {
	// Route to appropriate handler based on job type
	switch job.Type {
	case JobTypeArtistArt:
		w.processArtistArtJob(ctx, job)
	default:
		w.processAlbumArtJob(ctx, job)
	}
}

// processAlbumArtJob processes an album artwork job
func (w *Worker) processAlbumArtJob(ctx context.Context, job *EnrichmentJob) {
	log.Debug().
		Str("jobID", job.ID).
		Str("albumID", job.AlbumID).
		Str("mbid", job.MBID).
		Int("retry", job.RetryCount).
		Msg("Processing album art enrichment job")

	// Mark as running
	job.Status = JobStatusRunning
	job.UpdatedAt = time.Now()
	w.store.UpdateJob(job)

	// Fetch artwork
	result, err := w.provider.FetchAlbumArt(ctx, job.MBID)
	if err != nil {
		w.handleJobError(job, err)
		return
	}

	// Save the artwork
	if w.config.SaveFunc != nil {
		if err := w.config.SaveFunc(job.AlbumID, result); err != nil {
			log.Error().
				Err(err).
				Str("jobID", job.ID).
				Msg("Failed to save artwork")
			w.handleJobError(job, ErrTemporaryFailure)
			return
		}
	}

	// Mark as completed
	now := time.Now()
	job.Status = JobStatusCompleted
	job.CompletedAt = &now
	job.UpdatedAt = now
	w.store.UpdateJob(job)

	log.Info().
		Str("jobID", job.ID).
		Str("albumID", job.AlbumID).
		Msg("Album art enrichment job completed successfully")
}

// processArtistArtJob processes an artist artwork job with fallback chain:
// 1. Fanart.tv (if MBID available and client configured)
// 2. Deezer (search by name, hotlink only)
// 3. First album artwork (last resort)
func (w *Worker) processArtistArtJob(ctx context.Context, job *EnrichmentJob) {
	log.Debug().
		Str("jobID", job.ID).
		Str("artistID", job.ArtistID).
		Str("artistName", job.ArtistName).
		Str("mbid", job.MBID).
		Int("retry", job.RetryCount).
		Msg("Processing artist art enrichment job")

	// Mark as running
	job.Status = JobStatusRunning
	job.UpdatedAt = time.Now()
	w.store.UpdateJob(job)

	// 1. Try Fanart.tv if MBID is available and client is configured
	if job.MBID != "" && w.fanartClient != nil && w.fanartClient.IsConfigured() {
		result, err := w.fanartClient.FetchArtistImage(ctx, job.MBID)
		if err == nil {
			// Save to cache/disk
			if w.config.SaveFuncArtist != nil {
				if err := w.config.SaveFuncArtist(job.ArtistID, result); err != nil {
					log.Warn().Err(err).Str("jobID", job.ID).Msg("Failed to save artist artwork from Fanart.tv")
				} else {
					w.completeJob(job)
					log.Info().
						Str("jobID", job.ID).
						Str("artistName", job.ArtistName).
						Str("source", "fanarttv").
						Msg("Artist art job completed (Fanart.tv)")
					return
				}
			}
		} else {
			log.Debug().
				Err(err).
				Str("artistName", job.ArtistName).
				Msg("Fanart.tv lookup failed, trying Deezer")
		}
	}

	// 2. Try Deezer (hotlink only - no caching)
	if w.deezerClient != nil {
		url, err := w.deezerClient.SearchArtistImageURL(ctx, job.ArtistName)
		if err == nil && url != "" {
			// Store URL directly in artist record (source: deezer)
			if w.artistProvider != nil {
				if err := w.artistProvider.UpdateArtistArtworkURL(job.ArtistID, url, string(SourceDeezer)); err != nil {
					log.Warn().Err(err).Str("jobID", job.ID).Msg("Failed to save Deezer URL")
				} else {
					w.completeJob(job)
					log.Info().
						Str("jobID", job.ID).
						Str("artistName", job.ArtistName).
						Str("source", "deezer").
						Msg("Artist art job completed (Deezer)")
					return
				}
			}
		} else {
			log.Debug().
				Err(err).
				Str("artistName", job.ArtistName).
				Msg("Deezer lookup failed, trying album fallback")
		}
	}

	// 3. Use first album artwork as fallback
	if w.artistProvider != nil {
		albumArtURL, err := w.artistProvider.GetFirstAlbumArtwork(job.ArtistName)
		if err == nil && albumArtURL != "" {
			if err := w.artistProvider.UpdateArtistArtworkURL(job.ArtistID, albumArtURL, string(SourceAlbumArt)); err != nil {
				log.Warn().Err(err).Str("jobID", job.ID).Msg("Failed to save album art fallback")
			} else {
				w.completeJob(job)
				log.Info().
					Str("jobID", job.ID).
					Str("artistName", job.ArtistName).
					Str("source", "albumart").
					Msg("Artist art job completed (album fallback)")
				return
			}
		}
	}

	// All fallbacks failed
	log.Debug().
		Str("artistName", job.ArtistName).
		Msg("All artist image sources failed")
	w.handleJobError(job, ErrArtworkNotFound)
}

// completeJob marks a job as successfully completed
func (w *Worker) completeJob(job *EnrichmentJob) {
	now := time.Now()
	job.Status = JobStatusCompleted
	job.CompletedAt = &now
	job.UpdatedAt = now
	w.store.UpdateJob(job)
}

// handleJobError handles errors during job processing
func (w *Worker) handleJobError(job *EnrichmentJob, err error) {
	job.LastError = err.Error()
	job.UpdatedAt = time.Now()

	// Check if this is a permanent error
	if IsPermanentError(err) {
		log.Debug().
			Str("jobID", job.ID).
			Str("error", err.Error()).
			Msg("Permanent failure, marking job as failed")
		job.Status = JobStatusFailed
		w.store.UpdateJob(job)
		return
	}

	// Temporary error - check retry count
	job.RetryCount++
	if job.RetryCount >= w.config.MaxRetries {
		log.Warn().
			Str("jobID", job.ID).
			Int("retries", job.RetryCount).
			Msg("Max retries exceeded, marking job as failed")
		job.Status = JobStatusFailed
		w.store.UpdateJob(job)
		return
	}

	// Schedule retry with exponential backoff
	backoff := CalculateBackoff(job.RetryCount)
	job.NextRetryAt = time.Now().Add(backoff)
	job.Status = JobStatusPending

	log.Debug().
		Str("jobID", job.ID).
		Int("retryCount", job.RetryCount).
		Dur("backoff", backoff).
		Time("nextRetry", job.NextRetryAt).
		Msg("Scheduling job retry")

	w.store.UpdateJob(job)
}

// AddJob adds a new job to the queue
func (w *Worker) AddJob(jobType JobType, albumID, mbid string, priority int) error {
	job := &EnrichmentJob{
		ID:          generateJobID(albumID, mbid),
		Type:        jobType,
		AlbumID:     albumID,
		MBID:        mbid,
		Status:      JobStatusPending,
		Priority:    priority,
		RetryCount:  0,
		MaxRetries:  w.config.MaxRetries,
		NextRetryAt: time.Now(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	return w.store.AddJob(job)
}

// generateJobID creates a unique job ID
func generateJobID(albumID, mbid string) string {
	return albumID + ":" + mbid
}
