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
   name, cleaned of quality suffixes (`__FLAC_192k-24b`, `_DSD64`, catalogue prefixes),
   then looked up on MusicBrainz. A match at score ≥ 90 supplies the canonical album
   title, album artist, release date, and MusicBrainz release ID. Ties between reissues
   break toward Official status and the earliest release date.
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
| `no confident MusicBrainz match` | Folder name is too far from the release title. Rename the folder to `Artist - Album` and re-run, or tag it yourself. |
| `WARNING: SSD did not return to read-only` | Investigate before playing anything. `findmnt -no OPTIONS /mnt/ssd` should start with `ro,`. |
| Album lands but does not appear in the UI | Check `mpc stats`; if MPD sees it, the backend cache is the issue — `curl localhost:3000/ready`. |
