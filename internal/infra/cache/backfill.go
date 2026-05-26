package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

// BackfillAlbumArtwork repairs the pre-2026-05-27 state where the album
// enrichment save path wrote the JPG file but never inserted the artwork
// row. For each album whose `artwork_id` points at nothing in the artwork
// table, look for an on-disk JPG/PNG/WEBP/GIF in `<cacheDir>/artwork/albums/`
// keyed by the album_id (matching the new convention), and INSERT the
// missing row with the deterministic `<album_id>_artwork` id so the next
// cache rebuild's relinker can pick it up.
//
// Returns the number of albums repaired. Idempotent: re-running on a clean
// state is a no-op.
//
// Safe to call from main.go on startup — pure read-from-disk + write-to-DB,
// no network, finishes in milliseconds for ~100 albums.
func BackfillAlbumArtwork(dao *DAO, cacheDir string) (int, error) {
	if dao == nil {
		return 0, fmt.Errorf("dao is nil")
	}
	db := dao.db.DB()
	if db == nil {
		return 0, fmt.Errorf("database not open")
	}

	// Find albums where artwork_id is set but no artwork row exists.
	rows, err := db.Query(`
		SELECT a.id, a.artwork_id
		FROM albums a
		WHERE a.artwork_id IS NOT NULL
		  AND a.artwork_id != ''
		  AND NOT EXISTS (SELECT 1 FROM artwork w WHERE w.id = a.artwork_id)
	`)
	if err != nil {
		return 0, fmt.Errorf("scan orphan albums: %w", err)
	}
	defer rows.Close()

	type orphan struct {
		albumID, artworkID string
	}
	var orphans []orphan
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.albumID, &o.artworkID); err != nil {
			return 0, fmt.Errorf("scan row: %w", err)
		}
		orphans = append(orphans, o)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rows iter: %w", err)
	}

	if len(orphans) == 0 {
		return 0, nil
	}

	albumsDir := filepath.Join(cacheDir, "artwork", "albums")
	repaired := 0
	for _, o := range orphans {
		// Try common extensions. The album save path writes JPGs by
		// default but also handles PNG/WEBP/GIF.
		var foundPath, mimeType string
		for _, ext := range []string{".jpg", ".png", ".webp", ".gif"} {
			p := filepath.Join(albumsDir, o.albumID+ext)
			if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
				foundPath = p
				mimeType = mimeFromExt(ext)
				break
			}
		}
		if foundPath == "" {
			log.Warn().
				Str("album_id", o.albumID).
				Str("orphan_artwork_id", o.artworkID).
				Msg("Backfill: no on-disk file found, leaving orphan in place for re-enrichment")
			continue
		}

		// Source is unknown at this point — the original fetch source
		// (CAA / Fanart / Deezer / albumart) wasn't persisted in the
		// album row. Tag as "backfill" so future audits can identify
		// recovered rows.
		if err := dao.UpsertAlbumArtwork(o.albumID, foundPath, "backfill", mimeType); err != nil {
			log.Warn().Err(err).Str("album_id", o.albumID).Msg("Backfill: upsert failed")
			continue
		}
		repaired++
	}

	if repaired > 0 {
		log.Info().
			Int("repaired", repaired).
			Int("scanned", len(orphans)).
			Msg("Backfill: linked orphan album artwork to preserved on-disk files")
	}
	return repaired, nil
}

func mimeFromExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/jpeg"
	}
}

// Ensure time is imported — kept for future use (timestamped log fields
// or extension to expires_at if we ever set TTLs on backfilled rows).
var _ = time.Now
