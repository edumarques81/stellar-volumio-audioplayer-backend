---
phase: 02-artist-identity-artwork-migration
plan: 03
subsystem: infra/cache
tags: [go, tdd, sqlite, artist-identity, artwork-migration, startup-migration]

# Dependency graph
requires:
  - phase: 02-01
    provides: "internal/infra/artistidentity.Collapse(raw string) string"
  - phase: 02-02
    provides: "buildArtists()/Service.GetArtists() now collapse raw MPD Artist tag variants to canonical identity before insert -- the identity change this plan's rekey exists to survive"
provides:
  - "internal/infra/cache.MigrateArtistArtwork(dao, dryRun) -- idempotent, network-free re-key of ARTIST artwork rows from raw-name identity onto the ARTIST-01 collapsed identity, wired into InitializeCache() startup"
  - "internal/infra/cache.ListOrphanedAlbumArtwork(dao) + RekeyAlbumArtwork(dao, orphanArtworkID, targetAlbumID, dryRun) -- the tested, idempotent, non-clobbering album-artwork APPLY primitive Plan 02-04's perceptual-hash matcher will call for each human-approved orphan-to-album mapping"
affects: [02-04-deploy]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Startup-migration idiom mirrored from BackfillAlbumArtwork: pure local-DB read+rewrite, non-fatal warn-and-continue on error, info-log only when something actually changed"
    - "Idempotence-check ordering: for RekeyAlbumArtwork, check the TARGET's current state first (not the source/orphan row's current state) -- because a successful first application renames the source row away, so a source-row-first check would incorrectly fail a safe repeat call"
    - "Two-pass claim-then-apply merge tie-break for MigrateArtistArtwork: pass 1 scans every row to identify exact-match (already-canonical) rows and claims their own artwork slot if one exists; pass 2 processes only collapsible (raw != canonical) rows against the claimed set plus a live DB existence check, so idempotence and the merge-collision non-clobber rule both fall out of the same mechanism without a separate 'already migrated' tracking table"

key-files:
  created:
    - internal/infra/cache/artistmigration.go
    - internal/infra/cache/artistmigration_test.go
    - internal/infra/cache/albummigration.go
    - internal/infra/cache/albummigration_test.go
  modified:
    - internal/transport/socketio/server.go

key-decisions:
  - "RekeyAlbumArtwork's idempotence/clobber checks are ordered target-album-state-first, not orphan-row-first, deliberately DEPARTING from the plan's literal <action> step ordering -- the literal ordering (verify orphan row exists, with a same-id-only skip) would return a 'not found' error on a safe repeat call, because the orphan row no longer exists under its original id after the first application renamed it away. hard_constraint #2 explicitly named this exact case as 'the planner got this wrong once -- get it right', so the corrected ordering (inspect the target album's artwork_id first; only reach the orphan-existence check on a genuine first-application attempt) was implemented instead and is the version under test."
  - "MigrateArtistArtwork's merge tie-break is implemented as two passes over ALL scanned rows (not a pre-filtered candidate list) so that exact-match rows -- which are always skip-worthy by construction, since artists.id already equals md5(raw name) for every pre-migration row -- still get to CLAIM their own artwork slot in pass 1 before pass 2 evaluates collapsible variants against that claimed set. This reconciles an internal inconsistency in the plan's literal <action> wording (which described pass 1 as iterating rows from a 'remaining candidate' list that, by the plan's own skip condition, could never contain an exact-match row)."
  - "Checked the previously-unchecked `defer rows.Close()` / `defer tx.Rollback()` return values in both new production files (as `defer func() { _ = x.Close() }()`), rather than leaving them as bare defers. This is the same established codebase idiom used unchecked in backfill.go/builder.go, but those are PRE-EXISTING findings from before this plan (verified via git worktree diff against the pre-plan commit); the versions in this plan's brand-new files are NEW findings this plan would otherwise introduce, and the plan's own <verification> section requires zero new findings in the five touched files."

requirements-completed: []  # ART-01/ART-02/ART-03 intentionally NOT marked complete -- see Requirements Bookkeeping below

# Metrics
duration: ~35min
completed: 2026-08-12
---

# Phase 02 Plan 03: Artist-artwork rekey mechanism + album-artwork orphan apply primitive Summary

**Two new leaf files in `internal/infra/cache` (mirroring the existing `BackfillAlbumArtwork` startup-migration precedent): `MigrateArtistArtwork` auto-runs on every boot to move ARTIST artwork rows onto the Plan 02-02 collapsed identity with a proven merge tie-break and true idempotence, and `RekeyAlbumArtwork`/`ListOrphanedAlbumArtwork` give Plan 02-04's perceptual-hash matcher a tested, idempotent, non-clobbering primitive to apply each human-approved orphan-to-album mapping -- both entirely local-DB, zero network calls, zero filesystem writes.**

## Performance

- **Duration:** ~35 min (RED `440661e` → GREEN `177bd1f` for Task 1; RED `2712a6d` → GREEN `b26e2aa` for Task 2)
- **Tasks:** 2/2 completed, each following the RED/GREEN TDD split as separate commits
- **Files modified:** 5 (2 production, 2 test, 1 wiring)

## Accomplishments

- **Task 1 (`MigrateArtistArtwork`, ART-01):** Re-keys `artwork.id`/`artwork.artist_id` (and best-effort `artists.artwork_id`) from an artist's raw (pre-collapse) MPD-tag identity onto its `artistidentity.Collapse`-computed canonical identity. Proven against the plan's exact Karajan acceptance case (`"Herbert von Karajan  Wiener Philharmoniker"` → `"Herbert von Karajan"`, artwork follows), the Moby merge-collision tie-break (an exact-match bare `"Moby"` row's own artwork wins over a `"Moby, Jim James"` collaboration-credit row's artwork; the loser's own artwork is left in place, untouched), true idempotence (second run after success: `Rekeyed == 0`, artwork table byte-identical), dry-run (same report a real run would produce, zero writes), and the no-op-safety case for the 88-of-124 real values that need no collapse at all. Wired into `InitializeCache()` immediately after the existing `BackfillAlbumArtwork` call, same non-fatal warn-and-continue-on-error / info-on-success style.
- **Task 2 (`ListOrphanedAlbumArtwork` + `RekeyAlbumArtwork`, ART-02/ART-03):** `ListOrphanedAlbumArtwork` mirrors the exact query hand-validated against the live Pi DB during planning (`type='album' AND album_id NOT IN (SELECT id FROM albums)`, 38 rows confirmed 2026-08-12) and correctly excludes `type='artist'` rows even when their `album_id` column happens to be empty. `RekeyAlbumArtwork(dao, orphanArtworkID, targetAlbumID, dryRun)` is the apply primitive: proven for first-application (renames the orphan row's id/album_id to `<targetAlbumID>_artwork`/`targetAlbumID`, links `albums.artwork_id`, preserves the original file_path exactly -- D-09), the critical repeat-call safe no-op (re-applying the IDENTICAL pair after success returns `nil`, not an error, even though the orphan row no longer exists under its original id), refuse-to-clobber (a target album already linked to a DIFFERENT non-empty artwork errors and writes nothing), missing-orphan error (a bad/stale orphan id on a genuine first-application attempt errors descriptively), and dry-run (performs every precondition check, returns nil, writes nothing).
- Neither new production file imports `net/http`, an enrichment package, or the `os` package -- confirmed by grep as part of the acceptance criteria, not just by code review.

## Task Commits

1. **Task 1 RED** — `440661e` `test(02-03): add failing tests for artist-artwork rekey migration` — 5 tests added; all fail to compile (`undefined: cache.MigrateArtistArtwork`), the expected RED shape for an unimplemented function.
2. **Task 1 GREEN** — `177bd1f` `feat(02-03): rekey artist artwork onto collapsed identity on startup` — all 5 tests pass; wired into `InitializeCache()`.
3. **Task 2 RED** — `2712a6d` `test(02-03): add failing tests for album-artwork orphan apply primitive` — 6 tests added; all fail to compile (`undefined: cache.ListOrphanedAlbumArtwork` / `cache.RekeyAlbumArtwork`).
4. **Task 2 GREEN** — `b26e2aa` `feat(02-03): add album-artwork orphan apply primitive (ART-02, ART-03)` — all 6 tests pass; also folds in a lint cleanup to `artistmigration.go`/`artistmigration_test.go` (see Deviations).

No separate REFACTOR commits were needed -- both GREEN implementations were gofmt-clean on first pass, and the one lint finding each new file introduced was fixed within the same GREEN commit rather than deferred to a third commit.

## Files Created/Modified

- `internal/infra/cache/artistmigration.go` — `MigrateArtistArtwork(dao, dryRun) (*ArtistArtworkMigrationReport, error)`, `ArtistRekey`, `ArtistArtworkMigrationReport`. Package doc + function doc explicitly state zero network I/O; only imports `database/sql`, `fmt`, and this repo's own `artistidentity` package.
- `internal/infra/cache/artistmigration_test.go` — 5 tests + 2 shared test helpers (`newMigrationTestDB`, `dumpArtwork`, `md5ID`) reused by `albummigration_test.go`.
- `internal/infra/cache/albummigration.go` — `ListOrphanedAlbumArtwork(dao) ([]AlbumArtworkOrphan, error)`, `RekeyAlbumArtwork(dao, orphanArtworkID, targetAlbumID, dryRun) error`, `AlbumArtworkOrphan`. Imports only `database/sql` and `fmt` -- no `os`, confirmed by grep.
- `internal/infra/cache/albummigration_test.go` — 6 tests covering listing shape, first-application, repeat-call idempotence, refuse-to-clobber, missing-orphan error, and dry-run.
- `internal/transport/socketio/server.go` — `InitializeCache()` now calls `cache.MigrateArtistArtwork(s.cacheDAO, false)` immediately after the existing `cache.BackfillAlbumArtwork(...)` call, inside the same `s.cacheDAO != nil` guard, with matching warn-and-continue/info-on-success logging.

## Decisions Made

See `key-decisions` in frontmatter for the two implementation departures from the plan's literal `<action>` wording (both required to satisfy the plan's own explicitly-stated behavior/hard-constraint requirements) and the lint-cleanliness decision.

## Deviations from Plan

### Auto-fixed Issues (Rule 1 -- correctness fixes required to satisfy the plan's own explicit behavior spec)

**1. [Rule 1 - Bug] `RekeyAlbumArtwork` check ordering reversed from the plan's literal `<action>` text to actually satisfy the required idempotence semantics**
- **Found during:** Task 2, while translating the `<action>` block's literal step sequence into code before writing the repeat-call test.
- **Issue:** The plan's `<action>` text specifies, in order: (1) verify the orphan row exists at `orphanArtworkID` (erroring if not, "skip this check when `orphanArtworkID == wantArtworkID`"), then (2) look up the target album's current `artwork_id`, then (3) compare it against `wantArtworkID` for the idempotence/clobber decision. Followed literally, a REPEAT call with the identical `(orphanArtworkID, targetAlbumID)` pair after a successful first call would fail step (1): the orphan row's original id no longer exists in the `artwork` table (the first call renamed it to `wantArtworkID`), and `orphanArtworkID != wantArtworkID` in the normal case (the orphan's original id follows the source album's old, now-orphaned convention, not the target album's), so the "skip this check" exemption never applies. This would return an "orphan artwork row not found" error on the second call -- the exact wrong-not-nil-on-repeat failure hard_constraint #2 named explicitly as the mistake to avoid.
- **Fix:** Reordered the checks to be target-album-state-first: look up the target album's current `artwork_id` first; if it already equals `wantArtworkID` AND a matching artwork row exists there with the right `album_id`, return `nil` immediately (idempotent no-op) without ever needing the original orphan row to still exist. The orphan-row-existence check now only runs in the genuine first-application branch (target album currently unlinked), where it is the correct guard against a bad/stale orphan id.
- **Files modified:** `internal/infra/cache/albummigration.go`
- **Verification:** `TestAlbumMigration_RekeyAlbumArtwork_RepeatCallIsSafeNoOp` explicitly calls `RekeyAlbumArtwork` twice with the identical pair and asserts the second call returns `nil` and the artwork table is byte-identical before/after. `TestAlbumMigration_RekeyAlbumArtwork_RefusesToClobberDifferentArtwork` and `TestAlbumMigration_RekeyAlbumArtwork_ErrorsWhenOrphanMissing` confirm the reordering didn't weaken either the clobber-refusal or missing-orphan-error paths.
- **Committed in:** `b26e2aa` (part of Task 2's GREEN commit -- this was the correct implementation from the first GREEN write, not a later patch)

**2. [Rule 1 - Bug] `MigrateArtistArtwork`'s two-pass structure scans ALL rows (not a pre-filtered "candidate" list) to resolve an internal inconsistency in the plan's `<action>` wording**
- **Found during:** Task 1, while designing the merge-tie-break algorithm before writing the Moby fixture test.
- **Issue:** The plan's `<action>` text describes computing a skip condition (`canonical == "" or generateArtistID(canonical) == id`) that removes a row from further consideration, THEN says "process the remaining candidate rows in two passes... pass 1 iterates rows where `name == canonical`". But for every row in this codebase, `id` is always `md5(name)` (rows are inserted pre-collapse using the raw name as both `name` and the source of `id`), so `name == canonical` implies `generateArtistID(canonical) == id` by construction -- meaning any row pass 1 would want to iterate was, per the plan's own skip condition, already removed from the candidate list before pass 1 could see it. Read literally, pass 1 would always be an empty no-op, and the described "claim the target artwork slot" behavior (needed for the Moby merge fixture to pass) would never happen.
- **Fix:** Implemented pass 1 as a scan over ALL rows (not a pre-filtered list): for each row, compute `canonical` and `newID`; if `newID == r.id` (the exact-match / no-op-safety case), claim the row's own artwork slot (if it has one) in the `claimed` map and count it as `Skipped` -- this is the same disposition the plan's skip condition describes, just executed as part of pass 1 rather than as a separate pre-filter. Only rows where `newID != r.id` become pass-2 candidates.
- **Files modified:** `internal/infra/cache/artistmigration.go`
- **Verification:** `TestMigrateArtistArtwork_MergeTieBreakPrefersExactMatch` (the plan's own specified Moby fixture) passes: the exact-match `"Moby"` row's own artwork is preserved at the target id, and the `"Moby, Jim James"` variant's own artwork is left untouched, exactly as the plan's `<behavior>` section specifies.
- **Committed in:** `177bd1f` (part of Task 1's GREEN commit)

**3. [Rule 2 - Missing correctness requirement, lint cleanliness] Checked deferred `Close()`/`Rollback()` error returns in both new production files**
- **Found during:** Post-GREEN verification for Task 2, running `golangci-lint` scoped to the touched files (the plan's `<verification>` section requires "zero new findings in the five modified/created files").
- **Issue:** Both new production files followed `backfill.go`/`builder.go`'s established bare `defer rows.Close()` / `defer tx.Rollback()` idiom, which is accepted as PRE-EXISTING baseline noise elsewhere in the codebase (confirmed via a `git worktree` diff against the pre-plan commit `cd2b5a1`: `builder.go:213`'s identical pattern was present before this plan and is unchanged by it). But the specific instances in `artistmigration.go`/`albummigration.go` are BRAND NEW lines this plan introduces, so leaving them unchecked would be new findings the plan's own verification bar explicitly disallows, not pre-existing debt this plan is exempt from.
- **Fix:** Wrapped each bare `rows.Close()` / `tx.Rollback()` call in `defer func() { _ = x.Close() }()` (or the inline `_ = rows.Close()` form at non-deferred call sites in `artistmigration.go`'s row-scanning loop). No behavior change -- these calls were already best-effort cleanup, not part of any success path.
- **Files modified:** `internal/infra/cache/artistmigration.go`, `internal/infra/cache/albummigration.go`, `internal/infra/cache/artistmigration_test.go` (the shared `dumpArtwork` test helper had the same bare `defer rows.Close()` pattern)
- **Verification:** `golangci-lint run ./...` re-run 3 times with `golangci-lint cache clean` between each run (golangci-lint's `errcheck` output showed run-to-run non-determinism in which subset of the codebase's pre-existing ~50-issue errcheck backlog it samples/reports, so a single run's total count is not a reliable signal) -- all 3 runs showed zero findings referencing `cache/artistmigration.go`, `cache/albummigration.go`, or `transport/socketio/server.go`.
- **Committed in:** `b26e2aa` (Task 2's GREEN commit folds in this fix to both Task 1's and Task 2's production files, since both needed the same correction and Task 1 was already committed)

### Notes on plan-vs-implementation departures

Both Rule 1 fixes above are DELIBERATE departures from the plan's literal `<action>` prose, made to satisfy that same plan's explicit `<behavior>` test cases and `hard_constraints` (in particular hard_constraint #2's direct instruction: "the planner got this wrong once — get it right" about exactly the repeat-call idempotence case fix #1 addresses). Nothing was added beyond the plan's stated scope; both fixes are narrower, more literal-minded readings of "make the specified tests pass" applied where the `<action>` prose's step ordering could not have produced a passing implementation as written.

## Issues Encountered

None blocking. No auth gates (this plan never touches the network or the live Pi). No architectural questions (no schema change was needed -- confirmed no new table required, so `CurrentSchemaVersion` stays at `"5"`).

## User Setup Required

None. Code-only plan; no live-system access; no Pi deployment (correctly out of scope per hard_constraint #6 and D-10 -- the collapse + this migration + the album-orphan matching all land together in Plan 02-04, behind a human checkpoint, never before it).

## Requirements Bookkeeping

ART-01, ART-02, and ART-03 are **satisfiable in code** as of this plan:
- ART-01 (artist artwork survives the collapse): `MigrateArtistArtwork` exists, is wired into every boot via `InitializeCache()`, and is proven correct-and-idempotent by 5 unit tests using the plan's own specified fixtures (Karajan, Moby merge, idempotence, dry-run, no-op-safety).
- ART-02/ART-03 (album-orphan recovery mechanism, non-destructive): `ListOrphanedAlbumArtwork`/`RekeyAlbumArtwork` exist and are proven correct, idempotent, and non-clobbering by 6 unit tests, including the exact repeat-call and refuse-to-clobber cases hard_constraint #2 called out as the load-bearing behavior to get right.

None are marked Complete in this plan's commits, per the deliverable instructions: `requirements.mark-complete` was intentionally NOT invoked. ROADMAP.md's stated success criteria for all three requirements describe observable, on-Pi outcomes (artwork visibly still resolving after the collapse ships; the "albums with no artwork link" count dropping toward 0 with before/after evidence per D-11) that only Plan 02-04's live deployment and perceptual-hash matching run can satisfy. This plan built and tested the MECHANISM; Plan 02-04 supplies the MATCHING (which orphan belongs to which album) and the live verification evidence.

## Verification Evidence

**Task-scoped tests:**
```
$ go test ./internal/infra/cache/... -run TestMigrateArtistArtwork -v
--- PASS: TestMigrateArtistArtwork_RekeysSingleArtistArtwork (0.02-0.05s)
--- PASS: TestMigrateArtistArtwork_MergeTieBreakPrefersExactMatch (0.02-0.07s)
--- PASS: TestMigrateArtistArtwork_IdempotentOnSecondRun (0.02-0.07s)
--- PASS: TestMigrateArtistArtwork_DryRunLeavesArtworkTableUnchanged (0.02-0.06s)
--- PASS: TestMigrateArtistArtwork_NoOpWhenNameAlreadyCanonical (0.02-0.06s)
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache

$ go test ./internal/infra/cache/... -run TestAlbumMigration -v
--- PASS: TestAlbumMigration_ListOrphanedAlbumArtwork (0.03-0.05s)
--- PASS: TestAlbumMigration_RekeyAlbumArtwork_AppliesApprovedMapping (0.03-0.04s)
--- PASS: TestAlbumMigration_RekeyAlbumArtwork_RepeatCallIsSafeNoOp (0.03-0.05s)   <- the critical no-op case
--- PASS: TestAlbumMigration_RekeyAlbumArtwork_RefusesToClobberDifferentArtwork (0.03-0.04s)  <- the clobber-refusal case
--- PASS: TestAlbumMigration_RekeyAlbumArtwork_ErrorsWhenOrphanMissing (0.03-0.04s)
--- PASS: TestAlbumMigration_RekeyAlbumArtwork_DryRunPerformsChecksButNoWrite (0.03-0.04s)
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache
```

**Full-repo build/test/vet:**
```
$ go build ./...
(exit 0, no output)

$ go test ./...
32 packages, all "ok" or "no test files", exit 0
(includes all pre-existing suites -- builder_test.go's TestBuilder_FullBuild_RelinksArtistArtwork/
TestBuilder_FullBuild_RelinksAlbumArtwork, internal/domain/library's collapse tests from Plan 02-02,
and every other package -- no regressions)

$ go vet ./...
(exit 0, no output)
```

**Structural zero-network-call proof (acceptance criteria, Task 1):**
```
$ grep -c "net/http\|/enrichment" internal/infra/cache/artistmigration.go
0
```

**Structural zero-filesystem-write proof (acceptance criteria, Task 2):**
```
$ grep -c '"os"' internal/infra/cache/albummigration.go
0
```

**Layering constraint (hard_constraint #5):**
```
$ go list -f '{{.ImportPath}}: {{.Imports}}' ./internal/infra/... | grep -i "internal/domain"
(empty — CLEAN: no internal/infra package imports internal/domain)
```

**gofmt on touched files:**
```
$ gofmt -l internal/infra/cache/artistmigration.go internal/infra/cache/albummigration.go \
         internal/infra/cache/artistmigration_test.go internal/infra/cache/albummigration_test.go
(empty — all four clean)
```
Note: `internal/transport/socketio/server.go` has PRE-EXISTING gofmt drift (struct field alignment,
unrelated to this plan's edit region) confirmed present before this plan's changes via
`git stash` + `gofmt -l` -- not introduced by this plan, not fixed by this plan, per hard_constraint #8.

**golangci-lint, scoped to the five touched files (3 independent runs, `golangci-lint cache clean`
between each, to rule out the tool's own run-to-run sampling non-determinism observed on the
pre-existing ~50-issue errcheck backlog):**
```
$ golangci-lint run ./... 2>&1 | grep -n "cache/artistmigration\|cache/albummigration\|transport/socketio/server.go"
(empty, 3/3 runs)
```

**True pre-plan baseline vs. current repo-wide lint total, verified via `git worktree` against
commit `cd2b5a1` (the tip of Plan 02-02, immediately before this plan's work):**
Both the pre-plan baseline and the current tree report "62 issues (50 errcheck, 1 ineffassign,
7 staticcheck, 4 unused)" -- but a line-by-line diff of the actual issue lists (not just the
totals) showed golangci-lint's errcheck linter samples a different subset of the codebase's
real backlog on each run (confirmed non-deterministic even between two runs of the identical
tree), which is why the total count alone is not trustworthy evidence. The scoped
grep-for-touched-files check above is the reliable signal, and it is clean.

## TDD Gate Compliance

RED gate (Task 1): `test(02-03): add failing tests for artist-artwork rekey migration` (`440661e`) — confirmed failing via `go test -run TestMigrateArtistArtwork -v` before implementation (compile error: `undefined: cache.MigrateArtistArtwork`, the correct RED shape for a not-yet-implemented function).
GREEN gate (Task 1): `feat(02-03): rekey artist artwork onto collapsed identity on startup` (`177bd1f`) — confirmed all 5 tests pass immediately after.
RED gate (Task 2): `test(02-03): add failing tests for album-artwork orphan apply primitive` (`2712a6d`) — confirmed failing via `go test -run TestAlbumMigration -v` before implementation (compile error: `undefined: cache.ListOrphanedAlbumArtwork` / `cache.RekeyAlbumArtwork`).
GREEN gate (Task 2): `feat(02-03): add album-artwork orphan apply primitive (ART-02, ART-03)` (`b26e2aa`) — confirmed all 6 new tests pass immediately after, plus the full cache-package suite (43 tests) remains green.
REFACTOR gate: not applicable/not needed as a separate commit for either task — the lint-cleanliness fix (deviation #3) was folded into Task 2's GREEN commit rather than split into a third commit, since it touched both Task 1's and Task 2's files together and Task 1 was already committed.

Gate sequence verified in git log: `git log --oneline -4` shows `b26e2aa` (feat) after `2712a6d` (test) after `177bd1f` (feat) after `440661e` (test) — two clean RED-before-GREEN pairs, one per task.

## Next Phase Readiness

- `MigrateArtistArtwork` is live in `InitializeCache()` and will run automatically the moment this plan's changes reach the Pi -- but per D-10/hard_constraint #6, that deployment has NOT happened yet. This plan is code-only.
- Plan 02-04 needs: (1) deploy Plan 02-02's collapse + this plan's migration together (never separately, D-10); (2) capture the D-11 before/after artwork-link-count evidence on the Pi; (3) run the perceptual-hash matcher against the live `/albumart` endpoint and the 38 orphan files, producing an approved `(orphanArtworkID, targetAlbumID)` mapping table; (4) call `RekeyAlbumArtwork` for each approved pair (this plan's primitive is ready to receive that call, dry-run first per the plan's own suggested workflow).
- `ListOrphanedAlbumArtwork(dao)` is also ready to use directly as Plan 02-04's "which 38 rows are candidates" starting point -- no need to re-derive the query.

---
*Phase: 02-artist-identity-artwork-migration*
*Completed: 2026-08-12*

## Self-Check: PASSED

All four created files confirmed present on disk (`internal/infra/cache/artistmigration.go`,
`internal/infra/cache/artistmigration_test.go`, `internal/infra/cache/albummigration.go`,
`internal/infra/cache/albummigration_test.go`), plus the one modified file
(`internal/transport/socketio/server.go`). All four commit hashes (`440661e`, `177bd1f`,
`2712a6d`, `b26e2aa`) confirmed present in `git log`.
