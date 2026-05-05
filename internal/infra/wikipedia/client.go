package wikipedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://en.wikipedia.org/api/rest_v1/page/summary"
	defaultTimeout = 5 * time.Second
	userAgent      = "Stellar/1.0 (https://github.com/edumarques81/stellar-volumio-audioplayer-backend; bio cache)"
)

// Config configures a Client.
type Config struct {
	BaseURL    string        // override for tests; defaults to en.wikipedia.org REST endpoint
	HTTPClient *http.Client  // override for tests; defaults to a stdlib client
	Timeout    time.Duration // applied per request via context
}

// Client is a Wikipedia REST summary client.
type Client struct {
	baseURL string
	http    *http.Client
	timeout time.Duration
}

// NewClient builds a Client with sensible defaults.
func NewClient(cfg Config) *Client {
	c := &Client{
		baseURL: cfg.BaseURL,
		http:    cfg.HTTPClient,
		timeout: cfg.Timeout,
	}
	if c.baseURL == "" {
		c.baseURL = defaultBaseURL
	}
	if c.http == nil {
		c.http = &http.Client{}
	}
	if c.timeout == 0 {
		c.timeout = defaultTimeout
	}
	return c
}

// LookupAlbum tries the disambiguated title "Album (Artist album)" first,
// then the bare album title. Returns a wrapped ErrNotFound if both miss.
func (c *Client) LookupAlbum(ctx context.Context, artist, album string) (Result, error) {
	candidates := []string{
		fmt.Sprintf("%s (%s album)", album, artist),
		album,
	}
	var lastErr error
	for _, title := range candidates {
		r, err := c.fetch(ctx, title)
		if err == nil {
			r.Kind = "album"
			return r, nil
		}
		lastErr = err
		if !errors.Is(err, ErrNotFound) {
			// Real error (timeout, network, 5xx) — don't try further candidates.
			return Result{}, err
		}
	}
	if lastErr == nil {
		lastErr = ErrNotFound
	}
	return Result{}, fmt.Errorf("LookupAlbum %q / %q: %w", artist, album, lastErr)
}

// LookupArtist returns the artist page summary, or a wrapped ErrNotFound.
func (c *Client) LookupArtist(ctx context.Context, artist string) (Result, error) {
	r, err := c.fetch(ctx, artist)
	if err != nil {
		return Result{}, fmt.Errorf("LookupArtist %q: %w", artist, err)
	}
	r.Kind = "artist"
	return r, nil
}

// LookupAlbumOrArtist tries the album page; if missing, falls back to the
// artist page. Returns the first hit; wrapped ErrNotFound if both miss.
func (c *Client) LookupAlbumOrArtist(ctx context.Context, artist, album string) (Result, error) {
	if r, err := c.LookupAlbum(ctx, artist, album); err == nil {
		return r, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Result{}, err
	}
	return c.LookupArtist(ctx, artist)
}

func (c *Client) fetch(ctx context.Context, title string) (Result, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	// Wikipedia REST URLs use underscores for spaces and percent-encode the rest.
	encoded := url.PathEscape(strings.ReplaceAll(title, " ", "_"))
	u := c.baseURL + "/" + encoded

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("wikipedia GET %s: %w", title, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return Result{}, ErrNotFound
	}
	if resp.StatusCode >= 400 {
		return Result{}, fmt.Errorf("wikipedia GET %s: status %d", title, resp.StatusCode)
	}

	var raw struct {
		Title       string `json:"title"`
		Extract     string `json:"extract"`
		ContentURLs struct {
			Desktop struct {
				Page string `json:"page"`
			} `json:"desktop"`
		} `json:"content_urls"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Result{}, fmt.Errorf("decode wikipedia response for %q: %w", title, err)
	}
	if strings.TrimSpace(raw.Extract) == "" {
		// Some Wikipedia responses are 200 with empty extract (disambiguation pages, redirects).
		return Result{}, ErrNotFound
	}
	return Result{
		Title:     raw.Title,
		Extract:   raw.Extract,
		SourceURL: raw.ContentURLs.Desktop.Page,
	}, nil
}
