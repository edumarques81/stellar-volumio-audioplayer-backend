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
