// Package enrichment provides web metadata enrichment services for artwork.
package enrichment

import (
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Album represents minimal album info for enrichment.
type Album struct {
	ID          string
	Title       string
	AlbumArtist string
	FirstTrack  string
	HasArtwork  bool
}

// AlbumProvider provides album info for enrichment.
type AlbumProvider interface {
	GetAlbumsWithoutArtwork() ([]Album, error)
	UpdateAlbumArtwork(albumID, artworkID string) error
}

// Coordinator orchestrates artwork enrichment from web sources.
// It finds albums missing artwork, looks up MBIDs, and queues jobs.
type Coordinator struct {
	mu             sync.Mutex
	mbClient       *MusicBrainzClient
	caaClient      *CAAClient
	jobStore       JobStore
	albumProvider  AlbumProvider
	cacheDir       string
	running        bool
	processingDone chan struct{}
}

// NewCoordinator creates a new enrichment coordinator.
func NewCoordinator(
	mbClient *MusicBrainzClient,
	caaClient *CAAClient,
	jobStore JobStore,
	albumProvider AlbumProvider,
	cacheDir string,
) *Coordinator {
	return &Coordinator{
		mbClient:      mbClient,
		caaClient:     caaClient,
		jobStore:      jobStore,
		albumProvider: albumProvider,
		cacheDir:      cacheDir,
	}
}

// QueueMissingArtwork finds albums without artwork, looks up MBIDs,
// and queues enrichment jobs. This is called after cache build completes.
func (c *Coordinator) QueueMissingArtwork(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil // Already running
	}
	c.running = true
	c.processingDone = make(chan struct{})
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.running = false
		close(c.processingDone)
		c.mu.Unlock()
	}()

	log.Info().Msg("Starting artwork enrichment queue processing")

	// Get albums without artwork
	albums, err := c.albumProvider.GetAlbumsWithoutArtwork()
	if err != nil {
		return fmt.Errorf("get albums without artwork: %w", err)
	}

	if len(albums) == 0 {
		log.Info().Msg("No albums missing artwork")
		return nil
	}

	log.Info().Int("count", len(albums)).Msg("Found albums missing artwork")

	queued := 0
	skipped := 0

	for _, album := range albums {
		select {
		case <-ctx.Done():
			log.Info().Msg("Enrichment queue processing cancelled")
			return ctx.Err()
		default:
		}

		// Look up MBID
		mbid, err := c.mbClient.SearchRelease(ctx, album.AlbumArtist, album.Title)
		if err != nil {
			log.Debug().
				Err(err).
				Str("album", album.Title).
				Str("artist", album.AlbumArtist).
				Msg("MusicBrainz lookup failed")
			skipped++
			continue
		}

		if mbid == "" {
			log.Debug().
				Str("album", album.Title).
				Str("artist", album.AlbumArtist).
				Msg("No MusicBrainz match found")
			skipped++
			continue
		}

		// Queue job
		job := &EnrichmentJob{
			ID:          generateJobID(album.ID, mbid),
			Type:        JobTypeAlbumArt,
			AlbumID:     album.ID,
			MBID:        mbid,
			Status:      JobStatusPending,
			Priority:    0,
			MaxRetries:  3,
			NextRetryAt: time.Now(),
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := c.jobStore.AddJob(job); err != nil {
			log.Warn().Err(err).Str("albumID", album.ID).Msg("Failed to queue enrichment job")
			continue
		}

		queued++
		log.Debug().
			Str("album", album.Title).
			Str("mbid", mbid).
			Msg("Queued enrichment job")
	}

	log.Info().
		Int("queued", queued).
		Int("skipped", skipped).
		Msg("Enrichment queue processing complete")

	return nil
}

// CreateSaveFunc returns a SaveFunc that saves artwork to disk and updates the cache.
func (c *Coordinator) CreateSaveFunc() SaveFunc {
	return func(albumID string, result *FetchResult) error {
		// Determine file extension
		ext := ".jpg"
		switch result.MimeType {
		case "image/png":
			ext = ".png"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		}

		// Create cache directory
		artworkDir := filepath.Join(c.cacheDir, "artwork", "albums")
		if err := os.MkdirAll(artworkDir, 0755); err != nil {
			return fmt.Errorf("create artwork dir: %w", err)
		}

		// Save file
		filename := albumID + ext
		filePath := filepath.Join(artworkDir, filename)
		if err := os.WriteFile(filePath, result.Data, 0644); err != nil {
			return fmt.Errorf("write artwork file: %w", err)
		}

		// Generate artwork ID and update album
		artworkID := generateArtworkID(albumID, "album")
		if c.albumProvider != nil {
			if err := c.albumProvider.UpdateAlbumArtwork(albumID, artworkID); err != nil {
				log.Warn().Err(err).Str("albumID", albumID).Msg("Failed to update album artwork")
			}
		}

		log.Info().
			Str("albumID", albumID).
			Str("path", filePath).
			Int("size", len(result.Data)).
			Msg("Saved enriched artwork")

		return nil
	}
}

// IsRunning returns whether enrichment processing is running.
func (c *Coordinator) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// WaitForDone waits for the current enrichment processing to complete.
func (c *Coordinator) WaitForDone() {
	c.mu.Lock()
	done := c.processingDone
	c.mu.Unlock()

	if done != nil {
		<-done
	}
}

func generateArtworkID(albumID, artType string) string {
	data := albumID + "\x00" + artType
	return fmt.Sprintf("%x", md5.Sum([]byte(data)))
}
