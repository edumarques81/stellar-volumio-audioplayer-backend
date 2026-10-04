#!/usr/bin/env bash
#
# dsd-to-flac.sh — batch-convert a DSD album (or tree of albums) to 24-bit FLAC
#                  with an explicit, measured low-pass.
#
# RUN THIS ON THE MAC, not the Pi. It is a build-machine tool: the Pi's library
# is mounted read-only and conversion is CPU-bound (~12 s per minute of audio
# per core at the default settings).
#
# Why the low-pass matters
# -----------------------
# ffmpeg's DSD decoder exposes no options at all, so its internal 1-bit
# decimation filter is fixed. Everything above the audio band that the DSD
# modulator put there — ~-51 dB relative to the music on a DSD256 release, and
# rising towards 80 kHz — survives the decode untouched. It is inaudible, it is
# incompressible, and it roughly doubles the FLAC. It is also the energy that
# drives a DAC's analog stage on very quiet recordings.
#
# The lever is the resampler's own anti-alias filter, not a separate `lowpass`
# stage: a biquad cascade droops ~2 dB at 18-20 kHz, inside the audio band.
# Measured on Vaughan Williams / Tallis (DSD256), 24-bit out:
#
#   corner    passband          just above corner   FLAC (60 s)
#   none      flat              -51 dB (DSD noise)  28M
#   50 kHz    flat to 48 kHz    -21 dB -> -140 dB   ~19M
#   30 kHz    flat to 24 kHz    -88 dB              16M
#   24 kHz    flat to 22 kHz    -137 dB             14M
#
# filter_size sets transition steepness, measured just above a 24 kHz corner:
# 64 -> -79 dB (with 0.9 dB droop at 22 kHz), 128 -> -90 dB, 256 -> -115 dB.
# 128 is the default here; it costs half of 256 and is already 40 dB below
# anything the source contains. `phase_shift` is a no-op for exact ratios such
# as DSD256 -> 88.2 kHz (16:1), so it is deliberately not set.
#
# Usage
# -----
#   dsd-to-flac.sh [options] <src-dir-or-file> <dest-dir>
#
#   -r RATE      output sample rate, Hz          (default 88200)
#   -c HZ        low-pass corner, Hz             (default 24000)
#   -f SIZE      resampler filter_size           (default 128)
#   -j N         parallel jobs                   (default: physical cores)
#   -n           dry run — print the plan, convert nothing
#   -F           re-convert even if the .flac already exists
#
# The corner must sit below the output Nyquist; a 50 kHz corner therefore needs
# -r 176400 or higher. The script checks and refuses rather than silently
# clamping.
#
# Output mirrors the source tree. Tags and embedded cover art are carried over.
# Nothing is ever deleted or overwritten unless -F is passed.

set -euo pipefail

RATE=88200
CORNER=24000
FSIZE=128
JOBS=""
DRY=0
FORCE=0

die() { printf 'dsd-to-flac: %s\n' "$*" >&2; exit 1; }

while getopts ':r:c:f:j:nFh' opt; do
  case "$opt" in
    r) RATE=$OPTARG ;;
    c) CORNER=$OPTARG ;;
    f) FSIZE=$OPTARG ;;
    j) JOBS=$OPTARG ;;
    n) DRY=1 ;;
    F) FORCE=1 ;;
    h) sed -n '2,/^set -euo/p' "$0" | sed 's/^#\{1,2\} \{0,1\}//; $d'; exit 0 ;;
    :) die "option -$OPTARG needs a value" ;;
    \?) die "unknown option -$OPTARG (try -h)" ;;
  esac
done
shift $((OPTIND - 1))

[ $# -eq 2 ] || die "usage: $(basename "$0") [options] <src-dir-or-file> <dest-dir> (try -h)"
SRC=${1%/}
DEST=${2%/}

command -v ffmpeg  >/dev/null || die "ffmpeg not found"
command -v ffprobe >/dev/null || die "ffprobe not found"
[ -e "$SRC" ] || die "source does not exist: $SRC"

case "$RATE$CORNER$FSIZE" in *[!0-9]*) die "-r, -c and -f must be integers" ;; esac
NYQUIST=$((RATE / 2))
[ "$CORNER" -lt "$NYQUIST" ] || die "corner ${CORNER} Hz is at or above the output Nyquist \
(${NYQUIST} Hz at -r ${RATE}). Raise -r (e.g. 176400 for a 50 kHz corner) or lower -c."
[ "$CORNER" -ge 20000 ] || die "corner ${CORNER} Hz would cut into the audio band; refusing"

if [ -z "$JOBS" ]; then
  JOBS=$(sysctl -n hw.physicalcpu 2>/dev/null || nproc 2>/dev/null || echo 2)
fi
case "$JOBS" in ''|*[!0-9]*) die "-j must be an integer" ;; esac
[ "$JOBS" -ge 1 ] || die "-j must be at least 1"

# cutoff is a fraction of the OUTPUT Nyquist, which is what swresample wants.
CUTOFF=$(awk -v c="$CORNER" -v n="$NYQUIST" 'BEGIN { printf "%.6f", c / n }')
AF="aresample=${RATE}:filter_type=kaiser:kaiser_beta=16:filter_size=${FSIZE}:cutoff=${CUTOFF}"

printf 'source   %s\n' "$SRC"
printf 'dest     %s\n' "$DEST"
printf 'output   %s Hz, 24-bit FLAC, low-pass %s Hz (cutoff=%s, filter_size=%s)\n' \
  "$RATE" "$CORNER" "$CUTOFF" "$FSIZE"
printf 'jobs     %s%s\n\n' "$JOBS" "$([ "$DRY" = 1 ] && echo '   (dry run)')"

# Collect inputs. -print0 so names with spaces, quotes and accents survive.
inputs=$(mktemp); trap 'rm -f "$inputs"' EXIT INT TERM HUP
if [ -d "$SRC" ]; then
  find "$SRC" -type f \( -iname '*.dsf' -o -iname '*.dff' \) -print0 | sort -z > "$inputs"
  SRC_ROOT=$SRC
else
  printf '%s\0' "$SRC" > "$inputs"
  SRC_ROOT=$(dirname "$SRC")
fi

count=$(tr -dc '\0' < "$inputs" | wc -c | tr -d ' ')
[ "$count" -gt 0 ] || die "no .dsf/.dff files found under $SRC"

convert_one() {
  src=$1
  rel=${src#"$SRC_ROOT"/}
  out="$DEST/${rel%.*}.flac"

  if [ "$FORCE" = 0 ] && [ -f "$out" ]; then
    printf 'skip   %s\n' "$rel"
    return 0
  fi
  if [ "$DRY" = 1 ]; then
    printf 'would  %s\n' "$rel"
    return 0
  fi

  mkdir -p "$(dirname "$out")"
  tmp="$out.partial"
  # -map '0:v?' is optional so files without embedded art still convert.
  if ffmpeg -nostdin -v error -y -i "$src" \
       -map 0:a:0 -map '0:v?' \
       -af "$AF" \
       -c:a flac -sample_fmt s32 -compression_level 8 \
       -c:v copy -disposition:v attached_pic \
       -map_metadata 0 \
       -f flac "$tmp"
  then
    # Only publish a file that decodes end to end; a half-written FLAC that
    # looks plausible is worse than no file.
    if ffmpeg -nostdin -v error -i "$tmp" -f null - 2>/dev/null; then
      mv -f "$tmp" "$out"
      printf 'ok     %s\n' "$rel"
    else
      rm -f "$tmp"
      printf 'BAD    %s (output failed verification)\n' "$rel" >&2
      return 1
    fi
  else
    rm -f "$tmp"
    printf 'FAIL   %s\n' "$rel" >&2
    return 1
  fi
}
export -f convert_one
export SRC_ROOT DEST AF DRY FORCE

# xargs -P for parallelism; -0 keeps the NUL framing all the way through.
rc=0
xargs -0 -n1 -P "$JOBS" bash -c 'convert_one "$0"' < "$inputs" || rc=$?

echo
if [ "$rc" -ne 0 ]; then
  printf '%s file(s) seen; at least one conversion failed — see FAIL/BAD lines above.\n' "$count"
  exit "$rc"
fi
printf '%s file(s) seen, done.\n' "$count"
