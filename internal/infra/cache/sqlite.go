// Package cache provides a SQLite-based caching layer for library metadata.
package cache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

const (
	// CurrentSchemaVersion is the current database schema version.
	CurrentSchemaVersion = "1"

	// DefaultDBPath is the default path for the cache database.
	DefaultDBPath = "data/library.db"
)

// DB represents the SQLite cache database.
type DB struct {
	mu       sync.RWMutex
	db       *sql.DB
	path     string
	isBuilding bool
	buildProgress int
}

// NewDB creates a new cache database instance.
func NewDB(path string) *DB {
	if path == "" {
		path = DefaultDBPath
	}
	return &DB{
		path: path,
	}
}

// Open opens the database and initializes the schema.
func (d *DB) Open() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Ensure directory exists
	dir := filepath.Dir(d.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Open database
	db, err := sql.Open("sqlite3", d.path+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("failed to open cache database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite only supports one writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	d.db = db

	// Initialize schema
	if err := d.initSchema(); err != nil {
		d.db.Close()
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	log.Info().Str("path", d.path).Msg("Cache database opened")
	return nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.db != nil {
		err := d.db.Close()
		d.db = nil
		return err
	}
	return nil
}

// initSchema initializes the database schema.
func (d *DB) initSchema() error {
	// Get current schema version
	currentVersion := d.getSchemaVersion()

	if currentVersion == "" {
		// Fresh database, create all tables
		if err := d.createSchema(); err != nil {
			return err
		}
		return d.setMeta("schema_version", CurrentSchemaVersion)
	}

	// Check if migration is needed
	if currentVersion != CurrentSchemaVersion {
		log.Info().
			Str("current", currentVersion).
			Str("target", CurrentSchemaVersion).
			Msg("Migrating cache schema")
		// Add migration logic here when schema changes
		return d.setMeta("schema_version", CurrentSchemaVersion)
	}

	return nil
}

// createSchema creates all database tables.
func (d *DB) createSchema() error {
	schema := `
	-- Albums table
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
		added_at TEXT,
		last_played TEXT,
		artwork_id TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP
	);

	-- Artists table
	CREATE TABLE IF NOT EXISTS artists (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		album_count INTEGER DEFAULT 0,
		track_count INTEGER DEFAULT 0,
		artwork_id TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP
	);

	-- Tracks table
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
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (album_id) REFERENCES albums(id) ON DELETE CASCADE
	);

	-- Artwork cache metadata
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

	-- Radio stations
	CREATE TABLE IF NOT EXISTS radio_stations (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		uri TEXT NOT NULL,
		icon TEXT,
		genre TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP
	);

	-- Cache metadata
	CREATE TABLE IF NOT EXISTS cache_meta (
		key TEXT PRIMARY KEY,
		value TEXT,
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP
	);

	-- Indexes for album queries
	CREATE INDEX IF NOT EXISTS idx_albums_artist ON albums(album_artist);
	CREATE INDEX IF NOT EXISTS idx_albums_source ON albums(source);
	CREATE INDEX IF NOT EXISTS idx_albums_year ON albums(year);
	CREATE INDEX IF NOT EXISTS idx_albums_added ON albums(added_at DESC);
	CREATE INDEX IF NOT EXISTS idx_albums_title ON albums(title COLLATE NOCASE);

	-- Indexes for artist queries
	CREATE INDEX IF NOT EXISTS idx_artists_name ON artists(name COLLATE NOCASE);

	-- Indexes for track queries
	CREATE INDEX IF NOT EXISTS idx_tracks_album ON tracks(album_id);
	CREATE INDEX IF NOT EXISTS idx_tracks_artist ON tracks(artist COLLATE NOCASE);

	-- Indexes for artwork queries
	CREATE INDEX IF NOT EXISTS idx_artwork_album ON artwork(album_id);
	CREATE INDEX IF NOT EXISTS idx_artwork_artist ON artwork(artist_id);
	CREATE INDEX IF NOT EXISTS idx_artwork_expires ON artwork(expires_at);
	`

	_, err := d.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	log.Info().Msg("Cache schema created")
	return nil
}

// getSchemaVersion returns the current schema version.
func (d *DB) getSchemaVersion() string {
	var version string
	err := d.db.QueryRow("SELECT value FROM cache_meta WHERE key = 'schema_version'").Scan(&version)
	if err != nil {
		return ""
	}
	return version
}

// setMeta sets a metadata value.
func (d *DB) setMeta(key, value string) error {
	_, err := d.db.Exec(`
		INSERT INTO cache_meta (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`, key, value, time.Now().Format(time.RFC3339), value, time.Now().Format(time.RFC3339))
	return err
}

// getMeta gets a metadata value.
func (d *DB) getMeta(key string) (string, error) {
	var value string
	err := d.db.QueryRow("SELECT value FROM cache_meta WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// GetStats returns cache statistics.
func (d *DB) GetStats() (*CacheStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.db == nil {
		return nil, fmt.Errorf("database not open")
	}

	stats := &CacheStats{
		IsBuilding:    d.isBuilding,
		BuildProgress: d.buildProgress,
	}

	// Get counts
	var err error
	err = d.db.QueryRow("SELECT COUNT(*) FROM albums").Scan(&stats.AlbumCount)
	if err != nil {
		return nil, err
	}

	err = d.db.QueryRow("SELECT COUNT(*) FROM artists").Scan(&stats.ArtistCount)
	if err != nil {
		return nil, err
	}

	err = d.db.QueryRow("SELECT COUNT(*) FROM tracks").Scan(&stats.TrackCount)
	if err != nil {
		return nil, err
	}

	err = d.db.QueryRow("SELECT COUNT(*) FROM artwork WHERE file_path IS NOT NULL").Scan(&stats.ArtworkCount)
	if err != nil {
		return nil, err
	}

	err = d.db.QueryRow("SELECT COUNT(*) FROM albums WHERE artwork_id IS NULL").Scan(&stats.ArtworkMissing)
	if err != nil {
		return nil, err
	}

	err = d.db.QueryRow("SELECT COUNT(*) FROM radio_stations").Scan(&stats.RadioCount)
	if err != nil {
		return nil, err
	}

	// Get metadata
	stats.SchemaVersion, _ = d.getMeta("schema_version")

	lastBuild, _ := d.getMeta("last_full_build")
	if lastBuild != "" {
		stats.LastFullBuild, _ = time.Parse(time.RFC3339, lastBuild)
	}

	lastUpdated, _ := d.getMeta("last_updated")
	if lastUpdated != "" {
		stats.LastUpdated, _ = time.Parse(time.RFC3339, lastUpdated)
	}

	return stats, nil
}

// SetBuildingState sets the cache building state.
func (d *DB) SetBuildingState(building bool, progress int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.isBuilding = building
	d.buildProgress = progress
}

// BeginTx starts a new transaction.
func (d *DB) BeginTx() (*sql.Tx, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.db == nil {
		return nil, fmt.Errorf("database not open")
	}

	return d.db.Begin()
}

// Clear removes all data from the cache (but keeps schema).
func (d *DB) Clear() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.db == nil {
		return fmt.Errorf("database not open")
	}

	tables := []string{"tracks", "albums", "artists", "artwork", "radio_stations"}
	for _, table := range tables {
		if _, err := d.db.Exec("DELETE FROM " + table); err != nil {
			return fmt.Errorf("failed to clear %s: %w", table, err)
		}
	}

	// Update metadata
	now := time.Now().Format(time.RFC3339)
	d.setMeta("last_updated", now)

	log.Info().Msg("Cache cleared")
	return nil
}

// MarkBuildComplete marks the cache build as complete.
func (d *DB) MarkBuildComplete() error {
	now := time.Now().Format(time.RFC3339)
	if err := d.setMeta("last_full_build", now); err != nil {
		return err
	}
	return d.setMeta("last_updated", now)
}

// DB returns the underlying sql.DB for direct queries.
// Use with caution - prefer the DAO methods.
func (d *DB) DB() *sql.DB {
	return d.db
}
