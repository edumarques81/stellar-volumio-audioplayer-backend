package enrichment_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/enrichment"
)

func TestNormalizeForSearch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Pink Floyd", "pink floyd"},
		{"The Wall (Remaster)", "the wall"},
		{"The Wall (Deluxe Edition)", "the wall"},
		{"The Wall [2011 Remaster]", "the wall"},
		{"AC/DC", "ac dc"},
		{"Guns N' Roses", "guns n roses"},
		{"  Extra   Spaces  ", "extra spaces"},
		{"UPPER CASE", "upper case"},
		{"Album: Part 1", "album part 1"},
		{"Hello (Live) (Bonus Track)", "hello"},
		{"Nirvana [Remastered] [Deluxe]", "nirvana"},
		{"", ""},
		{"  ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := enrichment.NormalizeForSearch(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeForSearch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLongestWord(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"pink floyd", "floyd"},
		{"a bb ccc", "ccc"},
		{"single", "single"},
		{"", ""},
		{"a b c", "a"}, // all same length, first wins
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := enrichment.LongestWord(tt.input)
			if got != tt.want {
				t.Errorf("LongestWord(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMusicBrainzClientSearchRelease(t *testing.T) {
	tests := []struct {
		name           string
		artist         string
		album          string
		serverResponse string
		serverStatus   int
		wantMBID       string
		wantErr        bool
	}{
		{
			name:   "high confidence match",
			artist: "Beethoven",
			album:  "Symphony No. 9",
			serverResponse: `{
				"releases": [
					{"id": "abc123-mbid", "title": "Symphony No. 9", "score": 95, "status": "Official"}
				],
				"count": 1,
				"offset": 0
			}`,
			serverStatus: http.StatusOK,
			wantMBID:     "abc123-mbid",
			wantErr:      false,
		},
		{
			name:   "medium confidence match",
			artist: "Some Artist",
			album:  "Some Album",
			serverResponse: `{
				"releases": [
					{"id": "def456-mbid", "title": "Some Album", "score": 65, "status": "Official"}
				],
				"count": 1,
				"offset": 0
			}`,
			serverStatus: http.StatusOK,
			wantMBID:     "def456-mbid",
			wantErr:      false,
		},
		{
			name:   "low confidence match rejected",
			artist: "Unknown Artist",
			album:  "Unknown Album",
			serverResponse: `{
				"releases": [
					{"id": "ghi789-mbid", "title": "Different Album", "score": 30, "status": "Official"}
				],
				"count": 1,
				"offset": 0
			}`,
			serverStatus: http.StatusOK,
			wantMBID:     "",
			wantErr:      false,
		},
		{
			name:           "no results",
			artist:         "Nonexistent Artist",
			album:          "Nonexistent Album",
			serverResponse: `{"releases": [], "count": 0, "offset": 0}`,
			serverStatus:   http.StatusOK,
			wantMBID:       "",
			wantErr:        false,
		},
		{
			name:           "rate limited",
			artist:         "Test Artist",
			album:          "Test Album",
			serverResponse: "",
			serverStatus:   http.StatusTooManyRequests,
			wantMBID:       "",
			wantErr:        true,
		},
		{
			name:           "server error",
			artist:         "Test Artist",
			album:          "Test Album",
			serverResponse: "",
			serverStatus:   http.StatusServiceUnavailable,
			wantMBID:       "",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				if r.Method != http.MethodGet {
					t.Errorf("expected GET request, got %s", r.Method)
				}
				if r.Header.Get("Accept") != "application/json" {
					t.Errorf("expected Accept: application/json header")
				}
				if r.Header.Get("User-Agent") == "" {
					t.Errorf("expected User-Agent header")
				}

				w.WriteHeader(tt.serverStatus)
				w.Write([]byte(tt.serverResponse))
			}))
			defer server.Close()

			// Create client with test server URL
			client := enrichment.NewMusicBrainzClient(
				enrichment.WithMBBaseURL(server.URL),
			)

			// Execute search
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			mbid, err := client.SearchRelease(ctx, tt.artist, tt.album)

			// Verify results
			if (err != nil) != tt.wantErr {
				t.Errorf("SearchRelease() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if mbid != tt.wantMBID {
				t.Errorf("SearchRelease() mbid = %v, want %v", mbid, tt.wantMBID)
			}
		})
	}
}

func TestMusicBrainzClientSearchReleaseMultipleResults(t *testing.T) {
	// Server returns multiple results with varying scores
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := enrichment.MBSearchResponse{
			Releases: []enrichment.MBRelease{
				{ID: "low-score", Title: "Wrong Album", Score: 40, Status: "Official"},
				{ID: "high-score", Title: "Correct Album", Score: 90, Status: "Official"},
				{ID: "medium-score", Title: "Similar Album", Score: 70, Status: "Official"},
			},
			Count:  3,
			Offset: 0,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := enrichment.NewMusicBrainzClient(
		enrichment.WithMBBaseURL(server.URL),
	)

	ctx := context.Background()
	mbid, err := client.SearchRelease(ctx, "Test Artist", "Test Album")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return the first high-score (>=80) result
	if mbid != "high-score" {
		t.Errorf("expected high-score mbid, got %s", mbid)
	}
}

func TestMusicBrainzClientSpecialCharacters(t *testing.T) {
	var receivedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		resp := enrichment.MBSearchResponse{
			Releases: []enrichment.MBRelease{
				{ID: "test-id", Title: "Test", Score: 90},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := enrichment.NewMusicBrainzClient(
		enrichment.WithMBBaseURL(server.URL),
	)

	ctx := context.Background()
	// Test with special characters
	_, err := client.SearchRelease(ctx, `Artist "With" (Quotes)`, `Album: Part 1`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify that special characters were escaped
	if receivedQuery == "" {
		t.Error("expected query to be sent")
	}
}

func TestSearchReleaseFuzzyFallback(t *testing.T) {
	t.Run("exact match fails, unquoted succeeds", func(t *testing.T) {
		var requestCount int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)
			query := r.URL.Query().Get("query")

			if count == 1 {
				// First request (exact quoted) - return no results
				if !strings.Contains(query, `"`) {
					t.Errorf("expected quoted query on first attempt, got: %s", query)
				}
				json.NewEncoder(w).Encode(enrichment.MBSearchResponse{})
			} else if count == 2 {
				// Second request (unquoted) - return match
				json.NewEncoder(w).Encode(enrichment.MBSearchResponse{
					Releases: []enrichment.MBRelease{
						{ID: "fuzzy-match", Title: "The Wall", Score: 90},
					},
				})
			}
		}))
		defer server.Close()

		client := enrichment.NewMusicBrainzClient(enrichment.WithMBBaseURL(server.URL))
		mbid, err := client.SearchRelease(context.Background(), "pink floyd", "The Wall (Remaster)")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mbid != "fuzzy-match" {
			t.Errorf("expected fuzzy-match, got %s", mbid)
		}
		if atomic.LoadInt32(&requestCount) < 2 {
			t.Error("expected at least 2 requests (exact + unquoted)")
		}
	})

	t.Run("artist-only fallback with title matching", func(t *testing.T) {
		var requestCount int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)

			if count <= 2 {
				// First two strategies return no results
				json.NewEncoder(w).Encode(enrichment.MBSearchResponse{})
			} else {
				// Third (artist-only) - return releases, one matching album
				json.NewEncoder(w).Encode(enrichment.MBSearchResponse{
					Releases: []enrichment.MBRelease{
						{ID: "wrong-album", Title: "Different Album", Score: 95},
						{ID: "correct-album", Title: "The Wall", Score: 90},
					},
				})
			}
		}))
		defer server.Close()

		client := enrichment.NewMusicBrainzClient(enrichment.WithMBBaseURL(server.URL))
		mbid, err := client.SearchRelease(context.Background(), "Pink Floyd", "The Wall")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mbid != "correct-album" {
			t.Errorf("expected correct-album, got %s", mbid)
		}
	})
}

func TestSearchArtistFuzzyFallback(t *testing.T) {
	t.Run("exact fails, unquoted succeeds", func(t *testing.T) {
		var requestCount int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)

			if count == 1 {
				// Exact quoted - no results
				json.NewEncoder(w).Encode(enrichment.MBArtistSearchResponse{})
			} else {
				// Unquoted - match
				json.NewEncoder(w).Encode(enrichment.MBArtistSearchResponse{
					Artists: []enrichment.MBArtist{
						{ID: "fuzzy-artist", Name: "Pink Floyd", Score: 95, Type: "Group"},
					},
				})
			}
		}))
		defer server.Close()

		client := enrichment.NewMusicBrainzClient(enrichment.WithMBBaseURL(server.URL))
		mbid, err := client.SearchArtist(context.Background(), "pink floyd")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mbid != "fuzzy-artist" {
			t.Errorf("expected fuzzy-artist, got %s", mbid)
		}
	})

	t.Run("longest word fallback", func(t *testing.T) {
		var requestCount int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)

			if count <= 2 {
				// First two strategies fail
				json.NewEncoder(w).Encode(enrichment.MBArtistSearchResponse{})
			} else {
				// Longest word search succeeds
				json.NewEncoder(w).Encode(enrichment.MBArtistSearchResponse{
					Artists: []enrichment.MBArtist{
						{ID: "longest-match", Name: "Beethoven", Score: 90, Type: "Person"},
					},
				})
			}
		}))
		defer server.Close()

		client := enrichment.NewMusicBrainzClient(enrichment.WithMBBaseURL(server.URL))
		mbid, err := client.SearchArtist(context.Background(), "Ludwig van Beethoven")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mbid != "longest-match" {
			t.Errorf("expected longest-match, got %s", mbid)
		}
	})
}
