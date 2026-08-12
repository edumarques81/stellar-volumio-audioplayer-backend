package wikipedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/artistidentity"
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
//
// The bare title is a WEAK candidate: album titles are widely reused, so a
// hit proves only that *someone* released a record by that name. toe's
// "The Future Is Now" has no Wikipedia page, but Non Phixion's album of the
// same name does — and the LCD displayed that band's history under toe's
// album. A bare-title hit is therefore accepted only when the page actually
// corroborates the artist we asked about; otherwise it is reported as a miss
// so the caller can fall back, rather than shown as a confident wrong answer.
func (c *Client) LookupAlbum(ctx context.Context, artist, album string) (Result, error) {
	candidates := []struct {
		title string
		// requireArtist gates weak candidates whose title does not already
		// encode the artist.
		requireArtist bool
	}{
		{title: fmt.Sprintf("%s (%s album)", album, artist)},
		{title: album, requireArtist: true},
	}

	lastErr := ErrNotFound
	for _, cand := range candidates {
		r, err := c.fetch(ctx, cand.title)
		if err != nil {
			lastErr = err
			if !errors.Is(err, ErrNotFound) {
				// Real error (timeout, network, 5xx) — don't try further candidates.
				return Result{}, err
			}
			continue
		}
		if cand.requireArtist && !mentionsArtist(r, artist) {
			lastErr = ErrNotFound
			continue
		}
		r.Kind = "album"
		return r, nil
	}
	return Result{}, fmt.Errorf("LookupAlbum %q / %q: %w", artist, album, lastErr)
}

// LookupArtist returns the artist page summary, or a wrapped ErrNotFound.
//
// The bare artist name is tried first because it is correct for the great
// majority of artists and costs a single request. It is gated on the page
// looking musical at all: a short, lowercase band name collides with ordinary
// vocabulary, and bare "toe" returns the anatomy article ("Toes are the digits
// of the foot of a tetrapod"), which was being served as that band's bio. When
// the bare page is rejected, the standard Wikipedia disambiguators are tried;
// their titles encode the subject, so they are trusted without a gate.
func (c *Client) LookupArtist(ctx context.Context, artist string) (Result, error) {
	// A raw MPD Artist tag can carry credits Wikipedia has no page for --
	// "Miles Davis - Arranged and Directed by Gil Evans" is documented simply
	// as Miles Davis. Try the collapsed primary artist too, but only when it
	// actually differs, so the common case still costs a single request.
	names := []string{artist}
	if collapsed := artistidentity.Collapse(artist); collapsed != "" && collapsed != artist {
		names = append(names, collapsed)
	}

	type candidate struct {
		title          string
		requireMusical bool
	}
	// Bare names first (cheap and correct for most artists), then the
	// standard disambiguators, whose titles encode the subject.
	var candidates []candidate
	for _, n := range names {
		candidates = append(candidates, candidate{title: n, requireMusical: true})
	}
	for _, n := range names {
		candidates = append(candidates,
			candidate{title: fmt.Sprintf("%s (band)", n)},
			candidate{title: fmt.Sprintf("%s (musician)", n)})
	}

	lastErr := ErrNotFound
	for _, cand := range candidates {
		r, err := c.fetch(ctx, cand.title)
		if err != nil {
			lastErr = err
			if !errors.Is(err, ErrNotFound) {
				return Result{}, err
			}
			continue
		}
		if cand.requireMusical && !looksMusical(r) {
			lastErr = ErrNotFound
			continue
		}
		r.Kind = "artist"
		return r, nil
	}
	return Result{}, fmt.Errorf("LookupArtist %q: %w", artist, lastErr)
}

// musicalTerms are matched as plain substrings against a page's description
// and extract, so "band" also catches "bandleader" and "music" catches
// "musician"/"musical".
var musicalTerms = []string{
	"music", "band", "album", "singer", "song", "rapper", "composer",
	"guitar", "piano", "drummer", "bassist", "vocal", "orchestra",
	"conductor", "jazz", "rock", "hip hop", "hip-hop", "discography",
	"record label", "recording artist", "ensemble", "trumpet", "saxophon",
	"soprano", "tenor", "violin", "cello", "duo", "quartet", "quintet",
	"dj ", "producer",
}

// looksMusical reports whether a page plausibly describes a musical act.
// It is a rejection filter for weak candidates, not a classifier: a false
// negative costs one extra disambiguated request, while a false positive
// puts an unrelated article on screen as an artist bio.
func looksMusical(r Result) bool {
	hay := strings.ToLower(r.Description + " " + r.Extract)
	for _, term := range musicalTerms {
		if strings.Contains(hay, term) {
			return true
		}
	}
	return false
}

// mentionsArtist reports whether a page corroborates the artist we searched
// for, matching on word boundaries so a short name like "toe" does not match
// inside "together" or "toes". Both the raw tag value and its collapsed
// primary-artist form are accepted, since an album credited to
// "Miles Davis - Arranged and Directed by Gil Evans" is documented on
// Wikipedia simply as Miles Davis.
func mentionsArtist(r Result, artist string) bool {
	hay := r.Title + " " + r.Description + " " + r.Extract

	seen := make(map[string]struct{}, 2)
	for _, name := range []string{artist, artistidentity.Collapse(artist)} {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}

		re, err := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
		if err != nil {
			// Unreachable in practice (the name is quoted), but a compile
			// failure must not be read as corroboration.
			continue
		}
		if re.MatchString(hay) {
			return true
		}
	}
	return false
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
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
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
	// A disambiguation page ("Miles Ahead may refer to: ...") is a 200 with a
	// real extract, and it usually name-drops the very artist we searched for
	// — so it slips past an artist-mention check while carrying no bio at all.
	// Wikipedia labels these explicitly; treat them as a miss.
	if strings.EqualFold(strings.TrimSpace(raw.Type), "disambiguation") {
		return Result{}, ErrNotFound
	}
	return Result{
		Title:       raw.Title,
		Description: raw.Description,
		Extract:     raw.Extract,
		SourceURL:   raw.ContentURLs.Desktop.Page,
	}, nil
}
