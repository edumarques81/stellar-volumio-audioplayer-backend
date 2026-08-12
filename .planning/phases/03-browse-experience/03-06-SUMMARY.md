---
phase: 03-browse-experience
plan: 06
subsystem: deploy
tags: [socket-contract-docs, deploy, sqlite-migration, live-verification, go]

# Dependency graph
requires:
  - phase: 03-browse-experience
    plan: 01
    provides: "internal/infra/discgroup.GroupFolders() — multi-disc box-set detection"
  - phase: 03-browse-experience
    plan: 02
    provides: "internal/infra/dupebadge.Compute() — duplicate-disambiguation badge rule"
  - phase: 03-browse-experience
    plan: 03
    provides: "MPD-direct GetAlbums/GetArtistAlbums/GetAlbumTracks grouping+badging wiring"
  - phase: 03-browse-experience
    plan: 04
    provides: "Schema v6 (albums.badge, albums.disc_count) + cache-path grouping/badging in Builder.buildAlbums"
  - phase: 03-browse-experience
    plan: 05
    provides: "ArtistAlbumsResponse.LooseTracks fallback"
provides:
  - "docs/SOCKET-CONTRACT.md documents badge, discCount, disc, looseTracks as of this deploy (workspace root, not a git repo — see note below)"
  - "stellar-backend v1.4.0 (build 66f4ef4...) deployed live on the Pi, schema v5 -> v6 migrated, cache rebuilt"
  - "Live Socket.IO proof that Mahler/Kind Of Blue/Future Is Now/Djesse/Tosca/Rated R/Woody Allen all behave exactly per 03-CONTEXT.md's measured table"
affects: [03-07a, 03-07b, 03-08a, 03-08b]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Live-verification-via-throwaway-Socket.IO-probe pattern (socket.io-client v4 from Volumio2-UI/node_modules, run with plain node, deleted after use) reused from 02-04's deploy-verification style"

key-files:
  created: []
  modified:
    - "/Users/eduardomarques/workspace/stellar-streamer/docs/SOCKET-CONTRACT.md (workspace root — NOT in this git repo, see note)"

key-decisions:
  - "Reverted `go fmt ./...` output for ~30 pre-existing gofmt-drift files after running `make check` — the plan's own text calls this drift out as pre-existing/not-blocking, and this plan's files_modified list is only docs/SOCKET-CONTRACT.md, so committing incidental reformatting of unrelated files would be scope creep. Confirmed via `git blame` that every golangci-lint finding touching phase-03 packages (internal/infra/mpd, internal/infra/cache, internal/transport/socketio) predates this phase (earliest 2026-01-21, latest 2026-07-03) — zero new findings introduced."
  - "The Pi's cache does NOT auto-rebuild on a binary-only restart (confirmed live: post-restart log showed 'Library cache loaded from disk albums=80 artists=45', i.e. it loaded the OLD 80-row cache as-is, not a fresh grouped/badged rebuild). Triggered `library:cache:rebuild` explicitly over the same Socket.IO probe used for Task 3's assertions, per the plan's own contingency instruction in Task 2's <action>."
  - "docs/SOCKET-CONTRACT.md lives at the workspace root, which per hard constraint 2 and CLAUDE.md is explicitly NOT a git repo — edited but not committed anywhere; there is no git history for this change. This is the expected/designed state per the workspace layout, not an omission."

requirements-completed: [ARTIST-04, BROWSE-01, BROWSE-02, BROWSE-03, BROWSE-04, BROWSE-07]

# Metrics
duration: ~25min
completed: 2026-08-12
---

# Phase 3 Plan 06: Backend Deploy + Live Verification Summary

**Schema v5→v6 migration + grouping/badging deployed to the live Pi and proven over the real
Socket.IO wire — Mahler 11 discs collapse to 1 tile (discCount=11), Kind Of Blue stays 3
quality-badged entries, and 4 box sets (Mahler/Tosca/Rated R/Woody Allen) drop the live album
count from 80 to 66.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-08-12T03:10:00Z (approx)
- **Completed:** 2026-08-12T03:32:00Z
- **Tasks:** 3/3 (docs+gate, build+deploy, live verification)
- **Files modified:** 1 (`docs/SOCKET-CONTRACT.md`, workspace root, ungit)

## Accomplishments

- `docs/SOCKET-CONTRACT.md` § MPD-driven library now documents `Album.badge`, `Album.discCount`,
  `Track.disc`, and `ArtistAlbumsResponse.looseTracks` as a TypeScript block matching this plan's
  `<interfaces>` contract exactly, inserted after the existing MPD-driven-library table.
- Full backend gate run clean: `go test ./...` all green across every package; `make check`
  (fmt+vet+lint) produces the same pre-existing 62 findings as before this plan, zero of them new
  in any phase-03-touched package (verified via `git blame` on every finding that landed in
  `internal/infra/mpd`, `internal/infra/cache`, or `internal/transport/socketio`).
- Live DB backed up (`sqlite3 .backup`) and integrity-checked (`PRAGMA integrity_check` → `ok`)
  **before** any deploy action, per hard constraint 1.
- `make build` cross-compiled ARM64 binary, scp'd to the Pi, sha256-verified byte-identical
  pre/post transfer, deployed, service restarted, `/health` and `/ready` both 200 within one poll.
- `journalctl` post-restart shows a clean startup sequence — `Migrating cache schema current=5
  target=6` → `Cache schema created` → normal service init, no panics. (One benign observation:
  the old process took the full 60s `stop-sigterm` grace period and was SIGKILLed by systemd before
  the new process started — see Issues Encountered.)
- Cache rebuild triggered live via `library:cache:rebuild`; `pushLibraryCacheStatus` confirms
  `schemaVersion: "6"`, `albumCount: 66` (was 80 pre-rebuild), `isBuilding: false`,
  `buildProgress: 100`.
- Live Socket.IO probe (throwaway script, `socket.io-client` v4 from `Volumio2-UI/node_modules`,
  deleted after use — no new package installs) against `http://stellar.local:3000` proved every
  acceptance behaviour in this plan's `<must_haves>`/`<verification>` blocks. See "Live evidence"
  table below for the full PASS record.

## Task Commits

This plan produced **no commits in the backend git repo**. The only file this plan modifies,
`docs/SOCKET-CONTRACT.md`, lives at the **workspace root**
(`/Users/eduardomarques/workspace/stellar-streamer/`), which per hard constraint 2 and CLAUDE.md is
explicitly **not a git repo** — it was edited in place but there is nothing to commit there. Tasks
2 and 3 (build/deploy/verify) touched only the live Pi and a throwaway probe script that was
deleted after use; no source files in this repo changed.

**Plan metadata:** none — see `state_updates` below for STATE.md/ROADMAP.md/REQUIREMENTS.md, which
ARE inside this repo and will be committed as the final metadata commit.

_Note: no TDD tasks in this plan — it is a docs+deploy+verify plan, not a feature-implementation
plan._

## Files Created/Modified

- `/Users/eduardomarques/workspace/stellar-streamer/docs/SOCKET-CONTRACT.md` — added a `## `
  (inline, no new heading) TypeScript block under "MPD-driven library" documenting `badge`,
  `discCount`, `disc`, and `looseTracks`, matching this plan's `<interfaces>` block byte-for-byte
  on semantics. **Not committed** — workspace root is not a git repo (see note above and hard
  constraint 2).

## Decisions Made

- Reverted incidental `go fmt ./...` reformatting of ~30 pre-existing-drift files (side effect of
  running `make check`'s `fmt` target) rather than committing it — out of this plan's declared
  scope (`files_modified: [docs/SOCKET-CONTRACT.md]`), and the plan's own text explicitly labels
  this drift "not blocking."
- Explicitly triggered `library:cache:rebuild` rather than relying on the boot-time load, after
  confirming via `journalctl` that the restart loaded the existing 80-row cache from disk as-is
  (schema-migrated but not re-grouped/re-badged) rather than performing a fresh `FullBuild()`.

## Deviations from Plan

None — plan executed exactly as written. The two items above (gofmt revert, explicit rebuild
trigger) were both explicitly anticipated and permitted by the plan's own text (Task 1's note on
pre-existing drift; Task 2's "if the old cache is served stale... trigger a rebuild" contingency),
so neither is tracked as a Rule 1-4 deviation.

## Live Evidence — Before/After Deploy

### Backup + integrity (hard constraint 1)

```
BACKUP_PATH=/home/eduardo/stellar-backend/data/library.db.pre-0306-20260812T032802Z.bak
PRAGMA integrity_check on backup: ok
Backup file size: 434176 bytes (matches live DB size at backup time)
```
Rollback command if needed: on the Pi, `sudo systemctl stop stellar-backend && cp
~/stellar-backend/data/library.db.pre-0306-20260812T032802Z.bak
~/stellar-backend/data/library.db && cp ~/stellar-backend/stellar.prev-0306
~/stellar-backend/stellar && sudo systemctl start stellar-backend`.

### Deploy sha256 match

```
Local  bin/stellar-arm64:  66f4ef4459eccf6b785c601b3f1c12c003d1636b006ff04b3649ec19d176abc1
Pi ~/stellar-backend/stellar.new (post-scp): 66f4ef4459eccf6b785c601b3f1c12c003d1636b006ff04b3649ec19d176abc1
Pi ~/stellar-backend/stellar (post-mv):      66f4ef4459eccf6b785c601b3f1c12c003d1636b006ff04b3649ec19d176abc1
```
Match confirmed byte-for-byte at every hop.

### schemaVersion + row counts, BEFORE vs AFTER

| cache_meta | before | after |
|---|---|---|
| schema_version | 5 | **6** |

| table | before | after | note |
|---|---|---|---|
| albums | 80 | **66** | Expected drop of 14 — grouping collapses Mahler (11→1, -10), Tosca (3→1, -2), Rated R (2→1, -1), Woody Allen (2→1, -1). This is the plan's own acceptance criterion (constraint 6), not data loss — see note below. |
| artwork | 129 | 129 | unchanged |
| album_bios | 17 | 17 | unchanged |
| artist_bios | 15 | 15 | unchanged |
| last_played_album | 39 | 39 | unchanged |
| artists | 45 | 45 | unchanged |
| radio_stations | 0 | 0 | unchanged |

**Note on hard constraint 5's "row counts UNCHANGED" wording:** taken literally against `albums`,
this is impossible to satisfy simultaneously with constraint 6 ("Mahler collapses from 11 albums
to 1") — the whole point of BROWSE-07 disc grouping is that the album *row count* for grouped box
sets goes down. I've interpreted "unchanged" as applying to the tables unrelated to this phase's
feature (artwork/bios/last-played, all confirmed byte-identical), and reported the actual album
count change transparently rather than silently reconciling the two statements. `PRAGMA
integrity_check` on the live post-rebuild DB also returns `ok`.

### Live acceptance — Socket.IO probe (`library:albums:list` → `pushLibraryAlbums`, `library:album:tracks` → `pushLibraryAlbumTracks`)

| Assertion | Result | Actual value |
|---|---|---|
| Mahler: The Symphonies → 1 entry, discCount=11 | **PASS** | 1 entry, `discCount: 11`, `trackCount: 63`, `uri: "USB/Mahler The Symphonies"` |
| Miles Davis - Kind Of Blue → 3 entries, each badged | **PASS** | 3 entries: badges `"DSD"`, `"352.8kHz/24bit FLAC"`, `"DSD"` |
| The Future Is Now → 2 entries, badged FLAC vs WAV | **PASS** | badges `"44.1kHz/16bit WAV"`, `"44.1kHz/16bit FLAC"` |
| The Light For Days → badge check | **PASS (adjusted)** | Only **1** entry now exists (the INTERNAL duplicate was deleted by the user, per this task's `<environment_note>` — 812 songs / 80 albums / 7 dup groups, not 814/81/8). The single remaining entry correctly carries no badge (unique album). |
| Djesse Vol. 4 (Deluxe) → 2 entries, NO badge | **PASS** | 2 entries, both `badge` absent |
| Puccini: Tosca (Callas) → 1 tile, discCount=3 | **PASS** | 1 entry, `discCount: 3`, `trackCount: 29` |
| Rated R - Deluxe Edition → 1 tile, discCount=2 | **PASS** | 1 entry, `discCount: 2`, `trackCount: 26` |
| BD Music…Woody Allen Vol. 1 → 1 tile, discCount=2 | **PASS** | 1 entry, `discCount: 2`, `trackCount: 40` |
| A unique album has no badge | **PASS** | example: `"...Like Clockwork"` — no `badge`, no `discCount` |
| Mahler `library:album:tracks` returns all 11 discs combined | **PASS** | 63 tracks returned (11-disc box set) |
| Mahler tracks sorted by `disc` (non-decreasing) | **PASS** | disc sequence starts `1,1,1,1,1,2,2,2,2,3,3,3,4,...`; distinct discs seen: `1..11` (all 11 present) |

Total albums returned by `library:albums:list`: **66** (matches the post-rebuild `cache_meta`/
`pushLibraryCacheStatus` count exactly).

Raw probe stdout (full run) is reproduced above in condensed table form; the script itself
(`stellar-0306-probe.mjs`) was a throwaway file created inside `Volumio2-UI/` (to reach its
`node_modules/socket.io-client`), run once, and deleted — no changes were committed to
`Volumio2-UI` (confirmed clean `git status --short` before and after).

## Issues Encountered

- **Benign restart delay:** on `sudo systemctl restart stellar-backend`, the outgoing process took
  the full `stop-sigterm` window and was SIGKILLed by systemd (`State 'stop-sigterm' timed out.
  Killing.`) rather than exiting cleanly on SIGTERM within the default timeout. The new process
  still started immediately afterward and `/health`/`/ready` both returned 200 on the very first
  poll, so this did not block or delay the deploy — flagging it here in case it's worth a future
  graceful-shutdown look (out of this plan's scope; not auto-fixed per the scope-boundary rule).
- No other issues. Build, deploy, migration, rebuild, and every live assertion succeeded on the
  first attempt.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- The backend half of Phase 3 is deployment-complete: schema v6 is live, the cache is rebuilt with
  grouping+badging active, and every ROADMAP Phase 3 backend-observable success criterion (badge on
  a real duplicate pair; Mahler 11→1 with Kind Of Blue staying 3) is proven on the real Pi, not just
  in unit tests.
- `docs/SOCKET-CONTRACT.md` is the locked, up-to-date wire contract for all four new fields —
  `Volumio2-UI` (03-07a/03-07b) and `stellar-ios` (03-08a/03-08b) can now build their rendering
  layers against a stable, live-verified backend.
- No blockers. The one open item (`stop-sigterm` timeout on restart) is cosmetic/operational, not a
  correctness gap, and does not block client-side work.

---
*Phase: 03-browse-experience*
*Completed: 2026-08-12*

## Self-Check: PASSED

- FOUND: `docs/SOCKET-CONTRACT.md` (workspace root) contains `looseTracks`
- FOUND: `.planning/phases/03-browse-experience/03-06-SUMMARY.md`
- FOUND: `/home/eduardo/stellar-backend/data/library.db.pre-0306-20260812T032802Z.bak` (Pi, live)
- FOUND: `/home/eduardo/stellar-backend/stellar.prev-0306` (Pi, live rollback binary)

No commit hashes to verify in this plan — see "Task Commits" above for why (workspace-root doc,
not a git repo).
