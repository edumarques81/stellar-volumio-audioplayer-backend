#!/usr/bin/env python3
"""Re-encode non-UTF-8 strings in a WAV's RIFF LIST/INFO chunk to UTF-8.

Why this exists
---------------
Some commercially mastered WAVs (Reference Recordings HRx, for one) store INFO
strings in Windows-1252 rather than UTF-8, so `Études-tableaux` is a lone 0xC9
byte. That byte is not valid UTF-8 on its own, MPD replaces it with `?`, and
every client in the chain shows `?tudes-tableaux`.

What it does
------------
Rewrites only the LIST/INFO chunk. In these files LIST sits *after* the `data`
chunk, so the multi-hundred-MB audio payload is never read or copied: the tool
seeks to the LIST offset and rewrites from there to EOF. Chunk growth is
absorbed by shifting the trailing chunks (an `id3 ` blob these files carry and
MPD cannot see anyway) and fixing the RIFF size field.

Safety
------
* Dry run by default; `--apply` is required to write.
* Refuses any file whose LIST chunk starts before the end of `data`, because
  that would mean copying the audio.
* Records the exact original bytes from the LIST offset to EOF in a manifest,
  so `--revert <manifest>` restores the file byte for byte.
* Hashes the head and tail of the `data` payload before and after and compares
  the chunk table, so a write that somehow touched audio is caught.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import struct
import sys
import time

PROBE = 1 << 20  # bytes of the audio payload hashed at each end as a tripwire


def read_chunks(f, size):
    """Yield (fourcc, offset_of_header, payload_len) for each top-level chunk."""
    f.seek(0)
    hdr = f.read(12)
    if hdr[0:4] != b"RIFF" or hdr[8:12] != b"WAVE":
        raise ValueError("not a RIFF/WAVE file")
    pos = 12
    out = []
    while pos + 8 <= size:
        f.seek(pos)
        ch = f.read(8)
        if len(ch) < 8:
            break
        cid = ch[0:4]
        clen = struct.unpack("<I", ch[4:8])[0]
        out.append((cid, pos, clen))
        pos += 8 + clen + (clen & 1)
    return out, pos


def parse_info(body):
    """Split an INFO list body into [(fourcc, value_bytes), ...]."""
    if body[0:4] != b"INFO":
        raise ValueError("LIST is not of type INFO")
    fields = []
    pos = 4
    while pos + 8 <= len(body):
        fid = body[pos : pos + 4]
        flen = struct.unpack("<I", body[pos + 4 : pos + 8])[0]
        val = body[pos + 8 : pos + 8 + flen]
        fields.append((fid, val))
        pos += 8 + flen + (flen & 1)
    return fields


def build_info(fields):
    out = bytearray(b"INFO")
    for fid, val in fields:
        out += fid + struct.pack("<I", len(val)) + val
        if len(val) & 1:
            out += b"\x00"
    return bytes(out)


def reencode(val):
    """Return (new_value, changed). Latin-1/CP1252 -> UTF-8, NUL padding kept."""
    core = val.rstrip(b"\x00")
    pad = len(val) - len(core)
    try:
        core.decode("utf-8")
        return val, False
    except UnicodeDecodeError:
        pass
    try:
        text = core.decode("cp1252")
    except UnicodeDecodeError:
        return val, False
    fixed = text.encode("utf-8")
    # Keep at least one terminating NUL, as the originals do.
    return fixed + b"\x00" * max(pad, 1), True


def probe_hashes(f, off, length):
    h = hashlib.sha256()
    f.seek(off)
    h.update(f.read(min(PROBE, length)))
    if length > PROBE:
        f.seek(off + length - PROBE)
        h.update(f.read(PROBE))
    return h.hexdigest()


def plan(path):
    size = os.path.getsize(path)
    with open(path, "rb") as f:
        chunks, end = read_chunks(f, size)
        by_id = {c[0]: c for c in chunks}
        if b"data" not in by_id:
            raise ValueError("no data chunk")
        if b"LIST" not in by_id:
            return None
        _, data_off, data_len = by_id[b"data"]
        lid, list_off, list_len = by_id[b"LIST"]
        data_end = data_off + 8 + data_len + (data_len & 1)
        if list_off < data_end:
            raise ValueError(
                f"LIST at {list_off} precedes end of data at {data_end}; "
                "rewriting it would copy the audio payload — refusing"
            )
        f.seek(list_off + 8)
        body = f.read(list_len)
        fields = parse_info(body)
        changes = []
        new_fields = []
        for fid, val in fields:
            nv, changed = reencode(val)
            if changed:
                changes.append(
                    (fid.decode("ascii", "replace"),
                     val.rstrip(b"\x00").decode("cp1252"))
                )
            new_fields.append((fid, nv))
        if not changes:
            return None
        new_body = build_info(new_fields)
        f.seek(list_off)
        tail = f.read()  # LIST header through EOF
        new_tail = (
            b"LIST" + struct.pack("<I", len(new_body)) + new_body
            + (b"\x00" if len(new_body) & 1 else b"")
            + tail[8 + list_len + (list_len & 1):]
        )
        return {
            "path": path,
            "size": size,
            "list_off": list_off,
            "data_off": data_off + 8,
            "data_len": data_len,
            "changes": changes,
            "old_tail": tail,
            "new_tail": new_tail,
            "probe": probe_hashes(f, data_off + 8, data_len),
        }


def apply(p):
    path, off = p["path"], p["list_off"]
    new_size = off + len(p["new_tail"])
    with open(path, "r+b") as f:
        f.seek(off)
        f.write(p["new_tail"])
        f.truncate(new_size)
        f.seek(4)
        f.write(struct.pack("<I", new_size - 8))
        f.flush()
        os.fsync(f.fileno())
    # Verify: chunk table intact, audio untouched.
    with open(path, "rb") as f:
        chunks, _ = read_chunks(f, os.path.getsize(path))
        by_id = {c[0]: c for c in chunks}
        _, d_off, d_len = by_id[b"data"]
        if d_off + 8 != p["data_off"] or d_len != p["data_len"]:
            raise SystemExit(f"FATAL: data chunk moved in {path}")
        if probe_hashes(f, d_off + 8, d_len) != p["probe"]:
            raise SystemExit(f"FATAL: audio payload changed in {path}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("files", nargs="*")
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--revert", metavar="MANIFEST")
    ap.add_argument("--manifest-dir", default=".")
    args = ap.parse_args()

    if args.revert:
        with open(args.revert) as fh:
            man = json.load(fh)
        for e in man["entries"]:
            with open(e["path"], "r+b") as f:
                f.seek(e["list_off"])
                f.write(base64.b64decode(e["old_tail"]))
                f.truncate(e["list_off"] + len(base64.b64decode(e["old_tail"])))
                f.seek(4)
                f.write(struct.pack("<I", e["size"] - 8))
            print(f"reverted {e['path']}")
        return

    plans = []
    for path in args.files:
        try:
            p = plan(path)
        except ValueError as exc:
            print(f"SKIP  {path}: {exc}")
            continue
        if p is None:
            print(f"ok    {os.path.basename(path)}: nothing to fix")
            continue
        plans.append(p)
        delta = len(p["new_tail"]) - len(p["old_tail"])
        print(f"FIX   {os.path.basename(path)}  (+{delta} bytes)")
        for fid, text in p["changes"]:
            print(f"        {fid}: {text!r}")

    if not plans:
        return
    if not args.apply:
        print(f"\n{len(plans)} file(s) would change. Re-run with --apply.")
        return

    stamp = time.strftime("%Y%m%d-%H%M%S")
    mpath = os.path.join(args.manifest_dir, f"wav-info-encoding-{stamp}.json")
    man = {
        "created": stamp,
        "entries": [
            {
                "path": p["path"],
                "size": p["size"],
                "list_off": p["list_off"],
                "old_tail": base64.b64encode(p["old_tail"]).decode("ascii"),
            }
            for p in plans
        ],
    }
    with open(mpath, "w") as fh:
        json.dump(man, fh)
    print(f"manifest: {mpath}")

    for p in plans:
        apply(p)
        print(f"wrote {p['path']}")
    print(f"\nRevert with: {sys.argv[0]} --revert {mpath}")


if __name__ == "__main__":
    main()
