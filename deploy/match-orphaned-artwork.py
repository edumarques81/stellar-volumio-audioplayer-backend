#!/usr/bin/env python3
"""
match-orphaned-artwork.py — recover the orphan→album mapping for album artwork
rows whose album id changed underneath them.

Background (see .planning/phases/02-artist-identity-artwork-migration/
02-ORPHAN-PROVENANCE-CORRECTION.md): album identity is md5(albumArtist‖album‖uri),
so any file move re-keys every album. Artwork rows written by
BackfillAlbumArtwork (source='backfill', id '<album_id>_artwork') are then
orphaned. md5 cannot be inverted — but the orphaned FILE is the actual cover
image, so the mapping is recoverable by comparing pictures.

Method: 64-bit dHash + Hamming distance, with the threshold CALIBRATED against
albums that still have a valid artwork link (known-good pairs) rather than
guessed. A uniqueness margin rejects ambiguous best-matches instead of guessing.

DRY RUN BY DEFAULT — writes nothing. Emits a report for human approval.

Zero external network calls: /albumart is a local backend endpoint reading MPD
and local files. Nothing here contacts Fanart.tv, Deezer, or MusicBrainz.
"""

import argparse
import json
import os
import sqlite3
import sys
import urllib.parse
import urllib.request
from io import BytesIO

try:
    from PIL import Image
except ImportError:
    sys.exit("PIL/Pillow is required (Pi has 9.4.0). pip install Pillow")

HASH_SIZE = 8  # 8x8 dHash -> 64 bits


def dhash(data: bytes, size: int = HASH_SIZE):
    """64-bit difference hash. Resolution/scan independent, which matters:
    orphans are Cover Art Archive scans, /albumart returns embedded art."""
    try:
        img = Image.open(BytesIO(data)).convert("L").resize(
            (size + 1, size), Image.LANCZOS)
    except Exception:
        return None
    px = list(img.getdata())
    bits = 0
    for r in range(size):
        row = r * (size + 1)
        for c in range(size):
            bits = (bits << 1) | (1 if px[row + c] > px[row + c + 1] else 0)
    return bits


def hamming(a, b):
    return bin(a ^ b).count("1")


def fetch_albumart(base, uri, timeout=20):
    url = base + "/albumart?path=" + urllib.parse.quote(uri)
    try:
        with urllib.request.urlopen(url, timeout=timeout) as r:
            if r.status != 200:
                return None
            return r.read()
    except Exception:
        return None


def read_file(path):
    try:
        with open(path, "rb") as f:
            return f.read()
    except Exception:
        return None


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", default=os.path.expanduser(
        "~/stellar-backend/data/library.db"))
    ap.add_argument("--base", default="http://localhost:3000")
    ap.add_argument("--json-out", default="")
    ap.add_argument("--margin", type=int, default=6,
                    help="best match must beat runner-up by at least this many bits")
    args = ap.parse_args()

    con = sqlite3.connect(args.db)
    con.row_factory = sqlite3.Row

    # ---- calibration set: albums that still have a VALID artwork link -------
    cal = con.execute("""
        SELECT al.id, al.title, al.album_artist, al.first_track, w.file_path
        FROM albums al JOIN artwork w ON w.id = al.artwork_id
        WHERE w.type='album' AND w.file_path LIKE '/home/%'
    """).fetchall()

    # ---- orphans: album-artwork rows with no owning album ------------------
    orphans = con.execute("""
        SELECT a.id, a.file_path, a.source
        FROM artwork a
        WHERE a.type='album'
          AND NOT EXISTS (SELECT 1 FROM albums al WHERE al.id||'_artwork' = a.id)
    """).fetchall()

    # ---- candidates: albums with no artwork link ---------------------------
    unlinked = con.execute("""
        SELECT id, title, album_artist, first_track FROM albums
        WHERE (artwork_id IS NULL OR artwork_id='') AND first_track != ''
    """).fetchall()

    print(f"calibration pairs : {len(cal)}")
    print(f"orphan artwork    : {len(orphans)}")
    print(f"unlinked albums   : {len(unlinked)}")
    print()

    # ---- CALIBRATION -------------------------------------------------------
    # same-album  = dhash(linked artwork file) vs dhash(/albumart of that album)
    # cross-album = the same file vs a DIFFERENT album's /albumart
    same, cal_pairs = [], []
    for r in cal:
        fh = dhash(read_file(r["file_path"]) or b"")
        ah = dhash(fetch_albumart(args.base, r["first_track"]) or b"")
        if fh is None or ah is None:
            continue
        cal_pairs.append((r["id"], fh, ah, r["title"]))
        same.append(hamming(fh, ah))

    cross = []
    for i, (_, fh, _, _) in enumerate(cal_pairs):
        for j, (_, _, ah, _) in enumerate(cal_pairs):
            if i != j:
                cross.append(hamming(fh, ah))

    if not same:
        sys.exit("CALIBRATION FAILED: no usable known-good pairs — cannot pick a "
                 "threshold. Refusing to guess.")

    same.sort()
    cross.sort()

    def pct(v, p):
        return v[min(len(v) - 1, int(len(v) * p))]

    print("=== CALIBRATION (Hamming distance, 64-bit dHash) ===")
    print(f"  same-album  n={len(same):4d}  min={same[0]:2d}  p50={pct(same,.50):2d}"
          f"  p90={pct(same,.90):2d}  p95={pct(same,.95):2d}  max={same[-1]:2d}")
    print(f"  cross-album n={len(cross):4d}  min={cross[0]:2d}  p05={pct(cross,.05):2d}"
          f"  p50={pct(cross,.50):2d}  max={cross[-1]:2d}")

    # Threshold: generous to same-album (p95) but strictly below where
    # cross-album pairs start appearing. If those overlap, the corpus cannot
    # support automatic matching and we say so instead of guessing.
    thr_hi = pct(same, .95)
    thr_lo_cross = cross[0]
    if thr_hi >= thr_lo_cross:
        print(f"\n  !! OVERLAP: same-album p95 ({thr_hi}) >= cross-album min "
              f"({thr_lo_cross}).")
        threshold = max(0, thr_lo_cross - 1)
        print(f"  Falling back to a conservative threshold of {threshold} "
              f"(cross-min minus 1). Expect fewer auto-matches.")
    else:
        threshold = thr_hi
    print(f"  CHOSEN THRESHOLD: {threshold} bits (margin required: {args.margin})")
    print()

    # ---- hash orphans + candidates ----------------------------------------
    oh = []
    for r in orphans:
        h = dhash(read_file(r["file_path"]) or b"")
        oh.append((r["id"], r["file_path"], h))

    ch = []
    for r in unlinked:
        h = dhash(fetch_albumart(args.base, r["first_track"]) or b"")
        ch.append((r["id"], r["title"], r["album_artist"], h))

    n_bad_o = sum(1 for _, _, h in oh if h is None)
    n_bad_c = sum(1 for _, _, _, h in ch if h is None)
    if n_bad_o or n_bad_c:
        print(f"  note: {n_bad_o} orphan files and {n_bad_c} album covers "
              f"could not be hashed (unreadable / no art)")

    # ---- match with uniqueness guard --------------------------------------
    matched, ambiguous, unmatched = [], [], []
    for oid, opath, h in oh:
        if h is None:
            unmatched.append((oid, opath, "orphan image unreadable"))
            continue
        scored = sorted(
            ((hamming(h, c[3]), c) for c in ch if c[3] is not None),
            key=lambda t: t[0])
        if not scored:
            unmatched.append((oid, opath, "no hashable candidates"))
            continue
        best_d, best = scored[0]
        run_d = scored[1][0] if len(scored) > 1 else 999
        rec = {
            "orphan_artwork_id": oid,
            "orphan_file": opath,
            "album_id": best[0],
            "album_title": best[1],
            "album_artist": best[2],
            "distance": best_d,
            "runner_up": run_d,
            "margin": run_d - best_d,
        }
        if best_d > threshold:
            unmatched.append((oid, opath, f"best distance {best_d} > threshold {threshold}"))
        elif (run_d - best_d) < args.margin:
            ambiguous.append(rec)
        else:
            matched.append(rec)

    print("=== DRY RUN — NOTHING WRITTEN ===")
    print(f"  MATCHED   : {len(matched)}")
    print(f"  AMBIGUOUS : {len(ambiguous)}  (best match too close to runner-up)")
    print(f"  UNMATCHED : {len(unmatched)}")
    print()
    if matched:
        print("--- proposed links ---")
        print(f"{'orphan':10} {'d':>3} {'run':>4} {'mrg':>4}  album")
        for m in sorted(matched, key=lambda r: r["distance"]):
            print(f"{m['orphan_artwork_id'][:8]:10} {m['distance']:3d} "
                  f"{m['runner_up']:4d} {m['margin']:4d}  "
                  f"{m['album_title'][:44]} — {m['album_artist'][:26]}")
    if ambiguous:
        print("\n--- ambiguous (needs human eyes, NOT auto-applied) ---")
        for m in sorted(ambiguous, key=lambda r: r["distance"]):
            print(f"{m['orphan_artwork_id'][:8]:10} {m['distance']:3d} "
                  f"{m['runner_up']:4d} {m['margin']:4d}  "
                  f"{m['album_title'][:44]} — {m['album_artist'][:26]}")

    if args.json_out:
        with open(args.json_out, "w") as f:
            json.dump({
                "threshold": threshold, "margin": args.margin,
                "calibration": {
                    "same_min": same[0], "same_p95": pct(same, .95), "same_max": same[-1],
                    "cross_min": cross[0] if cross else None,
                    "cross_p50": pct(cross, .50) if cross else None,
                },
                "matched": matched, "ambiguous": ambiguous,
                "unmatched": [{"orphan_artwork_id": a, "orphan_file": b, "reason": c}
                              for a, b, c in unmatched],
            }, f, indent=2)
        print(f"\nreport written: {args.json_out}")


if __name__ == "__main__":
    main()
