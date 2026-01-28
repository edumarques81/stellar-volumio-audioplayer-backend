// Package enrichment provides web metadata enrichment services for artwork.
package enrichment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// DefaultDeezerBaseURL is the Deezer API base URL
	DefaultDeezerBaseURL = "https://api.deezer.com"

	// DefaultDeezerUserAgent follows API guidelines
	DefaultDeezerUserAgent = "Stellar/1.5.0 (https://github.com/edumarques81/stellar-volumio-audioplayer-backend)"

	// DefaultDeezerTimeout for HTTP requests
	DefaultDeezerTimeout = 30 * time.Second

	// DefaultDeezerRateLimit - Deezer allows higher rate but we stay conservative
	DefaultDeezerRateLimit = 5 // 5 requests per second
)

// DeezerClient searches for artist images via Deezer API.
// NOTE: Per Deezer ToS, images CANNOT be cached locally - must be hotlinked.
type DeezerClient struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
	limiter    *rateLimiter
}

// DeezerOption is a functional option for configuring the Deezer client.
type DeezerOption func(*DeezerClient)

// WithDeezerBaseURL sets a custom base URL (useful for testing).
func WithDeezerBaseURL(url string) DeezerOption {
	return func(c *DeezerClient) {
		c.baseURL = url
	}
}

// WithDeezerUserAgent sets a custom User-Agent header.
func WithDeezerUserAgent(ua string) DeezerOption {
	return func(c *DeezerClient) {
		c.userAgent = ua
	}
}

// WithDeezerHTTPClient sets a custom HTTP client.
func WithDeezerHTTPClient(client *http.Client) DeezerOption {
	return func(c *DeezerClient) {
		c.httpClient = client
	}
}

// NewDeezerClient creates a new Deezer API client.
// No API key required for public endpoints.
func NewDeezerClient(opts ...DeezerOption) *DeezerClient {
	c := &DeezerClient{
		baseURL:   DefaultDeezerBaseURL,
		userAgent: DefaultDeezerUserAgent,
		httpClient: &http.Client{
			Timeout: DefaultDeezerTimeout,
		},
		limiter: newRateLimiter(DefaultDeezerRateLimit),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// DeezerSearchResponse represents a Deezer artist search response.
type DeezerSearchResponse struct {
	Data  []DeezerArtist `json:"data"`
	Total int            `json:"total"`
}

// DeezerArtist represents an artist from Deezer API.
type DeezerArtist struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`        // Default URL
	PictureSmall  string `json:"picture_small"`  // 56x56
	PictureMedium string `json:"picture_medium"` // 250x250
	PictureBig    string `json:"picture_big"`    // 500x500
	PictureXL     string `json:"picture_xl"`     // 1000x1000
}

// SearchArtistImageURL searches Deezer for an artist and returns the image URL.
// NOTE: Per Deezer ToS, images CANNOT be cached - must be hotlinked.
// Returns the URL to be stored in the artist record for direct use.
func (c *DeezerClient) SearchArtistImageURL(ctx context.Context, artistName string) (string, error) {
	// Wait for rate limiter
	if err := c.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}

	// Build search URL
	searchURL := fmt.Sprintf("%s/search/artist?q=%s&limit=5",
		c.baseURL, url.QueryEscape(artistName))

	log.Debug().
		Str("artistName", artistName).
		Str("url", searchURL).
		Msg("Searching Deezer for artist image")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Handle response status codes
	switch resp.StatusCode {
	case http.StatusOK:
		// Success
	case http.StatusTooManyRequests:
		log.Warn().Str("artistName", artistName).Msg("Deezer rate limit exceeded")
		return "", ErrRateLimited
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		log.Warn().Int("status", resp.StatusCode).Msg("Deezer temporary error")
		return "", ErrTemporaryFailure
	default:
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	// Parse JSON response
	var searchResp DeezerSearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	// Find best matching artist (case-insensitive name match)
	normalizedSearch := strings.ToLower(artistName)
	for _, artist := range searchResp.Data {
		normalizedName := strings.ToLower(artist.Name)

		// Exact match or close match
		if normalizedName == normalizedSearch || strings.Contains(normalizedName, normalizedSearch) {
			if artist.PictureXL != "" {
				log.Debug().
					Str("artistName", artistName).
					Str("deezerName", artist.Name).
					Int("deezerID", artist.ID).
					Msg("Found artist image on Deezer")
				return artist.PictureXL, nil
			}
			// Fall back to smaller sizes
			if artist.PictureBig != "" {
				return artist.PictureBig, nil
			}
			if artist.PictureMedium != "" {
				return artist.PictureMedium, nil
			}
		}
	}

	// If no exact match, use first result if available
	if len(searchResp.Data) > 0 && searchResp.Data[0].PictureXL != "" {
		artist := searchResp.Data[0]
		log.Debug().
			Str("artistName", artistName).
			Str("deezerName", artist.Name).
			Int("deezerID", artist.ID).
			Msg("Using first Deezer result (no exact match)")
		return artist.PictureXL, nil
	}

	log.Debug().
		Str("artistName", artistName).
		Msg("No artist image found on Deezer")
	return "", ErrArtworkNotFound
}
