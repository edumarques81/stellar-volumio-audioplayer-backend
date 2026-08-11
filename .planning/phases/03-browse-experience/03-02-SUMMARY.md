---
phase: 03-browse-experience
plan: 02
subsystem: infra
tags: [go, tdd, leaf-package, badge, table-driven-tests]

# Dependency graph
requires:
  - phase: 03-browse-experience
    plan: 01
    provides: "internal/infra/discgroup.GroupFolders() — grouping runs BEFORE badging, so badge logic operates on the post-grouping album set (removes Mahler/Tosca/Rated R/Woody Allen from this function's remit)"
  - phase: 02-artist-identity-artwork-migration
    provides: "internal/infra/artistidentity — the leaf-package precedent (package layering doc, table-driven real-corpus test style) this plan mirrors"
provides:
  - "internal/infra/dupebadge package with Candidate type and Compute() entrypoint"
  - "REVISED duplicate-disambiguation badge rule (D-02): quality -> disc -> source -> no badge precedence, proven against 4 live duplicate groups + 3 synthetic/negative edge cases"
affects: [03-03, 03-04, cache-builder, domain-library-service]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Leaf package under internal/infra/ with zero internal/domain imports, mirroring internal/infra/artistidentity and internal/infra/discgroup"
    - "Table-driven tests built from live-measured corpus data, with synthetic edge cases explicitly labeled as such in the test name"
    - "Test-only mirror of a domain-layer helper (formatQualityLabel) to prevent fixture drift without violating infra->domain layering"

key-files:
  created:
    - internal/infra/dupebadge/badge.go
    - internal/infra/dupebadge/badge_test.go
  modified: []

key-decisions:
  - "Compute() does not format quality strings itself — Candidate.Quality is accepted pre-formatted per the plan's <interfaces> contract (callers run formatQualityLabel before constructing Candidate). Hard constraint 5 ('reuse formatQualityLabel, mirror it in the leaf package if layering forbids import, and test the two produce identical strings') was satisfied by adding a byte-for-byte test-only mirror (mirrorFormatQualityLabel in badge_test.go, part of package dupebadge) plus TestQualityLabelMirror_MatchesFixtures, which asserts the mirror produces the exact fixture Quality strings for the real sample_rate/bit_depth/track_type triples measured on the Pi. This guards the fixtures against hand-typing drift without adding unused formatting logic to badge.go's production API, since Compute() itself never formats anything."
  - "No Disc value supplied for The Future Is Now's fixture (plan's <behavior> block did not specify one) — used Disc=\"\" on both candidates. Irrelevant to the outcome since the quality tier already wins for this cluster; documented inline in the test case name."

requirements-completed: [BROWSE-01, BROWSE-02, BROWSE-03]

# Metrics
duration: ~12min
completed: 2026-08-12
---

# Phase 3 Plan 02: Duplicate-Disambiguation Badge Summary

**`internal/infra/dupebadge.Compute()` — pure quality->disc->source precedence badge rule (D-02), proven against live Pi-measured Kind Of Blue/The Future Is Now/The Light For Days/Djesse Vol. 4 (Deluxe) post-grouping duplicate data, including the load-bearing Djesse no-badge negative case.**

## Performance

- **Duration:** ~12 min
- **Completed:** 2026-08-12
- **Tasks:** 2 (TDD RED + GREEN)
- **Files modified:** 2 (both created)

## Accomplishments
- `internal/infra/dupebadge` package created as a zero-dependency leaf under `internal/infra/`, importable by both `internal/domain/library` and `internal/infra/cache` without an import cycle (verified: `go build ./...` succeeds; the only actual `import (...)` statement in `badge.go` is `"strings"` — stdlib only; all `internal/domain` mentions in the file are doc-comment prose).
- `Compute(candidates []Candidate) []string` clusters by `(lower(Title), lower(Artist))`, preserving input order/index (built by index into a pre-sized result slice, not derived from map iteration order), and implements the exact 4-tier precedence from the `<interfaces>` contract: quality (cardinality>1) wins outright over disc, disc wins over source, and a cluster where nothing tracked varies gets `""` for every member.
- All 4 live post-grouping duplicate cases pass exactly as specified: Kind Of Blue (quality tier, 2 distinct qualities among 3 → each candidate's own Quality string verbatim), The Future Is Now (quality/format tier, FLAC vs WAV → own Quality string verbatim), The Light For Days (source tier, same quality/no disc, LOCAL vs USB → `["LOCAL","USB"]`), and Djesse Vol. 4 (Deluxe) — the load-bearing negative case — asserted explicitly as `["",""]`.
- The unique-album (cluster size 1) case is proven to yield `[""]`, satisfying BROWSE-03's "the common case stays clean" requirement.
- Two synthetic/defensive edge cases pass, each labeled as synthetic in the test name per the plan's instruction: the disc tier (no live example survives Plan 01's grouping, so this is invented data proving the tier exists and works) and the precedence short-circuit (a cluster varying in BOTH quality and source gets ONLY the quality badge, not a combination).
- Hard constraint 5 (reuse `formatQualityLabel` semantics) satisfied via a test-only byte-for-byte mirror (`mirrorFormatQualityLabel`, part of `package dupebadge` inside `badge_test.go`) plus `TestQualityLabelMirror_MatchesFixtures`, which asserts the mirror's output for the real sample_rate/bit_depth/track_type triples measured on the Pi matches the literal Quality strings hand-typed into the fixtures above — preventing the fixtures from silently drifting from the real formatting rules in `cached_service.go:12`.

## Task Commits

Each task was committed atomically:

1. **Task 1 (RED): failing table-driven tests** - `a756e4c` (test)
2. **Task 2 (GREEN): implement dupebadge.Compute()** - `a2db57c` (feat)

**Plan metadata:** (this commit)

## Files Created/Modified
- `internal/infra/dupebadge/badge.go` - `Candidate` type, `Compute()` entrypoint, package-layering doc comment mirroring `artistidentity`'s rationale plus the D-02 "why disc, not quality-only" rationale (6 of 8 live duplicate groups had one distinct quality)
- `internal/infra/dupebadge/badge_test.go` - `TestCompute_RealDuplicateGroupsAndEdgeCases` (7 subtests: Kind Of Blue, The Future Is Now, The Light For Days, Djesse negative, unique-album, synthetic disc-tier, synthetic precedence short-circuit) + `mirrorFormatQualityLabel` + `TestQualityLabelMirror_MatchesFixtures` (6 subtests, fixture-drift guard)

## Decisions Made
See `key-decisions` in frontmatter. Neither deviation required deviating from the plan's stated behavior or interface contract — both are implementation choices made where the plan left a gap (Disc value for The Future Is Now; where exactly to place the formatQualityLabel-mirroring test given Compute() itself doesn't format).

## Deviations from Plan

None - plan executed exactly as written. Both items in `key-decisions` are gap-fills within the plan's own stated discretion, not corrections to a bug or an architectural change, so they are not logged as Rule 1-4 deviations.

## Issues Encountered
None.

## Verification Evidence

RED-state evidence (Task 1, before `badge.go` existed):
```
$ go test ./internal/infra/dupebadge/...
internal/infra/dupebadge/badge_test.go:26:16: undefined: Candidate
internal/infra/dupebadge/badge_test.go:31:18: undefined: Candidate
...
internal/infra/dupebadge/badge_test.go:105:11: undefined: Compute
FAIL	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/dupebadge [build failed]
FAIL
```

GREEN-state evidence (Task 2, after `badge.go` implemented), all four precedence tiers plus the no-badge cases exercised:
```
$ go test ./internal/infra/dupebadge/... -v -run TestCompute
=== RUN   TestCompute_RealDuplicateGroupsAndEdgeCases
--- PASS: TestCompute_RealDuplicateGroupsAndEdgeCases (0.00s)
    --- PASS: .../Miles_Davis_-_Kind_Of_Blue:_quality_tier_...same_Disc=1,_same_Source=usb (0.00s)
    --- PASS: .../toe_-_The_Future_Is_Now:_quality/format_tier_...same_Source=usb (0.00s)
    --- PASS: .../Jacob_Collier_-_The_Light_For_Days:_source_tier_...differing_Source_(local_vs_usb) (0.00s)
    --- PASS: .../Jacob_Collier_-_Djesse_Vol._4_(Deluxe):_NEGATIVE_...expect_no_badge (0.00s)
    --- PASS: .../unique_album_(cluster_size_1)_—_no_badge_(BROWSE-03,_D-03) (0.00s)
    --- PASS: .../synthetic/defensive:_disc_tier_...same_Source_(no_live_example_survives_Plan_01's_grouping) (0.00s)
    --- PASS: .../synthetic/defensive:_precedence_short-circuit_...expect_ONLY_the_quality_badge (0.00s)
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/dupebadge	0.158s
```

Fixture-drift guard (constraint 5) evidence:
```
$ go test ./internal/infra/dupebadge/... -v -run TestQualityLabelMirror
=== RUN   TestQualityLabelMirror_MatchesFixtures
--- PASS: TestQualityLabelMirror_MatchesFixtures (0.00s)
    --- PASS: .../Kind_Of_Blue:_DSF_folder,_native_11.2896MHz_rate (0.00s)
    --- PASS: .../Kind_Of_Blue:_FLAC_folder,_352.8kHz/24bit (0.00s)
    --- PASS: .../The_Future_Is_Now:_FLAC,_44.1kHz/16bit (0.00s)
    --- PASS: .../The_Future_Is_Now:_WAV,_44.1kHz/16bit (0.00s)
    --- PASS: .../The_Light_For_Days:_FLAC,_96kHz/24bit_(both_copies) (0.00s)
    --- PASS: .../Djesse_Vol._4_(Deluxe):_FLAC,_96kHz/24bit_(both_copies) (0.00s)
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/dupebadge	0.151s
```

Gates run and their actual results:
- `go build ./...` — succeeds, no errors, whole repo.
- `go test ./...` — all packages `ok` (dupebadge: `ok ... 0.151s`, no failures anywhere else in the repo).
- `go vet ./internal/infra/dupebadge/...` — clean, exit 0, no output.
- `golangci-lint run ./internal/infra/dupebadge/...` — `0 issues.`
- `gofmt -l internal/infra/dupebadge/*.go` — no output (both new files already gofmt-clean).
- `grep -r "internal/domain" internal/infra/dupebadge/badge.go` — every match is doc-comment prose; the file's only `import (...)` block contains `"strings"` only.

## Known Stubs
None. This is a pure function with no I/O, no UI, no wiring — nothing to stub. Wiring `Compute()` into the album-listing code paths (cache builder / MPD-direct service) is explicitly out of scope for this plan (Plan 03/04, wave 2/3, per the plan's own objective).

## Threat Flags
None. Per the plan's own threat model (T-03-02, disposition "accept"), this is a pure function over already-trusted internal data with no new trust boundary. Verified no new I/O, network, or file access was introduced.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `dupebadge.Compute()` and its `Candidate` type are ready to be wired into `internal/domain/library/service.go` / `internal/infra/cache/builder.go` by a later wave-2/3 plan in this phase, which will construct `Candidate` slices (running `formatQualityLabel` first to populate `Candidate.Quality`) from the POST-grouping album set produced by `discgroup.GroupFolders()`.
- No live Pi contact was required or performed to build or verify this plan, per its own success criteria.
- Not deployed, not pushed, per hard constraint 7 — local commits only (`a756e4c`, `a2db57c`), pending the phase orchestrator's push per the user's stated per-phase commit+push rule.

## Self-Check: PASSED

- FOUND: internal/infra/dupebadge/badge.go
- FOUND: internal/infra/dupebadge/badge_test.go
- FOUND: a756e4c (RED commit)
- FOUND: a2db57c (GREEN commit)

---
*Phase: 03-browse-experience*
*Completed: 2026-08-12*
