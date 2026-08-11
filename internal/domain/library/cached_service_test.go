package library

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

// --- helpers ---

// openTestDB opens a fresh in-memory-like SQLite DB in a temp dir.
func openTestDB(t *testing.T) *cache.DB {
	t.Helper()
	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestCachedService_GetAlbums_MapsGenre proves that CachedAlbum.Genre is
// mapped onto the Library Album payload returned to socket clients. The
// Library album-meta strip relies on this for the new "Ambient / Post-Rock"
// segment.
func TestCachedService_GetAlbums_MapsGenre(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dao := cache.NewDAO(db)

	withGenre := &cache.CachedAlbum{
		ID:          "id-with-genre",
		Title:       "Midnight Shores",
		AlbumArtist: "Hollow Tides",
		URI:         "NAS/Hollow Tides/Midnight Shores",
		Source:      "nas",
		Genre:       "Ambient / Post-Rock",
	}
	noGenre := &cache.CachedAlbum{
		ID:          "id-no-genre",
		Title:       "Untitled",
		AlbumArtist: "Anonymous",
		URI:         "NAS/Anonymous/Untitled",
		Source:      "nas",
	}
	if err := dao.InsertAlbum(withGenre); err != nil {
		t.Fatalf("insert with genre: %v", err)
	}
	if err := dao.InsertAlbum(noGenre); err != nil {
		t.Fatalf("insert no genre: %v", err)
	}

	svc := NewCachedService(&MockMPDClient{}, &MockPathClassifier{}, db)

	resp := svc.GetAlbums(GetAlbumsRequest{Scope: ScopeAll, Sort: SortAlphabetical})

	got := map[string]string{}
	for _, a := range resp.Albums {
		got[a.ID] = a.Genre
	}
	if got["id-with-genre"] != "Ambient / Post-Rock" {
		t.Errorf("with-genre.Genre = %q, want %q", got["id-with-genre"], "Ambient / Post-Rock")
	}
	if got["id-no-genre"] != "" {
		t.Errorf("no-genre.Genre = %q, want empty string", got["id-no-genre"])
	}
}

// TestCachedService_GetAlbums_IsBuilding_DoesNotFallbackToMPD proves that
// GetAlbums does NOT call the MPD fallback when AlbumCount is 0 but the cache
// is currently building. During a rebuild AlbumCount transiently reads 0 and
// we must not hammer MPD with live queries.
func TestCachedService_GetAlbums_IsBuilding_DoesNotFallbackToMPD(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	// Simulate a rebuild in progress: cache is empty (no albums) but IsBuilding=true.
	db.SetBuildingState(true, 50)

	// The mock will fail the test if ListAlbums is called (MPD fallback).
	mpdCalled := false
	mock := &MockMPDClient{
		ListAlbumsResponse: []AlbumInfo{{Album: "Fallback Album", AlbumArtist: "Artist"}},
	}
	_ = mock // suppress unused warning; the real test assertion is mpdCalled

	// Wrap mock so we can detect if any MPD call is made.
	spy := &mpdCallSpy{MockMPDClient: mock, called: &mpdCalled}
	svc := NewCachedService(spy, &MockPathClassifier{}, db)

	resp := svc.GetAlbums(GetAlbumsRequest{Scope: ScopeAll})

	if mpdCalled {
		t.Error("GetAlbums called MPD fallback while IsBuilding=true (should serve empty cache instead)")
	}
	// Should return empty result from the (empty) cache, not MPD data.
	if len(resp.Albums) != 0 {
		t.Errorf("expected 0 albums from empty cache while building, got %d", len(resp.Albums))
	}
}

// TestCachedService_GetAlbums_NotBuilding_FallsBackToMPD proves that
// GetAlbums DOES fall back to MPD when AlbumCount is 0 and IsBuilding is false
// (normal empty-cache startup path).
func TestCachedService_GetAlbums_NotBuilding_FallsBackToMPD(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	// IsBuilding defaults to false; no albums in cache.

	mpdCalled := false
	mock := &MockMPDClient{}
	spy := &mpdCallSpy{MockMPDClient: mock, called: &mpdCalled}
	svc := NewCachedService(spy, &MockPathClassifier{}, db)

	_ = svc.GetAlbums(GetAlbumsRequest{Scope: ScopeAll})

	if !mpdCalled {
		t.Error("GetAlbums should have fallen back to MPD when AlbumCount=0 and IsBuilding=false")
	}
}

// TestCachedService_GetArtists_IsBuilding_DoesNotFallbackToMPD mirrors the
// albums test for the GetArtists fallback path.
func TestCachedService_GetArtists_IsBuilding_DoesNotFallbackToMPD(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	db.SetBuildingState(true, 50)

	mpdCalled := false
	mock := &MockMPDClient{ListArtistsResponse: []string{"Some Artist"}}
	spy := &mpdCallSpy{MockMPDClient: mock, called: &mpdCalled}
	svc := NewCachedService(spy, &MockPathClassifier{}, db)

	resp := svc.GetArtists(GetArtistsRequest{})

	if mpdCalled {
		t.Error("GetArtists called MPD fallback while IsBuilding=true")
	}
	if len(resp.Artists) != 0 {
		t.Errorf("expected 0 artists from empty cache while building, got %d", len(resp.Artists))
	}
}

// TestCachedService_GetArtists_NotBuilding_FallsBackToMPD verifies the normal
// fallback path for GetArtists is preserved.
func TestCachedService_GetArtists_NotBuilding_FallsBackToMPD(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	mpdCalled := false
	mock := &MockMPDClient{}
	spy := &mpdCallSpy{MockMPDClient: mock, called: &mpdCalled}
	svc := NewCachedService(spy, &MockPathClassifier{}, db)

	_ = svc.GetArtists(GetArtistsRequest{})

	if !mpdCalled {
		t.Error("GetArtists should have fallen back to MPD when ArtistCount=0 and IsBuilding=false")
	}
}

// mpdCallSpy wraps MockMPDClient and records whether any album/artist list
// call was made (indicating a fall-through to the MPD live path).
type mpdCallSpy struct {
	*MockMPDClient
	called *bool
}

func (s *mpdCallSpy) ListAlbums() ([]AlbumInfo, error) {
	*s.called = true
	return s.MockMPDClient.ListAlbums()
}

func (s *mpdCallSpy) ListAlbumsInBase(basePath string) ([]AlbumInfo, error) {
	*s.called = true
	return s.MockMPDClient.ListAlbumsInBase(basePath)
}

func (s *mpdCallSpy) GetAlbumDetails(basePath string) ([]AlbumDetails, error) {
	*s.called = true
	return s.MockMPDClient.GetAlbumDetails(basePath)
}

func (s *mpdCallSpy) ListArtists() ([]string, error) {
	*s.called = true
	return s.MockMPDClient.ListArtists()
}

func (s *mpdCallSpy) FindAlbumsByArtist(artist string) ([]AlbumInfo, error) {
	*s.called = true
	return s.MockMPDClient.FindAlbumsByArtist(artist)
}

// TestMPDDataProviderAdapter_CountUntagged proves mpdDataProviderAdapter's
// CountUntagged is wired to SearchByBase + musicfile.CountUntagged (DATA-02),
// and that a SearchByBase error propagates rather than being swallowed.
func TestMPDDataProviderAdapter_CountUntagged(t *testing.T) {
	t.Parallel()

	t.Run("counts real untagged songs, excludes resource-fork junk", func(t *testing.T) {
		t.Parallel()
		mockMPD := &MockMPDClient{
			SearchByBaseResp: map[string][]map[string]string{
				"USB": {
					{"file": "USB/a/01.flac", "Album": "A"},
					{"file": "USB/a/02.flac", "Album": ""},
					{"file": "USB/a/._02.flac", "Album": ""},
				},
			},
		}
		adapter := &mpdDataProviderAdapter{mpd: mockMPD}

		got, err := adapter.CountUntagged("USB")
		if err != nil {
			t.Fatalf("CountUntagged: %v", err)
		}
		if got != 1 {
			t.Errorf("CountUntagged = %d, want 1", got)
		}
	})

	t.Run("propagates SearchByBase error", func(t *testing.T) {
		t.Parallel()
		wantErr := fmt.Errorf("mpd unavailable")
		mockMPD := &MockMPDClient{SearchByBaseError: wantErr}
		adapter := &mpdDataProviderAdapter{mpd: mockMPD}

		_, err := adapter.CountUntagged("USB")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
