package cache_test

import (
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

// stubMPDDataProvider is a minimal MPDDataProvider used to drive the cache
// builder in tests without requiring a live MPD connection.
type stubMPDDataProvider struct {
	albumsByBase      map[string][]cache.AlbumDetailsData
	artists           map[string]int
	tracks            map[string][]cache.TrackData
	playlists         []string
	playlistInfo      map[string][]cache.TrackData
	countAlbumsResult int
	countAlbumsErr    error
	untaggedByBase    map[string]int
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
func (s *stubMPDDataProvider) CountAlbums() (int, error) {
	return s.countAlbumsResult, s.countAlbumsErr
}
func (s *stubMPDDataProvider) CountUntagged(basePath string) (int, error) {
	return s.untaggedByBase[basePath], nil
}

// TestFullBuild_PreservesCache_WhenMPDEmpty verifies that FullBuild does NOT
// call Clear and returns nil when CountAlbums returns 0, preserving any
// albums already in the cache.
func TestFullBuild_PreservesCache_WhenMPDEmpty(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		count  int
		err    error
		wantOK bool // true = no error returned, false = error expected
	}{
		{
			name:   "mpd returns zero albums",
			count:  0,
			err:    nil,
			wantOK: true,
		},
		{
			name:   "mpd returns error",
			count:  0,
			err:    errors.New("connection refused"),
			wantOK: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
			if err := db.Open(); err != nil {
				t.Fatalf("open db: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })

			// Pre-populate the cache with one album so we can verify it is
			// preserved after the skipped rebuild.
			dao := cache.NewDAO(db)
			if err := dao.InsertAlbum(&cache.CachedAlbum{
				ID:          "preserved-album-id",
				Title:       "Should Stay",
				AlbumArtist: "Artist",
				URI:         "NAS/Artist/Should Stay",
				Source:      "nas",
			}); err != nil {
				t.Fatalf("InsertAlbum: %v", err)
			}

			provider := &stubMPDDataProvider{
				countAlbumsResult: tc.count,
				countAlbumsErr:    tc.err,
			}

			builder := cache.NewBuilder(db, provider, cache.NewDefaultPathClassifier())

			if err := builder.FullBuild(); err != nil {
				t.Fatalf("FullBuild returned error: %v (want nil)", err)
			}

			// The album must still be present — Clear must NOT have been called.
			albums, total, err := dao.QueryAlbums(cache.AlbumFilter{}, cache.SortAlphabetical, cache.NewPagination(1, 50))
			if err != nil {
				t.Fatalf("QueryAlbums: %v", err)
			}
			if total == 0 || len(albums) == 0 {
				t.Fatalf("cache was cleared (total=%d) but should have been preserved", total)
			}
			if albums[0].ID != "preserved-album-id" {
				t.Errorf("unexpected album id %q; cache was not preserved", albums[0].ID)
			}
		})
	}
}

// TestFullBuild_ProceedsNormally_WhenMPDHasAlbums verifies that FullBuild
// performs the normal rebuild when CountAlbums returns > 0.
func TestFullBuild_ProceedsNormally_WhenMPDHasAlbums(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	provider := &stubMPDDataProvider{
		countAlbumsResult: 2, // MPD has albums → rebuild should proceed
		albumsByBase: map[string][]cache.AlbumDetailsData{
			"NAS": {
				{
					Album:       "New Album",
					AlbumArtist: "New Artist",
					TrackCount:  5,
					FirstTrack:  "NAS/New Artist/New Album/01.flac",
					TotalTime:   1200,
					Format:      "44100:16:2",
				},
			},
		},
		artists: map[string]int{"New Artist": 1},
	}

	builder := cache.NewBuilder(db, provider, cache.NewDefaultPathClassifier())
	builder.SetBasePaths([]string{"NAS"})

	if err := builder.FullBuild(); err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	dao := cache.NewDAO(db)
	albums, total, err := dao.QueryAlbums(cache.AlbumFilter{}, cache.SortAlphabetical, cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("QueryAlbums: %v", err)
	}
	if total == 0 || len(albums) == 0 {
		t.Fatal("rebuild did not populate albums")
	}
	if albums[0].Title != "New Album" {
		t.Errorf("got album %q, want %q", albums[0].Title, "New Album")
	}
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
		countAlbumsResult: 2, // non-zero so FullBuild proceeds normally
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
		countAlbumsResult: 1, // non-zero so FullBuild proceeds normally
		artists:           map[string]int{artistName: 1},
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

// TestBuilder_FullBuild_RelinksAlbumArtwork verifies that a FullBuild
// reconnects a freshly-inserted album row to a preserved artwork row that
// was previously inserted by enrichment. Mirrors the artist relink, fixes
// the data-loss-across-rebuild gap surfaced 2026-05-27: today every
// cache:rebuild wipes the albums table and re-inserts from MPD with
// artwork_id=NULL, forcing the enrichment worker to re-fetch every album's
// artwork from MusicBrainz/CAA (rate-limited to 1 req/sec — slow + wasteful).
//
// With the fix: enrichment inserts artwork rows with id=`<album_id>_artwork`
// and album_id=<album_id>, and the album relinker restores the FK in a
// single batched UPDATE, mirroring the artist relinker semantics exactly.
func TestBuilder_FullBuild_RelinksAlbumArtwork(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Pre-seed an artwork row exactly as the fixed album save path would,
	// with deterministic id = `<album_id>_artwork`. The album_id mirrors
	// generateAlbumID(albumArtist, album, uri) so the relinker can find it.
	albumArtist := "Hollow Tides"
	album := "Whatever"
	uri := "NAS/Hollow Tides/Whatever"
	idData := albumArtist + "\x00" + album + "\x00" + uri
	albumID := fmt.Sprintf("%x", md5.Sum([]byte(idData)))
	artworkID := albumID + "_artwork"

	dao := cache.NewDAO(db)
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID:        artworkID,
		AlbumID:   albumID,
		Type:      "album",
		FilePath:  "/tmp/fake-album.jpg",
		Source:    "cover_art_archive",
		MimeType:  "image/jpeg",
		FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork: %v", err)
	}

	provider := &stubMPDDataProvider{
		countAlbumsResult: 1, // non-zero so FullBuild proceeds normally
		artists:           map[string]int{albumArtist: 1},
		albumsByBase: map[string][]cache.AlbumDetailsData{
			"NAS": {{
				Album:       album,
				AlbumArtist: albumArtist,
				TrackCount:  1,
				FirstTrack:  uri + "/01.flac",
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

	cachedAlbum, err := dao.GetAlbum(albumID)
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if cachedAlbum == nil {
		t.Fatalf("album %q not in DB after FullBuild", albumID)
	}
	if cachedAlbum.ArtworkID != artworkID {
		t.Errorf("album artwork_id = %q, want %q (album relink failed)", cachedAlbum.ArtworkID, artworkID)
	}
}

// TestBackfillAlbumArtwork_RecoversOrphanIDsFromDisk reproduces the live
// state observed 2026-05-27: 40 albums had `artwork_id` set to a hash
// that pointed at nothing in the `artwork` table, because the buggy
// album save path wrote the JPG file but never inserted the row. The
// JPG file is on disk; we just need to write the missing row.
//
// The backfill handler:
//  1. Scans albums with non-empty artwork_id and no matching artwork row.
//  2. For each, looks for a JPG/PNG/WEBP/GIF in the album artwork dir
//     keyed by the album_id (matching the new save path convention).
//  3. Inserts an artwork row using the deterministic
//     `<album_id>_artwork` id so the next rebuild's relinker can pick
//     it up. Updates albums.artwork_id to the new id as a side effect.
//
// Idempotent: re-running on a clean state must be a no-op.
func TestBackfillAlbumArtwork_RecoversOrphanIDsFromDisk(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dao := cache.NewDAO(db)

	// Seed an album with an orphan artwork_id (the pre-fix bug shape):
	// the album points at an artwork ID that doesn't exist in the DB,
	// even though the JPG file is on disk.
	albumID := "fab051cf038f488e41a1d0001be6a483"
	orphanID := "b4839643df150b93c6013f97799c5edf"
	if err := dao.InsertAlbum(&cache.CachedAlbum{
		ID:          albumID,
		Title:       "Whatever",
		AlbumArtist: "Hollow Tides",
		URI:         "NAS/Hollow Tides/Whatever",
		Source:      "nas",
		ArtworkID:   orphanID, // dangling — no artwork row exists yet
	}); err != nil {
		t.Fatalf("InsertAlbum: %v", err)
	}

	// Materialise the on-disk JPG that the buggy save path wrote.
	artworkDir := filepath.Join(tmpDir, "cache", "artwork", "albums")
	if err := os.MkdirAll(artworkDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	jpgPath := filepath.Join(artworkDir, albumID+".jpg")
	if err := os.WriteFile(jpgPath, []byte("\xff\xd8\xff stub"), 0644); err != nil {
		t.Fatalf("write jpg: %v", err)
	}

	// Run the backfill.
	count, err := cache.BackfillAlbumArtwork(dao, filepath.Join(tmpDir, "cache"))
	if err != nil {
		t.Fatalf("BackfillAlbumArtwork: %v", err)
	}
	if count != 1 {
		t.Errorf("backfill count = %d, want 1", count)
	}

	// The album's artwork_id must now use the deterministic format so
	// the rebuild relinker can find it.
	updated, err := dao.GetAlbum(albumID)
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	wantNewID := albumID + "_artwork"
	if updated.ArtworkID != wantNewID {
		t.Errorf("post-backfill artwork_id = %q, want %q", updated.ArtworkID, wantNewID)
	}

	// The artwork row must exist with type='album', album_id linked, and
	// file_path pointing at the on-disk JPG.
	art, err := dao.GetArtwork(wantNewID)
	if err != nil {
		t.Fatalf("GetArtwork: %v", err)
	}
	if art == nil {
		t.Fatalf("artwork row %q missing after backfill", wantNewID)
	}
	if art.Type != "album" || art.AlbumID != albumID {
		t.Errorf("artwork row metadata wrong: type=%q album_id=%q", art.Type, art.AlbumID)
	}
	if art.FilePath != jpgPath {
		t.Errorf("artwork file_path = %q, want %q", art.FilePath, jpgPath)
	}

	// Idempotency: a second run on the same state must be a no-op.
	again, err := cache.BackfillAlbumArtwork(dao, filepath.Join(tmpDir, "cache"))
	if err != nil {
		t.Fatalf("second BackfillAlbumArtwork: %v", err)
	}
	if again != 0 {
		t.Errorf("idempotent re-run count = %d, want 0", again)
	}
}

// TestFullBuild_PersistsSkippedCount verifies that FullBuild sums
// CountUntagged across every configured basePath and persists the total via
// cache_meta, readable back from GetStats().SkippedCount (DATA-02).
func TestFullBuild_PersistsSkippedCount(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	provider := &stubMPDDataProvider{
		countAlbumsResult: 1, // MPD has albums -> rebuild should proceed
		albumsByBase: map[string][]cache.AlbumDetailsData{
			"USB": {
				{
					Album:       "Some Album",
					AlbumArtist: "Some Artist",
					TrackCount:  1,
					FirstTrack:  "USB/Some Artist/Some Album/01.flac",
					TotalTime:   200,
				},
			},
		},
		artists:        map[string]int{"Some Artist": 1},
		untaggedByBase: map[string]int{"INTERNAL": 3, "USB": 13, "NAS": 0},
	}

	builder := cache.NewBuilder(db, provider, cache.NewDefaultPathClassifier())
	builder.SetBasePaths([]string{"INTERNAL", "USB", "NAS"})

	if err := builder.FullBuild(); err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	stats, err := db.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.SkippedCount != 16 {
		t.Errorf("SkippedCount = %d, want 16 (3 INTERNAL + 13 USB + 0 NAS)", stats.SkippedCount)
	}
}

// TestBuildArtists_CollapsesAndMergesPavarottiVariants verifies ARTIST-01/
// ARTIST-02: the 15 real `Luciano Pavarotti, ...` raw MPD Artist tag values
// captured in 02-ARTIST-CORPUS.md all collapse to a single "Luciano
// Pavarotti" row whose AlbumCount is the SUM of all 15 raw variants' counts,
// not the count of whichever variant buildArtists happened to process last
// (map iteration order in Go is randomized, so a last-write-wins bug would
// be flaky/undetectable without this explicit sum assertion).
func TestBuildArtists_CollapsesAndMergesPavarottiVariants(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pavarottiVariants := map[string]int{
		"Luciano Pavarotti, Berliner Philharmoniker, Herbert von Karajan":                                                  1,
		"Luciano Pavarotti, Coro del Teatro Comunale di Bologna, Orchestra del Teatro Comunale di Bologna, Anton Guadagno": 1,
		"Luciano Pavarotti, English Chamber Orchestra, Richard Bonynge":                                                    1,
		"Luciano Pavarotti, John Alldis Choir, Wandsworth School Boys Choir, London Philharmonic Orchestra, Zubin Mehta":   1,
		"Luciano Pavarotti, London Symphony Orchestra, Richard Bonynge":                                                    1,
		"Luciano Pavarotti, Mirella Freni, Berliner Philharmoniker, Herbert von Karajan":                                   1,
		"Luciano Pavarotti, National Philharmonic Orchestra, Giancarlo Chiaramello":                                        1,
		"Luciano Pavarotti, National Philharmonic Orchestra, Nicola Rescigno":                                              1,
		"Luciano Pavarotti, New Philharmonia Orchestra, Richard Bonynge":                                                   1,
		"Luciano Pavarotti, Nicolai Ghiaurov, National Philharmonic Orchestra, Robin Stapleton":                            1,
		"Luciano Pavarotti, Orchestra del Teatro Comunale di Bologna, Richard Bonynge":                                     1,
		"Luciano Pavarotti, Orchestra of the Royal Opera House, Covent Garden, Edward Downes":                              1,
		"Luciano Pavarotti, Philharmonia Orchestra, Piero Gamba":                                                           1,
		"Luciano Pavarotti, Wiener Opernorchester, Nicola Rescigno":                                                        1,
		"Luciano Pavarotti, Wiener Philharmoniker, Sir Georg Solti":                                                        1,
		"Luciano Pavarotti, Wiener Volksopernorchester, Leone Magiera":                                                     1,
	}
	if len(pavarottiVariants) != 16 {
		t.Fatalf("test setup error: expected 16 distinct raw keys (15 Pavarotti variants + this check), got %d", len(pavarottiVariants))
	}

	provider := &stubMPDDataProvider{
		countAlbumsResult: 1, // non-zero so FullBuild proceeds normally
		artists:           pavarottiVariants,
	}

	builder := cache.NewBuilder(db, provider, cache.NewDefaultPathClassifier())
	if err := builder.FullBuild(); err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	dao := cache.NewDAO(db)
	artists, total, err := dao.QueryArtists("", cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("QueryArtists: %v", err)
	}
	if total != 1 {
		t.Fatalf("total artists = %d, want 1 (all 16 raw Pavarotti variants must collapse to one row)", total)
	}
	if len(artists) != 1 {
		t.Fatalf("len(artists) = %d, want 1", len(artists))
	}
	if artists[0].Name != "Luciano Pavarotti" {
		t.Errorf("artist name = %q, want %q", artists[0].Name, "Luciano Pavarotti")
	}
	if artists[0].AlbumCount != 16 {
		t.Errorf("AlbumCount = %d, want 16 (sum of all 16 raw variants, not last-write-wins)", artists[0].AlbumCount)
	}
}

// TestBuildArtists_MergesCanonicalAndVariants verifies the mixed case: an
// already-canonical raw name ("Moby": 3) plus two raw variants that both
// collapse to the same canonical ("Moby, Jim James": 1, "Moby, Mindy
// Jones": 1) must merge into ONE "Moby" row with AlbumCount 5 (3+1+1), not
// three separate rows and not a last-write-wins overwrite.
func TestBuildArtists_MergesCanonicalAndVariants(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	provider := &stubMPDDataProvider{
		countAlbumsResult: 1,
		artists: map[string]int{
			"Moby":              3,
			"Moby, Jim James":   1,
			"Moby, Mindy Jones": 1,
		},
	}

	builder := cache.NewBuilder(db, provider, cache.NewDefaultPathClassifier())
	if err := builder.FullBuild(); err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	dao := cache.NewDAO(db)
	artists, total, err := dao.QueryArtists("", cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("QueryArtists: %v", err)
	}
	if total != 1 {
		t.Fatalf("total artists = %d, want 1", total)
	}
	if artists[0].Name != "Moby" {
		t.Errorf("artist name = %q, want %q", artists[0].Name, "Moby")
	}
	if artists[0].AlbumCount != 5 {
		t.Errorf("AlbumCount = %d, want 5 (3+1+1 merged)", artists[0].AlbumCount)
	}
}

// TestBuildArtists_EmptyRawNameProducesNoRow verifies ARTIST-03: MPD's real
// `list artist` output contains one empty-string entry, and buildArtists
// must never insert a row for it.
func TestBuildArtists_EmptyRawNameProducesNoRow(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	provider := &stubMPDDataProvider{
		countAlbumsResult: 1,
		artists: map[string]int{
			"":     7,
			"Moby": 3,
		},
	}

	builder := cache.NewBuilder(db, provider, cache.NewDefaultPathClassifier())
	if err := builder.FullBuild(); err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	dao := cache.NewDAO(db)
	artists, total, err := dao.QueryArtists("", cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("QueryArtists: %v", err)
	}
	if total != 1 {
		t.Fatalf("total artists = %d, want 1 (empty-name entry must be skipped)", total)
	}
	for _, a := range artists {
		if a.Name == "" {
			t.Errorf("found artist row with empty Name — ARTIST-03 violated")
		}
	}
}

// TestBuildArtists_NonCollapsibleNamesPassThroughUnchanged is the
// regression safety net for the 88 "leave alone" corpus values: raw names
// with no collapse-eligible delimiter must produce one row per name,
// unchanged, with unchanged counts.
func TestBuildArtists_NonCollapsibleNamesPassThroughUnchanged(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	provider := &stubMPDDataProvider{
		countAlbumsResult: 1,
		artists: map[string]int{
			"Radiohead":      4,
			"Björk":          2,
			"Massive Attack": 1,
		},
	}

	builder := cache.NewBuilder(db, provider, cache.NewDefaultPathClassifier())
	if err := builder.FullBuild(); err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	dao := cache.NewDAO(db)
	artists, total, err := dao.QueryArtists("", cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("QueryArtists: %v", err)
	}
	if total != 3 {
		t.Fatalf("total artists = %d, want 3 (no collapsing should occur)", total)
	}
	gotCounts := map[string]int{}
	for _, a := range artists {
		gotCounts[a.Name] = a.AlbumCount
	}
	want := map[string]int{"Radiohead": 4, "Björk": 2, "Massive Attack": 1}
	for name, wantCount := range want {
		if gotCounts[name] != wantCount {
			t.Errorf("artist %q AlbumCount = %d, want %d", name, gotCounts[name], wantCount)
		}
	}
}

// TestGetStats_SkippedCountDefaultsToZero_WhenNoMetaRow verifies that a
// fresh DB with no skipped_count meta row (e.g. before any FullBuild ever
// ran) reports SkippedCount == 0, not an error.
func TestGetStats_SkippedCountDefaultsToZero_WhenNoMetaRow(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stats, err := db.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.SkippedCount != 0 {
		t.Errorf("SkippedCount = %d, want 0 on a fresh DB", stats.SkippedCount)
	}
}
