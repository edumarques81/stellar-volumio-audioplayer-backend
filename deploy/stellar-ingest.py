#!/usr/bin/env python3
"""stellar-ingest — stage music from the Samba drop-box onto the read-only SSD.

Runs on the Pi. Nothing moves until this is invoked; dropping files into the
Samba [Inbox] share is inert on its own.

Pipeline, per item in the inbox:

    extract archives -> strip macOS junk -> refuse partial downloads
    -> read tags -> fill missing ones from MusicBrainz -> fetch cover art
    -> prove the audio bitstream is untouched -> collision-check the SSD
    -> trap-guarded remount,rw -> copy -> verify -> remount,ro
    -> mpc update

Design constraints this script exists to honour:

  * /mnt/ssd holds irreplaceable masters and is mounted ro. The only write
    window is a guarded remount, and it is held open for the copy alone --
    all tagging happens in the inbox on the SD card, so a master is never
    modified in place.
  * Never overwrite. A target path that already exists means the item is
    refused and reported, not merged.
  * Every format gets a tagging attempt. Nothing is refused for being a
    format we're pessimistic about; instead we re-read after writing and
    report what did not stick.
  * mutagen rewrites only the metadata block. We verify that claim per file
    with a decoded-stream MD5 rather than trusting it.

Usage:
    stellar-ingest.py                 # ingest everything staged in the inbox
    stellar-ingest.py --dry-run       # full pipeline, no writes to the SSD
    stellar-ingest.py --no-network    # skip MusicBrainz/Cover Art Archive
    stellar-ingest.py --item NAME     # ingest a single inbox entry
"""

from __future__ import annotations

import argparse
import json
import re
import shutil
import subprocess
import sys
import time
import unicodedata
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path

# --------------------------------------------------------------------------
# Configuration
# --------------------------------------------------------------------------

INBOX = Path("/home/eduardo/inbox")
REJECTED = INBOX / ".rejected"
MUSIC_ROOT = Path("/mnt/ssd/Music")
SSD_MOUNT = "/mnt/ssd"

AUDIO_EXTS = {".flac", ".dsf", ".wav", ".dff", ".aiff", ".aif", ".mp3", ".m4a", ".ape", ".wv"}
ARCHIVE_EXTS = {".zip", ".7z", ".rar", ".tar", ".tgz", ".gz", ".bz2", ".xz"}
ART_NAMES = ("folder.jpg", "cover.jpg", "front.jpg", "folder.png", "cover.png")

# macOS / Windows droppings. Samba vetoes most of these at the door, but an
# archive extracted on the Pi can still carry them.
JUNK_NAMES = {".DS_Store", "Thumbs.db", "desktop.ini", ".localized"}
JUNK_DIRS = {"__MACOSX", ".AppleDouble", ".Trashes", ".fseventsd", ".Spotlight-V100"}
JUNK_PREFIXES = ("._",)

# A download that was never finished. Refusing these is the whole reason the
# trigger is explicit -- see the 38GB of Qobuz `segment-N` files that had
# quietly accumulated inside the MPD root before this script existed.
PARTIAL_RE = re.compile(r"^(segment-\d+|.*\.(part|crdownload|download|partial|!ut))$", re.I)

MB_ROOT = "https://musicbrainz.org/ws/2"
CAA_ROOT = "https://coverartarchive.org"
USER_AGENT = "stellar-ingest/1.0 ( https://github.com/edumarques81/stellar-volumio-audioplayer-backend )"
MB_RATE_LIMIT_S = 1.1  # MusicBrainz allows 1 req/s; leave headroom.
HTTP_TIMEOUT_S = 20

try:
    import mutagen
    from mutagen.flac import FLAC, Picture
    from mutagen.id3 import ID3, TALB, TPE1, TPE2, TIT2, TDRC, TRCK, TXXX
except ImportError:  # pragma: no cover - environment guard
    sys.exit("stellar-ingest: python3-mutagen is required (apt install python3-mutagen)")


# --------------------------------------------------------------------------
# Reporting
# --------------------------------------------------------------------------

@dataclass
class ItemReport:
    name: str
    status: str = "pending"          # ingested | would-ingest | refused | skipped
    reason: str = ""
    target: str = ""
    audio_files: int = 0
    tagged: list[str] = field(default_factory=list)
    tag_failures: list[str] = field(default_factory=list)
    md5_mismatches: list[str] = field(default_factory=list)
    mb_release: str = ""
    art: str = ""
    notes: list[str] = field(default_factory=list)


# In --json mode stdout carries the report document and nothing else, so the
# running commentary is diverted to stderr. Callers that parse us (the backend's
# ingest service) can therefore read stdout blind.
JSON_MODE = False


def log(msg: str = "") -> None:
    print(msg, file=sys.stderr if JSON_MODE else sys.stdout, flush=True)


def run(cmd: list[str], check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, capture_output=True, text=True, check=check)


# --------------------------------------------------------------------------
# Junk / partial handling
# --------------------------------------------------------------------------

def strip_junk(root: Path) -> int:
    """Remove macOS and Windows metadata droppings. Returns count removed."""
    removed = 0
    for path in sorted(root.rglob("*"), key=lambda p: len(p.parts), reverse=True):
        name = path.name
        if path.is_dir() and name in JUNK_DIRS:
            shutil.rmtree(path, ignore_errors=True)
            removed += 1
        elif path.is_file() and (name in JUNK_NAMES or name.startswith(JUNK_PREFIXES)):
            path.unlink(missing_ok=True)
            removed += 1
    return removed


def find_partials(root: Path) -> list[Path]:
    return [p for p in root.rglob("*") if p.is_file() and PARTIAL_RE.match(p.name)]


def extract_archive(archive: Path, dest: Path) -> bool:
    dest.mkdir(parents=True, exist_ok=True)
    suffix = archive.suffix.lower()
    if suffix == ".zip":
        cmd = ["unzip", "-q", "-o", str(archive), "-d", str(dest)]
    elif suffix in {".7z", ".rar"}:
        cmd = ["7z", "x", "-y", f"-o{dest}", str(archive)]
    else:
        cmd = ["tar", "-xf", str(archive), "-C", str(dest)]
    proc = run(cmd, check=False)
    if proc.returncode != 0:
        log(f"    extract failed: {proc.stderr.strip()[:200]}")
        return False
    return True


def collapse_single_dir(root: Path) -> Path:
    """An archive of `Album/` extracts to `staging/Album/`. Descend into it."""
    while True:
        entries = [p for p in root.iterdir() if not p.name.startswith(".")]
        if len(entries) == 1 and entries[0].is_dir():
            root = entries[0]
        else:
            return root


# --------------------------------------------------------------------------
# Tag reading and writing
# --------------------------------------------------------------------------

FIELDS = ("album", "albumartist", "artist", "title", "date", "tracknumber")

ID3_FRAMES = {
    "album": TALB,
    "artist": TPE1,
    "albumartist": TPE2,
    "title": TIT2,
    "date": TDRC,
    "tracknumber": TRCK,
}


def read_tags(path: Path) -> dict[str, str]:
    """Read the fields we care about, normalised across containers."""
    try:
        audio = mutagen.File(path)
    except Exception:
        return {}
    if audio is None or audio.tags is None:
        return {}

    out: dict[str, str] = {}
    if isinstance(audio, FLAC) or hasattr(audio.tags, "get") and not isinstance(audio.tags, ID3):
        for field_name in FIELDS:
            value = audio.tags.get(field_name) or audio.tags.get(field_name.upper())
            if value:
                out[field_name] = str(value[0]) if isinstance(value, list) else str(value)
    else:
        for field_name, frame in ID3_FRAMES.items():
            frames = audio.tags.getall(frame.__name__)
            if frames and frames[0].text:
                out[field_name] = str(frames[0].text[0])
    return out


def write_tags(path: Path, tags: dict[str, str]) -> tuple[bool, str]:
    """Write tags in whatever the container supports. Returns (ok, error)."""
    try:
        audio = mutagen.File(path)
        if audio is None:
            return False, "unrecognised container"

        if isinstance(audio, FLAC):
            for key, value in tags.items():
                audio[key] = value
            audio.save()
            return True, ""

        # Everything else in this library (DSF, WAV, DFF) carries ID3.
        if audio.tags is None:
            try:
                audio.add_tags()
            except Exception as exc:
                return False, f"cannot add ID3 container: {exc}"
        for key, value in tags.items():
            frame = ID3_FRAMES.get(key)
            if frame is None:
                audio.tags.add(TXXX(encoding=3, desc=key, text=value))
            else:
                audio.tags.add(frame(encoding=3, text=value))
        audio.save()
        return True, ""
    except Exception as exc:
        return False, str(exc)


def decoded_md5(path: Path) -> str:
    """MD5 of the decoded audio stream -- proves the bitstream is untouched.

    -nostdin matters: ffmpeg reads stdin by default and will eat a caller's
    input if this is ever driven from a shell loop.
    """
    proc = run(
        ["ffmpeg", "-nostdin", "-v", "error", "-i", str(path), "-map", "0:a", "-f", "md5", "-"],
        check=False,
    )
    if proc.returncode != 0:
        return ""
    return proc.stdout.strip()


# --------------------------------------------------------------------------
# MusicBrainz + Cover Art Archive
#
# Mirrors internal/infra/enrichment/{musicbrainz,caa}.go: search a release by
# (artist, album), take the MBID, pull the front cover. The difference is that
# the Go path looks up art for albums that are already tagged, whereas here we
# also adopt the canonical release *title* -- which the Go SearchRelease
# currently discards even though MBRelease carries it.
# --------------------------------------------------------------------------

_last_mb_call = 0.0


def _http_json(url: str) -> dict | None:
    global _last_mb_call
    wait = MB_RATE_LIMIT_S - (time.monotonic() - _last_mb_call)
    if wait > 0:
        time.sleep(wait)
    _last_mb_call = time.monotonic()

    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT, "Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=HTTP_TIMEOUT_S) as resp:
            return json.load(resp)
    except Exception:
        return None


QUALITY_TAIL_RE = re.compile(
    r"[\s\-_]*(?:__)?(?:"
    r"flac|dsf|dff|wav|aiff|alac|ape|"
    r"\d{2,4}k(?:hz)?|\d{1,2}b(?:it)?|\d{2,3}-\d{2,3}|hd|hi-?res|dsd\d*|sacd|remaster(?:ed)?"
    r")\b",
    re.I,
)
CATALOGUE_PREFIX_RE = re.compile(r"^[A-Z]{2,6}\d{3,6}[\s\-_]+")


def candidate_album_title(folder_name: str) -> str:
    """Derive a searchable album title from a folder name.

    MusicBrainz has no way to identify an untagged file -- SearchRelease is a
    lookup keyed on metadata you already hold, not a fingerprint. The folder
    name is the only seed available, so clean the quality decorations off it:

        RStrauss Also Sprach Zarathustra ... VPO__FLAC_352k-24b
            -> RStrauss Also Sprach Zarathustra ... VPO
        HDTT13879 Miles Davis - Kind Of Blue-DSF-11289k-1b
            -> Miles Davis - Kind Of Blue
    """
    name = folder_name
    name = name.split("__")[0]
    name = CATALOGUE_PREFIX_RE.sub("", name)
    prev = None
    while prev != name:
        prev = name
        name = QUALITY_TAIL_RE.sub(" ", name).strip(" -_")
    name = re.sub(r"[\s_]+", " ", name).strip(" -_")
    return name


def candidate_queries(folder_name: str, artist_tag: str) -> list[tuple[str, str]]:
    """Build (artist, album) search candidates from a folder name.

    Folder naming in this library is inconsistent, and the dominant convention
    is `Artist - Album`. Searching the whole string as a release title finds
    nothing -- `release:"Miles Davis - Kind Of Blue"` returns zero results
    while `release:"Kind Of Blue" AND artist:"Miles Davis"` scores 100. So
    emit several readings of the name, most specific first.
    """
    cleaned = candidate_album_title(folder_name)
    candidates: list[tuple[str, str]] = []

    def add(artist: str, album: str) -> None:
        album = album.strip(" -_")
        artist = artist.strip(" -_")
        if album and (artist, album) not in candidates:
            candidates.append((artist, album))

    add(artist_tag, cleaned)
    # `Artist - Album` (and `Artist - Album - Extra`): treat the first segment
    # as the artist and the remainder as the title, then the reverse split.
    if " - " in cleaned:
        head, _, tail = cleaned.partition(" - ")
        add(head, tail)
        add(artist_tag, tail)
        add("", tail)
    add("", cleaned)
    return candidates[:4]  # cap the rate-limited round trips per album


def _release_sort_key(release: dict) -> tuple:
    """Rank releases: score desc, Official first, earliest date first.

    Reissues of a well-known album all score 100, so score alone breaks ties
    arbitrarily. Preferring the earliest Official pressing gives a stable
    answer and a `date` worth writing into the tag.
    """
    date = release.get("date") or "9999"
    return (
        -release.get("score", 0),
        0 if release.get("status") == "Official" else 1,
        date,
    )


def search_release(artist: str, album: str, folder_name: str = "") -> dict | None:
    """Return the best MusicBrainz release across candidate readings, or None."""
    candidates = candidate_queries(folder_name or album, artist) if folder_name else [(artist, album)]

    for cand_artist, cand_album in candidates:
        terms = [f'release:"{escape_lucene(cand_album)}"']
        if cand_artist:
            terms.append(f'artist:"{escape_lucene(cand_artist)}"')
        url = f"{MB_ROOT}/release?query={urllib.parse.quote(' AND '.join(terms))}&fmt=json&limit=10"
        releases = (_http_json(url) or {}).get("releases") or []
        releases = [r for r in releases if r.get("score", 0) >= 90]
        if releases:
            return sorted(releases, key=_release_sort_key)[0]
    return None


def escape_lucene(text: str) -> str:
    return re.sub(r'([+\-!(){}\[\]^"~*?:\\/]|&&|\|\|)', r"\\\1", text)


def fetch_front_cover(mbid: str) -> bytes | None:
    req = urllib.request.Request(
        f"{CAA_ROOT}/release/{mbid}/front-500", headers={"User-Agent": USER_AGENT}
    )
    try:
        with urllib.request.urlopen(req, timeout=HTTP_TIMEOUT_S) as resp:
            data = resp.read()
            return data if data and len(data) > 1024 else None
    except Exception:
        return None


# --------------------------------------------------------------------------
# SSD write window
# --------------------------------------------------------------------------

def remount(mode: str) -> bool:
    proc = run(["sudo", "mount", "-o", f"remount,{mode}", SSD_MOUNT], check=False)
    if proc.returncode != 0:
        log(f"    remount,{mode} failed: {proc.stderr.strip()}")
        return False
    opts = run(["findmnt", "-no", "OPTIONS", SSD_MOUNT], check=False).stdout.strip()
    actual = opts.split(",")[0]
    if actual != mode:
        log(f"    remount,{mode} did not take (mount is {actual})")
        return False
    return True


def copy_to_ssd(source: Path, target: Path) -> tuple[bool, str]:
    """Copy inside a guarded write window. Verifies before declaring success.

    The copy runs through sudo because exFAT has no per-file ownership: the
    whole volume is mounted uid=mpd,gid=audio, so remounting rw is necessary
    but not sufficient -- an unprivileged process still cannot create anything
    under /mnt/ssd/Music.
    """
    if not remount("rw"):
        return False, "could not open a write window on the SSD"
    try:
        # -r --preserve=timestamps, not -a: exFAT stores no ownership, so
        # preserving it is both impossible and fatal to the copy.
        proc = run(["sudo", "cp", "-r", "--preserve=timestamps", str(source), str(target)],
                   check=False)
        if proc.returncode != 0:
            run(["sudo", "rm", "-rf", str(target)], check=False)
            return False, f"copy failed: {proc.stderr.strip()[:200]}"

        src_files = sorted(p.relative_to(source) for p in source.rglob("*") if p.is_file())
        dst_files = sorted(p.relative_to(target) for p in target.rglob("*") if p.is_file())
        if src_files != dst_files:
            run(["sudo", "rm", "-rf", str(target)], check=False)
            return False, "copy verification failed (file list differs)"
        for rel in src_files:
            if (source / rel).stat().st_size != (target / rel).stat().st_size:
                run(["sudo", "rm", "-rf", str(target)], check=False)
                return False, f"copy verification failed (size differs: {rel})"
        return True, ""
    except Exception as exc:
        run(["sudo", "rm", "-rf", str(target)], check=False)
        return False, f"copy failed: {exc}"
    finally:
        # Always close the window, on every path out -- including the raise.
        if not remount("ro"):
            log("    WARNING: SSD did not return to read-only; investigate before playback")


# --------------------------------------------------------------------------
# Per-item pipeline
# --------------------------------------------------------------------------

def safe_dirname(name: str) -> str:
    name = unicodedata.normalize("NFC", name)
    name = re.sub(r'[/\\:*?"<>|]', "-", name).strip(". ")
    return name or "Unknown Album"


def process_item(entry: Path, staging: Path, args) -> ItemReport:
    report = ItemReport(name=entry.name)
    log(f"\n--- {entry.name}")

    work = staging / safe_dirname(entry.stem if entry.is_file() else entry.name)
    if work.exists():
        shutil.rmtree(work)

    if entry.is_file():
        if entry.suffix.lower() not in ARCHIVE_EXTS:
            report.status = "skipped"
            report.reason = f"loose file, not an archive ({entry.suffix or 'no extension'})"
            log(f"    skipped: {report.reason}")
            return report
        log("    extracting archive")
        if not extract_archive(entry, work):
            report.status = "refused"
            report.reason = "archive could not be extracted"
            return report
        work = collapse_single_dir(work)
    else:
        shutil.copytree(entry, work)
        work = collapse_single_dir(work)

    removed = strip_junk(work)
    if removed:
        report.notes.append(f"removed {removed} junk file(s)")
        log(f"    stripped {removed} junk file(s)")

    partials = find_partials(work)
    if partials:
        report.status = "refused"
        report.reason = f"contains {len(partials)} partial/incomplete download(s), e.g. {partials[0].name}"
        log(f"    refused: {report.reason}")
        return report

    audio = sorted(p for p in work.rglob("*") if p.is_file() and p.suffix.lower() in AUDIO_EXTS)
    if not audio:
        report.status = "refused"
        report.reason = "no audio files found"
        log(f"    refused: {report.reason}")
        return report
    report.audio_files = len(audio)
    log(f"    {len(audio)} audio file(s)")

    # ---- what is already tagged? ------------------------------------------
    existing = [read_tags(p) for p in audio]
    album = next((t.get("album") for t in existing if t.get("album")), "")
    albumartist = next((t.get("albumartist") for t in existing if t.get("albumartist")), "")
    artist = next((t.get("artist") for t in existing if t.get("artist")), "")

    fill: dict[str, str] = {}
    if not album:
        candidate = candidate_album_title(work.name)
        log(f"    no Album tag; candidate from folder name: {candidate!r}")
        if args.no_network:
            fill["album"] = candidate
            report.notes.append("offline: album taken from folder name, not verified")
        else:
            match = search_release(artist or albumartist, candidate, folder_name=work.name)
            if match:
                fill["album"] = match["title"]
                report.mb_release = f"{match['title']} ({match['id']}, score {match.get('score')})"
                log(f"    MusicBrainz: {report.mb_release}")
                mb_artist = ((match.get("artist-credit") or [{}])[0].get("name")) or ""
                if mb_artist and not albumartist:
                    fill["albumartist"] = mb_artist
                if match.get("date"):
                    fill["date"] = match["date"][:4]
                fill["musicbrainz_albumid"] = match["id"]
            else:
                fill["album"] = candidate
                report.notes.append("no MusicBrainz match; album taken from folder name")
                log("    MusicBrainz: no confident match, using folder name")

    if not albumartist and not fill.get("albumartist") and artist:
        # MPD groups on AlbumArtist and falls back to Artist; making it
        # explicit keeps the album from splitting per-track.
        fill["albumartist"] = artist

    # ---- write, and check what stuck --------------------------------------
    if fill:
        log(f"    writing tags: {', '.join(sorted(fill))}")
        for path in audio:
            before = decoded_md5(path)
            ok, err = write_tags(path, fill)
            if not ok:
                report.tag_failures.append(f"{path.name}: {err}")
                continue
            after = decoded_md5(path)
            if before and after and before != after:
                report.md5_mismatches.append(path.name)

            back = read_tags(path)
            missed = [k for k, v in fill.items()
                      if k in FIELDS and back.get(k, "").strip() != v.strip()]
            if missed:
                report.tag_failures.append(f"{path.name}: {', '.join(missed)} did not stick")
            else:
                report.tagged.append(path.name)

    # ---- cover art --------------------------------------------------------
    has_art = any((work / n).exists() for n in ART_NAMES)
    if not has_art and not args.no_network and fill.get("musicbrainz_albumid"):
        data = fetch_front_cover(fill["musicbrainz_albumid"])
        if data:
            (work / "folder.jpg").write_bytes(data)
            report.art = "folder.jpg from Cover Art Archive"
            log(f"    cover art: {report.art} ({len(data) // 1024} KB)")
        else:
            log("    cover art: none on Cover Art Archive")
    elif has_art:
        report.art = "already present"

    # ---- collision check --------------------------------------------------
    final_name = safe_dirname(fill.get("album") or album or work.name)
    target = MUSIC_ROOT / final_name
    report.target = str(target)
    if target.exists():
        report.status = "refused"
        report.reason = f"target already exists: {target}"
        log(f"    refused: {report.reason}")
        return report

    if args.dry_run:
        # Distinct from "skipped": this item passed every gate and would be
        # written for real. The preview UI keys its confirm button off this.
        report.status = "would-ingest"
        report.reason = f"dry run -- would land at {target}"
        log(f"    dry run: would land at {target}")
        return report

    # ---- land it ----------------------------------------------------------
    log(f"    copying to {target}")
    ok, err = copy_to_ssd(work, target)
    if not ok:
        report.status = "refused"
        report.reason = err
        log(f"    refused: {err}")
        return report

    # Retire the source so a second run does not collide with the first.
    done = INBOX / ".done" / entry.name
    if done.exists():
        shutil.rmtree(done, ignore_errors=True) if done.is_dir() else done.unlink()
    shutil.move(str(entry), str(done))

    report.status = "ingested"
    log(f"    ingested (source retired to .done/{entry.name})")
    return report


# --------------------------------------------------------------------------
# Main
# --------------------------------------------------------------------------

def mpd_update() -> dict[str, int]:
    """Trigger an MPD rescan and return the resulting Artists/Albums/Songs counts."""
    log("\nTriggering MPD update...")
    run(["mpc", "update"], check=False)
    for _ in range(120):
        status = run(["mpc", "status"], check=False).stdout
        if "Updating DB" not in status:
            break
        time.sleep(1)
    stats = run(["mpc", "stats"], check=False).stdout
    counts: dict[str, int] = {}
    for line in stats.splitlines():
        if line.startswith(("Artists", "Albums", "Songs")):
            log(f"  {line}")
            key, _, value = line.partition(":")
            try:
                counts[key.strip().lower()] = int(value.strip())
            except ValueError:
                pass
    return counts


SCHEMA_VERSION = 1


def emit_json(reports: list[ItemReport], dry_run: bool, exit_code: int,
              mpd_stats: dict[str, int] | None = None, error: str = "") -> None:
    """Write the machine-readable report to stdout.

    Every exit path calls this in --json mode, including the ones that never
    reach an item, so the backend's ingest service can parse stdout blind and
    never has to fall back on scraping human text.
    """
    if not JSON_MODE:
        return

    items = [
        {
            "name": r.name,
            "status": r.status,
            "reason": r.reason,
            "target": r.target,
            "audioFiles": r.audio_files,
            "tagged": r.tagged,
            "tagFailures": r.tag_failures,
            "md5Mismatches": r.md5_mismatches,
            "mbRelease": r.mb_release,
            "art": r.art,
            "notes": r.notes,
        }
        for r in reports
    ]

    def count(status: str) -> int:
        return sum(1 for r in reports if r.status == status)

    document = {
        "schema": SCHEMA_VERSION,
        "dryRun": dry_run,
        "error": error,
        "exitCode": exit_code,
        "items": items,
        "summary": {
            "total": len(reports),
            "ingested": count("ingested"),
            "wouldIngest": count("would-ingest"),
            "refused": count("refused"),
            "skipped": count("skipped"),
            "tagFailures": sum(len(r.tag_failures) for r in reports),
            "audioAltered": sum(len(r.md5_mismatches) for r in reports),
        },
        "mpd": mpd_stats or {},
    }
    print(json.dumps(document, indent=2), flush=True)


def print_report(reports: list[ItemReport], args) -> int:
    log("\n" + "=" * 66)
    log("INGEST REPORT")
    log("=" * 66)

    ingested = [r for r in reports if r.status == "ingested"]
    would = [r for r in reports if r.status == "would-ingest"]
    refused = [r for r in reports if r.status == "refused"]
    skipped = [r for r in reports if r.status == "skipped"]

    for group, title in ((ingested, "INGESTED"), (would, "WOULD INGEST"),
                         (refused, "REFUSED"), (skipped, "SKIPPED")):
        if not group:
            continue
        log(f"\n{title} ({len(group)})")
        for r in group:
            log(f"  - {r.name}")
            if r.reason:
                log(f"      {r.reason}")
            if r.status in ("ingested", "would-ingest"):
                log(f"      -> {r.target}  ({r.audio_files} track(s))")
            if r.mb_release:
                log(f"      MusicBrainz: {r.mb_release}")
            if r.art:
                log(f"      art: {r.art}")
            for note in r.notes:
                log(f"      note: {note}")
            if r.tagged:
                log(f"      tags written to {len(r.tagged)} file(s)")
            for failure in r.tag_failures:
                log(f"      TAG DID NOT STICK: {failure}")
            for name in r.md5_mismatches:
                log(f"      AUDIO CHANGED (investigate): {name}")

    total_failures = sum(len(r.tag_failures) for r in reports)
    total_md5 = sum(len(r.md5_mismatches) for r in reports)
    log("")
    log(f"Summary: {len(ingested)} ingested, {len(would)} would ingest, "
        f"{len(refused)} refused, {len(skipped)} skipped")
    if total_failures:
        log(f"         {total_failures} tag write(s) did not stick (see above)")
    if total_md5:
        log(f"         {total_md5} file(s) had their audio stream altered -- INVESTIGATE")

    return 1 if (refused or total_md5) else 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Ingest staged music onto the Stellar SSD.")
    parser.add_argument("--dry-run", action="store_true", help="run everything except the SSD write")
    parser.add_argument("--no-network", action="store_true", help="skip MusicBrainz and Cover Art Archive")
    parser.add_argument("--item", help="ingest only this inbox entry")
    parser.add_argument("--keep-staging", action="store_true", help="leave the staging tree for inspection")
    parser.add_argument("--json", action="store_true",
                        help="emit a machine-readable report on stdout (commentary goes to stderr)")
    args = parser.parse_args()

    global JSON_MODE
    JSON_MODE = args.json

    def fail(message: str, code: int = 2) -> int:
        log(message)
        emit_json([], args.dry_run, code, error=message)
        return code

    if not INBOX.is_dir():
        return fail(f"inbox not found: {INBOX}")
    if not MUSIC_ROOT.is_dir():
        return fail(f"music root not found: {MUSIC_ROOT}")

    entries = [p for p in sorted(INBOX.iterdir())
               if not p.name.startswith(".") and p.name != "staging"]
    if args.item:
        entries = [p for p in entries if p.name == args.item]
        if not entries:
            return fail(f"no such inbox entry: {args.item}")

    if not entries:
        # Not an error: an empty inbox is the resting state, and the preview
        # button hits this constantly.
        log(f"Inbox is empty ({INBOX}). Nothing to do.")
        emit_json([], args.dry_run, 0)
        return 0

    log(f"Inbox: {INBOX}  ({len(entries)} item(s))")
    if args.dry_run:
        log("DRY RUN -- the SSD will not be written")

    staging = INBOX / "staging"
    staging.mkdir(exist_ok=True)

    reports = []
    try:
        for entry in entries:
            try:
                reports.append(process_item(entry, staging, args))
            except Exception as exc:
                reports.append(ItemReport(name=entry.name, status="refused",
                                          reason=f"unexpected error: {exc}"))
                log(f"    refused: unexpected error: {exc}")
    finally:
        if not args.keep_staging:
            shutil.rmtree(staging, ignore_errors=True)

    mpd_stats: dict[str, int] = {}
    if any(r.status == "ingested" for r in reports):
        mpd_stats = mpd_update()

    code = print_report(reports, args)
    emit_json(reports, args.dry_run, code, mpd_stats)
    return code


if __name__ == "__main__":
    sys.exit(main())
