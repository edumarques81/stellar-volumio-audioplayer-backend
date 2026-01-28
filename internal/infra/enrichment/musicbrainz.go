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
	// DefaultMBBaseURL is the MusicBrainz API base URL
	DefaultMBBaseURL = "https://musicbrainz.org/ws/2"

	// DefaultMBUserAgent follows MusicBrainz guidelines
	DefaultMBUserAgent = "Stellar/1.4.0 (https://github.com/edumarques81/stellar-volumio-audioplayer-backend)"

	// DefaultMBRateLimit is 1 request per second (MusicBrainz guideline)
	DefaultMBRateLimit = 1

	// DefaultMBTimeout for HTTP requests
	DefaultMBTimeout = 30 * time.Second
)

// MusicBrainzClient searches for release MBIDs using the MusicBrainz API.
type MusicBrainzClient struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
	limiter    *rateLimiter
}

// MBOption is a functional option for configuring the MusicBrainz client.
type MBOption func(*MusicBrainzClient)

// WithMBBaseURL sets a custom base URL (useful for testing).
func WithMBBaseURL(url string) MBOption {
	return func(c *MusicBrainzClient) {
		c.baseURL = url
	}
}

// WithMBUserAgent sets a custom User-Agent header.
func WithMBUserAgent(ua string) MBOption {
	return func(c *MusicBrainzClient) {
		c.userAgent = ua
	}
}

// WithMBHTTPClient sets a custom HTTP client.
func WithMBHTTPClient(client *http.Client) MBOption {
	return func(c *MusicBrainzClient) {
		c.httpClient = client
	}
}

// NewMusicBrainzClient creates a new MusicBrainz API client.
func NewMusicBrainzClient(opts ...MBOption) *MusicBrainzClient {
	c := &MusicBrainzClient{
		baseURL:   DefaultMBBaseURL,
		userAgent: DefaultMBUserAgent,
		httpClient: &http.Client{
			Timeout: DefaultMBTimeout,
		},
		limiter: newRateLimiter(DefaultMBRateLimit),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// MBRelease represents a release from MusicBrainz API.
type MBRelease struct {
	ID     string `json:"id"`     // MusicBrainz Release ID (MBID)
	Title  string `json:"title"`  // Release title
	Score  int    `json:"score"`  // Search relevance score (0-100)
	Status string `json:"status"` // Release status (e.g., "Official")
}

// MBSearchResponse represents the MusicBrainz search API response.
type MBSearchResponse struct {
	Releases []MBRelease `json:"releases"`
	Count    int         `json:"count"`
	Offset   int         `json:"offset"`
}

// SearchRelease searches for a release by artist and album name.
// Returns the best matching release MBID or empty string if not found.
func (c *MusicBrainzClient) SearchRelease(ctx context.Context, artist, album string) (string, error) {
	// Wait for rate limiter
	if err := c.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}

	// Build search query using Lucene syntax
	// Query: artist:"Artist Name" AND release:"Album Name"
	query := fmt.Sprintf(`artist:"%s" AND release:"%s"`, escapeQuery(artist), escapeQuery(album))

	// Build URL
	reqURL := fmt.Sprintf("%s/release?query=%s&fmt=json&limit=5",
		c.baseURL, url.QueryEscape(query))

	log.Debug().
		Str("artist", artist).
		Str("album", album).
		Str("url", reqURL).
		Msg("Searching MusicBrainz for release")

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Handle response status
	switch resp.StatusCode {
	case http.StatusOK:
		// Success
	case http.StatusTooManyRequests:
		log.Warn().Msg("MusicBrainz rate limit exceeded")
		return "", ErrRateLimited
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		log.Warn().Int("status", resp.StatusCode).Msg("MusicBrainz temporary error")
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
	var searchResp MBSearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	// Find best match
	if len(searchResp.Releases) == 0 {
		log.Debug().
			Str("artist", artist).
			Str("album", album).
			Msg("No MusicBrainz releases found")
		return "", nil
	}

	// Return the highest-scored release (first result with score >= 80)
	for _, release := range searchResp.Releases {
		if release.Score >= 80 {
			log.Debug().
				Str("artist", artist).
				Str("album", album).
				Str("mbid", release.ID).
				Int("score", release.Score).
				Msg("Found MusicBrainz release")
			return release.ID, nil
		}
	}

	// If no high-confidence match, return first result if score > 50
	if searchResp.Releases[0].Score > 50 {
		release := searchResp.Releases[0]
		log.Debug().
			Str("artist", artist).
			Str("album", album).
			Str("mbid", release.ID).
			Int("score", release.Score).
			Msg("Found MusicBrainz release (lower confidence)")
		return release.ID, nil
	}

	log.Debug().
		Str("artist", artist).
		Str("album", album).
		Int("bestScore", searchResp.Releases[0].Score).
		Msg("MusicBrainz matches too low confidence")
	return "", nil
}

// MBArtist represents an artist from MusicBrainz API.
type MBArtist struct {
	ID    string `json:"id"`    // MusicBrainz Artist ID (MBID)
	Name  string `json:"name"`  // Artist name
	Score int    `json:"score"` // Search relevance score (0-100)
	Type  string `json:"type"`  // Artist type (Person, Group, etc.)
}

// MBArtistSearchResponse represents the MusicBrainz artist search API response.
type MBArtistSearchResponse struct {
	Artists []MBArtist `json:"artists"`
	Count   int        `json:"count"`
	Offset  int        `json:"offset"`
}

// SearchArtist searches for an artist by name.
// Returns the best matching artist MBID or empty string if not found.
func (c *MusicBrainzClient) SearchArtist(ctx context.Context, artistName string) (string, error) {
	// Wait for rate limiter
	if err := c.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}

	// Build search query using Lucene syntax
	query := fmt.Sprintf(`artist:"%s"`, escapeQuery(artistName))

	// Build URL
	reqURL := fmt.Sprintf("%s/artist?query=%s&fmt=json&limit=5",
		c.baseURL, url.QueryEscape(query))

	log.Debug().
		Str("artistName", artistName).
		Str("url", reqURL).
		Msg("Searching MusicBrainz for artist")

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Handle response status
	switch resp.StatusCode {
	case http.StatusOK:
		// Success
	case http.StatusTooManyRequests:
		log.Warn().Msg("MusicBrainz rate limit exceeded")
		return "", ErrRateLimited
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		log.Warn().Int("status", resp.StatusCode).Msg("MusicBrainz temporary error")
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
	var searchResp MBArtistSearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	// Find best match
	if len(searchResp.Artists) == 0 {
		log.Debug().
			Str("artistName", artistName).
			Msg("No MusicBrainz artists found")
		return "", nil
	}

	// Return the highest-scored artist (first result with score >= 80)
	for _, artist := range searchResp.Artists {
		if artist.Score >= 80 {
			log.Debug().
				Str("artistName", artistName).
				Str("mbid", artist.ID).
				Int("score", artist.Score).
				Msg("Found MusicBrainz artist")
			return artist.ID, nil
		}
	}

	// If no high-confidence match, return first result if score > 50
	if searchResp.Artists[0].Score > 50 {
		artist := searchResp.Artists[0]
		log.Debug().
			Str("artistName", artistName).
			Str("mbid", artist.ID).
			Int("score", artist.Score).
			Msg("Found MusicBrainz artist (lower confidence)")
		return artist.ID, nil
	}

	log.Debug().
		Str("artistName", artistName).
		Int("bestScore", searchResp.Artists[0].Score).
		Msg("MusicBrainz artist matches too low confidence")
	return "", nil
}

// escapeQuery escapes special characters in Lucene query.
func escapeQuery(s string) string {
	// Escape Lucene special characters: + - && || ! ( ) { } [ ] ^ " ~ * ? : \ /
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		`+`, `\+`,
		`-`, `\-`,
		`!`, `\!`,
		`(`, `\(`,
		`)`, `\)`,
		`{`, `\{`,
		`}`, `\}`,
		`[`, `\[`,
		`]`, `\]`,
		`^`, `\^`,
		`~`, `\~`,
		`*`, `\*`,
		`?`, `\?`,
		`:`, `\:`,
		`/`, `\/`,
	)
	return replacer.Replace(s)
}
