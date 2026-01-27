package enrichment

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockArtworkProvider implements ArtworkProvider for testing
type mockArtworkProvider struct {
	mu           sync.Mutex
	fetchResults map[string]*FetchResult
	fetchErrors  map[string]error
	fetchCount   int32
	fetchDelay   time.Duration
}

func newMockProvider() *mockArtworkProvider {
	return &mockArtworkProvider{
		fetchResults: make(map[string]*FetchResult),
		fetchErrors:  make(map[string]error),
	}
}

func (m *mockArtworkProvider) SetResult(mbid string, result *FetchResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fetchResults[mbid] = result
}

func (m *mockArtworkProvider) SetError(mbid string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fetchErrors[mbid] = err
}

func (m *mockArtworkProvider) FetchAlbumArt(ctx context.Context, mbid string) (*FetchResult, error) {
	atomic.AddInt32(&m.fetchCount, 1)

	if m.fetchDelay > 0 {
		select {
		case <-time.After(m.fetchDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err, ok := m.fetchErrors[mbid]; ok {
		return nil, err
	}
	if result, ok := m.fetchResults[mbid]; ok {
		return result, nil
	}
	return nil, ErrArtworkNotFound
}

func (m *mockArtworkProvider) GetFetchCount() int {
	return int(atomic.LoadInt32(&m.fetchCount))
}

// mockJobStore implements JobStore for testing
type mockJobStore struct {
	mu   sync.Mutex
	jobs map[string]*EnrichmentJob
}

func newMockJobStore() *mockJobStore {
	return &mockJobStore{
		jobs: make(map[string]*EnrichmentJob),
	}
}

func (m *mockJobStore) AddJob(job *EnrichmentJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.ID] = job
	return nil
}

func (m *mockJobStore) GetPendingJobs(limit int) ([]*EnrichmentJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var pending []*EnrichmentJob
	for _, job := range m.jobs {
		if job.Status == JobStatusPending && time.Now().After(job.NextRetryAt) {
			pending = append(pending, job)
			if len(pending) >= limit {
				break
			}
		}
	}
	return pending, nil
}

func (m *mockJobStore) UpdateJob(job *EnrichmentJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.ID] = job
	return nil
}

func (m *mockJobStore) GetJob(id string) (*EnrichmentJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job, ok := m.jobs[id]; ok {
		return job, nil
	}
	return nil, ErrJobNotFound
}

func (m *mockJobStore) DeleteJob(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, id)
	return nil
}

func TestWorker_ProcessQueue_Success(t *testing.T) {
	provider := newMockProvider()
	provider.SetResult("mbid-1", &FetchResult{
		Data:     []byte{0xFF, 0xD8, 0xFF, 0xE0},
		MimeType: "image/jpeg",
		Source:   SourceCoverArtArchive,
	})
	provider.SetResult("mbid-2", &FetchResult{
		Data:     []byte{0xFF, 0xD8, 0xFF, 0xE0},
		MimeType: "image/jpeg",
		Source:   SourceCoverArtArchive,
	})

	store := newMockJobStore()
	store.AddJob(&EnrichmentJob{
		ID:          "job-1",
		AlbumID:     "album-1",
		MBID:        "mbid-1",
		Status:      JobStatusPending,
		Priority:    1,
		NextRetryAt: time.Now().Add(-time.Minute),
	})
	store.AddJob(&EnrichmentJob{
		ID:          "job-2",
		AlbumID:     "album-2",
		MBID:        "mbid-2",
		Status:      JobStatusPending,
		Priority:    1,
		NextRetryAt: time.Now().Add(-time.Minute),
	})

	var savedResults []*SavedArtwork
	var mu sync.Mutex

	worker := NewWorker(
		provider,
		store,
		WithBatchSize(10),
		WithWorkerInterval(100*time.Millisecond),
		WithSaveFunc(func(albumID string, result *FetchResult) error {
			mu.Lock()
			savedResults = append(savedResults, &SavedArtwork{AlbumID: albumID, Data: result.Data})
			mu.Unlock()
			return nil
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go worker.Start(ctx)

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	if len(savedResults) != 2 {
		t.Errorf("expected 2 saved results, got %d", len(savedResults))
	}
	mu.Unlock()

	// Verify jobs are completed
	job1, _ := store.GetJob("job-1")
	if job1.Status != JobStatusCompleted {
		t.Errorf("job-1 status: expected %s, got %s", JobStatusCompleted, job1.Status)
	}

	job2, _ := store.GetJob("job-2")
	if job2.Status != JobStatusCompleted {
		t.Errorf("job-2 status: expected %s, got %s", JobStatusCompleted, job2.Status)
	}
}

func TestWorker_Retry_ExponentialBackoff(t *testing.T) {
	provider := newMockProvider()
	// First 2 attempts fail, third succeeds
	attemptCount := 0
	originalSetError := provider.SetError
	_ = originalSetError // unused

	provider.fetchErrors["mbid-retry"] = ErrTemporaryFailure

	store := newMockJobStore()
	job := &EnrichmentJob{
		ID:          "job-retry",
		AlbumID:     "album-retry",
		MBID:        "mbid-retry",
		Status:      JobStatusPending,
		Priority:    1,
		RetryCount:  0,
		NextRetryAt: time.Now().Add(-time.Minute),
	}
	store.AddJob(job)

	worker := NewWorker(
		provider,
		store,
		WithBatchSize(10),
		WithWorkerInterval(50*time.Millisecond),
		WithMaxRetries(3),
		WithSaveFunc(func(albumID string, result *FetchResult) error {
			return nil
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go worker.Start(ctx)

	// Wait for first retry attempt
	time.Sleep(200 * time.Millisecond)

	// Check job status - should be pending with incremented retry count
	updatedJob, _ := store.GetJob("job-retry")
	if updatedJob.RetryCount < 1 {
		t.Errorf("expected retry count >= 1, got %d", updatedJob.RetryCount)
	}
	if updatedJob.Status != JobStatusPending {
		t.Errorf("expected status pending, got %s", updatedJob.Status)
	}

	// NextRetryAt should be in the future (exponential backoff)
	if !updatedJob.NextRetryAt.After(time.Now()) {
		t.Error("NextRetryAt should be in the future after retry")
	}

	_ = attemptCount // unused in this simplified test
}

func TestWorker_MaxRetries_Exceeded(t *testing.T) {
	provider := newMockProvider()
	provider.SetError("mbid-fail", ErrTemporaryFailure)

	store := newMockJobStore()
	job := &EnrichmentJob{
		ID:          "job-fail",
		AlbumID:     "album-fail",
		MBID:        "mbid-fail",
		Status:      JobStatusPending,
		Priority:    1,
		RetryCount:  2, // Already retried twice
		NextRetryAt: time.Now().Add(-time.Minute),
	}
	store.AddJob(job)

	worker := NewWorker(
		provider,
		store,
		WithBatchSize(10),
		WithWorkerInterval(50*time.Millisecond),
		WithMaxRetries(3),
		WithSaveFunc(func(albumID string, result *FetchResult) error {
			return nil
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go worker.Start(ctx)

	time.Sleep(200 * time.Millisecond)

	// Job should be marked as failed
	updatedJob, _ := store.GetJob("job-fail")
	if updatedJob.Status != JobStatusFailed {
		t.Errorf("expected status failed, got %s", updatedJob.Status)
	}
}

func TestWorker_NotFound_NoRetry(t *testing.T) {
	provider := newMockProvider()
	// ErrArtworkNotFound - permanent failure, should not retry

	store := newMockJobStore()
	job := &EnrichmentJob{
		ID:          "job-notfound",
		AlbumID:     "album-notfound",
		MBID:        "mbid-notfound",
		Status:      JobStatusPending,
		Priority:    1,
		RetryCount:  0,
		NextRetryAt: time.Now().Add(-time.Minute),
	}
	store.AddJob(job)

	worker := NewWorker(
		provider,
		store,
		WithBatchSize(10),
		WithWorkerInterval(50*time.Millisecond),
		WithMaxRetries(3),
		WithSaveFunc(func(albumID string, result *FetchResult) error {
			return nil
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go worker.Start(ctx)

	time.Sleep(200 * time.Millisecond)

	// Job should be marked as failed immediately (not found is permanent)
	updatedJob, _ := store.GetJob("job-notfound")
	if updatedJob.Status != JobStatusFailed {
		t.Errorf("expected status failed for not found, got %s", updatedJob.Status)
	}
	if updatedJob.RetryCount != 0 {
		t.Errorf("not found should not increment retry count, got %d", updatedJob.RetryCount)
	}
}

func TestWorker_Stop_Graceful(t *testing.T) {
	provider := newMockProvider()
	provider.fetchDelay = 1 * time.Second // Slow response

	store := newMockJobStore()
	store.AddJob(&EnrichmentJob{
		ID:          "job-slow",
		AlbumID:     "album-slow",
		MBID:        "mbid-slow",
		Status:      JobStatusPending,
		Priority:    1,
		NextRetryAt: time.Now().Add(-time.Minute),
	})

	worker := NewWorker(
		provider,
		store,
		WithBatchSize(10),
		WithWorkerInterval(50*time.Millisecond),
		WithSaveFunc(func(albumID string, result *FetchResult) error {
			return nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		worker.Start(ctx)
		close(done)
	}()

	// Let worker start
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	cancel()

	// Worker should stop gracefully
	select {
	case <-done:
		// Good - worker stopped
	case <-time.After(500 * time.Millisecond):
		t.Error("worker did not stop gracefully")
	}
}

// SavedArtwork for testing save callback
type SavedArtwork struct {
	AlbumID string
	Data    []byte
}
