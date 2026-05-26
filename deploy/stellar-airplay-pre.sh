#!/bin/sh
# Pause MPD before AirPlay claims the USB DAC.
mpc pause >/dev/null 2>&1
echo "$(date -Is) airplay session START" >> /var/log/stellar-airplay.log
exit 0
