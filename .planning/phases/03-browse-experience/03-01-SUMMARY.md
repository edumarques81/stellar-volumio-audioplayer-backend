---
phase: 03-browse-experience
plan: 01
subsystem: infra
tags: [go, tdd, leaf-package, regexp, table-driven-tests]

# Dependency graph
requires:
  - phase: 02-artist-identity-artwork-migration
    provides: internal/infra/artistidentity — the leaf-package precedent (package layering doc, table-driven real-corpus test style) this plan mirrors
provides:
  - "internal/infra/discgroup package with Folder/Group types and GroupFolders() entrypoint"
  - "Multi-disc box-set detection rule (BROWSE-07), proven against 5 real-corpus cases + 4 synthetic edge cases"
affects: [03-02, 03-03, cache-builder, domain-library-service]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Leaf package under internal/infra/ with zero internal/domain imports, mirroring internal/infra/musicfile and internal/infra/artistidentity"
    - "Table-driven tests built from live-measured corpus data (not invented examples), with synthetic edge cases explicitly labeled as such in the test name"

key-files:
  created:
    - internal/infra/discgroup/group.go
    - internal/infra/discgroup/group_test.go
  modified: []

key-decisions:
  - "Renamed the plan's specified entrypoint function from Group to GroupFolders — the plan's <interfaces> block specifies both `type Group struct` and `func Group(...) []Group` in the same package, which Go rejects as identifier redeclaration (does not compile). Kept the Group struct as specified since Plan 03's callers hold onto it pervasively; the function is discgroup.GroupFolders(folders), not discgroup.Group(folders)."
  - "Chose DiscCount=1 (not 0) as this package's convention for a single, ungrouped folder — documented in the Group.DiscCount field doc and the GroupFolders doc comment, per the plan's own 'pick one and document it' instruction."
  - "Representative-folder tie-break for RootDir/FirstTrack/Format/Genre on a merged group: lowest parsed Disc number, falling back to lexicographically first Directory on parse failure or a tie — implemented exactly as specified in the plan's <action> block."

requirements-completed: [BROWSE-07]

# Metrics
duration: ~15min
completed: 2026-08-12
---

# Phase 3 Plan 01: Multi-Disc Box-Set Detection Summary

**`internal/infra/discgroup.GroupFolders()` — pure two-signal (Disc tag + `/CD ?\d+/` path marker) box-set collapse rule, proven against live Pi-measured Mahler/Tosca/Rated R/Woody Allen groupings and the load-bearing Kind Of Blue non-grouping negative case.**

## Performance

- **Duration:** ~15 min
- **Completed:** 2026-08-12
- **Tasks:** 2 (TDD RED + GREEN)
- **Files modified:** 2 (both created)

## Accomplishments
- `internal/infra/discgroup` package created as a zero-dependency leaf under `internal/infra/`, importable by both `internal/domain/library` and `internal/infra/cache` without an import cycle (verified: `go build ./...` succeeds; no `internal/domain` import statement in `group.go`, only doc-comment prose referencing it by name).
- `GroupFolders(folders []Folder) []Group` clusters by `(lower(Album), lower(AlbumArtist))` and merges a cluster only when all three checks pass: (1) every Disc tag non-empty and mutually distinct, (2) every Directory matches the `/CD ?\d+/` path marker, (3) every folder shares one common parent directory.
- All 4 live positive cases collapse correctly: Mahler (11→1, DiscCount=11), Tosca (3→1, DiscCount=3, anchored at the parent of the `CD 1..3` folders — one level below the artist folder, per the plan's specific warning), Rated R (2→1, DiscCount=2, TrackCount=26 matching the live `search base` total), Woody Allen (2→1, DiscCount=2).
- The load-bearing negative case passes: Kind Of Blue's 3 folders, all carrying identical `Disc: "1"`, stay as 3 separate `Group`s — explicitly asserted field-by-field (Album, AlbumArtist, RootDir, TrackCount, Format) to be unchanged from the input.
- 4 synthetic defensive edge cases pass: missing Disc tag (with CD marker present), missing CD marker (with distinct Disc tags), a lone single-folder CD-marker cluster (never a box set at size 1), and a completely unrelated single album passthrough.

## Task Commits

Each task was committed atomically:

1. **Task 1 (RED): failing table-driven tests** - `f0155bc` (test)
2. **Task 2 (GREEN): implement discgroup.GroupFolders()** - `14fc6dc` (feat)

**Plan metadata:** (this commit)

## Files Created/Modified
- `internal/infra/discgroup/group.go` - `Folder`/`Group` types, `GroupFolders()` entrypoint, package-layering doc comment mirroring `artistidentity`'s rationale
- `internal/infra/discgroup/group_test.go` - `TestGroup_RealCorpus` (5 subtests: Mahler, Kind Of Blue negative, Tosca, Rated R, Woody Allen) + `TestGroup_DefensiveEdgeCases` (4 synthetic subtests)

## Decisions Made
- See `key-decisions` in frontmatter. The function-rename deviation is the only load-bearing one; documented inline in `group.go`'s doc comment above `GroupFolders` as well, so future readers hit the explanation at the call site, not just in this SUMMARY.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug in plan's interfaces contract] Renamed entrypoint function from `Group` to `GroupFolders`**
- **Found during:** Task 2 (GREEN implementation)
- **Issue:** The plan's `<interfaces>` block specifies both `type Group struct { ... }` and `func Group(folders []Folder) []Group` in the same `package discgroup`. Go does not allow a type and a function to share one identifier in one package — `go build` fails with `Group redeclared in this block`. This is not a design ambiguity; it's a language-level compile error baked into the plan text as written (confirmed by reproducing the exact error before fixing).
- **Fix:** Kept `type Group struct` exactly as specified (all field names/types/doc comments unchanged) since it is the pervasive return type Plan 03's callers will hold onto (`[]discgroup.Group`, `g.DiscCount`, etc.). Renamed the entrypoint function to `GroupFolders`. Documented the rename both in a doc-comment block directly above `func GroupFolders` in `group.go` and here, so Plan 03's implementer sees `discgroup.GroupFolders(folders)` is the correct call, not `discgroup.Group(folders)`.
- **Files modified:** `internal/infra/discgroup/group.go`, `internal/infra/discgroup/group_test.go` (all 9 call sites)
- **Verification:** `go build ./...` succeeds; `go test ./internal/infra/discgroup/... -v` — all 9 subtests pass (see Verification Evidence below).
- **Committed in:** `14fc6dc` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 — compile-blocking bug in the plan's own interface contract)
**Impact on plan:** Necessary for the code to compile at all. No scope creep — the `Group` struct's shape, all field semantics, and the clustering/merge algorithm match the plan's `<interfaces>` and `<action>` blocks exactly. **Action required for Plan 03 (wave 2):** when wiring this into `internal/domain/library/service.go` and `internal/infra/cache/builder.go`, call `discgroup.GroupFolders(folders)`, not `discgroup.Group(folders)`.

## Issues Encountered
None beyond the deviation above.

## Verification Evidence

```
$ go test ./internal/infra/discgroup/... -v -run TestGroup
=== RUN   TestGroup_RealCorpus
=== RUN   TestGroup_RealCorpus/Mahler_The_Symphonies:_11_CD-folders_collapse_to_1_Group_DiscCount=11
--- PASS: TestGroup_RealCorpus/Mahler_The_Symphonies:_11_CD-folders_collapse_to_1_Group_DiscCount=11 (0.00s)
=== RUN   TestGroup_RealCorpus/Miles_Davis_-_Kind_Of_Blue:_identical_Disc=1_on_all_3_folders_must_NOT_group_(D-06_negative_case)
--- PASS: TestGroup_RealCorpus/Miles_Davis_-_Kind_Of_Blue:_identical_Disc=1_on_all_3_folders_must_NOT_group_(D-06_negative_case) (0.00s)
=== RUN   TestGroup_RealCorpus/Puccini_Tosca_(Callas):_anchor_is_the_parent_of_the_CD_folders,_one_level_below_the_artist_folder
--- PASS: TestGroup_RealCorpus/Puccini_Tosca_(Callas):_anchor_is_the_parent_of_the_CD_folders,_one_level_below_the_artist_folder (0.00s)
=== RUN   TestGroup_RealCorpus/Rated_R_-_Deluxe_Edition:_2_CD-folders_collapse_to_1_Group_DiscCount=2,_TrackCount=26
--- PASS: TestGroup_RealCorpus/Rated_R_-_Deluxe_Edition:_2_CD-folders_collapse_to_1_Group_DiscCount=2,_TrackCount=26 (0.00s)
=== RUN   TestGroup_RealCorpus/BD_Music...Woody_Allen_Vol._1:_2_CD-folders_collapse_to_1_Group_DiscCount=2
--- PASS: TestGroup_RealCorpus/BD_Music...Woody_Allen_Vol._1:_2_CD-folders_collapse_to_1_Group_DiscCount=2 (0.00s)
--- PASS: TestGroup_RealCorpus (0.00s)
=== RUN   TestGroup_DefensiveEdgeCases
=== RUN   TestGroup_DefensiveEdgeCases/synthetic:_CD_marker_present_but_Disc_tag_missing_on_both_folders_->_NOT_grouped
--- PASS: TestGroup_DefensiveEdgeCases/synthetic:_CD_marker_present_but_Disc_tag_missing_on_both_folders_->_NOT_grouped (0.00s)
=== RUN   TestGroup_DefensiveEdgeCases/synthetic:_Disc_tag_distinct_but_no_CD_path_marker_->_NOT_grouped
--- PASS: TestGroup_DefensiveEdgeCases/synthetic:_Disc_tag_distinct_but_no_CD_path_marker_->_NOT_grouped (0.00s)
=== RUN   TestGroup_DefensiveEdgeCases/synthetic:_cluster_size_1_with_a_lone_CD-marker_folder_is_NOT_treated_as_a_box_set
--- PASS: TestGroup_DefensiveEdgeCases/synthetic:_cluster_size_1_with_a_lone_CD-marker_folder_is_NOT_treated_as_a_box_set (0.00s)
=== RUN   TestGroup_DefensiveEdgeCases/synthetic:_completely_unrelated_single-folder_album_passes_through_unchanged
--- PASS: TestGroup_DefensiveEdgeCases/synthetic:_completely_unrelated_single-folder_album_passes_through_unchanged (0.00s)
--- PASS: TestGroup_DefensiveEdgeCases (0.00s)
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/discgroup	0.383s
```

Gates run and their actual results:
- `go build ./...` — succeeds, no errors, whole repo.
- `go test ./...` — all packages `ok` (discgroup: `ok ... 0.149s`, no failures anywhere else in the repo).
- `go vet ./internal/infra/discgroup/...` — clean, no output.
- `golangci-lint run ./internal/infra/discgroup/...` — `0 issues.`
- `gofmt -l internal/infra/discgroup/*.go` — no output (both new files already gofmt-clean).
- `grep -r "internal/domain" internal/infra/discgroup/group.go` — only doc-comment prose mentions the string "internal/domain"; the file's only `import (...)` block contains `path`, `regexp`, `strconv`, `strings` (stdlib only).

RED-state evidence (Task 1, before `group.go` existed):
```
$ go test ./internal/infra/discgroup/...
internal/infra/discgroup/group_test.go:25:16: undefined: Folder
internal/infra/discgroup/group_test.go:43:10: undefined: Group
...
FAIL	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/discgroup [build failed]
```

## Known Stubs
None. This is a pure function with no I/O, no UI, no wiring — nothing to stub.

## Threat Flags
None. Per the plan's own threat model (T-03-01, disposition "accept"), this is a pure function over already-trusted internal data with no new trust boundary. Verified no new I/O, network, or file access was introduced.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `discgroup.GroupFolders()` and its `Folder`/`Group` types are ready to be wired into `internal/domain/library/service.go` and `internal/infra/cache/builder.go` by a later wave-2 plan in this phase.
- **Blocker/action item for the next plan:** the entrypoint is `discgroup.GroupFolders`, not `discgroup.Group` as originally specified in this plan's `<interfaces>` block — see Deviations above.
- No live Pi contact was required or performed to build or verify this plan, per its own success criteria.

## Self-Check: PASSED

- FOUND: internal/infra/discgroup/group.go
- FOUND: internal/infra/discgroup/group_test.go
- FOUND: .planning/phases/03-browse-experience/03-01-SUMMARY.md
- FOUND: f0155bc (RED commit)
- FOUND: 14fc6dc (GREEN commit)

---
*Phase: 03-browse-experience*
*Completed: 2026-08-12*
