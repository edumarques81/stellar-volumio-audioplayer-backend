# Stellar Streamer — Library Browsing

## What This Is

Stellar is a bit-perfect audiophile streamer running on a Raspberry Pi 5 with a Singxer SU-6 USB
DAC, driven by MPD. A Go backend serves a Svelte LCD interface (1920x440 touchscreen) same-origin on
`:3000`, plus a minimal SwiftUI iPhone remote, over one Volumio-compatible Socket.IO contract. This
milestone is about the **library browsing experience**: making every album in the library actually
reachable, making the artist list read like a human wrote it, and making multiple quality versions
of the same album distinguishable at a glance.

## Core Value

**Every album on the disk is findable and playable from the LCD and the phone, and the browse
surface is honest about what it's showing.** Bit-perfect playback is already solved and must not
regress; this milestone is about the path to pressing play.

## Requirements

### Validated

<!-- Shipped and confirmed valuable — inferred from the existing codebase, verified this session. -->

- ✓ MPD-backed library browsing (albums, artists, artist→albums, album→tracks) — existing
- ✓ Volumio-compatible Socket.IO contract shared by LCD, iOS, and Volumio Connect clients — existing
- ✓ SQLite cache for album/artist metadata with non-destructive rebuild guards — existing
- ✓ Artwork enrichment pipeline: MusicBrainz + Cover Art Archive (albums), Fanart.tv → Deezer →
  first-album fallback (artists) — existing
- ✓ Album/artist bios from Wikipedia summarised by an LLM, 90-day TTL — existing
- ✓ Quality label derivation from MPD `Format` (PCM kHz/bit + DSD inference) — existing
- ✓ Pi-hosted topology: backend serves the built frontend same-origin, MPD on localhost,
  self-healing via systemd `Type=notify` + watchdog — existing
- ✓ Bit-perfect playback chain (mixer_type none, raw `hw:` ALSA, native DSD) — existing

### Active

<!-- Current scope. Hypotheses until shipped and validated. -->

- [ ] **R1 — No album is silently invisible.** Files lacking an `Album` tag currently vanish from
  the entire browse path while their artist still appears, producing an artist screen with zero
  content. Fix the source data, and add a safety net so this failure mode is *detectable* rather
  than silent if it recurs.
- [ ] **R2 — MPD's database reflects the actual music.** 566 of 1369 indexed rows (41%) are macOS
  `._` resource-fork files. Remove them at source and make the backend defensively immune to them
  everywhere, not just in `GetAlbumTracks`.
- [ ] **R3 — The artist list shows main artists.** Collapse credited-collaborator strings to the
  first credited performer, so all of an artist's work lands under one entry. Uniform rule across
  genres; must be validated against the real library's four observed join conventions.
- [ ] **R4 — Artist artwork survives the collapse.** Re-key existing artwork onto the new artist
  identities and recover the 38 album-artwork rows already orphaned by the NAS→SSD move. No
  re-downloading against rate-limited APIs.
- [ ] **R5 — Duplicate album versions are distinguishable.** When two or more albums share
  title + artist, show the quality label as a badge on the tile — on the LCD and in the iPhone app.
  No badge when there is no ambiguity.
- [ ] **R6 — Artists with only loose songs render sensibly**, not as an empty album grid.

### Out of Scope

- **MPD-as-sole-source-of-truth refactor** (demoting SQLite to an enhancement-only store) — deferred
  to the next milestone. Analysis is complete and favourable, but the artist-identity work in R3/R4
  will teach us what the identity scheme actually needs. Doing both at once triples the blast radius
  across three repos.
- **Folder-name fallback for untagged albums** — considered and rejected. The user will retag the 16
  offending files at source instead, which yields clean data rather than a heuristic that has to
  parse strings like `..._FLAC_352k-24b`. R1's safety net covers recurrence.
- **A separate Composer browse axis** — considered for this classical-heavy library, rejected for
  now in favour of the simpler uniform "first credited performer" rule. Revisit if the artist list
  still reads badly after R3.
- **Fixing `sort=year` / `sort=recently_added`** — both were effectively broken (no `Year` was ever
  mapped from MPD; `AddedAt` was set to build time). Real, but a separate concern from this
  milestone's browse-correctness goal. **FIXED out-of-band on 2026-08-19 (commit `6e627f4`)**: MPD's
  `Date` tag and newest per-album `Last-Modified` now populate both fields through the discgroup and
  cache paths, and `AddedAt` is normalised to UTC because `albums.added_at` is a TEXT column and
  `ORDER BY added_at DESC` is therefore a lexical compare. Deployed and verified on the Pi.
- **Anything touching the bit-perfect audio chain** — permanently out of scope without an explicit
  separate decision.

## Context

**Repository layout.** `stellar-streamer/` is a workspace root, not a git repo. It contains four
independent repos: `stellar-volumio-audioplayer-backend` (Go, `main`) — where this `.planning/`
lives; `Volumio2-UI` (Svelte 5 + TS, `master`); `stellar-ios` (Swift 6 + SwiftUI, `main`); and
`volumio3-backend` (vendored upstream Volumio, read-only reference). Architecture is documented in
the workspace-root `CLAUDE.md`; the wire contract in `docs/SOCKET-CONTRACT.md`; the deployment
history in `MIGRATION-PLAN.md`.

**Live environment, verified 2026-08-11.** Raspberry Pi 5, Debian 12 bookworm, MPD 0.23.12
(protocol 0.23.5), music on a USB exFAT SSD mounted at `/mnt/ssd` with
`/var/lib/mpd/music/USB -> /mnt/ssd/Music`. MPD reports 122 artists / 58 albums / 1380 songs; after
excluding `._` junk the real figure is 803 songs. The SQLite cache holds 78 albums, 48 artists,
125 artwork rows (258 MB of files on disk), 17 album bios, 15 artist bios, 38 last-played rows, and
**0 tracks** — the `tracks` table is dead code.

**R1 root cause, traced this session.** `internal/infra/mpd/client.go:749` in `GetAlbumDetails`:

```go
// Skip songs without album tag
if album == "" {
    continue
}
```

The reproducer is `/mnt/ssd/Music/RStrauss Also Sprach Zarathustra Till Eulenspiegel Karajn
VPO__FLAC_352k-24b/`. Its 10 real FLACs carry only `Artist: Herbert von Karajan  Wiener
Philharmoniker`, `Title`, `Track`, `Format: 352800:24:2` — no `Album`, no `AlbumArtist`. The artist
appears because `GetArtists` reads MPD's `list artist`; drilling in calls `GetArtistAlbums`, which
iterates `GetAlbumDetails` and drops the folder. Result: artist visible, zero songs. Library-wide,
16 real songs are affected across 3 folders (10 Karajan, 4 `toe - The Future Is Now - WAV`,
2 `Sigxer SU-6 test`); 40 real songs lack `AlbumArtist` and fall back to `Artist`.

**R3 evidence.** `mpc list artist` yields 123 distinct values against 49 for `albumartist`, with at
least four join conventions plus an empty value:

```
(empty)
Adderley - Coltrane - Chambers - Cobb - Kelly
Duke Ellington, John Coltrane
Ella Fitzgerald - vocals  Paul Smith - piano
Ella Fitzgerald with Nelson Riddle And His Orchestra
Herbert von Karajan  Wiener Philharmoniker
```

A naive comma split handles exactly one of these. The library skews classical (Karajan/VPO, Mahler
symphony cycles, Dénes Várjon plays Bartók, Pavarotti), which is where the conventions are messiest.

**R4 evidence — identity is already fragile.** Album identity is `md5(albumArtist‖album‖uri)` and
artist identity is `md5(name)`, both derived from mutable inputs. The NAS→SSD migration changed
URIs, which changed album IDs, which detached artwork: the live DB has **38 orphaned album-artwork
rows against 39 albums with no artwork link**. The two relink queries in `builder.go:282` and `:344`
exist to paper over this and only work when the path is unchanged. Collapsing artist names in R3
will do the same thing to artist artwork unless R4 lands with it.

**Prior art in-repo.** `CachedService` overrides only `GetAlbums`/`GetArtists`/`GetRadioStations`;
`GetArtistAlbums` and `GetAlbumTracks` already fall through to the MPD-direct base `Service`, so
MPD-direct browsing is in production today. `GetAlbumDetails` issues `search base <path>` and pulls
every song in the subtree per request — fine at 803 songs, a scaling problem later, and the reason
the deferred refactor matters.

## Constraints

- **Audio (absolute)**: bit-perfect chain is untouchable — `mpd.conf` `mixer_type "none"`, raw `hw:`
  ALSA device, no `dop` line, `hybrid_dsd` disabled, shairport `ignore_volume_control = "yes"`, DAC
  exclusive to MPD/shairport. Backend and frontend never open ALSA. *Why:* it is the entire point of
  the product and it currently works with zero xruns.
- **Contract**: any Socket.IO event-shape change is a three-repo change (backend + Volumio2-UI +
  stellar-ios). *Why:* three independent clients, including v2-era Volumio Connect apps.
- **Data**: the user's music files are masters. No writes to `/mnt/ssd/Music` without explicit
  per-operation confirmation. *Why:* irreplaceable, and some are 650 MB single-track DXD files.
- **Artwork**: 258 MB of enrichment artwork was fetched against rate-limited APIs (MusicBrainz at
  1 req/s). Migrations must preserve it. *Why:* re-fetching costs hours and API quota.
- **Deploy**: backend ships as an ARM64 static binary (`make build` → `scp` → `systemctl restart`);
  frontend as `npm run build` → `scp dist/` to `/home/eduardo/stellar-volumio`; iOS needs
  `xcodegen generate` on new `.swift` files and an explicit `IOS_SIM_ID`.
- **Process**: commit **and push** at every phase boundary, never batch (backend/iOS → `main`,
  Volumio2-UI → `master`). *Why:* work spans repos; unpushed work is lost on a context drop.
- **Verification**: driven by the agent on the real Pi and the simulator, not delegated to the user
  as a QA tier.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Collapse artists to the **first credited performer**, uniformly across genres | One rule is testable and predictable; a classical-specific rule needs a Composer axis we deliberately deferred | — Pending |
| **Fix tags at source** rather than add a folder-name fallback | Yields clean data instead of a heuristic parsing `..._FLAC_352k-24b`; user applies tags in their own tagger | — Pending |
| Quality badge **only when title+artist is duplicated** | Distinguishes versions where it matters without adding noise to every tile | — Pending |
| **Delete `._` files from the SSD** and harden the backend | Fixes MPD's own stats at source; defensive filtering alone leaves 41% junk in the index forever | — Pending |
| **Defer** the MPD-as-source-of-truth refactor to the next milestone | R3/R4 will reveal what the identity scheme needs; doing both at once triples blast radius | — Pending |
| **Migrate and re-key** artwork rather than let enrichment re-fetch | Preserves 258 MB fetched against 1 req/s APIs; also recovers the 38 existing orphans | — Pending |
| `.planning/` lives in the **backend repo**, tracked | Workspace root is deliberately not a git repo; most of this milestone is Go work | ✓ Good |
| Planning/execution agents run on the **Budget model profile** (Haiku where possible, Sonnet otherwise) | User directive; keeps cost down. The artwork re-key plan is reviewed by the lead agent before execution given its blast radius | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-08-11 after initialization*
