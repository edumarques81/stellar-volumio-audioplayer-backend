package cache

import (
	"database/sql"
	"fmt"
)

// This file performs no network I/O and never touches the filesystem: it
// only reads and rewrites local SQLite `artwork`/`albums` rows via
// database/sql. The on-disk JPG an orphan row points at keeps its original
// filename permanently -- only the DB row's identity moves (D-09).
//
// The MATCHING decision (which orphan belongs to which album) is computed
// elsewhere by Plan 02-04's perceptual-hash matcher, running live on the
// Pi, and passed in here as an already-approved (orphanArtworkID,
// targetAlbumID) pair. This file contains no image-comparison logic -- it
// only APPLIES an approved pair safely.

// AlbumArtworkOrphan describes one 'album' artwork row whose album_id no
// longer matches any row in the albums table -- an artwork row that
// outlived the album identity it was originally linked to. See
// 02-ORPHAN-PROVENANCE-CORRECTION.md for how these rows come to exist
// (BackfillAlbumArtwork, source='backfill'): they are genuine, on-disk
// cover images, not corrupted or unknown data.
type AlbumArtworkOrphan struct {
	ArtworkID string
	AlbumID   string
	FilePath  string
}

// ListOrphanedAlbumArtwork returns every 'album' artwork row whose
// album_id points at nothing in the albums table. Read-only, no writes,
// no network I/O -- mirrors the exact query manually validated against the
// live Pi DB during Plan 02 planning (38 rows, 2026-08-12).
func ListOrphanedAlbumArtwork(dao *DAO) ([]AlbumArtworkOrphan, error) {
	if dao == nil {
		return nil, fmt.Errorf("dao is nil")
	}
	db := dao.db.DB()
	if db == nil {
		return nil, fmt.Errorf("database not open")
	}

	rows, err := db.Query(`
		SELECT id, album_id, COALESCE(file_path, '')
		FROM artwork
		WHERE type = 'album' AND album_id NOT IN (SELECT id FROM albums)
	`)
	if err != nil {
		return nil, fmt.Errorf("list orphaned album artwork: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var orphans []AlbumArtworkOrphan
	for rows.Next() {
		var o AlbumArtworkOrphan
		if err := rows.Scan(&o.ArtworkID, &o.AlbumID, &o.FilePath); err != nil {
			return nil, fmt.Errorf("scan orphan row: %w", err)
		}
		orphans = append(orphans, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iter: %w", err)
	}
	return orphans, nil
}

// RekeyAlbumArtwork applies one human-approved (orphanArtworkID ->
// targetAlbumID) mapping: it renames the orphan artwork row's id/album_id
// onto the target album's identity (`<targetAlbumID>_artwork`) and links
// albums.artwork_id, without ever touching the filesystem.
//
// Idempotence and clobber-safety are checked by inspecting the TARGET
// ALBUM's current state first, not the orphan row's current state -- this
// is deliberate: after a successful application the orphan row no longer
// exists under its original id (it was renamed), so a naive
// orphan-row-first check would incorrectly fail a safe repeat call.
//
//   - If the target album already links to exactly the artwork row this
//     call would produce, the mapping was already applied: return nil
//     without writing (safe, idempotent no-op -- D-09; re-running Plan
//     02-04's apply script must never error on rows it already fixed).
//   - If the target album already links to any OTHER non-empty artwork,
//     refuse: return a descriptive error and write nothing (never clobber
//     unrelated existing artwork, even given a wrong or stale pairing).
//   - Otherwise (target album currently has no artwork link), this is a
//     first application: verify the orphan row exists, then move it.
//
// dryRun performs every precondition check and returns nil on success
// without writing.
func RekeyAlbumArtwork(dao *DAO, orphanArtworkID, targetAlbumID string, dryRun bool) error {
	if dao == nil {
		return fmt.Errorf("dao is nil")
	}
	db := dao.db.DB()
	if db == nil {
		return fmt.Errorf("database not open")
	}

	wantArtworkID := targetAlbumID + "_artwork"

	var currentArtworkID sql.NullString
	err := db.QueryRow(`SELECT artwork_id FROM albums WHERE id = ?`, targetAlbumID).Scan(&currentArtworkID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("target album %q not found", targetAlbumID)
	}
	if err != nil {
		return fmt.Errorf("look up target album %q: %w", targetAlbumID, err)
	}

	switch {
	case currentArtworkID.Valid && currentArtworkID.String == wantArtworkID:
		// The album already links to the id this exact mapping would
		// produce. Confirm the artwork row itself actually landed there
		// with the right album_id before declaring this a safe no-op.
		var linkedAlbumID string
		lookupErr := db.QueryRow(`SELECT album_id FROM artwork WHERE id = ?`, wantArtworkID).Scan(&linkedAlbumID)
		if lookupErr == nil && linkedAlbumID == targetAlbumID {
			return nil // already applied -- safe, idempotent no-op (D-09)
		}
		if lookupErr != nil && lookupErr != sql.ErrNoRows {
			return fmt.Errorf("verify already-applied artwork row %q: %w", wantArtworkID, lookupErr)
		}
		return fmt.Errorf("album %q artwork_id already set to %q but no matching artwork row was found; refusing to write", targetAlbumID, wantArtworkID)

	case currentArtworkID.Valid && currentArtworkID.String != "":
		return fmt.Errorf("album %q already has different artwork %q, refusing to overwrite", targetAlbumID, currentArtworkID.String)
	}

	// First application: target album currently has no artwork link.
	var exists int
	err = db.QueryRow(`SELECT 1 FROM artwork WHERE id = ?`, orphanArtworkID).Scan(&exists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("orphan artwork row %q not found", orphanArtworkID)
	}
	if err != nil {
		return fmt.Errorf("look up orphan artwork row %q: %w", orphanArtworkID, err)
	}

	if dryRun {
		return nil
	}

	tx, err := dao.db.BeginTx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`UPDATE artwork SET id = ?, album_id = ? WHERE id = ?`,
		wantArtworkID, targetAlbumID, orphanArtworkID); err != nil {
		return fmt.Errorf("rename orphan artwork row: %w", err)
	}
	if _, err := tx.Exec(`UPDATE albums SET artwork_id = ? WHERE id = ?`,
		wantArtworkID, targetAlbumID); err != nil {
		return fmt.Errorf("link album to artwork: %w", err)
	}

	return tx.Commit()
}
