package enrichment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMusicBrainzClient_SearchArtist(t *testing.T) {
	tests := []struct {
		name       string
		artistName string
		response   string
		statusCode int
		wantErr    bool
		wantMBID   string
	}{
		{
			name:       "high confidence match",
			artistName: "Coldplay",
			response: `{
				"artists": [
					{"id": "cc197bad-dc9c-440d-a5b5-d52ba2e14234", "name": "Coldplay", "score": 100, "type": "Group"}
				],
				"count": 1,
				"offset": 0
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantMBID:   "cc197bad-dc9c-440d-a5b5-d52ba2e14234",
		},
		{
			name:       "medium confidence match",
			artistName: "Cold Play",
			response: `{
				"artists": [
					{"id": "cc197bad-dc9c-440d-a5b5-d52ba2e14234", "name": "Coldplay", "score": 65, "type": "Group"}
				],
				"count": 1,
				"offset": 0
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantMBID:   "cc197bad-dc9c-440d-a5b5-d52ba2e14234",
		},
		{
			name:       "low confidence rejected",
			artistName: "Unknown Artist XYZ",
			response: `{
				"artists": [
					{"id": "some-id", "name": "Different Artist", "score": 30, "type": "Person"}
				],
				"count": 1,
				"offset": 0
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantMBID:   "",
		},
		{
			name:       "no results",
			artistName: "Nonexistent Artist 12345",
			response: `{
				"artists": [],
				"count": 0,
				"offset": 0
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantMBID:   "",
		},
		{
			name:       "rate limited",
			artistName: "Test Artist",
			response:   ``,
			statusCode: http.StatusTooManyRequests,
			wantErr:    true,
		},
		{
			name:       "server error",
			artistName: "Test Artist",
			response:   ``,
			statusCode: http.StatusServiceUnavailable,
			wantErr:    true,
		},
		{
			name:       "multiple results picks best score",
			artistName: "Test",
			response: `{
				"artists": [
					{"id": "low-id", "name": "Test Low", "score": 50, "type": "Person"},
					{"id": "high-id", "name": "Test High", "score": 95, "type": "Group"},
					{"id": "medium-id", "name": "Test Medium", "score": 75, "type": "Person"}
				],
				"count": 3,
				"offset": 0
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantMBID:   "high-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify it's an artist query
				if r.URL.Path != "/artist" {
					t.Errorf("expected path /artist, got %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewMusicBrainzClient(WithMBBaseURL(server.URL))

			mbid, err := client.SearchArtist(context.Background(), tt.artistName)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if mbid != tt.wantMBID {
				t.Errorf("got MBID %q, want %q", mbid, tt.wantMBID)
			}
		})
	}
}

func TestMusicBrainzClient_SearchArtist_SpecialCharacters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"artists": [{"id": "test-id", "name": "Test", "score": 90, "type": "Person"}],
			"count": 1,
			"offset": 0
		}`))
	}))
	defer server.Close()

	client := NewMusicBrainzClient(WithMBBaseURL(server.URL))

	// Test with special characters that need escaping
	specialNames := []string{
		`Artist "With" Quotes`,
		`Artist: With Colon`,
		`Artist (With Parens)`,
		`Artist + And + Plus`,
	}

	for _, name := range specialNames {
		t.Run(name, func(t *testing.T) {
			_, err := client.SearchArtist(context.Background(), name)
			if err != nil {
				t.Errorf("SearchArtist(%q) failed: %v", name, err)
			}
		})
	}
}
