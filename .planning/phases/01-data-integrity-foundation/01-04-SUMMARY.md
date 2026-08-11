---
phase: 01-data-integrity-foundation
plan: 04
subsystem: infra
tags: [pi, ssh, mpd, exfat, appledouble, verification]

# Dependency graph
requires:
  - phase: 01-data-integrity-foundation
    plan: 01-03
    provides: "934-entry NUL-delimited dotunderscore-manifest.txt on the Pi, user-approved for deletion"
provides:
  - "/mnt/ssd/Music free of all 934 ._-prefixed AppleDouble sidecar files (deleted, verified live)"
  - "deploy/verify-data-integrity.sh: reusable PASS/FAIL gate (mirrors verify-cutover.sh) asserting the USB/SSD source's mpc index count matches the real file count and zero ._ entries remain"
affects: [01-05, 01-06, 01-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "check()/FAILED/exit-code PASS/FAIL gate idiom (from verify-cutover.sh) reused for a second on-Pi verification script"
    - "trap-based unconditional remount-to-ro on both success and failure paths of a remount-rw delete loop"

key-files:
  created:
    - deploy/verify-data-integrity.sh
  modified: []

key-decisions:
  - "DATA-03: mpc stats' global Songs: line aggregates MPD's INTERNAL source (11 unrelated onboard-storage files) alongside USB (/mnt/ssd/Music, the SSD); verify-data-integrity.sh scopes I1 to `mpc listall USB` rather than `mpc stats` to avoid a false FAIL from that unrelated source."

patterns-established:
  - "On-Pi delete loops touching a normally-read-only mount must sudo the destructive command itself, not just the remount -- the exFAT mount's uid=111 ownership blocks unprivileged rm even while the mount is rw."

requirements-completed: [DATA-03]

# Metrics
duration: 20min
completed: 2026-08-11
---

# Phase 01 Plan 04: Delete `._` manifest, restore read-only mount, verify DATA-03 Summary

**All 934 user-approved `._` AppleDouble sidecar files deleted from `/mnt/ssd/Music`, mount restored to `ro`, MPD rescanned — the SSD's MPD-indexed song count (803, via the `USB` source) now exactly matches the real on-disk audio file count with zero `._` entries anywhere in MPD's index, closing DATA-03.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-08-11T19:52:00Z (approx, pre-state check)
- **Completed:** 2026-08-11T19:58:00Z (approx)
- **Tasks:** 2/2 completed
- **Files modified:** 1 (repo) + Pi-side deletion of 934 files (not a repo change)

## Accomplishments

- Re-verified pre-deletion state matched Plan 01-03's approved baseline exactly before touching
  anything: manifest sha256 `0ecffca71...` unchanged, 934 NUL-delimited entries, mount `ro`, 803 real
  audio files, 112 real `.jpg` (41,340,474 bytes), 1 real `.jpeg` (285,202 bytes), 12 real `.pdf`
  (31,930,312 bytes).
- Remounted `/mnt/ssd` `rw`, deleted all 934 manifest-listed `._` files with a per-entry
  defense-in-depth re-check (basename must start with `._` AND path must be under
  `/mnt/ssd/Music`), then unconditionally remounted `ro` via a shell `trap` on `EXIT` (runs on both
  success and failure paths).
- Ran `mpc update` and polled to completion (`mpc idle database`), confirmed by MPD's `DB Updated`
  timestamp advancing.
- Created `deploy/verify-data-integrity.sh`, a reusable PASS/FAIL gate mirroring
  `deploy/verify-cutover.sh`'s `check()`/`FAILED`/exit-code idiom, and ran it live on the Pi: both
  gates PASS.
- Ran full post-delete verification: real audio file count, real jpg/jpeg/pdf counts+bytes, mount
  state, `mpc stats`, and `._`-remaining counts (disk and MPD index) — every real-content number is
  byte-for-byte unchanged from the pre-deletion baseline; zero `._` entries remain anywhere.
- Marked `DATA-03` complete in REQUIREMENTS.md (deferred by 01-03 specifically to this plan, since
  01-03 was enumeration-only).

## Task Commits

1. **Task 1: Delete manifest files, remount read-only, rescan** — no commit (Pi-side action only,
   `files_modified: none` per plan frontmatter; no repo files changed)
2. **Task 2: Add deploy/verify-data-integrity.sh and confirm criterion 1** — `2d9cbb4` (feat)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified

- `deploy/verify-data-integrity.sh` — new PASS/FAIL gate script; `I1` compares `mpc listall USB`'s
  line count against the real (non-`._`) audio file count on `/mnt/ssd/Music`; `I2` asserts
  `mpc listall | grep -c '/\._'` (full index, not just USB) is `0`. Runnable via
  `bash ~/stellar-backend/deploy/verify-data-integrity.sh`.

## Pi-side (non-repo) changes

- 934 `._`-prefixed AppleDouble sidecar files deleted from `/mnt/ssd/Music`, per the manifest
  approved in Plan 01-03. Delete-run logs retained on the Pi at
  `~/stellar-backend/data/delete-run-*.log` (first, all-skipped attempt — see Deviations) and
  `~/stellar-backend/data/delete-run2-*.log` (successful, 934 deleted / 0 skipped) as an audit trail.
- `/mnt/ssd` mount: `rw` transiently during the delete loop, restored to `ro` unconditionally
  afterward via a `trap ... EXIT`.
- `deploy/verify-data-integrity.sh` copied to `~/stellar-backend/deploy/` on the Pi.

## Verification Evidence (live command output)

**Pre-deletion baseline re-check** (matched Plan 01-03's approved figures exactly):
```
manifest sha256: 0ecffca71ac507b31576cb9919ebb479692e83e4180c59628ad1ee7301ac4916
manifest entries: 934
mount: /dev/sda1 on /mnt/ssd type exfat (ro,relatime,uid=111,gid=29,...)
mpc stats: Songs: 1380
real audio files: 803
real .jpg: 112 files, 41340474 bytes
real .jpeg: 1 file, 285202 bytes
real .pdf: 12 files, 31930312 bytes
```

**Delete run** (`delete-run2-20260811T195449.log`):
```
DELETED=934 SKIPPED=0
```

**Post-delete verification** (all numbers unchanged from baseline — zero data loss):
```
=== 1. real audio files still on disk (MUST be 803) ===
803
=== 2. real .jpg count/bytes (MUST be 112 / 41340474) ===
112 41340474
=== 3. real .jpeg count/bytes (MUST be 1 / 285202) ===
1 285202
=== 4. real .pdf count/bytes (MUST be 12 / 31930312) ===
12 31930312
=== 5. mount state (MUST show ro) ===
/dev/sda1 on /mnt/ssd type exfat (ro,relatime,uid=111,gid=29,fmask=0022,dmask=0022,iocharset=utf8,errors=remount-ro)
=== 6. mpc stats after mpc update ===
Artists:    122
Albums:      58
Songs:      814
=== 7. count of ._ entries remaining in MPD index (MUST be 0) ===
0
=== 8. count of ._ entries remaining on disk (MUST be 0) ===
0
```

**`mpc stats` shows `Songs: 814`, not `803` — this is explained, not a defect.** MPD's
`music_directory` (`/etc/mpd.conf`) aggregates three sources: `USB` (symlink to `/mnt/ssd/Music`, the
SSD this plan cleaned), `INTERNAL` (the Pi's own onboard storage, a directory unrelated to this
plan), and `NAS` (symlink to an intentionally-unmounted `/mnt/NAS`, currently contributing 0). Scoped
per-source counts:
```
mpc listall | sed -E 's#^([^/]+)/.*#\1#' | sort | uniq -c
     11 INTERNAL
    803 USB
```
`mpc listall USB | wc -l` = **803**, exactly matching the real file count on `/mnt/ssd/Music`. The 11
`INTERNAL` entries (2 `.flac`, 9 `.dsf` under `INTERNAL/Jacob Collier - The Light For Days/` and
`INTERNAL/OCT0073-IntroducingJudeKofie DSD512/`) are pre-existing, real, unrelated audio files on the
Pi's own onboard storage — not under `/mnt/ssd/Music`, never touched by this plan, and out of DATA-03's
scope. `deploy/verify-data-integrity.sh`'s `I1` gate was written to scope its comparison to the `USB`
source specifically, for this reason (see Deviations).

**`deploy/verify-data-integrity.sh` run on the Pi:**
```
=== SSD music library data integrity (DATA-03) ===
  OK I1 mpc listall USB count (803) == real file count on /mnt/ssd/Music (803)
  OK I2 zero ._ entries in MPD's full index

=== ALL GATES PASS -- SSD data integrity verified ===
EXIT:0
```

**Task 1's specified verify command:**
```
$ mount | grep -q 'on /mnt/ssd type exfat (ro' && echo REMOUNT_RO_OK
REMOUNT_RO_OK
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - blocking issue] Unprivileged `rm` failed on every manifest entry; delete loop needed `sudo`**
- **Found during:** Task 1, first delete-loop attempt.
- **Issue:** The plan's action text specified `rm -f -- "$path"` after `sudo mount -o remount,rw`.
  The exFAT mount is owned `uid=111,gid=29` (not `eduardo`, uid 1000), so even with the mount `rw`,
  unprivileged `rm` failed on all 934 entries with `Permission denied`. The `trap`-based unconditional
  remount-to-`ro` fired correctly on this failure path, so **zero files were deleted and the mount
  was safely restored** — no data was at risk from this failed first attempt.
- **Fix:** Confirmed passwordless `sudo` is available on this single-user appliance (already
  disclosed and accepted as T-01-09 in Plan 01-03's checkpoint text), then re-ran the identical loop
  with `sudo rm -f -- "$path"`. Second attempt: `DELETED=934 SKIPPED=0`.
- **Files modified:** None in this repo (Pi-side only).
- **Commit:** N/A (no repo files touched by this fix; both delete-run logs retained on the Pi as an
  audit trail: `delete-run-*.log` for the failed attempt, `delete-run2-*.log` for the successful one).

**2. [Rule 1 - bug] `verify-data-integrity.sh`'s I1 gate scoped to the `USB` source, not global `mpc stats`**
- **Found during:** Task 2, first live run of the verification logic (as an ad-hoc check before
  writing the script) surfaced `mpc stats` reporting 814 songs against 803 real files on
  `/mnt/ssd/Music`.
- **Issue:** The plan's action text for I1 says "the `Songs:` line from `mpc stats` equals the real
  audio file count on `/mnt/ssd/Music`." Taken literally, this compares a Pi-wide count (all of
  MPD's `music_directory`, which also includes an `INTERNAL` onboard-storage source with 11 unrelated
  real files) against a count scoped to one source (`/mnt/ssd/Music`/`USB`). Written literally, this
  gate would permanently FAIL regardless of `/mnt/ssd/Music`'s actual cleanliness, because MPD
  legitimately indexes files outside `/mnt/ssd/Music` that DATA-03 was never about.
  Investigated and confirmed: `mpc listall | sed -E 's#^([^/]+)/.*#\1#' | sort | uniq -c` shows
  `INTERNAL: 11`, `USB: 803` — exhaustive, nothing unaccounted for.
- **Fix:** Implemented I1 as `mpc listall USB | wc -l` compared against the real file count on
  `/mnt/ssd/Music`, which is what the ROADMAP success criterion and DATA-03's intent actually require
  (the SSD's index matching the SSD's real files). I2 (the `._`-in-index check) is intentionally left
  scoped to the *full* `mpc listall` output, since a `._` file appearing under `INTERNAL` would be just
  as much of a defect as one under `USB` — that check has no false-positive risk from the multi-source
  topology.
- **Files modified:** `deploy/verify-data-integrity.sh` (written with the corrected scoping from the
  start; documented inline in the script's header comment for future maintainers).
- **Commit:** `2d9cbb4`

**Total deviations:** 2, both Rule 1/3, both self-resolved with no data loss and no plan-scope
change beyond correcting a comparison that would have produced a permanent false FAIL.

## Issues Encountered

None beyond the two auto-fixed deviations above. Both were caught and resolved without touching
audio-chain config, without any unintended file deletion, and without needing a second checkpoint.

## User Setup Required

None. This plan required no new user action — Plan 01-03's approval was the only user gate, and it
was already cleared before this plan ran.

## Next Phase Readiness

- `DATA-03` is now marked complete in `.planning/REQUIREMENTS.md`.
- ROADMAP Phase 1 success criterion 1 is met and verified live: `mpc listall USB` (803) matches the
  real audio file count on `/mnt/ssd/Music` (803), zero `._`-prefixed entries remain anywhere in
  MPD's index (0 across `USB` + `INTERNAL` combined).
- `deploy/verify-data-integrity.sh` is a durable, reusable check — future plans (or a human) can
  re-run it at any time via `bash ~/stellar-backend/deploy/verify-data-integrity.sh` without needing
  to reconstruct this plan's ad-hoc verification commands.
- Plans 01-05/01-06/01-07 (DATA-01/DATA-02/DATA-04 per the phase's remaining scope) are unblocked; none
  of them depend on repo-side artifacts from this plan beyond the new verify script being available as
  a pattern to follow.

---
*Phase: 01-data-integrity-foundation*
*Completed: 2026-08-11*

## Self-Check: PASSED

- `deploy/verify-data-integrity.sh` exists in the repo at the path claimed: verified via `git show
  2d9cbb4 --stat` (file created, mode 100755).
- Commit `2d9cbb4` exists: verified via `git log --oneline` showing it as HEAD at time of writing.
- All quantitative claims in "Verification Evidence" above are copy-pasted live command output from
  this session, not assertions — captured directly in the transcript at the time each command ran.
