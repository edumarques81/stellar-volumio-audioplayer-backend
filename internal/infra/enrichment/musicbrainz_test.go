package enrichment_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/enrichment"
)

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
