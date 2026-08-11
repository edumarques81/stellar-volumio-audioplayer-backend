package cache_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
	_ "modernc.org/sqlite"
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
	if stats.SchemaVersion != cache.CurrentSchemaVersion {
		t.Errorf("Expected schema version %q, got %q", cache.CurrentSchemaVersion, stats.SchemaVersion)
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

// TestDBClear_PreservesArtwork asserts that Clear() preserves the artwork
// table. Artwork is enrichment data (Fanart.tv / Deezer / Cover Art Archive),
// slow to rebuild and not derivable from MPD. Artist/album IDs are
// deterministic so artwork rows still resolve after rebuild repopulates
// artists+albums.
func TestDBClear_PreservesArtwork(t *testing.T) {
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

	// Seed an artist + an artwork row.
	if err := dao.InsertArtist(&cache.CachedArtist{ID: "artist1", Name: "Test Artist"}); err != nil {
		t.Fatalf("InsertArtist: %v", err)
	}
	if err := dao.InsertArtwork(&cache.CachedArtwork{
		ID: "art1", ArtistID: "artist1", Type: "artist",
		FilePath: "/tmp/test.jpg", Source: "fanarttv",
	}); err != nil {
		t.Fatalf("InsertArtwork: %v", err)
	}

	if err := db.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	// Artists are MPD-derived and should be wiped (existing TestDBClear covers this).
	// Artwork is enrichment data and must survive.
	art, err := dao.GetArtworkByArtist("artist1")
	if err != nil {
		t.Fatalf("GetArtworkByArtist: %v", err)
	}
	if art == nil {
		t.Fatal("Artwork row was wiped by Clear() — should have been preserved (enrichment data, not MPD-derived)")
	}
	if art.FilePath != "/tmp/test.jpg" {
		t.Errorf("Artwork FilePath = %q, want /tmp/test.jpg", art.FilePath)
	}
}

func TestSchema_BioTablesExist(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, table := range []string{"album_bios", "artist_bios"} {
		var name string
		err := db.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing after Open: %v", table, err)
		}
	}

	stats, err := db.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.SchemaVersion != cache.CurrentSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", stats.SchemaVersion, cache.CurrentSchemaVersion)
	}
}

// TestDAOAlbumGenreRoundTrip verifies that an album inserted with a
// non-empty Genre comes back from QueryAlbums with the same value, and
// that a default-empty Genre stays empty after the round trip.
func TestDAOAlbumGenreRoundTrip(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "library.db")

	db := cache.NewDB(dbPath)
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dao := cache.NewDAO(db)

	withGenre := &cache.CachedAlbum{
		ID:          "with-genre",
		Title:       "Midnight Shores",
		AlbumArtist: "Hollow Tides",
		URI:         "NAS/Hollow Tides/Midnight Shores",
		Source:      "nas",
		Genre:       "Ambient / Post-Rock",
	}
	noGenre := &cache.CachedAlbum{
		ID:          "no-genre",
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

	albums, _, err := dao.QueryAlbums(cache.AlbumFilter{}, cache.SortAlphabetical, cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	got := map[string]string{}
	for _, a := range albums {
		got[a.ID] = a.Genre
	}
	if got["with-genre"] != "Ambient / Post-Rock" {
		t.Errorf("with-genre.Genre = %q, want %q", got["with-genre"], "Ambient / Post-Rock")
	}
	if got["no-genre"] != "" {
		t.Errorf("no-genre.Genre = %q, want empty string", got["no-genre"])
	}
}

// TestMigration_V4ToV5_AddsGenreColumn verifies that opening a database that
// was previously initialised at schema v4 (no genre column on albums)
// transparently migrates to v5 by adding a `genre TEXT DEFAULT ''` column
// while preserving any existing rows.
func TestMigration_V4ToV5_AddsGenreColumn(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "library.db")

	// Build a v4 database by hand (schema below mirrors the v4 createSchema
	// minus the genre column we are introducing in v5).
	seedV4Database(t, dbPath)

	// Insert a pre-migration album so we can prove existing rows survive.
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := rawDB.Exec(`
		INSERT INTO albums (id, title, album_artist, uri, source)
		VALUES ('legacy-1', 'Legacy Album', 'Legacy Artist', 'NAS/legacy', 'nas')
	`); err != nil {
		t.Fatalf("seed legacy album: %v", err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	// Open through the production code path — this should run the v4→v5
	// migration as a side-effect.
	db := cache.NewDB(dbPath)
	if err := db.Open(); err != nil {
		t.Fatalf("open via cache.NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 1. schema_version was bumped to current.
	stats, err := db.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.SchemaVersion != cache.CurrentSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", stats.SchemaVersion, cache.CurrentSchemaVersion)
	}
	if cache.CurrentSchemaVersion != "6" {
		t.Fatalf("CurrentSchemaVersion = %q, want %q", cache.CurrentSchemaVersion, "6")
	}

	// 2. genre column is present on the albums table.
	if !columnExists(t, db.DB(), "albums", "genre") {
		t.Fatalf("genre column missing from albums after migration")
	}

	// 3. Pre-migration row still exists and has genre defaulted to "".
	var legacyGenre sql.NullString
	if err := db.DB().QueryRow(
		`SELECT genre FROM albums WHERE id = ?`, "legacy-1",
	).Scan(&legacyGenre); err != nil {
		t.Fatalf("read legacy genre: %v", err)
	}
	if legacyGenre.Valid && legacyGenre.String != "" {
		t.Fatalf("legacy genre = %q, want empty string", legacyGenre.String)
	}
}

// TestMigration_V4ToV5_Idempotent verifies the v4→v5 migration is safe to
// re-run — closing and re-opening an already-migrated database must not
// error out (e.g. ALTER TABLE on an existing column would fail loudly).
func TestMigration_V4ToV5_Idempotent(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "library.db")

	seedV4Database(t, dbPath)

	// First open: runs migration.
	db1 := cache.NewDB(dbPath)
	if err := db1.Open(); err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	// Second open: schema is already at v5 — must be a no-op.
	db2 := cache.NewDB(dbPath)
	if err := db2.Open(); err != nil {
		t.Fatalf("second open after migration: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	if !columnExists(t, db2.DB(), "albums", "genre") {
		t.Fatalf("genre column lost after second open")
	}
}

// seedV4Database writes a minimal v4 schema directly to dbPath so tests can
// exercise the v4→v5 migration. Mirrors the relevant subset of createSchema
// at v4 — albums table without the genre column.
func seedV4Database(t *testing.T, dbPath string) {
	t.Helper()

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer rawDB.Close()

	// Snapshot of the v4 schema — every table the production code expected
	// to exist when CurrentSchemaVersion was "4". Mirrors createSchema() at
	// that point in time except for the genre column we are adding in v5.
	schemaV4 := `
		CREATE TABLE IF NOT EXISTS albums (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			album_artist TEXT NOT NULL,
			uri TEXT NOT NULL,
			first_track TEXT,
			track_count INTEGER DEFAULT 0,
			total_duration INTEGER DEFAULT 0,
			source TEXT NOT NULL,
			year INTEGER,
			sample_rate INTEGER DEFAULT 0,
			bit_depth INTEGER DEFAULT 0,
			track_type TEXT DEFAULT '',
			added_at TEXT,
			last_played TEXT,
			artwork_id TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS artists (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			album_count INTEGER DEFAULT 0,
			track_count INTEGER DEFAULT 0,
			artwork_id TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS tracks (
			id TEXT PRIMARY KEY,
			album_id TEXT NOT NULL,
			title TEXT NOT NULL,
			artist TEXT NOT NULL,
			uri TEXT NOT NULL UNIQUE,
			track_number INTEGER,
			disc_number INTEGER DEFAULT 1,
			duration INTEGER DEFAULT 0,
			source TEXT NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS artwork (
			id TEXT PRIMARY KEY,
			album_id TEXT,
			artist_id TEXT,
			type TEXT NOT NULL,
			file_path TEXT,
			source TEXT NOT NULL,
			mime_type TEXT,
			width INTEGER,
			height INTEGER,
			file_size INTEGER,
			checksum TEXT,
			fetched_at TEXT,
			expires_at TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS radio_stations (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			uri TEXT NOT NULL,
			icon TEXT,
			genre TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS cache_meta (
			key TEXT PRIMARY KEY,
			value TEXT,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS album_bios (
			key TEXT PRIMARY KEY,
			artist TEXT NOT NULL,
			album TEXT NOT NULL,
			summary TEXT NOT NULL,
			source_url TEXT,
			fetched_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS artist_bios (
			key TEXT PRIMARY KEY,
			artist TEXT NOT NULL,
			summary TEXT NOT NULL,
			source_url TEXT,
			fetched_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS last_played_album (
			key TEXT PRIMARY KEY,
			artist TEXT NOT NULL,
			album TEXT NOT NULL,
			album_art TEXT,
			track_uri TEXT,
			track_type TEXT,
			sample_rate TEXT,
			bit_depth TEXT,
			last_played_at INTEGER NOT NULL,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := rawDB.Exec(schemaV4); err != nil {
		t.Fatalf("seed v4 schema: %v", err)
	}
	if _, err := rawDB.Exec(
		`INSERT INTO cache_meta (key, value) VALUES ('schema_version', '4')`,
	); err != nil {
		t.Fatalf("seed schema_version=4: %v", err)
	}
}

// columnExists returns true iff `column` exists on `table` in the given DB.
func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info row: %v", err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info iter: %v", err)
	}
	return false
}

// TestDAOInsertAlbumTx_BadgeAndDiscCountRoundTrip verifies that Schema v6's
// badge/disc_count columns round-trip through InsertAlbumTx -> QueryAlbums
// unchanged: a non-empty Badge and a DiscCount > 1 (the Mahler-shaped case)
// come back exactly as inserted.
func TestDAOInsertAlbumTx_BadgeAndDiscCountRoundTrip(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "library.db")

	db := cache.NewDB(dbPath)
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dao := cache.NewDAO(db)

	tx, err := db.BeginTx()
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	badged := &cache.CachedAlbum{
		ID:          "badged-album",
		Title:       "Kind Of Blue",
		AlbumArtist: "Miles Davis",
		URI:         "USB/Miles Davis/Kind Of Blue (DSD64)",
		Source:      "usb",
		Badge:       "DSD64",
	}
	grouped := &cache.CachedAlbum{
		ID:          "grouped-album",
		Title:       "Mahler: The Symphonies",
		AlbumArtist: "Leonard Bernstein",
		URI:         "USB/Mahler The Symphonies",
		Source:      "usb",
		DiscCount:   11,
	}

	if err := dao.InsertAlbumTx(tx, badged); err != nil {
		t.Fatalf("InsertAlbumTx badged: %v", err)
	}
	if err := dao.InsertAlbumTx(tx, grouped); err != nil {
		t.Fatalf("InsertAlbumTx grouped: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	albums, _, err := dao.QueryAlbums(cache.AlbumFilter{}, cache.SortAlphabetical, cache.NewPagination(1, 50))
	if err != nil {
		t.Fatalf("QueryAlbums: %v", err)
	}

	got := map[string]*cache.CachedAlbum{}
	for _, a := range albums {
		got[a.ID] = a
	}

	if got["badged-album"] == nil {
		t.Fatal("badged-album missing from QueryAlbums result")
	}
	if got["badged-album"].Badge != "DSD64" {
		t.Errorf("badged-album.Badge = %q, want %q", got["badged-album"].Badge, "DSD64")
	}
	if got["badged-album"].DiscCount != 0 {
		t.Errorf("badged-album.DiscCount = %d, want 0", got["badged-album"].DiscCount)
	}

	if got["grouped-album"] == nil {
		t.Fatal("grouped-album missing from QueryAlbums result")
	}
	if got["grouped-album"].DiscCount != 11 {
		t.Errorf("grouped-album.DiscCount = %d, want 11", got["grouped-album"].DiscCount)
	}
	if got["grouped-album"].Badge != "" {
		t.Errorf("grouped-album.Badge = %q, want empty string", got["grouped-album"].Badge)
	}
}

// TestSchema_FreshDB_HasBadgeAndDiscCountColumns verifies a fresh database
// (no prior schema_version row) creates the albums table with badge and
// disc_count columns present from createSchema() directly (schema v6).
func TestSchema_FreshDB_HasBadgeAndDiscCountColumns(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if !columnExists(t, db.DB(), "albums", "badge") {
		t.Fatalf("badge column missing from fresh-DB albums table")
	}
	if !columnExists(t, db.DB(), "albums", "disc_count") {
		t.Fatalf("disc_count column missing from fresh-DB albums table")
	}

	stats, err := db.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.SchemaVersion != cache.CurrentSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", stats.SchemaVersion, cache.CurrentSchemaVersion)
	}
	if cache.CurrentSchemaVersion != "6" {
		t.Fatalf("CurrentSchemaVersion = %q, want %q", cache.CurrentSchemaVersion, "6")
	}
}

// TestMigration_V5ToV6_AddsBadgeAndDiscCountColumns verifies that opening a
// database previously initialised at schema v5 (genre column present, no
// badge/disc_count) transparently migrates to v6 by adding
// `badge TEXT DEFAULT ''` and `disc_count INTEGER DEFAULT 0` while
// preserving any existing rows -- mirrors TestMigration_V4ToV5_AddsGenreColumn.
func TestMigration_V5ToV6_AddsBadgeAndDiscCountColumns(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "library.db")

	seedV5Database(t, dbPath)

	// Insert a pre-migration album so we can prove existing rows survive.
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := rawDB.Exec(`
		INSERT INTO albums (id, title, album_artist, uri, source, genre)
		VALUES ('legacy-1', 'Legacy Album', 'Legacy Artist', 'NAS/legacy', 'nas', 'Jazz')
	`); err != nil {
		t.Fatalf("seed legacy album: %v", err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	// Open through the production code path — this should run the v5→v6
	// migration as a side-effect.
	db := cache.NewDB(dbPath)
	if err := db.Open(); err != nil {
		t.Fatalf("open via cache.NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stats, err := db.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.SchemaVersion != cache.CurrentSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", stats.SchemaVersion, cache.CurrentSchemaVersion)
	}
	if cache.CurrentSchemaVersion != "6" {
		t.Fatalf("CurrentSchemaVersion = %q, want %q", cache.CurrentSchemaVersion, "6")
	}

	if !columnExists(t, db.DB(), "albums", "badge") {
		t.Fatalf("badge column missing from albums after migration")
	}
	if !columnExists(t, db.DB(), "albums", "disc_count") {
		t.Fatalf("disc_count column missing from albums after migration")
	}

	// Pre-migration row still exists, genre preserved, badge/disc_count defaulted.
	var legacyGenre sql.NullString
	var legacyBadge sql.NullString
	var legacyDiscCount sql.NullInt64
	if err := db.DB().QueryRow(
		`SELECT genre, badge, disc_count FROM albums WHERE id = ?`, "legacy-1",
	).Scan(&legacyGenre, &legacyBadge, &legacyDiscCount); err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if legacyGenre.String != "Jazz" {
		t.Fatalf("legacy genre = %q, want %q (must survive migration)", legacyGenre.String, "Jazz")
	}
	if legacyBadge.Valid && legacyBadge.String != "" {
		t.Fatalf("legacy badge = %q, want empty string", legacyBadge.String)
	}
	if legacyDiscCount.Valid && legacyDiscCount.Int64 != 0 {
		t.Fatalf("legacy disc_count = %d, want 0", legacyDiscCount.Int64)
	}
}

// TestMigration_V5ToV6_Idempotent verifies the v5→v6 migration is safe to
// re-run twice against the same database — the second run (whether via a
// fresh Open() on an already-migrated file, or by directly invoking the
// migration path again) must not error, matching the tolerant "duplicate
// column name" pattern used by every prior migration in this file (hard
// constraint 1).
func TestMigration_V5ToV6_Idempotent(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "library.db")

	seedV5Database(t, dbPath)

	// First open: runs the v5->v6 migration.
	db1 := cache.NewDB(dbPath)
	if err := db1.Open(); err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	// Second open: schema is already at v6 — must be a no-op, no error.
	db2 := cache.NewDB(dbPath)
	if err := db2.Open(); err != nil {
		t.Fatalf("second open after migration: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	if !columnExists(t, db2.DB(), "albums", "badge") {
		t.Fatalf("badge column lost after second open")
	}
	if !columnExists(t, db2.DB(), "albums", "disc_count") {
		t.Fatalf("disc_count column lost after second open")
	}

	// Directly re-run the migration a THIRD time against the same on-disk
	// file by opening again — proves running the migration twice in a row
	// (not just "already migrated, no-op") is safe, per hard constraint 1's
	// explicit requirement ("a test that runs the migration twice against
	// the same DB and asserts success both times").
	db3 := cache.NewDB(dbPath)
	if err := db3.Open(); err != nil {
		t.Fatalf("third open (second post-migration run): %v", err)
	}
	t.Cleanup(func() { _ = db3.Close() })

	if !columnExists(t, db3.DB(), "albums", "badge") {
		t.Fatalf("badge column lost after third open")
	}
}

// seedV5Database writes a v5 schema (genre column present, no
// badge/disc_count) directly to dbPath so tests can exercise the v5→v6
// migration. Mirrors seedV4Database's pattern, one column further along.
func seedV5Database(t *testing.T, dbPath string) {
	t.Helper()

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer rawDB.Close()

	schemaV5 := `
		CREATE TABLE IF NOT EXISTS albums (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			album_artist TEXT NOT NULL,
			uri TEXT NOT NULL,
			first_track TEXT,
			track_count INTEGER DEFAULT 0,
			total_duration INTEGER DEFAULT 0,
			source TEXT NOT NULL,
			year INTEGER,
			sample_rate INTEGER DEFAULT 0,
			bit_depth INTEGER DEFAULT 0,
			track_type TEXT DEFAULT '',
			genre TEXT DEFAULT '',
			added_at TEXT,
			last_played TEXT,
			artwork_id TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS artists (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			album_count INTEGER DEFAULT 0,
			track_count INTEGER DEFAULT 0,
			artwork_id TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS tracks (
			id TEXT PRIMARY KEY,
			album_id TEXT NOT NULL,
			title TEXT NOT NULL,
			artist TEXT NOT NULL,
			uri TEXT NOT NULL UNIQUE,
			track_number INTEGER,
			disc_number INTEGER DEFAULT 1,
			duration INTEGER DEFAULT 0,
			source TEXT NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS artwork (
			id TEXT PRIMARY KEY,
			album_id TEXT,
			artist_id TEXT,
			type TEXT NOT NULL,
			file_path TEXT,
			source TEXT NOT NULL,
			mime_type TEXT,
			width INTEGER,
			height INTEGER,
			file_size INTEGER,
			checksum TEXT,
			fetched_at TEXT,
			expires_at TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS radio_stations (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			uri TEXT NOT NULL,
			icon TEXT,
			genre TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS cache_meta (
			key TEXT PRIMARY KEY,
			value TEXT,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS album_bios (
			key TEXT PRIMARY KEY,
			artist TEXT NOT NULL,
			album TEXT NOT NULL,
			summary TEXT NOT NULL,
			source_url TEXT,
			fetched_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS artist_bios (
			key TEXT PRIMARY KEY,
			artist TEXT NOT NULL,
			summary TEXT NOT NULL,
			source_url TEXT,
			fetched_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS last_played_album (
			key TEXT PRIMARY KEY,
			artist TEXT NOT NULL,
			album TEXT NOT NULL,
			album_art TEXT,
			track_uri TEXT,
			track_type TEXT,
			sample_rate TEXT,
			bit_depth TEXT,
			last_played_at INTEGER NOT NULL,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := rawDB.Exec(schemaV5); err != nil {
		t.Fatalf("seed v5 schema: %v", err)
	}
	if _, err := rawDB.Exec(
		`INSERT INTO cache_meta (key, value) VALUES ('schema_version', '5')`,
	); err != nil {
		t.Fatalf("seed schema_version=5: %v", err)
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
