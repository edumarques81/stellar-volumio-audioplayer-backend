// Package cache provides a SQLite-based caching layer for library metadata.
package cache

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// DAO provides data access operations for the cache.
type DAO struct {
	db *DB
}

// NewDAO creates a new DAO instance.
func NewDAO(db *DB) *DAO {
	return &DAO{db: db}
}

// --- Album Operations ---

// InsertAlbum inserts or updates an album in the cache.
func (dao *DAO) InsertAlbum(album *CachedAlbum) error {
	db := dao.db.DB()
	if db == nil {
		return fmt.Errorf("database not open")
	}

	now := time.Now().Format(time.RFC3339)
	addedAt := ""
	if !album.AddedAt.IsZero() {
		addedAt = album.AddedAt.Format(time.RFC3339)
	}
	lastPlayed := ""
	if !album.LastPlayed.IsZero() {
		lastPlayed = album.LastPlayed.Format(time.RFC3339)
	}

	_, err := db.Exec(`
		INSERT INTO albums (id, title, album_artist, uri, first_track, track_count, total_duration,
			source, year, added_at, last_played, artwork_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = ?, album_artist = ?, uri = ?, first_track = COALESCE(?, albums.first_track),
			track_count = ?, total_duration = ?,
			source = ?, year = ?, added_at = COALESCE(albums.added_at, ?),
			last_played = COALESCE(?, albums.last_played), artwork_id = COALESCE(?, albums.artwork_id),
			updated_at = ?
	`,
		album.ID, album.Title, album.AlbumArtist, album.URI, album.FirstTrack, album.TrackCount, album.TotalDuration,
		album.Source, album.Year, addedAt, lastPlayed, album.ArtworkID, now, now,
		album.Title, album.AlbumArtist, album.URI, album.FirstTrack, album.TrackCount, album.TotalDuration,
		album.Source, album.Year, addedAt, lastPlayed, album.ArtworkID, now,
	)
	return err
}

// InsertAlbumTx inserts an album within a transaction.
func (dao *DAO) InsertAlbumTx(tx *sql.Tx, album *CachedAlbum) error {
	now := time.Now().Format(time.RFC3339)
	addedAt := ""
	if !album.AddedAt.IsZero() {
		addedAt = album.AddedAt.Format(time.RFC3339)
	}

	_, err := tx.Exec(`
		INSERT INTO albums (id, title, album_artist, uri, first_track, track_count, total_duration,
			source, year, added_at, artwork_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = ?, album_artist = ?, uri = ?, first_track = COALESCE(?, albums.first_track),
			track_count = ?, total_duration = ?,
			source = ?, year = ?, added_at = COALESCE(albums.added_at, ?),
			artwork_id = COALESCE(?, albums.artwork_id), updated_at = ?
	`,
		album.ID, album.Title, album.AlbumArtist, album.URI, album.FirstTrack, album.TrackCount, album.TotalDuration,
		album.Source, album.Year, addedAt, album.ArtworkID, now, now,
		album.Title, album.AlbumArtist, album.URI, album.FirstTrack, album.TrackCount, album.TotalDuration,
		album.Source, album.Year, addedAt, album.ArtworkID, now,
	)
	return err
}

// GetAlbum retrieves an album by ID.
func (dao *DAO) GetAlbum(id string) (*CachedAlbum, error) {
	db := dao.db.DB()
	if db == nil {
		return nil, fmt.Errorf("database not open")
	}

	album := &CachedAlbum{}
	var addedAt, lastPlayed, createdAt, updatedAt sql.NullString
	var year sql.NullInt64
	var artworkID, firstTrack sql.NullString

	err := db.QueryRow(`
		SELECT id, title, album_artist, uri, first_track, track_count, total_duration, source,
			year, added_at, last_played, artwork_id, created_at, updated_at
		FROM albums WHERE id = ?
	`, id).Scan(
		&album.ID, &album.Title, &album.AlbumArtist, &album.URI, &firstTrack, &album.TrackCount,
		&album.TotalDuration, &album.Source, &year, &addedAt, &lastPlayed,
		&artworkID, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if year.Valid {
		album.Year = int(year.Int64)
	}
	if firstTrack.Valid {
		album.FirstTrack = firstTrack.String
	}
	if addedAt.Valid {
		album.AddedAt, _ = time.Parse(time.RFC3339, addedAt.String)
	}
	if lastPlayed.Valid {
		album.LastPlayed, _ = time.Parse(time.RFC3339, lastPlayed.String)
	}
	if artworkID.Valid {
		album.ArtworkID = artworkID.String
	}
	if createdAt.Valid {
		album.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}
	if updatedAt.Valid {
		album.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	}

	return album, nil
}

// QueryAlbums queries albums with filters, sorting, and pagination.
func (dao *DAO) QueryAlbums(filter AlbumFilter, sort SortOrder, pag Pagination) ([]*CachedAlbum, int, error) {
	db := dao.db.DB()
	if db == nil {
		return nil, 0, fmt.Errorf("database not open")
	}

	// Build WHERE clause
	var conditions []string
	var args []interface{}

	if filter.Scope != "" && filter.Scope != "all" {
		switch filter.Scope {
		case "nas":
			conditions = append(conditions, "source = 'nas'")
		case "local":
			conditions = append(conditions, "(source = 'local' OR source = 'usb')")
		case "usb":
			conditions = append(conditions, "source = 'usb'")
		}
	}

	if filter.Query != "" {
		conditions = append(conditions, "(title LIKE ? COLLATE NOCASE OR album_artist LIKE ? COLLATE NOCASE)")
		searchTerm := "%" + filter.Query + "%"
		args = append(args, searchTerm, searchTerm)
	}

	if filter.Artist != "" {
		conditions = append(conditions, "album_artist = ?")
		args = append(args, filter.Artist)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Build ORDER BY clause
	orderClause := "ORDER BY "
	switch sort {
	case SortByArtist:
		orderClause += "album_artist COLLATE NOCASE, title COLLATE NOCASE"
	case SortRecentlyAdded:
		orderClause += "added_at DESC, title COLLATE NOCASE"
	case SortYear:
		orderClause += "year DESC, title COLLATE NOCASE"
	default: // SortAlphabetical
		orderClause += "title COLLATE NOCASE"
	}

	// Get total count
	countQuery := "SELECT COUNT(*) FROM albums " + whereClause
	var total int
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Get paginated results
	query := fmt.Sprintf(`
		SELECT id, title, album_artist, uri, first_track, track_count, total_duration, source,
			year, added_at, last_played, artwork_id, created_at, updated_at
		FROM albums %s %s LIMIT ? OFFSET ?
	`, whereClause, orderClause)

	args = append(args, pag.Limit, pag.Offset)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var albums []*CachedAlbum
	for rows.Next() {
		album := &CachedAlbum{}
		var addedAt, lastPlayed, createdAt, updatedAt sql.NullString
		var year sql.NullInt64
		var artworkID, firstTrack sql.NullString

		err := rows.Scan(
			&album.ID, &album.Title, &album.AlbumArtist, &album.URI, &firstTrack, &album.TrackCount,
			&album.TotalDuration, &album.Source, &year, &addedAt, &lastPlayed,
			&artworkID, &createdAt, &updatedAt,
		)
		if err != nil {
			return nil, 0, err
		}

		if year.Valid {
			album.Year = int(year.Int64)
		}
		if firstTrack.Valid {
			album.FirstTrack = firstTrack.String
		}
		if addedAt.Valid {
			album.AddedAt, _ = time.Parse(time.RFC3339, addedAt.String)
		}
		if lastPlayed.Valid {
			album.LastPlayed, _ = time.Parse(time.RFC3339, lastPlayed.String)
		}
		if artworkID.Valid {
			album.ArtworkID = artworkID.String
		}

		albums = append(albums, album)
	}

	return albums, total, nil
}

// --- Artist Operations ---

// InsertArtist inserts or updates an artist in the cache.
func (dao *DAO) InsertArtist(artist *CachedArtist) error {
	db := dao.db.DB()
	if db == nil {
		return fmt.Errorf("database not open")
	}

	now := time.Now().Format(time.RFC3339)

	_, err := db.Exec(`
		INSERT INTO artists (id, name, album_count, track_count, artwork_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = ?, album_count = ?, track_count = ?,
			artwork_id = COALESCE(?, artists.artwork_id), updated_at = ?
	`,
		artist.ID, artist.Name, artist.AlbumCount, artist.TrackCount, artist.ArtworkID, now, now,
		artist.Name, artist.AlbumCount, artist.TrackCount, artist.ArtworkID, now,
	)
	return err
}

// InsertArtistTx inserts an artist within a transaction.
func (dao *DAO) InsertArtistTx(tx *sql.Tx, artist *CachedArtist) error {
	now := time.Now().Format(time.RFC3339)

	_, err := tx.Exec(`
		INSERT INTO artists (id, name, album_count, track_count, artwork_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = ?, album_count = ?, track_count = ?,
			artwork_id = COALESCE(?, artists.artwork_id), updated_at = ?
	`,
		artist.ID, artist.Name, artist.AlbumCount, artist.TrackCount, artist.ArtworkID, now, now,
		artist.Name, artist.AlbumCount, artist.TrackCount, artist.ArtworkID, now,
	)
	return err
}

// QueryArtists queries artists with filters and pagination.
func (dao *DAO) QueryArtists(query string, pag Pagination) ([]*CachedArtist, int, error) {
	db := dao.db.DB()
	if db == nil {
		return nil, 0, fmt.Errorf("database not open")
	}

	var conditions []string
	var args []interface{}

	if query != "" {
		conditions = append(conditions, "name LIKE ? COLLATE NOCASE")
		args = append(args, "%"+query+"%")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count
	countQuery := "SELECT COUNT(*) FROM artists " + whereClause
	var total int
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Get paginated results
	querySQL := fmt.Sprintf(`
		SELECT id, name, album_count, track_count, artwork_id, created_at, updated_at
		FROM artists %s ORDER BY name COLLATE NOCASE LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, pag.Limit, pag.Offset)
	rows, err := db.Query(querySQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var artists []*CachedArtist
	for rows.Next() {
		artist := &CachedArtist{}
		var artworkID sql.NullString
		var createdAt, updatedAt sql.NullString

		err := rows.Scan(
			&artist.ID, &artist.Name, &artist.AlbumCount, &artist.TrackCount,
			&artworkID, &createdAt, &updatedAt,
		)
		if err != nil {
			return nil, 0, err
		}

		if artworkID.Valid {
			artist.ArtworkID = artworkID.String
		}
		if createdAt.Valid {
			artist.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
		}
		if updatedAt.Valid {
			artist.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
		}

		artists = append(artists, artist)
	}

	return artists, total, nil
}

// --- Track Operations ---

// InsertTrack inserts a track in the cache.
func (dao *DAO) InsertTrack(track *CachedTrack) error {
	db := dao.db.DB()
	if db == nil {
		return fmt.Errorf("database not open")
	}

	now := time.Now().Format(time.RFC3339)

	_, err := db.Exec(`
		INSERT INTO tracks (id, album_id, title, artist, uri, track_number, disc_number, duration, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			album_id = ?, title = ?, artist = ?, track_number = ?, disc_number = ?, duration = ?, source = ?
	`,
		track.ID, track.AlbumID, track.Title, track.Artist, track.URI,
		track.TrackNumber, track.DiscNumber, track.Duration, track.Source, now,
		track.AlbumID, track.Title, track.Artist, track.TrackNumber, track.DiscNumber, track.Duration, track.Source,
	)
	return err
}

// InsertTrackTx inserts a track within a transaction.
func (dao *DAO) InsertTrackTx(tx *sql.Tx, track *CachedTrack) error {
	now := time.Now().Format(time.RFC3339)

	_, err := tx.Exec(`
		INSERT INTO tracks (id, album_id, title, artist, uri, track_number, disc_number, duration, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(uri) DO UPDATE SET
			album_id = ?, title = ?, artist = ?, track_number = ?, disc_number = ?, duration = ?, source = ?
	`,
		track.ID, track.AlbumID, track.Title, track.Artist, track.URI,
		track.TrackNumber, track.DiscNumber, track.Duration, track.Source, now,
		track.AlbumID, track.Title, track.Artist, track.TrackNumber, track.DiscNumber, track.Duration, track.Source,
	)
	return err
}

// GetTracksByAlbum retrieves all tracks for an album.
func (dao *DAO) GetTracksByAlbum(albumID string) ([]*CachedTrack, error) {
	db := dao.db.DB()
	if db == nil {
		return nil, fmt.Errorf("database not open")
	}

	rows, err := db.Query(`
		SELECT id, album_id, title, artist, uri, track_number, disc_number, duration, source, created_at
		FROM tracks WHERE album_id = ? ORDER BY disc_number, track_number
	`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []*CachedTrack
	for rows.Next() {
		track := &CachedTrack{}
		var createdAt sql.NullString

		err := rows.Scan(
			&track.ID, &track.AlbumID, &track.Title, &track.Artist, &track.URI,
			&track.TrackNumber, &track.DiscNumber, &track.Duration, &track.Source, &createdAt,
		)
		if err != nil {
			return nil, err
		}

		if createdAt.Valid {
			track.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
		}

		tracks = append(tracks, track)
	}

	return tracks, nil
}

// --- Artwork Operations ---

// InsertArtwork inserts artwork metadata.
func (dao *DAO) InsertArtwork(art *CachedArtwork) error {
	db := dao.db.DB()
	if db == nil {
		return fmt.Errorf("database not open")
	}

	now := time.Now().Format(time.RFC3339)
	fetchedAt := ""
	if !art.FetchedAt.IsZero() {
		fetchedAt = art.FetchedAt.Format(time.RFC3339)
	}
	expiresAt := ""
	if !art.ExpiresAt.IsZero() {
		expiresAt = art.ExpiresAt.Format(time.RFC3339)
	}

	_, err := db.Exec(`
		INSERT INTO artwork (id, album_id, artist_id, type, file_path, source, mime_type, width, height, file_size, checksum, fetched_at, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			file_path = ?, source = ?, mime_type = ?, width = ?, height = ?, file_size = ?, checksum = ?, fetched_at = ?, expires_at = ?
	`,
		art.ID, art.AlbumID, art.ArtistID, art.Type, art.FilePath, art.Source,
		art.MimeType, art.Width, art.Height, art.FileSize, art.Checksum, fetchedAt, expiresAt, now,
		art.FilePath, art.Source, art.MimeType, art.Width, art.Height, art.FileSize, art.Checksum, fetchedAt, expiresAt,
	)
	return err
}

// GetArtwork retrieves artwork by ID.
func (dao *DAO) GetArtwork(id string) (*CachedArtwork, error) {
	db := dao.db.DB()
	if db == nil {
		return nil, fmt.Errorf("database not open")
	}

	art := &CachedArtwork{}
	var albumID, artistID, filePath, mimeType, checksum sql.NullString
	var width, height, fileSize sql.NullInt64
	var fetchedAt, expiresAt, createdAt sql.NullString

	err := db.QueryRow(`
		SELECT id, album_id, artist_id, type, file_path, source, mime_type, width, height, file_size, checksum, fetched_at, expires_at, created_at
		FROM artwork WHERE id = ?
	`, id).Scan(
		&art.ID, &albumID, &artistID, &art.Type, &filePath, &art.Source,
		&mimeType, &width, &height, &fileSize, &checksum, &fetchedAt, &expiresAt, &createdAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if albumID.Valid {
		art.AlbumID = albumID.String
	}
	if artistID.Valid {
		art.ArtistID = artistID.String
	}
	if filePath.Valid {
		art.FilePath = filePath.String
	}
	if mimeType.Valid {
		art.MimeType = mimeType.String
	}
	if width.Valid {
		art.Width = int(width.Int64)
	}
	if height.Valid {
		art.Height = int(height.Int64)
	}
	if fileSize.Valid {
		art.FileSize = int(fileSize.Int64)
	}
	if checksum.Valid {
		art.Checksum = checksum.String
	}
	if fetchedAt.Valid {
		art.FetchedAt, _ = time.Parse(time.RFC3339, fetchedAt.String)
	}
	if expiresAt.Valid {
		art.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt.String)
	}
	if createdAt.Valid {
		art.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}

	return art, nil
}

// GetArtworkByAlbum retrieves artwork by album ID.
func (dao *DAO) GetArtworkByAlbum(albumID string) (*CachedArtwork, error) {
	db := dao.db.DB()
	if db == nil {
		return nil, fmt.Errorf("database not open")
	}

	art := &CachedArtwork{}
	var artistID, filePath, mimeType, checksum sql.NullString
	var width, height, fileSize sql.NullInt64
	var fetchedAt, expiresAt, createdAt sql.NullString

	err := db.QueryRow(`
		SELECT id, album_id, artist_id, type, file_path, source, mime_type, width, height, file_size, checksum, fetched_at, expires_at, created_at
		FROM artwork WHERE album_id = ?
	`, albumID).Scan(
		&art.ID, &art.AlbumID, &artistID, &art.Type, &filePath, &art.Source,
		&mimeType, &width, &height, &fileSize, &checksum, &fetchedAt, &expiresAt, &createdAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if artistID.Valid {
		art.ArtistID = artistID.String
	}
	if filePath.Valid {
		art.FilePath = filePath.String
	}
	if mimeType.Valid {
		art.MimeType = mimeType.String
	}
	if width.Valid {
		art.Width = int(width.Int64)
	}
	if height.Valid {
		art.Height = int(height.Int64)
	}
	if fileSize.Valid {
		art.FileSize = int(fileSize.Int64)
	}
	if checksum.Valid {
		art.Checksum = checksum.String
	}
	if fetchedAt.Valid {
		art.FetchedAt, _ = time.Parse(time.RFC3339, fetchedAt.String)
	}
	if expiresAt.Valid {
		art.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt.String)
	}
	if createdAt.Valid {
		art.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}

	return art, nil
}

// UpdateAlbumArtwork links an album to an artwork entry.
func (dao *DAO) UpdateAlbumArtwork(albumID, artworkID string) error {
	db := dao.db.DB()
	if db == nil {
		return fmt.Errorf("database not open")
	}

	now := time.Now().Format(time.RFC3339)
	_, err := db.Exec("UPDATE albums SET artwork_id = ?, updated_at = ? WHERE id = ?", artworkID, now, albumID)
	return err
}

// --- Radio Station Operations ---

// InsertRadioStation inserts or updates a radio station.
func (dao *DAO) InsertRadioStation(station *CachedRadioStation) error {
	db := dao.db.DB()
	if db == nil {
		return fmt.Errorf("database not open")
	}

	now := time.Now().Format(time.RFC3339)

	_, err := db.Exec(`
		INSERT INTO radio_stations (id, name, uri, icon, genre, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = ?, uri = ?, icon = ?, genre = ?, updated_at = ?
	`,
		station.ID, station.Name, station.URI, station.Icon, station.Genre, now, now,
		station.Name, station.URI, station.Icon, station.Genre, now,
	)
	return err
}

// QueryRadioStations queries radio stations with filters and pagination.
func (dao *DAO) QueryRadioStations(query string, pag Pagination) ([]*CachedRadioStation, int, error) {
	db := dao.db.DB()
	if db == nil {
		return nil, 0, fmt.Errorf("database not open")
	}

	var conditions []string
	var args []interface{}

	if query != "" {
		conditions = append(conditions, "(name LIKE ? COLLATE NOCASE OR genre LIKE ? COLLATE NOCASE)")
		searchTerm := "%" + query + "%"
		args = append(args, searchTerm, searchTerm)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count
	countQuery := "SELECT COUNT(*) FROM radio_stations " + whereClause
	var total int
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Get paginated results
	querySQL := fmt.Sprintf(`
		SELECT id, name, uri, icon, genre, created_at, updated_at
		FROM radio_stations %s ORDER BY name COLLATE NOCASE LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, pag.Limit, pag.Offset)
	rows, err := db.Query(querySQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var stations []*CachedRadioStation
	for rows.Next() {
		station := &CachedRadioStation{}
		var icon, genre sql.NullString
		var createdAt, updatedAt sql.NullString

		err := rows.Scan(
			&station.ID, &station.Name, &station.URI, &icon, &genre, &createdAt, &updatedAt,
		)
		if err != nil {
			return nil, 0, err
		}

		if icon.Valid {
			station.Icon = icon.String
		}
		if genre.Valid {
			station.Genre = genre.String
		}

		stations = append(stations, station)
	}

	return stations, total, nil
}

// LogCacheStats logs cache statistics.
func (dao *DAO) LogCacheStats() {
	stats, err := dao.db.GetStats()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get cache stats")
		return
	}

	log.Info().
		Int("albums", stats.AlbumCount).
		Int("artists", stats.ArtistCount).
		Int("tracks", stats.TrackCount).
		Int("artwork", stats.ArtworkCount).
		Int("radio", stats.RadioCount).
		Msg("Cache statistics")
}

// AlbumInfo contains basic album info for enrichment queries.
type AlbumInfo struct {
	ID          string
	Title       string
	AlbumArtist string
	FirstTrack  string
	HasArtwork  bool
}

// GetAlbumsWithoutArtwork returns albums that don't have cached artwork.
func (dao *DAO) GetAlbumsWithoutArtwork() ([]AlbumInfo, error) {
	db := dao.db.DB()
	if db == nil {
		return nil, fmt.Errorf("database not open")
	}

	rows, err := db.Query(`
		SELECT a.id, a.title, a.album_artist, a.first_track, a.artwork_id
		FROM albums a
		WHERE a.artwork_id IS NULL OR a.artwork_id = ''
		ORDER BY a.title COLLATE NOCASE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []AlbumInfo
	for rows.Next() {
		var album AlbumInfo
		var firstTrack, artworkID sql.NullString

		err := rows.Scan(&album.ID, &album.Title, &album.AlbumArtist, &firstTrack, &artworkID)
		if err != nil {
			return nil, err
		}

		if firstTrack.Valid {
			album.FirstTrack = firstTrack.String
		}
		album.HasArtwork = artworkID.Valid && artworkID.String != ""

		albums = append(albums, album)
	}

	return albums, nil
}
