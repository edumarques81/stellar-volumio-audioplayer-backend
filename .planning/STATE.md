---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: milestone
status: completed
stopped_at: Completed 03-01-PLAN.md
last_updated: "2026-08-11T21:51:45.226Z"
last_activity: 2026-08-11
progress:
  total_phases: 6
  completed_phases: 2
  total_plans: 21
  completed_plans: 12
  percent: 33
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-08-11)

**Core value:** Every album on the disk is findable and playable from the LCD and the phone, and the
browse surface is honest about what it's showing.
**Current focus:** Phase 2 — Artist Identity & Artwork Migration

## Current Position

Phase: 2 (Artist Identity & Artwork Migration) — EXECUTING
Plan: Phase 2 complete (4/4). Next: Phase 3.
Status: Phase 2 complete. Next: Phase 3 (Browse Experience — three-repo change).
Last activity: 2026-08-11

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity:**

- Total plans completed: 0
- Average duration: - min
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| - | - | - | - |

**Recent Trend:**

- Last 5 plans: -
- Trend: -

*Updated after each plan completion*
| Phase 01 P01 | 10min | 2 tasks | 4 files |
| Phase 01 P04 | 20min | 2 tasks | 1 files |
| Phase 01 P02 | 35min | 3 tasks | 9 files |
| Phase 01 P05 | 15min | 2 tasks | 2 files |
| Phase 02 P01 | 2min | 2 tasks | 2 files |
| Phase 02 P02 | 12min | 2 tasks | 4 files |
| Phase 02 P03 | 35min | 2 tasks | 5 files |
| Phase 03 P01 | 15min | 2 tasks | 2 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- [Roadmap]: DATA-03/DATA-04 (`._` cleanup + defensive hardening) scheduled first — 41% of MPD's
  index is junk and pollutes any counting done by later phases.

- [Roadmap]: ART-01/02/03 (artwork re-key) paired into the same phase as ARTIST-01 (artist collapse)
  so the identity change never orphans artwork before the migration lands.

- [Roadmap]: ARTIST-04 (empty-grid artist) merged into Phase 3 with BROWSE-04 since both require the
  same underlying "loose songs render as a song list" fix on both clients.

- [Phase 1 planning]: The `._` predicate goes in a NEW leaf package `internal/infra/musicfile`, NOT
  `internal/domain/library`. Verified: `internal/infra/**` imports `internal/domain/**` in zero
  non-test files, while domain→infra happens in 5+ files. Since `GetAlbumDetails` (which needs the
  filter) lives in `internal/infra/mpd`, a domain-side helper would invert the layering for the
  first time in this codebase.

- [Phase 1 planning]: `GetAlbumDetails` in `internal/infra/mpd/client.go` is the single chokepoint —
  `GetAlbums`, `GetArtistAlbums`, the cache builder, and localmusic all route through it. Hardening
  it once covers all four browse paths (call graph traced and confirmed by the plan-checker).

- [Phase 1 planning]: Skipped-count wiring adds an additive `CountUntagged` method to
  `cache.MPDDataProvider` rather than changing `GetAlbumDetails`'s signature, which would have
  forced churn across 3 unrelated interfaces.

- [Phase 01]: musicfile helper placed at internal/infra/musicfile (leaf, zero internal deps), not internal/domain/library/filter.go — internal/infra/mpd (Plan 01-02) needs the predicate and internal/infra never imports internal/domain elsewhere in this codebase
- [Phase 01]: player.isAppleDouble left as-is, not refactored to use musicfile — out of Plan 01-01 scope per its interfaces section; musicfile is the canonical implementation for new call sites going forward
- [Phase 01]: DATA-03: mpc stats' global Songs: line aggregates MPD's INTERNAL source (11 unrelated files) alongside USB (/mnt/ssd/Music, the SSD); verify-data-integrity.sh scopes I1 to mpc listall USB rather than mpc stats to avoid a false FAIL from that unrelated source.
- [Phase ?]: groupAlbumDetails stays package-mpd-local; only the resource-fork predicate is shared via musicfile
- [Phase ?]: buildSkippedCount runs after buildAlbums, non-fatal on error, matching the buildRadioStations precedent
- [Phase ?]: mpdDataProviderAdapter.CountUntagged wires through existing SearchByBase, avoiding a 3-interface signature-change blast radius
- [Phase 01]: DATA-04 verified live: I3 gate diffs the FULL albums.track_count + artists.album_count snapshot before/after a synthetic ._ file, not a fixed set of pre-selected IDs — No per-album ID bookkeeping needed and catches a regression anywhere in the library, matching the ROADMAP criterion wording (any album... any artist)
- [Phase 01]: Cache rebuilds triggered via new deploy/rebuild-cache.py (python-socketio client), not HTTP or CDP browser automation — Backend has no HTTP rebuild endpoint; CDP dynamic-import of /src/lib/services/socket.ts fails against the kiosk's production build, which has no dev-server module path
- [Phase 02]: Single earliest-delimiter-wins scan (comma, ' - ', ' with ', double-space) handles all four ROADMAP join conventions with one code path in artistidentity.Collapse — No per-convention special-casing needed; verified against all 124 real corpus values with zero mismatches
- [Phase 02]: No hand-maintained exception list for Collapse (D-06 discretion exercised toward uniform rule) — Adderley -> Adderley is a known, accepted imperfection, pinned by its own explicit standalone test rather than silently accepted as a table row
- [Phase 02]: artistidentity package placed at internal/infra/artistidentity (leaf, zero internal deps) — Mirrors internal/infra/musicfile from Phase 1 -- internal/infra/cache (a later plan) needs Collapse without inverting the infra->domain import layering
- [Phase 02]: Collapse wired at BOTH call sites (cache-build + MPD-direct fallback) per D-06, mirroring Phase 1 DATA-04 precedent
- [Phase 02]: Merge-by-sum on collision (not last-write-wins) in both paths, proven with real 16-variant Pavarotti and Moby corpus cases
- [Phase 02]: Service.GetArtists query filter runs against the canonical (post-collapse) name, not the raw MPD tag
- [Phase 02-03]: RekeyAlbumArtwork idempotence check ordered target-album-state-first (not orphan-row-first) so a safe repeat call never fails on the renamed-away orphan id — hard_constraint #2 explicitly flagged this exact case as a prior planning mistake to correct
- [Phase 02-03]: MigrateArtistArtwork merge tie-break scans all rows in pass 1 (not a pre-filtered candidate list) so exact-match rows can still claim their own artwork slot — resolves an internal inconsistency in the plan's action wording; id always equals md5(name) for pre-migration rows
- [Phase ?]: [Phase 03-01]: Renamed discgroup entrypoint from Group to GroupFolders (plan's interfaces contract specified both a type and func named Group in the same package, which Go rejects) — Plan 03 wave-2 callers must use discgroup.GroupFolders(folders), not discgroup.Group(folders)

### Verified Environment Facts (measured 2026-08-11, supersede earlier estimates)

- `/mnt/ssd` is mounted **read-only** (`ro,nofail,uid=mpd,gid=audio` in `/etc/fstab`; live `findmnt`
  confirms `ro`). `blockdev --getro /dev/sda1` returns `0`, so it is a software mount-option flip,
  not hardware write protection — deletion requires a `remount,rw` → delete → `remount,ro`
  round-trip. Passwordless sudo works on the Pi.

- Live counts: `mpc stats` = **1380** songs; **803** real audio files on disk; **699** `._` files
  carrying audio extensions; **934** `._` files total. The earlier "566" figure was scoped to
  `search base "USB"` and to MPD-indexed entries only — it understated the cleanup. Plans treat live
  measurement as authoritative rather than hardcoding these.

### Pending Todos

- **USER REMINDER (set 2026-08-11):** Once Phase 1 closes, run `/gsd:autonomous` to carry Phases 2
  and 3 through discuss → plan → execute without per-phase prompting. The user explicitly chose to
  wait until Phase 1's human gates are cleared before switching to autonomous mode. **Surface this
  reminder at the Phase 1 → Phase 2 boundary.**

### Blockers/Concerns

- DATA-01 requires the user to retag 16 real audio files in their own tag editor (agent must not
  write to `/mnt/ssd/Music`). Phase 1 delivers the precise file list + recommended tags, then the
  agent verifies the result on the Pi once the user confirms retagging is done. This is an
  out-of-band dependency, not a blocker on planning.

- Phase 3 requires a Socket.IO event-shape change (duplicate-detection signal + loose-song list) —
  a three-repo change across backend, `Volumio2-UI`, and `stellar-ios`. Update
  `docs/SOCKET-CONTRACT.md` and ship all three together.

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| *(none — first milestone)* | | | |

## Session Continuity

Last session: 2026-08-11T21:51:45.223Z
Stopped at: Completed 03-01-PLAN.md
`/gsd:plan-phase 1`.
Resume file: None
