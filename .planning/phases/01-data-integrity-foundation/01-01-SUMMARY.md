---
phase: 01-data-integrity-foundation
plan: 01
subsystem: infra
tags: [go, table-driven-tests, tdd, layering]

# Dependency graph
requires: []
provides:
  - "internal/infra/musicfile package exporting IsResourceFork(filePath string) bool and CountUntagged(songs []map[string]string) int"
  - "GetAlbumTracks refactored to call musicfile.IsResourceFork instead of an inline strings.HasPrefix check"
affects: [01-02-mpd-client-hardening, 01-03-get-album-details, 01-04-cache-builder, socketio-cache-status]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "internal/infra/{leaf-package} for cross-cutting predicates needed by both internal/infra/mpd and internal/domain/library, avoiding an infra→domain import inversion"

key-files:
  created:
    - internal/infra/musicfile/musicfile.go
    - internal/infra/musicfile/musicfile_test.go
  modified:
    - internal/domain/library/service.go
    - internal/domain/library/service_test.go

key-decisions:
  - "Helper package placed at internal/infra/musicfile (leaf package, zero internal deps), not internal/domain/library/filter.go as 01-PATTERNS.md recommended — internal/infra/mpd (Plan 01-02) needs this predicate and internal/infra never imports internal/domain elsewhere in this codebase"
  - "player.isAppleDouble left as-is, not refactored to call musicfile.IsResourceFork — out of this phase's scope per the plan's interfaces section; musicfile is the canonical implementation going forward for new call sites"
  - "Pinning-test pattern for Task 2: wrote TestService_GetAlbumTracks_ExcludesResourceForkFiles against the CURRENT inline implementation first (asserted to already pass), then refactored — catches a regression immediately rather than a conventional RED-must-fail gate, matching the plan's explicit <action> instructions"

patterns-established:
  - "Table-driven Go tests: struct{name, input..., want...} + t.Parallel() + tc := tc + t.Run(tc.name, ...)"

requirements-completed: [DATA-04]

# Metrics
duration: ~10min
completed: 2026-08-11
---

# Phase 01 Plan 01: Shared resource-fork predicate package Summary

**New `internal/infra/musicfile` leaf package (IsResourceFork, CountUntagged) with 17 table-driven test cases, consumed by `GetAlbumTracks` in place of its inline `._`-prefix check.**

## Performance

- **Duration:** ~10 min
- **Started:** 2026-08-11T19:2x (approx, first commit 19:25:20+10:00)
- **Completed:** 2026-08-11T19:29:11+10:00
- **Tasks:** 2/2 completed
- **Files modified:** 4 (2 created, 2 modified) + 1 deferred-items.md doc

## Accomplishments
- Created `internal/infra/musicfile`, a dependency-free leaf package exporting `IsResourceFork` (the DATA-04 shared predicate for macOS `._` resource-fork detection) and `CountUntagged` (the DATA-02 skipped-file predicate), both table-driven tested with 17 total cases across the two functions (13 for `IsResourceFork` including a real Pi junk-basename case, 5 for `CountUntagged`).
- Refactored `GetAlbumTracks` — the one existing call site that filtered `._` files inline — to call `musicfile.IsResourceFork`, closing the loop the plan set up: Plan 01-02 (`internal/infra/mpd/client.go`) can now import the same predicate without inverting the codebase's infra→domain layering.
- Package doc comment on `musicfile.go` records the two verified layering facts (zero non-test `internal/infra`→`internal/domain` imports exist; `internal/domain/library` already imports `internal/infra/cache`) that justified overriding 01-PATTERNS.md's `internal/domain/library/filter.go` recommendation.

## Task Commits

Each task was committed atomically (TDD tasks split across RED/GREEN commits):

1. **Task 1: Create internal/infra/musicfile (RED)** - `37a6473` (test) — failing tests, package doesn't compile yet
2. **Task 1: Create internal/infra/musicfile (GREEN)** - `485857d` (feat) — implementation, all tests pass
3. **Task 2: Refactor GetAlbumTracks (pinning test)** - `91278aa` (test) — regression test added, passes against pre-refactor code by design
4. **Task 2: Refactor GetAlbumTracks (GREEN)** - `3b3fd71` (refactor) — inline check replaced with `musicfile.IsResourceFork`
5. **Deferred-items documentation** - `9d2f5b8` (docs) — logs out-of-scope gofmt/lint drift found during verification

**Plan metadata:** (this commit, following SUMMARY.md creation)

## Files Created/Modified
- `internal/infra/musicfile/musicfile.go` - `IsResourceFork` (basename `._`-prefix check via `path.Base` + `strings.HasPrefix`) and `CountUntagged` (real+untagged song counter); package doc explains the layering rationale
- `internal/infra/musicfile/musicfile_test.go` - `TestIsResourceFork` (13 cases, mirrors `player.isAppleDouble`'s table plus a live-Pi-realistic basename) and `TestCountUntagged` (5 cases: empty, all-tagged, one-untagged, resource-fork-with-empty-Album-excluded, no-file-key)
- `internal/domain/library/service.go` - `GetAlbumTracks` now calls `musicfile.IsResourceFork(file)` instead of the inline `path.Base` + `strings.HasPrefix(base, "._")` block; added the `musicfile` import
- `internal/domain/library/service_test.go` - added `TestService_GetAlbumTracks_ExcludesResourceForkFiles` (3-entry `FindAlbumTracksResp`: 2 real + 1 `._`-prefixed; asserts exactly 2 `Tracks` returned and no URI contains `._01`); added `strings` import for the `strings.Contains` assertion
- `.planning/phases/01-data-integrity-foundation/deferred-items.md` - new doc logging out-of-scope repo-wide gofmt/golangci-lint findings (see Deviations below)

## Decisions Made
- Confirmed and executed the plan's overridden helper location (`internal/infra/musicfile`, not `internal/domain/library/filter.go`) — verified independently by grepping for `internal/infra` files importing `internal/domain` (zero non-test hits) before writing any code.
- Followed the plan's explicit non-standard TDD instruction for Task 2: the "RED" step is a pinning test that is expected to pass against the pre-refactor code (proving current behavior), not a test that must fail — this is called out in the plan's `<action>` block and is intentional, not a gate violation.
- Left `player.isAppleDouble` untouched, per the plan's `<interfaces>` section marking it explicitly out of scope for this plan.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking, then reverted as out-of-scope] `make check`'s `go fmt ./...` step reformatted ~30 unrelated files**
- **Found during:** Task 2 verification (`make check`)
- **Issue:** Running the plan-specified `make check` gate triggers `go fmt ./...` repo-wide as its first step. It reformatted struct-tag column alignment in ~30 pre-existing files this plan never touched (e.g. `internal/infra/cache/types.go`, `internal/transport/socketio/server.go`), indicating a local `gofmt`/Go-toolchain version different from whatever last formatted the repo.
- **Fix:** Reverted the unrelated files with `git checkout -- <files>` (kept only the two files this plan actually modified staged), then re-verified `gofmt -l`, `go vet`, and `golangci-lint run` scoped to `internal/infra/musicfile/...` and `internal/domain/library/...` directly instead of re-running the repo-wide `make check` target.
- **Files modified:** None (reverted, not fixed) — logged instead to `.planning/phases/01-data-integrity-foundation/deferred-items.md`
- **Verification:** `gofmt -l` on the two touched files → clean; `go vet ./internal/domain/library/... ./internal/infra/musicfile/...` → clean; `golangci-lint run ./internal/infra/musicfile/... ./internal/domain/library/...` → `0 issues`
- **Committed in:** `9d2f5b8` (deferred-items.md documentation, no code changes)

**2. [Scope boundary - logged, not fixed] 62 pre-existing golangci-lint findings repo-wide**
- **Found during:** Task 2 verification (`make check`)
- **Issue:** `golangci-lint run ./...` reports 62 pre-existing issues (50 errcheck, 1 ineffassign, 7 staticcheck, 4 unused) across packages this plan never touched (`cmd/stellar`, `internal/infra/lcd`, `internal/infra/mpd`, `internal/infra/enrichment`, `internal/domain/artwork`, `internal/domain/localmusic`, `internal/domain/streaming/qobuz`, `internal/domain/sources`, `internal/transport/socketio`).
- **Fix:** Not fixed — confirmed zero findings in the two files this plan modified via a scoped `golangci-lint` run, and documented the repo-wide findings in `deferred-items.md` for a future dedicated cleanup pass.
- **Files modified:** None
- **Verification:** `golangci-lint run ./internal/infra/musicfile/... ./internal/domain/library/...` → `0 issues`
- **Committed in:** `9d2f5b8`

**3. [Rule 1 - Bug in own process] Corrected premature DATA-04 completion marking**
- **Found during:** State-update step, after running `requirements.mark-complete DATA-04`
- **Issue:** The state-update step mechanically marks every requirement ID in a plan's frontmatter
  as complete. This plan's frontmatter lists `requirements: [DATA-04]`, so the SDK flipped
  `DATA-04` to `[x]` and "Complete" in `REQUIREMENTS.md` after only this plan ran. But DATA-04's
  own text requires `._` filtering "everywhere it reads MPD song lists — not only in
  `GetAlbumTracks`", and Plans 01-02 (`GetAlbumDetails`) and 01-05 (cache builder paths) — both of
  which also declare `DATA-04` in their frontmatter — have not executed yet. Marking it fully
  complete here would misrepresent the requirement's actual state to anyone reading
  `REQUIREMENTS.md`.
- **Fix:** Reverted `DATA-04`'s checkbox to `[ ]` and its traceability-table status to
  "In Progress (01-01 done; 01-02, 01-05 pending)", with an inline note recording what this plan
  actually delivered (the shared predicate + `GetAlbumTracks`) versus what remains.
- **Files modified:** `.planning/REQUIREMENTS.md`
- **Verification:** Manual read of DATA-04's own acceptance text against this plan's actual scope
  (one of three call sites) confirms the correction is accurate.
- **Committed in:** part of the plan-metadata commit (final commit of this execution)

---

**Total deviations:** 3 (2 logged out-of-scope discoveries during verification, 1 auto-fixed process bug)
**Impact on plan:** None on the code delivered. Items 1-2 are pre-existing repo state unrelated to
`IsResourceFork`/`CountUntagged`/`GetAlbumTracks`; this plan's own files are gofmt-clean,
`go vet`-clean, and `golangci-lint`-clean. Item 3 corrects bookkeeping only — no code changed —
so `REQUIREMENTS.md` accurately reflects that DATA-04 needs Plans 01-02 and 01-05 to complete.

## Issues Encountered
None beyond the deviations documented above.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `musicfile.IsResourceFork` is ready for Plan 01-02 (`internal/infra/mpd/client.go` hardening) to import directly — no domain-layer import needed, confirming the layering decision holds.
- `musicfile.CountUntagged` is implemented and tested but not yet wired into any call site — Plan 01-0x covering DATA-02 (skipped-file observability / `CacheStats.SkippedCount`) is the consumer; this plan only delivered the predicate, per its stated objective of interface-first ordering.
- `deferred-items.md` flags the repo-wide gofmt/lint drift for whoever next runs a repo-wide `make check` in this phase — expect the same ~30-file reformatting noise and scope it out the same way (or address it as a deliberate separate commit).

---
*Phase: 01-data-integrity-foundation*
*Completed: 2026-08-11*

## Self-Check: PASSED

All 5 created/modified files confirmed present on disk; all 5 commit hashes
(37a6473, 485857d, 91278aa, 3b3fd71, 9d2f5b8) confirmed present in `git log`.
