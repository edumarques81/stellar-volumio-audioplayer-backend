# Roadmap: Stellar Streamer — Library Browsing

## Overview

Three phases take the browse surface from "41% junk in MPD's index and albums that silently vanish"
to "every real album findable and playable, artist names read like a human wrote them, and
duplicate versions are distinguishable at a glance." Phase 1 cleans the data at the source so every
later count and verification is trustworthy. Phase 2 collapses credited-collaborator artist strings
to one entry per real artist and re-keys existing artwork onto the new identities in the same pass,
so the collapse never orphans artwork. Phase 3 closes the loop on the client surfaces — LCD and
iPhone both grow a duplicate-quality badge and both stop showing a dead end when an artist has only
loose songs. Audio playback is never touched.

## Phases

**Phase Numbering:**

- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [x] **Phase 1: Data Integrity Foundation** - Remove `._` junk from MPD's index, harden the backend against it everywhere, fix the 16 untagged real songs at source (user-executed), and make future data gaps detectable instead of silent.
- [ ] **Phase 4: Tail Cleanup — DSD Test File Conversion** - Convert the last untagged `.dff` test tone to `.dsf` on the MacBook so MPD can read its tags and `skippedCount` reaches 0. Runs after all feature phases.
- [ ] **Phase 2: Artist Identity & Artwork Migration** - Collapse credited-collaborator artist strings to one entry per real performer and re-key existing artwork onto the new identities in the same pass, recovering the 38 orphaned album-artwork rows too.
- [ ] **Phase 3: Browse Experience — Duplicate Badges & Empty States** - Add the quality-disambiguation badge to duplicate albums on both the LCD and the iPhone app, and make artists with only loose songs (or zero albums) render a real, playable result instead of an empty grid.

## Phase Details

### Phase 1: Data Integrity Foundation

**Goal**: MPD's database and every backend browse path reflect only real audio files, and any future data gap is visible rather than silently dropped.
**Mode:** mvp
**Depends on**: Nothing (first phase)
**Requirements**: DATA-01, DATA-02, DATA-03, DATA-04
**Success Criteria** (what must be TRUE):

  1. `mpc stats` on the Pi reports a song count matching the real file count on `/mnt/ssd/Music`, with zero `._`-prefixed entries remaining in MPD's index (verified live via SSH).
  2. The backend produces a precise list of the 16 untagged real songs (3 folders) with recommended `Album`/`AlbumArtist` values; after the user retags them in their own editor and MPD rescans, the "R. Strauss — Also Sprach Zarathustra / Till Eulenspiegel" album (Karajan/VPO) appears in Library → Albums on the LCD with all 10 tracks present and playable, and the `toe` and "Singxer SU-6 test" folders likewise appear under their artists.
  3. The cache-status payload (or an equivalent field/log) reports a count of skipped/untagged files instead of quietly omitting them; the count reads 0 once the retag in criterion 2 is verified.
  4. Copying a `._`-prefixed file onto the SSD and rescanning does not change any album's track count or any artist's album count — verified against at least `GetAlbumTracks` and `GetArtistAlbums`, not just the one call site that was previously hardened.

**Plans**: 7 plans (4 waves)

Plans:

- [x] 01-01-PLAN.md — Shared internal/infra/musicfile resource-fork predicate + GetAlbumTracks refactor (DATA-04)
- [x] 01-02-PLAN.md — Harden GetAlbumDetails against ._ junk + wire skipped/untagged count to cache + Socket.IO (DATA-04, DATA-02)
- [x] 01-03-PLAN.md — Enumerate ._ junk on the Pi into a manifest + human checkpoint before deletion (DATA-03)
- [x] 01-04-PLAN.md — Delete manifested ._ files, restore read-only mount, verify mpc stats (DATA-03)
- [x] 01-05-PLAN.md — Deploy hardened backend + live ._-recurrence regression test (DATA-04)
- [x] 01-06-PLAN.md — Produce the DATA-01 retag recommendation list + human-action handoff (DATA-01)
- [x] 01-07-PLAN.md — Verify retag landed + skippedCount reads 0 (DATA-01, DATA-02; blocked on the user's own retag step)

### Phase 2: Artist Identity & Artwork Migration

**Goal**: The artist list shows one row per real performer, using a uniform collapse rule validated against the real library, and every artist/album artwork row that existed before the collapse still resolves after it.
**Mode:** mvp
**Depends on**: Phase 1
**Requirements**: ARTIST-01, ARTIST-02, ARTIST-03, ART-01, ART-02, ART-03
**Success Criteria** (what must be TRUE):

  1. On the real Pi, Library → Artists shows a single "Herbert von Karajan" row (not "Herbert von Karajan  Wiener Philharmoniker") and a single "Luciano Pavarotti" row collapsing every label-credit variant — verified live, not just in a test.
  2. Each of the four observed join conventions collapses correctly, proven by a table-driven test built from the real 123 `list artist` values and spot-checked live on the Pi with one example per convention: comma (`Duke Ellington, John Coltrane`), spaced hyphen (`Adderley - Coltrane - Chambers - Cobb - Kelly`), role suffix (`Ella Fitzgerald - vocals  Paul Smith - piano`), and `with`/`and his orchestra` (`Ella Fitzgerald with Nelson Riddle And His Orchestra`).
  3. The artist list contains no blank row on the LCD, where the empty value from MPD's `list artist` output previously would have produced one.
  4. Artist photos/artwork that rendered before the collapse still render after it for the same real artists — no broken-image placeholders — verified by comparing artwork-link counts (or screenshots) on the Pi before and after the migration.
  5. The count of albums with no artwork link drops from 39 toward 0 without any new network call to Fanart.tv, Deezer, or MusicBrainz; re-running the migration a second time changes nothing and deletes no files under the artwork directory (idempotent, non-destructive).

**Plans**: TBD

Plans:

- [ ] 02-01: TBD

### Phase 3: Browse Experience — Duplicate Badges & Empty States

**Goal**: Every browse surface on the LCD and the iPhone app tells the truth: duplicate album versions are distinguishable at a glance, and an artist with no browsable albums never looks like a dead end.
**Mode:** mvp
**Depends on**: Phase 2
**Requirements**: ARTIST-04, BROWSE-01, BROWSE-02, BROWSE-03, BROWSE-04
**Success Criteria** (what must be TRUE):

  1. On the LCD's album interface, when two or more albums share the same title and artist, each tile shows its quality label (e.g. `352.8kHz/24bit FLAC` vs `96kHz/24bit FLAC`) as a badge — verified against a real duplicate pair on the Pi.
  2. The same badge appears in the iPhone app's album grid and artist→albums grid for that same duplicate pair, verified on the simulator against the live backend.
  3. A unique album (no title+artist duplicate) shows no badge on either client — spot-checked against a known unique album so the common case stays clean.
  4. Tapping into an artist whose tracks belong to no album (or who resolves to zero albums) shows a real, playable song list on both the LCD and the iPhone app instead of an empty grid — verified end-to-end by playing a track from that list.

**Plans**: TBD
**UI hint**: yes

Plans:

- [ ] 03-01: TBD

**Note:** Phase 3 touches the Socket.IO contract shared by backend, `Volumio2-UI`, and `stellar-ios`
(the duplicate-detection signal and the loose-song list are new payload fields). Any event-shape
change here is a three-repo change — update `docs/SOCKET-CONTRACT.md` and ship all three together.

### Phase 4: Tail Cleanup — DSD Test File Conversion
**Goal**: The last untagged file in the library becomes visible, taking `skippedCount` from 1 to 0 and closing the data-integrity story opened in Phase 1.
**Mode:** mvp
**Depends on**: Phase 3 (runs last, after all feature work)
**Requirements**: DATA-05
**Success Criteria** (what must be TRUE):
  1. `USB/Sigxer SU-6 test/DSD-测试文件 ANNOUNCEMENT FOR BASIC CHECKS (Voice).dff` is replaced by a `.dsf` equivalent that MPD indexes with `Album = "Singxer SU-6 Test"` and `AlbumArtist = "Test Signals"` — verified live via `mpc search`.
  2. The backend's `skippedCount` reads **0** after a cache rebuild (was 16 pre-Phase-1, 1 after Phase 1).
  3. The DSD audio survives the container change bit-perfectly — the decoded stream MD5 of the `.dsf` matches the original `.dff` (`ffmpeg -nostdin -i <f> -map 0:a -f md5 -`). A DSD→PCM conversion is a FAILURE, not an acceptable outcome, on a bit-perfect appliance.
  4. `deploy/verify-data-integrity.sh` still passes all gates, `/mnt/ssd` ends mounted `ro`, and no `._` files are created.
**Plans**: TBD

> **⚠ Feasibility investigated 2026-08-12 — this is NOT a one-liner.** The tooling to do it is not
> on the Pi:
> - `ffmpeg` 5.1.8 has a **dsf demuxer but no dsf or dsdiff muxer** (`ffmpeg -muxers | grep dsf` →
>   0 hits). Upstream ffmpeg cannot *write* either DSD container. `-c:a copy` to `.dsf` will fail.
> - `dff2dsf`, `sacd_extract`, `dsf2flac` are all absent from the Pi.
> - Source file: `dsd_msbf`, 2ch, 42,940,056 bytes. It already carries a correct ID3 chunk written by
>   `mutagen.dsdiff` in Phase 1 — the blocker was never the tag, it is that **MPD 0.23.12's DSDIFF
>   decoder does not read ID3 back**. Converting the container is the workaround.
>
> **DECIDED (user, 2026-08-12): do the conversion on the MacBook**, not on the Pi. No appliance
> dependency gets installed.
>
> **Recommended mechanism — copy the file, never mount the SSD on macOS:**
> `scp` the `.dff` from the Pi → convert on the Mac → `scp` the `.dsf` back → remove the `.dff` on
> the Pi from Linux. This keeps macOS away from the exFAT volume entirely, so **no `._` sidecars are
> created** and no `dot_clean` pass is needed. Mounting the SSD on the Mac would reintroduce exactly
> the junk Phase 1 removed.
>
> **Known before planning starts (checked 2026-08-12, do not re-derive):**
> - Mac `ffmpeg` is 8.0.1 — it also has a **dsf demuxer but no dsf/dsdiff muxer**. Stock ffmpeg on
>   either machine cannot write DSF.
> - No `sox`, `xld`, `dbpoweramp`, `dff2dsf`, `sacd_extract`, `dsf2flac`, or AudioGate on the Mac.
>   `numpy` 1.26.3 is present; `mutagen` is not (it is on the Pi).
> - So Phase 4 must still pick a tool: install one via Homebrew, use a GUI converter, or write a
>   small DFF→DSF container remuxer (both wrap the same DSD bitstream, but DSF is planar 4096-byte
>   blocks per channel and typically LSB-first, while this DFF is interleaved `dsd_msbf` — so the
>   remux needs de-interleaving and bit-order reversal, not a byte copy).
>
> **Fallback if conversion proves not worth it:** delete the file, or accept it. This is a **DAC
> verification tone, not music**, and `skippedCount: 1` is now legible rather than silent — which was
> the actual Phase 1 goal. Criterion 3 (bit-perfect DSD survival) is non-negotiable; a DSD→PCM
> "conversion" fails this phase rather than completing it.

Plans:
- [ ] 04-01: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Data Integrity Foundation | 5/7 | In Progress|  |
| 2. Artist Identity & Artwork Migration | 0/TBD | Not started | - |
| 3. Browse Experience — Duplicate Badges & Empty States | 0/TBD | Not started | - |
| 4. Tail Cleanup — DSD Test File Conversion | 0/TBD | Not started | - |
