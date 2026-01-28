// Package enrichment provides web metadata enrichment services for artwork.
package enrichment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// DefaultFanartBaseURL is the Fanart.tv API base URL
	DefaultFanartBaseURL = "https://webservice.fanart.tv/v3/music"

	// DefaultFanartUserAgent follows API guidelines
	DefaultFanartUserAgent = "Stellar/1.5.0 (https://github.com/edumarques81/stellar-volumio-audioplayer-backend)"

	// DefaultFanartTimeout for HTTP requests
	DefaultFanartTimeout = 30 * time.Second
)

// FanartClient fetches artist images from Fanart.tv API.
type FanartClient struct {
	baseURL    string
	apiKey     string
	userAgent  string
	httpClient *http.Client
	limiter    *rateLimiter
}

// FanartOption is a functional option for configuring the Fanart.tv client.
type FanartOption func(*FanartClient)

// WithFanartAPIKey sets the API key.
func WithFanartAPIKey(key string) FanartOption {
	return func(c *FanartClient) {
		c.apiKey = key
	}
}

// WithFanartBaseURL sets a custom base URL (useful for testing).
func WithFanartBaseURL(url string) FanartOption {
	return func(c *FanartClient) {
		c.baseURL = url
	}
}

// WithFanartUserAgent sets a custom User-Agent header.
func WithFanartUserAgent(ua string) FanartOption {
	return func(c *FanartClient) {
		c.userAgent = ua
	}
}

// WithFanartHTTPClient sets a custom HTTP client.
func WithFanartHTTPClient(client *http.Client) FanartOption {
	return func(c *FanartClient) {
		c.httpClient = client
	}
}

// NewFanartClient creates a new Fanart.tv client.
// Requires API key (free registration at fanart.tv).
func NewFanartClient(apiKey string, opts ...FanartOption) *FanartClient {
	c := &FanartClient{
		baseURL:   DefaultFanartBaseURL,
		apiKey:    apiKey,
		userAgent: DefaultFanartUserAgent,
		httpClient: &http.Client{
			Timeout: DefaultFanartTimeout,
		},
		limiter: newRateLimiter(1), // 1 request per second to be safe
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// FanartArtistResponse represents the Fanart.tv artist response.
type FanartArtistResponse struct {
	Name        string        `json:"name"`
	MBID        string        `json:"mbid_id"`
	ArtistThumb []FanartImage `json:"artistthumb"`      // Artist photos (square)
	ArtistBG    []FanartImage `json:"artistbackground"` // Backgrounds (wide)
	MusicLogo   []FanartImage `json:"musiclogo"`        // Logos (transparent)
	HDMusicLogo []FanartImage `json:"hdmusiclogo"`      // HD Logos
	MusicBanner []FanartImage `json:"musicbanner"`      // Banners
}

// FanartImage represents an image from Fanart.tv.
type FanartImage struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Likes string `json:"likes"`
}

// getLikes returns the likes count as an integer.
func (i FanartImage) getLikes() int {
	likes, _ := strconv.Atoi(i.Likes)
	return likes
}

// FetchArtistImage fetches artist image from Fanart.tv by MBID.
// Returns the best artistthumb image data and metadata.
func (c *FanartClient) FetchArtistImage(ctx context.Context, mbid string) (*FetchResult, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("fanart.tv API key not configured")
	}

	// Wait for rate limiter
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	// Build request URL
	url := fmt.Sprintf("%s/%s?api_key=%s", c.baseURL, mbid, c.apiKey)

	log.Debug().
		Str("mbid", mbid).
		Msg("Fetching artist image from Fanart.tv")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Handle response status codes
	switch resp.StatusCode {
	case http.StatusOK:
		// Success - parse response
	case http.StatusNotFound:
		log.Debug().Str("mbid", mbid).Msg("Artist not found in Fanart.tv")
		return nil, ErrArtworkNotFound
	case http.StatusTooManyRequests:
		log.Warn().Str("mbid", mbid).Msg("Fanart.tv rate limit exceeded")
		return nil, ErrRateLimited
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		log.Warn().Str("mbid", mbid).Int("status", resp.StatusCode).Msg("Fanart.tv temporary error")
		return nil, ErrTemporaryFailure
	default:
		log.Warn().Str("mbid", mbid).Int("status", resp.StatusCode).Msg("Fanart.tv unexpected status")
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Parse JSON response
	var artistResp FanartArtistResponse
	if err := json.Unmarshal(body, &artistResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Check if artistthumb images are available
	if len(artistResp.ArtistThumb) == 0 {
		log.Debug().Str("mbid", mbid).Msg("No artistthumb images in Fanart.tv response")
		return nil, ErrArtworkNotFound
	}

	// Sort by likes (descending) and get the best one
	images := artistResp.ArtistThumb
	sort.Slice(images, func(i, j int) bool {
		return images[i].getLikes() > images[j].getLikes()
	})

	bestImage := images[0]

	// Download the image
	return c.downloadImage(ctx, bestImage.URL, mbid)
}

// downloadImage downloads an image from the given URL.
func (c *FanartClient) downloadImage(ctx context.Context, imageURL, mbid string) (*FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create image request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "image/*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// Read image data with size limit (10MB)
	limitedReader := io.LimitReader(resp.Body, MaxImageSize)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("read image data: %w", err)
	}

	if len(data) == 0 {
		return nil, ErrArtworkNotFound
	}

	// Get content type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = detectMimeType(data)
	}

	log.Debug().
		Str("mbid", mbid).
		Int("size", len(data)).
		Str("type", contentType).
		Msg("Successfully fetched artist image from Fanart.tv")

	return &FetchResult{
		Data:     data,
		MimeType: contentType,
		Source:   SourceFanartTV,
	}, nil
}

// IsConfigured returns true if the client has an API key configured.
func (c *FanartClient) IsConfigured() bool {
	return c.apiKey != ""
}
