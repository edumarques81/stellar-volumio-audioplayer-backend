package enrichment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFanartClient_IsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		apiKey   string
		expected bool
	}{
		{"with API key", "test-api-key", true},
		{"without API key", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewFanartClient(tt.apiKey)
			if got := client.IsConfigured(); got != tt.expected {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFanartClient_FetchArtistImage(t *testing.T) {
	tests := []struct {
		name       string
		apiKey     string
		mbid       string
		response   string
		statusCode int
		wantErr    bool
		errType    error
	}{
		{
			name:   "successful fetch",
			apiKey: "test-key",
			mbid:   "test-mbid",
			response: `{
				"name": "Test Artist",
				"mbid_id": "test-mbid",
				"artistthumb": [
					{"id": "123", "url": "IMAGEURL", "likes": "10"}
				]
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "not found",
			apiKey:     "test-key",
			mbid:       "unknown-mbid",
			response:   `{"error": "not found"}`,
			statusCode: http.StatusNotFound,
			wantErr:    true,
			errType:    ErrArtworkNotFound,
		},
		{
			name:       "rate limited",
			apiKey:     "test-key",
			mbid:       "test-mbid",
			response:   ``,
			statusCode: http.StatusTooManyRequests,
			wantErr:    true,
			errType:    ErrRateLimited,
		},
		{
			name:       "no artistthumb",
			apiKey:     "test-key",
			mbid:       "test-mbid",
			response:   `{"name": "Test Artist", "artistthumb": []}`,
			statusCode: http.StatusOK,
			wantErr:    true,
			errType:    ErrArtworkNotFound,
		},
		{
			name:       "no API key",
			apiKey:     "",
			mbid:       "test-mbid",
			response:   ``,
			statusCode: http.StatusOK,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			var imageServer *httptest.Server
			if tt.name == "successful fetch" {
				// Create a separate server for the image download
				imageServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "image/jpeg")
					w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0}) // JPEG magic bytes
				}))
				defer imageServer.Close()
				// Replace IMAGEURL in response
				tt.response = `{
					"name": "Test Artist",
					"mbid_id": "test-mbid",
					"artistthumb": [
						{"id": "123", "url": "` + imageServer.URL + `", "likes": "10"}
					]
				}`
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewFanartClient(tt.apiKey, WithFanartBaseURL(server.URL))

			result, err := client.FetchArtistImage(context.Background(), tt.mbid)

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

			if result == nil {
				t.Error("expected result, got nil")
				return
			}

			if result.Source != SourceFanartTV {
				t.Errorf("expected source %s, got %s", SourceFanartTV, result.Source)
			}
		})
	}
}

func TestFanartImage_getLikes(t *testing.T) {
	tests := []struct {
		name     string
		likes    string
		expected int
	}{
		{"valid number", "42", 42},
		{"zero", "0", 0},
		{"invalid", "abc", 0},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := FanartImage{Likes: tt.likes}
			if got := img.getLikes(); got != tt.expected {
				t.Errorf("getLikes() = %v, want %v", got, tt.expected)
			}
		})
	}
}
