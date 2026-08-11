# Plan 02-04 Summary — Deploy Collapse + Artwork Migration, Live Verification

**Completed:** 2026-08-12
**Requirements:** ARTIST-01 ✅, ARTIST-02 ✅, ARTIST-03 ✅, ART-01 ✅, ART-02 ⚠ (see below), ART-03 ✅
**Status:** Complete

## Live outcome on the Pi

| Check | Before | After |
|---|---|---|
| `Herbert von Karajan` rows | `Herbert von Karajan  Wiener Philharmoniker` | **1 row, `Herbert von Karajan`** |
| `Luciano Pavarotti` rows | **15** label-credit variants | **1 row** |
| Blank artist row | present (MPD's empty `list artist` value) | **0** |
| Artists in cache | 49 | 45 |
| Artists with an artwork link | — | **45 / 45**, `/artistart` 200 on every sample |
| Albums without artwork link | 40 | 39 |
| Orphaned album-artwork rows | 38 | 37 |
| Artwork on disk | 270,994,699 bytes / 177 files | **identical** |

Startup log: `Artist artwork migration re-keyed rows onto collapsed identity on startup rekeyed=8`.

Binary sha256 `b48da71164907ba8148df10f…` — local build matches deployed byte-for-byte.
`/health` 200, `/ready` 200. Phase 1's `verify-data-integrity.sh` gates still all PASS.

## D-09 idempotence + non-destructiveness — verified by a second restart

```
before: artists/artwork/linked-albums = 45/129/42   artwork bytes=270994699 files=177
after : artists/artwork/linked-albums = 45/129/42   artwork bytes=270994699 files=177
✓ DB counts unchanged
✓ artwork files untouched
```

No rekey log lines on the second boot. Re-running is a true no-op.

## ART-02 — honest assessment: 1 of 38 recovered, and that is the correct answer

The perceptual matcher (`deploy/match-orphaned-artwork.py`) was calibrated against the 41 albums that
still held a valid artwork link, rather than using a guessed threshold:

```
same-album  n=  41  min=0  p50= 0  p90=21  p95=27  max=32
cross-album n=1640  min=0  p05=24  p50=32  max=48
```

**Cross-album minimum distance is 0** — distinct albums with perceptually identical covers. Cause
found in the library itself: multi-disc sets legitimately share one cover (11 albums under
`USB/Mahler The Symphonies`, 3 under Maria Callas, 2 under `Rated R - Deluxe Edition`). The
assumption underpinning the calibration — that different albums have different covers — is false
here. The script detected the overlap and fell back to exact-match-only rather than fabricating
links.

Result: **1 matched** (distance 0, runner-up 23, margin 23 — `Beethoven: Symphony No. 9`,
Pittsburgh Symphony Orchestra), **0 ambiguous**, **37 unmatched**.

The 37 unmatched cluster at best-candidate distance **17–29**, with a clean gap (nothing between 1
and 16). That band sits inside the cross-album distribution (p05 = 24), so raising the threshold to
21 would "recover" 10 links statistically indistinguishable from random pairings. That is
fabrication, not recovery. The user was shown this and chose to apply only the single confident
match.

**The decisive context:** 39 of the 40 unlinked albums **already return real cover art** via
`GET /albumart?path=…` (HTTP 200). `/albumart?path=` bypasses the artwork table entirely, so covers
already display on the LCD and the phone. The orphan rows are bookkeeping; their only real cost is
that the enrichment worker may re-fetch art for those albums.

ART-02's bar was "drops from 39 toward 0". It went 40 → 39. Technically met, trivially. Recorded
plainly rather than dressed up.

## Application method

`RekeyAlbumArtwork` exists in the new binary but has no CLI/endpoint wiring, and the binary was not
yet deployed at apply time. Per the plan, the single approved pair was applied by hand with
parameterized `sqlite3` statements mirroring the function's transaction exactly
(`internal/infra/cache/albummigration.go:144-158`), after checking its exact preconditions:

```
target album artwork_id: '' (empty → first application)   orphan row exists: 1
BEGIN;
  UPDATE artwork SET id=<album>_artwork, album_id=<album> WHERE id=<orphan>;
  UPDATE albums  SET artwork_id=<album>_artwork WHERE id=<album>;
COMMIT;
```

Post-apply the idempotence no-op path was confirmed: album links to the produced id and the artwork
row's `album_id` matches, so a repeat call returns nil without writing.

## Safety

- DB backup `library.db.pre-phase2-20260811T203306Z.bak` (434,176 bytes), `PRAGMA integrity_check` = `ok`.
- Before-evidence in `~/stellar-backend/data/phase2-evidence-20260811T203306Z/`.
- Zero external network calls during matching — `/albumart` is a local backend read.
- No writes to `/mnt/ssd` (mount stayed `ro`); no artwork files created, moved, or deleted.
- D-10 honoured: the collapse and the artwork migration deployed in the same binary, same restart.

## Deviations

1. **Executed inline by the orchestrator, not a `gsd-executor`.** Three consecutive executor spawns
   died on API session limits. Task 1 was SSH orchestration plus a Python script, done directly at a
   fraction of the cost. No plan content skipped.
2. **Matcher yielded far less than the plan hoped.** Not a defect — the calibration did its job and
   refused to guess. Documented above rather than papered over.

## Residue (not defects)

- **5 orphaned artist-artwork rows** (was 1). These are artwork rows belonging to collapsed-away
  artist variants whose canonical artist already had an image. All 45 artists have a working link and
  `/artistart` returns 200, so there is no user-visible effect.
- **37 orphaned album-artwork rows** remain, by explicit user decision.
- Both classes recur on any future storage migration until album/artist identity stops being derived
  from mutable inputs — that is ARCH-02, deferred to the v2 milestone.
