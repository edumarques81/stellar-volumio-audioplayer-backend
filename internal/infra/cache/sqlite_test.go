package cache_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

func TestNewDB(t *testing.T) {
	db := cache.NewDB("")
	if db == nil {
		t.Error("NewDB should return a non-nil instance")
	}
}

func TestDBOpenClose(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "cache_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db := cache.NewDB(dbPath)

	// Open database
	if err := db.Open(); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file should exist after Open()")
	}

	// Close database
	if err := db.Close(); err != nil {
		t.Errorf("Failed to close database: %v", err)
	}
}

func TestDBGetStats(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "cache_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db := cache.NewDB(dbPath)

	if err := db.Open(); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	stats, err := db.GetStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	// Empty database should have zero counts
	if stats.AlbumCount != 0 {
		t.Errorf("Expected 0 albums, got %d", stats.AlbumCount)
	}
	if stats.ArtistCount != 0 {
		t.Errorf("Expected 0 artists, got %d", stats.ArtistCount)
	}
	if stats.TrackCount != 0 {
		t.Errorf("Expected 0 tracks, got %d", stats.TrackCount)
	}
	if stats.SchemaVersion != "1" {
		t.Errorf("Expected schema version '1', got '%s'", stats.SchemaVersion)
	}
}

func TestDAOInsertAndQueryAlbums(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "cache_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db := cache.NewDB(dbPath)

	if err := db.Open(); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	dao := cache.NewDAO(db)

	// Insert test albums
	album1 := &cache.CachedAlbum{
		ID:            "album1",
		Title:         "Test Album 1",
		AlbumArtist:   "Test Artist",
		URI:           "INTERNAL/Test Artist/Test Album 1",
		TrackCount:    10,
		TotalDuration: 3600,
		Source:        "local",
		Year:          2024,
		AddedAt:       time.Now(),
	}

	album2 := &cache.CachedAlbum{
		ID:            "album2",
		Title:         "Another Album",
		AlbumArtist:   "Another Artist",
		URI:           "NAS/Another Artist/Another Album",
		TrackCount:    8,
		TotalDuration: 2400,
		Source:        "nas",
		Year:          2023,
		AddedAt:       time.Now(),
	}

	if err := dao.InsertAlbum(album1); err != nil {
		t.Fatalf("Failed to insert album1: %v", err)
	}
	if err := dao.InsertAlbum(album2); err != nil {
		t.Fatalf("Failed to insert album2: %v", err)
	}

	// Query all albums
	albums, total, err := dao.QueryAlbums(cache.AlbumFilter{}, cache.SortAlphabetical, cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("Failed to query albums: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected 2 albums, got %d", total)
	}
	if len(albums) != 2 {
		t.Errorf("Expected 2 albums in result, got %d", len(albums))
	}

	// Query with NAS scope
	nasAlbums, nasTotal, err := dao.QueryAlbums(cache.AlbumFilter{Scope: "nas"}, cache.SortAlphabetical, cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("Failed to query NAS albums: %v", err)
	}

	if nasTotal != 1 {
		t.Errorf("Expected 1 NAS album, got %d", nasTotal)
	}
	if len(nasAlbums) != 1 {
		t.Errorf("Expected 1 NAS album in result, got %d", len(nasAlbums))
	}
	if nasAlbums[0].Title != "Another Album" {
		t.Errorf("Expected 'Another Album', got '%s'", nasAlbums[0].Title)
	}

	// Query with search
	searchAlbums, searchTotal, err := dao.QueryAlbums(cache.AlbumFilter{Query: "test"}, cache.SortAlphabetical, cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("Failed to search albums: %v", err)
	}

	if searchTotal != 1 {
		t.Errorf("Expected 1 album matching 'test', got %d", searchTotal)
	}
	if len(searchAlbums) != 1 {
		t.Errorf("Expected 1 album in search result, got %d", len(searchAlbums))
	}
}

func TestDAOInsertAndQueryArtists(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "cache_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db := cache.NewDB(dbPath)

	if err := db.Open(); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	dao := cache.NewDAO(db)

	// Insert test artists
	artist1 := &cache.CachedArtist{
		ID:         "artist1",
		Name:       "Test Artist",
		AlbumCount: 5,
		TrackCount: 50,
	}

	artist2 := &cache.CachedArtist{
		ID:         "artist2",
		Name:       "Another Artist",
		AlbumCount: 3,
		TrackCount: 30,
	}

	if err := dao.InsertArtist(artist1); err != nil {
		t.Fatalf("Failed to insert artist1: %v", err)
	}
	if err := dao.InsertArtist(artist2); err != nil {
		t.Fatalf("Failed to insert artist2: %v", err)
	}

	// Query all artists
	artists, total, err := dao.QueryArtists("", cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("Failed to query artists: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected 2 artists, got %d", total)
	}
	if len(artists) != 2 {
		t.Errorf("Expected 2 artists in result, got %d", len(artists))
	}

	// Query with search
	searchArtists, searchTotal, err := dao.QueryArtists("test", cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("Failed to search artists: %v", err)
	}

	if searchTotal != 1 {
		t.Errorf("Expected 1 artist matching 'test', got %d", searchTotal)
	}
	if len(searchArtists) != 1 {
		t.Errorf("Expected 1 artist in search result, got %d", len(searchArtists))
	}
}

func TestDAOInsertAndGetTracks(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "cache_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db := cache.NewDB(dbPath)

	if err := db.Open(); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	dao := cache.NewDAO(db)

	// Insert an album first
	album := &cache.CachedAlbum{
		ID:            "album1",
		Title:         "Test Album",
		AlbumArtist:   "Test Artist",
		URI:           "INTERNAL/Test Artist/Test Album",
		TrackCount:    2,
		TotalDuration: 600,
		Source:        "local",
	}
	if err := dao.InsertAlbum(album); err != nil {
		t.Fatalf("Failed to insert album: %v", err)
	}

	// Insert tracks
	track1 := &cache.CachedTrack{
		ID:          "track1",
		AlbumID:     "album1",
		Title:       "Track 1",
		Artist:      "Test Artist",
		URI:         "INTERNAL/Test Artist/Test Album/01 - Track 1.flac",
		TrackNumber: 1,
		DiscNumber:  1,
		Duration:    300,
		Source:      "local",
	}

	track2 := &cache.CachedTrack{
		ID:          "track2",
		AlbumID:     "album1",
		Title:       "Track 2",
		Artist:      "Test Artist",
		URI:         "INTERNAL/Test Artist/Test Album/02 - Track 2.flac",
		TrackNumber: 2,
		DiscNumber:  1,
		Duration:    300,
		Source:      "local",
	}

	if err := dao.InsertTrack(track1); err != nil {
		t.Fatalf("Failed to insert track1: %v", err)
	}
	if err := dao.InsertTrack(track2); err != nil {
		t.Fatalf("Failed to insert track2: %v", err)
	}

	// Get tracks by album
	tracks, err := dao.GetTracksByAlbum("album1")
	if err != nil {
		t.Fatalf("Failed to get tracks: %v", err)
	}

	if len(tracks) != 2 {
		t.Errorf("Expected 2 tracks, got %d", len(tracks))
	}

	// Verify order (should be by disc and track number)
	if tracks[0].Title != "Track 1" {
		t.Errorf("Expected 'Track 1' first, got '%s'", tracks[0].Title)
	}
	if tracks[1].Title != "Track 2" {
		t.Errorf("Expected 'Track 2' second, got '%s'", tracks[1].Title)
	}
}

func TestDBClear(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "cache_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db := cache.NewDB(dbPath)

	if err := db.Open(); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	dao := cache.NewDAO(db)

	// Insert data
	album := &cache.CachedAlbum{
		ID:          "album1",
		Title:       "Test Album",
		AlbumArtist: "Test Artist",
		URI:         "test",
		Source:      "local",
	}
	if err := dao.InsertAlbum(album); err != nil {
		t.Fatalf("Failed to insert album: %v", err)
	}

	// Verify data exists
	stats, _ := db.GetStats()
	if stats.AlbumCount != 1 {
		t.Errorf("Expected 1 album before clear, got %d", stats.AlbumCount)
	}

	// Clear database
	if err := db.Clear(); err != nil {
		t.Fatalf("Failed to clear database: %v", err)
	}

	// Verify data is gone
	stats, _ = db.GetStats()
	if stats.AlbumCount != 0 {
		t.Errorf("Expected 0 albums after clear, got %d", stats.AlbumCount)
	}
}

func TestPagination(t *testing.T) {
	pag := cache.NewPagination(1, 50)
	if pag.Page != 1 {
		t.Errorf("Expected page 1, got %d", pag.Page)
	}
	if pag.Limit != 50 {
		t.Errorf("Expected limit 50, got %d", pag.Limit)
	}
	if pag.Offset != 0 {
		t.Errorf("Expected offset 0, got %d", pag.Offset)
	}

	pag = cache.NewPagination(2, 50)
	if pag.Offset != 50 {
		t.Errorf("Expected offset 50 for page 2, got %d", pag.Offset)
	}

	pag = cache.NewPagination(0, 0)
	if pag.Page != 1 {
		t.Errorf("Expected page 1 for invalid input, got %d", pag.Page)
	}
	if pag.Limit != 50 {
		t.Errorf("Expected limit 50 for invalid input, got %d", pag.Limit)
	}

	pag = cache.NewPagination(1, 500)
	if pag.Limit != 200 {
		t.Errorf("Expected max limit 200, got %d", pag.Limit)
	}
}
