package enrichment

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// DefaultCAABaseURL is the Cover Art Archive API base URL
	DefaultCAABaseURL = "https://coverartarchive.org"

	// DefaultUserAgent follows MusicBrainz guidelines
	DefaultUserAgent = "Stellar/1.4.0 (https://github.com/edumarques81/stellar-volumio-audioplayer-backend)"

	// DefaultRateLimit is 1 request per second (MusicBrainz guideline)
	DefaultRateLimit = 1

	// DefaultTimeout for HTTP requests
	DefaultTimeout = 30 * time.Second

	// MaxImageSize is the maximum image size to download (10MB)
	MaxImageSize = 10 * 1024 * 1024
)

// CAAClient is a client for the Cover Art Archive API
type CAAClient struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
	rateLimit  int
	limiter    *rateLimiter
}

// CAAOption is a functional option for configuring the CAA client
type CAAOption func(*CAAClient)

// WithBaseURL sets a custom base URL (useful for testing)
func WithBaseURL(url string) CAAOption {
	return func(c *CAAClient) {
		c.baseURL = url
	}
}

// WithUserAgent sets a custom User-Agent header
func WithUserAgent(ua string) CAAOption {
	return func(c *CAAClient) {
		c.userAgent = ua
	}
}

// WithRateLimit sets the rate limit in requests per second
func WithRateLimit(rps int) CAAOption {
	return func(c *CAAClient) {
		c.rateLimit = rps
		c.limiter = newRateLimiter(rps)
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) CAAOption {
	return func(c *CAAClient) {
		c.httpClient = client
	}
}

// NewCAAClient creates a new Cover Art Archive client
func NewCAAClient(opts ...CAAOption) *CAAClient {
	c := &CAAClient{
		baseURL:   DefaultCAABaseURL,
		userAgent: DefaultUserAgent,
		rateLimit: DefaultRateLimit,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	// Initialize rate limiter if not already set
	if c.limiter == nil {
		c.limiter = newRateLimiter(c.rateLimit)
	}

	return c
}

// FetchAlbumArt fetches album artwork from the Cover Art Archive
func (c *CAAClient) FetchAlbumArt(ctx context.Context, mbid string) (*FetchResult, error) {
	// Wait for rate limiter
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	// Build request URL: /release/{mbid}/front
	url := fmt.Sprintf("%s/release/%s/front", c.baseURL, mbid)

	log.Debug().
		Str("mbid", mbid).
		Str("url", url).
		Msg("Fetching album art from CAA")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "image/*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Handle response status codes
	switch resp.StatusCode {
	case http.StatusOK:
		// Success - read the image
	case http.StatusNotFound:
		log.Debug().Str("mbid", mbid).Msg("Album art not found in CAA")
		return nil, ErrArtworkNotFound
	case http.StatusTooManyRequests:
		log.Warn().Str("mbid", mbid).Msg("CAA rate limit exceeded")
		return nil, ErrRateLimited
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		log.Warn().Str("mbid", mbid).Int("status", resp.StatusCode).Msg("CAA temporary error")
		return nil, ErrTemporaryFailure
	default:
		log.Warn().Str("mbid", mbid).Int("status", resp.StatusCode).Msg("CAA unexpected status")
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Read response body with size limit
	limitedReader := io.LimitReader(resp.Body, MaxImageSize)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
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
		Msg("Successfully fetched album art from CAA")

	return &FetchResult{
		Data:     data,
		MimeType: contentType,
		Source:   SourceCoverArtArchive,
	}, nil
}

// detectMimeType detects the MIME type from image data
func detectMimeType(data []byte) string {
	if len(data) < 4 {
		return "application/octet-stream"
	}

	// Check magic bytes
	switch {
	case data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47:
		return "image/png"
	case data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46:
		return "image/gif"
	case data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46:
		// RIFF header - could be WebP
		if len(data) >= 12 && data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50 {
			return "image/webp"
		}
	}

	return "application/octet-stream"
}

// rateLimiter implements a simple token bucket rate limiter
type rateLimiter struct {
	mu          sync.Mutex
	interval    time.Duration
	lastRequest time.Time
}

func newRateLimiter(requestsPerSecond int) *rateLimiter {
	interval := time.Second / time.Duration(requestsPerSecond)
	return &rateLimiter{
		interval: interval,
	}
}

// Wait blocks until a request can be made
func (r *rateLimiter) Wait(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	nextAllowed := r.lastRequest.Add(r.interval)

	if now.Before(nextAllowed) {
		waitTime := nextAllowed.Sub(now)
		select {
		case <-time.After(waitTime):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	r.lastRequest = time.Now()
	return nil
}
