---
phase: 03-browse-experience
plan: 03
subsystem: domain-library
tags: [go, tdd, wiring, mpd, socketio, disc-grouping, dupe-badge]

# Dependency graph
requires:
  - phase: 03-browse-experience
    plan: 01
    provides: "internal/infra/discgroup.GroupFolders() — multi-disc box-set detection (BROWSE-07)"
  - phase: 03-browse-experience
    plan: 02
    provides: "internal/infra/dupebadge.Compute() — quality->disc->source duplicate-disambiguation badge rule (D-02)"
provides:
  - "library.Album.Badge / Album.DiscCount / Track.Disc — new additive wire fields"
  - "Service.GetAlbums and Service.GetArtistAlbums (MPD-direct path) return grouped + badged Album records"
  - "Service.GetAlbumTracks sorts a multi-disc album's combined track list by (disc, trackNumber, title)"
  - "MPD Disc tag threaded end-to-end: mpd.AlbumDetails -> socketio adapter -> library.AlbumDetails"
affects: [03-04, cache-builder, frontend-volumio2-ui, ios-remote]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Grouping (discgroup) stays basePath-scoped (a box set's discs always share one root under one source); badging (dupebadge) runs ONCE over the full merged album list across all basePaths, computed after the per-basePath loop rather than inside it — deviation from the plan's literal per-basePath wording, made to catch live cross-source duplicates (The Light For Days: LOCAL vs USB) that per-basePath badging would silently miss"
    - "discgroup.Group.Disc threaded through service.go as an unexported parallel slice aligned by index (not exposed on Album), consumed only by applyDupeBadges"

key-files:
  created: []
  modified:
    - internal/domain/library/types.go
    - internal/infra/mpd/client.go
    - internal/infra/mpd/client_albumdetails_test.go
    - internal/transport/socketio/library_mpd_adapter.go
    - internal/domain/library/service.go
    - internal/domain/library/service_test.go

key-decisions:
  - "Badging computed globally (across all queried basePaths) rather than per-basePath as the plan's <action> text literally describes. Reason: live duplicate groups measured in 03-CONTEXT.md ('The Light For Days': LOCAL vs USB, 'Djesse Vol. 4 (Deluxe)': two source copies) span DIFFERENT basePaths ('INTERNAL' vs 'USB' vs 'NAS'). Per-basePath badging (as literally written) would never see both halves of such a duplicate cluster in the same dupebadge.Compute() call, silently failing BROWSE-01/02 for exactly the cross-source cases the phase's own measured corpus documents. Grouping (discgroup.GroupFolders) stays per-basePath as specified, since a box set's member disc-folders always share one root directory under one source by construction. This is a Rule 1/2-class fix (correctness gap in the plan's own literal wording, not an architectural change) — no new files, no new types, no layering change; only the point in service.go where dupebadge.Compute() is called moved from inside getAlbumsFromBasePath to after the per-basePath loop in GetAlbums/GetArtistAlbums. All of this plan's own required tests (which use single-basePath fixtures) pass either way, since per-basePath and global badging are observationally identical when only one basePath is queried."
  - "discgroup.Group.Disc (only meaningful when DiscCount<=1) is threaded through as an unexported []string parallel to []Album, aligned by index, never added as an Album field — per the plan's explicit instruction not to expose it on Album."
  - "Album.DiscCount is only set when group.DiscCount>1; an ungrouped discgroup.Group (whose own convention is DiscCount=1) maps to Album.DiscCount=0 so omitempty drops it, preserving the existing 'zero/unset = ordinary single-disc album' JSON contract."

requirements-completed: [BROWSE-01, BROWSE-02, BROWSE-03, BROWSE-07]

# Metrics
duration: ~20min
completed: 2026-08-12
---

# Phase 3 Plan 03: Backend Grouping + Badging Wiring Summary

**Wired `discgroup.GroupFolders()` and `dupebadge.Compute()` into `Service.GetAlbums`/`GetArtistAlbums` (the MPD-direct fallback path — the ONLY path `GetArtistAlbums` ever takes), fixed the grouped-album URI bug (RootDir, not `path.Dir(FirstTrack)`), made `GetAlbumTracks` disc-aware, and added the `Badge`/`DiscCount`/`Disc` wire-contract fields Plan 04's cache wiring depends on — with badging deliberately computed across the full merged album list (not per basePath) to catch live cross-source duplicates.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-08-12T08:08:19+10:00
- **Completed:** 2026-08-12T08:13:56+10:00
- **Tasks:** 3 (Task 1 auto; Tasks 2 & 3 TDD RED→GREEN)
- **Files modified:** 6

## Accomplishments
- `library.Album` gains `Badge` (`json:"badge,omitempty"`) and `DiscCount` (`json:"discCount,omitempty"`); `library.Track` gains `Disc` (`json:"disc,omitempty"`) — all additive, verified via a throwaway marshal check that zero-value structs omit all three keys and non-zero values include them (constraint 2, D-11).
- MPD's `Disc` tag now flows end-to-end: `mpd.AlbumDetails.Disc` (first-track-wins, `groupAlbumDetails`) → `LibraryMPDAdapter.GetAlbumDetails` → `library.AlbumDetails.Disc`, with a new `TestGroupAlbumDetails_DiscFirstTrackWins` pinning the capture.
- `Service.getAlbumsFromBasePath` and `Service.GetArtistAlbums` both group raw per-folder `AlbumDetails` via `discgroup.GroupFolders` before building `Album` records. The grouped-album URI bug is fixed: URI is now `group.RootDir` (the box set's common parent directory), not `path.Dir(group.FirstTrack)` (which would have pointed at disc 1's own subfolder) — this is what makes `GetAlbumTracks`'s `SearchByBase(uri)` recursively return every disc's tracks with no new query logic.
- Duplicate-disambiguation badging (`dupebadge.Compute`) runs once over the full merged album list per request (see Deviations below), not per basePath, so cross-source duplicate groups get badged correctly, not just same-source ones.
- `GetAlbumTracks` now parses MPD's `Disc` tag into `Track.Disc` (same `"N"`/`"N/M"`-tolerant parsing as `TrackNumber`) and sorts the final track list by `(Disc, TrackNumber, Title)` instead of `(TrackNumber, Title)`, so a grouped multi-disc album's combined, MPD-interleaved `SearchByBase` result never interleaves discs in the response.
- Kind Of Blue's load-bearing D-06 negative case is asserted at this wiring layer (not just the leaf `discgroup`/`dupebadge` packages), at both `GetAlbums` and `GetArtistAlbums`: 3 folders sharing identical `Disc:"1"` and no CD-marker path stay 3 separate `Album` records, each carrying its own quality string as `Badge` (the quality tier fires because the 3 fixture qualities are DSD64/DSD128/352.8kHz-24bit-FLAC, all distinct).
- Mahler's acceptance case is asserted at both layers too: 11 raw `AlbumDetails` (Disc `"1".."11"`, `CD 01`..`CD 11` folders) collapse to exactly 1 `Album` with `DiscCount==11`, `URI` equal to the common parent, `TrackCount` summed across all 11 discs, and `Badge==""` (a single collapsed album is not a duplicate cluster).

## Task Commits

Each task was committed atomically:

1. **Task 1: Add Badge/DiscCount/Disc wire fields + thread MPD Disc tag** - `509b0d0` (feat)
2. **Task 2 (RED): failing grouping+badging wiring tests** - `e8929e8` (test)
3. **Task 2 (GREEN): wire discgroup.GroupFolders() + dupebadge.Compute()** - `159cd7a` (feat)
4. **Task 3 (RED): failing disc-aware GetAlbumTracks sort test** - `e4096c5` (test)
5. **Task 3 (GREEN): disc-aware GetAlbumTracks sort** - `8fa8cb9` (feat)

**Plan metadata:** (this commit)

## Files Created/Modified
- `internal/domain/library/types.go` - `Album.Badge`/`Album.DiscCount`/`Track.Disc` additive JSON fields
- `internal/infra/mpd/client.go` - `AlbumDetails.Disc` field + `groupAlbumDetails` first-track-wins capture
- `internal/infra/mpd/client_albumdetails_test.go` - `TestGroupAlbumDetails_DiscFirstTrackWins`
- `internal/transport/socketio/library_mpd_adapter.go` - `Disc: d.Disc,` in the `GetAlbumDetails` copy literal
- `internal/domain/library/service.go` - own `AlbumDetails.Disc` mirror; `getAlbumsFromBasePath`/`GetArtistAlbums` grouping+URI fix; new `foldersFromAlbumDetails`/`albumFromGroup`/`applyDupeBadges` helpers; `GetAlbumTracks` disc parsing + sort comparator
- `internal/domain/library/service_test.go` - 7 new tests: Mahler-shaped grouping (GetAlbums + GetArtistAlbums), Kind Of Blue D-06 negative case (GetAlbums + GetArtistAlbums), unique-album no-badge case, disc-aware sort, disc-absent-yields-zero

## Decisions Made
See `key-decisions` in frontmatter. The badging-scope decision is the only load-bearing deviation from the plan's literal wording; the other two are gap-fills within the plan's own explicit instructions (Disc threading, DiscCount zero-vs-1 convention).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1/2 — correctness gap in the plan's literal per-basePath badging wording] Computed dupebadge.Compute() over the full merged album list, not per basePath**
- **Found during:** Task 2 (GREEN implementation), while re-reading 03-CONTEXT.md's live duplicate-group table alongside the plan's `<action>` text.
- **Issue:** The plan's `<action>` block describes computing badges "after building the full albums slice for **this basePath**" — i.e., inside `getAlbumsFromBasePath`, scoped to one basePath's own results. But 03-CONTEXT.md's own measured live corpus documents duplicate groups that span DIFFERENT basePaths: "The Light For Days" (LOCAL vs USB) and "Djesse Vol. 4 (Deluxe)" (two source copies). `GetAlbums(scope=all)` queries `INTERNAL`, `USB`, and `NAS` as three separate `getAlbumsFromBasePath` calls; if badging happened inside each call, `dupebadge.Compute()` would only ever see one basePath's candidates at a time and could never detect a duplicate whose two versions live in different basePaths — silently failing BROWSE-01/02 for exactly the cross-source cases this phase's own scoping work measured and documented.
- **Fix:** Kept grouping (`discgroup.GroupFolders`) per-basePath exactly as specified — a box set's member disc-folders always share one common root directory under one source by construction, so per-basePath grouping is correct and required. Moved the badging call: `getAlbumsFromBasePath` now returns `([]Album, []string)` (albums plus each album's representative `discgroup.Group.Disc` value, aligned by index, never exposed on `Album`); `GetAlbums` and `GetArtistAlbums` each accumulate albums+discs across their basePath loop and call the new `applyDupeBadges(albums, discs)` helper ONCE, after all basePaths have been merged, before sorting/pagination.
- **Files modified:** `internal/domain/library/service.go` only — no new files, no new types, no layering change.
- **Verification:** All of this plan's own required tests use single-basePath fixtures (`ScopeUSB`, one basePath's `GetAlbumDetailsResp` entry), so per-basePath and global badging are observationally IDENTICAL for every test this plan specifies — moving the badging call did not require changing any planned test's expected outcome, and all 5 originally-specified acceptance-criteria tests plus 2 additional GetArtistAlbums-layer tests pass. `go build ./...` and `go test ./...` both clean.
- **Committed in:** `159cd7a` (Task 2 GREEN commit)

---

**Total deviations:** 1 auto-fixed (Rule 1/2 — cross-basePath badging correctness gap in the plan's literal per-basePath wording). No architectural change, no new dependency, no renamed/removed field — additive-only per hard constraint 2.
**Impact on plan:** Strengthens BROWSE-01/02 to actually hold for `scope=all` and multi-source duplicate groups documented in this phase's own CONTEXT.md, without touching any test this plan required.

## Issues Encountered
None beyond the deviation above.

## Verification Evidence

Task 1 (RED n/a — non-TDD task; verified directly):
```
$ go build ./... && go test ./internal/infra/mpd/... -run TestGroupAlbumDetails -v
--- PASS: TestGroupAlbumDetails (0.00s)
    --- PASS: TestGroupAlbumDetails/case_a:... (0.00s)
    --- PASS: TestGroupAlbumDetails/case_b:... (0.00s)
    --- PASS: TestGroupAlbumDetails/case_c:... (0.00s)
    --- PASS: TestGroupAlbumDetails/case_d:... (0.00s)
--- PASS: TestGroupAlbumDetails_AggregationUnchanged (0.00s)
--- PASS: TestGroupAlbumDetails_DiscFirstTrackWins (0.00s)
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd	0.396s
```

Task 2 RED (before wiring, `e8929e8`):
```
$ go test ./internal/domain/library/... -run 'TestService_GetAlbums_MahlerShaped|TestService_GetAlbums_KindOfBlueShaped|TestService_GetAlbums_UniqueAlbum|TestService_GetArtistAlbums_MahlerShaped|TestService_GetArtistAlbums_KindOfBlueShaped' -v
--- FAIL: TestService_GetAlbums_MahlerShaped_GroupsToOneAlbumWithDiscCount
    service_test.go:474: Expected 1 grouped Mahler album, got 11: [...]
--- FAIL: TestService_GetAlbums_KindOfBlueShaped_StaysSeparateWithQualityBadges
    service_test.go:537: Badge is empty, want each album's own quality string for "USB/Miles Davis/Kind Of Blue (DSD64)"
    service_test.go:540: Badge = "", want it to equal this album's own Quality "DSD64" (quality tier)
    ...
--- PASS: TestService_GetAlbums_UniqueAlbum_NoBadgeNoDiscCount   (trivially true pre-wiring)
--- FAIL: TestService_GetArtistAlbums_MahlerShaped_GroupsToOneAlbumWithDiscCount
--- FAIL: TestService_GetArtistAlbums_KindOfBlueShaped_StaysSeparate
FAIL
```

Task 2 GREEN (after wiring, `159cd7a`):
```
$ go test ./internal/domain/library/... -v
--- PASS: TestService_GetAlbums_MahlerShaped_GroupsToOneAlbumWithDiscCount (0.00s)
--- PASS: TestService_GetAlbums_KindOfBlueShaped_StaysSeparateWithQualityBadges (0.00s)
--- PASS: TestService_GetAlbums_UniqueAlbum_NoBadgeNoDiscCount (0.00s)
--- PASS: TestService_GetArtistAlbums_MahlerShaped_GroupsToOneAlbumWithDiscCount (0.00s)
--- PASS: TestService_GetArtistAlbums_KindOfBlueShaped_StaysSeparate (0.00s)
[... all 27 pre-existing library-package tests also PASS, unmodified ...]
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library	0.371s
```

Task 3 RED (before disc-aware sort, `e4096c5`):
```
$ go test ./internal/domain/library/... -run 'TestService_GetAlbumTracks_SortsByDiscThenTrackNumber|TestService_GetAlbumTracks_DiscZeroWhenAbsent' -v
--- FAIL: TestService_GetAlbumTracks_SortsByDiscThenTrackNumber
    service_test.go:1199: Tracks[0] = (Disc:0, TrackNumber:1), want (Disc:1, TrackNumber:1): [...]
    service_test.go:1199: Tracks[1] = (Disc:0, TrackNumber:1), want (Disc:1, TrackNumber:2): [...]
    service_test.go:1199: Tracks[2] = (Disc:0, TrackNumber:2), want (Disc:2, TrackNumber:1): [...]
--- PASS: TestService_GetAlbumTracks_DiscZeroWhenAbsent (0.00s)   (trivially true pre-wiring: Disc was always 0)
FAIL
```

Task 3 GREEN (after disc-aware sort, `8fa8cb9`):
```
$ go test ./internal/domain/library/... -v -run TestService_GetAlbumTracks
--- PASS: TestService_GetAlbumTracks_EmptyAlbum (0.00s)
--- PASS: TestService_GetAlbumTracks_WithTracks (0.00s)
--- PASS: TestService_GetAlbumTracks_ExcludesResourceForkFiles (0.00s)
--- PASS: TestService_GetAlbumTracks_SortsByDiscThenTrackNumber (0.00s)
--- PASS: TestService_GetAlbumTracks_DiscZeroWhenAbsent (0.00s)
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library	0.266s
```

Wire-contract additive-fields check (`omitempty` verified by marshal, throwaway test not committed):
```
zero Album:  {"id":"","title":"","artist":"","uri":"","source":"","addedAt":"0001-01-01T00:00:00Z"}
non-zero:    {..., "badge":"Disc 04","discCount":11}
zero Track:  {"id":"","title":"","artist":"","album":"","uri":"","source":""}
non-zero:    {..., "disc":2, "source":""}
```
No `badge`/`discCount`/`disc` key appears on zero values; both appear when set — D-10/D-11 satisfied.

Gates run and their actual results (whole repo, after all 3 tasks):
- `go build ./...` — succeeds, no errors.
- `go test ./...` — all packages `ok` (library: `ok ... 0.207-0.371s` across runs; mpd: `ok ... 2.568s`; the only non-`ok` line is `internal/domain/streaming [no test files]`, pre-existing and unrelated).
- `go vet ./...` — clean, no output, whole repo.
- `golangci-lint run ./internal/domain/library/... ./internal/infra/mpd/... ./internal/transport/socketio/...` — 23 pre-existing issues, ALL in files this plan did not touch (`client_test.go`, `audioengine_handlers.go`, `bio_handlers.go`, `cache_handlers.go`, `remote_audio.go`, `remote_sources.go`, `server.go`, `server_test.go`, `spectrum_ingest.go`; the one `client.go:1028` staticcheck finding pre-dates this plan's edits, confirmed via `git show <pre-plan-commit>:internal/infra/mpd/client.go`). 0 issues in any file this plan created or modified.
- `gofmt -l` on every file this plan touched (`service.go`, `service_test.go`, `client.go`, `client_albumdetails_test.go`, `library_mpd_adapter.go`) — no output, all clean. `types.go` shows pre-existing gofmt drift in the unrelated `SortOrder` const block, confirmed present before this plan's Task 1 commit via `git show <parent>:internal/domain/library/types.go | gofmt -l -` (part of the documented ~30 pre-existing drifted files, not this plan's to fix).

## Known Stubs
None. All wiring is real — `Service.GetAlbums`/`GetArtistAlbums`/`GetAlbumTracks` compute grouped, badged, disc-sorted data from live MPD responses via the mock-tested code path; no hardcoded/placeholder values were introduced.

## Threat Flags
None beyond the plan's own threat model (T-03-03, T-03-04, both disposition "accept"). No new network endpoints, auth paths, or trust boundaries were introduced — `applyDupeBadges`/`foldersFromAlbumDetails`/`albumFromGroup` are pure in-process transformations over already-trusted MPD-sourced data, matching T-03-04's "purely derived from MPD tag data already fully exposed" disposition. The grouped-album URI widening (T-03-03) is exactly as the plan's threat model already anticipated: `RootDir` is a superset-widening of what recursive `search base` already returned for any basePath-level query, not a new client capability.

## User Setup Required
None — no external service configuration required.

## Next Phase Readiness
- `library.Album.Badge`/`DiscCount` and `library.Track.Disc` are the additive wire-contract fields Plan 04 (cache wiring) needs to populate on the cache-primary path (`CachedService`/`internal/infra/cache.Builder`).
- The `foldersFromAlbumDetails`/`albumFromGroup`/`applyDupeBadges` helpers in `internal/domain/library/service.go` are private to this package; Plan 04's cache builder lives under `internal/infra/cache` and will need its own call sites into `discgroup.GroupFolders`/`dupebadge.Compute` (per the infra->domain layering rule, it cannot import these helpers from `internal/domain/library`).
- **Note for Plan 04 / any future cache-side badging work:** replicate the badging-scope decision from this plan's Deviations section — compute `dupebadge.Compute()` over the FULL corpus being assembled, not per source/basePath, to catch cross-source duplicates like "The Light For Days" and "Djesse Vol. 4 (Deluxe)".
- `docs/SOCKET-CONTRACT.md` still needs the `badge`/`discCount`/`disc` fields documented (D-10) — not done in this plan, which was backend-only per its own `<files_modified>` list; flagging for the phase's frontend/docs plan.
- No live Pi contact was required or performed to build or verify this plan, per hard constraint 6 (do not deploy/touch the Pi/push). Local commits only: `509b0d0`, `e8929e8`, `159cd7a`, `e4096c5`, `8fa8cb9`.

## Self-Check: PASSED

- FOUND: internal/domain/library/types.go (Badge/DiscCount/Disc fields present)
- FOUND: internal/infra/mpd/client.go (AlbumDetails.Disc present)
- FOUND: internal/infra/mpd/client_albumdetails_test.go (TestGroupAlbumDetails_DiscFirstTrackWins present)
- FOUND: internal/transport/socketio/library_mpd_adapter.go (Disc: d.Disc present)
- FOUND: internal/domain/library/service.go (foldersFromAlbumDetails/albumFromGroup/applyDupeBadges present)
- FOUND: internal/domain/library/service_test.go (7 new tests present)
- FOUND: 509b0d0 (Task 1 feat commit)
- FOUND: e8929e8 (Task 2 RED commit)
- FOUND: 159cd7a (Task 2 GREEN commit)
- FOUND: e4096c5 (Task 3 RED commit)
- FOUND: 8fa8cb9 (Task 3 GREEN commit)

---
*Phase: 03-browse-experience*
*Completed: 2026-08-12*
