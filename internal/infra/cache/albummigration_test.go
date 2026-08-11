package cache_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

// TestAlbumMigration_ListOrphanedAlbumArtwork mirrors the exact query
// manually validated against the live Pi DB during planning (38 rows,
// 2026-08-12): every 'album' artwork row whose album_id points at nothing
// in the albums table.
func TestAlbumMigration_ListOrphanedAlbumArtwork(t *testing.T) {
	t.Parallel()
	_, dao := newMigrationTestDB(t)

	// A live album with a linked (non-orphan) artwork row.
	liveAlbumID := "live-album-id"
	if err := dao.InsertAlbum(&cache.CachedAlbum{ID: liveAlbumID, Title: "Live", AlbumArtist: "X", URI: "u", Source: "usb"}); err != nil {
		t.Fatalf("InsertAlbum: %v", err)
	}
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: liveAlbumID + "_artwork", AlbumID: liveAlbumID, Type: "album",
		FilePath: "/tmp/live.jpg", Source: "cover_art_archive", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork(live): %v", err)
	}
	if err := dao.UpdateAlbumArtwork(liveAlbumID, liveAlbumID+"_artwork"); err != nil {
		t.Fatalf("UpdateAlbumArtwork: %v", err)
	}

	// An artist artwork row -- type='artist', must never be treated as an
	// orphan album row even though its album_id column is empty.
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: "some-artist_artwork", ArtistID: "some-artist", Type: "artist",
		FilePath: "/tmp/artist.jpg", Source: "fanarttv", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork(artist): %v", err)
	}

	// The orphan: an 'album' artwork row whose album_id points at nothing
	// in albums (this album's identity changed underneath it).
	orphanID := "gone-album-id_artwork"
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: orphanID, AlbumID: "gone-album-id", Type: "album",
		FilePath: "/tmp/orphan.jpg", Source: "backfill", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork(orphan): %v", err)
	}

	orphans, err := cache.ListOrphanedAlbumArtwork(dao)
	if err != nil {
		t.Fatalf("ListOrphanedAlbumArtwork: %v", err)
	}
	if len(orphans) != 1 {
		t.Fatalf("len(orphans) = %d, want 1: %+v", len(orphans), orphans)
	}
	got := orphans[0]
	if got.ArtworkID != orphanID || got.AlbumID != "gone-album-id" || got.FilePath != "/tmp/orphan.jpg" {
		t.Fatalf("orphan = %+v, want ArtworkID=%q AlbumID=%q FilePath=%q",
			got, orphanID, "gone-album-id", "/tmp/orphan.jpg")
	}
}

// TestAlbumMigration_RekeyAlbumArtwork_AppliesApprovedMapping proves the
// first, successful application of a human-approved orphan->album pairing.
func TestAlbumMigration_RekeyAlbumArtwork_AppliesApprovedMapping(t *testing.T) {
	t.Parallel()
	_, dao := newMigrationTestDB(t)

	orphanID := "orphan1_artwork"
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: orphanID, AlbumID: "old-gone-id-1", Type: "album",
		FilePath: "/tmp/orphan1.jpg", Source: "backfill", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork: %v", err)
	}

	targetAlbumID := "target-album-id-1"
	if err := dao.InsertAlbum(&cache.CachedAlbum{ID: targetAlbumID, Title: "T", AlbumArtist: "X", URI: "u1", Source: "usb"}); err != nil {
		t.Fatalf("InsertAlbum: %v", err)
	}

	if err := cache.RekeyAlbumArtwork(dao, orphanID, targetAlbumID, false); err != nil {
		t.Fatalf("RekeyAlbumArtwork: %v", err)
	}

	wantArtworkID := targetAlbumID + "_artwork"
	art, err := dao.GetArtwork(wantArtworkID)
	if err != nil {
		t.Fatalf("GetArtwork(new): %v", err)
	}
	if art == nil {
		t.Fatalf("expected artwork row at %q", wantArtworkID)
	}
	if art.AlbumID != targetAlbumID {
		t.Fatalf("AlbumID = %q, want %q", art.AlbumID, targetAlbumID)
	}
	if art.FilePath != "/tmp/orphan1.jpg" {
		t.Fatalf("FilePath changed: %q, want the original on-disk file path preserved (D-09)", art.FilePath)
	}

	album, err := dao.GetAlbum(targetAlbumID)
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if album.ArtworkID != wantArtworkID {
		t.Fatalf("album.ArtworkID = %q, want %q", album.ArtworkID, wantArtworkID)
	}

	oldStill, err := dao.GetArtwork(orphanID)
	if err != nil {
		t.Fatalf("GetArtwork(old): %v", err)
	}
	if oldStill != nil {
		t.Fatalf("old orphan artwork row %q still present after rekey", orphanID)
	}
}

// TestAlbumMigration_RekeyAlbumArtwork_RepeatCallIsSafeNoOp is the
// hard_constraint #2 case the planner previously got wrong: re-applying the
// SAME (orphanArtworkID, targetAlbumID) pair after it already succeeded
// must return nil, not an error, and change nothing.
func TestAlbumMigration_RekeyAlbumArtwork_RepeatCallIsSafeNoOp(t *testing.T) {
	t.Parallel()
	db, dao := newMigrationTestDB(t)

	orphanID := "orphan2_artwork"
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: orphanID, AlbumID: "old-gone-id-2", Type: "album",
		FilePath: "/tmp/orphan2.jpg", Source: "backfill", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork: %v", err)
	}
	targetAlbumID := "target-album-id-2"
	if err := dao.InsertAlbum(&cache.CachedAlbum{ID: targetAlbumID, Title: "T2", AlbumArtist: "X", URI: "u2", Source: "usb"}); err != nil {
		t.Fatalf("InsertAlbum: %v", err)
	}

	if err := cache.RekeyAlbumArtwork(dao, orphanID, targetAlbumID, false); err != nil {
		t.Fatalf("first call: %v", err)
	}

	before := dumpArtwork(t, db)

	// Re-apply the IDENTICAL pair. Note the orphan row under its ORIGINAL
	// id no longer exists (it was renamed by the first call) -- this must
	// still succeed as a safe no-op, not fail with "orphan not found".
	if err := cache.RekeyAlbumArtwork(dao, orphanID, targetAlbumID, false); err != nil {
		t.Fatalf("second call with identical pair returned an error, want nil (safe no-op): %v", err)
	}

	after := dumpArtwork(t, db)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("artwork table changed on repeat call:\nbefore=%+v\nafter=%+v", before, after)
	}
}

// TestAlbumMigration_RekeyAlbumArtwork_RefusesToClobberDifferentArtwork
// proves the target album is never overwritten when it already carries a
// DIFFERENT, unrelated artwork link.
func TestAlbumMigration_RekeyAlbumArtwork_RefusesToClobberDifferentArtwork(t *testing.T) {
	t.Parallel()
	db, dao := newMigrationTestDB(t)

	targetAlbumID := "target-album-id-4"
	existingArtworkID := "legacy-unrelated-artwork-id" // deliberately NOT targetAlbumID+"_artwork"
	if err := dao.InsertAlbum(&cache.CachedAlbum{ID: targetAlbumID, Title: "T4", AlbumArtist: "X", URI: "u4", Source: "usb"}); err != nil {
		t.Fatalf("InsertAlbum: %v", err)
	}
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: existingArtworkID, AlbumID: targetAlbumID, Type: "album",
		FilePath: "/tmp/existing.jpg", Source: "cover_art_archive", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork(existing): %v", err)
	}
	if err := dao.UpdateAlbumArtwork(targetAlbumID, existingArtworkID); err != nil {
		t.Fatalf("UpdateAlbumArtwork: %v", err)
	}

	orphanID := "orphan4_artwork"
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: orphanID, AlbumID: "old-gone-id-4", Type: "album",
		FilePath: "/tmp/orphan4.jpg", Source: "backfill", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork(orphan): %v", err)
	}

	before := dumpArtwork(t, db)

	err := cache.RekeyAlbumArtwork(dao, orphanID, targetAlbumID, false)
	if err == nil {
		t.Fatalf("expected refuse-to-clobber error, got nil")
	}

	after := dumpArtwork(t, db)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("artwork table changed despite refuse-to-clobber:\nbefore=%+v\nafter=%+v", before, after)
	}
}

// TestAlbumMigration_RekeyAlbumArtwork_ErrorsWhenOrphanMissing proves a bad
// or stale orphan id is a descriptive, non-panicking error, not a silent
// no-op or a crash.
func TestAlbumMigration_RekeyAlbumArtwork_ErrorsWhenOrphanMissing(t *testing.T) {
	t.Parallel()
	_, dao := newMigrationTestDB(t)

	targetAlbumID := "target-album-id-5"
	if err := dao.InsertAlbum(&cache.CachedAlbum{ID: targetAlbumID, Title: "T5", AlbumArtist: "X", URI: "u5", Source: "usb"}); err != nil {
		t.Fatalf("InsertAlbum: %v", err)
	}

	err := cache.RekeyAlbumArtwork(dao, "does-not-exist_artwork", targetAlbumID, false)
	if err == nil {
		t.Fatalf("expected error for missing orphan row, got nil")
	}
}

// TestAlbumMigration_RekeyAlbumArtwork_DryRunPerformsChecksButNoWrite
// proves dryRun validates preconditions and returns nil on success, but
// writes nothing.
func TestAlbumMigration_RekeyAlbumArtwork_DryRunPerformsChecksButNoWrite(t *testing.T) {
	t.Parallel()
	db, dao := newMigrationTestDB(t)

	orphanID := "orphan3_artwork"
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: orphanID, AlbumID: "old-gone-id-3", Type: "album",
		FilePath: "/tmp/orphan3.jpg", Source: "backfill", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("InsertArtwork: %v", err)
	}
	targetAlbumID := "target-album-id-3"
	if err := dao.InsertAlbum(&cache.CachedAlbum{ID: targetAlbumID, Title: "T3", AlbumArtist: "X", URI: "u3", Source: "usb"}); err != nil {
		t.Fatalf("InsertAlbum: %v", err)
	}

	beforeArtwork := dumpArtwork(t, db)
	beforeAlbum, err := dao.GetAlbum(targetAlbumID)
	if err != nil {
		t.Fatalf("GetAlbum(before): %v", err)
	}

	if err := cache.RekeyAlbumArtwork(dao, orphanID, targetAlbumID, true); err != nil {
		t.Fatalf("dry run: %v", err)
	}

	afterArtwork := dumpArtwork(t, db)
	afterAlbum, err := dao.GetAlbum(targetAlbumID)
	if err != nil {
		t.Fatalf("GetAlbum(after): %v", err)
	}

	if !reflect.DeepEqual(beforeArtwork, afterArtwork) {
		t.Fatalf("artwork table changed during dry run:\nbefore=%+v\nafter=%+v", beforeArtwork, afterArtwork)
	}
	if beforeAlbum.ArtworkID != afterAlbum.ArtworkID {
		t.Fatalf("album.ArtworkID changed during dry run: before=%q after=%q", beforeAlbum.ArtworkID, afterAlbum.ArtworkID)
	}
}
