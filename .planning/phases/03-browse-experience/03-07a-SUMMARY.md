---
phase: 03-browse-experience
plan: 07a
subsystem: ui
tags: [svelte5, typescript, socket.io, lcd-kiosk, vitest]

# Dependency graph
requires:
  - phase: 03-browse-experience (03-06)
    provides: >
      Deployed, contract-locked backend (schema v6) emitting Album.badge,
      Album.discCount, Track.disc, and ArtistAlbumsResponse.looseTracks —
      all additive fields, documented in docs/SOCKET-CONTRACT.md.
provides:
  - Volumio2-UI/src/lib/stores/library.ts types + artistLooseTracks store +
    libraryActions.playLooseTracks(tracks) action
  - AlbumPage.svelte duplicate-badge rendering (data-testid=album-duplicate-badge)
  - AlbumTrackList.svelte disc-grouped "Disc N" headers for discCount>1 albums
  - LibraryView.svelte loose-track fallback view for a zero-album artist drill-in
affects: [03-07b, 03-08a]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Optional backend-pushed fields degrade to 'render nothing' — no undefined text, no empty pills, no layout shift (verified per-field with explicit absent/empty test cases)."
    - "New fallback UI (loose-tracks-view) built inline in the parent component rather than extracted, since it has exactly one call site and reuses AlbumTrackList directly."

key-files:
  created: []
  modified:
    - Volumio2-UI/src/lib/stores/library.ts
    - Volumio2-UI/src/lib/stores/__tests__/library.test.ts
    - Volumio2-UI/src/lib/components/redesign/AlbumPage.svelte
    - Volumio2-UI/src/lib/components/redesign/__tests__/AlbumPage.test.ts
    - Volumio2-UI/src/lib/components/redesign/AlbumTrackList.svelte
    - Volumio2-UI/src/lib/components/redesign/__tests__/AlbumTrackList.test.ts
    - Volumio2-UI/src/lib/components/redesign/LibraryView.svelte
    - Volumio2-UI/src/lib/components/redesign/__tests__/LibraryView.test.ts
    - Volumio2-UI/src/lib/components/redesign/__tests__/PlayerLayout.test.ts

key-decisions:
  - "Duplicate badge uses an outlined pill (border + text in --color-accent, transparent fill) rather than the filled-gold capsule style used by HiResBadge/AirplaySourceBadge — it needs to render arbitrary-length text (e.g. '352.8kHz/24bit FLAC') as supplementary info, not a headline, per the plan's LCD-density constraint."
  - "Disc-group header rendered as a plain <li class=\"disc-header\"> inside the existing <ol> rather than a separate <h2>/<div> structure — keeps the flat vs. grouped branches structurally parallel (same <ol>/<li> DOM shape) and simplest to keep the discCount<=1 branch byte-identical to the pre-Phase-3 markup."
  - "LibraryView's loose-track fallback (LooseTracksView) is built inline rather than extracted to its own .svelte file, per the plan's 'your call' discretion — it has exactly one call site, reuses AlbumTrackList directly, and extraction would only add an import+test-mock indirection with no reuse benefit."
  - "AlbumTrackList's tracks prop passed the real Track[] from artistLooseTracks directly (not remapped to a narrower shape) — Track already structurally satisfies AlbumTrackList's TrackRow type (uri/title/duration required, disc/trackNumber optional), so no runtime remapping was needed; tsc confirms the structural compatibility."

patterns-established:
  - "Every new optional backend field gets three test cases: value-present (renders), value-absent/undefined (renders nothing), and — for badge specifically — value-empty-string (also renders nothing), since the D-08/D-09 defensive-empty-state pattern from 03-CONTEXT.md treats '' the same as absent."

requirements-completed: [ARTIST-04, BROWSE-01, BROWSE-03, BROWSE-04, BROWSE-07]

# Metrics
duration: 20min
completed: 2026-08-12
---

# Phase 3 Plan 07a: LCD Rendering for Duplicate Badge, Disc Grouping, Loose Tracks Summary

**Volumio2-UI (LCD) now renders `Album.badge` as an outlined pill on AlbumPage, groups `AlbumTrackList` rows under "Disc N" headers for `discCount>1` albums (numbered by each track's own `trackNumber`, not the flat array index), and shows a playable "Play All" loose-track list on `LibraryView` when an artist drill-in resolves to zero albums — all three features degrade to their pre-Phase-3 rendering when the new fields are absent.**

## Performance

- **Duration:** ~20 min (setup/reading ~15 min, three task commits over ~5 min)
- **Started:** 2026-08-12T05:40:00Z (approx, first file read)
- **Completed:** 2026-08-12T05:55:35Z
- **Tasks:** 3/3
- **Files modified:** 9 (3 source components/stores + 1 new test each, plus one downstream test-mock fix)

## Accomplishments
- `library.ts` gained `Album.badge`/`discCount`, `Track.disc`, `ArtistAlbumsResponse.looseTracks`, a new `artistLooseTracks` writable populated by `pushLibraryArtistAlbums` and cleared by `clearArtistFilter`, and a `libraryActions.playLooseTracks(tracks)` action mirroring the existing `clearQueue`/`addToQueue`/`play`/`goToPlayer` shape.
- `AlbumPage.svelte` renders `album.badge` as an outlined gold pill (`data-testid="album-duplicate-badge"`) next to the meta-strip, absent entirely when `badge` is undefined or `''`.
- `AlbumTrackList.svelte` accepts a `discCount` prop; when `>1` it groups consecutive same-disc tracks under `data-testid="disc-header"` rows ("Disc N"), numbering each group from its own `trackNumber`; when `<=1` (the default, and 61/66 of the live library) it renders byte-identical to the pre-Phase-3 flat list.
- `LibraryView.svelte` shows a new inline loose-track fallback (`data-testid="library-loose-tracks"`) — artist name, `AlbumTrackList` fed `$artistLooseTracks`, and a "Play All" button — scoped strictly to `$selectedArtist !== null`; the whole-library empty state is untouched even with stale loose-track data.

## Task Commits

Each task was committed atomically:

1. **Task 1: Extend library.ts types + store for badge/discCount/disc/looseTracks** - `87d962ac` (feat)
2. **Task 2: Render the badge on AlbumPage + disc-grouped headers in AlbumTrackList** - `efeb2eb4` (feat)
3. **Task 3: LibraryView loose-track fallback when a drilled-in artist resolves to zero albums** - `a6ee31d3` (feat, includes the PlayerLayout.test.ts mock fix)

_No plan-metadata commit was made in the Volumio2-UI repo per the execution contract — this SUMMARY and STATE/ROADMAP updates live in the planning repo._

## Files Created/Modified
- `Volumio2-UI/src/lib/stores/library.ts` - Album/Track/ArtistAlbumsResponse type extensions, `artistLooseTracks` store, `playLooseTracks` action
- `Volumio2-UI/src/lib/stores/__tests__/library.test.ts` - handler + action tests for the above
- `Volumio2-UI/src/lib/components/redesign/AlbumPage.svelte` - duplicate-badge pill, `discCount` pass-through to AlbumTrackList
- `Volumio2-UI/src/lib/components/redesign/__tests__/AlbumPage.test.ts` - badge present/absent/empty + discCount pass-through tests
- `Volumio2-UI/src/lib/components/redesign/AlbumTrackList.svelte` - disc-grouping logic + "Disc N" headers
- `Volumio2-UI/src/lib/components/redesign/__tests__/AlbumTrackList.test.ts` - flat-regression + multi-disc grouping tests
- `Volumio2-UI/src/lib/components/redesign/LibraryView.svelte` - loose-track fallback view + Play All wiring
- `Volumio2-UI/src/lib/components/redesign/__tests__/LibraryView.test.ts` - fallback-shown / fallback-not-shown / whole-library-untouched / normal-carousel-untouched tests
- `Volumio2-UI/src/lib/components/redesign/__tests__/PlayerLayout.test.ts` - added `artistLooseTracks`/`playLooseTracks` to its `$lib/stores/library` mock (deviation, see below)

## Decisions Made
See `key-decisions` in frontmatter (badge visual treatment, disc-header DOM shape, inline vs. extracted loose-tracks view, no remapping of the Track shape passed into AlbumTrackList).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] PlayerLayout.test.ts mock missing new library-store exports**
- **Found during:** Task 3 (LibraryView loose-track fallback), full-suite verification run
- **Issue:** `PlayerLayout.test.ts` mocks `$lib/stores/library` with an explicit export list (not `importOriginal`). `LibraryView.svelte`'s new `import { artistLooseTracks } from '$lib/stores/library'` broke that mock — Vitest threw `No "artistLooseTracks" export is defined on the mock`, failing `PlayerLayout > renders LibraryView when currentView is library`.
- **Fix:** Added `artistLooseTracks: writable<any[]>([])` to the test file's `libraryMocks` hoisted block and its `vi.mock('$lib/stores/library', ...)` export list, plus `playLooseTracks: vi.fn()` to the mocked `libraryActions` for consistency with the rest of the mock's action surface.
- **Files modified:** `Volumio2-UI/src/lib/components/redesign/__tests__/PlayerLayout.test.ts`
- **Verification:** Full `npm run test:run` — 985 passed / 1 skipped (pre-existing skip, unrelated) / 0 failed.
- **Committed in:** `a6ee31d3` (part of Task 3 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Necessary to keep the full suite green per the plan's Gate 7 requirement (`npm run test:run` full suite, zero regressions). No scope creep — confined to a test-only mock update in a file this plan's Task 3 change directly broke.

## Issues Encountered
None beyond the deviation above.

## User Setup Required
None - no external service configuration required.

## Known Stubs
None. All four fields (`badge`, `discCount`, `disc`, `looseTracks`) are wired end-to-end from the store to real DOM output; no placeholder text, hardcoded empty values, or unwired components were introduced.

## Threat Flags
None. The only new rendering surface is `album.badge` text via Svelte's default auto-escaped interpolation (`{album.badge}`), matching the plan's threat-model disposition (T-03-10, accept — no `{@html}` introduced). No new network calls, auth paths, or schema changes.

## Verification Evidence

**Per-task (Task 1):**
```
$ npm run test:run src/lib/stores/__tests__/library.test.ts
 ✓ src/lib/stores/__tests__/library.test.ts (34 tests) 9ms
 Test Files  1 passed (1)
      Tests  34 passed (34)
$ npx tsc --noEmit
(no output — clean)
```

**Per-task (Task 2):**
```
$ npm run test:run src/lib/components/redesign/__tests__/AlbumPage.test.ts src/lib/components/redesign/__tests__/AlbumTrackList.test.ts
 ✓ src/lib/components/redesign/__tests__/AlbumTrackList.test.ts (13 tests) 97ms
 ✓ src/lib/components/redesign/__tests__/AlbumPage.test.ts (20 tests) 100ms
 Test Files  2 passed (2)
      Tests  33 passed (33)
$ npx tsc --noEmit
(no output — clean)
```

**Per-task (Task 3):**
```
$ npm run test:run src/lib/components/redesign/__tests__/LibraryView.test.ts
 ✓ src/lib/components/redesign/__tests__/LibraryView.test.ts (34 tests) 221ms
 Test Files  1 passed (1)
      Tests  34 passed (34)
$ npx tsc --noEmit
(no output — clean)
```

**Final full-suite gate (post all 3 tasks, includes the PlayerLayout.test.ts fix):**
```
$ npm run test:run
 Test Files  66 passed (66)
      Tests  985 passed | 1 skipped (986)
$ npx tsc --noEmit
(no output — clean, exit 0)
```

## Next Phase Readiness
- 03-07b (backend deploy + live LCD visual verification of this plan's rendering) is unblocked — all three `must_haves.truths` from this plan's frontmatter (badge visible on a real duplicate, no badge on a unique album, Disc N headers on a real multi-disc box set, playable loose-track list on a zero-album artist) are implemented and unit/component-tested against the locked contract, but **not yet visually confirmed on the physical LCD** — that is explicitly 03-07b's job per this plan's `<success_criteria>`.
- 03-08a (stellar-ios rendering of the same four fields) can proceed independently; nothing in this plan touched the iOS repo.
- No blockers. `Volumio2-UI` was left uncommitted-to-remote by design (this plan does not push); the orchestrator handles push per the workspace's "commit and push at every phase boundary" rule.

---
*Phase: 03-browse-experience*
*Completed: 2026-08-12*

## Self-Check: PASSED

All 9 created/modified files verified present on disk in the Volumio2-UI repo (branch `master`).
All 3 task commits (`87d962ac`, `efeb2eb4`, `a6ee31d3`) verified present in `git log`.
