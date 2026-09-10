#!/usr/bin/env python3
"""Normalise the album-artist tag on the three classical releases that MPD
splits into several albums.

Why the tag differs per format
------------------------------
MPD groups albums on AlbumArtist and falls back to Artist when it is empty
(backend: internal/infra/mpd/client.go groupAlbumDetails). Which tag MPD can
actually see depends on the decoder plugin that claims the file:

  *.wav -> [sndfile].  libsndfile reads the RIFF LIST/INFO chunk and exposes
           title/artist/album/date/track/genre/comment. There is no
           album-artist string in libsndfile's API at all, so no WAV file in
           this library can ever report one -- the trailing ID3 chunk these
           files carry (TPE2 is correct in FR-768) is invisible to MPD, and
           ffmpeg does not surface it either. The only lever is INFO/IART.

  *.dsf -> [dsf].  Reads ID3 via the id3tag plugin, TPE2 -> AlbumArtist.
           Verified against Time Out (Brubeck), which reports one. So the
           lever is TPE2, and per-track TPE1 keeps its own value.

Nothing outside the one tag is touched. WAV files are patched in place inside
the existing INFO chunk when the new value fits, and by rewriting only the
trailing chunk when it does not -- the 2.4 GB data chunks are never copied.
Every run writes a manifest that reverts itself byte for byte.

Applied on the Pi 2026-09-11; the revert manifest for that run is at
~/stellar-backend/retag/retag-20260910T195029Z.json (root-owned). Re-running
is safe -- files already carrying the target value report "unchanged".

  sudo mount -o remount,rw /mnt/ssd
  sudo python3 retag-albumartist.py            # dry run
  sudo python3 retag-albumartist.py --apply
  sudo mount -o remount,ro /mnt/ssd
  mpc rescan "USB/<album dir>"                 # backend rebuilds its cache
                                               # off MPD's database idle event
Revert:
  sudo python3 retag-albumartist.py --revert <manifest.json>

/mnt/ssd is exFAT mounted uid=mpd, so every write needs root as well as the
remount -- as eduardo the open() fails with EACCES even when rw.
"""
import argparse
import base64
import hashlib
import json
import os
import struct
import sys
from datetime import datetime, timezone

MUSIC = "/mnt/ssd/Music"

# album dir -> (kind, album-artist to write). WAV writes RIFF INFO/IART,
# DSF writes ID3 TPE2.
TARGETS = [
    (
        "To Awaken the Sleeper- Works of Joel Thompson",
        "wav",
        "Kansas City Symphony; EXIGENCE Vocal Ensemble; Michael Stern; Joel Thompson",
    ),
    (
        "Beethoven Symphony No. 9 - FR741/DXD 32bit - 2ch",
        "wav",
        "Pittsburgh Symphony Orchestra; Manfred Honeck",
    ),
    (
        "Mahler Symphony No 8 - Busoni Orchestral Works - Horenstein-DSF-11289k-1b",
        "dsf",
        "Jascha Horenstein",
    ),
]


# ---------------------------------------------------------------- RIFF / WAV

def riff_chunks(path):
    """Every top-level chunk, in file order. Returns (riff_size, [chunk])."""
    with open(path, "rb") as fh:
        head = fh.read(12)
        if head[:4] != b"RIFF" or head[8:12] != b"WAVE":
            raise ValueError("not a RIFF/WAVE file: %s" % path)
        riff_size = struct.unpack("<I", head[4:8])[0]
        chunks = []
        while True:
            off = fh.tell()
            hdr = fh.read(8)
            if len(hdr) < 8:
                break
            cid = hdr[:4]
            size = struct.unpack("<I", hdr[4:])[0]
            chunks.append({"id": cid, "off": off, "size": size, "payload": off + 8})
            fh.seek(size + (size & 1), 1)
    return riff_size, chunks


def parse_info(payload):
    """LIST/INFO payload -> [[fourcc, value bytes]] preserving order."""
    if payload[:4] != b"INFO":
        raise ValueError("LIST chunk is not INFO")
    items, p = [], 4
    while p + 8 <= len(payload):
        cid = payload[p:p + 4]
        size = struct.unpack("<I", payload[p + 4:p + 8])[0]
        items.append([cid, payload[p + 8:p + 8 + size]])
        p += 8 + size + (size & 1)
    return items


def build_info(items):
    out = bytearray(b"INFO")
    for cid, val in items:
        out += cid + struct.pack("<I", len(val)) + val
        if len(val) & 1:
            out += b"\x00"
    return bytes(out)


def data_digest(path, payload_off, size):
    h = hashlib.sha256()
    with open(path, "rb") as fh:
        fh.seek(payload_off)
        left = size
        while left:
            block = fh.read(min(8 << 20, left))
            if not block:
                break
            left -= len(block)
            h.update(block)
    return h.hexdigest()


def plan_wav(path, target):
    riff_size, chunks = riff_chunks(path)
    data = next((c for c in chunks if c["id"] == b"data"), None)
    if data is None:
        raise ValueError("no data chunk: %s" % path)

    lists = [c for c in chunks if c["id"] == b"LIST"]
    info = None
    for c in lists:
        with open(path, "rb") as fh:
            fh.seek(c["payload"])
            if fh.read(4) == b"INFO":
                info = c
                break
    if info is None:
        raise ValueError("no LIST/INFO chunk: %s" % path)

    with open(path, "rb") as fh:
        fh.seek(info["payload"])
        payload = fh.read(info["size"])

    items = parse_info(payload)
    iart = next((it for it in items if it[0] == b"IART"), None)
    if iart is None:
        raise ValueError("no INFO/IART field: %s" % path)

    old_val = iart[1]
    old_text = old_val.split(b"\x00")[0].decode("utf-8", "replace")
    new_val = target.encode("utf-8") + b"\x00"
    # Shrinking?  Keep the field's byte length by NUL-padding, so the chunk
    # size never changes and the write stays a pure in-place overwrite.
    if len(new_val) < len(old_val):
        new_val += b"\x00" * (len(old_val) - len(new_val))
    iart[1] = new_val

    new_payload = build_info(items)
    is_last = info is chunks[-1]

    if new_payload == payload:
        mode = "unchanged"
    elif len(new_payload) == info["size"]:
        mode = "inplace"
    elif is_last:
        mode = "tail"
    else:
        raise ValueError(
            "INFO chunk must grow by %d bytes but is not the last chunk: %s"
            % (len(new_payload) - info["size"], path)
        )

    with open(path, "rb") as fh:
        fh.seek(info["off"])
        orig_chunk = fh.read(8 + info["size"] + (info["size"] & 1))

    return {
        "kind": "wav",
        "path": path,
        "mode": mode,
        "old": old_text,
        "new": target,
        "list_off": info["off"],
        "orig_chunk_b64": base64.b64encode(orig_chunk).decode(),
        "orig_file_size": os.path.getsize(path),
        "orig_riff_size": riff_size,
        "data_off": data["payload"],
        "data_size": data["size"],
        "new_payload_b64": base64.b64encode(new_payload).decode(),
    }


def apply_wav(step):
    payload = base64.b64decode(step["new_payload_b64"])
    path = step["path"]
    if step["mode"] == "inplace":
        with open(path, "r+b") as fh:
            fh.seek(step["list_off"] + 8)
            fh.write(payload)
    elif step["mode"] == "tail":
        with open(path, "r+b") as fh:
            fh.seek(step["list_off"])
            fh.write(b"LIST" + struct.pack("<I", len(payload)) + payload)
            if len(payload) & 1:
                fh.write(b"\x00")
            fh.truncate()
            size = fh.tell()
            fh.seek(4)
            fh.write(struct.pack("<I", size - 8))


def revert_wav(step):
    chunk = base64.b64decode(step["orig_chunk_b64"])
    with open(step["path"], "r+b") as fh:
        fh.seek(step["list_off"])
        fh.write(chunk)
        if fh.tell() < step["orig_file_size"]:
            # tail rewrite grew or shrank the file; nothing followed the
            # chunk, so restoring the original length is enough.
            fh.truncate(step["orig_file_size"])
        else:
            fh.truncate(max(fh.tell(), step["orig_file_size"]))
        fh.seek(4)
        fh.write(struct.pack("<I", step["orig_riff_size"]))


# --------------------------------------------------------------------- DSF

def dsf_data_region(path):
    """(offset, size) of the DSD sample payload."""
    with open(path, "rb") as fh:
        if fh.read(4) != b"DSD ":
            raise ValueError("not a DSF file: %s" % path)
        fh.seek(28)
        if fh.read(4) != b"fmt ":
            raise ValueError("no fmt chunk: %s" % path)
        fmt_size = struct.unpack("<Q", fh.read(8))[0]
        fh.seek(28 + fmt_size)
        if fh.read(4) != b"data":
            raise ValueError("no data chunk: %s" % path)
        size = struct.unpack("<Q", fh.read(8))[0]
        return fh.tell(), size - 12


def plan_dsf(path, target):
    from mutagen.dsf import DSF
    tags = DSF(path).tags
    cur = tags.get("TPE2") if tags else None
    cur = str(cur) if cur else ""
    off, size = dsf_data_region(path)
    return {
        "kind": "dsf",
        "path": path,
        "mode": "unchanged" if cur == target else "id3",
        "old": cur,
        "new": target,
        "data_off": off,
        "data_size": size,
    }


def apply_dsf(step):
    from mutagen.dsf import DSF
    from mutagen.id3 import TPE2
    audio = DSF(step["path"])
    if audio.tags is None:
        audio.add_tags()
    audio.tags.setall("TPE2", [TPE2(encoding=3, text=[step["new"]])])
    audio.save()


def revert_dsf(step):
    from mutagen.dsf import DSF
    from mutagen.id3 import TPE2
    audio = DSF(step["path"])
    if audio.tags is None:
        return
    if step["old"]:
        audio.tags.setall("TPE2", [TPE2(encoding=3, text=[step["old"]])])
    else:
        audio.tags.delall("TPE2")
    audio.save()


# --------------------------------------------------------------------- main

def collect():
    steps = []
    for rel, kind, target in TARGETS:
        base = os.path.join(MUSIC, rel)
        if not os.path.isdir(base):
            raise SystemExit("missing album directory: %s" % base)
        names = sorted(n for n in os.listdir(base) if n.lower().endswith("." + kind))
        if not names:
            raise SystemExit("no .%s files in %s" % (kind, base))
        for name in names:
            path = os.path.join(base, name)
            steps.append(plan_wav(path, target) if kind == "wav"
                         else plan_dsf(path, target))
    return steps


def show(steps):
    for s in steps:
        print("%-9s %-9s %s" % (s["mode"], s["kind"], os.path.relpath(s["path"], MUSIC)))
        if s["mode"] != "unchanged":
            print("            - %s" % s["old"])
            print("            + %s" % s["new"])


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--revert", metavar="MANIFEST")
    ap.add_argument("--manifest-dir", default=os.path.expanduser("~/stellar-backend/retag"))
    args = ap.parse_args()

    if args.revert:
        with open(args.revert) as fh:
            manifest = json.load(fh)
        for s in manifest["steps"]:
            if s["mode"] == "unchanged":
                continue
            before = data_digest(s["path"], s["data_off"], s["data_size"])
            (revert_wav if s["kind"] == "wav" else revert_dsf)(s)
            after = data_digest(s["path"], s["data_off"], s["data_size"])
            ok = "OK " if before == after else "AUDIO-CHANGED"
            print("%s reverted %s" % (ok, os.path.relpath(s["path"], MUSIC)))
        return

    steps = collect()
    show(steps)
    changed = [s for s in steps if s["mode"] != "unchanged"]
    print("\n%d file(s) to change, %d already correct" % (len(changed), len(steps) - len(changed)))

    if not args.apply:
        print("dry run -- nothing written. re-run with --apply")
        return
    if not changed:
        return

    os.makedirs(args.manifest_dir, exist_ok=True)
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    manifest_path = os.path.join(args.manifest_dir, "retag-%s.json" % stamp)
    with open(manifest_path, "w") as fh:
        json.dump({"created": stamp, "steps": changed}, fh, indent=1)
    print("manifest: %s" % manifest_path)

    failures = 0
    for s in changed:
        before = data_digest(s["path"], s["data_off"], s["data_size"])
        (apply_wav if s["kind"] == "wav" else apply_dsf)(s)
        if s["kind"] == "dsf":
            # mutagen may move the trailing metadata chunk; re-locate.
            off, size = dsf_data_region(s["path"])
        else:
            off, size = s["data_off"], s["data_size"]
        after = data_digest(s["path"], off, size)
        if before == after:
            print("OK            %s" % os.path.relpath(s["path"], MUSIC))
        else:
            failures += 1
            print("AUDIO CHANGED %s  <-- revert this run" % os.path.relpath(s["path"], MUSIC))
    print("\n%d/%d audio-identical" % (len(changed) - failures, len(changed)))
    if failures:
        sys.exit(1)


if __name__ == "__main__":
    main()
