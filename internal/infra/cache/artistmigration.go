package cache

import (
	"database/sql"
	"fmt"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/artistidentity"
)

// This file performs no network I/O. It reads and rewrites only local
// SQLite `artists`/`artwork` rows already present in the cache database
// via database/sql, plus this repo's own internal/infra/artistidentity
// package for the pure Collapse computation. It imports no HTTP client
// package and no enrichment-worker package (D-12).

// ArtistRekey records one artist-artwork row that MigrateArtistArtwork
// moved (or, in dry-run mode, would move) from its pre-collapse raw-name
// identity onto its Plan 02-02 collapsed identity.
type ArtistRekey struct {
	OldID     string // artists.id under the raw (pre-collapse) name
	OldName   string // the raw MPD Artist tag value
	NewID     string // generateArtistID(canonical) -- the collapsed identity
	NewName   string // the collapsed (canonical) name
	ArtworkID string // the artwork row's new id: NewID + "_artwork"
}

// ArtistArtworkMigrationReport summarizes one MigrateArtistArtwork run.
type ArtistArtworkMigrationReport struct {
	Scanned int // every artists row read
	Rekeyed int // rows whose artwork row was actually moved (or would be, dry-run)
	Skipped int // rows needing no action: already-canonical identity, merge
	// collision loser, or no artwork row to move
	Rekeys []ArtistRekey
}

// MigrateArtistArtwork re-keys ARTIST artwork rows from a raw
// (pre-ARTIST-01-collapse) artist identity onto the Plan 02-02 collapsed
// identity computed by artistidentity.Collapse, so artwork already fetched
// (at real API cost, rate-limited) before the collapse ships keeps
// resolving afterward without re-fetching (D-07/D-12).
//
// Artist identity is generateArtistID(name) = md5(name)
// (internal/infra/cache/builder.go). Plan 02-02 changes `name` for
// collapsible artists, which changes their id, which orphans any artwork
// row already linked under the old id. This function moves the artwork
// row's `id`/`artist_id` columns (and, best-effort, the new-identity
// artists row's `artwork_id`) onto the new id -- no filesystem write, no
// network call, ever (D-09 non-destructive).
//
// Merge tie-break: when multiple raw names collapse to the same canonical
// target (e.g. a bare "Moby" row and a "Moby, Jim James" collaboration
// credit both collapsing to "Moby"), the row whose own raw name already
// equals its canonical form claims the target artwork slot first -- an
// artist's own genuine photo is preferred over a same-artist collaboration
// credit's photo, and the losing row's own artwork is left in place,
// untouched (non-destructive).
//
// Idempotent: re-running after a successful run finds no artwork row left
// at any already-rekeyed artist's old id, so nothing is rekeyed twice
// (report.Rekeyed == 0 on a clean second run).
//
// dryRun computes and returns the exact same report a real run would
// produce -- report.Rekeys reflects what WOULD move -- without writing
// anything.
func MigrateArtistArtwork(dao *DAO, dryRun bool) (*ArtistArtworkMigrationReport, error) {
	if dao == nil {
		return nil, fmt.Errorf("dao is nil")
	}
	db := dao.db.DB()
	if db == nil {
		return nil, fmt.Errorf("database not open")
	}

	rows, err := db.Query(`SELECT id, name FROM artists`)
	if err != nil {
		return nil, fmt.Errorf("scan artists: %w", err)
	}
	type artistRow struct{ id, name string }
	var all []artistRow
	for rows.Next() {
		var r artistRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan artist row: %w", err)
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("rows iter: %w", err)
	}
	_ = rows.Close()

	report := &ArtistArtworkMigrationReport{}
	claimed := make(map[string]bool)

	type candidate struct {
		id, name, canonical, newID, newArtworkID string
	}
	var candidates []candidate

	// Pass 1: rows whose raw name already equals its canonical form (the
	// common case -- 88 of 124 real values). Their own artist id already
	// equals the target id, so there is nothing to rekey; but if they have
	// their own artwork row, claim the target artwork slot so pass 2's
	// collapsible-variant rows never clobber a genuine same-name artist's
	// own artwork.
	for _, r := range all {
		report.Scanned++

		canonical := artistidentity.Collapse(r.name)
		if canonical == "" {
			report.Skipped++
			continue
		}

		newID := generateArtistID(canonical)
		if newID == r.id {
			ownArtworkID := r.id + "_artwork"
			if artworkRowExistsByID(db, ownArtworkID) {
				claimed[ownArtworkID] = true
			}
			report.Skipped++
			continue
		}

		candidates = append(candidates, candidate{
			id:           r.id,
			name:         r.name,
			canonical:    canonical,
			newID:        newID,
			newArtworkID: newID + "_artwork",
		})
	}

	// Pass 2: collapsible rows (raw name != canonical form). Rekey each
	// one's own artwork row onto the canonical target id, unless that
	// target is already claimed (by an exact-match row's own artwork, or by
	// an earlier collapsible row processed earlier in this same pass) or
	// already occupied in the DB (guards against a target populated by a
	// prior run, preserving idempotence).
	for _, c := range candidates {
		if claimed[c.newArtworkID] {
			report.Skipped++
			continue
		}
		if artworkRowExistsByID(db, c.newArtworkID) {
			report.Skipped++
			continue
		}

		oldArtworkID := c.id + "_artwork"
		if !artworkRowExistsByID(db, oldArtworkID) {
			// Nothing to rekey -- no artwork was ever fetched for this raw
			// identity.
			report.Skipped++
			continue
		}

		report.Rekeys = append(report.Rekeys, ArtistRekey{
			OldID:     c.id,
			OldName:   c.name,
			NewID:     c.newID,
			NewName:   c.canonical,
			ArtworkID: c.newArtworkID,
		})
		report.Rekeyed++
		claimed[c.newArtworkID] = true

		if dryRun {
			continue
		}

		if err := rekeyArtistArtworkRow(dao, oldArtworkID, c.newArtworkID, c.newID); err != nil {
			return nil, fmt.Errorf("rekey artist %q -> %q: %w", c.id, c.newID, err)
		}
	}

	return report, nil
}

func artworkRowExistsByID(db *sql.DB, artworkID string) bool {
	var exists int
	err := db.QueryRow(`SELECT 1 FROM artwork WHERE id = ?`, artworkID).Scan(&exists)
	return err == nil
}

// rekeyArtistArtworkRow moves one artwork row's id/artist_id onto the new
// identity, then best-effort updates the new-identity artists row's
// artwork_id (a no-op if that row doesn't exist yet -- expected when this
// runs before the next cache rebuild inserts it; zero rows affected is not
// treated as an error).
func rekeyArtistArtworkRow(dao *DAO, oldArtworkID, newArtworkID, newArtistID string) error {
	tx, err := dao.db.BeginTx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`UPDATE artwork SET id = ?, artist_id = ? WHERE id = ?`,
		newArtworkID, newArtistID, oldArtworkID); err != nil {
		return fmt.Errorf("update artwork: %w", err)
	}
	if _, err := tx.Exec(`UPDATE artists SET artwork_id = ? WHERE id = ?`,
		newArtworkID, newArtistID); err != nil {
		return fmt.Errorf("update artists: %w", err)
	}

	return tx.Commit()
}
