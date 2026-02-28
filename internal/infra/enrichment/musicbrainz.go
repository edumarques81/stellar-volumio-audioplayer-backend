// Package enrichment provides web metadata enrichment services for artwork.
package enrichment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
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

	// fuzzyScoreThreshold is the minimum score for fuzzy fallback matches
	fuzzyScoreThreshold = 85
)

// Pre-compiled regexps for NormalizeForSearch
var (
	reParenSuffix  = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)
	reSpecialChars = regexp.MustCompile(`[^\p{L}\p{N}\s]`)
	reMultiSpace   = regexp.MustCompile(`\s+`)
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

// SearchRelease searches for a release by artist and album name using
// a multi-strategy approach with fallback:
//  1. Exact quoted query: artist:"Pink Floyd" AND release:"The Wall"
//  2. Unquoted words: artist:Pink AND Floyd AND release:The AND Wall
//  3. Artist-only search, pick highest score release matching album (score >= 85)
func (c *MusicBrainzClient) SearchRelease(ctx context.Context, artist, album string) (string, error) {
	normalizedArtist := NormalizeForSearch(artist)
	normalizedAlbum := NormalizeForSearch(album)

	// Strategy 1: Exact quoted query
	query := fmt.Sprintf(`artist:"%s" AND release:"%s"`, escapeQuery(artist), escapeQuery(album))
	mbid, err := c.searchReleaseWithQuery(ctx, query, artist, album, "exact")
	if err != nil {
		return "", err
	}
	if mbid != "" {
		return mbid, nil
	}

	// Strategy 2: Unquoted words with AND
	if normalizedArtist != "" && normalizedAlbum != "" {
		query = buildUnquotedReleaseQuery(normalizedArtist, normalizedAlbum)
		mbid, err = c.searchReleaseWithQuery(ctx, query, artist, album, "unquoted")
		if err != nil {
			return "", err
		}
		if mbid != "" {
			return mbid, nil
		}
	}

	// Strategy 3: Artist-only search, pick best release matching album name
	if normalizedArtist != "" {
		query = buildArtistOnlyReleaseQuery(normalizedArtist)
		mbid, err = c.searchReleaseArtistOnly(ctx, query, normalizedAlbum, artist, album)
		if err != nil {
			return "", err
		}
		if mbid != "" {
			return mbid, nil
		}
	}

	log.Debug().
		Str("artist", artist).
		Str("album", album).
		Msg("All MusicBrainz release search strategies exhausted")
	return "", nil
}

// SearchArtist searches for an artist by name using a multi-strategy approach:
//  1. Exact quoted query: artist:"Pink Floyd"
//  2. Unquoted words: artist:Pink AND Floyd
//  3. Single longest word search
func (c *MusicBrainzClient) SearchArtist(ctx context.Context, artistName string) (string, error) {
	normalized := NormalizeForSearch(artistName)

	// Strategy 1: Exact quoted query
	query := fmt.Sprintf(`artist:"%s"`, escapeQuery(artistName))
	mbid, err := c.searchArtistWithQuery(ctx, query, artistName, "exact")
	if err != nil {
		return "", err
	}
	if mbid != "" {
		return mbid, nil
	}

	// Strategy 2: Unquoted words with AND
	words := strings.Fields(normalized)
	if len(words) > 1 {
		parts := make([]string, len(words))
		for i, w := range words {
			parts[i] = "artist:" + w
		}
		query = strings.Join(parts, " AND ")
		mbid, err = c.searchArtistWithQuery(ctx, query, artistName, "unquoted")
		if err != nil {
			return "", err
		}
		if mbid != "" {
			return mbid, nil
		}
	}

	// Strategy 3: Single longest word
	longest := LongestWord(normalized)
	if longest != "" && longest != normalized {
		query = "artist:" + longest
		mbid, err = c.searchArtistWithQuery(ctx, query, artistName, "longest-word")
		if err != nil {
			return "", err
		}
		if mbid != "" {
			return mbid, nil
		}
	}

	log.Debug().
		Str("artistName", artistName).
		Msg("All MusicBrainz artist search strategies exhausted")
	return "", nil
}

// searchReleaseWithQuery executes a release search with the given Lucene query.
func (c *MusicBrainzClient) searchReleaseWithQuery(ctx context.Context, query, artist, album, strategy string) (string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}

	reqURL := fmt.Sprintf("%s/release?query=%s&fmt=json&limit=5",
		c.baseURL, url.QueryEscape(query))

	log.Debug().
		Str("artist", artist).
		Str("album", album).
		Str("strategy", strategy).
		Msg("Searching MusicBrainz for release")

	resp, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return "", err
	}

	var searchResp MBSearchResponse
	if err := json.Unmarshal(resp, &searchResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(searchResp.Releases) == 0 {
		log.Debug().
			Str("artist", artist).
			Str("album", album).
			Str("strategy", strategy).
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
				Str("strategy", strategy).
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
			Str("strategy", strategy).
			Msg("Found MusicBrainz release (lower confidence)")
		return release.ID, nil
	}

	return "", nil
}

// searchReleaseArtistOnly searches by artist only and picks the best release
// that fuzzy-matches the album name with score >= fuzzyScoreThreshold.
func (c *MusicBrainzClient) searchReleaseArtistOnly(ctx context.Context, query, normalizedAlbum, artist, album string) (string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}

	reqURL := fmt.Sprintf("%s/release?query=%s&fmt=json&limit=10",
		c.baseURL, url.QueryEscape(query))

	log.Debug().
		Str("artist", artist).
		Str("album", album).
		Str("strategy", "artist-only").
		Msg("Searching MusicBrainz releases by artist only")

	resp, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return "", err
	}

	var searchResp MBSearchResponse
	if err := json.Unmarshal(resp, &searchResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	// Find best release whose title matches the album
	for _, release := range searchResp.Releases {
		if release.Score < fuzzyScoreThreshold {
			continue
		}
		normalizedTitle := NormalizeForSearch(release.Title)
		if normalizedTitle == normalizedAlbum || strings.Contains(normalizedTitle, normalizedAlbum) || strings.Contains(normalizedAlbum, normalizedTitle) {
			log.Debug().
				Str("artist", artist).
				Str("album", album).
				Str("matchedTitle", release.Title).
				Str("mbid", release.ID).
				Int("score", release.Score).
				Msg("Found MusicBrainz release via artist-only search")
			return release.ID, nil
		}
	}

	return "", nil
}

// searchArtistWithQuery executes an artist search with the given Lucene query.
func (c *MusicBrainzClient) searchArtistWithQuery(ctx context.Context, query, artistName, strategy string) (string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}

	reqURL := fmt.Sprintf("%s/artist?query=%s&fmt=json&limit=5",
		c.baseURL, url.QueryEscape(query))

	log.Debug().
		Str("artistName", artistName).
		Str("strategy", strategy).
		Msg("Searching MusicBrainz for artist")

	resp, err := c.doRequest(ctx, reqURL)
	if err != nil {
		return "", err
	}

	var searchResp MBArtistSearchResponse
	if err := json.Unmarshal(resp, &searchResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(searchResp.Artists) == 0 {
		log.Debug().
			Str("artistName", artistName).
			Str("strategy", strategy).
			Msg("No MusicBrainz artists found")
		return "", nil
	}

	// For fuzzy strategies (non-exact), require higher confidence
	minScore := 50
	if strategy != "exact" {
		minScore = fuzzyScoreThreshold
	}

	// Return the highest-scored artist (first result with score >= 80)
	for _, artist := range searchResp.Artists {
		if artist.Score >= 80 {
			log.Debug().
				Str("artistName", artistName).
				Str("mbid", artist.ID).
				Int("score", artist.Score).
				Str("strategy", strategy).
				Msg("Found MusicBrainz artist")
			return artist.ID, nil
		}
	}

	// If no high-confidence match, return first result if score > minScore
	if searchResp.Artists[0].Score > minScore {
		artist := searchResp.Artists[0]
		log.Debug().
			Str("artistName", artistName).
			Str("mbid", artist.ID).
			Int("score", artist.Score).
			Str("strategy", strategy).
			Msg("Found MusicBrainz artist (lower confidence)")
		return artist.ID, nil
	}

	return "", nil
}

// doRequest executes an HTTP request and returns the response body.
// Handles status codes for rate limiting and temporary failures.
func (c *MusicBrainzClient) doRequest(ctx context.Context, reqURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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

	switch resp.StatusCode {
	case http.StatusOK:
		// Success
	case http.StatusTooManyRequests:
		log.Warn().Msg("MusicBrainz rate limit exceeded")
		return nil, ErrRateLimited
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		log.Warn().Int("status", resp.StatusCode).Msg("MusicBrainz temporary error")
		return nil, ErrTemporaryFailure
	default:
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return body, nil
}

// NormalizeForSearch prepares a string for fuzzy search by lowercasing,
// removing parenthetical/bracket suffixes like "(Remaster)", "(Deluxe Edition)",
// "[2011 Remaster]", and stripping special characters.
func NormalizeForSearch(s string) string {
	s = strings.ToLower(s)
	s = reParenSuffix.ReplaceAllString(s, "")
	s = reSpecialChars.ReplaceAllString(s, " ")
	s = reMultiSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// buildUnquotedReleaseQuery builds a Lucene query with unquoted words joined by AND.
// e.g. "pink floyd", "the wall" → "artist:pink AND artist:floyd AND release:the AND release:wall"
func buildUnquotedReleaseQuery(normalizedArtist, normalizedAlbum string) string {
	var parts []string
	for _, w := range strings.Fields(normalizedArtist) {
		parts = append(parts, "artist:"+w)
	}
	for _, w := range strings.Fields(normalizedAlbum) {
		parts = append(parts, "release:"+w)
	}
	return strings.Join(parts, " AND ")
}

// buildArtistOnlyReleaseQuery builds a release query using only the artist name.
func buildArtistOnlyReleaseQuery(normalizedArtist string) string {
	words := strings.Fields(normalizedArtist)
	parts := make([]string, len(words))
	for i, w := range words {
		parts[i] = "artist:" + w
	}
	return strings.Join(parts, " AND ")
}

// LongestWord returns the longest word from a space-separated string.
func LongestWord(s string) string {
	longest := ""
	for _, w := range strings.Fields(s) {
		if len(w) > len(longest) {
			longest = w
		}
	}
	return longest
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

