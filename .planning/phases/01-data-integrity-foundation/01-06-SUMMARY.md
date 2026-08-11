# Plan 01-06 Summary — DATA-01 Retag Recommendations

**Completed:** 2026-08-12
**Requirements:** DATA-01 (partial — the handoff half; verification is Plan 01-07)
**Status:** Complete, handed off to the user. Phase now waits on out-of-band retagging.

## What was delivered

`.planning/phases/01-data-integrity-foundation/DATA-01-RETAG-RECOMMENDATIONS.md` — the precise
16-song / 3-folder retag list with recommended `Album` / `AlbumArtist` / `Artist` values, current
values shown alongside for diffing, plus three operational warnings.

## Live data (not the stale planning baseline)

Queried via MPD `listallinfo` on `stellar.local:6600` at generation time:

```
total songs indexed : 814   (803 USB + 11 INTERNAL)
missing Album tag   : 16    (matches deployed backend's skippedCount: 16)
```

| Folder | Files | Type | Current Artist |
|---|---:|---|---|
| `USB/RStrauss Also Sprach Zarathustra Till Eulenspiegel Karajn VPO__FLAC_352k-24b` | 10 | flac | `Herbert von Karajan  Wiener Philharmoniker` |
| `USB/toe - The Future Is Now - WAV` | 4 | wav | *(empty)* |
| `USB/Sigxer SU-6 test` | 2 | dff + wav | *(empty)* |

## Deviations

1. **Executed inline by the orchestrator, not a spawned `gsd-executor`.** Two consecutive executor
   spawns for this plan died on API session limits before writing anything. The deliverable is a
   document whose content was already settled with the user, so inline execution (the mode GSD
   exposes as `--interactive`) produced the same artifact at a fraction of the token cost. No plan
   content was skipped.

2. **Added a cross-check against the `toe` FLAC sibling** — not specified by the plan. Queried
   `USB/toe - The Future Is Now - FLAC/` live and found it already carries
   `Album='The Future Is Now'`, `AlbumArtist='toe'`, `Artist='toe'`, `Date='2012'`. The
   recommendations for the WAV edition were pinned to those exact values so the two folders form a
   genuine same-album/different-format pair — which is the Phase 3 duplicate-badge test case
   (BROWSE-01/02). Divergent spelling would have silently broken that pairing.

3. **Documented a WAV/DFF reliability risk the plan did not anticipate.** 6 of the 16 files are WAV
   or DSDIFF, formats with no dependable tag convention (ID3 chunk vs RIFF INFO; weak `.dff`
   support). MPD may not read back what any tagger writes. The document states this plainly and
   gives the user three named options if it happens, rather than promising all 16 will work.

## Constraint compliance

- **No tag was written to any file under `/mnt/ssd/Music`** (D-04). No `metaflac`, `ffmpeg`, `kid3`,
  or `mid3v2` was invoked; no `remount,rw` was performed by this plan.
- All Pi access was read-only MPD protocol queries.
- No audio-chain config touched.
- D-06 respected: this is a one-time human-reviewed derivation, not a backend runtime fallback. The
  backend still requires an `Album` tag and still skips + counts untagged songs.

## Handoff

**Blocked on the user.** DATA-01 cannot close until the user applies tags in their own tag editor.
Plan 01-07 then verifies: rescan → cache rebuild → assert the 3 albums appear → assert
`skippedCount` drops 16 → 0 → report per-file which tags landed.

01-07 is designed to run independently in a later session, so it does not block Phase 2.
