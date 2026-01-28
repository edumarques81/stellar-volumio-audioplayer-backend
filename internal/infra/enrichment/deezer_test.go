package enrichment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeezerClient_SearchArtistImageURL(t *testing.T) {
	tests := []struct {
		name       string
		artistName string
		response   string
		statusCode int
		wantErr    bool
		wantURL    string
	}{
		{
			name:       "exact match",
			artistName: "Coldplay",
			response: `{
				"data": [
					{
						"id": 123,
						"name": "Coldplay",
						"picture_xl": "https://example.com/coldplay_xl.jpg",
						"picture_big": "https://example.com/coldplay_big.jpg"
					}
				],
				"total": 1
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantURL:    "https://example.com/coldplay_xl.jpg",
		},
		{
			name:       "partial match",
			artistName: "cold",
			response: `{
				"data": [
					{
						"id": 123,
						"name": "Coldplay",
						"picture_xl": "https://example.com/coldplay_xl.jpg"
					}
				],
				"total": 1
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantURL:    "https://example.com/coldplay_xl.jpg",
		},
		{
			name:       "no results",
			artistName: "nonexistent artist xyz123",
			response: `{
				"data": [],
				"total": 0
			}`,
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "rate limited",
			artistName: "Test Artist",
			response:   `{"error": {"type": "QuotaException"}}`,
			statusCode: http.StatusTooManyRequests,
			wantErr:    true,
		},
		{
			name:       "uses first result when no exact match",
			artistName: "Test",
			response: `{
				"data": [
					{
						"id": 456,
						"name": "Different Artist",
						"picture_xl": "https://example.com/different_xl.jpg"
					}
				],
				"total": 1
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantURL:    "https://example.com/different_xl.jpg",
		},
		{
			name:       "fallback to picture_big",
			artistName: "Coldplay",
			response: `{
				"data": [
					{
						"id": 123,
						"name": "Coldplay",
						"picture_xl": "",
						"picture_big": "https://example.com/coldplay_big.jpg"
					}
				],
				"total": 1
			}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantURL:    "https://example.com/coldplay_big.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewDeezerClient(WithDeezerBaseURL(server.URL))

			url, err := client.SearchArtistImageURL(context.Background(), tt.artistName)

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

			if url != tt.wantURL {
				t.Errorf("got URL %q, want %q", url, tt.wantURL)
			}
		})
	}
}

func TestNewDeezerClient_Defaults(t *testing.T) {
	client := NewDeezerClient()

	if client.baseURL != DefaultDeezerBaseURL {
		t.Errorf("expected baseURL %q, got %q", DefaultDeezerBaseURL, client.baseURL)
	}

	if client.userAgent != DefaultDeezerUserAgent {
		t.Errorf("expected userAgent %q, got %q", DefaultDeezerUserAgent, client.userAgent)
	}

	if client.httpClient == nil {
		t.Error("expected httpClient to be initialized")
	}

	if client.limiter == nil {
		t.Error("expected limiter to be initialized")
	}
}
