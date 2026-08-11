# Phase 2: Artist Identity & Artwork Migration - Context

**Gathered:** 2026-08-12
**Status:** Ready for planning
**Source:** Locked user decisions from `/gsd:new-project` questioning (2026-08-11, recorded in
`.planning/PROJECT.md` § Key Decisions) plus live evidence measured on the Pi. Not auto-answered
guesses — every D-xx below traces to an explicit user choice or a verified measurement.

<domain>
## Phase Boundary

The artist list shows one row per real performer, using a uniform collapse rule validated against
the real library, **and** every artwork row that resolved before the collapse still resolves after
it. The collapse and the artwork re-key ship together — never separately.

Out of this phase: duplicate-album badges and empty-state rendering (Phase 3), the `.dff` conversion
(Phase 4), the MPD-as-source-of-truth refactor (v2 milestone).

</domain>

<decisions>
## Implementation Decisions

### The collapse rule (ARTIST-01, ARTIST-02)
- **D-01:** Collapse to the **first credited performer**, uniformly across all genres. The user chose
  this over a classical-specific rule and over "keep the full ensemble string". `Herbert von Karajan
  Wiener Philharmoniker` → `Herbert von Karajan`; every `Luciano Pavarotti, <label>` variant → one
  `Luciano Pavarotti`.
- **D-02:** Composer is **not** surfaced as an artist. A separate Composer browse axis was considered
  and deferred (BROWSE-06). Do not add one here.
- **D-03:** The rule must handle all four join conventions observed live, proven by a table-driven
  test built from the **real 123 `list artist` values**, not invented examples:
  | Convention | Example |
  |---|---|
  | comma | `Duke Ellington, John Coltrane` |
  | spaced hyphen | `Adderley - Coltrane - Chambers - Cobb - Kelly` |
  | role suffix | `Ella Fitzgerald - vocals  Paul Smith - piano` |
  | `with` / `and his orchestra` | `Ella Fitzgerald with Nelson Riddle And His Orchestra` |
  Note the **double space** in `Herbert von Karajan  Wiener Philharmoniker` and in the Fitzgerald
  role-suffix case — whitespace is not reliably single.
- **D-04:** A naive comma split handles exactly one of the four. Do not ship one.
- **D-05:** The empty artist value present in MPD's `list artist` output must not render as a blank
  row on either client (ARTIST-03).
- **D-06 (Claude's discretion):** whether the collapse happens at query time, at cache-build time, or
  both; the exact package placement; whether a small hand-maintained exception list is warranted for
  names the heuristic gets wrong. Prefer a pure function with no I/O so it is trivially testable.

### Artwork identity and migration (ART-01, ART-02, ART-03)
- **D-07:** **Migrate and re-key. Do not let enrichment re-fetch.** The user chose this explicitly.
  258 MB of artwork was fetched against rate-limited APIs (MusicBrainz at 1 req/s); re-fetching costs
  hours and API quota.
- **D-08:** The migration must **also recover the 38 already-orphaned album-artwork rows** left by
  the NAS→SSD move, not just handle the artist collapse.
- **D-09:** The migration must be **idempotent** (re-running changes nothing) and **non-destructive**
  (it never deletes files under the artwork cache directory).
- **D-10:** The collapse and the migration land in the **same phase**, and the collapse must never
  ship to the Pi without the migration. Artist identity is `md5(name)`
  (`internal/infra/cache/builder.go:433`) — changing the name changes the key and orphans the image.

### Sequencing safety
- **D-11:** Before/after evidence is required, not assertions: capture artwork-link counts (and/or
  screenshots) on the Pi before the collapse and after the migration, and show that artists which had
  images still have them.
- **D-12:** Success for ART-02 is the "albums with no artwork link" count dropping from 39 toward 0
  **with zero new network calls** to Fanart.tv, Deezer, or MusicBrainz. A migration that silently
  triggers re-fetching has failed even if the count improves.

</decisions>

<specifics>
## Specific Ideas

- Live baseline measured after Phase 1 (use these, re-measure to confirm):
  - MPD: 814 songs (803 USB + 11 INTERNAL), 122→ recount artists live
  - `mpc list artist` = 123 distinct values; `mpc list albumartist` = 49
  - cache: 81 albums, 49 artists, 127 artwork rows
  - **38 orphaned album-artwork rows** vs **39 albums with no artwork link**
- The Karajan album is the visible acceptance case: after Phase 1 it is a real 10-track album with
  `AlbumArtist = "Herbert von Karajan"` already set, while `Artist` remains
  `Herbert von Karajan  Wiener Philharmoniker`. So the collapse must make the **Artist** list agree
  with what the AlbumArtist already says.
- The two relink queries at `internal/infra/cache/builder.go:282` and `:344` already paper over the
  path-derived-identity problem for the unchanged-path case. The migration should complement them,
  not fight them.

</specifics>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project + architecture
- `/Users/eduardomarques/workspace/stellar-streamer/CLAUDE.md` — workspace layout, Pi topology,
  bit-perfect DO-NOT-TOUCH list, build/deploy commands per repo
- `.planning/PROJECT.md` — verified evidence, constraints, Key Decisions
- `.planning/phases/01-data-integrity-foundation/01-07-SUMMARY.md` — what Phase 1 changed on disk

### Code under change
- `internal/infra/cache/builder.go:433` — `generateArtistID` = `md5(name)`, the identity that moves
- `internal/infra/cache/builder.go:428` — `generateAlbumID` = `md5(albumArtist‖album‖uri)`
- `internal/infra/cache/builder.go:282` and `:344` — the existing album/artist artwork relink queries
- `internal/infra/cache/builder.go:307-366` — `buildArtists`, where collapsed names would be written
- `internal/domain/library/cached_service.go:193-255` — `GetArtists`, the read path and artwork URL
  resolution (`/artistart?id=`)
- `internal/domain/library/service.go:264-341` — MPD-direct `GetArtists` fallback
- `internal/infra/cache/dao.go` — `QueryArtists`, `GetArtworkByArtist`, `UpdateArtistArtwork`,
  `UpdateArtistArtworkURL`, `GetArtistsWithoutArtwork`, `GetAlbumsWithoutArtwork`
- `internal/infra/cache/sqlite.go:154-308` — schema; a migration may need a new schema version
  (currently `CurrentSchemaVersion = "5"`)

### Wire contract
- `/Users/eduardomarques/workspace/stellar-streamer/docs/SOCKET-CONTRACT.md` — `pushLibraryArtists`
  shape. A pure rename of displayed artist values needs **no** contract change; adding fields does.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/infra/musicfile` (new in Phase 1) is the precedent for a small, pure, leaf helper package
  with table-driven tests — the collapse function should follow that shape.
- The Phase 1 schema-migration pattern in `sqlite.go:96-151` (`initSchema` + version bump + tolerated
  duplicate-column errors) is the model for any schema change here.
- `deploy/verify-data-integrity.sh` establishes the on-Pi PASS/FAIL gate idiom; an artwork-integrity
  gate should match it.

### Established Patterns
- Layering is strictly one-way: `internal/infra` **never** imports `internal/domain` (verified in
  Phase 1). A collapse helper needed by both must live under `internal/infra/` as a leaf.
- Table-driven Go tests; `make check` = fmt + vet + golangci-lint. ~30 files of pre-existing gofmt
  drift and ~38-62 pre-existing lint findings are NOT this phase's problem — keep new files clean.
- MPD access is behind interfaces; tests fake them rather than needing a live MPD.

### Integration Points
- Backend → clients via `pushLibraryArtists` (display values only).
- `/artistart?id=<artistID>` resolves via the artwork table — this is the endpoint that breaks if
  identity changes without migration.
- Deploy: `make build` → scp → `sudo systemctl restart stellar-backend`.

</code_context>

<deferred>
## Deferred Ideas

- Composer browse axis — BROWSE-06, v2.
- Content-derived identity replacing `md5(name)` / `md5(albumArtist‖album‖uri)` — ARCH-02, v2. This
  phase migrates *within* the existing scheme; it does not replace it.
- `sort=year` / `sort=recently_added` being broken — BROWSE-05, v2.
- Duplicate-album badges — Phase 3.

</deferred>

---

*Phase: 02-artist-identity-artwork-migration*
*Context gathered: 2026-08-12*
