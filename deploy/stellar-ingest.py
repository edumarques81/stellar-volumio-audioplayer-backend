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
MB_MIN_SCORE = 90      # Necessary, nowhere near sufficient -- see choose_release.
MB_SEARCH_ATTEMPTS = 3  # A blip must not degrade into a weaker query's answer.

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

def is_junk(path: Path) -> bool:
    """True for anything strip_junk would delete, including its contents.

    Load-bearing for the preview, which inspects the inbox in place and so
    still sees junk that a commit would have deleted first. `._Track01.wav`
    is an AppleDouble file that ends in `.wav` -- counting it as audio would
    inflate the track count and hand the tag probe a 4 KB stub to write to.
    """
    if path.name in JUNK_NAMES or path.name.startswith(JUNK_PREFIXES):
        return True
    return any(part in JUNK_DIRS for part in path.parts)


def junk_paths(root: Path) -> list[Path]:
    """Everything strip_junk would remove, deepest-first."""
    return [p for p in sorted(root.rglob("*"), key=lambda p: len(p.parts), reverse=True)
            if (p.is_dir() and p.name in JUNK_DIRS)
            or (p.is_file() and (p.name in JUNK_NAMES or p.name.startswith(JUNK_PREFIXES)))]


def strip_junk(root: Path) -> int:
    """Remove macOS and Windows metadata droppings. Returns count removed."""
    removed = 0
    for path in junk_paths(root):
        if path.is_dir():
            shutil.rmtree(path, ignore_errors=True)
        else:
            path.unlink(missing_ok=True)
        removed += 1
    return removed


def find_partials(root: Path) -> list[Path]:
    return [p for p in root.rglob("*")
            if p.is_file() and not is_junk(p) and PARTIAL_RE.match(p.name)]


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


def probe_tag_write(path: Path, fill: dict[str, str]) -> tuple[str, bool]:
    """Write `fill` to `path` and read it back. Returns (problem, altered).

    `problem` is "" when every field round-tripped; `altered` is True when the
    write changed the decoded audio stream, which is the one outcome that must
    never reach the SSD.
    """
    before = decoded_md5(path)
    ok, err = write_tags(path, fill)
    if not ok:
        return err, False

    after = decoded_md5(path)
    altered = bool(before and after and before != after)

    back = read_tags(path)
    missed = [k for k, v in fill.items()
              if k in FIELDS and back.get(k, "").strip() != v.strip()]
    if missed:
        return f"{', '.join(missed)} did not stick", altered
    return "", altered


# --------------------------------------------------------------------------
# Per-track tags
# --------------------------------------------------------------------------
#
# Album-level tags alone leave every track titled with its own filename, which
# is what MPD falls back to when TIT2 is absent. A Bandcamp WAV download is the
# worst case: the files carry no tags whatsoever, so a 17-track album arrives
# as seventeen rows reading "artist - album - 01 Title.wav".
#
# The filename is the only per-track metadata that exists, and unlike a folder
# name it is highly structured. It is still a guess, so it is gated the same
# way the MusicBrainz match is: derive numbers and titles for the whole album,
# then accept them only on positive evidence that the parse was right.

TRACK_NUM_RE = re.compile(r"^(\d{1,3})(?!\d)[\s._)\-]+(.+)$")


def _common_prefix(names: list[str]) -> str:
    """Longest prefix shared by every name, trimmed to a separator boundary.

    Bandcamp names every file "<artist> - <album> - NN Title", so the shared
    prefix is exactly the part that is not per-track. Taking only what *all*
    files share is what makes this safe: a prefix that is not really a prefix
    cannot survive the comparison.

    Two trims matter. Trailing digits come off first, because a nine-track
    album shares the "0" of "01".."09" and eating it would renumber the album.
    Then the prefix is cut back to the last separator, so it can never end
    mid-word and steal the first letters of a title.
    """
    if len(names) < 2:
        return ""
    head = names[0]
    for name in names[1:]:
        limit = min(len(head), len(name))
        i = 0
        while i < limit and head[i] == name[i]:
            i += 1
        head = head[:i]
        if not head:
            return ""
    head = re.sub(r"\d+$", "", head)
    m = re.search(r"^(.*[\s._)\-])", head)
    return m.group(1) if m else ""


def _parse_stems(audio: list[Path], strip_prefix: bool) -> dict[Path, tuple[int, str]]:
    """Parse (number, title) out of each filename. Empty dict if any file fails."""
    stems = [p.stem for p in audio]
    prefix = _common_prefix(stems) if strip_prefix else ""
    out: dict[Path, tuple[int, str]] = {}
    for path, stem in zip(audio, stems):
        rest = stem[len(prefix):] if prefix and stem.startswith(prefix) else stem
        m = TRACK_NUM_RE.match(rest.strip())
        if not m:
            return {}
        title = m.group(2).strip(" -_.")
        if not title:
            return {}
        out[path] = (int(m.group(1)), title)
    return out


def derive_track_tags(audio: list[Path],
                      mb_tracks: dict[int, str] | None = None
                      ) -> tuple[dict[Path, dict[str, str]], str]:
    """Per-file title/tracknumber from filenames. Returns (tags, why-not).

    The gate is that the parsed numbers are exactly 1..N with no gaps and no
    duplicates. That is strong evidence the parse found real track numbers
    rather than a year, a bitrate, or the leading digits of a title: a wrong
    reading essentially never lands on a complete sequence. A genuine gap --
    a partial download, or a disc split across folders -- fails the gate too,
    which is the right answer, because renumbering those would be worse than
    leaving them alone.

    `mb_tracks` only ever *canonicalises*: a MusicBrainz title is adopted in
    place of the filename's when the two already agree after normalisation, so
    "Into The Freedom" picks up the release's "Into the Freedom". Where they
    disagree the filename wins and nothing is invented -- the release may be a
    different edition, as a Bandcamp download and its CD pressing often are.
    """
    for strip_prefix in (True, False):
        parsed = _parse_stems(audio, strip_prefix)
        if not parsed:
            continue
        numbers = sorted(n for n, _ in parsed.values())
        if numbers != list(range(1, len(audio) + 1)):
            continue

        out: dict[Path, dict[str, str]] = {}
        for path, (number, title) in parsed.items():
            canonical = (mb_tracks or {}).get(number, "")
            if canonical and normalise_name(canonical) == normalise_name(title):
                title = canonical
            out[path] = {"title": title, "tracknumber": str(number)}
        return out, ""

    return {}, ("filenames do not yield a complete 1..%d track sequence" % len(audio))


def fetch_release_tracks(release_id: str) -> dict[int, str]:
    """Track number -> canonical title for a release. Empty on any failure.

    Best-effort by design: this only ever improves the spelling of a title the
    filename already supplied, so a network failure here must not hold up an
    ingest that is otherwise complete.
    """
    url = f"{MB_ROOT}/release/{urllib.parse.quote(release_id)}?inc=recordings&fmt=json"
    data = _http_json(url)
    if not data:
        return {}
    out: dict[int, str] = {}
    for medium in data.get("media") or []:
        for track in medium.get("tracks") or []:
            try:
                number = int(str(track.get("number", "")).strip())
            except (TypeError, ValueError):
                continue
            title = (track.get("title") or "").strip()
            if title and number not in out:
                out[number] = title
    return out


def by_extension(paths: list[Path]) -> dict[str, list[Path]]:
    grouped: dict[str, list[Path]] = {}
    for path in paths:
        grouped.setdefault(path.suffix.lower(), []).append(path)
    return grouped


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


class SearchUnavailable(Exception):
    """MusicBrainz could not be reached.

    Distinct from "MusicBrainz has no such release", and the distinction is
    load-bearing: search_release walks progressively weaker queries, so a
    transient blip on a well-constrained query used to degrade silently into a
    match from a weaker one. An unreachable server must abort the search, not
    advance it.
    """


def _http_json(url: str, attempts: int = 1) -> dict | None:
    """Rate-limited JSON GET. Returns None only when every attempt failed."""
    global _last_mb_call
    for _ in range(attempts):
        wait = MB_RATE_LIMIT_S - (time.monotonic() - _last_mb_call)
        if wait > 0:
            time.sleep(wait)
        _last_mb_call = time.monotonic()

        req = urllib.request.Request(
            url, headers={"User-Agent": USER_AGENT, "Accept": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=HTTP_TIMEOUT_S) as resp:
                return json.load(resp)
        except Exception:
            continue
    return None


# A self-evident quality decoration: a container/encoding name, a sample rate or
# a bit depth. Safe to strip wherever it appears, so long as it is a whole token.
_QUALITY_WORD = (
    # Container / encoding names, optionally carrying the DSD rate (DSF256).
    r"flac|dsf\d*|dff\d*|dfs\d*|wav|aiff|alac|ape|hd|hi-?res|dsd\d*|sacd"
    r"|remaster(?:ed)?"
    r"|\d{2,6}(?:\.\d+)?k(?:hz)?"   # 352k, 11289k, 88.2kHz
    r"|\d{1,2}b(?:it)?"             # 24b, 1bit
)
# A bare number is only decoration by association -- inside a run of other
# decorations, as in "-096-HD-176-WAV". On its own it is part of the title:
# "Miles Davis + 19", "Symphony No 8", "(1962 Recording)".
_QUALITY_ANY = rf"(?:{_QUALITY_WORD}|\d{{2,4}})"

# Never strip out of the middle of a word. "NativeDSD" is a record label, not a
# DSD decoration, and would otherwise be truncated to "Native". The dot keeps
# the pattern off the tail of a decimal, and the digits off the tail of a longer
# number -- without that, `11289k` matched only its last four digits and left a
# `-1` stub glued to the album title.
_TOKEN_START = r"(?<![A-Za-z0-9.])"

# A trailing run of decorations, taken as a whole so that bare numbers inside it
# go with it. The run may only *begin* on a bare number when a hyphen or
# underscore introduces it ("Dances-096-HD-176-WAV"): a space-separated number
# reads as prose, and consuming it would turn "Miles Davis + 19-DSF-11289k-1b"
# into "Miles Davis +".
QUALITY_RUN_RE = re.compile(
    rf"(?:[\s\-_]+|^){_TOKEN_START}(?:{_QUALITY_WORD}|(?<=[-_])\d{{2,4}})"
    rf"(?:[\s\-_]+{_QUALITY_ANY})*[\s\-_]*$",
    re.I,
)
# The same decorations mid-name, for folders that append a qualifier after them
# ("Kind Of Blue-FLAC-352k-24b Corrected Speed").
QUALITY_WORD_RE = re.compile(rf"[\s\-_]*{_TOKEN_START}(?:{_QUALITY_WORD})\b", re.I)

CATALOGUE_PREFIX_RE = re.compile(r"^[A-Z]{2,6}\d{3,6}[\s\-_]+")

# The `Artist - Album` separator, spelled with a hyphen, en dash or em dash.
SEPARATOR_RE = re.compile(r"\s+[-–—]\s+")


def candidate_album_title(folder_name: str) -> str:
    """Derive a searchable album title from a folder name.

    MusicBrainz has no way to identify an untagged file -- SearchRelease is a
    lookup keyed on metadata you already hold, not a fingerprint. The folder
    name is the only seed available, so clean the quality decorations off it:

        RStrauss Also Sprach Zarathustra ... VPO__FLAC_352k-24b
            -> RStrauss Also Sprach Zarathustra ... VPO
        HDTT13879 Miles Davis - Kind Of Blue-DSF-11289k-1b
            -> Miles Davis - Kind Of Blue
        Rachmaninoff-Sumphonic-Dances-096-HD-176-WAV
            -> Rachmaninoff-Sumphonic-Dances
    """
    name = folder_name
    name = name.split("__")[0]
    name = CATALOGUE_PREFIX_RE.sub("", name)
    # The trailing run first: bare numbers are only recognisable as decoration
    # while their neighbours are still there to vouch for them.
    prev = None
    while prev != name:
        prev = name
        name = QUALITY_RUN_RE.sub("", name).strip(" -_")
    prev = None
    while prev != name:
        prev = name
        name = QUALITY_WORD_RE.sub(" ", name).strip(" -_")
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
    # The separator may be a hyphen, en dash or em dash -- one real folder is
    # "Nat King Cole – Just One Of Those Things", and on a plain hyphen split it
    # yielded no artist at all.
    parts = SEPARATOR_RE.split(cleaned, maxsplit=1)
    if len(parts) == 2:
        head, tail = parts
        add(head, tail)
        add(artist_tag, tail)
        add("", tail)
    add("", cleaned)
    return candidates[:4]  # cap the rate-limited round trips per album


def _release_sort_key(release: dict) -> tuple:
    """Rank releases: score desc, Official first, earliest date first.

    Reissues of one album all score 100, so score alone breaks ties
    arbitrarily. Preferring the earliest Official pressing gives a stable
    answer and a `date` worth writing into the tag.

    This orders *reissues of the same album*, and nothing else. It is only ever
    applied inside a single corroborated group (see _group_key); applying it
    across unrelated releases is what filed daoud's "ok" (2025) as
    "Ok ok ok ok ok ok ok" by The Bombhappies (2007).
    """
    date = release.get("date") or "9999"
    return (
        -release.get("score", 0),
        0 if release.get("status") == "Official" else 1,
        date,
    )


def normalise_name(text: str) -> str:
    """Casefold, strip accents and punctuation, collapse whitespace.

    For comparing a MusicBrainz value against a folder-derived one, where
    "L'Oeil de Jules" and "l oeil de jules" must compare equal.

    "&" folds to "and" because the two spellings of the same conjunction are the
    single most common difference between a folder name and MusicBrainz's
    canonical form ("Cannonball and Coltrane" vs "Cannonball & Coltrane"). That
    is an orthographic equivalence, not fuzzy matching -- everything else here
    stays strict, because loose title comparison is what admitted the wrong
    album in the first place.
    """
    decomposed = unicodedata.normalize("NFKD", text or "")
    stripped = "".join(c for c in decomposed if not unicodedata.combining(c))
    stripped = stripped.replace("&", " and ")
    return " ".join(re.sub(r"[^\w\s]+", " ", stripped).casefold().split())


def release_artist_credit(release: dict) -> str:
    """Join a search result's artist-credit into its display string."""
    parts = []
    for credit in release.get("artist-credit") or []:
        parts.append(credit.get("name") or "")
        parts.append(credit.get("joinphrase") or "")
    return "".join(parts).strip()


def _is_token_prefix(shorter: str, longer: str) -> bool:
    a, b = shorter.split(), longer.split()
    return bool(a) and len(a) <= len(b) and b[:len(a)] == a


def artist_matches(release: dict, queried: str) -> bool:
    """Does this release's credit plausibly name the artist we asked for?

    MusicBrainz treats `artist:"..."` as a *scoring* term, not a filter, so a
    result set is not guaranteed to be by the artist requested. Accepts an
    exact normalised match or a leading-token-run match in either direction, so
    that "Herbert von Karajan  Wiener Philharmoniker" matches the credit
    "Herbert von Karajan", and "Miles Davis" matches "Miles Davis Quintet".
    Deliberately not a substring test: "Ella Fitzgerald" must not match
    "Louis Armstrong & Ella Fitzgerald" as the album artist.
    """
    want = normalise_name(queried)
    if not want:
        return False
    got = normalise_name(release_artist_credit(release))
    if not got:
        return False
    return got == want or _is_token_prefix(want, got) or _is_token_prefix(got, want)


def titles_equivalent(release_title: str, queried: str) -> bool:
    """Exact normalised title equality — no containment, no fuzz.

    Containment is what makes short titles dangerous: "ok" is contained in
    "Ok ok ok ok ok ok ok", and in "OK Computer".
    """
    return bool(normalise_name(queried)) and \
        normalise_name(release_title) == normalise_name(queried)


def release_is_corroborated(release: dict, queried_artist: str, queried_album: str) -> bool:
    """Is there positive evidence this release is the album in the folder?

    A high score is not evidence. `release:"ok"` returns ten unrelated albums
    all scoring >=90, one of them at 100. Require either:

      * the artist we asked for matches the credit -- the query was constrained
        and the constraint held, so the canonical title may be adopted even
        when it differs from the folder name (this is how a folder called
        "RStrauss Also Sprach Zarathustra ... VPO" acquires a real title); or
      * the title matches exactly -- the only corroboration available when the
        folder name yields no artist to constrain on.

    An artist-less query whose title does not match exactly is rejected. That
    is the fallback that produced the Bombhappies match.
    """
    return artist_matches(release, queried_artist) or \
        titles_equivalent(release.get("title", ""), queried_album)


def _group_key(release: dict) -> tuple[str, str]:
    """Identity of "the same album" for ambiguity detection.

    Prefer MusicBrainz's own release-group id: it is precisely "these releases
    are the same album", which is the question being asked, and it is present in
    search results. Comparing title+credit instead reads reissues as different
    albums whenever the credit is spelled differently -- the three pressings of
    "Cannonball & Coltrane" are credited to both "Cannonball Adderley & John
    Coltrane" and "Cannonball Adderley Quintet", and would look like a genuine
    three-way ambiguity. Fall back to title+credit only if the field is absent.
    """
    group_id = (release.get("release-group") or {}).get("id")
    if group_id:
        return ("mbrg", group_id)
    return (normalise_name(release.get("title", "")),
            normalise_name(release_artist_credit(release)))


def choose_release(releases: list[dict], queried_artist: str,
                   queried_album: str) -> tuple[dict | None, str]:
    """Pick one release from a search result set, or explain the refusal."""
    scored = [r for r in releases if r.get("score", 0) >= MB_MIN_SCORE]
    if not scored:
        return None, "no result scored >= %d" % MB_MIN_SCORE

    corroborated = [r for r in scored
                    if release_is_corroborated(r, queried_artist, queried_album)]
    if not corroborated:
        top = sorted(scored, key=_release_sort_key)[0]
        return None, (
            f"{len(scored)} result(s) scored >= {MB_MIN_SCORE} but none is "
            f"corroborated by artist or exact title (best was "
            f"{top.get('title')!r} by {release_artist_credit(top)!r})")

    best_score = max(r.get("score", 0) for r in corroborated)
    top_set = [r for r in corroborated if r.get("score", 0) == best_score]
    groups = {_group_key(r) for r in top_set}
    if len(groups) > 1:
        names = ", ".join(sorted(f"{r.get('title')!r} by {release_artist_credit(r)!r}"
                                 for r in top_set)[:3])
        return None, (f"ambiguous: {len(groups)} different releases tie at score "
                      f"{best_score} ({names})")

    # One album, possibly several pressings of it: now the reissue tie-break
    # is the right tool.
    return sorted(top_set, key=_release_sort_key)[0], ""


def search_release(artist: str, album: str,
                   folder_name: str = "") -> tuple[dict | None, str]:
    """Best MusicBrainz release across candidate readings, plus a reason.

    Returns (release, "") on a confident match, or (None, reason). Walks the
    candidate readings most-specific-first and stops at the first one that
    yields a corroborated, unambiguous answer; a query that returns hits but
    no confident answer does not stop the walk, but an unreachable server does.
    """
    candidates = candidate_queries(folder_name or album, artist) if folder_name else [(artist, album)]

    reasons: list[str] = []
    for cand_artist, cand_album in candidates:
        terms = [f'release:"{escape_lucene(cand_album)}"']
        if cand_artist:
            terms.append(f'artist:"{escape_lucene(cand_artist)}"')
        query = " AND ".join(terms)
        url = f"{MB_ROOT}/release?query={urllib.parse.quote(query)}&fmt=json&limit=10"

        payload = _http_json(url, attempts=MB_SEARCH_ATTEMPTS)
        if payload is None:
            raise SearchUnavailable(
                f"MusicBrainz unreachable after {MB_SEARCH_ATTEMPTS} attempts "
                f"({query})")

        match, reason = choose_release(payload.get("releases") or [],
                                      cand_artist, cand_album)
        if match:
            return match, ""
        reasons.append(f"{query} -> {reason}")

    return None, "; ".join(reasons) if reasons else "no candidate queries"


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

    # `tree` is what we inspect; `mutable` says whether we own it and may write
    # to it. A preview of a plain folder inspects the inbox in place: copying
    # the album first is what made the first real preview take three and a half
    # minutes on a 5.2 GB import, and every byte of it was thrown away.
    if entry.is_file():
        if entry.suffix.lower() not in ARCHIVE_EXTS:
            report.status = "skipped"
            report.reason = f"loose file, not an archive ({entry.suffix or 'no extension'})"
            log(f"    skipped: {report.reason}")
            return report
        # An archive has to be unpacked before anything can be said about it.
        log("    extracting archive")
        if not extract_archive(entry, work):
            report.status = "refused"
            report.reason = "archive could not be extracted"
            return report
        tree = collapse_single_dir(work)
        mutable = True
    elif args.dry_run:
        tree = collapse_single_dir(entry)
        mutable = False
        log("    preview: inspecting in place, no copy")
    else:
        shutil.copytree(entry, work)
        tree = collapse_single_dir(work)
        mutable = True

    if mutable:
        removed = strip_junk(tree)
        if removed:
            report.notes.append(f"removed {removed} junk file(s)")
            log(f"    stripped {removed} junk file(s)")
    else:
        removed = len(junk_paths(tree))
        if removed:
            report.notes.append(f"would remove {removed} junk file(s)")
            log(f"    would strip {removed} junk file(s)")

    partials = find_partials(tree)
    if partials:
        report.status = "refused"
        report.reason = f"contains {len(partials)} partial/incomplete download(s), e.g. {partials[0].name}"
        log(f"    refused: {report.reason}")
        return report

    audio = sorted(p for p in tree.rglob("*")
                   if p.is_file() and not is_junk(p) and p.suffix.lower() in AUDIO_EXTS)
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
    mb_release_id = ""
    if not album:
        candidate = candidate_album_title(tree.name)
        log(f"    no Album tag; candidate from folder name: {candidate!r}")
        if args.no_network:
            fill["album"] = candidate
            report.notes.append("offline: album taken from folder name, not verified")
        else:
            try:
                match, why = search_release(artist or albumartist, candidate,
                                            folder_name=tree.name)
            except SearchUnavailable as exc:
                match, why = None, str(exc)

            if match:
                fill["album"] = match["title"]
                credit = release_artist_credit(match)
                report.mb_release = (f"{match['title']} by {credit} "
                                     f"({match['id']}, score {match.get('score')})")
                log(f"    MusicBrainz: {report.mb_release}")
                if not titles_equivalent(match["title"], candidate):
                    # The canonical title differs from the folder name. Legitimate
                    # and often desirable, but it is the shape a misidentification
                    # takes, so say so out loud rather than only in the tag.
                    note = (f"adopted MusicBrainz title {match['title']!r} over "
                            f"folder-derived {candidate!r}")
                    report.notes.append(note)
                    log(f"    note: {note}")
                mb_artist = ((match.get("artist-credit") or [{}])[0].get("name")) or ""
                if mb_artist and not albumartist:
                    fill["albumartist"] = mb_artist
                if match.get("date"):
                    fill["date"] = match["date"][:4]
                fill["musicbrainz_albumid"] = match["id"]
                mb_release_id = match["id"]
            else:
                # Refusing to guess is the safe branch: the folder name is what
                # the human who assembled the album called it.
                fill["album"] = candidate
                report.notes.append(
                    f"no confident MusicBrainz match; album taken from folder name ({why})")
                log(f"    MusicBrainz: no confident match, using folder name")
                log(f"      why: {why}")

    if not albumartist and not fill.get("albumartist") and artist:
        # MPD groups on AlbumArtist and falls back to Artist; making it
        # explicit keeps the album from splitting per-track.
        fill["albumartist"] = artist

    # A file with no Artist tag displays a blank artist even when AlbumArtist
    # is set, because the two are separate fields and clients read Artist for
    # the now-playing line. Mirror it down rather than leave the album looking
    # anonymous on the LCD.
    if not artist and not any(t.get("artist") for t in existing):
        inherited = fill.get("albumartist") or albumartist
        if inherited:
            fill["artist"] = inherited

    # ---- per-track tags ----------------------------------------------------
    per_file: dict[Path, dict[str, str]] = {}
    if not any(t.get("title") for t in existing):
        mb_tracks = fetch_release_tracks(mb_release_id) if (mb_release_id and not args.no_network) else {}
        per_file, why_not = derive_track_tags(audio, mb_tracks)
        if per_file:
            canonical = sum(1 for path, tags in per_file.items()
                            if mb_tracks.get(int(tags["tracknumber"])) == tags["title"])
            note = f"track titles and numbers derived from filenames ({len(per_file)} track(s))"
            if canonical:
                note += f", {canonical} confirmed against MusicBrainz"
            report.notes.append(note)
            log(f"    {note}")
        else:
            report.notes.append(f"no per-track titles: {why_not}")
            log(f"    no per-track titles: {why_not}")

    # ---- write, and check what stuck --------------------------------------
    def tags_for(path: Path) -> dict[str, str]:
        """Album-level tags, plus this file's own title and track number."""
        merged = dict(fill)
        merged.update(per_file.get(path, {}))
        return merged

    if (fill or per_file) and mutable:
        written = sorted(set(fill) | {k for t in per_file.values() for k in t})
        log(f"    writing tags: {', '.join(written)}")
        for path in audio:
            problem, altered = probe_tag_write(path, tags_for(path))
            if altered:
                report.md5_mismatches.append(path.name)
            if problem:
                report.tag_failures.append(f"{path.name}: {problem}")
            else:
                report.tagged.append(path.name)
    elif fill or per_file:
        # Preview. Whether a tag sticks is a property of the container format,
        # not of the individual file, so one file per extension answers the
        # question for the whole album -- and the smallest one answers it
        # fastest. The inbox is never written to: the probe runs on a copy in
        # staging, which is thrown away with the rest of it.
        grouped = by_extension(audio)
        log(f"    preview: probing tag writes on {len(grouped)} file(s), "
            f"one per format ({', '.join(sorted(grouped))})")
        work.mkdir(parents=True, exist_ok=True)
        for ext, members in sorted(grouped.items()):
            rep = min(members, key=lambda p: p.stat().st_size)
            probe = work / f"probe{ext}"
            shutil.copy2(rep, probe)
            try:
                problem, altered = probe_tag_write(probe, tags_for(rep))
            finally:
                probe.unlink(missing_ok=True)

            names = sorted(m.name for m in members)
            if altered:
                report.md5_mismatches.extend(names)
            if problem:
                report.tag_failures.append(
                    f"{ext} ({len(members)} file(s)): {problem} -- probed on {rep.name}")
            else:
                report.tagged.extend(names)
        report.notes.append(
            f"preview probed {len(grouped)} file(s), one per format; "
            f"the import will tag all {len(audio)}")

    # ---- cover art --------------------------------------------------------
    has_art = any((tree / n).exists() for n in ART_NAMES)
    if not has_art and not args.no_network and fill.get("musicbrainz_albumid"):
        data = fetch_front_cover(fill["musicbrainz_albumid"])
        if data:
            if mutable:
                (tree / "folder.jpg").write_bytes(data)
                report.art = "folder.jpg from Cover Art Archive"
            else:
                # Confirmed available, but a preview writes nothing. The import
                # fetches it again for real.
                report.art = "folder.jpg from Cover Art Archive (on import)"
            log(f"    cover art: {report.art} ({len(data) // 1024} KB)")
        else:
            log("    cover art: none on Cover Art Archive")
    elif has_art:
        report.art = "already present"

    # ---- collision check --------------------------------------------------
    final_name = safe_dirname(fill.get("album") or album or tree.name)
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
    ok, err = copy_to_ssd(tree, target)
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
