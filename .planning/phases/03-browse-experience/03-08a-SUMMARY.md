---
phase: 03-browse-experience
plan: 08a
subsystem: ui
tags: [swiftui, swift6, observation, socketio, xctest, ios]

# Dependency graph
requires:
  - phase: 03-browse-experience (03-06)
    provides: >
      Deployed, contract-locked backend (schema v6) emitting Album.badge,
      Album.discCount, Track.disc, and ArtistAlbumsResponse.looseTracks —
      all additive fields, documented in docs/SOCKET-CONTRACT.md.
provides:
  - stellar-ios LibraryModels.swift Album.badge/discCount, Track.disc,
    PushLibraryArtistAlbums.looseTracks (Codable AND rawDict parsing paths)
  - ArtistPickerStore.artistLooseTracks + applyArtistAlbumsPayload(_:) (testable
    extraction of the pushLibraryArtistAlbums handler)
  - Shared AlbumDuplicateBadge chip rendered on both AlbumPickerView's and
    ArtistDetailView's AlbumTile
  - AlbumTracksView.TrackList disc-grouped "Disc N" headers (discCount>1),
    TrackList/TrackRow made non-private for cross-view reuse
  - ArtistDetailView loose-track fallback for a zero-album artist drill-in
affects: [03-08b]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Optional backend-pushed fields degrade to nil/absent, never a crash or empty pill — Track.disc is the one exception (defaults to 0, matching the existing trackNumber/duration zero-default convention already in this file, documented inline)."
    - "Socket-event handler logic extracted into a small testable method on the store (ArtistPickerStore.applyArtistAlbumsPayload) rather than left inline in the bind() closure — mirrors AlbumPickerStore.handleLibraryCacheUpdated's existing testability convention, since this project's swift test can't exercise SwiftUI views (@Observable + UIKit-backed) or fire live socket events in unit tests."
    - "Pure view-adjacent algorithms (disc-grouping) extracted as static functions on the View struct so they're unit-testable without rendering — TrackList.groupTracksByDisc(_:), mirroring AlbumPickerStore.computeFingerprint's precedent."

key-files:
  created:
    - StellarVolumiOTests/AlbumTracksViewGroupingTests.swift
  modified:
    - StellarVolumiO/Models/LibraryModels.swift
    - StellarVolumiO/Stores/ArtistPickerStore.swift
    - StellarVolumiO/Views/Library/AlbumPickerView.swift
    - StellarVolumiO/Views/Library/ArtistDetailView.swift
    - StellarVolumiO/Views/Library/AlbumTracksView.swift
    - StellarVolumiOTests/Fixtures/Fixtures.swift
    - StellarVolumiOTests/LibraryEnvelopeParserTests.swift
    - StellarVolumiOTests/LibraryAutoRefreshTests.swift

key-decisions:
  - "Track.disc defaults to 0 via an explicit designated initializer default parameter (disc: Int = 0), not a required parameter — keeps the two pre-existing Track(id:...) call sites in LibraryAutoRefreshTests.swift and AlbumTracksStoreTests.swift compiling unchanged, avoiding an unrelated test-file edit outside this plan's scope."
  - "ArtistPickerStore's pushLibraryArtistAlbums handler logic was extracted into applyArtistAlbumsPayload(_:) specifically to make it unit-testable — the bind() closure itself can't be triggered from a unit test (no live socket connection), so without this extraction the payload-to-state mapping (including the guard that looseTracks only surfaces when albums is empty) would have had zero test coverage."
  - "AlbumDuplicateBadge factored into ONE new shared, non-private struct (declared in AlbumPickerView.swift, reused by ArtistDetailView.swift) rather than duplicated inline in both files' AlbumTile — the plan explicitly permitted leaving the two AlbumTile structs duplicated, but the badge chip itself is small enough that keeping ONE definition avoids the two grids' badges silently drifting in style over time. The two AlbumTile structs themselves remain deliberately duplicated per the plan."
  - "TrackList.discGroups only takes the grouped rendering branch when BOTH discCount>1 AND the actual grouping produces >1 distinct group — guards against a discCount/track.disc data mismatch (e.g. discCount says 2 but every track's disc value collapsed to the same number) producing a single spurious 'Disc N' header instead of falling back cleanly to the flat list."
  - "ArtistDetailView adds a ProgressView branch (store.loadingArtistAlbums, both artistAlbums and artistLooseTracks still empty) ahead of the plan's literal 'both empty -> No albums' branch — avoids a one-frame 'No albums' flash while the initial library:artist:albums fetch is still in flight. This is additive UI-correctness, not a plan requirements change; the plan's three named branches (grid / loose-tracks / true-empty) are all still exactly as specified once loading settles."

patterns-established:
  - "Every new optional backend field on LibraryAlbum/Track/PushLibraryArtistAlbums is threaded through BOTH the Codable path (CodingKeys/init(from:)/encode(to:)) AND the tolerant init?(rawDict:) extension, with matching present/absent test cases in LibraryEnvelopeParserTests.swift, per this file's existing dual-path convention."

requirements-completed: [ARTIST-04, BROWSE-02, BROWSE-03, BROWSE-04, BROWSE-07]

# Metrics
duration: 25min
completed: 2026-08-12
---

# Phase 3 Plan 08a: iOS Rendering for Duplicate Badge, Disc Grouping, Loose Tracks Summary

**stellar-ios now renders `Album.badge` as a gold outline chip on both the Albums grid and Artist→Albums grid tiles, groups `AlbumTracksView`'s track list under "Disc N" headers for `discCount>1` box sets, and shows a real tap-to-play loose-track list on `ArtistDetailView` when an artist drill-in resolves to zero albums — all three degrade to their pre-Phase-3 rendering when the new fields are absent, mirroring 03-07a's LCD implementation on the iOS side of the same locked contract.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-08-12T16:25:00Z (approx, first file read)
- **Completed:** 2026-08-12T16:34:15Z
- **Tasks:** 3/3
- **Files modified:** 8 (5 source files + 3 test files, 1 new test file)

## Accomplishments
- `LibraryModels.swift` gained `LibraryAlbum.badge`/`discCount`, `Track.disc`, and `PushLibraryArtistAlbums.looseTracks` across both their Codable and tolerant `rawDict` parsing paths — a mismatch between the two paths is a compile error in this codebase, not a silent runtime bug, and `scripts/build.sh` confirms they stayed in sync.
- `ArtistPickerStore` exposes `artistLooseTracks: [Track]`, populated only when the backend's `albums` array is empty (mirroring the backend's own `looseTracks` invariant defensively, in case of a stale/malformed payload), and reset in both `select(_:)` and `clearSelection()`.
- A new shared `AlbumDuplicateBadge` chip renders `album.badge` on both `AlbumPickerView`'s and `ArtistDetailView`'s independently-duplicated `AlbumTile` structs — absent/empty badge renders nothing, no layout shift, verified for both the 5 badged and 61 unbadged albums in the live library's shape via fixture tests.
- `AlbumTracksView.TrackList` groups pre-sorted tracks into "Disc N" sections when `discCount>1` and the tracks' actual `disc` values produce more than one group (an 11-disc Mahler box set is the real shape tested); single-disc albums render byte-identical to the pre-Phase-3 flat list.
- `ArtistDetailView` now has four distinct render states driven by `store.artistAlbums`/`artistLooseTracks`/`loadingArtistAlbums`: the normal grid (unaffected), a reused `TrackList`-based loose-track fallback with real tap-to-play, a loading spinner, and a "No albums" message for the true (not expected live) zero-content case — replacing the previous silent blank screen.

## Task Commits

Each task was committed atomically:

1. **Task 1: Extend LibraryModels.swift + wire ArtistPickerStore** - `b52b650` (feat)
2. **Task 2: Badge overlay on both AlbumTile definitions + disc-grouped headers** - `86e6b9c` (feat)
3. **Task 3: ArtistDetailView loose-track fallback** - `503c14e` (feat)

_No plan-metadata commit was made in the stellar-ios repo per the execution contract — this SUMMARY and STATE/ROADMAP updates live in the planning repo (stellar-volumio-audioplayer-backend)._

## Files Created/Modified
- `StellarVolumiO/Models/LibraryModels.swift` - `LibraryAlbum.badge`/`discCount`, `Track.disc` (default 0), `PushLibraryArtistAlbums.looseTracks`, across Codable + rawDict paths
- `StellarVolumiO/Stores/ArtistPickerStore.swift` - `artistLooseTracks` property, `applyArtistAlbumsPayload(_:)` extraction, reset in `select()`/`clearSelection()`
- `StellarVolumiO/Views/Library/AlbumPickerView.swift` - badge overlay on `AlbumTile` + new shared `AlbumDuplicateBadge` struct
- `StellarVolumiO/Views/Library/ArtistDetailView.swift` - badge overlay on its own `AlbumTile`; loose-track/loading/empty fallback states; `playLooseTrack(_:)`
- `StellarVolumiO/Views/Library/AlbumTracksView.swift` - `TrackList`/`TrackRow`/`DiscHeader` made non-private; disc-grouping via `TrackList.groupTracksByDisc(_:)`
- `StellarVolumiOTests/Fixtures/Fixtures.swift` - badge/discCount/looseTracks raw-dict fixtures
- `StellarVolumiOTests/LibraryEnvelopeParserTests.swift` - 6 new parser tests (badge/discCount/disc/looseTracks, present + absent)
- `StellarVolumiOTests/LibraryAutoRefreshTests.swift` - 4 new `ArtistPickerStore.applyArtistAlbumsPayload`/`select`/`clearSelection` tests
- `StellarVolumiOTests/AlbumTracksViewGroupingTests.swift` (new) - 5 tests for `TrackList.groupTracksByDisc(_:)`, incl. the real 11-disc box-set shape

## Decisions Made
See `key-decisions` in frontmatter (Track.disc default-parameter back-compat, applyArtistAlbumsPayload testability extraction, single shared AlbumDuplicateBadge vs. fully duplicated, disc-grouping mismatch guard, loading-state addition ahead of the "No albums" branch).

## Deviations from Plan

### Auto-fixed Issues

None. No bugs, missing critical functionality, or blocking issues were encountered — the plan's `<interfaces>` orientation notes (dual Codable/rawDict paths, both AlbumTile duplicates, TrackList/TrackRow non-private for reuse) matched the real files exactly.

### Additive, in-scope choices (not deviations from a written behavior, but worth flagging)

- Added a `ProgressView` branch to `ArtistDetailView` for the `loadingArtistAlbums && both-empty` state, ahead of the plan's literal "both empty → No albums" instruction. This does not change any of the plan's three named `must_haves` states — it only prevents a one-frame flash of "No albums" while `library:artist:albums` is still in flight, which is UI-correctness within Rule 1's scope (avoiding a factually-wrong transient message), not new functionality. Documented rather than silently added since it technically extends the state machine.
- Extracted `TrackList.groupTracksByDisc(_:)` as a `static` function (rather than a private computed property) specifically so a new `AlbumTracksViewGroupingTests.swift` could unit-test the grouping algorithm directly — the plan's acceptance criteria explicitly allowed "a snapshot or state-inspection test if the project has UI test infra... otherwise via a build-and-manual-reasoning check," but a real unit test for the pure algorithm was achievable without SwiftUI rendering, so it was added instead of relying solely on reasoning.

**Total deviations:** 0 auto-fixed. 2 additive choices, both documented above, neither expanding scope beyond the plan's stated behavior/acceptance criteria.
**Impact on plan:** None — plan executed as written; the two additive choices are defensive/testability improvements within the same task's boundaries.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Known Stubs
None. `badge`, `discCount`, `disc`, and `looseTracks` are all wired end-to-end from `LibraryModels.swift` parsing through the stores to real SwiftUI rendering (`AlbumDuplicateBadge`, `DiscHeader`, the loose-track `TrackList`) — no placeholder text, hardcoded empty values, or unwired components were introduced.

## Threat Flags
None. The only new rendering surface is `album.badge`/`Track`/`looseTracks` text via SwiftUI `Text(...)` (no raw HTML/webview rendering anywhere in this app), matching the plan's threat-model disposition (T-03-12, mitigate via the existing `as? String`/`as? Int` defensive-default rawDict convention — pinned by the new absent-field test cases). No new network calls, auth paths, or schema changes.

## Verification Evidence

**Per-task (Task 1):**
```
$ export IOS_SIM_ID=68601B82-18A9-48EA-BEEC-0E5C74C29E52
$ xcodebuild test -scheme StellarVolumiO -destination "platform=iOS Simulator,id=$IOS_SIM_ID" \
    -only-testing:StellarVolumiOTests/LibraryEnvelopeParserTests \
    -only-testing:StellarVolumiOTests/LibraryAutoRefreshTests
...
Test Suite 'LibraryAutoRefreshTests' passed at 2026-08-12 16:28:25.268.
Test Suite 'LibraryEnvelopeParserTests' passed at 2026-08-12 16:28:25.292.
Test Suite 'Selected tests' passed at 2026-08-12 16:28:25.293.
```
Full-suite gate also run after Task 1 (`scripts/test.sh`, exit 0).

**Per-task (Task 2):**
```
$ xcodegen generate --spec project.yml
Created project at .../StellarVolumiO.xcodeproj
$ scripts/build.sh
... BUILD EXIT:0
$ xcodebuild test ... -only-testing:StellarVolumiOTests/AlbumTracksViewGroupingTests
Test Suite 'AlbumTracksViewGroupingTests' passed at 2026-08-12 16:31:02.825.
(5/5 tests passed, incl. testElevenDiscBoxSetGroupsCorrectly)
```
Full-suite gate also run after Task 2 (`scripts/build.sh` + full `xcodebuild test`, both exit 0).

**Per-task (Task 3):**
```
$ scripts/build.sh
... BUILD EXIT:0
$ xcodebuild test -scheme StellarVolumiO -destination "platform=iOS Simulator,id=$IOS_SIM_ID" -quiet
... TEST EXIT:0
```

**Final full-suite gate (post all 3 tasks):**
```
$ export IOS_SIM_ID=68601B82-18A9-48EA-BEEC-0E5C74C29E52
$ scripts/build.sh   # BUILD:0
$ scripts/test.sh    # TEST:0
$ xcodebuild test -scheme StellarVolumiO -destination "platform=iOS Simulator,id=$IOS_SIM_ID"
...
Test Suite 'StellarVolumiOTests.xctest' passed at 2026-08-12 16:34:01.554.
	 Executed 154 tests, with 0 failures (0 unexpected) in 12.973 (13.059) seconds
Test Suite 'All tests' passed at 2026-08-12 16:34:01.555.
	 Executed 154 tests, with 0 failures (0 unexpected) in 12.973 (13.059) seconds
```
154/154 tests passing, zero regressions to any pre-existing suite (`BackendDiscoveryServiceTests`,
`ConnectionGraceTests`, `LastPlayedAlbumTests`, `LastPlayedStoreTests`, `LcdStoreTests`,
`NowPlayingDisplayStateTests`, `PlayerStateParserTests`, `PlayerStoreOptimisticTests`, `SmokeTest`,
`SocketDecodeErrorSurfaceTests`, `TapDebouncerTests`, `AlbumTracksStoreTests`, all present and green).

## Next Phase Readiness
- 03-08b (live simulator visual verification of this plan's rendering against the deployed Pi
  backend) is unblocked — all four `must_haves.truths` from this plan's frontmatter (duplicate
  badge visible/hidden correctly on both grids, "Disc N" headers on a multi-disc drill-in, real
  tappable/playable loose-track list on a zero-album artist) are implemented and store/unit-tested
  against the locked contract, but **not yet visually confirmed on the simulator against live data**
  — that is explicitly 03-08b's job per this plan's `<success_criteria>`.
- No blockers. `stellar-ios` was left uncommitted-to-remote by design (this plan does not push);
  the orchestrator handles push per the workspace's "commit and push at every phase boundary" rule.
- Working tree is clean apart from the three task commits; `git status --short` shows nothing
  unstaged or untracked at completion.

---
*Phase: 03-browse-experience*
*Completed: 2026-08-12*

## Self-Check: PASSED

All 8 modified files + 1 created file verified present on disk in the stellar-ios repo (branch `main`).
All 3 task commits (`b52b650`, `86e6b9c`, `503c14e`) verified present in `git log`.
