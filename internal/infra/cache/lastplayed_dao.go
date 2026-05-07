package cache

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

// LastPlayedAlbum is the persisted shape of a single resume-state row.
type LastPlayedAlbum struct {
	Artist       string
	Album        string
	AlbumArt     string
	TrackURI     string
	TrackType    string
	SampleRate   string
	BitDepth     string
	LastPlayedAt int64 // unix seconds
}

// LastPlayedDAO provides CRUD over the last_played_album table.
type LastPlayedDAO struct {
	db *sql.DB
}

// LastPlayedDAO returns a DAO bound to this DB. Safe to call repeatedly.
func (d *DB) LastPlayedDAO() *LastPlayedDAO {
	return &LastPlayedDAO{db: d.db}
}

// normalizeAlbumKey lower-cases and trims artist + album, joined with "|".
// Mirrors the bio cache key shape so future cross-table joins are trivial.
func normalizeAlbumKey(artist, album string) string {
	return strings.ToLower(strings.TrimSpace(artist)) + "|" + strings.ToLower(strings.TrimSpace(album))
}

// Put upserts a last-played row. The key is the normalized "artist|album"
// so replaying the same album just bumps last_played_at.
func (l *LastPlayedDAO) Put(row LastPlayedAlbum) error {
	if strings.TrimSpace(row.Artist) == "" || strings.TrimSpace(row.Album) == "" {
		return errors.New("LastPlayedDAO.Put: artist and album required")
	}
	if row.LastPlayedAt == 0 {
		return errors.New("LastPlayedDAO.Put: last_played_at required")
	}
	key := normalizeAlbumKey(row.Artist, row.Album)
	_, err := l.db.Exec(
		`INSERT INTO last_played_album
		   (key, artist, album, album_art, track_uri, track_type, sample_rate, bit_depth, last_played_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		   album_art      = excluded.album_art,
		   track_uri      = excluded.track_uri,
		   track_type     = excluded.track_type,
		   sample_rate    = excluded.sample_rate,
		   bit_depth      = excluded.bit_depth,
		   last_played_at = excluded.last_played_at,
		   updated_at     = CURRENT_TIMESTAMP`,
		key, row.Artist, row.Album, row.AlbumArt, row.TrackURI, row.TrackType, row.SampleRate, row.BitDepth, row.LastPlayedAt,
	)
	if err != nil {
		log.Error().Err(err).Str("key", key).Msg("LastPlayedDAO.Put failed")
		return fmt.Errorf("LastPlayedDAO.Put: %w", err)
	}
	return nil
}

// GetMostRecent returns the most recently played album row, or (zero, false, nil) on miss.
func (l *LastPlayedDAO) GetMostRecent() (LastPlayedAlbum, bool, error) {
	var row LastPlayedAlbum
	var albumArt, trackURI, trackType, sampleRate, bitDepth sql.NullString
	err := l.db.QueryRow(
		`SELECT artist, album, album_art, track_uri, track_type, sample_rate, bit_depth, last_played_at
		 FROM last_played_album
		 ORDER BY last_played_at DESC
		 LIMIT 1`,
	).Scan(&row.Artist, &row.Album, &albumArt, &trackURI, &trackType, &sampleRate, &bitDepth, &row.LastPlayedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return LastPlayedAlbum{}, false, nil
	}
	if err != nil {
		return LastPlayedAlbum{}, false, fmt.Errorf("LastPlayedDAO.GetMostRecent: %w", err)
	}
	row.AlbumArt = albumArt.String
	row.TrackURI = trackURI.String
	row.TrackType = trackType.String
	row.SampleRate = sampleRate.String
	row.BitDepth = bitDepth.String
	return row, true, nil
}
