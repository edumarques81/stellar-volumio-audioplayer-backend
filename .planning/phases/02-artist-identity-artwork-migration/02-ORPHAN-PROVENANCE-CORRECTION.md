# Correction — the 38 orphaned album-artwork rows ARE recoverable

**Written:** 2026-08-12
**Supersedes:** the "material finding" in Plan 02-03/02-04's planning notes, which concluded the
orphans' provenance was unknown and the mapping mechanically unrecoverable.
**Trigger:** the user pointed out that the backend has enrichment/hydration functions and that these
rows were likely a consequence of them. That hypothesis was correct.

## What the planner got wrong

The planner brute-forced md5 reconstruction (81 albums × historical path prefixes × NFC/NFD
variants) against the 38 orphan hashes, found zero matches, and concluded the mapping was
unrecoverable because md5 is one-way. **The conclusion doesn't follow** — it only proves you cannot
invert the hash. It never checked where the rows came from.

## Actual provenance — a known code path

All 38 orphans carry **`source = 'backfill'`**, not `cover_art_archive`:

```
type    source             n
album   cover_art_archive  40
album   backfill           39   <- 38 of these 39 are orphaned
artist  fanarttv           27
artist  deezer             18
artist  albumart            5
```

They were written by `BackfillAlbumArtwork` (`internal/infra/cache/backfill.go:25`), which repairs
the pre-2026-05-27 state where the enrichment save path wrote the JPG but never inserted the artwork
row. It scans `<cacheDir>/artwork/albums/<album_id>.<ext>` and inserts a row with the deterministic
id `<album_id>_artwork` and `source: "backfill"`.

So these rows were created **legitimately, while their album IDs still existed**. They orphaned later
when album identity (`md5(albumArtist‖album‖uri)`) changed underneath them. `38 of 38` orphan ids end
in `_artwork`, exactly matching the convention — further confirmation.

## Why that makes them recoverable

The hash cannot be inverted, but **the artwork itself is still on disk and is the actual cover
image**. Identity can be recovered by comparing pictures, not by reversing hashes.

Verified live on the Pi:

| Fact | Value |
|---|---|
| Orphan rows | 38 |
| Orphan files present on disk | **38 / 38** (zero missing) |
| Total bytes of orphaned cover art | **61,529,868** (~61.5 MB) |
| Albums currently with no artwork link | 40 |
| Unlinked albums that return art via `/albumart` | HTTP 200 with real bytes (sampled 3/3) |
| `PIL` on the Pi | **9.4.0** — perceptual hashing is available |

38 orphan images against 40 unlinked albums is close to a 1:1 mapping.

## Recommended approach — replaces "Claude views each orphan JPG"

Plan 02-04 currently proposes recovering the mapping by having the agent visually inspect each
orphan image. That is slow, unauditable, and not reproducible. Do this instead:

1. For each of the 40 albums with no artwork link, fetch its current cover via
   `GET /albumart?path=<first_track>` (embedded/folder art — no network, already proven to return 200).
2. Compute a perceptual hash (dHash or aHash via PIL) for each of those 40 images and for each of the
   38 orphan files.
3. Match on smallest Hamming distance, with a distance threshold and a uniqueness requirement
   (reject ambiguous ties rather than guessing).
4. Emit a **dry-run report** of proposed `orphan → album` links for human approval, then apply.
5. Unmatched orphans stay in place — untouched, not deleted (D-09 non-destructive).

This is deterministic, re-runnable, produces an auditable mapping table, and makes **zero network
calls** (D-12 satisfied — `/albumart` reads local files and MPD, it does not call Fanart/Deezer/CAA).

## Caveat to test, not assume

Orphan files came from Cover Art Archive; `/albumart` returns embedded or folder art. For the same
album these are usually the *same cover* but often **different scans/resolutions**, so exact byte
equality will mostly fail. That is why perceptual hashing, not `sha256`, is the right comparator —
but the distance threshold must be calibrated against a few known-good pairs before trusting it in
bulk. If perceptual matching proves unreliable on this corpus, fall back to human review of only the
ambiguous cases, not all 38.

## Also worth knowing

`BackfillAlbumArtwork` already runs on startup and is idempotent. It is not the bug — it is the
repair mechanism that ran correctly. The residual problem is upstream: album identity is derived from
a mutable file path, so any move re-keys every album (ARCH-02, deferred to v2). Until that changes,
orphaning will recur on the next storage migration, and this recovery step will be needed again —
which is an argument for landing it as a reusable, tested migration rather than a one-off cleanup.
