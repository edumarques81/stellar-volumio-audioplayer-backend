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
- [x] **Phase 2: Artist Identity & Artwork Migration** - Collapse credited-collaborator artist strings to one entry per real performer and re-key existing artwork onto the new identities in the same pass, recovering the 38 orphaned album-artwork rows too.
- [x] **Phase 3: Browse Experience — Duplicate Badges & Empty States** - Add the quality-disambiguation badge to duplicate albums on both the LCD and the iPhone app, and make artists with only loose songs (or zero albums) render a real, playable result instead of an empty grid. (completed 2026-08-12)
- [ ] **Phase 4: Tail Cleanup — DSD Test File Conversion** - Convert the last untagged `.dff` test tone to `.dsf` on the MacBook so MPD can read its tags and `skippedCount` reaches 0. Runs after all feature phases.

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

**Plans**: 4 plans (3 waves)

Plans:

- [x] 02-01-PLAN.md — TDD: pure Collapse() artist-name rule, proven against the real 124-value corpus (ARTIST-01, ARTIST-02, ARTIST-03)
- [x] 02-02-PLAN.md — Wire Collapse() into the cache builder + the MPD-direct fallback, merging album counts across collapsed variants (ARTIST-01, ARTIST-02, ARTIST-03)
- [x] 02-03-PLAN.md — Deterministic artist-artwork rekey (auto-runs on boot) + album-artwork orphan rekey helper, unit-tested, zero live Pi contact (ART-01, ART-02, ART-03)
- [x] 02-04-PLAN.md — Deploy collapse + migration together, human-confirmed album-artwork matching, live verification (autonomous: false) (ARTIST-01, ARTIST-02, ARTIST-03, ART-01, ART-02, ART-03)

### Phase 3: Browse Experience — Duplicate Badges & Empty States

**Goal**: Every browse surface on the LCD and the iPhone app tells the truth: duplicate album versions are distinguishable at a glance, and an artist with no browsable albums never looks like a dead end.
**Mode:** mvp
**Depends on**: Phase 2
**Requirements**: ARTIST-04, BROWSE-01, BROWSE-02, BROWSE-03, BROWSE-04, BROWSE-07
**Success Criteria** (what must be TRUE):

  1. On the LCD's album interface, when two or more albums share the same title and artist, each tile shows its quality label (e.g. `352.8kHz/24bit FLAC` vs `96kHz/24bit FLAC`) as a badge — verified against a real duplicate pair on the Pi.
  2. The same badge appears in the iPhone app's album grid and artist→albums grid for that same duplicate pair, verified on the simulator against the live backend.
  3. A unique album (no title+artist duplicate) shows no badge on either client — spot-checked against a known unique album so the common case stays clean.
  4. Tapping into an artist whose tracks belong to no album (or who resolves to zero albums) shows a real, playable song list on both the LCD and the iPhone app instead of an empty grid — proven by a synthetic test fixture, since no artist in the live library has zero albums.
  5. `Mahler: The Symphonies` renders as ONE album tile on the LCD and the iPhone (was 11 identical tiles), and drilling into it exposes its 11 discs. `Miles Davis - Kind Of Blue` does NOT group — it stays 3 separate entries distinguished by badge, because every track is `Disc: 1`.

**Plans**: 10 plans (6 waves)
**UI hint**: yes

Plans:

- [x] 03-01-PLAN.md — TDD: pure multi-disc box-set grouping rule (discgroup), proven against real Mahler/Kind Of Blue Pi data (BROWSE-07)
- [x] 03-02-PLAN.md — TDD: pure duplicate-disambiguation badge rule (dupebadge), quality->disc->source precedence (BROWSE-01, BROWSE-02, BROWSE-03)
- [x] 03-03-PLAN.md — Backend contract (Album.badge/discCount, Track.disc) + wire grouping/badging into the MPD-direct GetAlbums/GetArtistAlbums path + disc-aware track sort (BROWSE-01, BROWSE-02, BROWSE-03, BROWSE-07)
- [x] 03-04-PLAN.md — Cache schema v6 + wire grouping/badging into the cache-build path Builder.buildAlbums/CachedService.GetAlbums (BROWSE-01, BROWSE-02, BROWSE-03, BROWSE-07)
- [x] 03-05-PLAN.md — Loose-track fallback for GetArtistAlbums, proven by a synthetic zero-album fixture (ARTIST-04, BROWSE-04)
- [x] 03-06-PLAN.md — docs/SOCKET-CONTRACT.md update + backend build/deploy/live Socket.IO verification against the real 81-album library (ARTIST-04, BROWSE-01, BROWSE-02, BROWSE-03, BROWSE-04, BROWSE-07)
- [x] 03-07a-PLAN.md — Volumio2-UI (LCD): badge render, disc-grouped track headers, loose-track fallback (ARTIST-04, BROWSE-01, BROWSE-03, BROWSE-04, BROWSE-07)
- [x] 03-07b-PLAN.md — Volumio2-UI deploy + CDP screenshot verification + human checkpoint (BROWSE-01, BROWSE-03, BROWSE-07)
- [x] 03-08a-PLAN.md — stellar-ios: badge render on both album grids, disc-grouped track headers, loose-track fallback (ARTIST-04, BROWSE-02, BROWSE-03, BROWSE-04, BROWSE-07)
- [x] 03-08b-PLAN.md — stellar-ios simulator build + screenshot verification + human checkpoint (BROWSE-02, BROWSE-03, BROWSE-07)

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

## Backlog

Unsequenced items. Not part of the v1.0 milestone phases; promote with `/gsd:review-backlog`.

### Phase 999.3: Player format strip shows wrong bit depth + wrong "HI-RES" + wrong bio (FIXED 2026-08-12)

**Captured:** 2026-08-12, observed directly in Plan 03-07b's live LCD screenshots. **Pre-existing —
NOT a Phase 3 regression.** The new Phase 3 badge renders the same album correctly as
`44.1kHz/16bit FLAC`, which is what proves the backend data is right and the fault is downstream.

Three distinct defects visible on one screen (`03-07b-lcd-duplicate-badge-visible.png`):

1. **Bit depth renders as `44-bit`.** The album is 44100 Hz / **16** bit. The format strip shows
   "44-bit / 44.1 kHz | FLAC". Reproduced on a second album (Mahler, also 44.1/16) in
   `03-07b-lcd-mahler-one-tile-with-disc-headers.png`, so it is systematic, not a one-off. Looks like
   the sample rate is being read into the bit-depth slot.

2. **`HI-RES 44.1kHz` badge on CD-quality audio.** 44.1kHz/16bit is Red Book, not hi-res. The
   threshold for that badge is wrong or absent.

3. **Wrong album bio.** For toe's *The Future Is Now*, the bio reads "*The Future Is Now* is Non
   Phixion's sole studio album, released in March 2002 on Uncle Howie/Landspeed Records…" — that is a
   different artist's album of the same name. The Wikipedia→LLM bio lookup matched on title alone
   without disambiguating by artist.

Defect 1 is the most embarrassing on an audiophile streamer — the device misreports the format of the
music it is playing. Defects 1 and 2 are frontend format-strip logic; defect 3 is backend
(`internal/domain/bios/`), where the lookup should include the artist and reject low-confidence
matches rather than displaying a confident wrong answer.

**RESOLVED 2026-08-12** — all three defects fixed, deployed to the Pi, and verified live.

- **Defects 1 + 2 had a single root cause.** `parseBitDepth` (Volumio2-UI
  `src/lib/components/redesign/playerStateParsers.ts`) matched the FIRST run of digits in the
  string. `AlbumPage` feeds it the backend's composite `album.quality` label, so
  `"44.1kHz/16bit FLAC"` parsed to **44** -> rendered "44-bit", and `44 >= 24` then tripped the
  HI-RES branch in `pickBadgeKind`. The badge threshold itself was always correct. Fixed by
  requiring an explicit `bit`/`bits` token (or an otherwise bare number).
  Live LCD after: `...Like Clockwork` (44.1/16) shows `16-bit / 44.1 kHz` with **no** HI-RES
  cluster; `Cannonball and Coltrane` (352.8/24) still shows `HI-RES 352.8kHz | 24-bit / 352.8 kHz`.
  Scope was larger than reported: of the 11 distinct `quality` shapes in the live library, **10 were
  rendering a wrong bit depth** (48 of 66 albums), and 14 Red Book albums were falsely badged HI-RES.
  A third instance of the same parsing bug was found and fixed: `parseSampleRate("DSD64")` read a
  bare 64 -> 64 kHz, so `dsdRate()` rendered the badge as **"DSD1"**.

- **Defect 3** (`internal/infra/wikipedia/client.go`): `LookupAlbum` fell back to the bare album
  title and accepted whatever page returned. `"The Future Is Now (toe album)"` 404s, so it served
  Non Phixion's album page as a confident match. Three gates added:
  1. a bare-title hit must corroborate the artist (word-boundary match on the raw tag **and** its
     `artistidentity.Collapse` form);
  2. `type: "disambiguation"` pages are rejected outright (Wikipedia's `Miles Ahead` page is one, and
     it name-drops Miles Davis, so an artist check alone would have accepted it);
  3. `LookupArtist` gates the bare name on the page looking musical at all, then tries
     `(band)`/`(musician)` — bare `toe` returns the anatomy article ("Toes are the digits of the foot
     of a tetrapod"), which was being served as that band's bio.
  `LookupArtist` also now tries the collapsed primary artist, so an album credited to
  `Miles Davis - Arranged and Directed by Gil Evans` resolves to Miles Davis.
  Live after: toe -> `en.wikipedia.org/wiki/Toe_(band)` ("Japanese post-rock and math rock band from
  Tokyo"). Controls unchanged: Kind Of Blue and ...Like Clockwork still return their correct album
  bios; Miles Ahead went from empty to the Miles Davis artist bio.

- **iOS needed no change** — `FormatBadgeStrip` reads the discrete `bitdepth` field from `pushState`,
  never the composite quality label, so it never had defects 1/2.

### Phase 999.2: Ship files to the Pi — upload, unzip, land on the SSD, index in MPD (BACKLOG)

**Captured:** 2026-08-12 (user: *"I need a way to send files to the pi so it can unzip and add those files to the ssd drive and to mpd"*)

**Status: deliberately un-designed.** The user asked to capture this and explicitly deferred all
thinking about it until the v1.0 milestone phases finish. Do NOT start solving it here — promote it
via `/gsd:review-backlog` when the milestone closes, then run it through discuss → plan properly.

**The ask, in the user's terms:** a way to send files (archives) to the Pi, have the Pi unzip them,
place the contents on the SSD music drive, and get them into MPD's database.

**Already-established facts that any future design must respect** (recorded so they are not
re-derived, not as design decisions):

- `/mnt/ssd` is mounted **read-only** (`ro,nofail,uid=mpd,gid=audio`). Any write needs a
  `remount,rw` → work → `remount,ro` round-trip, trap-guarded.

- **macOS recreates `._` AppleDouble junk** on this exFAT volume. Phase 1 deleted 934 of them. An
  upload path that routes through a macOS mount reintroduces the problem; copying over the network
  (scp/rsync from Linux side) does not.

- MPD needs an explicit `mpc update` plus a backend cache rebuild before new music appears.
- Files land under `/mnt/ssd/Music`, exposed to MPD as `USB/` via
  `/var/lib/mpd/music/USB -> /mnt/ssd/Music`.

- New albums require `Album` and `AlbumArtist` tags or they are invisible (Phase 1, DATA-01/02);
  `skippedCount` on `pushLibraryCacheStatus` will report untagged arrivals.

- The music library holds irreplaceable masters — any ingest path needs care around overwrite.

**Open questions for the future discuss phase** (listed, not answered): transport (web upload in the
existing UI vs scp/rsync vs a watched folder vs Samba), who unzips and where, how to handle
name collisions and partial uploads, whether tagging is validated at ingest, and whether this
belongs in the backend or as a separate small service.

### Phase 999.1: Miles Ahead — cover art FIXED 2026-08-12; booklet still BACKLOG

**Captured:** 2026-08-12 (user: *"Miles Ahead album from Miles Davis cover art and booklet … I can't see a cover art for it now"*)

**COVER ART RESOLVED 2026-08-12.** The investigation note below was right that the art was never
missing; the fault was in the URL. The backend built album-art URLs by raw string concatenation
(`"/albumart?path=" + trackPath`) in **ten** places, and a literal `+` in a query value decodes to a
SPACE — so the handler looked for `Miles Davis   19-DSF-11289k-1b`, found nothing, and returned 404,
while the same request with `%2B` returned the cover. Fixed by a new leaf package
`internal/infra/arturl` (query-encodes the value, preserves the `/albumart?path=` prefix that
`isArtworkRedirectURL` matches on); all ten sites now call it. `&`, `#`, `%` and `=` were the same
bug waiting on a differently-named album. `docs/SOCKET-CONTRACT.md` updated.

Verified live from the kiosk browser, fetching the exact URL the backend emits: Miles Ahead returns
**HTTP 200, 440,399 bytes**. All 66 album covers were fetched in the same pass — the only failure is
`Singxer SU-6 Test`, the DAC test-tone folder, which genuinely has no artwork.

**Still open:** surfacing the booklet PDF in album metadata. Unchanged from the note below — there is
no booklet/attachment field in the Socket.IO contract, so it needs a design decision (new field on
the album payload + a client affordance to open it) before it is plannable.

**Album:** `Miles Ahead - Miles Davis + 19`
**Album artist tag:** `Miles Davis - Arranged and Directed by Gil Evans`
**Folder:** `/mnt/ssd/Music/Miles Ahead - Miles Davis + 19-DSF-11289k-1b/` (10 × `.dsf`, DSD)
**Cache row:** `e8280b866030e11f2ae8b81fc6f236da` — **`artwork_id` is NULL**

**⚠ Investigated 2026-08-12 — the naive framing is wrong. The art is NOT missing.**

- The `.dsf` files **already carry embedded cover art**: `ffprobe` shows a second stream,
  `codec_name=mjpeg codec_type=video`, alongside `dsd_lsbf_planar` audio.

- The backend **already serves it successfully**:
  `GET /albumart?path=USB/Miles Ahead - Miles Davis + 19-DSF-11289k-1b/01-Springsville.dsf`
  → **HTTP 200, 440,399 bytes**.

- The **booklet PDF is already on the SSD** too:
  `Miles Davis + 19 - Miles Ahead  Booklet.pdf` (note the double space in the filename). The user also
  uploaded a copy (5,194,438 bytes, 3 pages, 2 embedded images).

So this is **not** "fetch missing artwork". The real question is why a cover the backend returns with
HTTP 200 does not reach the screen. Investigate in this order:

1. Which URL the LCD/iOS actually requests for this album, and whether it 200s (the album row's
   `first_track` may differ from the file that works, or URL-encoding of `+` / double-space may break).
   Note `+` in a URL query decodes to a space — this album's path contains a literal `+`.

2. Whether the empty `artwork_id` matters on the path the client takes. Per
   `reference_stellar_albumart_bypasses_artwork_table`, `/albumart?path=` bypasses the artwork table
   entirely while `/artistart` requires it — so a NULL `artwork_id` should NOT block the cover.

3. Whether DSD/`.dsf` embedded art is handled differently from FLAC anywhere in the resolution chain.

**Second, separate ask:** surface the booklet PDF in album metadata. No mechanism exists today — the
Socket.IO contract has no booklet/attachment field, so this needs a design decision (new field on the
album payload + a client affordance to open it) before it is plannable.

**Relationship to Phase 2:** the album artist `Miles Davis - Arranged and Directed by Gil Evans`
contains ` - `, so Phase 2's collapse rule will render it as `Miles Davis`. Worth confirming that does
not further disturb this album's artwork linkage.

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Data Integrity Foundation | 5/7 | In Progress|  |
| 2. Artist Identity & Artwork Migration | 3/4 | In Progress|  |
| 3. Browse Experience — Duplicate Badges & Empty States | 10/10 | Complete   | 2026-08-12 |
| 4. Tail Cleanup — DSD Test File Conversion | 0/TBD | Not started | - |
