---
phase: 03-browse-experience
plan: 05
subsystem: api
tags: [go, mpd, gompd, socket.io, tdd]

# Dependency graph
requires:
  - phase: 03-browse-experience
    provides: "03-03's GetArtistAlbums grouping/filtering/pagination wiring, extended here"
provides:
  - "FindTracksByArtist on the MPD client + LibraryMPDAdapter (Artist-tag substring search, independent of AlbumArtist grouping)"
  - "ArtistAlbumsResponse.LooseTracks — additive fallback field, populated only when Albums resolves to zero"
  - "Service.trackFromRawSong — shared raw-song-to-Track builder, used by GetAlbumTracks and the new fallback"
affects: [browse-experience, ios-remote, lcd-kiosk]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Defensive zero-result fallback proven by synthetic MockMPDClient fixture, not a live acceptance step, when the live library has no reproducing case (D-09)"
    - "Shared raw-song->Track builder (trackFromRawSong) to prevent parsing drift between call sites that both consume MPD's map[string]string song shape"

key-files:
  created: []
  modified:
    - internal/infra/mpd/client.go
    - internal/infra/mpd/client_test.go
    - internal/transport/socketio/library_mpd_adapter.go
    - internal/domain/library/types.go
    - internal/domain/library/service.go
    - internal/domain/library/service_test.go

key-decisions:
  - "LooseTracks is additive and omitempty — the wire shape for every artist with albums (all real artists today) is byte-identical to before this plan"
  - "Fallback query uses MPD's Artist tag via `search` (substring), then filters with strings.EqualFold for an exact match — mirrors the AlbumArtist filtering pattern GetArtistAlbums already used, and closes the T-03-08 over-matching threat"
  - "Extracted trackFromRawSong out of GetAlbumTracks instead of duplicating its ~50 lines of title-fallback/duration/disc-parsing logic in the fallback path"

requirements-completed: [ARTIST-04, BROWSE-04]

# Metrics
duration: ~35min (this session; resumed after a prior session was cut off by an API session limit mid-Task-2)
completed: 2026-08-12
---

# Phase 3 Plan 5: ARTIST-04/BROWSE-04 loose-track fallback Summary

**GetArtistAlbums now returns a playable `LooseTracks` list (MPD Artist-tag search, exact-match filtered) instead of a silent empty response whenever the AlbumArtist-grouped album list comes back empty — proven by a synthetic MockMPDClient fixture since no live artist reproduces the zero-album case.**

## Performance

- **Duration:** ~35 min this session (RESUMED — a prior executor was cut off by an API session limit partway through Task 2; Task 1 and the `LooseTracks` field on `ArtistAlbumsResponse` were already done/committed before this session started)
- **Completed:** 2026-08-12
- **Tasks:** 2/2 complete (Task 1 was already committed on resume; this session completed Task 2 under TDD: RED commit then GREEN commit)
- **Files modified:** 6 total across both tasks (3 in Task 1, 3 in Task 2 — `types.go` touched in both)

## Accomplishments
- `Client.FindTracksByArtist` (MPD-layer, `search artist <name>`) and `LibraryMPDAdapter.FindTracksByArtist` — already done pre-session (commit `1ac5e21`)
- `ArtistAlbumsResponse.LooseTracks []Track` — additive, `omitempty`, doc-commented — the field itself was an uncommitted WIP edit at session start; committed as part of the RED test commit this session
- `Service.trackFromRawSong` — extracted from `GetAlbumTracks`'s inline per-song loop into a single shared helper (title-fallback, track/disc-number parsing, duration parsing, `/albumart` path, source classification, resource-fork exclusion), now called from both `GetAlbumTracks` and the new fallback
- `GetArtistAlbums` fallback: when the grouped/filtered `albums` slice is empty, calls `FindTracksByArtist`, filters to an exact `strings.EqualFold` Artist match, maps through `trackFromRawSong`, sorts by (Album, Disc, TrackNumber, Title), and sets `resp.LooseTracks`
- Synthetic zero-album fixture test (`TestService_GetArtistAlbums_LooseTracksFallback_SyntheticZeroAlbumArtist`) and non-regression test (`TestService_GetArtistAlbums_WithAlbums_LooseTracksNeverPopulated`) both pass

## Task Commits

Task 1 was completed and committed in the prior (interrupted) session:

1. **Task 1: Add MPD-layer FindTracksByArtist** - `1ac5e21` (feat) — *not touched this session, verified present*

This session (resumed at Task 2):

2. **Task 2 (RED): synthetic zero-album fixture test, ArtistAlbumsResponse.LooseTracks field** - `70d216f` (test)
3. **Task 2 (GREEN): wire the fallback into GetArtistAlbums** - `e3ce2a4` (feat)

**Plan metadata:** commit pending — see final-commit step below.

## Files Created/Modified
- `internal/infra/mpd/client.go` — `FindTracksByArtist(artist string) ([]mpd.Attrs, error)` (Task 1, pre-session)
- `internal/infra/mpd/client_test.go` — `TestClientFindTracksByArtistWithoutConnect` (Task 1, pre-session)
- `internal/transport/socketio/library_mpd_adapter.go` — `LibraryMPDAdapter.FindTracksByArtist` (Task 1, pre-session)
- `internal/domain/library/types.go` — `ArtistAlbumsResponse.LooseTracks []Track \`json:"looseTracks,omitempty"\`` (this session, committed in `70d216f`)
- `internal/domain/library/service.go` — `MPDClient.FindTracksByArtist` interface method, new `Service.trackFromRawSong` helper, `GetAlbumTracks` refactored to call it, `GetArtistAlbums` fallback wiring (this session, `e3ce2a4`)
- `internal/domain/library/service_test.go` — `MockMPDClient.FindTracksByArtist`, `TestService_GetArtistAlbums_LooseTracksFallback_SyntheticZeroAlbumArtist`, `TestService_GetArtistAlbums_WithAlbums_LooseTracksNeverPopulated` (this session, `70d216f`)

## Decisions Made
- Followed the plan's extraction instruction literally: `trackFromRawSong` has one definition (`service.go:599`) and exactly two call sites (`GetAlbumTracks` at line 705, the fallback at line 565) — grep-verified.
- Sort key for `LooseTracks` is `(Album, Disc, TrackNumber, Title)` per the plan's `<action>` text, even though the synthetic fixture's songs all share an empty `Album` and `Disc 0` — this still exercises the `TrackNumber` tier of the comparator (verified: "First Loose Track" (Track 1) sorts before "Second Loose Track" (Track 2) despite being inserted in reverse order in the fixture).
- Errors from `FindTracksByArtist` are logged at Debug level and swallowed (matching the existing `GetAlbumDetails`/`GetAlbumTracks` error-handling convention in this file) rather than surfaced on `ArtistAlbumsResponse` — an MPD error on the fallback path degrades to "no loose tracks" rather than blowing up a response that already has (correctly empty) `Albums`.

## Deviations from Plan

None — plan executed as written. The field `ArtistAlbumsResponse.LooseTracks` was already present as an uncommitted WIP edit at session start (per the resume brief); it was verified correct against the plan's `<interfaces>` spec and folded into this session's first commit rather than being redone.

## Issues Encountered
None. Build, `go vet` (scoped to `internal/domain/library/...`, `internal/infra/mpd/...`, `internal/transport/socketio/...`), and `golangci-lint run` (same scope) are all clean on every file this plan touched. `golangci-lint` does report 23 pre-existing findings (21 errcheck, 1 staticcheck, 1 unused) — all in files/lines this plan never touched (`client_test.go` test helpers, several `socketio/*_handlers.go` files, `server.go`, `server_test.go`, `spectrum_ingest.go`) — left untouched per hard constraint 8 / scope boundary.

## Verification Evidence

```
$ go test ./internal/domain/library/... -v -run TestService_GetArtistAlbums
=== RUN   TestService_GetArtistAlbums_MahlerShaped_GroupsToOneAlbumWithDiscCount
--- PASS: TestService_GetArtistAlbums_MahlerShaped_GroupsToOneAlbumWithDiscCount (0.00s)
=== RUN   TestService_GetArtistAlbums_KindOfBlueShaped_StaysSeparate
--- PASS: TestService_GetArtistAlbums_KindOfBlueShaped_StaysSeparate (0.00s)
=== RUN   TestService_GetArtistAlbums_LooseTracksFallback_SyntheticZeroAlbumArtist
--- PASS: TestService_GetArtistAlbums_LooseTracksFallback_SyntheticZeroAlbumArtist (0.00s)
=== RUN   TestService_GetArtistAlbums_WithAlbums_LooseTracksNeverPopulated
--- PASS: TestService_GetArtistAlbums_WithAlbums_LooseTracksNeverPopulated (0.00s)
=== RUN   TestService_GetArtistAlbums_Empty
--- PASS: TestService_GetArtistAlbums_Empty (0.00s)
=== RUN   TestService_GetArtistAlbums_WithAlbums
--- PASS: TestService_GetArtistAlbums_WithAlbums (0.00s)
=== RUN   TestService_GetArtistAlbums_PopulatesFullFields
--- PASS: TestService_GetArtistAlbums_PopulatesFullFields (0.00s)
PASS
ok  	github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library	0.338s
```

Before the GREEN commit (RED phase, confirming the test actually exercises new behavior):
```
=== RUN   TestService_GetArtistAlbums_LooseTracksFallback_SyntheticZeroAlbumArtist
    service_test.go:761: Expected 2 playable LooseTracks (resource-fork excluded), got 0: []
--- FAIL: TestService_GetArtistAlbums_LooseTracksFallback_SyntheticZeroAlbumArtist (0.00s)
```

Full suite + build:
```
$ go build ./...
(clean, exit 0)
$ go test ./... | grep -c "^ok"
35
$ go vet ./internal/domain/library/... ./internal/infra/mpd/... ./internal/transport/socketio/...
(clean, exit 0)
```

35 packages passing — same count as the pre-session baseline noted in the resume brief.

## TDD Gate Compliance

- RED gate: `70d216f` (`test(03-05): add failing synthetic zero-album LooseTracks fallback test`) — verified failing before GREEN via `go test -run TestService_GetArtistAlbums_LooseTracksFallback_SyntheticZeroAlbumArtist`.
- GREEN gate: `e3ce2a4` (`feat(03-05): wire LooseTracks fallback into GetArtistAlbums`) — verified passing.
- No REFACTOR-only commit was needed beyond the extraction, which was done as part of the GREEN commit (the plan's `<action>` explicitly scopes the `trackFromRawSong` extraction to the same task as the wiring, not a separate refactor step).

## User Setup Required
None — no external service configuration required.

## Next Phase Readiness
`Service.GetArtistAlbums` no longer has a silent dead-end response shape. `LooseTracks` is additive and `omitempty`, so no client (iOS, LCD kiosk, Volumio Connect v2) needs any change to keep working exactly as before; clients that want to render the fallback UI can start consuming `looseTracks` whenever convenient. No blockers for Plan 03-06 (deployment).

## Self-Check: PASSED

- FOUND: internal/infra/mpd/client.go
- FOUND: internal/infra/mpd/client_test.go
- FOUND: internal/transport/socketio/library_mpd_adapter.go
- FOUND: internal/domain/library/types.go
- FOUND: internal/domain/library/service.go
- FOUND: internal/domain/library/service_test.go
- FOUND: .planning/phases/03-browse-experience/03-05-SUMMARY.md
- FOUND commit: 1ac5e21 (Task 1, pre-session)
- FOUND commit: 70d216f (Task 2 RED, this session)
- FOUND commit: e3ce2a4 (Task 2 GREEN, this session)
- Confirmed `LooseTracks` field present in types.go with `omitempty` json tag
- Confirmed `trackFromRawSong` has exactly 1 definition + 2 call sites (grep)
