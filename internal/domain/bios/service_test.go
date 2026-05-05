package bios

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/llm"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/wikipedia"
)

func openTestDAO(t *testing.T) *cache.BiosDAO {
	t.Helper()
	dir := t.TempDir()
	db := cache.NewDB(filepath.Join(dir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db.BiosDAO()
}

// fakeWiki + fakeLLM let the service tests stay hermetic.
type fakeWiki struct {
	hits map[string]wikipedia.Result // key = artist|album|kind
}

func (f *fakeWiki) LookupAlbumOrArtist(_ context.Context, artist, album string) (wikipedia.Result, error) {
	if r, ok := f.hits[artist+"|"+album+"|album"]; ok {
		return r, nil
	}
	if r, ok := f.hits[artist+"||artist"]; ok {
		return r, nil
	}
	return wikipedia.Result{}, wikipedia.ErrNotFound
}

type fakeLLM struct {
	out string
	err error
	ins []string
}

func (f *fakeLLM) Summarize(_ context.Context, in string, _ llm.Options) (string, error) {
	f.ins = append(f.ins, in)
	if f.err != nil {
		return "", f.err
	}
	return f.out, nil
}

func TestService_GetAlbumBio_CacheMiss_FetchesAndCaches(t *testing.T) {
	t.Parallel()
	dao := openTestDAO(t)
	wiki := &fakeWiki{hits: map[string]wikipedia.Result{
		"Miles Davis|Kind of Blue|album": {
			Title:     "Kind of Blue",
			Kind:      "album",
			Extract:   "Recorded in 1959, considered influential in jazz.",
			SourceURL: "https://en.wikipedia.org/wiki/Kind_of_Blue",
		},
	}}
	ll := &fakeLLM{out: "Kind of Blue is a 1959 Miles Davis album. A pillar of modal jazz."}
	svc := NewService(dao, wiki, ll, Config{TTL: 90 * 24 * time.Hour})

	got, err := svc.GetAlbumBio(context.Background(), "Miles Davis", "Kind of Blue")
	if err != nil {
		t.Fatalf("GetAlbumBio: %v", err)
	}
	if got.Summary == "" {
		t.Fatalf("expected non-empty summary, got %q", got.Summary)
	}
	if got.Kind != "album" {
		t.Fatalf("expected Kind=album, got %q", got.Kind)
	}
	if len(ll.ins) != 1 {
		t.Fatalf("expected exactly 1 LLM call, got %d", len(ll.ins))
	}

	// Second call serves from cache (no extra LLM call).
	got2, err := svc.GetAlbumBio(context.Background(), "Miles Davis", "Kind of Blue")
	if err != nil {
		t.Fatalf("GetAlbumBio (cache): %v", err)
	}
	if got2.Summary != got.Summary {
		t.Fatalf("cache returned different summary")
	}
	if len(ll.ins) != 1 {
		t.Fatalf("cache hit should not trigger LLM; got %d total calls", len(ll.ins))
	}
}

func TestService_GetAlbumBio_FallsBackToArtist(t *testing.T) {
	t.Parallel()
	dao := openTestDAO(t)
	wiki := &fakeWiki{hits: map[string]wikipedia.Result{
		"Miles Davis||artist": {
			Title:     "Miles Davis",
			Kind:      "artist",
			Extract:   "American jazz trumpeter.",
			SourceURL: "https://en.wikipedia.org/wiki/Miles_Davis",
		},
	}}
	ll := &fakeLLM{out: "Miles Davis was a jazz trumpeter and bandleader."}
	svc := NewService(dao, wiki, ll, Config{TTL: time.Hour})

	got, err := svc.GetAlbumBio(context.Background(), "Miles Davis", "Some Obscure Album")
	if err != nil {
		t.Fatalf("GetAlbumBio: %v", err)
	}
	if got.Summary == "" || got.Kind != "artist" {
		t.Fatalf("expected artist fallback, got Kind=%q summary=%q", got.Kind, got.Summary)
	}
	// Subsequent call should hit the artist_bios cache, not re-call the LLM.
	if _, err := svc.GetAlbumBio(context.Background(), "Miles Davis", "Some Obscure Album"); err != nil {
		t.Fatal(err)
	}
	if len(ll.ins) != 1 {
		t.Fatalf("artist cache should serve subsequent calls; got %d LLM calls", len(ll.ins))
	}
}

func TestService_GetAlbumBio_AllMissReturnsEmpty(t *testing.T) {
	t.Parallel()
	dao := openTestDAO(t)
	wiki := &fakeWiki{hits: nil}
	ll := &fakeLLM{}
	svc := NewService(dao, wiki, ll, Config{TTL: time.Hour})

	got, err := svc.GetAlbumBio(context.Background(), "Nobody", "Nothing")
	if err != nil {
		t.Fatalf("GetAlbumBio should NOT error on misses: %v", err)
	}
	if got.Summary != "" {
		t.Fatalf("expected empty summary on full miss, got %q", got.Summary)
	}
	if got.Kind != "" {
		t.Fatalf("expected empty kind on full miss, got %q", got.Kind)
	}
}

func TestService_GetAlbumBio_LLMError_ReturnsEmptyAndDoesNotCache(t *testing.T) {
	t.Parallel()
	dao := openTestDAO(t)
	wiki := &fakeWiki{hits: map[string]wikipedia.Result{
		"X|Y|album": {Extract: "anything", Kind: "album"},
	}}
	ll := &fakeLLM{err: errors.New("rate limited")}
	svc := NewService(dao, wiki, ll, Config{TTL: time.Hour})

	got, _ := svc.GetAlbumBio(context.Background(), "X", "Y")
	if got.Summary != "" {
		t.Fatalf("LLM error should yield empty summary, got %q", got.Summary)
	}

	cached, ok, _ := dao.GetAlbumBio("X", "Y")
	if ok {
		t.Fatalf("LLM-failed bio must not be cached, found: %+v", cached)
	}
}

func TestService_RefreshAlbumBio_DeletesAndRefetches(t *testing.T) {
	t.Parallel()
	dao := openTestDAO(t)
	wiki := &fakeWiki{hits: map[string]wikipedia.Result{
		"X|Y|album": {Extract: "v2", Kind: "album"},
	}}
	ll := &fakeLLM{out: "v2 summary"}
	svc := NewService(dao, wiki, ll, Config{TTL: time.Hour})

	// Seed an old row directly into the DAO.
	if err := dao.PutAlbumBio(cache.AlbumBio{
		Artist: "X", Album: "Y", Summary: "v1 summary",
		FetchedAt: 0, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.RefreshAlbumBio(context.Background(), "X", "Y")
	if err != nil {
		t.Fatalf("RefreshAlbumBio: %v", err)
	}
	if got.Summary != "v2 summary" {
		t.Fatalf("expected refreshed summary, got %q", got.Summary)
	}
}

func TestService_GetAlbumBio_ExpiredCacheTriggersRefetch(t *testing.T) {
	t.Parallel()
	dao := openTestDAO(t)
	wiki := &fakeWiki{hits: map[string]wikipedia.Result{
		"X|Y|album": {Extract: "fresh extract", Kind: "album"},
	}}
	ll := &fakeLLM{out: "fresh summary"}
	svc := NewService(dao, wiki, ll, Config{TTL: time.Hour})

	// Seed an expired row.
	past := time.Now().Add(-2 * time.Hour).Unix()
	if err := dao.PutAlbumBio(cache.AlbumBio{
		Artist: "X", Album: "Y", Summary: "stale", FetchedAt: past, ExpiresAt: past,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.GetAlbumBio(context.Background(), "X", "Y")
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "fresh summary" {
		t.Fatalf("expired row should trigger refetch; got %q", got.Summary)
	}
}

func TestService_DefaultTTLAppliedWhenZero(t *testing.T) {
	t.Parallel()
	dao := openTestDAO(t)
	wiki := &fakeWiki{}
	ll := &fakeLLM{}
	svc := NewService(dao, wiki, ll, Config{}) // TTL zero → default
	if svc.ttl != defaultTTL {
		t.Fatalf("expected default TTL %v, got %v", defaultTTL, svc.ttl)
	}
}
