package cache

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

// AlbumBio is one cached album-bio row.
type AlbumBio struct {
	Artist    string
	Album     string
	Summary   string
	SourceURL string
	FetchedAt int64 // unix seconds
	ExpiresAt int64 // unix seconds
}

// ArtistBio is one cached artist-bio row.
type ArtistBio struct {
	Artist    string
	Summary   string
	SourceURL string
	FetchedAt int64
	ExpiresAt int64
}

// BiosDAO provides CRUD over album_bios + artist_bios tables.
type BiosDAO struct {
	db *sql.DB
}

// BiosDAO returns a DAO bound to this DB. Safe to call repeatedly.
func (d *DB) BiosDAO() *BiosDAO {
	return &BiosDAO{db: d.db}
}

// normalizeBioKey lower-cases and trims each part, then joins with "|"
// so "Miles Davis" / "miles davis" resolve to the same row.
func normalizeBioKey(parts ...string) string {
	clean := make([]string, len(parts))
	for i, p := range parts {
		clean[i] = strings.ToLower(strings.TrimSpace(p))
	}
	return strings.Join(clean, "|")
}

// PutAlbumBio upserts a row keyed on the normalized "artist|album".
func (b *BiosDAO) PutAlbumBio(bio AlbumBio) error {
	if strings.TrimSpace(bio.Artist) == "" || strings.TrimSpace(bio.Album) == "" {
		return errors.New("PutAlbumBio: artist and album required")
	}
	key := normalizeBioKey(bio.Artist, bio.Album)
	_, err := b.db.Exec(
		`INSERT INTO album_bios (key, artist, album, summary, source_url, fetched_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		   summary    = excluded.summary,
		   source_url = excluded.source_url,
		   fetched_at = excluded.fetched_at,
		   expires_at = excluded.expires_at`,
		key, bio.Artist, bio.Album, bio.Summary, bio.SourceURL, bio.FetchedAt, bio.ExpiresAt,
	)
	if err != nil {
		log.Error().Err(err).Str("key", key).Msg("PutAlbumBio failed")
		return fmt.Errorf("PutAlbumBio: %w", err)
	}
	return nil
}

// GetAlbumBio looks up a cached album bio.
// Returns (zero, false, nil) on miss; the caller decides freshness via
// the returned ExpiresAt — DAO never filters by expiry.
func (b *BiosDAO) GetAlbumBio(artist, album string) (AlbumBio, bool, error) {
	if strings.TrimSpace(artist) == "" || strings.TrimSpace(album) == "" {
		return AlbumBio{}, false, nil
	}
	key := normalizeBioKey(artist, album)
	var bio AlbumBio
	var sourceURL sql.NullString
	err := b.db.QueryRow(
		`SELECT artist, album, summary, source_url, fetched_at, expires_at
		 FROM album_bios WHERE key = ?`, key,
	).Scan(&bio.Artist, &bio.Album, &bio.Summary, &sourceURL, &bio.FetchedAt, &bio.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AlbumBio{}, false, nil
	}
	if err != nil {
		return AlbumBio{}, false, fmt.Errorf("GetAlbumBio: %w", err)
	}
	bio.SourceURL = sourceURL.String
	return bio, true, nil
}

// DeleteAlbumBio removes a row by normalized key. No-op on miss.
func (b *BiosDAO) DeleteAlbumBio(artist, album string) error {
	key := normalizeBioKey(artist, album)
	_, err := b.db.Exec(`DELETE FROM album_bios WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("DeleteAlbumBio: %w", err)
	}
	return nil
}

// PutArtistBio upserts a row keyed on the normalized artist name.
func (b *BiosDAO) PutArtistBio(bio ArtistBio) error {
	if strings.TrimSpace(bio.Artist) == "" {
		return errors.New("PutArtistBio: artist required")
	}
	key := normalizeBioKey(bio.Artist)
	_, err := b.db.Exec(
		`INSERT INTO artist_bios (key, artist, summary, source_url, fetched_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		   summary    = excluded.summary,
		   source_url = excluded.source_url,
		   fetched_at = excluded.fetched_at,
		   expires_at = excluded.expires_at`,
		key, bio.Artist, bio.Summary, bio.SourceURL, bio.FetchedAt, bio.ExpiresAt,
	)
	if err != nil {
		log.Error().Err(err).Str("key", key).Msg("PutArtistBio failed")
		return fmt.Errorf("PutArtistBio: %w", err)
	}
	return nil
}

// GetArtistBio looks up a cached artist bio. Same freshness contract as GetAlbumBio.
func (b *BiosDAO) GetArtistBio(artist string) (ArtistBio, bool, error) {
	if strings.TrimSpace(artist) == "" {
		return ArtistBio{}, false, nil
	}
	key := normalizeBioKey(artist)
	var bio ArtistBio
	var sourceURL sql.NullString
	err := b.db.QueryRow(
		`SELECT artist, summary, source_url, fetched_at, expires_at
		 FROM artist_bios WHERE key = ?`, key,
	).Scan(&bio.Artist, &bio.Summary, &sourceURL, &bio.FetchedAt, &bio.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ArtistBio{}, false, nil
	}
	if err != nil {
		return ArtistBio{}, false, fmt.Errorf("GetArtistBio: %w", err)
	}
	bio.SourceURL = sourceURL.String
	return bio, true, nil
}

// DeleteArtistBio removes a row by normalized key. No-op on miss.
func (b *BiosDAO) DeleteArtistBio(artist string) error {
	key := normalizeBioKey(artist)
	_, err := b.db.Exec(`DELETE FROM artist_bios WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("DeleteArtistBio: %w", err)
	}
	return nil
}
