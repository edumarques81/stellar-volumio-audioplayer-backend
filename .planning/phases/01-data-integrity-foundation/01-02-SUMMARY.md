---
phase: 01-data-integrity-foundation
plan: 02
subsystem: infra
tags: [go, table-driven-tests, tdd, mpd, cache, socketio, layering]

# Dependency graph
requires:
  - phase: 01-data-integrity-foundation
    plan: 01-01
    provides: "internal/infra/musicfile package (IsResourceFork, CountUntagged)"
provides:
  - "groupAlbumDetails(songs []mpd.Attrs) (albums []AlbumDetails, skipped int) -- pure, unit-testable, resource-fork-hardened grouping logic behind GetAlbumDetails (and transitively GetAlbums, GetArtistAlbums, cache builder's buildAlbums, localmusic's album browsing)"
  - "CacheStats.SkippedCount / CacheStatusResponse.SkippedCount -- DATA-02's skipped/untagged observability signal, persisted via cache_meta, surfaced on pushLibraryCacheStatus"
affects: [01-05, socketio-cache-status, docs/SOCKET-CONTRACT.md]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Extract-then-harden: pull an inline loop out of a public method into a pure unexported function so a live-MPD-free unit test can pin its behavior before adding a new check"
    - "Non-fatal secondary build step (buildSkippedCount) mirroring the existing buildRadioStations pattern in Builder.FullBuild -- log.Warn + continue, never fail the whole rebuild"
    - "cache_meta key/value upsert (setMeta/getMeta) reused for a new counter, matching schema_version/last_full_build/last_updated precedent"

key-files:
  created: []
  modified:
    - internal/infra/mpd/client.go
    - internal/infra/mpd/client_albumdetails_test.go
    - internal/infra/cache/builder.go
    - internal/infra/cache/builder_test.go
    - internal/infra/cache/sqlite.go
    - internal/infra/cache/types.go
    - internal/domain/library/cached_service.go
    - internal/domain/library/cached_service_test.go
    - internal/transport/socketio/cache_handlers.go
    - docs/SOCKET-CONTRACT.md (workspace root, not this repo -- not committed here)

key-decisions:
  - "groupAlbumDetails lives in internal/infra/mpd/client.go (package mpd, unexported), not a shared helper -- it is genuinely mpd-package-specific grouping/aggregation logic; only the resource-fork predicate itself is shared (via musicfile.IsResourceFork), matching the plan's interfaces section"
  - "client_albumdetails_test.go declares `package mpd` (not `mpd_test`) specifically so it can call the unexported groupAlbumDetails directly without a live MPD connection, per the plan's explicit action text"
  - "buildSkippedCount is called from FullBuild immediately after buildAlbums succeeds and is non-fatal on error (log.Warn + continue), matching the existing buildRadioStations precedent at builder.go -- a CountUntagged failure on one basePath must not fail the whole cache rebuild"
  - "mpdDataProviderAdapter.CountUntagged wires through the already-existing library.MPDClient.SearchByBase (no interface change on that side) + musicfile.CountUntagged, per the plan's stated rationale for avoiding a three-interface/three-adapter signature-change blast radius"

patterns-established: []

requirements-completed: []

# Metrics
duration: ~35min
completed: 2026-08-11
---

# Phase 01 Plan 02: MPD client hardening + DATA-02 skipped-count wiring Summary

**Extracted and hardened `groupAlbumDetails` (the grouping logic behind `GetAlbumDetails`, the single choke point for four album-browse paths) against `._` resource-fork junk, and wired a new `CacheStats.SkippedCount` end-to-end from a cache-builder `CountUntagged` scan through `cache_meta` persistence to the `pushLibraryCacheStatus` Socket.IO payload and `docs/SOCKET-CONTRACT.md`.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-08-11T19:59:xx (approx, first RED commit `404f8bb`)
- **Completed:** 2026-08-11T20:10:12Z
- **Tasks:** 3/3 completed
- **Files modified:** 9 in this repo (2 test files created, 7 existing files modified) + 1 doc at the workspace root (not this repo, not committed here)

## Accomplishments

- Extracted `GetAlbumDetails`'s inline grouping loop into a pure, unexported `groupAlbumDetails(songs []mpd.Attrs) (albums []AlbumDetails, skipped int)`, unit-tested with 4 table-driven behavior cases plus a dedicated aggregation-invariance test (TrackCount/TotalTime/Format/Genre first-track-wins) -- all live-MPD-free.
- Added a `musicfile.IsResourceFork` pre-check inside `groupAlbumDetails` that runs **before** the `album == ""` skip, so `._` junk can never inflate the "untagged real songs" counter (T-01-03) -- proven by table-driven case (c).
- `GetAlbumDetails`'s public signature is unchanged; it now logs a debug line (`basePath`, `skipped`) per call, satisfying D-08's log-line requirement for this call site.
- Added `CacheStats.SkippedCount` (json `skippedCount`), persisted via a new `Builder.buildSkippedCount()` that sums a new `MPDDataProvider.CountUntagged(basePath)` across all configured basePaths and writes the total through the existing `setMeta("skipped_count", ...)` upsert pattern; `GetStats()` reads it back via `getMeta`, defaulting to 0 when absent (fresh/never-built cache) rather than erroring.
- Wired `mpdDataProviderAdapter.CountUntagged` (in `internal/domain/library/cached_service.go`) through the already-existing `library.MPDClient.SearchByBase` + `musicfile.CountUntagged` -- zero interface changes needed on the `library.MPDClient` side.
- Surfaced the new signal on the wire: `CacheStatusResponse.SkippedCount` (json `skippedCount`) mapped from `stats.SkippedCount` in `handleGetCacheStatus`, riding the existing `library:cache:status` -> `pushLibraryCacheStatus` event (no new Socket.IO event, per D-08/T-01-04's "accept" disposition).
- Updated `docs/SOCKET-CONTRACT.md`'s `CacheStatus` TypeScript interface with `skippedCount: number;` and an inline comment clarifying that `._` junk is excluded from the count.

## Task Commits

Each task was committed atomically (TDD tasks split across RED/GREEN commits):

1. **Task 1: groupAlbumDetails extraction + hardening (RED)** - `404f8bb` (test) -- failing compile, `groupAlbumDetails` does not exist yet
2. **Task 1: groupAlbumDetails extraction + hardening (GREEN)** - `f8cfa36` (feat) -- extracted, hardened, all tests pass
3. **Task 2: skipped-count persistence + adapter wiring (RED)** - `fdba9fe` (test) -- failing compile, `CacheStats.SkippedCount` / `mpdDataProviderAdapter.CountUntagged` do not exist yet
4. **Task 2: skipped-count persistence + adapter wiring (GREEN)** - `b839d8e` (feat) -- all tests pass
5. **Task 3: Socket.IO wire-contract surfacing** - `82ae672` (feat) -- `CacheStatusResponse.SkippedCount` added; `docs/SOCKET-CONTRACT.md` updated separately (workspace root, not committed via this repo)

**Plan metadata:** (this commit, following SUMMARY.md creation)

## Files Created/Modified

- `internal/infra/mpd/client.go` -- `groupAlbumDetails` extracted from `GetAlbumDetails`'s inline loop; adds `musicfile.IsResourceFork` pre-check + `skipped` counter; `GetAlbumDetails` now calls it and logs a debug line
- `internal/infra/mpd/client_albumdetails_test.go` -- new, `package mpd` (internal, to reach the unexported function), `TestGroupAlbumDetails` (4 table-driven cases a/b/c/d) + `TestGroupAlbumDetails_AggregationUnchanged` (case e)
- `internal/infra/cache/builder.go` -- `MPDDataProvider.CountUntagged(basePath string) (int, error)` added to the interface; new `Builder.buildSkippedCount()` method; called (non-fatal) from `FullBuild()` right after `buildAlbums()`
- `internal/infra/cache/builder_test.go` -- `stubMPDDataProvider` gains `untaggedByBase` + `CountUntagged`; `TestFullBuild_PersistsSkippedCount` and `TestGetStats_SkippedCountDefaultsToZero_WhenNoMetaRow` added
- `internal/infra/cache/sqlite.go` -- `strconv` import added; `GetStats()` reads `skipped_count` from `cache_meta`, defaulting to 0 on absent/unparsable
- `internal/infra/cache/types.go` -- `CacheStats.SkippedCount int` (json `skippedCount`) added after `RadioCount`; struct's gofmt column alignment manually corrected for this block only (see Deviations)
- `internal/domain/library/cached_service.go` -- `musicfile` import added; `mpdDataProviderAdapter.CountUntagged` implemented via `SearchByBase` + `musicfile.CountUntagged`
- `internal/domain/library/cached_service_test.go` -- `fmt` import added; `TestMPDDataProviderAdapter_CountUntagged` (2 subtests: happy path + error propagation)
- `internal/transport/socketio/cache_handlers.go` -- `CacheStatusResponse.SkippedCount` (json `skippedCount`) added after `RadioCount`; mapped in `handleGetCacheStatus`
- `docs/SOCKET-CONTRACT.md` (workspace root, **not this git repo** -- see hard constraint #4) -- `skippedCount: number;` line added to `CacheStatus` interface, after `radioCount`, before `isBuilding`

## Decisions Made

- Confirmed via `<interfaces>` and re-reading the plan that `groupAlbumDetails` is package-`mpd`-local (not moved into `musicfile`) -- only the resource-fork predicate is shared; the album-grouping/aggregation logic is specific to this one call site's return shape (`AlbumDetails`).
- Placed the resource-fork check as the very first thing inside the per-song loop, before both the `album == ""` skip and any key construction, so junk is excluded from grouping *and* never touches the skipped counter (matches T-01-03's mitigation exactly).
- Followed the plan's exact ordering for `FullBuild()`: `buildSkippedCount()` runs right after `buildAlbums()` succeeds (not after `buildArtists`/`buildRadioStations`), and is non-fatal, matching the existing `buildRadioStations` pattern one section below it.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug in own change] `internal/infra/cache/types.go`'s `CacheStats` struct needed manual gofmt realignment after adding `SkippedCount`**
- **Found during:** Task 2 verification (`gofmt -l`)
- **Issue:** Adding `SkippedCount int` widened the struct's longest field-name column, which shifts gofmt's tab-alignment for every other field in the same struct block. Running a full `gofmt -w` on the file would have also reformatted ~4 other, unrelated pre-existing structs in the same file (`CachedAlbum`, `CachedArtwork`, `SortOrder`, `Pagination`) that carry the repo-wide struct-tag drift already documented in `deferred-items.md` from Plan 01-01 -- out of this plan's scope.
- **Fix:** Manually re-aligned only the `CacheStats` struct block's columns to match gofmt's expected output (verified via `gofmt -d` showing zero diff scoped to that struct afterward), leaving every other struct in the file exactly as it was (still gofmt-dirty, still documented in `deferred-items.md`).
- **Files modified:** `internal/infra/cache/types.go` (the `CacheStats` struct only)
- **Verification:** `gofmt -d internal/infra/cache/types.go | grep -A 15 "CacheStats struct"` -- empty output (clean); the remaining diff for the file is entirely the pre-existing unrelated drift.
- **Committed in:** `b839d8e`

**2. [Scope boundary - logged, not fixed] `internal/infra/cache/sqlite.go`'s `DB` struct and `internal/transport/socketio/cache_handlers.go`'s `CacheUpdatedEvent` struct carry pre-existing, unrelated gofmt drift**
- **Found during:** Task 2 and Task 3 verification (`gofmt -l`)
- **Issue:** `sqlite.go`'s `DB` struct (`isBuilding`/`buildProgress` column alignment) and `cache_handlers.go`'s `CacheUpdatedEvent` struct literal (`Timestamp`/`AlbumCount`/`TrackCount` alignment) both show as gofmt-dirty. Neither struct/literal was touched by this plan -- confirmed via `gofmt -d` showing the diff hunks do not overlap the lines I added or the `CacheStatusResponse`/`CacheStats` structs this plan modified.
- **Fix:** Not fixed -- out of scope, same category as the repo-wide drift Plan 01-01 already logged in `deferred-items.md`. Left untouched.
- **Files modified:** None
- **Verification:** `gofmt -d internal/infra/cache/sqlite.go` and `gofmt -d internal/transport/socketio/cache_handlers.go` both show only pre-existing, unrelated hunks; `gofmt -d ... | grep -i "SkippedCount"` returns empty for both files, confirming my additions are clean.
- **Committed in:** N/A (not fixed)

**Total deviations:** 2, both Rule 1 (self-caused, self-fixed) / scope-boundary (pre-existing, logged). No behavior change to anything outside this plan's stated scope.

## Issues Encountered

None beyond the deviations documented above. No auth gates, no architectural questions, no blockers.

## User Setup Required

None. No external service configuration, no Pi deployment (explicitly out of scope for this plan -- deferred to 01-05), no audio-chain changes.

## Verification Evidence (actual command output)

```
$ go test ./internal/infra/mpd/... -run TestGroupAlbumDetails -v
--- PASS: TestGroupAlbumDetails (0.00s)
    --- PASS: TestGroupAlbumDetails/case_a:... (0.00s)
    --- PASS: TestGroupAlbumDetails/case_b:... (0.00s)
    --- PASS: TestGroupAlbumDetails/case_d:... (0.00s)
    --- PASS: TestGroupAlbumDetails/case_c:... (0.00s)
--- PASS: TestGroupAlbumDetails_AggregationUnchanged (0.00s)
PASS
ok  	.../internal/infra/mpd	0.170s

$ go test ./internal/infra/cache/... -run "TestFullBuild_PersistsSkippedCount|TestGetStats_SkippedCountDefaultsToZero" -v
{"level":"debug","skipped":16,...,"message":"Skipped/untagged count cached"}
--- PASS: TestGetStats_SkippedCountDefaultsToZero_WhenNoMetaRow (0.01s)
--- PASS: TestFullBuild_PersistsSkippedCount (0.02s)
PASS
ok  	.../internal/infra/cache	0.195s

$ go test ./internal/domain/library/... -run TestMPDDataProviderAdapter_CountUntagged -v
--- PASS: TestMPDDataProviderAdapter_CountUntagged (0.00s)
    --- PASS: .../counts_real_untagged_songs,_excludes_resource-fork_junk (0.00s)
    --- PASS: .../propagates_SearchByBase_error (0.00s)
PASS

$ go build ./...
(no output -- success)

$ go vet ./...
(no output -- success)

$ go test ./... 2>&1 | grep -v "^ok\|no test files"
(no output -- every package passes)

$ grep -n "skippedCount" docs/SOCKET-CONTRACT.md internal/transport/socketio/cache_handlers.go
docs/SOCKET-CONTRACT.md:89:  skippedCount: number;     // real songs missing an Album tag; ...
internal/transport/socketio/cache_handlers.go:45:	SkippedCount   int    `json:"skippedCount"` // ...

$ gofmt -l internal/infra/mpd/client.go internal/infra/mpd/client_albumdetails_test.go \
    internal/infra/cache/builder.go internal/infra/cache/builder_test.go \
    internal/domain/library/cached_service.go internal/domain/library/cached_service_test.go
(no output -- clean; sqlite.go/types.go/cache_handlers.go flagged only for pre-existing
 unrelated struct drift, confirmed above and in Deviations)
```

`make check`'s `go fmt ./...` step was deliberately NOT run repo-wide (same reasoning as Plan 01-01's documented deviation: it would reformat ~30 unrelated pre-existing files). `go vet` and `golangci-lint run` were instead scoped directly to the touched packages:

```
$ golangci-lint run ./internal/infra/mpd/... ./internal/infra/cache/... \
    ./internal/domain/library/... ./internal/transport/socketio/...
38 issues (36 errcheck, 1 staticcheck, 1 unused) -- all in lines this plan never
touched (confirmed by cross-referencing every reported line number against `git diff`
for this plan's commits). None reference groupAlbumDetails, buildSkippedCount,
CountUntagged, SkippedCount, or CacheStatusResponse.
```

## Next Phase Readiness

- `GetAlbumDetails` is now hardened against `._` junk at the one call site D-10 named as the minimum bar, closing this plan's share of `DATA-04`. `DATA-04` is NOT marked complete in `REQUIREMENTS.md` -- Plan 01-05 (cache builder paths, per the objective's own scope note) is still pending and shares the requirement ID; see the deliverable note in this plan's execution context.
- `DATA-02` is NOT marked complete either -- the mechanism now exists end-to-end (MPD scan -> cache persistence -> Socket.IO wire), but D-09 defines success as "the count reading 0 after the DATA-01 retag," and DATA-01's retag is still blocked on the user (Plan 01-07). The live count today is 16 (10 Karajan + 4 toe + 2 Singxer), matching the documented baseline in this plan's prior-work context -- once the user retags and 01-05 lands, a live Pi check via `pushLibraryCacheStatus`/`curl` should show `skippedCount: 0`.
- `groupAlbumDetails` and `CountUntagged`/`SkippedCount` are both unit-tested without any live Pi dependency; the next plan that touches the cache builder or MPD client paths can build on this pattern (pure-function extraction + table-driven tests) without needing a live MPD server.
- `docs/SOCKET-CONTRACT.md` is updated at the workspace root; any downstream frontend/iOS work reading `pushLibraryCacheStatus` can now type `skippedCount: number` per the updated `CacheStatus` interface.

---
*Phase: 01-data-integrity-foundation*
*Completed: 2026-08-11*

## Self-Check: PASSED

- All 9 files claimed as created/modified in this repo confirmed present via `git show
  <commit> --stat` for their respective commits (`404f8bb`, `f8cfa36`, `fdba9fe`, `b839d8e`, `82ae672`).
- All 5 commit hashes confirmed present in `git log --oneline` at time of writing.
- `docs/SOCKET-CONTRACT.md`'s edit confirmed via `grep -n "skippedCount" docs/SOCKET-CONTRACT.md`
  returning the added line (workspace root, correctly not committed from this repo per
  hard constraint #4).
