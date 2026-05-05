package cache_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

func openBiosTestDB(t *testing.T) *cache.DB {
	t.Helper()
	dir := t.TempDir()
	db := cache.NewDB(filepath.Join(dir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestBiosDAO_AlbumBio_PutGetDelete(t *testing.T) {
	t.Parallel()
	db := openBiosTestDB(t)
	dao := db.BiosDAO()
	now := time.Now().Unix()

	// Initial Get -> miss
	got, ok, err := dao.GetAlbumBio("Miles Davis", "Kind of Blue")
	if err != nil {
		t.Fatalf("GetAlbumBio: %v", err)
	}
	if ok {
		t.Fatalf("expected miss, got hit: %+v", got)
	}

	// Put + read back
	in := cache.AlbumBio{
		Artist:    "Miles Davis",
		Album:     "Kind of Blue",
		Summary:   "Recorded in 1959, considered the bestselling jazz album of all time.",
		SourceURL: "https://en.wikipedia.org/wiki/Kind_of_Blue",
		FetchedAt: now,
		ExpiresAt: now + 90*86400,
	}
	if err := dao.PutAlbumBio(in); err != nil {
		t.Fatalf("PutAlbumBio: %v", err)
	}

	got, ok, err = dao.GetAlbumBio("Miles Davis", "Kind of Blue")
	if err != nil || !ok {
		t.Fatalf("GetAlbumBio after Put: ok=%v err=%v", ok, err)
	}
	if got.Summary != in.Summary || got.SourceURL != in.SourceURL {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, in)
	}

	// Case-insensitive normalization (artist + album)
	gotL, okL, _ := dao.GetAlbumBio("miles davis", "kind of blue")
	if !okL || gotL.Summary != in.Summary {
		t.Fatalf("expected normalized hit for lowercase keys, got ok=%v summary=%q", okL, gotL.Summary)
	}

	// Upsert overwrites the row, not creates a duplicate.
	updated := in
	updated.Summary = "Updated summary."
	if err := dao.PutAlbumBio(updated); err != nil {
		t.Fatalf("PutAlbumBio (update): %v", err)
	}
	gotU, _, _ := dao.GetAlbumBio("Miles Davis", "Kind of Blue")
	if gotU.Summary != "Updated summary." {
		t.Fatalf("upsert did not overwrite: %q", gotU.Summary)
	}

	// Delete
	if err := dao.DeleteAlbumBio("Miles Davis", "Kind of Blue"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, ok, _ = dao.GetAlbumBio("Miles Davis", "Kind of Blue")
	if ok {
		t.Fatalf("expected miss after delete, got hit")
	}
}

func TestBiosDAO_GetExpired_ReturnsHitButCallerCanCheck(t *testing.T) {
	t.Parallel()
	db := openBiosTestDB(t)
	dao := db.BiosDAO()
	past := time.Now().Unix() - 1

	if err := dao.PutAlbumBio(cache.AlbumBio{
		Artist: "X", Album: "Y", Summary: "old",
		FetchedAt: past, ExpiresAt: past,
	}); err != nil {
		t.Fatal(err)
	}

	got, ok, err := dao.GetAlbumBio("X", "Y")
	if err != nil || !ok {
		t.Fatalf("expected hit (caller decides freshness): ok=%v err=%v", ok, err)
	}
	if got.ExpiresAt > time.Now().Unix() {
		t.Fatalf("ExpiresAt should be in past, got %d", got.ExpiresAt)
	}
}

func TestBiosDAO_ArtistBio_PutGetDelete(t *testing.T) {
	t.Parallel()
	db := openBiosTestDB(t)
	dao := db.BiosDAO()
	now := time.Now().Unix()

	if err := dao.PutArtistBio(cache.ArtistBio{
		Artist: "Miles Davis", Summary: "American jazz trumpeter and composer.",
		SourceURL: "https://en.wikipedia.org/wiki/Miles_Davis",
		FetchedAt: now, ExpiresAt: now + 90*86400,
	}); err != nil {
		t.Fatal(err)
	}

	got, ok, err := dao.GetArtistBio("Miles Davis")
	if err != nil || !ok || got.Summary == "" {
		t.Fatalf("artist bio round-trip: ok=%v err=%v summary=%q", ok, err, got.Summary)
	}

	// Case-insensitive
	got2, ok2, _ := dao.GetArtistBio("miles davis")
	if !ok2 || got2.Summary == "" {
		t.Fatalf("artist case-insensitive miss: ok=%v summary=%q", ok2, got2.Summary)
	}

	if err := dao.DeleteArtistBio("Miles Davis"); err != nil {
		t.Fatal(err)
	}
	_, ok, _ = dao.GetArtistBio("Miles Davis")
	if ok {
		t.Fatalf("expected miss after delete")
	}
}

func TestBiosDAO_RequiresArtistAndAlbum(t *testing.T) {
	t.Parallel()
	db := openBiosTestDB(t)
	dao := db.BiosDAO()

	if err := dao.PutAlbumBio(cache.AlbumBio{Artist: "", Album: "Y"}); err == nil {
		t.Fatalf("expected error when artist is empty")
	}
	if err := dao.PutAlbumBio(cache.AlbumBio{Artist: "X", Album: ""}); err == nil {
		t.Fatalf("expected error when album is empty")
	}
	if err := dao.PutArtistBio(cache.ArtistBio{Artist: ""}); err == nil {
		t.Fatalf("expected error when artist is empty")
	}

	// Get with empty inputs should be a miss, not an error.
	if _, ok, err := dao.GetAlbumBio("", ""); err != nil || ok {
		t.Fatalf("Get with empty inputs should miss without error: ok=%v err=%v", ok, err)
	}
	if _, ok, err := dao.GetArtistBio(""); err != nil || ok {
		t.Fatalf("GetArtistBio empty should miss without error: ok=%v err=%v", ok, err)
	}
}
