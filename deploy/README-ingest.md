# Adding music to the Pi — the drop-box workflow

Copy an album into a writable SMB share on the Pi, then run one command. The ingest
script unzips, strips macOS junk, tags anything untagged from MusicBrainz, fetches
cover art, moves the result onto the SSD behind a guarded read-write window, and tells
MPD to index it.

Nothing moves until you run the command. The music master volume (`/mnt/ssd`) stays
mounted read-only at every other moment.

## Using it

1. **Copy in.** From the Mac, mount `smb://stellar.local/Inbox` (user `eduardo`, same
   password as SSH) and drop the album folder or `.zip` into it. One folder per album.
2. **Ingest.** On the Pi:

   ```bash
   ssh eduardo@stellar.local
   stellar-ingest --dry-run     # see exactly what would happen, change nothing
   stellar-ingest               # do it
   ```

3. **Read the report.** Every item comes back as `ingested`, `refused`, or `skipped`,
   with the reason. Successful sources retire to `inbox/.done/`; refused ones stay put
   so you can fix and re-run.

### Flags

| Flag | Effect |
|---|---|
| `--dry-run` | Full analysis — tags, MusicBrainz match, cover art, target path — writes nothing. |
| `--no-network` | Skip MusicBrainz and Cover Art Archive; tag from folder name only. |
| `--item NAME` | Process a single inbox entry instead of everything. |
| `--keep-staging` | Leave the staging copy under `inbox/.staging/` for inspection. |

## What it does, in order

1. **Stage.** Copies the inbox item to a staging dir on the SD card. The original is never
   worked on in place, so a failure leaves your drop untouched.
2. **Strip junk.** Removes `._*` AppleDouble files, `.DS_Store`, `__MACOSX/`, `Thumbs.db`,
   and friends. (Samba also blocks macOS from creating most of these in the share at all —
   see `veto files` below.)
3. **Refuse partials.** Anything matching `*.part`, `*.crdownload`, `*.download`,
   `segment-N` and similar means the copy or download is unfinished. The whole item is
   refused rather than half-ingested.
4. **Extract archives.** `.zip`, `.7z`, `.tar*` are unpacked; a single wrapping directory
   is collapsed away.
5. **Tag.** Files with no `Album` tag get one. The album title is guessed from the folder
   name, cleaned of quality suffixes (`__FLAC_192k-24b`, `-DSF-11289k-1b`, `-096-HD-176-WAV`,
   catalogue prefixes like `HDTT13879`), then looked up on MusicBrainz. A match supplies the
   canonical album title, album artist, release date, and MusicBrainz release ID. See
   *How the MusicBrainz match is decided* below — it refuses to guess, and refusing is a
   normal outcome, not a failure.
6. **Cover art.** If the folder has no `folder.jpg`/`cover.jpg`, the front cover is fetched
   from the Cover Art Archive using the matched release ID. MPD's `albumart` reads
   `folder.jpg` directly, so this is all the artwork plumbing needed.
7. **Land it.** Trap-guarded `mount -o remount,rw /mnt/ssd` → copy → `remount,ro`. The
   remount back to read-only runs on *every* path out, including exceptions.
8. **Index.** `mpc update`. The backend's MPD idle watcher sees the `database` event and
   rebuilds its cache and re-runs enrichment on its own — no backend restart, no manual step.

## Rules it will not break

- **Never overwrites.** If `/mnt/ssd/Music/<Album>` already exists, the item is refused and
  reported. Resolve it by hand.
- **Read-only by default.** The SSD is remounted `rw` only for the seconds it takes to copy,
  and always returns to `ro`. If it somehow does not, the report says so loudly.
- **Verified copies.** File list and per-file sizes are compared source-to-target before the
  source is retired. A mismatch deletes the partial target and refuses the item.
- **Tagging is best-effort, per format.** FLAC gets Vorbis comments; DSF/WAV/DFF get ID3v2
  frames. Any format where the write does not stick is reported rather than silently passed
  over — the audio bitstream itself is never touched, only the metadata block.

## How the MusicBrainz match is decided

Ingesting a folder called `daoud - ok` on 2026-08-19 tagged 14 files as
*Ok ok ok ok ok ok ok* by **The Bombhappies** (2007). The matcher was rewritten in
response, around one fact:

> **A high score is not evidence of a match.** `release:"ok"` returns ten unrelated albums,
> *all* scoring ≥ 90, topped at **100** by the Bombhappies record. And `artist:"..."` in a
> MusicBrainz query is a **scoring term, not a filter** — results are not guaranteed to be by
> the artist you asked for. The credit has to be checked afterwards.

The folder name is read as several progressively weaker queries — `Artist - Album` first,
then the title alone. Each result set is then filtered:

1. **Score ≥ 90** — necessary, nowhere near sufficient.
2. **Corroboration.** A release survives only with positive evidence that it is the album in
   the folder, being *either*
   - the **artist matches** the release's `artist-credit` — the query was constrained and the
     constraint held, so the canonical title may be adopted even when it differs from the
     folder name (this is how a folder named `RStrauss Also Sprach Zarathustra ... VPO`
     could acquire a real album title); *or*
   - the **title matches exactly** after normalisation — the only corroboration available
     when the folder name yields no artist to constrain on.

   Exact equality, never containment: `"ok"` is contained in both `"Ok ok ok ok ok ok ok"`
   and `"OK Computer"`. An artist-less query whose title does not match exactly is rejected,
   and that is precisely the fallback that produced the Bombhappies match.
3. **Ambiguity.** Among the corroborated releases tied at the top score, releases are grouped
   by MusicBrainz **release-group id** — its own answer to "these are the same album".
   More than one group means a genuine ambiguity and the match is refused. (Grouping on
   title+credit instead read the three pressings of *Cannonball & Coltrane* as three
   different albums, because two of them are credited `Cannonball Adderley & John Coltrane`
   and one `Cannonball Adderley Quintet`.)
4. **Reissue tie-break** — Official status, then earliest release date. This only ever runs
   *inside* one release group, which is what it was always designed for: choosing between
   pressings of a single album.

Two behaviours worth knowing:

- **Refusing to guess is the safe branch, not a failure.** With no confident match the album
  title falls back to the folder name — what the human who assembled the album called it —
  and the report says why, listing every query tried. The ingest still completes.
- **A network blip cannot be mistaken for "no such release".** An unreachable MusicBrainz
  (after 3 attempts) aborts the whole search rather than letting the candidate walk advance,
  which would otherwise let a transient failure on a well-constrained query silently degrade
  into a weaker query's answer.
- **Adopting a different title is announced.** When the canonical title differs from the
  folder name, the report carries an `adopted MusicBrainz title '<x>' over folder-derived
  '<y>'` note. That is usually desirable — but it is also the shape a misidentification
  takes, so it is stated out loud rather than only in the tag.

Regression tests: `deploy/test_stellar_ingest.py` (stdlib `unittest`, offline — every payload
is a trimmed capture of a real response). Run from the repo root:

```bash
python3 -m unittest discover -s deploy -p 'test_*.py' -v
```

## Why the copy runs through `sudo`

exFAT has no per-file ownership. The whole volume is mounted `uid=mpd,gid=audio`, so
remounting read-write is necessary but *not* sufficient — an unprivileged process still
cannot create anything under `/mnt/ssd/Music`. For the same reason the copy is
`cp -r --preserve=timestamps`, not `cp -a`: `-a` implies `--preserve=all` including
ownership, which exFAT cannot represent, and the copy fails outright.

## Pi-side install

The script lives at `/home/eduardo/stellar-backend/deploy/stellar-ingest.py`, symlinked to
`/usr/local/bin/stellar-ingest`:

```bash
scp deploy/stellar-ingest.py eduardo@stellar.local:/home/eduardo/stellar-backend/deploy/
ssh eduardo@stellar.local '
  chmod +x /home/eduardo/stellar-backend/deploy/stellar-ingest.py
  sudo ln -sf /home/eduardo/stellar-backend/deploy/stellar-ingest.py /usr/local/bin/stellar-ingest
  mkdir -p ~/inbox/.rejected ~/inbox/.done
'
```

Dependencies already present on the Pi: `python3`, `mutagen`, `mpc`, `unzip`, `p7zip`.

### Samba share

Appended to `/etc/samba/smb.conf` (backup at `/etc/samba/smb.conf.bak-999.2`), alongside
the pre-existing read-only `[Music]` share:

```ini
[Inbox]
   comment = Stellar ingest drop-box (staged, not the music library)
   path = /home/eduardo/inbox
   browseable = yes
   read only = no
   guest ok = no
   valid users = eduardo
   force user = eduardo
   force group = eduardo
   create mask = 0664
   directory mask = 0775
   veto files = /._*/.DS_Store/.AppleDouble/.AppleDesktop/.Trashes/.fseventsd/.Spotlight-V100/__MACOSX/
   delete veto files = yes
```

The share is authenticated (`valid users = eduardo`) rather than guest-writable on purpose:
`stellar-backend` runs as `eduardo`, and `eduardo` has passwordless sudo. The SMB password
is the existing account password, set once with `sudo smbpasswd -a eduardo`.

`veto files` is what actually solves the macOS junk problem — it stops the Finder from
creating `._*` and `.DS_Store` in the share at all, rather than cleaning up afterwards.
This matters because those files are how the library picked up 566 phantom MPD rows before;
the SSD must never be mounted on the Mac for the same reason.

## Troubleshooting

| Symptom | Cause |
|---|---|
| `refused: target already exists` | Album is already on the SSD. Rename or remove by hand. |
| `refused: partial download` | A `segment-N` / `.part` file is present. Finish the download. |
| `no confident MusicBrainz match` | Expected outcome, not an error — the album is filed under the folder name. The reason lists every query tried. Rename the folder to `Artist - Album` and re-run if you want the canonical metadata. |
| `ambiguous: N different releases tie` | Two genuinely different albums scored equally. Rename the folder to disambiguate, or tag it yourself. |
| `MusicBrainz unreachable after 3 attempts` | Network, not metadata. Re-run; nothing was guessed. |
| `adopted MusicBrainz title 'x' over folder-derived 'y'` | Informational. Usually correct, but it is also what a misidentification looks like — worth a glance for short or generic album titles. |
| `WARNING: SSD did not return to read-only` | Investigate before playing anything. `findmnt -no OPTIONS /mnt/ssd` should start with `ro,`. |
| Album lands but does not appear in the UI | Check `mpc stats`; if MPD sees it, the backend cache is the issue — `curl localhost:3000/ready`. |
