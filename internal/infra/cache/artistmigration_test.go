package cache_test

import (
	"crypto/md5"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

// newMigrationTestDB opens a fresh temp-file SQLite cache DB + DAO, closed
// automatically at test cleanup. Shared by artistmigration_test.go and
// albummigration_test.go (same cache_test package).
func newMigrationTestDB(t *testing.T) (*cache.DB, *cache.DAO) {
	t.Helper()
	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, cache.NewDAO(db)
}

// artworkRow is a full-table snapshot row used to prove the artwork table
// is byte-identical before/after an idempotent or dry-run call.
type artworkRow struct {
	ID, AlbumID, ArtistID, Type, FilePath, Source string
}

// dumpArtwork returns every row in the artwork table, ordered by id, for
// before/after equality comparisons.
func dumpArtwork(t *testing.T, db *cache.DB) []artworkRow {
	t.Helper()
	rows, err := db.DB().Query(`
		SELECT id, COALESCE(album_id,''), COALESCE(artist_id,''), type, COALESCE(file_path,''), source
		FROM artwork ORDER BY id
	`)
	if err != nil {
		t.Fatalf("dumpArtwork query: %v", err)
	}
	defer rows.Close()

	var out []artworkRow
	for rows.Next() {
		var r artworkRow
		if err := rows.Scan(&r.ID, &r.AlbumID, &r.ArtistID, &r.Type, &r.FilePath, &r.Source); err != nil {
			t.Fatalf("dumpArtwork scan: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("dumpArtwork rows.Err: %v", err)
	}
	return out
}

func md5ID(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}

// TestMigrateArtistArtwork_RekeysSingleArtistArtwork is the plan's Karajan
// acceptance case: a raw multi-credit name collapses to its first-credited
// performer, and the artwork already fetched under the raw-name identity
// must resolve under the collapsed identity afterward, without re-fetching.
func TestMigrateArtistArtwork_RekeysSingleArtistArtwork(t *testing.T) {
	t.Parallel()
	_, dao := newMigrationTestDB(t)

	rawName := "Herbert von Karajan  Wiener Philharmoniker"
	rawID := md5ID(rawName)
	oldArtworkID := rawID + "_artwork"

	if err := dao.InsertArtist(&cache.CachedArtist{ID: rawID, Name: rawName, AlbumCount: 1}); err != nil {
		t.Fatalf("InsertArtist: %v", err)
	}
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: oldArtworkID, ArtistID: rawID, Type: "artist",
		FilePath: "/tmp/karajan.jpg", Source: "fanarttv", MimeType: "image/jpeg",
		FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork: %v", err)
	}

	report, err := cache.MigrateArtistArtwork(dao, false)
	if err != nil {
		t.Fatalf("MigrateArtistArtwork: %v", err)
	}
	if report.Rekeyed != 1 {
		t.Fatalf("report.Rekeyed = %d, want 1", report.Rekeyed)
	}

	wantID := md5ID("Herbert von Karajan")
	wantArtworkID := wantID + "_artwork"

	art, err := dao.GetArtwork(wantArtworkID)
	if err != nil {
		t.Fatalf("GetArtwork(new): %v", err)
	}
	if art == nil {
		t.Fatalf("artwork row not found at new id %q", wantArtworkID)
	}
	if art.ArtistID != wantID {
		t.Fatalf("ArtistID = %q, want %q", art.ArtistID, wantID)
	}

	oldStill, err := dao.GetArtwork(oldArtworkID)
	if err != nil {
		t.Fatalf("GetArtwork(old): %v", err)
	}
	if oldStill != nil {
		t.Fatalf("old artwork row %q still exists after rekey, expected moved not duplicated", oldArtworkID)
	}
}

// TestMigrateArtistArtwork_MergeTieBreakPrefersExactMatch is the plan's Moby
// merge fixture: a bare, already-canonical row and a collapsible
// collaboration-credit row both collapse to the same target. The
// already-canonical row's own artwork must win; the collapsible row's own
// artwork must be left untouched in place (non-destructive, D-09).
func TestMigrateArtistArtwork_MergeTieBreakPrefersExactMatch(t *testing.T) {
	t.Parallel()
	db, dao := newMigrationTestDB(t)

	bareID := md5ID("Moby")
	bareArtworkID := bareID + "_artwork"
	if err := dao.InsertArtist(&cache.CachedArtist{ID: bareID, Name: "Moby", AlbumCount: 3}); err != nil {
		t.Fatalf("InsertArtist(bare): %v", err)
	}
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: bareArtworkID, ArtistID: bareID, Type: "artist",
		FilePath: "/tmp/moby-real.jpg", Source: "fanarttv", MimeType: "image/jpeg", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork(bare): %v", err)
	}

	variantName := "Moby, Jim James"
	variantID := md5ID(variantName)
	variantArtworkID := variantID + "_artwork"
	if err := dao.InsertArtist(&cache.CachedArtist{ID: variantID, Name: variantName, AlbumCount: 1}); err != nil {
		t.Fatalf("InsertArtist(variant): %v", err)
	}
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: variantArtworkID, ArtistID: variantID, Type: "artist",
		FilePath: "/tmp/moby-collab.jpg", Source: "deezer", MimeType: "image/jpeg", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork(variant): %v", err)
	}

	if _, err := cache.MigrateArtistArtwork(dao, false); err != nil {
		t.Fatalf("MigrateArtistArtwork: %v", err)
	}

	art, err := dao.GetArtwork(bareArtworkID)
	if err != nil {
		t.Fatalf("GetArtwork(bare): %v", err)
	}
	if art == nil {
		t.Fatalf("expected artwork row at %q", bareArtworkID)
	}
	if art.FilePath != "/tmp/moby-real.jpg" {
		t.Fatalf("FilePath = %q, want the exact-match Moby row's own file (clobbered by variant)", art.FilePath)
	}

	var count int
	if err := db.DB().QueryRow(`SELECT COUNT(*) FROM artwork WHERE id = ?`, bareArtworkID).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Fatalf("count at target id = %d, want 1", count)
	}

	stillThere, err := dao.GetArtwork(variantArtworkID)
	if err != nil {
		t.Fatalf("GetArtwork(variant): %v", err)
	}
	if stillThere == nil {
		t.Fatalf("variant's own artwork row %q should remain untouched (non-destructive, D-09)", variantArtworkID)
	}
}

// TestMigrateArtistArtwork_IdempotentOnSecondRun proves D-09: re-running
// after a successful run changes nothing.
func TestMigrateArtistArtwork_IdempotentOnSecondRun(t *testing.T) {
	t.Parallel()
	db, dao := newMigrationTestDB(t)

	rawName := "Herbert von Karajan  Wiener Philharmoniker"
	rawID := md5ID(rawName)
	if err := dao.InsertArtist(&cache.CachedArtist{ID: rawID, Name: rawName, AlbumCount: 1}); err != nil {
		t.Fatalf("InsertArtist: %v", err)
	}
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: rawID + "_artwork", ArtistID: rawID, Type: "artist",
		FilePath: "/tmp/karajan.jpg", Source: "fanarttv", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork: %v", err)
	}

	if _, err := cache.MigrateArtistArtwork(dao, false); err != nil {
		t.Fatalf("first run: %v", err)
	}

	before := dumpArtwork(t, db)

	report2, err := cache.MigrateArtistArtwork(dao, false)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if report2.Rekeyed != 0 {
		t.Fatalf("second-run report.Rekeyed = %d, want 0", report2.Rekeyed)
	}

	after := dumpArtwork(t, db)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("artwork table changed on idempotent re-run:\nbefore=%+v\nafter=%+v", before, after)
	}
}

// TestMigrateArtistArtwork_DryRunLeavesArtworkTableUnchanged proves dry-run
// computes the same report a real run would, without writing.
func TestMigrateArtistArtwork_DryRunLeavesArtworkTableUnchanged(t *testing.T) {
	t.Parallel()
	db, dao := newMigrationTestDB(t)

	rawName := "Herbert von Karajan  Wiener Philharmoniker"
	rawID := md5ID(rawName)
	if err := dao.InsertArtist(&cache.CachedArtist{ID: rawID, Name: rawName, AlbumCount: 1}); err != nil {
		t.Fatalf("InsertArtist: %v", err)
	}
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: rawID + "_artwork", ArtistID: rawID, Type: "artist",
		FilePath: "/tmp/karajan.jpg", Source: "fanarttv", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork: %v", err)
	}

	before := dumpArtwork(t, db)

	report, err := cache.MigrateArtistArtwork(dao, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if report.Rekeyed != 1 {
		t.Fatalf("dry-run report.Rekeyed = %d, want 1 (same report a real run would produce)", report.Rekeyed)
	}

	after := dumpArtwork(t, db)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("artwork table changed during dry run:\nbefore=%+v\nafter=%+v", before, after)
	}
}

// TestMigrateArtistArtwork_NoOpWhenNameAlreadyCanonical is the no-op-safety
// case: 88 of 124 real values need no collapse at all, and must be scanned
// but never rekeyed (their new id already equals their old id).
func TestMigrateArtistArtwork_NoOpWhenNameAlreadyCanonical(t *testing.T) {
	t.Parallel()
	_, dao := newMigrationTestDB(t)

	name := "Miles Davis" // no delimiter present -- already canonical
	id := md5ID(name)
	if err := dao.InsertArtist(&cache.CachedArtist{ID: id, Name: name, AlbumCount: 5}); err != nil {
		t.Fatalf("InsertArtist: %v", err)
	}
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: id + "_artwork", ArtistID: id, Type: "artist",
		FilePath: "/tmp/miles.jpg", Source: "fanarttv", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork: %v", err)
	}

	report, err := cache.MigrateArtistArtwork(dao, false)
	if err != nil {
		t.Fatalf("MigrateArtistArtwork: %v", err)
	}
	if report.Rekeyed != 0 {
		t.Fatalf("report.Rekeyed = %d, want 0 (already-canonical name needs no rekey)", report.Rekeyed)
	}
	if report.Scanned != 1 {
		t.Fatalf("report.Scanned = %d, want 1", report.Scanned)
	}

	art, err := dao.GetArtwork(id + "_artwork")
	if err != nil {
		t.Fatalf("GetArtwork: %v", err)
	}
	if art == nil || art.FilePath != "/tmp/miles.jpg" {
		t.Fatalf("artwork row moved/changed for an already-canonical artist, want untouched: %+v", art)
	}
}
