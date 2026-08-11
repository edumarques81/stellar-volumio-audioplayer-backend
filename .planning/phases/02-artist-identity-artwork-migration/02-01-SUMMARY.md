---
phase: 02-artist-identity-artwork-migration
plan: 01
subsystem: infra
tags: [go, table-driven-tests, tdd, layering, artist-identity]

# Dependency graph
requires: []
provides:
  - "internal/infra/artistidentity package exporting Collapse(raw string) string"
  - "Corpus-proven collapse rule (all four join conventions: comma, spaced hyphen, ' with ', double-space) validated against all 124 real MPD artist values"
affects: [02-02-cache-builder-wiring, 02-mpd-direct-fallback-wiring, 02-artwork-migration]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "internal/infra/{leaf-package} for cross-cutting pure functions needed by both internal/infra/cache and internal/domain/library, avoiding an infra->domain import inversion (second instance of the internal/infra/musicfile pattern from Phase 1)"

key-files:
  created:
    - internal/infra/artistidentity/collapse.go
    - internal/infra/artistidentity/collapse_test.go

key-decisions:
  - "Single earliest-delimiter-wins scan (comma, ' - ', ' with ', '  ') handles all four ROADMAP join conventions with one code path, per the plan's explicit instruction not to special-case any convention"
  - "No hand-maintained exception list (D-06 discretion exercised toward 'uniform rule'): Adderley -> Adderley is a known, accepted imperfection, asserted explicitly in its own test"
  - "Package placed at internal/infra/artistidentity (leaf, zero internal deps), not internal/domain/library — mirrors the internal/infra/musicfile precedent from Phase 1 Plan 01, because internal/infra/cache (a later plan in this phase) needs Collapse without inverting the infra->domain import direction"

patterns-established:
  - "Table-driven Go test built directly from a captured live-data corpus file (02-ARTIST-CORPUS.md), generated programmatically from the corpus text to eliminate manual-transcription risk across 124 rows"

requirements-completed: [ARTIST-01, ARTIST-02, ARTIST-03]

# Metrics
duration: ~2min
completed: 2026-08-12
---

# Phase 02 Plan 01: Artist collapse rule (Collapse) Summary

**Pure `internal/infra/artistidentity.Collapse(raw string) string`, proven correct against every one of the 124 real MPD `list artist` values captured live from the Pi — 36 collapse to their documented canonical name, 88 pass through byte-identical.**

## Performance

- **Duration:** ~2 min (RED commit `be61eca` 02:44:49 -> GREEN commit `6d0167d` 02:45:33)
- **Started:** 2026-08-12T02:44Z (approx)
- **Completed:** 2026-08-12T02:45:33+10:00
- **Tasks:** 2/2 completed (RED, GREEN — no REFACTOR commit needed; implementation was gofmt/vet/lint-clean on first pass)
- **Files modified:** 2 (both created)

## Accomplishments
- Created `internal/infra/artistidentity`, a dependency-free leaf package (only stdlib `strings`) exporting `Collapse`, the ARTIST-01/ARTIST-02/ARTIST-03 rule that reduces a multi-credit raw MPD `Artist` tag value to its single first-credited performer.
- Built the test table programmatically from `02-ARTIST-CORPUS.md`'s verbatim "Full Artist list" (all 124 real values, extracted via `awk`/`sed`, not retyped) to eliminate transcription risk, then verified the intended algorithm against the corpus's 36-row "collapse targets" table with a standalone Python simulation *before* writing any Go code — zero mismatches (36/36 collapse-target matches, 88/88 passthrough matches).
- Implemented the single earliest-delimiter-wins scan (comma, ` - `, ` with `, `  `) as one code path for all four join conventions, with no hand-maintained exception list — the plan's D-06 discretion was exercised toward "uniform rule."
- Explicit standalone test (`TestCollapse_AdderleyKnownAcceptedImperfection`) pins `Adderley - Coltrane - Chambers - Cobb - Kelly` -> `Adderley` as a documented, accepted imperfection rather than silently accepting it as a table row.
- Fuzz-safety subtests (`TestCollapse_FuzzSafety`) cover 11 degenerate/adversarial inputs (whitespace-only, delimiter-only, leading-delimiter) — none panic, satisfying threat T-02-01-02.

## Task Commits

Each task was committed atomically (TDD RED/GREEN split):

1. **Task 1: Write the full 124-value table-driven test (RED)** - `be61eca` (test) — 224 lines added; `go vet` fails with `undefined: Collapse` as expected, `Collapse` does not exist yet
2. **Task 2: Implement Collapse (GREEN)** - `6d0167d` (feat) — 77 lines added; all 124+ corpus/fuzz/Adderley test cases pass; `gofmt`, `go vet`, `golangci-lint` clean on first pass (no separate REFACTOR commit was needed)

**Plan metadata:** (this commit, following SUMMARY.md creation)

_Note: no REFACTOR commit — the GREEN implementation was already gofmt-clean, vet-clean, and lint-clean, so no cleanup pass was required._

## Files Created/Modified
- `internal/infra/artistidentity/collapse.go` - `Collapse(raw string) string`; package doc explains the leaf-package layering rationale (mirrors `internal/infra/musicfile`'s doc from Phase 1); trims input, returns `""` immediately for empty/whitespace-only input, otherwise scans for the earliest of `,`, ` - `, ` with `, `  ` and returns the trimmed prefix before it, or the trimmed input unchanged if none match
- `internal/infra/artistidentity/collapse_test.go` - `TestCollapse_Corpus` (124 table rows, one per real corpus value, 36 with expected-collapse output and 88 with expected-passthrough output), `TestCollapse_AdderleyKnownAcceptedImperfection` (standalone, documented imperfection), `TestCollapse_FuzzSafety` (11 panic-safety subtests)

## Decisions Made
- Confirmed and followed the plan's mandated package location (`internal/infra/artistidentity`, a leaf package under `internal/infra`) — matches the `internal/infra/musicfile` precedent cited in the plan's `<interfaces>` section; verified zero internal imports (`go list -f '{{.Imports}}'` shows only `strings`).
- Verified the algorithm against the corpus with an independent Python simulation *before* writing the Go implementation, to catch any corpus/algorithm mismatch while it was still cheap to fix (none found).
- Generated the 124-row test table programmatically from the corpus's verbatim text (via `awk`/`sed`/Python) rather than hand-transcribing, specifically because hard_constraint #3 requires tests built from the real corpus, not invented or manually-retyped examples that could silently drift from the source.
- No REFACTOR commit: the plan's TDD flow allows for an optional REFACTOR step; the GREEN implementation required none (already clean under gofmt/vet/lint), so per the tdd_execution guidance ("REFACTOR (if needed)") it was skipped rather than added as a no-op commit.

## Deviations from Plan

None — plan executed exactly as written. Both tasks completed with their exact specified files, and the implementation matches the plan's algorithm description (single earliest-delimiter-wins scan across the four named delimiters) verbatim.

## Issues Encountered

None.

## User Setup Required

None — no external service configuration, no I/O, no live-system access (per hard_constraint #7, this plan touches no live system).

## Verification Evidence

**`go test ./internal/infra/artistidentity/... -v` (tail excerpt, full run below the aggregate counts):**

```
=== RUN   TestCollapse_Corpus
...
--- PASS: TestCollapse_Corpus (0.00s)
    [124 of 124 subtests PASS, 0 FAIL]
=== RUN   TestCollapse_AdderleyKnownAcceptedImperfection
--- PASS: TestCollapse_AdderleyKnownAcceptedImperfection (0.00s)
=== RUN   TestCollapse_FuzzSafety
--- PASS: TestCollapse_FuzzSafety (0.00s)
    [11 of 11 subtests PASS, 0 FAIL]
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/artistidentity	0.165s
```

Counted directly from the test binary output:
- `grep -c "^    --- PASS: TestCollapse_Corpus"` -> **124** (every corpus value passes)
- Of those 124: **36** expect a changed (collapsed) value, **88** expect byte-identical passthrough — matches the corpus doc's own count exactly ("36 of 124 values need collapsing").
- Independent Python simulation of the algorithm against all 124 corpus values, run before any Go code was written: 36/36 collapse-target matches, 88/88 passthrough matches, 0 mismatches.

**Full-repo build/test:**
```
$ go build ./...
(exit 0, no output)

$ go test ./...
ok  	.../cmd/stellar	(cached)
... [32 packages total, all "ok"] ...
ok  	.../internal/infra/artistidentity	(cached)
... 
(exit 0)
```

**Gates scoped to the two new files:**
```
$ gofmt -l internal/infra/artistidentity/collapse.go internal/infra/artistidentity/collapse_test.go
(empty — clean)

$ go vet ./...
(exit 0, no output — repo-wide, not just the new package)

$ golangci-lint run ./internal/infra/artistidentity/...
0 issues.

$ go list -f '{{.Imports}}' ./internal/infra/artistidentity/...
[strings]
```

**Pre-existing drift confirmation (not this plan's responsibility, per hard_constraint #6):**
`gofmt -l .` across the full repo still lists the same ~30 pre-existing files documented in Phase 1's `01-01-SUMMARY.md` (e.g. `internal/infra/cache/sqlite.go`, `internal/transport/socketio/server.go`). Neither `collapse.go` nor `collapse_test.go` appears in that list.

## TDD Gate Compliance

RED gate: `test(02-01): add failing corpus test for artist Collapse` (`be61eca`) — confirmed failing via `go vet` reporting `undefined: Collapse` before any implementation existed.
GREEN gate: `feat(02-01): implement artistidentity.Collapse` (`6d0167d`) — confirmed all tests pass immediately after.
REFACTOR gate: not applicable / not needed — GREEN implementation was already gofmt/vet/lint-clean.

Gate sequence verified in git log: `git log --oneline -2` shows `6d0167d` (feat) after `be61eca` (test), confirming RED-before-GREEN ordering.

## Next Phase Readiness
- `artistidentity.Collapse` is ready for the next plan in this phase (cache-builder wiring at `internal/infra/cache/builder.go:307-366`'s `buildArtists`) to import directly — no domain-layer import needed, confirming the layering decision holds (mirrors Phase 1's `musicfile` outcome).
- The collapse rule is fully proven against the corpus in isolation; it is NOT yet wired into any read path (`buildArtists`, `GetArtists`, or the MPD-direct fallback) — per this plan's explicit scope (only `collapse.go`/`collapse_test.go` in `files_modified`), wiring is deferred to the next plan(s), which per D-10 must ship together with the artwork migration.
- Because `Collapse` is not yet wired anywhere, this plan alone changes no observable backend behavior and requires no Pi deployment or before/after evidence capture (D-11 applies to the migration plan, not this one).

---
*Phase: 02-artist-identity-artwork-migration*
*Completed: 2026-08-12*

## Self-Check: PASSED

Both created files confirmed present on disk; both commit hashes (`be61eca`, `6d0167d`) confirmed present in `git log`.
