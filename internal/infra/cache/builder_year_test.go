package cache_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

// TestBuilder_FullBuild_PersistsYearAndAddedAt covers the cache browse path.
// dao.go's SQL ordering for both sorts was always correct; the data was
// missing. AddedAt in particular used to be time.Now() for every album, which
// a FullBuild's Clear()-then-repopulate cycle re-stamped on every rebuild --
// defeating the DAO's `added_at = COALESCE(albums.added_at, ?)` preservation
// and making sort=recently_added arbitrary.
func TestBuilder_FullBuild_PersistsYearAndAddedAt(t *testing.T) {
	t.Parallel()

	// MPD reports Last-Modified in UTC, so fixtures are UTC. They are relative
	// to now because "Untagged Album" carries no mtime and therefore falls
	// back to the build clock -- ordering assertions have to be stable against
	// wherever that lands.
	now := time.Now().UTC().Truncate(time.Second)
	older := now.Add(-72 * time.Hour)
	newer := now.Add(-1 * time.Hour)
	// Later than the build clock, so it must outrank the fallback. This is the
	// zone-mix regression guard: added_at is a TEXT column, so a fallback left
	// in a +10:00 local zone would sort ABOVE this UTC timestamp lexically
	// even though it is chronologically earlier.
	future := now.Add(1 * time.Hour)

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	provider := &stubMPDDataProvider{
		countAlbumsResult: 4,
		albumsByBase: map[string][]cache.AlbumDetailsData{
			"USB": {
				{
					Album: "Newer Album", AlbumArtist: "Artist1", TrackCount: 10,
					FirstTrack: "USB/Newer/01.flac", Format: "96000:24:2",
					Year: 2024, LastModified: newer,
				},
				{
					Album: "Older Album", AlbumArtist: "Artist2", TrackCount: 8,
					FirstTrack: "USB/Older/01.flac", Format: "44100:16:2",
					Year: 1959, LastModified: older,
				},
				{
					Album: "Untagged Album", AlbumArtist: "Artist3", TrackCount: 6,
					FirstTrack: "USB/Untagged/01.flac", Format: "44100:16:2",
				},
				{
					Album: "Future Album", AlbumArtist: "Artist4", TrackCount: 4,
					FirstTrack: "USB/Future/01.flac", Format: "44100:16:2",
					Year: 2026, LastModified: future,
				},
			},
		},
	}

	builder := cache.NewBuilder(db, provider, cache.NewDefaultPathClassifier())
	builder.SetBasePaths([]string{"USB"})
	if err := builder.FullBuild(); err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	dao := cache.NewDAO(db)

	albums, _, err := dao.QueryAlbums(cache.AlbumFilter{}, cache.SortAlphabetical, cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("QueryAlbums: %v", err)
	}
	byTitle := map[string]*cache.CachedAlbum{}
	for _, a := range albums {
		byTitle[a.Title] = a
	}

	if got := byTitle["Newer Album"].Year; got != 2024 {
		t.Errorf("Newer Album year = %d, want 2024", got)
	}
	if got := byTitle["Older Album"].Year; got != 1959 {
		t.Errorf("Older Album year = %d, want 1959", got)
	}
	if got := byTitle["Untagged Album"].Year; got != 0 {
		t.Errorf("Untagged Album year = %d, want 0", got)
	}
	if got := byTitle["Newer Album"].AddedAt; !got.Equal(newer) {
		t.Errorf("Newer Album added_at = %v, want %v", got, newer)
	}
	if got := byTitle["Older Album"].AddedAt; !got.Equal(older) {
		t.Errorf("Older Album added_at = %v, want %v", got, older)
	}
	// No Last-Modified from MPD must not sort the album to the epoch.
	if byTitle["Untagged Album"].AddedAt.IsZero() {
		t.Error("Untagged Album added_at is zero; want a fallback timestamp")
	}

	byYear, _, err := dao.QueryAlbums(cache.AlbumFilter{}, cache.SortYear, cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("QueryAlbums(SortYear): %v", err)
	}
	wantYearOrder := []string{"Future Album", "Newer Album", "Older Album", "Untagged Album"}
	assertOrder(t, "sort=year", byYear, wantYearOrder)

	byAdded, _, err := dao.QueryAlbums(cache.AlbumFilter{}, cache.SortRecentlyAdded, cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("QueryAlbums(SortRecentlyAdded): %v", err)
	}
	// Untagged sits between Future and Newer purely because its fallback is
	// the build clock; the load-bearing part is that Future outranks it (see
	// the `future` fixture comment) and that Older is last.
	wantAddedOrder := []string{"Future Album", "Untagged Album", "Newer Album", "Older Album"}
	assertOrder(t, "sort=recently_added", byAdded, wantAddedOrder)
}

func assertOrder(t *testing.T, label string, albums []*cache.CachedAlbum, want []string) {
	t.Helper()
	got := make([]string, len(albums))
	for i, a := range albums {
		got[i] = a.Title
	}
	if len(got) != len(want) {
		t.Fatalf("%s returned %d albums, want %d (%v)", label, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s position %d = %q, want %q (full order: %v)", label, i, got[i], want[i], got)
		}
	}
}
