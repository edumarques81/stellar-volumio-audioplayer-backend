---
phase: 01-data-integrity-foundation
plan: 03
subsystem: infra
tags: [pi, ssh, enumeration, checkpoint, appledouble]

# Dependency graph
requires: []
provides:
  - "~/stellar-backend/data/dotunderscore-manifest.txt on the Pi: NUL-delimited, atomically-written manifest of all 934 ._-prefixed files under /mnt/ssd/Music, sha256 0ecffca71ac507b31576cb9919ebb479692e83e4180c59628ad1ee7301ac4916"
affects: [01-04-delete-dotunderscore]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "find -print0 > .tmp && mv .tmp target for atomic manifest writes over SSH (survives a dropped session mid-write)"

key-files:
  created: []
  modified: []

key-decisions:
  - "Checkpoint NOT auto-approved despite workflow.auto_advance=true in .planning/config.json -- the spawning orchestrator's execution_context explicitly instructed this executor to stop and not self-approve for this specific plan, given the stakes (irreplaceable master files). This plan paused at Task 2 as designed."
  - "Live re-measurement matched the CONTEXT.md baseline exactly (934 total, 699 audio-extension, 803 real audio files, mpc stats 1380 songs) -- no drift between planning-time and execution-time counts."

requirements-completed: []
# DATA-03 is NOT marked complete here -- this plan only enumerates; Plan 01-04 performs the
# deletion and mpc-verify that DATA-03 actually requires. See "Requirements" note below.

# Metrics
duration: ~15min
completed: 2026-08-11
---

# Phase 01 Plan 03: `._` deletion manifest enumeration + human checkpoint Summary

**Read-only `find /mnt/ssd/Music -name '._*' -print0` enumeration on the live Pi produced a 934-entry, atomically-written, NUL-delimited manifest at `~/stellar-backend/data/dotunderscore-manifest.txt`; plan is PAUSED at the Task 2 human-verify checkpoint awaiting explicit user approval before Plan 01-04 deletes anything.**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-08-11T09:35:02Z
- **Tasks:** 1/2 completed (Task 1 done; Task 2 is the checkpoint this plan stops at, by design)
- **Files modified in this repo:** 0 (per plan frontmatter `files_modified: []` -- the manifest is a
  Pi-side artifact, not a repo file)

## Accomplishments

- SSH'd to the Pi (`eduardo@stellar.local`) using the workspace `.env` credential convention
  (password never placed in argv or echoed; sourced via `sshpass -f <(printf ...)`).
- Confirmed `/mnt/ssd` is still mounted read-only (`ro,relatime,uid=111,gid=29,...`), matching the
  planning-session finding recorded in this plan's `<objective>`.
- Ran the enumeration exactly as specified: `find /mnt/ssd/Music -name '._*' -print0` written to a
  `.tmp` path then atomically `mv`'d into place, so a dropped SSH session mid-run could never leave
  a half-written manifest.
- Verified the manifest is non-empty, NUL-delimited, and 100% `._`-prefixed (zero non-`._` entries
  found by an explicit basename-prefix check across all 934 entries -- see "Manifest Integrity"
  below).
- Computed and categorized the full breakdown (see "Live Measurements" below), reconciled every one
  of the 934 entries into a named category with none left unaccounted for.
- Spot-checked the three folders named in the plan's objective as known untagged-song reproducers
  (Karajan Also Sprach Zarathustra, `toe - The Future Is Now`, Sigxer SU-6 test) against the
  manifest.
- Cleaned up the two ad-hoc Python breakdown scripts scp'd to the Pi for computing the categorized
  breakdown (`~/stellar-backend/data/breakdown*.py.tmp`) -- removed after use, leaving only the
  manifest and pre-existing `library.db*`/`cache/` artifacts in `~/stellar-backend/data/`.
- Stopped at the Task 2 checkpoint as required. Did not self-approve.

## Live Measurements (authoritative -- re-measured this session, 2026-08-11 09:35-09:37 UTC)

**Manifest:** `~/stellar-backend/data/dotunderscore-manifest.txt` on the Pi
**sha256:** `0ecffca71ac507b31576cb9919ebb479692e83e4180c59628ad1ee7301ac4916`
**Total entries:** 934 (NUL-delimited)
**Total bytes occupied by these files:** 3,825,664 bytes (~3.65 MiB)
**mpc stats (live, unchanged by this plan):** 1380 songs / 122 artists / 58 albums
**Real (non-`._`) audio files on disk:** 803

All figures match the CONTEXT.md/objective baseline (934 total / 699 audio-extension / 803 real
audio files / 1380 mpc-stats songs) exactly -- **no drift** between the planning-session numbers and
this execution's live re-measurement.

### Category breakdown (all 934 entries accounted for)

| Category | Count | Notes |
|---|---:|---|
| `.flac` (audio) | 566 | would be MPD-indexed junk if not filtered |
| `.dsf` (audio) | 129 | would be MPD-indexed junk if not filtered |
| `.wav` (audio) | 4 | would be MPD-indexed junk if not filtered |
| **Audio subtotal** | **699** | matches the 699 baseline exactly |
| `.jpg` sidecar | 101 | cover-art thumbnail ghosts |
| `.jpeg` sidecar | 1 | |
| **Image subtotal** | **102** | |
| `.pdf` sidecar | 12 | liner-note ghosts |
| `.DS_Store` sidecar (`._.DS_Store`) | 12 | |
| Directory-name ghosts (`._<FolderName>`, sibling directory confirmed to exist) | 108 | |
| Orphan directory-name ghost (sibling directory does **not** exist, e.g. `._Royal Concertgebouw Orchestra, New York Philharmonic, Wiener Philharmonic Orchestra, Leonard Bernstein`) | 1 | still `._`-prefixed AppleDouble junk per D-02's blanket rule; the folder it once shadowed was apparently renamed/consolidated (a `Mahler The Symphonies` folder exists alongside a still-live `Mahler Symphony No 8...` folder) but the ghost itself has no live counterpart to accidentally collide with |
| **GRAND TOTAL** | **934** | matches parsed entry count exactly, zero unaccounted-for entries |

### Manifest integrity check
Explicit per-entry check (`basename` must start with `._`) across all 934 entries: **0 violations**.
Every single entry in the manifest is a `._`-prefixed AppleDouble sidecar file. Nothing in the
manifest could match a real audio file's own name (real audio files never start with `._`).

### Known-reproducer folder spot-check
- **Karajan `Also Sprach Zarathustra`** (`RStrauss Also Sprach Zarathustra Till Eulenspiegel Karajn
  VPO__FLAC_352k-24b/`): 10 `._*.flac` ghosts (one per track, matching the 10-real-FLAC baseline)
  plus 1 directory-name ghost for the folder itself. Present in the manifest as expected.
- **`toe - The Future Is Now`**: two sibling folders exist on disk -- a `-FLAC` variant (3 `._*.flac`
  ghosts + 1 `._cover.jpg` + 1 directory-name ghost in the manifest) and a `-WAV` variant (only a
  directory-name ghost `._toe - The Future Is Now - WAV` in the manifest; the WAV folder's actual
  `.wav`/`cover.jpg` files have **no** `._` shadows -- they were apparently never opened on macOS, or
  their sidecars were already cleaned). Present in the manifest as expected, no anomalies.
- **`Sigxer SU-6 test`**: folder exists on disk with 2 real audio files (`.dff`, `.wav`) but has
  **zero** entries in the manifest -- it has no `._` junk at all. This is expected and not a defect:
  the manifest only contains what actually exists; this folder simply happens to be clean.

## Requirements

`requirements: [DATA-03]` is declared in this plan's frontmatter, but **DATA-03 is NOT marked
complete by this plan**. DATA-03's actual acceptance ("MPD's `mpc stats` matches the real file count,
zero `._` entries remain") is only satisfied after Plan 01-04 performs the deletion and re-verifies.
This plan is enumeration + checkpoint only. The `requirements.mark-complete` state-update step was
**not run** for this reason -- deferring to Plan 01-04 to mark DATA-03 complete once the deletion and
`mpc update` verification actually land.

## Deviations from Plan

### Auto-fixed / process notes (none rise to Rule 1-4 status -- logged for completeness)

**1. [Process note, not a deviation] Checkpoint auto-approval intentionally skipped**
- `.planning/config.json` has `workflow.auto_advance: true`, which per the standard auto-mode
  protocol would auto-approve a plain `checkpoint:human-verify` (this checkpoint's `gate="blocking"`,
  not `gate="blocking-human"`, so the letter of the auto-mode rule would technically permit
  auto-approval).
- This executor's spawning `execution_context` explicitly overrode that: `<checkpoint_handling>`
  instructed "STOP execution there. Do not proceed past it and do not self-approve," given the plan
  deletes files on the user's irreplaceable master music drive. Followed that explicit instruction
  over the general auto-mode default.
- **No files modified, no commit needed for this note** -- documented here for traceability only.

**2. [Rule 3 - minor, self-resolved] Initial categorized-breakdown script needed a rewrite**
- **Found during:** Task 1, computing the per-extension breakdown for the checkpoint.
- **Issue:** A first attempt to run an inline `python3 -c "..."` breakdown script over SSH inside a
  single-quoted remote command produced garbled/empty output due to quote-nesting between the local
  shell, SSH's single-quoted remote command, and Python's own string literals.
- **Fix:** Wrote the breakdown logic to a standalone `.py` file locally, `scp`'d it to the Pi, and
  executed it there with a clean argv, avoiding all quote-nesting. Iterated once more to add
  directory-name-ghost detection (checking whether a `._<Name>` entry's stripped name matches a real
  sibling directory) for a more meaningful category breakdown than a naive last-dot split (which
  misclassified directory names containing dots, e.g. `._Djesse Vol. 4 (Deluxe)`, as having a
  fake ".Deluxe)" extension).
- **Files modified:** None in this repo (scratchpad scripts only, cleaned up after use).
- **Commit:** N/A (no repo files touched).

**Total deviations:** 0 that touch repo files or plan scope. Both notes above are process/tooling
observations, not corrections to the plan's substance.

## Issues Encountered
None. SSH access, manifest write, and verification all succeeded on the first pass (after the
breakdown-script quoting rewrite noted above, which affected only the reporting tooling, not the
manifest itself).

## User Setup Required
**This plan is paused at a human checkpoint. User action is required to proceed to Plan 01-04.**
See "Checkpoint Status" below.

## Checkpoint Status: AWAITING APPROVAL

**Manifest path (Pi):** `~/stellar-backend/data/dotunderscore-manifest.txt`
**Total files that would be deleted:** 934
**Total bytes reclaimed:** 3,825,664 bytes (~3.65 MiB)
**Manifest integrity:** 0 non-`._`-prefixed entries found (verified by explicit per-entry check)
**No real audio file can match this manifest's selection criteria** -- the manifest was built by
`find -name '._*'`, a pure basename-prefix match; no real audio file on this library uses a `._`
prefix (confirmed: live real-audio-file count via `find ... ! -name '._*'` = 803, unchanged from the
803 baseline).

Read-only-mount mechanism Plan 01-04 will use (already disclosed to the user in this plan's own
checkpoint text, repeated here for the record): `/mnt/ssd` is mounted `ro` per `/etc/fstab`; deletion
requires `sudo mount -o remount,rw /mnt/ssd` → delete only manifest paths (re-validated against the
`._` prefix + `/mnt/ssd/Music` root at delete time) → `sudo mount -o remount,ro /mnt/ssd` → `mpc
update` → verify. Passwordless sudo is available on this single-user appliance (T-01-09, accepted
risk, disclosed).

**Next step:** Present this summary's counts/breakdown to the user, obtain "approved" (or revisions),
then Plan 01-04 may run.

## Next Phase Readiness
- Plan 01-04 (`depends_on: ["01-03"]`) is unblocked in terms of manifest availability but MUST NOT
  run until the user has explicitly typed "approved" (or equivalent) in response to this plan's
  checkpoint. That approval has **not** yet been obtained as of this SUMMARY.
- The manifest's sha256 (`0ecffca71ac507b31576cb9919ebb479692e83e4180c59628ad1ee7301ac4916`) is
  recorded here so Plan 01-04 (or a human) can verify the file was not altered between this plan's
  checkpoint and the next plan's deletion pass.

---
*Phase: 01-data-integrity-foundation*
*Completed: 2026-08-11 (Task 1 only -- plan paused at Task 2 checkpoint, awaiting user approval)*

## Self-Check: PASSED

Manifest existence, non-emptiness, NUL-delimiting, sha256, and 0-violation prefix check were all
verified live against the Pi in this session (see command output above). No repo-side files were
created or modified by this plan, so there are no local file/commit claims to verify beyond this
SUMMARY.md itself.
