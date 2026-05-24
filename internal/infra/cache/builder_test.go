package cache_test

import (
	"crypto/md5"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

// stubMPDDataProvider is a minimal MPDDataProvider used to drive the cache
// builder in tests without requiring a live MPD connection.
type stubMPDDataProvider struct {
	albumsByBase map[string][]cache.AlbumDetailsData
	artists      map[string]int
	tracks       map[string][]cache.TrackData
	playlists    []string
	playlistInfo map[string][]cache.TrackData
}

func (s *stubMPDDataProvider) GetAlbumDetails(basePath string) ([]cache.AlbumDetailsData, error) {
	return s.albumsByBase[basePath], nil
}
func (s *stubMPDDataProvider) GetArtistsWithAlbumCounts() (map[string]int, error) {
	if s.artists == nil {
		return map[string]int{}, nil
	}
	return s.artists, nil
}
func (s *stubMPDDataProvider) FindAlbumTracks(album, albumArtist string) ([]cache.TrackData, error) {
	key := album + "\x00" + albumArtist
	return s.tracks[key], nil
}
func (s *stubMPDDataProvider) ListPlaylists() ([]string, error) { return s.playlists, nil }
func (s *stubMPDDataProvider) ListPlaylistInfo(name string) ([]cache.TrackData, error) {
	return s.playlistInfo[name], nil
}

// TestBuilder_FullBuild_PersistsGenre verifies that the cache builder
// forwards MPD's per-album Genre into CachedAlbum.Genre so the column
// added in v5 actually gets populated on rebuild.
func TestBuilder_FullBuild_PersistsGenre(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	provider := &stubMPDDataProvider{
		albumsByBase: map[string][]cache.AlbumDetailsData{
			"NAS": {
				{
					Album:       "Midnight Shores",
					AlbumArtist: "Hollow Tides",
					TrackCount:  12,
					FirstTrack:  "NAS/Hollow Tides/Midnight Shores/01.flac",
					TotalTime:   2917,
					Format:      "96000:24:2",
					Genre:       "Ambient / Post-Rock",
				},
				{
					Album:       "No Genre",
					AlbumArtist: "Anonymous",
					TrackCount:  1,
					FirstTrack:  "NAS/Anonymous/No Genre/01.flac",
					TotalTime:   100,
					Format:      "44100:16:2",
				},
			},
		},
	}

	builder := cache.NewBuilder(db, provider, cache.NewDefaultPathClassifier())
	builder.SetBasePaths([]string{"NAS"})

	if err := builder.FullBuild(); err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	dao := cache.NewDAO(db)
	albums, _, err := dao.QueryAlbums(cache.AlbumFilter{}, cache.SortAlphabetical, cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("QueryAlbums: %v", err)
	}

	gotByTitle := map[string]string{}
	for _, a := range albums {
		gotByTitle[a.Title] = a.Genre
	}
	if gotByTitle["Midnight Shores"] != "Ambient / Post-Rock" {
		t.Errorf("Midnight Shores Genre = %q, want %q", gotByTitle["Midnight Shores"], "Ambient / Post-Rock")
	}
	if gotByTitle["No Genre"] != "" {
		t.Errorf("No Genre Genre = %q, want empty string", gotByTitle["No Genre"])
	}
}

// TestBuilder_FullBuild_RelinksArtistArtwork verifies that a FullBuild
// reconnects a freshly-inserted artist row to a preserved artwork row that
// was inserted by enrichment in a previous run.
//
// Regression for the post-cache-rebuild "artist images disappeared" bug:
// Clear() preserves the artwork table by design (enrichment data is slow
// to rebuild and not derivable from MPD), but the artists row is wiped and
// re-inserted from MPD with an empty artwork_id. Without the relink step
// in buildArtists, those preserved artwork rows become orphans and the
// frontend's `pushLibraryArtists` payload reports no artwork.
func TestBuilder_FullBuild_RelinksArtistArtwork(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Pre-seed an artwork row exactly as enrichment would, with the
	// deterministic id-naming convention `<artist_id>_artwork`.
	artistName := "Hollow Tides"
	artistID := fmt.Sprintf("%x", md5.Sum([]byte(artistName)))
	artworkID := artistID + "_artwork"

	dao := cache.NewDAO(db)
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID:        artworkID,
		ArtistID:  artistID,
		Type:      "artist",
		FilePath:  "/tmp/fake-artist.jpg",
		Source:    "fanarttv",
		MimeType:  "image/jpeg",
		FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork: %v", err)
	}

	provider := &stubMPDDataProvider{
		artists: map[string]int{artistName: 1},
		albumsByBase: map[string][]cache.AlbumDetailsData{
			"NAS": {{
				Album:       "Whatever",
				AlbumArtist: artistName,
				TrackCount:  1,
				FirstTrack:  "NAS/Hollow Tides/Whatever/01.flac",
				TotalTime:   100,
				Format:      "44100:16:2",
			}},
		},
	}

	builder := cache.NewBuilder(db, provider, cache.NewDefaultPathClassifier())
	builder.SetBasePaths([]string{"NAS"})
	if err := builder.FullBuild(); err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	artists, _, err := dao.QueryArtists("", cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("QueryArtists: %v", err)
	}

	var found bool
	for _, a := range artists {
		if a.Name != artistName {
			continue
		}
		found = true
		if a.ArtworkID != artworkID {
			t.Errorf("artist %q artwork_id = %q, want %q (relink failed)", artistName, a.ArtworkID, artworkID)
		}
	}
	if !found {
		t.Fatalf("artist %q not in DB after FullBuild", artistName)
	}
}
