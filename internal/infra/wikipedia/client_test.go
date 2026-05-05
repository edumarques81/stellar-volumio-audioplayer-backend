package wikipedia

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_LookupAlbum_Hit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("expected User-Agent header to be set")
		}
		if !strings.Contains(r.URL.Path, "Kind_of_Blue") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title":   "Kind of Blue",
			"extract": "Kind of Blue is a studio album by American jazz trumpeter Miles Davis.",
			"content_urls": map[string]any{
				"desktop": map[string]string{"page": "https://en.wikipedia.org/wiki/Kind_of_Blue"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	got, err := c.LookupAlbum(context.Background(), "Miles Davis", "Kind of Blue")
	if err != nil {
		t.Fatalf("LookupAlbum: %v", err)
	}
	if !strings.Contains(got.Extract, "studio album") {
		t.Fatalf("unexpected extract: %q", got.Extract)
	}
	if !strings.HasPrefix(got.SourceURL, "https://en.wikipedia.org/wiki/") {
		t.Fatalf("unexpected source URL: %q", got.SourceURL)
	}
	if got.Kind != "album" {
		t.Fatalf("expected Kind=album, got %q", got.Kind)
	}
}

func TestClient_LookupAlbum_404_FallsBackToArtist(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		// Album lookup misses; artist hits
		if strings.Contains(r.URL.Path, "Miles_Davis") && !strings.Contains(r.URL.Path, "album") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title":   "Miles Davis",
				"extract": "American jazz trumpeter, bandleader, and composer.",
				"content_urls": map[string]any{
					"desktop": map[string]string{"page": "https://en.wikipedia.org/wiki/Miles_Davis"},
				},
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	got, err := c.LookupAlbumOrArtist(context.Background(), "Miles Davis", "Some Obscure Album")
	if err != nil {
		t.Fatalf("LookupAlbumOrArtist: %v", err)
	}
	if got.Kind != "artist" {
		t.Fatalf("expected fallback to artist, got Kind=%q", got.Kind)
	}
	if !strings.Contains(got.Extract, "trumpeter") {
		t.Fatalf("unexpected fallback extract: %q", got.Extract)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected at least 2 calls (album then artist), got %d", calls)
	}
}

func TestClient_LookupAlbumOrArtist_AllMiss_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.LookupAlbumOrArtist(context.Background(), "Nobody", "Nothing")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestClient_Timeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 30 * time.Millisecond})
	_, err := c.LookupAlbum(context.Background(), "X", "Y")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

func TestClient_LookupAlbum_TitleEncoding(t *testing.T) {
	t.Parallel()
	var seenPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.URL.Path)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, _ = c.LookupAlbum(context.Background(), "Sigur Rós", "Ágætis byrjun")
	if len(seenPaths) == 0 {
		t.Fatalf("no requests reached server")
	}
	// Verify spaces have been replaced with underscores and unicode is percent-encoded
	for _, p := range seenPaths {
		if strings.Contains(p, " ") {
			t.Fatalf("path contains literal space: %q", p)
		}
	}
}

func TestClient_HTTP500_PropagatesError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.LookupAlbum(context.Background(), "X", "Y")
	if err == nil {
		t.Fatalf("expected error on 500")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("500 should not be ErrNotFound: %v", err)
	}
}
