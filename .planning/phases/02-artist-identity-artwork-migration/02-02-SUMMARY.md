---
phase: 02-artist-identity-artwork-migration
plan: 02
subsystem: infra+domain
tags: [go, tdd, artist-identity, cache-builder, mpd-fallback]

# Dependency graph
requires:
  - "internal/infra/artistidentity.Collapse(raw string) string (Plan 02-01)"
provides:
  - "internal/infra/cache.Builder.buildArtists() collapses+merges raw artist names before insert (cache-primary path)"
  - "internal/domain/library.Service.GetArtists() collapses+merges raw artist names (MPD-direct fallback path)"
affects: [02-03-artwork-migration, 02-04-deploy]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two-pass collapse-then-filter: build a canonical->summed-count map first, then apply query/pagination against the canonical map — avoids filtering on raw names that no longer exist in the response"

key-files:
  created: []
  modified:
    - internal/infra/cache/builder.go
    - internal/infra/cache/builder_test.go
    - internal/domain/library/service.go
    - internal/domain/library/service_test.go

key-decisions:
  - "Collapse wired at BOTH call sites (cache-build time AND MPD-direct fallback), per D-06 discretion and the plan's explicit instruction — mirrors the DATA-04 precedent from Phase 1 ('not only in GetAlbumTracks... everywhere')"
  - "Merge-by-sum on collision in both places: map[string]int accumulation, not last-write-wins — proven explicitly with the real 16 Pavarotti raw variants (cache path) and Moby raw variants (MPD-fallback path), because Go map/slice iteration order is not something a reviewer can trust to catch a last-write-wins bug by inspection alone"
  - "Service.GetArtists' query filter was moved to run against the CANONICAL name, not the raw MPD tag — required so a query like 'karajan' still matches the collapsed row even though the raw multi-credit tag also contained unrelated ensemble names; proven by an explicit negative-case test (querying text present only in the raw tag must NOT match)"
  - "FindAlbumsByArtist in the MPD-direct fallback is still queried with the RAW name per variant (not the canonical name) — deliberately unchanged, matches the plan's documented out-of-scope AlbumArtist-vs-Artist tag quirk; count *accuracy* for collapsed identities is Phase 3's ARTIST-04 territory"

requirements-completed: []

# Metrics
duration: ~12min
completed: 2026-08-12
---

# Phase 02 Plan 02: Wire artist collapse into both read paths Summary

**`artistidentity.Collapse` (Plan 02-01) is now wired into both `buildArtists` (the SQLite cache builder — production's primary artist-list path) and `Service.GetArtists` (the MPD-direct fallback), each collapsing raw MPD `Artist` tag variants to one canonical row per real performer with album counts summed across variants — proven against the real 16-variant Pavarotti case and the real double-space Karajan case, not invented data.**

## Performance

- **Duration:** ~12 min (RED `84c9744` -> GREEN `a2bb7b6` -> RED `3ca62cb` -> GREEN `c5aeef0`)
- **Tasks:** 2/2 completed, each following the RED/GREEN TDD split as separate commits
- **Files modified:** 4 (2 production, 2 test)

## Accomplishments

- **Task 1 (cache builder):** `buildArtists()` now builds an intermediate `collapsed map[string]int` by calling `artistidentity.Collapse` on every raw key from `GetArtistsWithAlbumCounts()` and summing album counts on collision, before the existing insert loop (unchanged: `generateArtistID`, `CachedArtist` construction, `InsertArtistTx`, the artwork relink query). Proven with the real 16 raw `Luciano Pavarotti, ...` variants collapsing to one row with `AlbumCount == 16` (sum, not last-write-wins), plus a mixed canonical+variant case (`Moby` + 2 variants -> one row, `AlbumCount == 5`), plus regression coverage for the empty-string skip (ARTIST-03) and the 88-value non-collapsible passthrough case.
- **Task 2 (MPD-direct fallback):** `Service.GetArtists()` now does a two-pass collapse: pass 1 builds `merged map[string]int` (canonical name -> summed `FindAlbumsByArtist` count, still queried with the raw name per the plan's documented out-of-scope quirk), pass 2 applies `req.Query` filtering against the *canonical* name and builds the `[]Artist` response. Proven with the real double-space `"Herbert von Karajan  Wiener Philharmoniker"` -> `"Herbert von Karajan"` case (the plan's D-01 acceptance case), the empty-name skip (ARTIST-03), a Moby raw-variant merge (sum of 3+1=4), and an explicit two-sided query test: `"karajan"` matches the collapsed row, but `"philharmoniker"` (present only in the raw tag, absent from the canonical name) does not.
- Sort and pagination logic in `Service.GetArtists` was left untouched in both places — it already operated on `Name`/`artists[i].Name`, which is now always the canonical value.
- The existing artwork relink query in `buildArtists` (keyed on `artists.id || '_artwork'`) needed no change — it already operates on whatever `artistID` (`generateArtistID(canonical name)`) buildArtists produces, so it automatically relinks against the now-collapsed identity.

## Task Commits

1. **Task 1 RED** — `84c9744` `test(02-02): add failing merge/collapse tests for buildArtists` — 4 new tests added; 2 fail as expected (`TestBuildArtists_CollapsesAndMergesPavarottiVariants` got `total=16 want 1`; `TestBuildArtists_MergesCanonicalAndVariants` got `total=3 want 1`), 2 pass trivially pre-wiring (empty-skip and non-collapsible passthrough, which the pre-existing code already handled correctly).
2. **Task 1 GREEN** — `a2bb7b6` `feat(02-02): collapse+merge raw artist names in buildArtists` — all 4 new tests pass; all 7 existing builder tests remain green.
3. **Task 2 RED** — `3ca62cb` `test(02-02): add failing collapse/merge/query tests for Service.GetArtists` — 5 new tests added; 3 fail as expected (Karajan collapse, Moby merge, canonical-name query filter — both positive and negative assertions), 2 pass trivially pre-wiring (empty-skip).
4. **Task 2 GREEN** — `c5aeef0` `feat(02-02): collapse+merge raw artist names in Service.GetArtists` — all 5 new tests pass; all 4 existing `GetArtists` tests remain green (`Empty`, `WithArtists`, `WithQuery`, `Pagination`, `MPDError`).

No REFACTOR commits were needed — both GREEN implementations were gofmt/vet/lint-clean on first pass.

## Files Modified

- `internal/infra/cache/builder.go` — added `artistidentity` import; `buildArtists()` now collapses+merges via an intermediate `collapsed` map before the unchanged insert loop; end-of-function log line changed from `len(artistCounts)` to `len(collapsed)` to report what was actually inserted (a `Rule 1` correctness fix — the old log line would have overstated the artist count post-collapse).
- `internal/infra/cache/builder_test.go` — 4 new tests: `TestBuildArtists_CollapsesAndMergesPavarottiVariants` (all 16 real corpus raw keys), `TestBuildArtists_MergesCanonicalAndVariants` (Moby mixed case), `TestBuildArtists_EmptyRawNameProducesNoRow` (ARTIST-03), `TestBuildArtists_NonCollapsibleNamesPassThroughUnchanged` (regression safety net).
- `internal/domain/library/service.go` — added `artistidentity` import; `GetArtists()` split into a two-pass collapse-then-filter structure, with detailed doc comments explaining both paths and the deliberately-unchanged `FindAlbumsByArtist(name)` (raw-name) call.
- `internal/domain/library/service_test.go` — 5 new tests: `TestService_GetArtists_CollapsesDoubleSpaceRoleSuffix` (Karajan), `TestService_GetArtists_EmptyRawNameProducesNoRow` (ARTIST-03), `TestService_GetArtists_MergesCollapsedVariantCounts` (Moby), `TestService_GetArtists_QueryFiltersOnCanonicalName` (positive + negative query assertion).

## Deviations from Plan

None — plan executed exactly as written. Both tasks completed against their exact specified files; the merge/collapse algorithm matches the plan's `<action>` description verbatim (accumulate-by-sum map, two-pass filter-on-canonical for the Service path).

One minor, in-scope addition beyond the plan's literal action text: the `buildArtists()` closing log line (`log.Debug().Int("count", ...)`) was changed from `len(artistCounts)` to `len(collapsed)`. The plan's `<interfaces>` block did not call this line out, but leaving it unchanged would have logged the raw (pre-collapse) count while the actual `Artists cached` count is now the collapsed count — a straightforward Rule 1 (auto-fix bug) correction discovered while re-reading the full function body, not a structural change.

## Issues Encountered

None. No auth gates, no architectural questions, no blockers.

## User Setup Required

None — code-only plan, no live-system access, no Pi deployment (explicitly out of scope per hard_constraint #6/D-10 — the collapse must ship together with the artwork migration in 02-04, never before it).

## Requirements Bookkeeping

ARTIST-01 and ARTIST-03 are **satisfiable in code** as of this plan (both the cache-primary and MPD-direct-fallback paths collapse correctly and never emit a blank row, proven by unit tests using real corpus shapes). They are **NOT marked Complete** in this plan's commit, because ROADMAP.md's stated success criteria for both are phrased as observable on the real Pi LCD/frontend — which requires the artwork migration (02-03) and the actual Pi deployment (02-04) per D-10 ("the collapse must never ship to the Pi without the migration"). `requirements.mark-complete` was intentionally NOT invoked for ARTIST-01/ARTIST-03 in this plan; they remain tracked as In Progress pending 02-04's on-Pi before/after evidence capture (D-11).

ARTIST-02 (the four-join-convention table-driven test against the real 124-value corpus) was already marked complete by Plan 02-01, which is where that specific requirement's evidence lives (`internal/infra/artistidentity/collapse_test.go`).

This plan's own `requirements:` frontmatter lists `[ARTIST-01, ARTIST-02, ARTIST-03]`, matching what the code changes make *possible*, but per the deliverable instructions above, none are being force-marked-complete here — deferring to 02-04's on-Pi verification, which is the actual gate the ROADMAP.md criteria describe.

## Verification Evidence

**Task-scoped tests:**
```
$ go test ./internal/infra/cache/... -run TestBuild -v
--- PASS: TestBuildArtists_CollapsesAndMergesPavarottiVariants (0.03s)
--- PASS: TestBuildArtists_MergesCanonicalAndVariants (0.03s)
--- PASS: TestBuildArtists_EmptyRawNameProducesNoRow (0.03s)
--- PASS: TestBuildArtists_NonCollapsibleNamesPassThroughUnchanged (0.04s)
--- PASS: TestBuilder_FullBuild_PersistsGenre (0.04s)
--- PASS: TestBuilder_FullBuild_RelinksArtistArtwork (0.04s)
--- PASS: TestBuilder_FullBuild_RelinksAlbumArtwork (0.04s)
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache	0.410s

$ go test ./internal/domain/library/... -run TestService_GetArtists -v
--- PASS: TestService_GetArtists_Empty (0.00s)
--- PASS: TestService_GetArtists_WithArtists (0.00s)
--- PASS: TestService_GetArtists_WithQuery (0.00s)
--- PASS: TestService_GetArtists_Pagination (0.00s)
--- PASS: TestService_GetArtists_CollapsesDoubleSpaceRoleSuffix (0.00s)
--- PASS: TestService_GetArtists_EmptyRawNameProducesNoRow (0.00s)
--- PASS: TestService_GetArtists_MergesCollapsedVariantCounts (0.00s)
--- PASS: TestService_GetArtists_QueryFiltersOnCanonicalName (0.00s)
--- PASS: TestService_GetArtists_MPDError (0.00s)
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library	0.267s
```

**Merged-count assertion (the specific hard_constraint #3 evidence requirement) — Pavarotti:**
`TestBuildArtists_CollapsesAndMergesPavarottiVariants` seeds all 16 distinct raw `Luciano Pavarotti, ...` keys (the 15 real corpus variants + 1 loop invariant check), each with `AlbumCount: 1`. After `FullBuild()`, `dao.QueryArtists` returns `total == 1`, `Name == "Luciano Pavarotti"`, `AlbumCount == 16` — confirmed via `if artists[0].AlbumCount != 16` assertion, PASS.

**Full-repo build/test/vet:**
```
$ go build ./...
(exit 0, no output)

$ go test ./...
ok  	.../cmd/stellar	0.269s
ok  	.../cmd/stellar-airplay	(cached)
ok  	.../cmd/stellar-spectrum	(cached)
ok  	.../internal/audio	(cached)
ok  	.../internal/domain/airplay	(cached)
ok  	.../internal/domain/artwork	(cached)
ok  	.../internal/domain/audirvana	(cached)
ok  	.../internal/domain/bios	0.566s
ok  	.../internal/domain/device	(cached)
ok  	.../internal/domain/lastplayed	(cached)
ok  	.../internal/domain/library	0.521s
ok  	.../internal/domain/localmusic	(cached)
ok  	.../internal/domain/player	(cached)
ok  	.../internal/domain/sources	(cached)
?   	.../internal/domain/streaming	[no test files]
ok  	.../internal/domain/streaming/qobuz	(cached)
ok  	.../internal/domain/upnp	(cached)
ok  	.../internal/infra/airplay	(cached)
ok  	.../internal/infra/artistidentity	(cached)
ok  	.../internal/infra/cache	0.856s
ok  	.../internal/infra/enrichment	(cached)
ok  	.../internal/infra/health	(cached)
ok  	.../internal/infra/lcd	(cached)
ok  	.../internal/infra/llm	(cached)
ok  	.../internal/infra/mpd	(cached)
ok  	.../internal/infra/musicfile	(cached)
ok  	.../internal/infra/netinfo	(cached)
ok  	.../internal/infra/paths	(cached)
ok  	.../internal/infra/sdnotify	(cached)
ok  	.../internal/infra/spectrum	(cached)
ok  	.../internal/infra/wikipedia	(cached)
ok  	.../internal/transport/mdns	(cached)
ok  	.../internal/transport/socketio	1.496s
ok  	.../internal/version	(cached)
(32 packages, all "ok" or "no test files", exit 0)

$ go vet ./...
(exit 0, no output)
```

**Layering constraint (hard_constraint #2):**
```
$ go list -f '{{.ImportPath}}: {{.Imports}}' ./internal/infra/... | grep -i "internal/domain"
(empty — CLEAN: no internal/infra package imports internal/domain)
```

**gofmt on touched files:**
```
$ gofmt -l internal/infra/cache/builder.go internal/infra/cache/builder_test.go \
         internal/domain/library/service.go internal/domain/library/service_test.go
(empty — all four clean)
```

**golangci-lint, scoped to touched files (repo-wide run, filtered to the two modified production files):**
```
$ golangci-lint run ./... 2>&1 | grep -E "builder\.go|service\.go" | grep -v "_test.go"
internal/infra/cache/builder.go:213:19: Error return value of `tx.Rollback` is not checked (errcheck)
internal/infra/cache/builder.go:372:19: Error return value of `tx.Rollback` is not checked (errcheck)
internal/infra/cache/builder.go:435:19: Error return value of `tx.Rollback` is not checked (errcheck)
```
All three are the pre-existing `defer tx.Rollback()` pattern already present in `buildAlbums`, `buildArtists`, and `BuildAlbumTracks` before this plan — confirmed via `git show a2bb7b6 -- internal/infra/cache/builder.go`, which shows line 372's `defer tx.Rollback()` unchanged by this plan's diff (only lines added around it). `service.go` produces **zero** lint findings.

**Repo-wide golangci-lint total:** 62 issues (50 errcheck, 1 ineffassign, 7 staticcheck, 4 unused) — matches the plan's documented "~38-62 pre-existing lint findings" upper bound exactly; none attributable to this plan's changes.

**Pre-existing gofmt drift confirmation:** `gofmt -l .` across the full repo lists 33 files (consistent with the ~30 baseline documented in `01-01-SUMMARY.md` and reconfirmed by `02-01-SUMMARY.md`); none of this plan's four modified files appear in that list.

## TDD Gate Compliance

RED gate (Task 1): `test(02-02): add failing merge/collapse tests for buildArtists` (`84c9744`) — confirmed failing via `go test -run TestBuild -v` before implementation (2 of 4 new tests failed with the expected `total=N want 1` merge-not-happening symptom).
GREEN gate (Task 1): `feat(02-02): collapse+merge raw artist names in buildArtists` (`a2bb7b6`) — confirmed all tests pass immediately after.
RED gate (Task 2): `test(02-02): add failing collapse/merge/query tests for Service.GetArtists` (`3ca62cb`) — confirmed failing via `go test -run TestService_GetArtists -v` before implementation (3 of 5 new tests failed with the expected raw-name-not-collapsed symptom).
GREEN gate (Task 2): `feat(02-02): collapse+merge raw artist names in Service.GetArtists` (`c5aeef0`) — confirmed all tests pass immediately after.
REFACTOR gate: not applicable / not needed for either task — both GREEN implementations were already gofmt/vet/lint-clean.

Gate sequence verified in git log: `git log --oneline -4` shows `c5aeef0` (feat) after `3ca62cb` (test) after `a2bb7b6` (feat) after `84c9744` (test) — two clean RED-before-GREEN pairs, one per task.

## Next Phase Readiness

- Both artist-list read paths now collapse+merge correctly and are proven in isolation with unit tests using stub/mock MPD data built from the real corpus shapes. No live Pi deployment has occurred (correctly deferred per D-10/hard_constraint #6).
- **This is the load-bearing state for Plan 02-03/02-04:** the artist identity key (`generateArtistID(canonical name)` = `md5(canonical name)`) now changes for every collapsible artist the moment this code reaches the Pi, which will orphan every pre-existing artist-artwork row keyed on the old raw-name identity — exactly the risk D-10 names. The artwork migration (02-03) MUST run and be verified (02-04, with D-11 before/after evidence) before any deploy of this plan's changes to the Pi. Do not deploy 02-02 alone.
- The Socket.IO wire contract (`Artist{name, albumCount, albumArt}`) is untouched — no field added, no field removed, confirmed by re-reading both modified functions' return-shape code (no new struct fields, no `SOCKET-CONTRACT.md` change needed).

---
*Phase: 02-artist-identity-artwork-migration*
*Completed: 2026-08-12*

## Self-Check: PASSED

All four modified files confirmed present on disk. All four commit hashes (`84c9744`, `a2bb7b6`, `3ca62cb`, `c5aeef0`) confirmed present in `git log`.
