#!/usr/bin/env bash
#
# Install a fully transparent XCursor theme and point the kiosk at it.
#
# WHY: the LCD panel is touch-only (ILITEK), but wlroots still renders a pointer
# for it. That pointer is drawn by the COMPOSITOR, not by the page, so the
# frontend's `cursor: none !important` (Volumio2-UI/src/app.css) cannot suppress
# it — and it does not appear in CDP screenshots either, because CDP captures the
# page and not the compositor. cage 0.2.0 exposes no hide-cursor flag (`-d -h -m
# -s -v` only), so the supported route is an XCursor theme whose every glyph is a
# 1x1 fully transparent image.
#
# If the theme ever fails to load, wlroots falls back to the stock cursor:
# visible, but harmless. Nothing here touches audio.
#
# Run on the Pi:  sudo bash install-blank-cursor.sh
set -euo pipefail

THEME_DIR=/usr/local/share/icons/blank
KIOSK=/usr/local/bin/stellar-kiosk.sh

python3 - "$THEME_DIR" <<'PY'
import os, struct, sys
base = sys.argv[1]
cur = os.path.join(base, "cursors")
os.makedirs(cur, exist_ok=True)

def xcursor_blank(sizes=(24, 32, 48, 64)):
    """Minimal Xcursor file: one 1x1 fully transparent ARGB image per nominal size.

    Format: 16-byte header, then one 12-byte TOC entry per image
    (type 0xfffd0002 = IMAGE, subtype = nominal size, absolute offset), then each
    image chunk: 36-byte header (hdrsize, type, subtype, version, w, h, xhot,
    yhot, delay) followed by w*h little-endian ARGB pixels.
    """
    header = b"Xcur" + struct.pack("<III", 16, 0x00010000, len(sizes))
    off = len(header) + 12 * len(sizes)
    toc = b""
    chunks = b""
    for s in sizes:
        toc += struct.pack("<III", 0xfffd0002, s, off)
        chunks += struct.pack("<IIIIIIIII", 36, 0xfffd0002, s, 1, 1, 1, 0, 0, 0)
        chunks += struct.pack("<I", 0x00000000)   # fully transparent pixel
        off += 36 + 4
    return header + toc + chunks

open(os.path.join(cur, "default"), "wb").write(xcursor_blank())

# Every cursor name wlroots / GTK / Chromium might request resolves to the same
# blank glyph, so no code path can reintroduce a visible pointer.
names = ["left_ptr", "arrow", "top_left_arrow", "pointer", "hand", "hand1",
         "hand2", "text", "xterm", "ibeam", "watch", "wait", "progress",
         "crosshair", "cross", "move", "all-scroll", "grab", "grabbing",
         "not-allowed", "help", "context-menu", "n-resize", "s-resize",
         "e-resize", "w-resize", "col-resize", "row-resize"]
for n in names:
    p = os.path.join(cur, n)
    if os.path.lexists(p):
        os.remove(p)
    os.symlink("default", p)

open(os.path.join(base, "index.theme"), "w").write(
    "[Icon Theme]\nName=blank\nComment=Fully transparent cursor for kiosk use\n")
open(os.path.join(base, "cursor.theme"), "w").write(
    "[Icon Theme]\nName=blank\nInherits=blank\n")
print("theme installed at %s (%d names)" % (base, len(names) + 1))
PY

if grep -q XCURSOR_THEME "$KIOSK"; then
    echo "$KIOSK already exports XCURSOR_THEME — leaving it alone"
else
    cp "$KIOSK" "$KIOSK.bak-$(date +%Y%m%d-%H%M%S)"
    python3 - "$KIOSK" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
anchor = "mkdir -p $XDG_RUNTIME_DIR\n"
assert s.count(anchor) == 1, "anchor not found in %s" % p
open(p, "w").write(s.replace(anchor, anchor + """
# Hide the compositor-drawn pointer (see install-blank-cursor.sh for why CSS
# cannot do this and cage has no flag for it).
export XCURSOR_PATH=/usr/local/share/icons:/usr/share/icons
export XCURSOR_THEME=blank
export XCURSOR_SIZE=24
"""))
print("patched %s" % p)
PY
fi

bash -n "$KIOSK" && echo "kiosk script parses OK"
echo
echo "Apply by restarting the kiosk:  pkill -x cage"
echo "(agetty autologin re-runs ~/.bash_profile, which execs $KIOSK)"
