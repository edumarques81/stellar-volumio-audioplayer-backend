package wikipedia

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_LookupAlbum_Hit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("expected User-Agent header to be set")
		}
		if !strings.Contains(r.URL.Path, "Kind_of_Blue") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title":   "Kind of Blue",
			"extract": "Kind of Blue is a studio album by American jazz trumpeter Miles Davis.",
			"content_urls": map[string]any{
				"desktop": map[string]string{"page": "https://en.wikipedia.org/wiki/Kind_of_Blue"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	got, err := c.LookupAlbum(context.Background(), "Miles Davis", "Kind of Blue")
	if err != nil {
		t.Fatalf("LookupAlbum: %v", err)
	}
	if !strings.Contains(got.Extract, "studio album") {
		t.Fatalf("unexpected extract: %q", got.Extract)
	}
	if !strings.HasPrefix(got.SourceURL, "https://en.wikipedia.org/wiki/") {
		t.Fatalf("unexpected source URL: %q", got.SourceURL)
	}
	if got.Kind != "album" {
		t.Fatalf("expected Kind=album, got %q", got.Kind)
	}
}

func TestClient_LookupAlbum_404_FallsBackToArtist(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		// Album lookup misses; artist hits
		if strings.Contains(r.URL.Path, "Miles_Davis") && !strings.Contains(r.URL.Path, "album") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title":   "Miles Davis",
				"extract": "American jazz trumpeter, bandleader, and composer.",
				"content_urls": map[string]any{
					"desktop": map[string]string{"page": "https://en.wikipedia.org/wiki/Miles_Davis"},
				},
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	got, err := c.LookupAlbumOrArtist(context.Background(), "Miles Davis", "Some Obscure Album")
	if err != nil {
		t.Fatalf("LookupAlbumOrArtist: %v", err)
	}
	if got.Kind != "artist" {
		t.Fatalf("expected fallback to artist, got Kind=%q", got.Kind)
	}
	if !strings.Contains(got.Extract, "trumpeter") {
		t.Fatalf("unexpected fallback extract: %q", got.Extract)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected at least 2 calls (album then artist), got %d", calls)
	}
}

func TestClient_LookupAlbumOrArtist_AllMiss_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.LookupAlbumOrArtist(context.Background(), "Nobody", "Nothing")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestClient_Timeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 30 * time.Millisecond})
	_, err := c.LookupAlbum(context.Background(), "X", "Y")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

func TestClient_LookupAlbum_TitleEncoding(t *testing.T) {
	t.Parallel()
	var seenPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.URL.Path)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, _ = c.LookupAlbum(context.Background(), "Sigur Rós", "Ágætis byrjun")
	if len(seenPaths) == 0 {
		t.Fatalf("no requests reached server")
	}
	// Verify spaces have been replaced with underscores and unicode is percent-encoded
	for _, p := range seenPaths {
		if strings.Contains(p, " ") {
			t.Fatalf("path contains literal space: %q", p)
		}
	}
}

func TestClient_HTTP500_PropagatesError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.LookupAlbum(context.Background(), "X", "Y")
	if err == nil {
		t.Fatalf("expected error on 500")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("500 should not be ErrNotFound: %v", err)
	}
}

// --- 999.3 defect 3: confident wrong bios -----------------------------------
//
// Fixtures below are the REAL en.wikipedia.org REST summary payloads captured
// 2026-08-12 for the reported case (toe / "The Future Is Now") and its
// controls. The bug: "The Future Is Now (toe album)" 404s, so the bare-title
// fallback returned Non Phixion's album page as a confident album match and
// the LCD displayed a different band's history under toe's album.

// wikiFixture is one canned page keyed by the exact URL-encoded title that
// selects it (spaces as underscores, e.g. "The_Future_Is_Now").
type wikiFixture struct {
	match       string // exact request path segment that selects this page
	title       string
	description string
	extract     string
	pageType    string // "standard" | "disambiguation"; empty means standard
}

// newFixtureServer serves the fixture whose match equals the requested title
// and 404s anything else — mirroring Wikipedia, where a title either exists or
// does not. Matching must be EXACT, not substring: "The Future Is Now
// (toe album)" contains "The_Future_Is_Now", and a substring match would serve
// Non Phixion's page for the disambiguated title that really 404s, hiding the
// very bug these tests pin down.
func newFixtureServer(t *testing.T, fixtures []wikiFixture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := strings.TrimPrefix(r.URL.Path, "/")
		for _, f := range fixtures {
			if requested != f.match {
				continue
			}
			pt := f.pageType
			if pt == "" {
				pt = "standard"
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":        pt,
				"title":       f.title,
				"description": f.description,
				"extract":     f.extract,
				"content_urls": map[string]any{
					"desktop": map[string]string{
						"page": "https://en.wikipedia.org/wiki/" + f.title,
					},
				},
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const (
	nonPhixionExtract = "The Future Is Now is the only studio album by the American hip-hop " +
		"group Non Phixion. It was released on March 26, 2002, via Uncle Howie/Landspeed Records."
	toeBandExtract = "Toe , stylized as toe, is a Japanese post-rock/math rock band from Tokyo. " +
		"toe stands for \"theory of everything\"."
	toeAnatomyExtract = "Toes are the digits of the foot of a tetrapod. Animal species such as " +
		"cats that walk on their toes are described as being digitigrade."
	kindOfBlueExtract = "Kind of Blue is a studio album by American jazz musician Miles Davis, " +
		"released on August 17, 1959, by Columbia Records."
)

func TestClient_LookupAlbum_RejectsBareTitleHitFromAnotherArtist(t *testing.T) {
	t.Parallel()
	// Only the bare title exists — exactly the live situation for toe.
	srv := newFixtureServer(t, []wikiFixture{
		{match: "The_Future_Is_Now", title: "The Future Is Now",
			description: "2002 studio album by Non Phixion", extract: nonPhixionExtract},
	})

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	_, err := c.LookupAlbum(context.Background(), "toe", "The Future Is Now")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unrelated artist's album page, got err=%v", err)
	}
}

func TestClient_LookupAlbum_AcceptsBareTitleHitMentioningArtist(t *testing.T) {
	t.Parallel()
	srv := newFixtureServer(t, []wikiFixture{
		{match: "Kind_of_Blue", title: "Kind of Blue",
			description: "1959 studio album by Miles Davis", extract: kindOfBlueExtract},
	})

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	got, err := c.LookupAlbum(context.Background(), "Miles Davis", "Kind of Blue")
	if err != nil {
		t.Fatalf("LookupAlbum: %v", err)
	}
	if got.Kind != "album" {
		t.Fatalf("expected Kind=album, got %q", got.Kind)
	}
}

func TestClient_LookupAlbum_AcceptsDisambiguatedTitleWithoutArtistMention(t *testing.T) {
	t.Parallel()
	// "<album> (<artist> album)" encodes the artist in the title itself, so it
	// is trusted even when the extract never repeats the artist's name.
	srv := newFixtureServer(t, []wikiFixture{
		{match: "Untitled_(Some_Artist_album)", title: "Untitled (Some Artist album)",
			description: "2019 studio album", extract: "It was recorded over two weeks."},
	})

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	got, err := c.LookupAlbum(context.Background(), "Some Artist", "Untitled")
	if err != nil {
		t.Fatalf("LookupAlbum: %v", err)
	}
	if got.Kind != "album" {
		t.Fatalf("expected Kind=album, got %q", got.Kind)
	}
}

func TestClient_RejectsDisambiguationPage(t *testing.T) {
	t.Parallel()
	// "Miles Ahead" is a real disambiguation page; its extract mentions Miles
	// Davis, so an artist-name check alone would wrongly accept it.
	srv := newFixtureServer(t, []wikiFixture{
		{match: "Miles_Ahead", title: "Miles Ahead", pageType: "disambiguation",
			description: "Topics referred to by the same term",
			extract:     "Miles Ahead may refer to:Miles Ahead (album), 1957 album by Miles Davis"},
	})

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	_, err := c.LookupAlbum(context.Background(), "Miles Davis", "Miles Ahead")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a disambiguation page, got err=%v", err)
	}
}

func TestClient_LookupArtist_RejectsNonMusicBarePageAndFallsBackToBand(t *testing.T) {
	t.Parallel()
	// Bare "toe" is the anatomy article. The band lives at "Toe (band)".
	srv := newFixtureServer(t, []wikiFixture{
		{match: "toe_(band)", title: "Toe (band)",
			description: "Japanese rock band", extract: toeBandExtract},
		{match: "toe", title: "Toe",
			description: "Digit of a foot", extract: toeAnatomyExtract},
	})

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	got, err := c.LookupArtist(context.Background(), "toe")
	if err != nil {
		t.Fatalf("LookupArtist: %v", err)
	}
	if got.Title != "Toe (band)" {
		t.Fatalf("expected the band page, got %q (extract: %q)", got.Title, got.Extract)
	}
	if got.Kind != "artist" {
		t.Fatalf("expected Kind=artist, got %q", got.Kind)
	}
}

func TestClient_LookupArtist_AcceptsMusicRelevantBarePage(t *testing.T) {
	t.Parallel()
	srv := newFixtureServer(t, []wikiFixture{
		{match: "Miles_Davis", title: "Miles Davis",
			description: "American jazz musician (1926–1991)",
			extract:     "Miles Dewey Davis III was an American trumpeter, bandleader, composer, and painter."},
	})

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	got, err := c.LookupArtist(context.Background(), "Miles Davis")
	if err != nil {
		t.Fatalf("LookupArtist: %v", err)
	}
	if got.Title != "Miles Davis" {
		t.Fatalf("expected the bare artist page, got %q", got.Title)
	}
}

func TestClient_LookupAlbumOrArtist_ToeEndToEnd(t *testing.T) {
	t.Parallel()
	// The full reported scenario: album page belongs to another band, bare
	// artist page is anatomy, correct page is "Toe (band)".
	srv := newFixtureServer(t, []wikiFixture{
		{match: "The_Future_Is_Now", title: "The Future Is Now",
			description: "2002 studio album by Non Phixion", extract: nonPhixionExtract},
		{match: "toe_(band)", title: "Toe (band)",
			description: "Japanese rock band", extract: toeBandExtract},
		{match: "toe", title: "Toe",
			description: "Digit of a foot", extract: toeAnatomyExtract},
	})

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	got, err := c.LookupAlbumOrArtist(context.Background(), "toe", "The Future Is Now")
	if err != nil {
		t.Fatalf("LookupAlbumOrArtist: %v", err)
	}
	if strings.Contains(got.Extract, "Non Phixion") {
		t.Fatalf("returned another band's album bio: %q", got.Extract)
	}
	if strings.Contains(got.Extract, "tetrapod") {
		t.Fatalf("returned the anatomy article: %q", got.Extract)
	}
	if got.Title != "Toe (band)" {
		t.Fatalf("expected the band page, got %q", got.Title)
	}
}

func TestClient_LookupArtist_FallsBackToCollapsedPrimaryArtist(t *testing.T) {
	t.Parallel()
	// Real tag from the library: the "Miles Ahead" album is credited to
	// "Miles Davis - Arranged and Directed by Gil Evans", which has no
	// Wikipedia page; the collapsed primary artist does.
	srv := newFixtureServer(t, []wikiFixture{
		{match: "Miles_Davis", title: "Miles Davis",
			description: "American jazz musician (1926–1991)",
			extract:     "Miles Dewey Davis III was an American trumpeter, bandleader, composer, and painter."},
	})

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	got, err := c.LookupArtist(context.Background(), "Miles Davis - Arranged and Directed by Gil Evans")
	if err != nil {
		t.Fatalf("LookupArtist: %v", err)
	}
	if got.Title != "Miles Davis" {
		t.Fatalf("expected the collapsed artist's page, got %q", got.Title)
	}
}

func TestClient_LookupArtist_SingleRequestWhenCollapseIsANoop(t *testing.T) {
	t.Parallel()
	// Guard the request budget: an artist name that needs no collapsing must
	// not gain speculative extra lookups.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "standard", "title": "Radiohead",
			"description": "English rock band",
			"extract":     "Radiohead are an English rock band formed in Abingdon.",
		})
	}))
	t.Cleanup(srv.Close)

	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), Timeout: 2 * time.Second})
	if _, err := c.LookupArtist(context.Background(), "Radiohead"); err != nil {
		t.Fatalf("LookupArtist: %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("expected exactly 1 request for a musical bare page, got %d", n)
	}
}
