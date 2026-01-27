// Package enrichment provides web metadata enrichment services for artwork.
package enrichment

import (
	"context"
	"errors"
	"time"
)

// Common errors
var (
	// ErrArtworkNotFound indicates artwork was not found (permanent failure)
	ErrArtworkNotFound = errors.New("artwork not found")

	// ErrTemporaryFailure indicates a temporary failure (should retry)
	ErrTemporaryFailure = errors.New("temporary failure")

	// ErrRateLimited indicates rate limit was exceeded
	ErrRateLimited = errors.New("rate limited")

	// ErrJobNotFound indicates the job was not found in the store
	ErrJobNotFound = errors.New("job not found")
)

// Source indicates where the artwork was fetched from
type Source string

const (
	SourceCoverArtArchive Source = "cover_art_archive"
	SourceLastFM          Source = "lastfm"
	SourceFanartTV        Source = "fanarttv"
	SourceMPD             Source = "mpd"
	SourceEmbedded        Source = "embedded"
	SourceFolder          Source = "folder"
)

// FetchResult contains the result of an artwork fetch operation
type FetchResult struct {
	Data     []byte
	MimeType string
	Source   Source
	Width    int
	Height   int
}

// ArtworkProvider defines the interface for fetching artwork from external sources
type ArtworkProvider interface {
	FetchAlbumArt(ctx context.Context, mbid string) (*FetchResult, error)
}

// JobStatus represents the status of an enrichment job
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

// JobType represents the type of enrichment job
type JobType string

const (
	JobTypeAlbumArt   JobType = "album_art"
	JobTypeArtistArt  JobType = "artist_art"
)

// EnrichmentJob represents a job in the enrichment queue
type EnrichmentJob struct {
	ID          string
	Type        JobType
	AlbumID     string    // For album art jobs
	ArtistID    string    // For artist art jobs
	MBID        string    // MusicBrainz ID
	Status      JobStatus
	Priority    int       // Higher = more important
	RetryCount  int
	MaxRetries  int
	NextRetryAt time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time
}

// JobStore defines the interface for storing and retrieving enrichment jobs
type JobStore interface {
	AddJob(job *EnrichmentJob) error
	GetJob(id string) (*EnrichmentJob, error)
	GetPendingJobs(limit int) ([]*EnrichmentJob, error)
	UpdateJob(job *EnrichmentJob) error
	DeleteJob(id string) error
}

// SaveFunc is a callback function for saving fetched artwork
type SaveFunc func(albumID string, result *FetchResult) error

// WorkerConfig contains configuration for the enrichment worker
type WorkerConfig struct {
	BatchSize    int
	Interval     time.Duration
	MaxRetries   int
	SaveFunc     SaveFunc
}

// DefaultWorkerConfig returns the default worker configuration
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		BatchSize:  10,
		Interval:   60 * time.Second,
		MaxRetries: 3,
	}
}

// IsPermanentError returns true if the error indicates a permanent failure
func IsPermanentError(err error) bool {
	return errors.Is(err, ErrArtworkNotFound)
}

// IsTemporaryError returns true if the error indicates a temporary failure
func IsTemporaryError(err error) bool {
	return errors.Is(err, ErrTemporaryFailure) || errors.Is(err, ErrRateLimited)
}

// CalculateBackoff returns the next retry delay using exponential backoff
func CalculateBackoff(retryCount int) time.Duration {
	// Base delay of 1 minute, doubles with each retry
	// Max backoff of 24 hours
	base := time.Minute
	delay := base * time.Duration(1<<retryCount) // 1, 2, 4, 8, 16... minutes

	maxDelay := 24 * time.Hour
	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}
