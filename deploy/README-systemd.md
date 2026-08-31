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

## The VU meter tap is not an MPD output any more

`/etc/mpd.conf` used to declare two outputs: `USB DAC` and `Spectrum FIFO` (the
LCD VU meter). They were **not** independent. MPD returns a decoded chunk to its
shared `MusicBuffer` pool only once *every* enabled output has consumed it, so a
second output sits on the DAC's critical path **by construction** and stalls the
decoder feeding it. MPD reports that as the misleading `alsa_output: Decoder is
too slow; playing silence to avoid xrun`.

Removing the resampler from that output (`format "*:16:2"`, 2026-08-26) cut the
dropout rate roughly 10x and eliminated the `too slow` line entirely, but it did
**not** solve the problem — the coupling is architectural, not a tuning knob. A
residual ~1 XRUN per 5-10 min survived it. The tap now lives in an ALSA
`type meter` PCM with a custom scope plugin, so MPD has exactly one output and no
shared pool is involved.

**→ `deploy/README-alsa-scope.md`** for the design, config, build steps, and the
two traps (`-DPIC`; alsa-lib's s16 scope aborting mpd on DSD).

Also rejected by measurement, do not retry: pinning or demoting the
`output:Spectrum` thread (made it *worse* — a slower consumer stalls `player`
longer), a cheaper `samplerate_converter`, and a larger `audio_buffer_size`.

The backend side is unchanged: it drains the FIFO unconditionally and applies the
frame rate by discarding windows, never by declining to read
(`internal/infra/spectrum`). It recovers the actual rate from how fast windows
arrive and remaps its bins, holding the drawn range at the 48 kHz Nyquist so the
meter looks identical whatever the album's rate.

**If you change the FIFO framing, `FFTSize` or `FPS`, the reader must still drain
at the full write rate.** `TestReaderDrainsIndependentlyOfTheFrameRate` guards it.

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
