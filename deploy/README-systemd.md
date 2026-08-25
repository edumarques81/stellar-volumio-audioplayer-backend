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

## The Spectrum FIFO must be drained at the rate MPD writes it

`/etc/mpd.conf` declares two outputs. `USB DAC` is the real one; `Spectrum FIFO`
only feeds the LCD VU meter. They are **not** independent: MPD's
`MultipleOutputs` makes the `player` thread wait for *every* enabled output to
accept each chunk, so whatever happens on the FIFO happens to the DAC.

The backend's spectrum reader (`internal/infra/spectrum`) used to pull one FFT
window per tick at a fixed frame rate. At the shipped `FFTSize 2048` x `FPS 20`
that is 40,960 samples/s, against the 44,100 samples/s MPD writes — a permanent
3,140 samples/s deficit. Measured on the Pi 2026-08-26:

```
  0s   77680 bytes queued in the pipe
  3s  115120        (+12,480 bytes/s, monotonic)
 12s  228368
 15s   18400        <- MPD's fifo output dumps ~210 KB to make room
```

Every ~15 s the pipe saturated and MPD stalled draining it, the ALSA ring buffer
ran down, and on 192 kHz material (4x less slack) some of those stalls surfaced
as **audible dropouts**. It was `FPS 30` before M1.B, which consumed 61,440
samples/s and kept the pipe empty — hence "this never used to happen".

`Config.frameInterval()` now clamps the tick to at most half the time MPD takes
to produce one window, so consumption can never fall below production. After the
fix the pipe sits flat at 0 bytes.

**If you change `FFTSize`, `FPS`, or the FIFO's `format` in `mpd.conf`, the
invariant `FFTSize/frameInterval >= SampleRate` must still hold.**
`TestFrameIntervalNeverStarvesTheFIFO` guards it.

Diagnosing a suspected recurrence:

```bash
# Watch the pipe. Anything other than a flat near-zero line is the bug.
BE=$(sudo lsof /tmp/mpd_spectrum.fifo | awk '/^stellar/{print $2; exit}')
FD=$(sudo lsof /tmp/mpd_spectrum.fifo | awk '/^stellar/{print $4; exit}' | tr -d rw)
sudo python3 -c "import fcntl,termios,array,os,time,sys
f=os.open(f'/proc/$BE/fd/$FD',os.O_RDONLY|os.O_NONBLOCK)
while True:
    b=array.array('i',[0]); fcntl.ioctl(f,termios.FIONREAD,b,True); print(b[0]); time.sleep(1)"
```

Note that scheduling is **not** the lever here, and two plausible-sounding fixes
were measured and rejected: pinning the Spectrum thread to a different core and
demoting it out of `SCHED_FIFO` made things *worse* (3 XRUNs in 11 min), because
a slower FIFO consumer stalls `player` for longer. Making the resample cheaper
(`samplerate_converter "internal"`, 5.3% -> 0.92% of a core) did not fix it
either. The deficit is a *rate* mismatch, not a speed problem.
