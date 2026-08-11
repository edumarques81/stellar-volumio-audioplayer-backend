# Requirements: Stellar Streamer — Library Browsing

**Defined:** 2026-08-11
**Core Value:** Every album on the disk is findable and playable from the LCD and the phone, and the
browse surface is honest about what it's showing.

## v1 Requirements

### Library Data Integrity

- [x] **DATA-01**: Every real audio file on the SSD carries an `Album` tag, so no folder is dropped
  by `GetAlbumDetails`. Verified by a library-wide scan reporting zero untagged real songs
  (currently 16 across 3 folders).

- [x] **DATA-02**: If untagged files ever reappear, they are **detectable rather than silent** — the
  backend surfaces a count of skipped/untagged files (log + a field on the existing cache-status
  payload) instead of quietly omitting them from browse results. *(Plan 01-02 built the mechanism
  end-to-end: `groupAlbumDetails` counts skipped songs and logs it; `CacheStats.SkippedCount` is
  persisted on every `FullBuild` and surfaced as `skippedCount` on `pushLibraryCacheStatus` /
  documented in `docs/SOCKET-CONTRACT.md`. Not yet checked off: D-09 defines success as the count
  reading **0** after the DATA-01 retag, and that retag (Plan 01-07) is still blocked on the user.
  Live count today is 16, matching the documented baseline.)*

- [x] **DATA-03**: MPD's database contains no macOS `._` resource-fork entries. Verified by
  `mpc stats` song count matching the real file count (currently 1380 indexed vs 803 real).

- [x] **DATA-04**: The backend ignores `._` files consistently everywhere it reads MPD song lists —
  not only in `GetAlbumTracks` — so a future macOS copy cannot corrupt album track counts or
  artist counts. *(Plan 01-01 delivered the shared `musicfile.IsResourceFork` predicate and
  refactored `GetAlbumTracks` to use it. Plan 01-02 hardened `GetAlbumDetails` — the single choke
  point behind `GetAlbums`, `GetArtistAlbums`, the cache builder's `buildAlbums`, and
  `localmusic`'s album browsing — via a new unit-tested `groupAlbumDetails` extraction. The cache
  builder paths (Plan 01-05) still need to adopt it before this requirement is fully satisfied.)*

### Artist Listing

- [x] **ARTIST-01**: The artist list shows one entry per main artist. Credited-collaborator strings
  collapse to the first credited performer, so `Herbert von Karajan  Wiener Philharmoniker` appears
  as `Herbert von Karajan` and every `Luciano Pavarotti, <label>` variant lands under
  `Luciano Pavarotti`.

- [x] **ARTIST-02**: The collapse rule handles all four join conventions observed in the real
  library — comma (`Duke Ellington, John Coltrane`), spaced hyphen
  (`Adderley - Coltrane - Chambers - Cobb - Kelly`), role suffix
  (`Ella Fitzgerald - vocals  Paul Smith - piano`), and the `with`/`and his orchestra` form
  (`Ella Fitzgerald with Nelson Riddle And His Orchestra`) — proven by a table-driven test built
  from the actual 123 artist values.

- [x] **ARTIST-03**: The empty artist value present in MPD's `list artist` output does not produce a
  blank row in the artist list on either client.

- [ ] **ARTIST-04**: Tapping an artist always leads somewhere real — an artist that resolves to zero
  albums never renders as a silently empty grid (this was the reported symptom).

### Artwork Identity

- [x] **ART-01**: Existing artist artwork survives the ARTIST-01 collapse. A one-shot migration
  re-points artwork rows onto the new artist identities; no artwork is re-downloaded from
  Fanart.tv / Deezer / MusicBrainz.

- [x] **ART-02**: The 38 album-artwork rows orphaned by the NAS→SSD move are reconnected to their
  albums, reducing "albums with no artwork link" from 39 toward 0 without re-fetching.

- [x] **ART-03**: The migration is idempotent and reversible — re-running it changes nothing, and
  the artwork files on disk are never deleted by it.

### Browse Experience

- [ ] **BROWSE-01**: When two or more albums share the same title and artist, each tile shows a badge on
  the **LCD** carrying whatever actually differs within that group — quality if it differs, else disc,
  else source. (Revised 2026-08-12: 6 of 8 live duplicate groups have identical quality, so a
  quality-only badge would disambiguate nothing.)

- [ ] **BROWSE-02**: The same duplicate-disambiguating badge appears in the **iPhone app**'s album
  and artist-album grids.

- [ ] **BROWSE-03**: No badge is shown when an album's title+artist is unique, so the grid stays
  clean for the common case.

- [x] **BROWSE-07**: Multi-disc box sets render as **one album tile that drills into its discs**,
  not as N identical tiles. `Mahler: The Symphonies` goes from 11 tiles to 1. Disc detection uses
  MPD's `Disc` tag, corroborated by the `/CD nn/` path marker. `Miles Davis - Kind Of Blue` must NOT
  group (all tracks are `Disc: 1`; it is 3 distinct releases) — the negative test case.

- [ ] **BROWSE-04**: An artist whose tracks belong to no album renders those tracks as a playable
  song list rather than an empty album grid.

### Tail Cleanup

- [ ] **DATA-05**: The last untagged file — `USB/Sigxer SU-6 test/DSD-测试文件 ANNOUNCEMENT FOR BASIC
  CHECKS (Voice).dff` — is converted to `.dsf` **on the MacBook** (copy off the Pi, convert, copy
  back; never mount the SSD on macOS) so MPD indexes its `Album`/`AlbumArtist` and `skippedCount`
  reaches 0. The DSD bitstream must survive bit-perfectly — a DSD→PCM conversion fails this
  requirement. Runs last, after all feature phases.

## v2 Requirements

Deferred. Tracked but not in this roadmap.

### Library Architecture

- **ARCH-01**: MPD becomes the sole source of truth for library data; SQLite is demoted to an
  enhancement-only store (artwork, bios, last-played, plus future favourites / play counts /
  ratings).

- **ARCH-02**: Album and artist identity move from path-derived `md5(albumArtist‖album‖uri)` to a
  content-derived key, so file moves never orphan enrichment again.

- **ARCH-03**: `GetAlbumDetails` stops issuing `search base <path>` (full-subtree scan per request)
  in favour of MPD's `list … group …` plus `search … sort … window` server-side paging.

- **ARCH-04**: Dead cache surface removed — the `tracks` table, `BuildAlbumTracks`, and
  `GetTracksByAlbum` (all currently zero-caller / zero-row).

### Browse

- **BROWSE-05**: `sort=year` and `sort=recently_added` actually work (today no `Year` is mapped from
  MPD and `AddedAt` is set to cache-build time, so both silently degrade to title sort).

- **BROWSE-06**: A dedicated Composer browse axis for the classical-heavy portion of the library.

## Out of Scope

| Feature | Reason |
|---------|--------|
| Folder-name fallback for untagged albums | Rejected in favour of fixing tags at source — avoids a heuristic that must parse strings like `..._FLAC_352k-24b`. DATA-02 covers recurrence. |
| Composer as a first-class artist in the artist list | The uniform "first credited performer" rule was chosen instead; a Composer axis is deferred to BROWSE-06. |
| Grouping duplicate albums into one tile with a version chooser | Considered; the quality badge was chosen as it costs no extra tap and reuses data the backend already computes. |
| Any change to the bit-perfect audio chain | Permanently excluded without a separate explicit decision — mixer_type none, raw `hw:` ALSA, native DSD, shairport volume policy. |
| Deleting or re-encoding the user's music files | Masters are irreplaceable. Only the `._` sidecar junk is removed, with explicit confirmation. |
| The MPD-as-source-of-truth refactor | Deferred to v2 (ARCH-01..04) so the artist-identity work can inform the design first. |

## Traceability

Which phases cover which requirements. Filled during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| DATA-01 | Phase 1 | Complete (15/16; 1 `.dff` blocked by MPD's DSDIFF reader) |
| DATA-02 | Phase 1 | In Progress (01-02 mechanism done; blocked on 01-07 user retag for D-09's "count == 0") |
| DATA-03 | Phase 1 | Complete |
| DATA-04 | Phase 1 | In Progress (01-01, 01-02 done; 01-05 pending) |
| ARTIST-01 | Phase 2 | In Progress (02-01 Collapse() rule implemented + corpus-tested; 02-02 wired it into buildArtists + Service.GetArtists with merged counts, unit-tested against real Pavarotti/Karajan/Moby cases — code-complete but unverified on the Pi. D-10 requires the artwork migration (02-03) to ship first; 02-04 deploys both together and captures the on-Pi before/after evidence this requirement's success criteria describe) |
| ARTIST-02 | Phase 2 | Complete |
| ARTIST-03 | Phase 2 | In Progress (02-01 Collapse("") returns "" without panicking; 02-02 wired the empty-value skip into both buildArtists and Service.GetArtists, unit-tested — code-complete but unverified on the Pi; gated on 02-04 same as ARTIST-01) |
| ARTIST-04 | Phase 3 | Pending |
| ART-01 | Phase 2 | Complete |
| ART-02 | Phase 2 | Complete (1/38 recovered; 39 of 40 unlinked albums already display covers via /albumart — see 02-04-SUMMARY) |
| ART-03 | Phase 2 | Complete |
| BROWSE-01 | Phase 3 | Pending |
| BROWSE-02 | Phase 3 | Pending |
| BROWSE-03 | Phase 3 | Pending |
| BROWSE-04 | Phase 3 | Pending |
| BROWSE-07 | Phase 3 | Complete |
| DATA-05 | Phase 4 | Pending |

**Coverage:**

- v1 requirements: 17 total
- Mapped to phases: 17 (Phase 1: 4, Phase 2: 6, Phase 3: 6, Phase 4: 1)
- Unmapped: 0 ✓

---
*Requirements defined: 2026-08-11*
*Last updated: 2026-08-11 after initialization*
