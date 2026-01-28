package enrichment

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// SQLiteJobStore implements JobStore using SQLite
type SQLiteJobStore struct {
	mu sync.RWMutex
	db *sql.DB
}

// NewSQLiteJobStore creates a new SQLite job store using an existing database connection
func NewSQLiteJobStore(db *sql.DB) (*SQLiteJobStore, error) {
	store := &SQLiteJobStore{db: db}

	if err := store.initSchema(); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return store, nil
}

// initSchema creates the enrichment jobs table if it doesn't exist
func (s *SQLiteJobStore) initSchema() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS enrichment_jobs (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			album_id TEXT,
			artist_id TEXT,
			artist_name TEXT,
			mbid TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			priority INTEGER DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			max_retries INTEGER DEFAULT 3,
			next_retry_at TEXT,
			last_error TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			completed_at TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_enrichment_jobs_status ON enrichment_jobs(status);
		CREATE INDEX IF NOT EXISTS idx_enrichment_jobs_next_retry ON enrichment_jobs(next_retry_at);
		CREATE INDEX IF NOT EXISTS idx_enrichment_jobs_album ON enrichment_jobs(album_id);
	`)
	// Add artist_name column if it doesn't exist (for existing databases)
	s.db.Exec(`ALTER TABLE enrichment_jobs ADD COLUMN artist_name TEXT`)
	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	log.Debug().Msg("Enrichment jobs schema initialized")
	return nil
}

// AddJob adds a new job to the store
func (s *SQLiteJobStore) AddJob(job *EnrichmentJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO enrichment_jobs
		(id, type, album_id, artist_id, artist_name, mbid, status, priority, retry_count, max_retries, next_retry_at, last_error, created_at, updated_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		job.ID,
		string(job.Type),
		job.AlbumID,
		job.ArtistID,
		job.ArtistName,
		job.MBID,
		string(job.Status),
		job.Priority,
		job.RetryCount,
		job.MaxRetries,
		formatTime(job.NextRetryAt),
		job.LastError,
		formatTime(job.CreatedAt),
		formatTime(job.UpdatedAt),
		formatTimePtr(job.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}

	return nil
}

// GetJob retrieves a job by ID
func (s *SQLiteJobStore) GetJob(id string) (*EnrichmentJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`
		SELECT id, type, album_id, artist_id, artist_name, mbid, status, priority, retry_count, max_retries, next_retry_at, last_error, created_at, updated_at, completed_at
		FROM enrichment_jobs
		WHERE id = ?
	`, id)

	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query job: %w", err)
	}

	return job, nil
}

// GetPendingJobs retrieves pending jobs that are ready for processing
func (s *SQLiteJobStore) GetPendingJobs(limit int) ([]*EnrichmentJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := formatTime(time.Now())
	rows, err := s.db.Query(`
		SELECT id, type, album_id, artist_id, artist_name, mbid, status, priority, retry_count, max_retries, next_retry_at, last_error, created_at, updated_at, completed_at
		FROM enrichment_jobs
		WHERE status = 'pending' AND (next_retry_at IS NULL OR next_retry_at <= ?)
		ORDER BY priority DESC, created_at ASC
		LIMIT ?
	`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*EnrichmentJob
	for rows.Next() {
		job, err := scanJobRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// UpdateJob updates an existing job
func (s *SQLiteJobStore) UpdateJob(job *EnrichmentJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE enrichment_jobs
		SET status = ?, priority = ?, retry_count = ?, next_retry_at = ?, last_error = ?, updated_at = ?, completed_at = ?
		WHERE id = ?
	`,
		string(job.Status),
		job.Priority,
		job.RetryCount,
		formatTime(job.NextRetryAt),
		job.LastError,
		formatTime(job.UpdatedAt),
		formatTimePtr(job.CompletedAt),
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}

	return nil
}

// DeleteJob removes a job from the store
func (s *SQLiteJobStore) DeleteJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM enrichment_jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete job: %w", err)
	}

	return nil
}

// GetStats returns statistics about the job queue
func (s *SQLiteJobStore) GetStats() (pending, running, completed, failed int, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT status, COUNT(*) FROM enrichment_jobs GROUP BY status
	`)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("query stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return 0, 0, 0, 0, fmt.Errorf("scan stats: %w", err)
		}

		switch JobStatus(status) {
		case JobStatusPending:
			pending = count
		case JobStatusRunning:
			running = count
		case JobStatusCompleted:
			completed = count
		case JobStatusFailed:
			failed = count
		}
	}

	return pending, running, completed, failed, nil
}

// CleanupCompleted removes completed jobs older than the specified duration
func (s *SQLiteJobStore) CleanupCompleted(olderThan time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := formatTime(time.Now().Add(-olderThan))
	result, err := s.db.Exec(`
		DELETE FROM enrichment_jobs
		WHERE status = 'completed' AND completed_at < ?
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("cleanup: %w", err)
	}

	return result.RowsAffected()
}

// Helper functions

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func formatTimePtr(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func parseTimePtr(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t := parseTime(s.String)
	return &t
}

// scannable is an interface for both *sql.Row and *sql.Rows
type scannable interface {
	Scan(dest ...interface{}) error
}

func scanJob(row *sql.Row) (*EnrichmentJob, error) {
	var job EnrichmentJob
	var jobType, status string
	var nextRetryAt, createdAt, updatedAt string
	var completedAt sql.NullString
	var albumID, artistID, artistName sql.NullString

	err := row.Scan(
		&job.ID,
		&jobType,
		&albumID,
		&artistID,
		&artistName,
		&job.MBID,
		&status,
		&job.Priority,
		&job.RetryCount,
		&job.MaxRetries,
		&nextRetryAt,
		&job.LastError,
		&createdAt,
		&updatedAt,
		&completedAt,
	)
	if err != nil {
		return nil, err
	}

	job.Type = JobType(jobType)
	job.Status = JobStatus(status)
	job.AlbumID = albumID.String
	job.ArtistID = artistID.String
	job.ArtistName = artistName.String
	job.NextRetryAt = parseTime(nextRetryAt)
	job.CreatedAt = parseTime(createdAt)
	job.UpdatedAt = parseTime(updatedAt)
	job.CompletedAt = parseTimePtr(completedAt)

	return &job, nil
}

func scanJobRow(rows *sql.Rows) (*EnrichmentJob, error) {
	var job EnrichmentJob
	var jobType, status string
	var nextRetryAt, createdAt, updatedAt string
	var completedAt sql.NullString
	var albumID, artistID, artistName sql.NullString

	err := rows.Scan(
		&job.ID,
		&jobType,
		&albumID,
		&artistID,
		&artistName,
		&job.MBID,
		&status,
		&job.Priority,
		&job.RetryCount,
		&job.MaxRetries,
		&nextRetryAt,
		&job.LastError,
		&createdAt,
		&updatedAt,
		&completedAt,
	)
	if err != nil {
		return nil, err
	}

	job.Type = JobType(jobType)
	job.Status = JobStatus(status)
	job.AlbumID = albumID.String
	job.ArtistID = artistID.String
	job.ArtistName = artistName.String
	job.NextRetryAt = parseTime(nextRetryAt)
	job.CreatedAt = parseTime(createdAt)
	job.UpdatedAt = parseTime(updatedAt)
	job.CompletedAt = parseTimePtr(completedAt)

	return &job, nil
}
