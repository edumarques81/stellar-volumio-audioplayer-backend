package enrichment

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestCAA_FetchAlbumArt_Success(t *testing.T) {
	// Mock Cover Art Archive server
	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG magic bytes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format: /release/{mbid}/front
		if r.URL.Path != "/release/test-mbid-1234/front" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(imageData)
	}))
	defer server.Close()

	client := NewCAAClient(WithBaseURL(server.URL))

	result, err := client.FetchAlbumArt(context.Background(), "test-mbid-1234")
	if err != nil {
		t.Fatalf("FetchAlbumArt failed: %v", err)
	}

	if result.MimeType != "image/jpeg" {
		t.Errorf("expected mime type image/jpeg, got %s", result.MimeType)
	}
	if len(result.Data) != len(imageData) {
		t.Errorf("expected %d bytes, got %d", len(imageData), len(result.Data))
	}
	if result.Source != SourceCoverArtArchive {
		t.Errorf("expected source %s, got %s", SourceCoverArtArchive, result.Source)
	}
}

func TestCAA_FetchAlbumArt_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewCAAClient(WithBaseURL(server.URL))

	_, err := client.FetchAlbumArt(context.Background(), "nonexistent-mbid")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if err != ErrArtworkNotFound {
		t.Errorf("expected ErrArtworkNotFound, got %v", err)
	}
}

func TestCAA_FetchAlbumArt_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewCAAClient(WithBaseURL(server.URL))

	_, err := client.FetchAlbumArt(context.Background(), "test-mbid")
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

func TestCAA_RateLimiting(t *testing.T) {
	requestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0})
	}))
	defer server.Close()

	// Create client with 2 req/sec rate limit for faster testing
	client := NewCAAClient(
		WithBaseURL(server.URL),
		WithRateLimit(2), // 2 requests per second
	)

	// Make 3 requests - should take at least 1 second due to rate limiting
	start := time.Now()
	for i := 0; i < 3; i++ {
		_, err := client.FetchAlbumArt(context.Background(), fmt.Sprintf("mbid-%d", i))
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	// 3 requests at 2/sec should take at least 1 second (2 in first second, 1 waits)
	if elapsed < 500*time.Millisecond {
		t.Errorf("rate limiting not working: 3 requests completed in %v", elapsed)
	}

	mu.Lock()
	if requestCount != 3 {
		t.Errorf("expected 3 requests, got %d", requestCount)
	}
	mu.Unlock()
}

func TestCAA_Context_Cancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response
		time.Sleep(5 * time.Second)
		w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0})
	}))
	defer server.Close()

	client := NewCAAClient(WithBaseURL(server.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.FetchAlbumArt(ctx, "test-mbid")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestCAA_UserAgent(t *testing.T) {
	var receivedUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0})
	}))
	defer server.Close()

	client := NewCAAClient(
		WithBaseURL(server.URL),
		WithUserAgent("TestApp/1.0"),
	)

	client.FetchAlbumArt(context.Background(), "test-mbid")

	if receivedUserAgent != "TestApp/1.0" {
		t.Errorf("expected User-Agent 'TestApp/1.0', got '%s'", receivedUserAgent)
	}
}
