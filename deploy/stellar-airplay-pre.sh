#!/bin/sh
# Stellar AirPlay session-start hook.
#
# Releases the USB DAC so shairport-sync can claim it for the incoming
# AirPlay stream. MPD holds the device open across `mpc pause` for fast
# resume, so a plain pause is NOT enough — shairport then fails to
# acquire the DAC and the AirPlay session dies within ~2s. `mpc stop`
# triggers MPD's auto-close (default behaviour) and the device frees up
# in well under a second.

set -e
LOG="/var/log/stellar-airplay.log"

mpc stop >/dev/null 2>&1 || true

# Brief wait for ALSA to actually release the PCM. Without this, a
# race between MPD's close-on-stop path and shairport's ALSA-open is
# possible. 300ms is empirically enough on the SU-6 over USB.
sleep 0.3

echo "$(date -Is) airplay session START (mpd released)" >> "$LOG" 2>/dev/null || true
exit 0
