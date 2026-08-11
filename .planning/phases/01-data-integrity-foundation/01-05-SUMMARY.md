---
phase: 01-data-integrity-foundation
plan: 05
subsystem: infra
tags: [pi, ssh, mpd, exfat, appledouble, verification, socket.io, sqlite]

# Dependency graph
requires:
  - phase: 01-data-integrity-foundation
    plan: 01-02
    provides: "Hardened GetAlbumDetails/groupAlbumDetails + skippedCount tracking, unit-tested against synthetic mpd.Attrs"
  - phase: 01-data-integrity-foundation
    plan: 01-04
    provides: "deploy/verify-data-integrity.sh (gates I1/I2) and the trap-based remount-rw/ro pattern"
provides:
  - "deploy/verify-data-integrity.sh gate I3: a live, reusable ._-recurrence regression check comparing the FULL per-album track_count + per-artist album_count snapshot from the deployed backend's cache DB before/after a synthetic ._ file appears and after cleanup"
  - "deploy/rebuild-cache.py: standalone Socket.IO client to trigger + await a library cache rebuild (no HTTP endpoint exists for this), reusable by future plans/scripts"
  - "ROADMAP Phase 1 success criterion 4 verified live against the deployed, hardened binary (not just unit tests)"
affects: [01-06, 01-07]

# Tech tracking
tech-stack:
  added: [python-socketio (already present on the Pi's python3, used by the new rebuild-cache.py client)]
  patterns:
    - "Full-table snapshot diff (all album track_count + all artist album_count, not just the one album under test) for a regression gate — no per-album ID bookkeeping needed, and catches a regression anywhere in the library"
    - "Socket.IO-events-only backend action (library:cache:rebuild / library:cache:status) automated headlessly via a python-socketio client script, since no HTTP endpoint exists and the kiosk Chromium page is a production build with no dev-server module paths to dynamic-import against"
    - "check()/FAILED/exit-code PASS/FAIL gate idiom (from 01-04) extended with a third gate in the same script, script-relative companion script resolution via $(dirname \"${BASH_SOURCE[0]}\")"

key-files:
  created:
    - deploy/rebuild-cache.py
  modified:
    - deploy/verify-data-integrity.sh

key-decisions:
  - "I3 diffs the ENTIRE albums.track_count + artists.album_count table, not just the one album/artist the throwaway file was written under — this makes the gate parameterization-light (only needs TEST_ALBUM_DIR to exist, not a specific album ID) and catches a regression anywhere in the cache, matching the ROADMAP criterion's wording ('any album... any artist')."
  - "Cache rebuild triggering required a new companion script (deploy/rebuild-cache.py) because the backend exposes NO HTTP endpoint for library:cache:rebuild — it's Socket.IO-only, and the kiosk Chromium page (confirmed via live CDP introspection) serves a production build with no /src/lib/services/socket.ts dev-server path to dynamic-import against, ruling out the CDP-console-eval approach used in earlier sessions' verification work."
  - "Rebuild completion is confirmed by lastUpdated advancing past its pre-rebuild value while isBuilding is false, not just isBuilding transitioning to false — a no-op rebuild was observed live to complete in under 1.5s (faster than the poll interval), so isBuilding-only polling would have falsely reported 'rebuild never happened'."

patterns-established:
  - "Future Socket.IO-only backend actions (cache rebuild, any future admin action with no HTTP route) can reuse deploy/rebuild-cache.py's connect/emit/poll/disconnect pattern rather than reaching for CDP browser automation, which turned out to be a dead end against the production kiosk build."

requirements-completed: [DATA-04]

# Metrics
duration: ~15min (this session: Task 1b approval confirmation + Task 2 execution; Task 1's deploy/baseline capture completed in a prior session)
completed: 2026-08-11
---

# Phase 01 Plan 05: Live `._`-recurrence regression (DATA-04) Summary

**Live-verified against the deployed, hardened backend on the Pi: writing a throwaway `._`-prefixed file into the SSD music library and rescanning changes no album's `track_count` and no artist's `album_count` — encoded as a new, reusable gate I3 in `deploy/verify-data-integrity.sh`, backed by a new `deploy/rebuild-cache.py` Socket.IO client since the backend has no HTTP cache-rebuild endpoint.**

## Performance

- **Duration:** ~15 min (this session)
- **Completed:** 2026-08-11T10:31:43Z
- **Tasks:** 2/2 completed this session (Task 1b checkpoint approval + Task 2); Task 1 (build/deploy/baseline) was completed and approved in a prior session per the resume context
- **Files modified:** 2 (1 new, 1 extended)

## Accomplishments

- Re-confirmed the Task 1 baseline was still intact before touching anything: binary sha256
  `ce2784b7c40aeb8e7ef41d61125597abb864c19f8cf2e5b29680a02e30d698fa` on the Pi still matches the
  local build exactly; `/health` and `/ready` both 200; all previously-recorded album/artist counts
  identical to Task 1's captured baseline.
- Discovered the backend has no HTTP endpoint for triggering a cache rebuild — it's Socket.IO-only
  (`library:cache:rebuild` / `library:cache:status`) — and that the kiosk Chromium page serves a
  production build (no `/src/lib/services/socket.ts` module path for CDP dynamic-import, which had
  worked in earlier dev-server-backed sessions). Built `deploy/rebuild-cache.py`, a standalone
  `python-socketio` client that connects directly to the backend, emits the rebuild event, and polls
  status until `isBuilding` is false AND `lastUpdated` has advanced past its pre-rebuild value
  (guards against missing a sub-1.5s no-op rebuild).
- Ran the actual live regression by hand first (to prove the sequence before encoding it): remounted
  `/mnt/ssd` `rw`, wrote `._verify-regression-test.flac` (4096 random bytes) into
  `/mnt/ssd/Music/Jacob Collier/Djesse Vol. 4 (Deluxe)`, remounted `ro`, ran `mpc update` (MPD's
  `Songs:` count rose 814→815 and 1 `/\._` entry appeared, confirming MPD DID index the junk file),
  triggered a cache rebuild, and confirmed all 4 tracked albums' `track_count` (21/43/22/24) and all
  3 tracked artists' `album_count` (Jacob Collier 3, Queens Of The Stone Age 9, Various Artists 2)
  were byte-identical to baseline with the junk file present — DATA-04's property held.
- Cleaned up: remounted `rw`, `sudo rm -f` the throwaway file, remounted `ro`, `mpc update` (Songs
  count back to 814, 0 `/\._` entries), rebuilt the cache again, and re-verified the same counts
  matched the ORIGINAL baseline once more (proves cleanup didn't itself perturb anything).
- Encoded the exact same sequence as gate `I3` in `deploy/verify-data-integrity.sh`, generalized to
  diff the FULL `albums.track_count` + `artists.album_count` snapshot (not just the 4/3 spot-checked
  above) so the gate is reusable without needing to know specific album IDs in advance, and
  parameterized via `TEST_ALBUM_DIR`/`LIBRARY_DB`/`BACKEND_URL`/`REBUILD_TIMEOUT_S`/`MOUNT_POINT` env
  vars with sane defaults.
- Ran the full, updated `deploy/verify-data-integrity.sh` end-to-end on the Pi (as the script itself,
  not the manual steps): all three gates (I1, I2, I3) PASS.
- Final state confirmed: mount `ro`, throwaway file gone (`find` returns nothing), 803 real audio
  files unchanged, 0 `._` entries anywhere (disk + MPD index), `/health`/`/ready` both 200, binary
  sha256 unchanged.
- Cleaned up ad-hoc CDP-exploration scratch files left on the Pi's home directory during the dead-end
  CDP approach (`cdp_eval.py`, `cdp_eval2.py`, `check_socket.js`, and the pre-final draft
  `rebuild_cache.py`) — none of these are repo artifacts, only the final `deploy/rebuild-cache.py`
  (deployed to `~/stellar-backend/deploy/`) remains.

## Task Commits

1. **Task 1: Build and deploy the hardened backend; snapshot baseline counts** — no commit
   (`files_modified: none` per plan frontmatter; Pi-side deploy action only). Completed and approved
   in a prior session per the resume context; re-verified identical in this session before proceeding.
2. **Task 1b: Confirm writing the throwaway `._` regression-test file** — checkpoint approval gate,
   no repo files touched. User explicitly approved before this session began (per
   `<user_approval>` in the resume context).
3. **Task 2: Run the live regression and extend verify-data-integrity.sh** — `cd7a2bc` (feat)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified

- `deploy/rebuild-cache.py` — new. Socket.IO client (`python-socketio`) that connects to the backend,
  emits `library:cache:rebuild`, and polls `library:cache:status` until `pushLibraryCacheStatus`
  reports `isBuilding: false` with an advanced `lastUpdated` timestamp. `python3 rebuild-cache.py
  [URL] [TIMEOUT_SECONDS]`, exits 0 on confirmed completion, 1 on timeout.
- `deploy/verify-data-integrity.sh` — extended with gate `I3`. Writes a throwaway `._`-prefixed file
  into a configurable album folder (`TEST_ALBUM_DIR`, default: an existing Jacob Collier album under
  `MUSIC_ROOT`), rescans MPD, triggers a live cache rebuild via the new companion script, diffs the
  full `albums.track_count` + `artists.album_count` snapshot from `LIBRARY_DB` before vs. after, then
  repeats cleanup + a second diff against the ORIGINAL baseline. Unconditional `trap ... EXIT` restores
  the mount to `ro` regardless of outcome, mirroring Plan 01-04's pattern. `I1`/`I2` from Plan 01-04
  are unchanged.

## Decisions Made

See `key-decisions` in frontmatter:
1. I3 diffs the entire albums/artists table rather than a fixed set of pre-selected IDs.
2. Built a new `python-socketio`-based companion script because no HTTP cache-rebuild endpoint exists
   and the CDP-dynamic-import approach used in earlier sessions doesn't work against the kiosk's
   production build.
3. Rebuild completion is gated on `lastUpdated` advancing, not just `isBuilding` going false, after
   observing a sub-1.5s no-op rebuild live.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - blocking issue] Plan's suggested `window.libraryActions.rebuildCache()` CDP approach doesn't work — no HTTP endpoint either, needed a new companion script**
- **Found during:** Task 2, first attempt to trigger a cache rebuild for the regression test.
- **Issue:** The plan's action text offers two options for triggering a rebuild: "emit
  `library:cache:rebuild`" or "use the frontend console helper `window.libraryActions.rebuildCache()`
  ... over the kiosk CDP connection." Live investigation found: (a) `window.libraryActions` is
  `undefined` on the kiosk page; (b) the CDP dynamic-import pattern used in earlier sessions
  (`await import('/src/lib/services/socket.ts')`) fails with "Failed to fetch dynamically imported
  module" because the kiosk serves a production build, not the dev server, so that source path
  doesn't exist as a fetchable module at runtime; (c) grepping the backend source confirmed there is
  no HTTP route for cache rebuild at all — it's Socket.IO-event-only.
- **Fix:** Built `deploy/rebuild-cache.py`, a `python-socketio` client (the package was already
  present on the Pi's python3) that connects directly to `http://localhost:3000`, emits
  `library:cache:rebuild`, and polls `library:cache:status` until a confirmed completed rebuild.
  Verified reliable across 4 live invocations in this session (including the two inside the deployed
  I3 gate's own run).
- **Files modified:** `deploy/rebuild-cache.py` (new), referenced from `deploy/verify-data-integrity.sh`'s I3 gate.
- **Commit:** `cd7a2bc`

**Total deviations:** 1, Rule 3, self-resolved with no data loss and no plan-scope change — the plan's
stated *goal* (trigger + await a cache rebuild) was met, only the specific mechanism it suggested
didn't exist in this deployment's actual topology.

## Issues Encountered

None beyond the one auto-fixed deviation above. The dead-end CDP exploration left temporary files on
the Pi's home directory (`cdp_eval.py`, `cdp_eval2.py`, `check_socket.js`, an earlier draft of
`rebuild_cache.py`); all four were removed at the end of this session and are not part of the repo or
the deployed `~/stellar-backend/deploy/` directory.

## User Setup Required

None. This plan required no new user action beyond the Task 1b checkpoint approval already recorded
in the resume context (writing/deleting the throwaway `._` test file).

## Next Phase Readiness

- `DATA-04` is now marked complete in `.planning/REQUIREMENTS.md`.
- ROADMAP Phase 1 success criterion 4 is met and verified live: a `._` file appearing on the SSD and a
  subsequent rescan changed no album's `track_count` and no artist's `album_count`, verified against
  the deployed, hardened backend's own cached representation — not just Plan 01-01/01-02's unit tests.
- `deploy/verify-data-integrity.sh` now has all three gates (I1/I2/I3) and is a durable, reusable
  regression suite: `bash ~/stellar-backend/deploy/verify-data-integrity.sh` re-runs everything
  end-to-end at any time, without needing to reconstruct this plan's ad-hoc verification commands.
- `deploy/rebuild-cache.py` is available as a general-purpose Socket.IO cache-rebuild trigger for any
  future plan or script that needs to programmatically rebuild the library cache without a browser.
- Library end state: 803 real audio files, 0 `._` files (disk + MPD index), mount `ro`, binary sha256
  unchanged, `/health`/`/ready` both 200 — identical to the pre-plan baseline.
- Plans 01-06/01-07 are unblocked; neither depends on repo-side artifacts from this plan beyond the
  extended verify script and the new rebuild-cache.py being available as patterns to follow.

---
*Phase: 01-data-integrity-foundation*
*Completed: 2026-08-11*

## Self-Check: PASSED

- `deploy/rebuild-cache.py` exists in the repo at the path claimed: verified via
  `git show cd7a2bc --stat` (file created, mode 100755).
- `deploy/verify-data-integrity.sh` extended as claimed: verified via `git show cd7a2bc --stat`
  (194 insertions across both files).
- Commit `cd7a2bc` exists: verified via `git log --oneline -1` showing it as HEAD at time of writing.
- All quantitative claims in "Accomplishments" are copy-pasted live command output from this session
  (baseline sqlite queries, `mpc stats` before/after, final `find`/`mount`/`curl`/`sha256sum` checks,
  and the deployed script's own PASS output for I1/I2/I3), not assertions.
