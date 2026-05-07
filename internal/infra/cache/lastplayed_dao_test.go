package cache_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

func openLastPlayedTestDB(t *testing.T) *cache.DB {
	t.Helper()
	dir := t.TempDir()
	db := cache.NewDB(filepath.Join(dir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestLastPlayedDAO_PutGet_Roundtrip(t *testing.T) {
	t.Parallel()
	db := openLastPlayedTestDB(t)
	dao := db.LastPlayedDAO()

	// Initial Get → miss
	if _, ok, err := dao.GetMostRecent(); err != nil || ok {
		t.Fatalf("expected miss, got ok=%v err=%v", ok, err)
	}

	now := time.Now().Unix()
	in := cache.LastPlayedAlbum{
		Artist:       "Miles Davis",
		Album:        "Kind of Blue",
		AlbumArt:     "/albumart?path=miles/kob/track1.flac",
		TrackURI:     "NAS/Miles Davis/Kind of Blue/01.flac",
		TrackType:    "flac",
		SampleRate:   "96000",
		BitDepth:     "24",
		LastPlayedAt: now,
	}
	if err := dao.Put(in); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok, err := dao.GetMostRecent()
	if err != nil {
		t.Fatalf("GetMostRecent: %v", err)
	}
	if !ok {
		t.Fatalf("expected hit after Put")
	}
	if got.Artist != in.Artist || got.Album != in.Album || got.AlbumArt != in.AlbumArt ||
		got.TrackURI != in.TrackURI || got.TrackType != in.TrackType ||
		got.SampleRate != in.SampleRate || got.BitDepth != in.BitDepth ||
		got.LastPlayedAt != in.LastPlayedAt {
		t.Fatalf("roundtrip mismatch:\n  in=%+v\n got=%+v", in, got)
	}
}

func TestLastPlayedDAO_Put_RequiresArtistAndAlbum(t *testing.T) {
	t.Parallel()
	db := openLastPlayedTestDB(t)
	dao := db.LastPlayedDAO()

	if err := dao.Put(cache.LastPlayedAlbum{Album: "x", LastPlayedAt: 1}); err == nil {
		t.Fatalf("expected error on empty artist")
	}
	if err := dao.Put(cache.LastPlayedAlbum{Artist: "x", LastPlayedAt: 1}); err == nil {
		t.Fatalf("expected error on empty album")
	}
	if err := dao.Put(cache.LastPlayedAlbum{Artist: "x", Album: "y"}); err == nil {
		t.Fatalf("expected error on zero LastPlayedAt")
	}
}

func TestLastPlayedDAO_GetMostRecent_OrdersByTimestamp(t *testing.T) {
	t.Parallel()
	db := openLastPlayedTestDB(t)
	dao := db.LastPlayedDAO()

	older := cache.LastPlayedAlbum{Artist: "Bach", Album: "Goldberg Variations", LastPlayedAt: 1000}
	newer := cache.LastPlayedAlbum{Artist: "Coltrane", Album: "A Love Supreme", LastPlayedAt: 2000}

	if err := dao.Put(older); err != nil {
		t.Fatalf("Put older: %v", err)
	}
	if err := dao.Put(newer); err != nil {
		t.Fatalf("Put newer: %v", err)
	}

	got, ok, err := dao.GetMostRecent()
	if err != nil || !ok {
		t.Fatalf("GetMostRecent: ok=%v err=%v", ok, err)
	}
	if got.Album != "A Love Supreme" {
		t.Fatalf("expected newest album, got %q", got.Album)
	}
}

func TestLastPlayedDAO_Put_ReplacesSameAlbum(t *testing.T) {
	t.Parallel()
	db := openLastPlayedTestDB(t)
	dao := db.LastPlayedDAO()

	first := cache.LastPlayedAlbum{
		Artist: "Brad Mehldau", Album: "Live in Tokyo",
		TrackURI: "tokyo/01.flac", LastPlayedAt: 1000,
	}
	second := cache.LastPlayedAlbum{
		Artist: "Brad Mehldau", Album: "Live in Tokyo",
		TrackURI: "tokyo/05.flac", LastPlayedAt: 2000,
	}
	if err := dao.Put(first); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	if err := dao.Put(second); err != nil {
		t.Fatalf("Put second: %v", err)
	}

	got, ok, err := dao.GetMostRecent()
	if err != nil || !ok {
		t.Fatalf("GetMostRecent: ok=%v err=%v", ok, err)
	}
	if got.LastPlayedAt != 2000 || got.TrackURI != "tokyo/05.flac" {
		t.Fatalf("expected upsert; got %+v", got)
	}

	// Ensure the table did not grow to two rows.
	var count int
	if err := db.DB().QueryRow("SELECT COUNT(*) FROM last_played_album").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected single-row upsert, got %d rows", count)
	}
}

func TestLastPlayedDAO_NormalizesArtistAlbumCase(t *testing.T) {
	t.Parallel()
	db := openLastPlayedTestDB(t)
	dao := db.LastPlayedDAO()

	first := cache.LastPlayedAlbum{Artist: "Miles Davis", Album: "Kind of Blue", LastPlayedAt: 1000}
	dup := cache.LastPlayedAlbum{Artist: "miles davis", Album: "KIND OF BLUE", LastPlayedAt: 2000}
	if err := dao.Put(first); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	if err := dao.Put(dup); err != nil {
		t.Fatalf("Put dup: %v", err)
	}

	var count int
	if err := db.DB().QueryRow("SELECT COUNT(*) FROM last_played_album").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected key collision (case-insensitive), got %d rows", count)
	}
}
