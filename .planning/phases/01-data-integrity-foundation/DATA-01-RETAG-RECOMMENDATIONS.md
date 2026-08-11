# DATA-01 — Retag Recommendations

**Generated:** 2026-08-12 from a live MPD query (`listallinfo` on `stellar.local:6600`)
**Live state at generation:** 814 songs indexed, **16 real songs missing an `Album` tag**, across 3
folders. The deployed backend reports `skippedCount: 16` on `pushLibraryCacheStatus`.

> **This is a recommendation document. No agent has written, or will write, tags to any file under
> `/mnt/ssd/Music`** — locked decision D-04. You apply these in your own tag editor. When you are
> done, Plan 01-07 verifies the result on the Pi.
>
> This is **not** the folder-name fallback that D-06 rejected. D-06 forbids the *backend* from
> inferring an Album from folder names at runtime; that remains forbidden and the backend still
> skips (and now counts) untagged songs. This document is a one-time, human-reviewed derivation.

## Why this matters

These 16 songs are invisible in the library. `GetAlbumDetails`
(`internal/infra/mpd/client.go:749`) drops any song with no `Album` tag, so the folder never
becomes an album. The artist still appears — MPD's `list artist` sees the `Artist` tag — which is
why the reported symptom was *"the streamer detects the Album artist but does not list the songs."*

---

## Folder 1 — R. Strauss / Karajan · 10 files · `.flac`

**Pi path:** `/mnt/ssd/Music/RStrauss Also Sprach Zarathustra Till Eulenspiegel Karajn VPO__FLAC_352k-24b/`
**MPD URI:** `USB/RStrauss Also Sprach Zarathustra Till Eulenspiegel Karajn VPO__FLAC_352k-24b/`

| Tag | Current | Set to |
|---|---|---|
| `Album` | *(empty)* | `Also sprach Zarathustra / Till Eulenspiegel` |
| `AlbumArtist` | *(empty)* | `Herbert von Karajan` |
| `Artist` | `Herbert von Karajan  Wiener Philharmoniker` | **leave unchanged** |

`Artist` keeps the full ensemble credit (note: two spaces before "Wiener"). Phase 2's collapse rule
reduces it to the first credited performer for display. Setting `AlbumArtist` to the bare
`Herbert von Karajan` now means Phase 2 has nothing to correct here.

```
01 Also sprach Zarathustra - Prelude (Sonnenaufgang).flac
02 Also sprach Zarathustra -Von den Hinterweltlern.flac
03 Also sprach Zarathustra -Von der großen Sehnsucht.flac
04 Also sprach Zarathustra -Von den Freuden und Leidenschaften.flac
05 Also sprach Zarathustra -Das Grablied.flac
06 Also sprach Zarathustra -Von der Wissenschaft.flac
07 Also sprach Zarathustra -Der Genesende.flac
08 Also sprach Zarathustra -Das Tanzlied - Das Nachtlied.flac
09 Also sprach Zarathustra -Das Nachtwandlerlied.flac
10 Till Eulenspiegels lustige Streiche, Op. 28.flac
```

**These 10 are the reliable ones** — FLAC has a well-defined Vorbis comment block. Expect all 10 to
work. This is also the album from the original bug report, so it is the acceptance case for the phase.

---

## Folder 2 — toe (WAV edition) · 4 files · `.wav`

**Pi path:** `/mnt/ssd/Music/toe - The Future Is Now - WAV/`
**MPD URI:** `USB/toe - The Future Is Now - WAV/`

| Tag | Current | Set to |
|---|---|---|
| `Album` | *(empty)* | `The Future Is Now` |
| `AlbumArtist` | *(empty)* | `toe` |
| `Artist` | *(empty)* | `toe` |
| `Date` | *(empty)* | `2012` *(optional, matches the FLAC sibling)* |

**Match these exactly.** The sibling folder `USB/toe - The Future Is Now - FLAC/` already carries
`Album='The Future Is Now'`, `AlbumArtist='toe'`, `Artist='toe'`, `Date='2012'` — verified live. Using
identical values makes the two folders a genuine same-album/different-format pair, which is
**exactly the Phase 3 duplicate-badge test case** (BROWSE-01/02). Any divergence in spelling breaks
that pairing.

```
toe - The Future Is Now - 01 Run For Word.wav
toe - The Future Is Now - 02 月、欠け.wav
toe - The Future Is Now - 03 Ordinary Days.wav
toe - The Future Is Now - 04 The Future Is Now.wav
```

---

## Folder 3 — Singxer SU-6 test signals · 2 files · `.dff` + `.wav`

**Pi path:** `/mnt/ssd/Music/Sigxer SU-6 test/`
**MPD URI:** `USB/Sigxer SU-6 test/`

| Tag | Current | Set to |
|---|---|---|
| `Album` | *(empty)* | `Singxer SU-6 Test` |
| `AlbumArtist` | *(empty)* | `Test Signals` |
| `Artist` | *(empty)* | `Test Signals` |

```
DSD-测试文件 ANNOUNCEMENT FOR BASIC CHECKS (Voice).dff
PCM-测试文件 ANNOUNCEMENT FOR BASIC CHECKS (Voice).wav
```

These are DAC verification tones, not music. If tagging them proves awkward, leaving them invisible
is a perfectly reasonable outcome — see the WAV/DFF warning below.

---

## Three operational warnings

### 1. The SSD is mounted read-only — you cannot tag in place

`/etc/fstab` has `UUID=D43D-7F5C /mnt/ssd exfat ro,nofail,uid=mpd,gid=audio,...`, and the live mount
confirms `ro`. The block device is **not** hardware write-protected (`blockdev --getro /dev/sda1`
returns `0`), so this is a software mount option only.

Two routes:
- **Simplest:** unplug the SSD from the Pi, tag on your Mac, plug it back in.
- **Alternative:** ask and the mount can be flipped to `rw` temporarily — but there is no network
  file share exported from the Pi, so you would still need a way to reach the files.

### 2. macOS will recreate the `._` junk we just deleted

Editing files on an exFAT volume from macOS writes AppleDouble sidecars — that is precisely how the
934 `._` files removed in Plan 01-04 got there. Before ejecting:

```
dot_clean -m /Volumes/<SSD_NAME>
```

If you forget, say so and the cleanup can be re-run. Either way
`deploy/verify-data-integrity.sh` (gates I1/I2/I3) detects it on the Pi.

### 3. 6 of the 16 files are WAV/DFF, where tagging is genuinely unreliable

WAV has no single standard tag format — taggers disagree between an embedded ID3 chunk and RIFF
`INFO` — and DSDIFF (`.dff`) metadata support is weaker still. **MPD may not read back what your
tagger writes**, no matter which tool you use.

- 10 FLAC (Karajan) → reliable, expect success.
- 4 WAV (toe) + 1 WAV + 1 DFF (Singxer) → may not take.

Plan 01-07's verification will show exactly which landed. If the WAV/DFF files do not take, you have
three options, and it is your call:
1. **Convert** them to FLAC (lossless for the WAVs; changes the files).
2. **Accept** them staying invisible — reasonable for the Singxer test tones; less so for the toe
   album, though its FLAC edition is already fully tagged and visible.
3. **Revisit** the folder-name fallback ruled out in D-06 — a deliberate reversal, not a default.

---

## When you are done

Say **"retagged"** and Plan 01-07 will:
1. `mpc update` on the Pi and wait for the rescan,
2. trigger a backend cache rebuild,
3. assert the 3 albums now appear in the library,
4. assert `skippedCount` has dropped from **16 → 0** (D-09's bar),
5. report per-file which tags landed and which did not.

Partial success is fine and expected — 01-07 reports honestly rather than pass/fail on all 16.

---

*Generated for Plan 01-06 · Phase 01 Data Integrity Foundation · requirement DATA-01*
