#!/usr/bin/env bash
#
# install-blank-cursor.sh — make the mouse pointer invisible on the LCD kiosk.
#
# The panel is touch-only, so any pointer glyph on screen is noise. Three
# different parties can draw one, and all three have to be closed off:
#
#   1. The page itself       — `cursor: none !important` in Volumio2-UI's
#                              src/app.css. Covers everything *inside* the
#                              document, and nothing else.
#   2. Chromium's own chrome — native <select> popups, drag feedback and the
#                              like are separate surfaces that page CSS cannot
#                              reach. Chromium asks the XCursor theme for them
#                              by name, and falls back to a *built-in bitmap*
#                              for any name the theme does not define. That
#                              fallback is the hole this script closes: the
#                              theme must answer to every name, not just the
#                              handful a desktop usually needs.
#   3. cage (the compositor) — draws `left_ptr` / `default` wherever no client
#                              has set a cursor.
#
# So: one fully transparent image, published under every cursor name anything
# is likely to request, as the theme `blank`. /usr/local/share/icons comes
# first on XCURSOR_PATH (set in /usr/local/bin/stellar-kiosk.sh), so this theme
# wins over Adwaita and friends in /usr/share/icons.
#
# Idempotent. Run with sudo on the Pi, then restart the kiosk:
#   sudo bash install-blank-cursor.sh && sudo systemctl restart getty@tty1
#
set -euo pipefail

THEME_DIR=/usr/local/share/icons/blank
DEFAULT_DIR=/usr/local/share/icons/default

if [[ $EUID -ne 0 ]]; then
  echo "install-blank-cursor.sh must run as root (it writes under /usr/local/share)." >&2
  exit 1
fi

mkdir -p "$THEME_DIR/cursors" "$DEFAULT_DIR"

# A 1x1 fully transparent Xcursor, published at several nominal sizes so that
# whatever XCURSOR_SIZE is in force resolves to this image rather than falling
# through to another theme.
#
# An existing file is left alone. A cursor image that libXcursor cannot parse
# is worse than no theme at all — the client falls back to its own built-in
# arrow — so a glyph already known to work is never overwritten on a re-run.
if [[ -s "$THEME_DIR/cursors/default" ]]; then
  echo "keeping the existing transparent glyph ($(stat -c%s "$THEME_DIR/cursors/default") bytes)"
else
python3 - "$THEME_DIR/cursors/default" <<'PY'
import struct, sys

SIZES = (16, 24, 32, 48, 64)
IMAGE_TYPE = 0xfffd0002

toc_bytes = 12 * len(SIZES)
offset = 16 + toc_bytes

toc, chunks = b"", b""
for size in SIZES:
    toc += struct.pack("<III", IMAGE_TYPE, size, offset)
    # chunk header (36) + one ARGB pixel, fully transparent
    chunks += struct.pack(
        "<IIIIIIIII",
        36,           # header length
        IMAGE_TYPE,
        size,         # subtype == nominal size
        1,            # chunk version
        1, 1,         # width, height
        0, 0,         # xhot, yhot
        0,            # delay
    ) + struct.pack("<I", 0x00000000)
    offset += 40

header = struct.pack("<4sIII", b"Xcur", 16, 0x10000, len(SIZES))
with open(sys.argv[1], "wb") as fh:
    fh.write(header + toc + chunks)
PY
fi

# Every name Chromium, GTK, Qt, wlroots or an X11 app might ask for. A name
# that is missing is not "no cursor" — it is the client's own built-in arrow.
NAMES=(
  # freedesktop / CSS
  default context-menu help pointer progress wait cell crosshair text
  vertical-text alias copy move no-drop not-allowed grab grabbing
  all-scroll col-resize row-resize n-resize e-resize s-resize w-resize
  ne-resize nw-resize se-resize sw-resize ew-resize ns-resize
  nesw-resize nwse-resize zoom-in zoom-out
  # X11 legacy names, still requested by plenty of toolkits
  arrow left_ptr right_ptr top_left_arrow left_ptr_watch watch
  xterm ibeam hand hand1 hand2 question_arrow cross crossed_circle
  fleur size_all size_hor size_ver size_fdiag size_bdiag
  sb_h_double_arrow sb_v_double_arrow sb_up_arrow sb_down_arrow
  sb_left_arrow sb_right_arrow double_arrow
  top_side bottom_side left_side right_side
  top_left_corner top_right_corner bottom_left_corner bottom_right_corner
  dotbox draped_box circle pirate plus target
  # drag-and-drop feedback
  dnd-none dnd-move dnd-copy dnd-link dnd-no-drop dnd-ask
  link pointer-move closedhand openhand
  # Chromium / Wayland extras
  color-picker up-arrow down-arrow left-arrow right-arrow
)

cd "$THEME_DIR/cursors"
for name in "${NAMES[@]}"; do
  [[ "$name" == "default" ]] && continue
  ln -sfn default "$name"
done

cat > "$THEME_DIR/index.theme" <<'EOF'
[Icon Theme]
Name=blank
Comment=Fully transparent cursor for kiosk use
EOF

cat > "$THEME_DIR/cursor.theme" <<'EOF'
[Icon Theme]
Name=blank
Inherits=blank
EOF

# Belt and braces: anything that resolves the theme literally named "default"
# (rather than honouring XCURSOR_THEME) lands on blank too, because
# /usr/local/share/icons precedes /usr/share/icons on XCURSOR_PATH.
cat > "$DEFAULT_DIR/index.theme" <<'EOF'
[Icon Theme]
Name=Default
Comment=Kiosk default — resolves to the fully transparent "blank" theme
Inherits=blank
EOF

chmod -R a+rX /usr/local/share/icons/blank "$DEFAULT_DIR"

echo "blank cursor theme installed: $(ls "$THEME_DIR/cursors" | wc -l) names"
