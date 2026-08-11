# Phase 3: Browse Experience — Context

**Gathered:** 2026-08-12
**Status:** Ready for planning
**Source:** Locked decisions from `/gsd:new-project` (2026-08-11) **plus three scope decisions the
user made on 2026-08-12** after live data invalidated the original design. All grounded in
measurements taken against the Pi after Phase 2 shipped.

<domain>
## Phase Boundary

Every browse surface on the LCD and the iPhone app tells the truth: multiple copies of the same album
are distinguishable at a glance, multi-disc box sets read as one album rather than N identical tiles,
and an artist with no browsable albums never looks like a dead end.

Out of scope: the `.dff` conversion (Phase 4), the MPD-as-source-of-truth refactor (v2), the
Miles Ahead cover-art investigation (backlog 999.1).

</domain>

<decisions>
## Implementation Decisions

### The disambiguation badge (BROWSE-01/02/03) — REVISED 2026-08-12
- **D-01 (SUPERSEDED):** the original decision was "show the quality label as a badge whenever two or
  more albums share title+artist".
- **D-02 (CURRENT):** **the badge shows whatever actually differs within the group** — quality if it
  differs, else disc, else source. Reason: live measurement showed **6 of 8 duplicate groups have
  only ONE distinct quality**, so a quality badge would print identical text on every tile and
  disambiguate nothing.
  Precedence for choosing the badge value within a duplicate group:
  1. **quality** (`sample_rate`/`bit_depth`/`track_type` via `formatQualityLabel`) if it differs
  2. **disc** (`CD 04`) if quality is uniform but disc differs
  3. **source** (`USB` / `LOCAL` / `NAS`) if quality and disc are uniform but source differs
  4. if nothing differs, show no badge rather than a meaningless one
- **D-03:** a unique album (no title+artist duplicate) shows **no badge** — the common case stays
  clean. 81 albums, only 8 duplicate groups.

### Multi-disc box sets (NEW — user folded this into Phase 3 on 2026-08-12)
- **D-04:** Multi-disc sets must render as **one album tile that drills into its discs**, not as N
  identical tiles. `Mahler: The Symphonies` currently renders as **11 tiles all with the same title**,
  which is worse UX than any missing badge.
- **D-05:** Disc detection has **two agreeing signals**, both verified live — prefer the tag, use the
  path as corroboration:
  1. **MPD `Disc` tag** — present and correct: Mahler `1..11`, Rated R `1,2`, Woody Allen `1,2`,
     Tosca `1,2,3`.
  2. **Path marker** `/CD ?\d+/` — present for exactly the same 4 album roots:
     `USB/Mahler The Symphonies`, `USB/Maria Callas…`, `USB/Queens Of The Stone Age/Rated R - Deluxe
     Edition`, `USB/Various Artists/BD Music Presents Woody Allen's Movies, Vol. 1`.
- **D-06:** `Miles Davis - Kind Of Blue` is **NOT** multi-disc — every track is `Disc: 1` across its
  3 folders. It is 3 distinct releases (2× DSF, 1× FLAC 352.8/24) and must keep using the badge, not
  the disc grouping. This is the load-bearing negative test case: do not group on title alone.
- **D-07:** Grouping multi-disc sets removes 4 of the 8 duplicate groups from the badge's remit. The
  two features interact — build and test them together.

### Empty states (ARTIST-04 / BROWSE-04)
- **D-08:** **No artist in the library currently has zero albums** (`album_count=0` → 0 rows), so the
  empty-grid state cannot be reproduced live. The original symptom (Karajan) was fixed by Phase 1's
  tagging.
- **D-09:** Implement the fallback **defensively anyway**, proven by a **synthetic test fixture**
  rather than a live case. Phase 1 demonstrated untagged imports do happen, and backlog 999.2 (file
  ingest) will make them more likely.

### Wire contract
- **D-10:** The badge value and any loose-song/disc payload are **new fields on existing events**.
  Additive fields are safe for the v2/v3/v4 Socket.IO client matrix, but this is still a **three-repo
  change**: backend + `Volumio2-UI` + `stellar-ios` ship together, and
  `docs/SOCKET-CONTRACT.md` must be updated in the same change.
- **D-11:** Do **not** rename or remove existing fields. Volumio Connect apps (v2 clients) consume
  this surface.

### Claude's discretion
- Where the badge value is computed (backend vs client). Backend is preferred so both clients agree
  and the logic is tested once.
- Exact badge wording/abbreviations, and its visual treatment within the existing tile design.
- Whether disc grouping happens at cache-build time or query time.

</decisions>

<specifics>
## Specific Ideas

Live duplicate groups measured 2026-08-12 (81 albums, 8 duplicate groups):

| Album | versions | distinct quality | real differentiator |
|---|---:|---:|---|
| Mahler: The Symphonies | 11 | 1 | **disc** (CD 01–11) |
| Miles Davis - Kind Of Blue | 3 | 2 | **quality** (2× DSD, 1× 352.8/24 FLAC) |
| Puccini: Tosca (Callas) | 3 | 1 | **disc** (CD 1–3) |
| BD Music … Woody Allen Vol. 1 | 2 | 1 | **disc** (CD 1–2) |
| Djesse Vol. 4 (Deluxe) | 2 | 1 | **source/path** (two copies) |
| Rated R - Deluxe Edition | 2 | 1 | **disc** (CD 1–2) |
| The Future Is Now | 2 | 2 | **format** (FLAC vs WAV) |
| The Light For Days | 2 | 1 | **source** (LOCAL/INTERNAL vs USB) |

**The Future Is Now** is the ready-made acceptance case for the badge — created deliberately in
Phase 1 by tagging the WAV edition to match its FLAC sibling. FLAC vs WAV, same title and artist.

**Mahler: The Symphonies** is the acceptance case for disc grouping: 11 → 1 tile.

</specifics>

<canonical_refs>
## Canonical References

### Project + contract
- `/Users/eduardomarques/workspace/stellar-streamer/CLAUDE.md` — workspace layout, all three repos'
  build/test/deploy commands, bit-perfect DO-NOT-TOUCH list
- `/Users/eduardomarques/workspace/stellar-streamer/docs/SOCKET-CONTRACT.md` — **must be updated**;
  see § MPD-driven library (`pushLibraryAlbums`, `pushLibraryArtistAlbums`, `pushLibraryAlbumTracks`)
- `.planning/phases/02-artist-identity-artwork-migration/02-04-SUMMARY.md` — what Phase 2 deployed

### Backend
- `internal/domain/library/cached_service.go:12` — `formatQualityLabel`, the existing quality string
- `internal/domain/library/cached_service.go:96` — `GetAlbums` (cache path)
- `internal/domain/library/service.go:74` / `:349` — `GetAlbums` / `GetArtistAlbums` (MPD-direct)
- `internal/domain/library/types.go` — the `Album` struct that gains a badge field
- `internal/infra/cache/builder.go:197` — `buildAlbums`, where disc grouping would land
- `internal/infra/mpd/client.go:723` — `GetAlbumDetails`, source of `Disc` data

### Frontend (`Volumio2-UI`, branch `master`)
- `src/lib/stores/library.ts` — album/artist stores
- `src/lib/components/` — AlbumGrid tile (badge render target); LCD is 1920x440, touch targets 44px min

### iOS (`stellar-ios`, branch `main`)
- `Stores/AlbumPickerStore.swift`, `Stores/ArtistPickerStore.swift`, `Models/LibraryModels.swift`
- `Views/Library/AlbumPickerView.swift` — badge render target
- Reminder: `xcodegen generate --spec project.yml` after adding files; `IOS_SIM_ID` must be set

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `formatQualityLabel(sampleRate, bitDepth, trackType)` already produces the exact strings the badge
  needs (`352.8kHz/24bit FLAC`, `DSD64`) — reuse, do not reimplement.
- `Album.Source` already carries `nas`/`usb`/`local`, so the source-fallback badge needs no new data.
- Phase 1's `internal/infra/musicfile` and Phase 2's `internal/infra/artistidentity` are the
  precedent: small pure leaf packages under `internal/infra/` with table-driven tests.

### Established Patterns
- `internal/infra` never imports `internal/domain`. A badge/disc helper needed by the cache builder
  must be a leaf under `internal/infra/`.
- Backend computes, clients render — keeps the two clients consistent and the logic tested once.
- Go: TDD, table-driven, `make check`. Pre-existing gofmt drift (~30 files) and lint findings (~62)
  are not this phase's problem.

### Integration Points
- `pushLibraryAlbums` / `pushLibraryArtistAlbums` carry the album list to both clients.
- `library:album:tracks` → `pushLibraryAlbumTracks` is where a disc-grouped album's discs would be
  expanded.
- Deploy: backend `make build` → scp → restart; frontend `npm run build` → scp `dist/` to
  `/home/eduardo/stellar-volumio`; iOS via simulator.

</code_context>

<deferred>
## Deferred Ideas

- Composer browse axis — BROWSE-06, v2.
- `sort=year` / `sort=recently_added` broken — BROWSE-05, v2.
- Content-derived identity (stops orphaning recurring) — ARCH-02, v2.
- Miles Ahead cover art not displaying — backlog 999.1.
- File ingest to the Pi — backlog 999.2.

</deferred>

---

*Phase: 03-browse-experience*
*Context gathered: 2026-08-12*
