package cache_test

import (
	"path/filepath"
	"testing"

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
