# Stellar Backend — systemd unit

## Unit file

`stellar-backend.service` is the production systemd unit for the Stellar audio
backend running on the Raspberry Pi 5. It uses `Type=notify` so systemd blocks
dependent units until the process signals `READY=1` (sent by the backend
immediately after binding the TCP listener, regardless of MPD or cache state).

## Required environment file

Create `/etc/stellar-backend/env` on the Pi with the following keys.
The file is loaded via `EnvironmentFile=` so values must NOT be quoted and
must NOT contain `export`.

```
# AI / enrichment
ANTHROPIC_API_KEY=sk-ant-...
ANTHROPIC_MODEL=claude-haiku-4-5-20251001

# Fanart.tv
FANART_API_KEY=...

# Shared bearer tokens for sidecar daemons
STELLAR_AIRPLAY_KEY=<random-token>
STELLAR_SPECTRUM_KEY=<random-token>

# Comma-separated IPs/CIDRs allowed to trigger shutdown/reboot
STELLAR_POWER_TRUSTED_REMOTES=192.168.1.0/24

# Comma-separated IPs/CIDRs allowed to trigger the drop-box ingest
# (ingest:status / ingest:preview / ingest:commit). Loopback is always
# allowed, so the LCD kiosk needs no entry here — this exists for the
# iPhone remote. Set to the LAN subnet rather than the phone's address:
# the phone is on DHCP and a pinned IP breaks on the next lease.
STELLAR_INGEST_TRUSTED_REMOTES=192.168.1.0/24
```

### Keys that MUST be absent or blank on a Pi-resident deployment

The following env vars activate the Mac-proxy code paths (M1.C remote mode).
Leave them unset so the Pi binary uses in-process LCD, local mounts, and
localhost MPD.

- `STELLAR_MOUNT_REMOTE_URL` — must be absent/blank
- `STELLAR_MOUNT_REMOTE_TOKEN` — must be absent/blank
- `STELLAR_LCD_REMOTE_URL` — must be absent/blank
- `STELLAR_LCD_REMOTE_TOKEN` — must be absent/blank
- `STELLAR_MPD_HOST` — must be absent/blank (the unit passes `-mpd-host 127.0.0.1`
  on the command line, which takes precedence; do not re-set it here)

## Install steps

```bash
# 1. Copy the unit to the system directory
sudo cp deploy/stellar-backend.service /etc/systemd/system/

# 2. Create the environment file (see above for required keys)
sudo mkdir -p /etc/stellar-backend
sudo nano /etc/stellar-backend/env        # fill in the keys
sudo chmod 600 /etc/stellar-backend/env   # keep secrets readable by root only

# 3. Reload systemd and enable the unit
sudo systemctl daemon-reload
sudo systemctl enable --now stellar-backend

# 4. Verify
systemctl status stellar-backend
journalctl -u stellar-backend -f
```

## Readiness probe

Consumers (kiosk, healthcheck scripts, CI smoke tests) should poll `GET /ready`
instead of `/health`.

| Endpoint | Returns 200 when |
|---|---|
| `/health` | HTTP server is reachable and MPD responds to Ping |
| `/ready`  | MPD connected **AND** cache loaded (AlbumCount > 0, not building) |

Example response when ready:

```json
{"ready":true,"mpd":"connected","cache":{"albums":312,"building":false},"airplay":{"active":false}}
```

Example response when cache is still building (503):

```json
{"ready":false,"mpd":"connected","cache":{"albums":0,"building":true},"airplay":{"active":false}}
```

AirPlay state (`airplay.active`) is informational — it never causes a 503.

## Watchdog

The unit sets `WatchdogSec=30`. The backend pings `WATCHDOG=1` every 10 s
(below the recommended `WatchdogSec / 2` threshold). If the main goroutine
deadlocks and the ping goroutine stops being scheduled, systemd will restart
the service after 30 s.

## CAP_SYSLOG

`AmbientCapabilities=CAP_SYSLOG` lets the Phase-2 xrun tailer read
`/dev/kmsg` as user `eduardo` (non-root). Remove this if the xrun tailer
is disabled or if the Pi image policy forbids ambient capabilities.

## Do NOT add RT scheduling

`CPUSchedulingPolicy=fifo` / `CPUSchedulingPriority` are deliberately absent.
Real-time scheduling is reserved for the `mpd.service` unit so audio wins CPU
time during periods of contention. The backend is capped at `Nice=5` and
`CPUWeight=50`.

## The Spectrum FIFO and the DAC are the same pipeline

`/etc/mpd.conf` declares two outputs: `USB DAC` and `Spectrum FIFO` (the LCD VU
meter). They are **not** independent. MPD only returns a decoded chunk to its
shared `MusicBuffer` pool once *every* enabled output has consumed it, so
anything that slows the FIFO output stalls the decoder feeding the DAC. MPD
reports this as the misleading `alsa_output: Decoder is too slow; playing
silence to avoid xrun`.

The FIFO used to be pinned to `format "44100:16:2"`. On a 192 kHz album that put
a resampler in its filter chain, which holds chunks — and produced audible
dropouts (2026-08-25/26). Measured, and each **rejected**: pinning or demoting
the `output:Spectrum` thread made it *worse* (3 XRUNs/11 min, a slower consumer
stalls `player` longer); a cheaper `samplerate_converter` did not help
(5.3% -> 0.92% of a core, still failing); `audio_buffer_size "32768"` did not
help. At 44.1 kHz — where no conversion happens — the same setup ran clean for
10 minutes. The resampler was the trigger, not CPU, priority or buffer size.

Two coordinated changes fix it:

1. `format "*:16:2"` on the FIFO output — `*` keeps the source rate, so there is
   **no resampler in that chain at any rate**.
2. The backend drains the FIFO unconditionally and applies the frame rate by
   discarding windows, never by declining to read (`internal/infra/spectrum`).
   It recovers the actual rate from how fast windows arrive and remaps its bins,
   holding the drawn range at the 48 kHz Nyquist so the meter looks identical
   whatever the album's rate.

Verified: 192 kHz + VU meter, 9 min, 0 XRUNs, 0 `too slow`, pipe flat at 0 bytes
while carrying 768 KB/s. The DAC is untouched — `hw_params` still reads
`S32_LE / 192000`.

**If you change the FIFO `format`, `FFTSize` or `FPS`, the reader must still
drain at the full write rate.** `TestReaderDrainsIndependentlyOfTheFrameRate`
guards it.

Diagnosing a suspected recurrence — anything but a flat near-zero line is the bug:

```bash
BE=$(sudo lsof /tmp/mpd_spectrum.fifo | awk '/^stellar/{print $2; exit}')
FD=$(sudo lsof /tmp/mpd_spectrum.fifo | awk '/^stellar/{print $4; exit}' | tr -d rw)
sudo python3 -c "import fcntl,termios,array,os,time
f=os.open(f'/proc/$BE/fd/$FD',os.O_RDONLY|os.O_NONBLOCK)
while True:
    b=array.array('i',[0]); fcntl.ioctl(f,termios.FIONREAD,b,True); print(b[0]); time.sleep(1)"
sudo journalctl -u mpd -f | grep "too slow"
```
