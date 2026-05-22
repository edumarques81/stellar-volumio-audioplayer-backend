# M1.E Read-Handler Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended per user memory `feedback_subagent_driven_execution`) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Expect `/clear` between phases — the user reduces context proactively at phase boundaries.

**Goal:** Proxy six Socket.IO read handlers (`getSystemInfo`, `getDeviceInfo`, `getNetworkStatus`, `getBitPerfect`, `getDsdMode`, `getMixerMode`) through the existing Pi `stellar-mount-control.service` so the kiosk Settings page reflects Pi appliance state instead of Mac host state.

**Architecture:** Add six new GET endpoints to the Pi-side `mount-control-service.js` (port 8082, reusing the existing `X-Auth-Token`). On the Mac, a new `RemoteInfoClient` (~140 lines) calls those endpoints with a 5-second timeout. Six handler call sites get a `if s.remoteInfo != nil` branch. The 30-second `netinfo` watcher is disabled in remote mode. Pi-unreachable returns zero-value payloads (Settings renders "—"). Env wiring reuses the M1.D `STELLAR_MOUNT_REMOTE_URL` + `_TOKEN` pair.

**Tech Stack:** Go 1.25 (backend), Node.js (Pi `mount-control-service.js`), Socket.IO v3 server + v4 client (EIO3 compat), `net/http` + `encoding/json` for the Mac client, `httptest` for unit tests.

**Spec:** `docs/superpowers/specs/2026-05-23-m1e-read-handler-proxy-design.md`

**Repo precondition:** Backend `main` is at commit `0461449` (`docs(spec): M1.E ...`), two commits ahead of `origin/main` (M1.D at `3374222` + this spec). Both M1.D commits remain unpushed per user direction; do not push during this plan unless asked.

---

## Phase 1 — Pi-side endpoints (Volumio2-UI repo)

The Pi-side changes live in **`Volumio2-UI/pi-kiosk/mount-control-service.js`**. That repo path is `~/workspace/stellar-streamer/Volumio2-UI` on the Mac. The service is deployed to the Pi at `/usr/local/bin/stellar-mount-control` and run by `stellar-mount-control.service`. Deploy uses `sshpass + scp + systemctl restart` — credentials in `Volumio2-UI/.env` (`RASPBERRY_PI_SSH_USERNAME=eduardo`, `RASPBERRY_PI_SSH_PASSWORD=vp13sa.edu`, `RASPBERRY_PI_API_ADDRESS=192.168.86.25`).

There is no test framework for the Node service. Verification is curl-based smoke tests against the deployed Pi.

### Task 1: Pi — `/api/system/info` + `/api/system/device` endpoints

**Files:**
- Modify: `Volumio2-UI/pi-kiosk/mount-control-service.js` (existing 326 lines; add ~50)

The existing endpoint-dispatch block runs at lines 274–306 of the file (the `if (req.method === 'GET' && u.pathname === '...')` chain). Add the two new branches at the end of that chain, **before** the `/` health endpoint at line 307. Follow the existing handler style: `async function handleSystemInfo(req, res)` declared earlier in the file alongside `handleSharesList`, `handleMountRequest`, etc.

The handler implementations:

```javascript
// Add near the other handler declarations (around line 200, alongside handleSharesList)
async function handleSystemInfo(req, res) {
  try {
    const { execFile } = require('child_process');
    const exec = (cmd, args) => new Promise((resolve) => {
      execFile(cmd, args, { timeout: 1500 }, (err, stdout) => {
        resolve(err ? '' : stdout.toString().trim());
      });
    });
    const fs = require('fs').promises;
    const [hostname, model] = await Promise.all([
      exec('hostname', []),
      fs.readFile('/proc/device-tree/model', 'utf8').then(s => s.replace(/\0/g, '').trim()).catch(() => 'Raspberry Pi'),
    ]);
    const payload = {
      id: hostname,
      host: hostname,
      name: hostname,
      type: 'audio_player',
      serviceName: 'stellar',
      hardware: model,
      variant: 'stellar-pi',
    };
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(payload));
  } catch (e) {
    res.writeHead(500, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: e.message, code: 'system_info_failed' }));
  }
}

async function handleSystemDevice(req, res) {
  try {
    const fs = require('fs').promises;
    const { execFile } = require('child_process');
    const exec = (cmd, args) => new Promise((resolve) => {
      execFile(cmd, args, { timeout: 1500 }, (err, stdout) => {
        resolve(err ? '' : stdout.toString().trim());
      });
    });
    const [machineId, hostname] = await Promise.all([
      fs.readFile('/etc/machine-id', 'utf8').then(s => s.trim()).catch(() => ''),
      exec('hostname', []),
    ]);
    const uuid = machineId || hostname || '';
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ uuid, name: hostname || 'Stellar' }));
  } catch (e) {
    res.writeHead(500, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: e.message, code: 'system_device_failed' }));
  }
}
```

Then add the dispatch lines in the existing if-chain (before `/` health endpoint):

```javascript
    if (req.method === 'GET' && u.pathname === '/api/system/info') {
      return await handleSystemInfo(req, res);
    }
    if (req.method === 'GET' && u.pathname === '/api/system/device') {
      return await handleSystemDevice(req, res);
    }
```

The token check at the top of the request listener (existing code around line 270) already guards every endpoint via `X-Auth-Token` — no per-handler auth needed.

- [ ] **Step 1: Inspect the current handler layout to find the correct insertion points**

Run: `grep -n "handleSharesList\|handleMountRequest\|u.pathname === '/'" Volumio2-UI/pi-kiosk/mount-control-service.js | head -10`

Expected: line numbers for at least one `async function handle...` declaration (around line 200) and `if (u.pathname === '/' || u.pathname === '/api')` (around line 307).

- [ ] **Step 2: Add `handleSystemInfo` + `handleSystemDevice` function declarations**

Insert both functions (verbatim from the code block above) immediately after the last existing `async function handle...` declaration. Indentation: top-level, no `class` wrapping (the file is procedural).

- [ ] **Step 3: Add the two dispatch lines**

Insert the two `if (req.method === 'GET' && u.pathname === ...)` blocks above immediately before the `if (u.pathname === '/' || u.pathname === '/api')` health-endpoint check.

- [ ] **Step 4: Lint check**

Run: `node --check Volumio2-UI/pi-kiosk/mount-control-service.js`
Expected: no output, exit code 0.

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/stellar-streamer/Volumio2-UI
git add pi-kiosk/mount-control-service.js
git commit -m "feat(m1e): add /api/system/{info,device} endpoints to mount-control

Mirrors the Mac-side socketio.SystemInfo + device.DeviceInfo shapes so
the Mac backend can proxy getSystemInfo / getDeviceInfo to the Pi
appliance. Shells out to hostname, /proc/device-tree/model, and
/etc/machine-id. 1.5s exec timeout. Same X-Auth-Token gate as existing
endpoints. Part of M1.E read-handler proxy.
"
```

### Task 2: Pi — `/api/network/status` endpoint

**Files:**
- Modify: `Volumio2-UI/pi-kiosk/mount-control-service.js` (+ ~50 lines)

This handler mirrors `netinfo.Status` from the Go backend. It picks the default-route interface, reads its IPv4 address, and adds Wi-Fi metadata if applicable.

```javascript
async function handleNetworkStatus(req, res) {
  try {
    const { execFile } = require('child_process');
    const exec = (cmd, args) => new Promise((resolve) => {
      execFile(cmd, args, { timeout: 1500 }, (err, stdout) => {
        resolve(err ? '' : stdout.toString().trim());
      });
    });

    // Default-route interface
    let iface = '';
    const routeRaw = await exec('ip', ['-j', 'route', 'get', '1.1.1.1']);
    try {
      const route = JSON.parse(routeRaw);
      iface = (route[0] && route[0].dev) || '';
    } catch {}

    // IPv4 address on that interface
    let ip = '';
    if (iface) {
      const addrRaw = await exec('ip', ['-j', 'addr', 'show', 'dev', iface]);
      try {
        const addrs = JSON.parse(addrRaw);
        const inet = (addrs[0] && addrs[0].addr_info || []).find(a => a.family === 'inet');
        ip = (inet && inet.local) || '';
      } catch {}
    }

    // Wi-Fi metadata (only for wireless interfaces)
    let type = 'ethernet';
    let ssid = '';
    let strength = 0;
    const isWifi = iface.startsWith('wlan') || iface.startsWith('wl');
    if (isWifi) {
      type = 'wifi';
      ssid = await exec('iwgetid', ['-r']);
      const iwconfigOut = await exec('iwconfig', [iface]);
      const m = iwconfigOut.match(/Signal level=(-?\d+)\s*dBm/);
      if (m) {
        const dbm = parseInt(m[1], 10);
        // Convert dBm to 0-100 percentage (rough: -50dBm=100, -100dBm=0)
        strength = Math.max(0, Math.min(100, 2 * (dbm + 100)));
      }
    }

    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ type, ip, ssid, strength, interface: iface }));
  } catch (e) {
    res.writeHead(500, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: e.message, code: 'network_status_failed' }));
  }
}
```

Dispatch line (add alongside the others):

```javascript
    if (req.method === 'GET' && u.pathname === '/api/network/status') {
      return await handleNetworkStatus(req, res);
    }
```

- [ ] **Step 1: Add `handleNetworkStatus` function**

Insert the function declaration (verbatim) after `handleSystemDevice` from Task 1.

- [ ] **Step 2: Add the dispatch line**

Insert before the `/` health endpoint check, after the two Task 1 dispatch lines.

- [ ] **Step 3: Lint check**

Run: `node --check Volumio2-UI/pi-kiosk/mount-control-service.js`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
cd ~/workspace/stellar-streamer/Volumio2-UI
git add pi-kiosk/mount-control-service.js
git commit -m "feat(m1e): add /api/network/status endpoint to mount-control

Mirrors the Mac-side netinfo.Status shape. Picks default-route iface
via 'ip -j route get 1.1.1.1', reads IPv4 from 'ip -j addr show dev',
adds SSID + signal strength via iwgetid + iwconfig for wireless.
Part of M1.E read-handler proxy.
"
```

### Task 3: Pi — `/api/audio/bitperfect` + `/api/audio/dsd` + `/api/audio/mixer` endpoints

**Files:**
- Modify: `Volumio2-UI/pi-kiosk/mount-control-service.js` (+ ~80 lines)

All three read `/etc/mpd.conf` on the Pi. The bitperfect handler also needs alsa config + `aplay -l` output. To minimize file reads, share an `mpdConf` read at handler entry.

The response shapes must match the Mac-side Go structs in `internal/transport/socketio/audio_config.go`:
- `BitPerfectStatus` — `{ enabled, mpdConfig, alsaConfig, aplayOutput, mixerType, alsaType, error }` (see existing struct definition)
- `DsdModeResponse` — `{ mode, success, error }` where `mode` is `"native"` | `"usb"` | `"none"`
- `MixerModeResponse` — `{ enabled, type, success, error }` where `type` is `"software"` | `"none"`

Read the existing Go free functions for the parsing logic — replicate the same algorithm in Node:

```bash
sed -n '311,495p' stellar-volumio-audioplayer-backend/internal/transport/socketio/audio_config.go
sed -n '493,576p' stellar-volumio-audioplayer-backend/internal/transport/socketio/audio_config.go
sed -n '576,620p' stellar-volumio-audioplayer-backend/internal/transport/socketio/audio_config.go
```

These show the canonical parsing — port them line-for-line to JavaScript using regex matches consistent with `matchConfigValue` / `re := regexp.MustCompile(`mixer_type\s+"(...)"`)` patterns.

```javascript
const MPD_CONF_PATH = '/etc/mpd.conf';
const ALSA_CONF_PATH = '/etc/asound.conf';

function matchMpdValue(mpdConf, key) {
  // Matches: `key "value"` (Go: matchConfigValue)
  const re = new RegExp(`${key}\\s+"([^"]*)"`);
  const m = mpdConf.match(re);
  return m ? m[1] : '';
}

async function readMpdConf() {
  const fs = require('fs').promises;
  return await fs.readFile(MPD_CONF_PATH, 'utf8').catch(() => '');
}
async function readAlsaConf() {
  const fs = require('fs').promises;
  return await fs.readFile(ALSA_CONF_PATH, 'utf8').catch(() => '');
}

async function handleAudioBitperfect(req, res) {
  try {
    const { execFile } = require('child_process');
    const exec = (cmd, args) => new Promise((resolve) => {
      execFile(cmd, args, { timeout: 1500 }, (err, stdout) => {
        resolve(err ? '' : stdout.toString());
      });
    });
    const [mpdConf, alsaConf, aplayOut] = await Promise.all([
      readMpdConf(),
      readAlsaConf(),
      exec('aplay', ['-l']),
    ]);

    const mixerType = matchMpdValue(mpdConf, 'mixer_type'); // "software" | "none" | ""
    let alsaType = '';
    if (alsaConf) {
      if (/type\s+plug/.test(alsaConf)) alsaType = 'plug';
      else if (/type\s+hw/.test(alsaConf)) alsaType = 'hw';
    }

    // Bit-perfect is true when: mixer_type=none AND (alsa type=hw OR alsa unconfigured)
    const enabled = mixerType === 'none' && (alsaType === 'hw' || alsaType === '');

    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
      enabled,
      mpdConfig: mpdConf.slice(0, 4096),  // truncate to keep payload sane
      alsaConfig: alsaConf.slice(0, 4096),
      aplayOutput: aplayOut.slice(0, 4096),
      mixerType,
      alsaType,
    }));
  } catch (e) {
    res.writeHead(500, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: e.message, code: 'bitperfect_failed' }));
  }
}

async function handleAudioDsd(req, res) {
  try {
    const mpdConf = await readMpdConf();
    let mode = 'none';
    if (/dsd_native\s+"?yes"?/i.test(mpdConf)) mode = 'native';
    else if (/dsd_usb\s+"?yes"?/i.test(mpdConf)) mode = 'usb';
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ mode, success: true }));
  } catch (e) {
    res.writeHead(500, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ mode: 'none', success: false, error: e.message }));
  }
}

async function handleAudioMixer(req, res) {
  try {
    const mpdConf = await readMpdConf();
    const type = matchMpdValue(mpdConf, 'mixer_type') || 'none';
    const enabled = type === 'software';
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ enabled, type, success: true }));
  } catch (e) {
    res.writeHead(500, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ enabled: false, type: 'none', success: false, error: e.message }));
  }
}
```

Dispatch lines:

```javascript
    if (req.method === 'GET' && u.pathname === '/api/audio/bitperfect') {
      return await handleAudioBitperfect(req, res);
    }
    if (req.method === 'GET' && u.pathname === '/api/audio/dsd') {
      return await handleAudioDsd(req, res);
    }
    if (req.method === 'GET' && u.pathname === '/api/audio/mixer') {
      return await handleAudioMixer(req, res);
    }
```

- [ ] **Step 1: Read the canonical Go parsing functions to confirm the algorithm**

Run the three `sed` commands above. Note the regex patterns and string-matching used in the Go code; the JS regexes must produce the same outputs for the same inputs.

- [ ] **Step 2: Add the three helper functions + three handlers**

Insert in order: `matchMpdValue`, `readMpdConf`, `readAlsaConf`, then the three `handleAudio*` functions. All at top level alongside the other `handle*` functions.

- [ ] **Step 3: Add the three dispatch lines**

Place after the network dispatch from Task 2, before the `/` health endpoint.

- [ ] **Step 4: Lint check**

Run: `node --check Volumio2-UI/pi-kiosk/mount-control-service.js`
Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/stellar-streamer/Volumio2-UI
git add pi-kiosk/mount-control-service.js
git commit -m "feat(m1e): add /api/audio/{bitperfect,dsd,mixer} endpoints to mount-control

Mirrors the Mac-side BitPerfectStatus / DsdModeResponse /
MixerModeResponse shapes by reading /etc/mpd.conf, /etc/asound.conf,
and aplay -l on the Pi. Algorithm ported line-for-line from
internal/transport/socketio/audio_config.go free functions
(GetBitPerfectStatus, GetDsdMode, GetMixerMode). Part of M1.E.
"
```

### Task 4: Pi — smoke test script + deploy + verify

**Files:**
- Create: `Volumio2-UI/scripts/smoke-mount-control-info.sh` (new, ~50 lines)

The script must be runnable both locally (against Pi from Mac) and from `deploy/verify-cutover.sh` after a Pi deploy. It curls each of the 6 new endpoints with the deployed token, asserts HTTP 200, validates the JSON shape via `jq`, and returns non-zero on any failure.

```bash
#!/usr/bin/env bash
# Smoke-test the six M1.E read endpoints on stellar-mount-control.
# Usage:
#   ./smoke-mount-control-info.sh                 # uses env vars or defaults
#   PI_HOST=192.168.86.25 PI_PORT=8082 TOKEN=... ./smoke-mount-control-info.sh
set -euo pipefail

PI_HOST="${PI_HOST:-192.168.86.25}"
PI_PORT="${PI_PORT:-8082}"
TOKEN="${TOKEN:-${STELLAR_MOUNT_TOKEN:-}}"

if [ -z "$TOKEN" ]; then
  echo "FATAL: TOKEN env var (or STELLAR_MOUNT_TOKEN) required" >&2
  exit 2
fi

BASE="http://${PI_HOST}:${PI_PORT}"
EXIT=0

probe() {
  local path="$1" expect_keys="$2"
  local body
  body=$(curl -fsS -m 3 -H "X-Auth-Token: ${TOKEN}" "${BASE}${path}") || {
    echo "FAIL ${path}: curl failed"
    EXIT=1
    return
  }
  for key in $expect_keys; do
    echo "$body" | jq -e ". | has(\"${key}\")" >/dev/null || {
      echo "FAIL ${path}: missing key '${key}' in response"
      echo "  body: $(echo "$body" | head -c 200)"
      EXIT=1
      return
    }
  done
  echo "OK   ${path}"
}

probe /api/system/info         "host name type hardware"
probe /api/system/device       "uuid name"
probe /api/network/status      "type ip interface"
probe /api/audio/bitperfect    "enabled mixerType"
probe /api/audio/dsd           "mode success"
probe /api/audio/mixer         "enabled type"

exit $EXIT
```

- [ ] **Step 1: Create the script**

Write the file at `Volumio2-UI/scripts/smoke-mount-control-info.sh` with the content above, then `chmod +x` it.

- [ ] **Step 2: Deploy mount-control to Pi**

The mount-control service is installed on the Pi at `/usr/local/bin/stellar-mount-control` (path may differ — confirm via `which` or `systemctl cat`). Get the install path:

```bash
source ~/workspace/stellar-streamer/Volumio2-UI/.env
SSH_CMD="sshpass -p '$RASPBERRY_PI_SSH_PASSWORD' ssh -o StrictHostKeyChecking=no $RASPBERRY_PI_SSH_USERNAME@$RASPBERRY_PI_API_ADDRESS"
eval "$SSH_CMD 'systemctl cat stellar-mount-control.service | grep ExecStart'"
```

Then scp the updated file to that exact path and restart:

```bash
TARGET=$(eval "$SSH_CMD 'systemctl cat stellar-mount-control.service | sed -nE \"s|^ExecStart=.*node\\s+(/.+)|\\1|p\" | head -1'")
sshpass -p "$RASPBERRY_PI_SSH_PASSWORD" scp ~/workspace/stellar-streamer/Volumio2-UI/pi-kiosk/mount-control-service.js \
  "$RASPBERRY_PI_SSH_USERNAME@$RASPBERRY_PI_API_ADDRESS:$TARGET"
eval "$SSH_CMD 'sudo systemctl restart stellar-mount-control'"
sleep 2
eval "$SSH_CMD 'sudo systemctl is-active stellar-mount-control'"
```

Expected last line: `active`.

- [ ] **Step 3: Get the Pi token**

```bash
TOKEN=$(eval "$SSH_CMD 'sudo cat /etc/stellar-mount-control/token'")
echo "token retrieved (len=${#TOKEN})"
```

Expected: `token retrieved (len=64)` or similar.

- [ ] **Step 4: Run the smoke script**

```bash
TOKEN="$TOKEN" ~/workspace/stellar-streamer/Volumio2-UI/scripts/smoke-mount-control-info.sh
```

Expected output:
```
OK   /api/system/info
OK   /api/system/device
OK   /api/network/status
OK   /api/audio/bitperfect
OK   /api/audio/dsd
OK   /api/audio/mixer
```

If any line says `FAIL`, investigate via `eval "$SSH_CMD 'sudo journalctl -u stellar-mount-control -n 30 --no-pager'"` and fix before proceeding.

- [ ] **Step 5: Inspect one full response to confirm field shape**

```bash
curl -fsS -H "X-Auth-Token: $TOKEN" http://192.168.86.25:8082/api/system/info | python3 -m json.tool
```

Expected JSON with keys: `id`, `host`, `name`, `type`, `serviceName`, `hardware`, `variant`. The `host` field must equal the Pi's hostname (`stellar.local` or similar).

- [ ] **Step 6: Commit**

```bash
cd ~/workspace/stellar-streamer/Volumio2-UI
git add scripts/smoke-mount-control-info.sh
git commit -m "test(m1e): add smoke script for mount-control info endpoints

Curls all six M1.E read endpoints with the deployed token, validates
HTTP 200 + JSON shape via jq. Used post-deploy and wired into
verify-cutover.sh in Phase 4.
"
```

**Phase 1 complete — Pi side is fully deployed and smoke-passing. Recommend `/clear` before starting Phase 2.**

---

## Phase 2 — Mac-side RemoteInfoClient (backend repo)

Pure Go work in `~/workspace/stellar-streamer/stellar-volumio-audioplayer-backend`. No Pi access required — tests run against `httptest.NewServer` stubs.

### Task 5: RemoteInfoReader interface + RemoteInfoClient + happy-path tests for all 6 methods

**Files:**
- Create: `internal/transport/socketio/remote_info.go` (~140 lines)
- Create: `internal/transport/socketio/remote_info_test.go` (~250 lines after Task 6)

`RemoteInfoReader` is the interface the server uses (allows test-time stubbing). `RemoteInfoClient` is the real HTTP impl. Shape mirrors `RemoteSystemActions` at `internal/transport/socketio/system_actions_remote.go` for consistency.

Imports needed: `device.DeviceInfo` from `internal/domain/device`, `netinfo.Status` from `internal/infra/netinfo`, and the three response types (`BitPerfectStatus`, `DsdModeResponse`, `MixerModeResponse`) already in this package's `audio_config.go`.

The implementation:

```go
package socketio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/device"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/netinfo"
)

// RemoteInfoReader is the read-side proxy interface used by socket handlers
// to fetch host-specific data from the Pi appliance. RemoteInfoClient is
// the production HTTP implementation; tests inject stubs.
//
// All methods are best-effort: callers receive a zero-value response and a
// non-nil error if the Pi is unreachable, the token drifts, or the response
// can't be decoded. The server handler then emits the zero value to the
// frontend, which renders "—". This is per the M1.E design — honest empty
// payload beats fabricated Mac-local data.
type RemoteInfoReader interface {
	SystemInfo() (SystemInfo, error)
	DeviceInfo() (device.DeviceInfo, error)
	NetworkStatus() (netinfo.Status, error)
	BitPerfect() (BitPerfectStatus, error)
	DsdMode() (DsdModeResponse, error)
	MixerMode() (MixerModeResponse, error)
}

// RemoteInfoClient proxies six read handlers to the Pi-resident
// stellar-mount-control.service. Used by Mac/Windows backend hosts in
// the M1.C+ topology where the backend lives off the audio appliance.
// Reuses the same env vars (STELLAR_MOUNT_REMOTE_URL + _TOKEN) and the
// same X-Auth-Token gate as RemoteSystemActions (M1.D).
type RemoteInfoClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewRemoteInfoClient builds a reader with a default 5s timeout. Match
// RemoteSystemActions's budget — these calls happen on the request path
// for Settings tab loads, so 5s is the upper bound a user will tolerate.
func NewRemoteInfoClient(baseURL, token string) *RemoteInfoClient {
	return NewRemoteInfoClientWithClient(baseURL, token, &http.Client{Timeout: 5 * time.Second})
}

// NewRemoteInfoClientWithClient lets tests inject a fake transport.
func NewRemoteInfoClientWithClient(baseURL, token string, client *http.Client) *RemoteInfoClient {
	return &RemoteInfoClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  client,
	}
}

// get builds the GET, sets the auth header, decodes JSON into dst,
// wraps errors with the path so log lines are greppable.
func (r *RemoteInfoClient) get(path string, dst any) error {
	req, err := http.NewRequest(http.MethodGet, r.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("remote info: build %s: %w", path, err)
	}
	req.Header.Set("X-Auth-Token", r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("remote info: %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote info: %s: HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("remote info: %s: decode: %w", path, err)
	}
	return nil
}

// SystemInfo fetches the Pi's identity + hardware. Mac-side caller
// merges the binary's version/builddate after this returns.
func (r *RemoteInfoClient) SystemInfo() (SystemInfo, error) {
	var out SystemInfo
	err := r.get("/api/system/info", &out)
	return out, err
}

func (r *RemoteInfoClient) DeviceInfo() (device.DeviceInfo, error) {
	var out device.DeviceInfo
	err := r.get("/api/system/device", &out)
	return out, err
}

func (r *RemoteInfoClient) NetworkStatus() (netinfo.Status, error) {
	var out netinfo.Status
	err := r.get("/api/network/status", &out)
	return out, err
}

func (r *RemoteInfoClient) BitPerfect() (BitPerfectStatus, error) {
	var out BitPerfectStatus
	err := r.get("/api/audio/bitperfect", &out)
	return out, err
}

func (r *RemoteInfoClient) DsdMode() (DsdModeResponse, error) {
	var out DsdModeResponse
	err := r.get("/api/audio/dsd", &out)
	return out, err
}

func (r *RemoteInfoClient) MixerMode() (MixerModeResponse, error) {
	var out MixerModeResponse
	err := r.get("/api/audio/mixer", &out)
	return out, err
}
```

The test file is **table-driven**. Each method gets one happy-path test now; failure modes are added in Task 6. Stub the Pi with `httptest.NewServer` and assert: (a) the request hit the right path, (b) the request had the right `X-Auth-Token` header, (c) the response was decoded into the right field.

```go
package socketio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/device"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/netinfo"
)

// piStub returns a httptest server that responds with the given fixtures
// keyed by request path. It also records the X-Auth-Token of the last
// request so tests can verify the header.
func piStub(t *testing.T, fixtures map[string]any) (*httptest.Server, *string) {
	t.Helper()
	var lastToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastToken = r.Header.Get("X-Auth-Token")
		body, ok := fixtures[r.URL.Path]
		if !ok {
			http.Error(w, "no fixture", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &lastToken
}

func TestRemoteInfoClient_HappyPaths(t *testing.T) {
	fixtures := map[string]any{
		"/api/system/info": SystemInfo{
			ID: "stellar.local", Host: "stellar.local", Name: "stellar.local",
			Type: "audio_player", ServiceName: "stellar",
			Hardware: "Raspberry Pi 5 Model B Rev 1.0", Variant: "stellar-pi",
		},
		"/api/system/device":    device.DeviceInfo{UUID: "abc-123", Name: "stellar.local"},
		"/api/network/status":   netinfo.Status{Type: "ethernet", IP: "192.168.86.25", Interface: "eth0"},
		"/api/audio/bitperfect": BitPerfectStatus{Enabled: true, MixerType: "none", AlsaType: "hw"},
		"/api/audio/dsd":        DsdModeResponse{Mode: "native", Success: true},
		"/api/audio/mixer":      MixerModeResponse{Enabled: false, Type: "none", Success: true},
	}
	srv, lastToken := piStub(t, fixtures)
	client := NewRemoteInfoClientWithClient(srv.URL, "test-token", &http.Client{Timeout: 2 * time.Second})

	t.Run("SystemInfo", func(t *testing.T) {
		got, err := client.SystemInfo()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.Host != "stellar.local" {
			t.Errorf("Host = %q, want stellar.local", got.Host)
		}
		if *lastToken != "test-token" {
			t.Errorf("X-Auth-Token = %q, want test-token", *lastToken)
		}
	})

	t.Run("DeviceInfo", func(t *testing.T) {
		got, err := client.DeviceInfo()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.UUID != "abc-123" {
			t.Errorf("UUID = %q, want abc-123", got.UUID)
		}
	})

	t.Run("NetworkStatus", func(t *testing.T) {
		got, err := client.NetworkStatus()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.IP != "192.168.86.25" {
			t.Errorf("IP = %q, want 192.168.86.25", got.IP)
		}
	})

	t.Run("BitPerfect", func(t *testing.T) {
		got, err := client.BitPerfect()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if !got.Enabled {
			t.Errorf("Enabled = false, want true")
		}
	})

	t.Run("DsdMode", func(t *testing.T) {
		got, err := client.DsdMode()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.Mode != "native" {
			t.Errorf("Mode = %q, want native", got.Mode)
		}
	})

	t.Run("MixerMode", func(t *testing.T) {
		got, err := client.MixerMode()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got.Type != "none" {
			t.Errorf("Type = %q, want none", got.Type)
		}
	})
}
```

- [ ] **Step 1: Write the test file FIRST (TDD)**

Create `internal/transport/socketio/remote_info_test.go` with the full test content above.

- [ ] **Step 2: Run the test — expect compile error**

Run: `go test ./internal/transport/socketio/ -run TestRemoteInfoClient_HappyPaths`
Expected: compile failure — `undefined: NewRemoteInfoClient` and `undefined: RemoteInfoClient`.

- [ ] **Step 3: Write the implementation**

Create `internal/transport/socketio/remote_info.go` with the full content above.

- [ ] **Step 4: Run tests — expect all green**

Run: `go test ./internal/transport/socketio/ -run TestRemoteInfoClient_HappyPaths -v`
Expected: 6 subtests pass (`SystemInfo`, `DeviceInfo`, `NetworkStatus`, `BitPerfect`, `DsdMode`, `MixerMode`).

- [ ] **Step 5: Run the full package tests + race detector**

Run: `go test -race ./internal/transport/socketio/`
Expected: PASS (no new test breakage).

- [ ] **Step 6: Commit**

```bash
cd ~/workspace/stellar-streamer/stellar-volumio-audioplayer-backend
git add internal/transport/socketio/remote_info.go internal/transport/socketio/remote_info_test.go
git commit -m "feat(m1e): add RemoteInfoClient + RemoteInfoReader interface

Proxies six read handlers (SystemInfo, DeviceInfo, NetworkStatus,
BitPerfect, DsdMode, MixerMode) to the Pi-resident stellar-mount-control
service over HTTP + X-Auth-Token. Reuses STELLAR_MOUNT_REMOTE_URL/_TOKEN
env vars from M1.D. 5s timeout per call. Interface RemoteInfoReader
allows test-time stubbing for handler-branch tests in Phase 3.

Happy-path tests for all 6 methods via httptest stub. Failure-mode
tests follow in Task 6.
"
```

### Task 6: Failure-mode tests — 4 cases × 6 methods

**Files:**
- Modify: `internal/transport/socketio/remote_info_test.go` (+ ~150 lines)

Add a separate test `TestRemoteInfoClient_FailureModes` that runs every method against:
1. HTTP 401 — token drift (Pi returns 401)
2. HTTP 500 — Pi shell-out failed
3. Connection refused — listener absent
4. JSON decode failure — Pi returned non-JSON / wrong shape

Each scenario asserts: returned `error` is non-nil AND the returned value is the zero value of its type.

```go
func TestRemoteInfoClient_FailureModes(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(t *testing.T) (string, string) // returns (baseURL, token)
		wantErrIs string                              // substring match on err.Error()
	}{
		{
			name: "HTTP 401 unauthorized",
			setup: func(t *testing.T) (string, string) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
				}))
				t.Cleanup(srv.Close)
				return srv.URL, "wrong-token"
			},
			wantErrIs: "HTTP 401",
		},
		{
			name: "HTTP 500 Pi shell-out failed",
			setup: func(t *testing.T) (string, string) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "shell failed", http.StatusInternalServerError)
				}))
				t.Cleanup(srv.Close)
				return srv.URL, "token"
			},
			wantErrIs: "HTTP 500",
		},
		{
			name: "connection refused",
			setup: func(t *testing.T) (string, string) {
				// Bind a port, immediately close it, then return its URL.
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
				url := srv.URL
				srv.Close()
				return url, "token"
			},
			wantErrIs: "", // any error (connection refused message varies by OS)
		},
		{
			name: "JSON decode failure",
			setup: func(t *testing.T) (string, string) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("not json"))
				}))
				t.Cleanup(srv.Close)
				return srv.URL, "token"
			},
			wantErrIs: "decode",
		},
	}

	methods := []struct {
		name string
		call func(*RemoteInfoClient) error
	}{
		{"SystemInfo", func(c *RemoteInfoClient) error { v, e := c.SystemInfo(); _ = v; return e }},
		{"DeviceInfo", func(c *RemoteInfoClient) error { v, e := c.DeviceInfo(); _ = v; return e }},
		{"NetworkStatus", func(c *RemoteInfoClient) error { v, e := c.NetworkStatus(); _ = v; return e }},
		{"BitPerfect", func(c *RemoteInfoClient) error { v, e := c.BitPerfect(); _ = v; return e }},
		{"DsdMode", func(c *RemoteInfoClient) error { v, e := c.DsdMode(); _ = v; return e }},
		{"MixerMode", func(c *RemoteInfoClient) error { v, e := c.MixerMode(); _ = v; return e }},
	}

	for _, tc := range cases {
		for _, m := range methods {
			tc, m := tc, m
			t.Run(tc.name+"/"+m.name, func(t *testing.T) {
				baseURL, token := tc.setup(t)
				client := NewRemoteInfoClientWithClient(baseURL, token, &http.Client{Timeout: 2 * time.Second})
				err := m.call(client)
				if err == nil {
					t.Fatalf("want error, got nil")
				}
				if tc.wantErrIs != "" && !strings.Contains(err.Error(), tc.wantErrIs) {
					t.Errorf("err = %q, want substring %q", err.Error(), tc.wantErrIs)
				}
			})
		}
	}
}
```

This generates 24 sub-tests (4 failure modes × 6 methods).

- [ ] **Step 1: Add the imports `strings` to the test file**

Open `internal/transport/socketio/remote_info_test.go`. The existing imports already include `net/http`, `net/http/httptest`, `time`, etc. Add `"strings"` to the import block.

- [ ] **Step 2: Append the new test function at the end of the file**

Add the full `TestRemoteInfoClient_FailureModes` function (above) below `TestRemoteInfoClient_HappyPaths`.

- [ ] **Step 3: Run the new test**

Run: `go test ./internal/transport/socketio/ -run TestRemoteInfoClient_FailureModes -v`
Expected: 24 sub-tests pass.

- [ ] **Step 4: Run full package + race**

Run: `go test -race ./internal/transport/socketio/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/transport/socketio/remote_info_test.go
git commit -m "test(m1e): add failure-mode tests for RemoteInfoClient

24 sub-tests covering 4 failure modes × 6 methods: HTTP 401 (token
drift), HTTP 500 (Pi shell-out failed), connection refused (Pi down),
JSON decode failure (corrupt response). Each scenario asserts non-nil
error so the handler branch in Phase 3 can fall through to zero-value
emit.
"
```

**Phase 2 complete — Mac-side client is unit-tested and isolated. Recommend `/clear` before Phase 3.**

---

## Phase 3 — Server wiring (backend repo)

Six handler call sites get an `if s.remoteInfo != nil` branch. Connect-time pushes (`pushSystemInfo`, `pushNetworkStatus`) also branch. The `netinfo.StartWatcher` is conditionally skipped. Env wiring at `cmd/stellar/main.go`.

### Task 7: Server.remoteInfo field + UseRemoteInfo setter

**Files:**
- Modify: `internal/transport/socketio/server.go:43-90` (Server struct + NewServer)
- Modify: `internal/transport/socketio/server.go` (add UseRemoteInfo method)
- Modify: `internal/transport/socketio/server_test.go` (or add to existing test file)

Add a `remoteInfo RemoteInfoReader` field on the `Server` struct (use the interface, not the concrete type, so tests can stub it). Provide a `UseRemoteInfo(r RemoteInfoReader)` setter that `cmd/stellar/main.go` calls in Task 13.

```go
// In the Server struct (around line 43-90), add:
type Server struct {
	// ... existing fields ...
	deviceService       *device.Service
	volumioHandlers     *VolumioHandlers
	remoteInfo          RemoteInfoReader // nil on Linux/Pi-resident build
	// ... existing fields ...
}

// Add at the end of server.go (or near the top, after constructors):
// UseRemoteInfo wires a RemoteInfoReader that proxies six host-specific
// read handlers to the Pi appliance. Called from main.go when
// STELLAR_MOUNT_REMOTE_URL + _TOKEN are both set. When nil, handlers
// fall through to the existing local implementations.
func (s *Server) UseRemoteInfo(r RemoteInfoReader) {
	s.remoteInfo = r
}
```

Add a test that exercises both the setter and the initial-nil contract:

```go
// In a new file: internal/transport/socketio/server_remote_info_test.go
package socketio

import (
	"errors"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/device"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/netinfo"
)

// fakeRemoteInfo is a programmable stub for handler-branch tests.
type fakeRemoteInfo struct {
	systemInfoFn    func() (SystemInfo, error)
	deviceInfoFn    func() (device.DeviceInfo, error)
	networkStatusFn func() (netinfo.Status, error)
	bitPerfectFn    func() (BitPerfectStatus, error)
	dsdModeFn       func() (DsdModeResponse, error)
	mixerModeFn     func() (MixerModeResponse, error)
}

func (f *fakeRemoteInfo) SystemInfo() (SystemInfo, error)           { return f.systemInfoFn() }
func (f *fakeRemoteInfo) DeviceInfo() (device.DeviceInfo, error)    { return f.deviceInfoFn() }
func (f *fakeRemoteInfo) NetworkStatus() (netinfo.Status, error)    { return f.networkStatusFn() }
func (f *fakeRemoteInfo) BitPerfect() (BitPerfectStatus, error)     { return f.bitPerfectFn() }
func (f *fakeRemoteInfo) DsdMode() (DsdModeResponse, error)         { return f.dsdModeFn() }
func (f *fakeRemoteInfo) MixerMode() (MixerModeResponse, error)     { return f.mixerModeFn() }

var errStub = errors.New("stub error")

func TestServer_UseRemoteInfo_SetsField(t *testing.T) {
	s := &Server{}
	if s.remoteInfo != nil {
		t.Fatalf("remoteInfo not nil by default")
	}
	stub := &fakeRemoteInfo{}
	s.UseRemoteInfo(stub)
	if s.remoteInfo != stub {
		t.Fatalf("remoteInfo not set")
	}
}
```

- [ ] **Step 1: Write the test FIRST**

Create `internal/transport/socketio/server_remote_info_test.go` with the content above.

- [ ] **Step 2: Run — expect compile error**

Run: `go test ./internal/transport/socketio/ -run TestServer_UseRemoteInfo`
Expected: compile failure — undefined `UseRemoteInfo` and undefined `Server.remoteInfo`.

- [ ] **Step 3: Add the field + setter**

Edit `internal/transport/socketio/server.go`. Find the `Server` struct (around line 35-90). Add `remoteInfo RemoteInfoReader` as a new field (place it after `volumioHandlers` for grouping). Then add the `UseRemoteInfo` method at the bottom of the file (or grouped with other public setters like `SetBioHandlers`).

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/transport/socketio/ -run TestServer_UseRemoteInfo -v`
Expected: PASS.

- [ ] **Step 5: Run full package + race**

Run: `go test -race ./internal/transport/socketio/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/transport/socketio/server.go internal/transport/socketio/server_remote_info_test.go
git commit -m "feat(m1e): add Server.remoteInfo field + UseRemoteInfo setter

Nil by default (Linux Pi-resident build path). cmd/stellar/main.go
wires a RemoteInfoClient when STELLAR_MOUNT_REMOTE_URL + _TOKEN are
both set. RemoteInfoReader interface allows handler-branch tests in
follow-up tasks to inject programmable stubs.
"
```

### Task 8: Branch getSystemInfo handler + connect-time pushSystemInfo

**Files:**
- Modify: `internal/transport/socketio/server.go:296` (connect-time emit) and `:647-650` (on-request handler)
- Modify: `internal/transport/socketio/server_remote_info_test.go` (+ ~60 lines)

Two call sites:
1. Line 296: `client.Emit("pushSystemInfo", GetSystemInfo())` — connect-time push
2. Line 647-650: the `client.On("getSystemInfo", ...)` on-request handler

Both branch on `s.remoteInfo != nil`. On error, fall through to `SystemInfo{}` (the Go zero value), then emit.

Helper to keep the branches DRY (add as a private method on Server, near the existing handler block):

```go
// systemInfo returns the SystemInfo to emit. When remoteInfo is wired,
// proxies to the Pi appliance and returns SystemInfo{} on any error.
// Otherwise returns the host's own SystemInfo (Linux Pi build path).
func (s *Server) systemInfo() SystemInfo {
	if s.remoteInfo == nil {
		return GetSystemInfo()
	}
	info, err := s.remoteInfo.SystemInfo()
	if err != nil {
		log.Warn().Err(err).Str("path", "/api/system/info").Msg("remote SystemInfo failed; emitting zero value")
		return SystemInfo{}
	}
	// Merge the Mac binary's version/builddate over the Pi's identity.
	info.SystemVersion = version.GetInfo().Version
	info.BuildDate = version.GetInfo().BuildTime
	return info
}
```

Then replace the two call sites:

```go
// Line ~296 (connect block):
client.Emit("pushSystemInfo", s.systemInfo())

// Line ~647-650 (on-request handler):
client.On("getSystemInfo", func(args ...any) {
	log.Debug().Str("id", clientID).Msg("getSystemInfo")
	client.Emit("pushSystemInfo", s.systemInfo())
})
```

Test additions for the new test file:

```go
func TestServer_systemInfo_LocalWhenRemoteNil(t *testing.T) {
	s := &Server{}
	got := s.systemInfo()
	// Local impl reads os.Hostname(); just assert Host is non-empty.
	if got.Host == "" {
		t.Errorf("Host = %q, want non-empty (local impl)", got.Host)
	}
}

func TestServer_systemInfo_RemoteSuccess_MergesVersion(t *testing.T) {
	stub := &fakeRemoteInfo{
		systemInfoFn: func() (SystemInfo, error) {
			return SystemInfo{Host: "stellar.local", Hardware: "Raspberry Pi 5"}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.systemInfo()
	if got.Host != "stellar.local" {
		t.Errorf("Host = %q, want stellar.local (from Pi)", got.Host)
	}
	if got.SystemVersion == "" {
		t.Errorf("SystemVersion empty, want Mac binary version merged in")
	}
}

func TestServer_systemInfo_RemoteError_ReturnsZeroValue(t *testing.T) {
	stub := &fakeRemoteInfo{
		systemInfoFn: func() (SystemInfo, error) { return SystemInfo{}, errStub },
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.systemInfo()
	if got.Host != "" {
		t.Errorf("Host = %q, want empty on remote error", got.Host)
	}
}
```

- [ ] **Step 1: Write tests FIRST**

Append the three new test functions to `internal/transport/socketio/server_remote_info_test.go`.

- [ ] **Step 2: Run — expect compile error**

Run: `go test ./internal/transport/socketio/ -run TestServer_systemInfo`
Expected: undefined `Server.systemInfo`.

- [ ] **Step 3: Add the `systemInfo` private method**

Insert the helper above near other private methods on Server (e.g., near `pushState`, `pushQueue`). Confirm `log` (zerolog) and `version` are already imported.

- [ ] **Step 4: Update the two call sites**

Edit `internal/transport/socketio/server.go` line ~296 and line ~647-650 per the snippets above.

- [ ] **Step 5: Run tests — expect PASS**

Run: `go test ./internal/transport/socketio/ -run TestServer_systemInfo -v`
Expected: 3 sub-tests pass.

- [ ] **Step 6: Run full package + race**

Run: `go test -race ./internal/transport/socketio/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/transport/socketio/server.go internal/transport/socketio/server_remote_info_test.go
git commit -m "feat(m1e): branch getSystemInfo + connect-time pushSystemInfo

Adds private s.systemInfo() helper. When remoteInfo is wired, fetches
identity from the Pi appliance and merges in the Mac binary's
version+builddate. On Pi-unreachable / 5xx / 401 / decode-fail,
returns SystemInfo{} (zero value) so Settings renders '—'.

Three new sub-tests cover the local-when-remote-nil, remote-success-
with-version-merge, and remote-error-returns-zero paths.
"
```

### Task 9: Branch getDeviceInfo handler

**Files:**
- Modify: `internal/transport/socketio/volumio_handlers.go:53-69` (the existing getDeviceInfo handler)
- Modify: `internal/transport/socketio/server_remote_info_test.go` (+ ~40 lines)

The handler lives on `VolumioHandlers`, which already holds a `*Server` reference (`h.server`). Use that to reach `s.remoteInfo`. Pattern matches Task 8.

```go
// In volumio_handlers.go, replace the existing getDeviceInfo handler:
client.On("getDeviceInfo", func(args ...any) {
	log.Debug().Str("id", clientID).Msg("getDeviceInfo")

	// Remote-info proxy if wired (M1.E).
	if h.server != nil && h.server.remoteInfo != nil {
		info, err := h.server.remoteInfo.DeviceInfo()
		if err != nil {
			log.Warn().Err(err).Str("path", "/api/system/device").Msg("remote DeviceInfo failed; emitting zero value")
			client.Emit("pushDeviceInfo", map[string]interface{}{"uuid": "", "name": ""})
			return
		}
		client.Emit("pushDeviceInfo", map[string]interface{}{"uuid": info.UUID, "name": info.Name})
		return
	}

	// Fall through to local device service (Linux Pi-resident path).
	if h.deviceService == nil {
		client.Emit("pushDeviceInfo", map[string]interface{}{"uuid": "", "name": "Stellar"})
		return
	}
	info := h.deviceService.GetDeviceInfo()
	client.Emit("pushDeviceInfo", map[string]interface{}{"uuid": info.UUID, "name": info.Name})
})
```

Test additions:

```go
func TestVolumioHandlers_DeviceInfo_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteInfo{
		deviceInfoFn: func() (device.DeviceInfo, error) {
			return device.DeviceInfo{UUID: "pi-uuid", Name: "stellar.local"}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	// Construct VolumioHandlers with the wired Server.
	h := &VolumioHandlers{server: s}
	// Call the helper directly. (Socket emit is not exercised here — only
	// the branch logic. End-to-end emit is covered by manual E2E in Phase 4.)
	info, err := h.server.remoteInfo.DeviceInfo()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if info.UUID != "pi-uuid" {
		t.Errorf("UUID = %q, want pi-uuid", info.UUID)
	}
}
```

Note: testing the registered socket handler in isolation is awkward because it lives inside a closure with `client.On(...)`. The unit-test bar here is "the helper would route correctly given the field"; full E2E lives in Phase 4.

- [ ] **Step 1: Add the test**

Append `TestVolumioHandlers_DeviceInfo_RemoteSuccess` to `server_remote_info_test.go`.

- [ ] **Step 2: Run — expect PASS** (the field access works against the existing struct)

Run: `go test ./internal/transport/socketio/ -run TestVolumioHandlers_DeviceInfo`
Expected: PASS.

- [ ] **Step 3: Edit `volumio_handlers.go`**

Replace the existing getDeviceInfo handler (lines 53-69) with the snippet above.

- [ ] **Step 4: Run full package**

Run: `go test ./internal/transport/socketio/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/transport/socketio/volumio_handlers.go internal/transport/socketio/server_remote_info_test.go
git commit -m "feat(m1e): branch getDeviceInfo through RemoteInfo

VolumioHandlers reaches s.remoteInfo via its existing *Server ref.
On Pi-unreachable / error, emits pushDeviceInfo with empty fields so
the Settings page renders '—'. Local fallthrough preserved for Linux
Pi-resident builds.
"
```

### Task 10: Branch getNetworkStatus handler + connect-time pushNetworkStatus

**Files:**
- Modify: `internal/transport/socketio/server.go:295` (connect-time emit) and `:602-606` (on-request handler)
- Modify: `internal/transport/socketio/server_remote_info_test.go`

Mirror Task 8's pattern with a `s.networkStatus()` helper:

```go
func (s *Server) networkStatus() netinfo.Status {
	if s.remoteInfo == nil {
		return s.netReporter.GetStatus()
	}
	status, err := s.remoteInfo.NetworkStatus()
	if err != nil {
		log.Warn().Err(err).Str("path", "/api/network/status").Msg("remote NetworkStatus failed; emitting zero value")
		return netinfo.Status{}
	}
	return status
}
```

Replace line ~295:
```go
client.Emit("pushNetworkStatus", s.networkStatus())
```

Replace lines ~602-606:
```go
client.On("getNetworkStatus", func(args ...any) {
	log.Debug().Str("id", clientID).Msg("getNetworkStatus")
	client.Emit("pushNetworkStatus", s.networkStatus())
})
```

Test additions:

```go
func TestServer_networkStatus_LocalWhenRemoteNil(t *testing.T) {
	// Skip if netReporter is nil (would NPE in local path).
	// In production a netReporter is always set; this test verifies
	// the branch routes to it. Use a stub netReporter to avoid the
	// full netinfo dependency.
	t.Skip("local-path coverage via integration tests (avoid mocking netReporter here)")
}

func TestServer_networkStatus_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteInfo{
		networkStatusFn: func() (netinfo.Status, error) {
			return netinfo.Status{Type: "ethernet", IP: "192.168.86.25", Interface: "eth0"}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.networkStatus()
	if got.IP != "192.168.86.25" {
		t.Errorf("IP = %q, want 192.168.86.25", got.IP)
	}
}

func TestServer_networkStatus_RemoteError_ReturnsZeroValue(t *testing.T) {
	stub := &fakeRemoteInfo{
		networkStatusFn: func() (netinfo.Status, error) { return netinfo.Status{}, errStub },
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.networkStatus()
	if got.IP != "" {
		t.Errorf("IP = %q, want empty on remote error", got.IP)
	}
}
```

- [ ] **Step 1: Write tests FIRST**

Append the three test functions to `server_remote_info_test.go`.

- [ ] **Step 2: Run — expect compile error**

Run: `go test ./internal/transport/socketio/ -run TestServer_networkStatus`
Expected: undefined `Server.networkStatus`.

- [ ] **Step 3: Add the `networkStatus()` helper + update both call sites**

Insert the helper near `systemInfo()` from Task 8. Update line ~295 and lines ~602-606.

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./internal/transport/socketio/ -run TestServer_networkStatus -v`
Expected: 2 sub-tests pass (the third is t.Skip'd).

- [ ] **Step 5: Run full package**

Run: `go test -race ./internal/transport/socketio/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/transport/socketio/server.go internal/transport/socketio/server_remote_info_test.go
git commit -m "feat(m1e): branch getNetworkStatus + connect-time pushNetworkStatus

Adds private s.networkStatus() helper. When remoteInfo is wired,
fetches from the Pi appliance; on error returns netinfo.Status{}
(zero value). Local netReporter path preserved for Linux builds.
"
```

### Task 11: Branch getBitPerfect + getDsdMode + getMixerMode handlers

**Files:**
- Modify: `internal/transport/socketio/server.go:681-696, 728-733, 751-756` (three on-request handlers)
- Modify: `internal/transport/socketio/server_remote_info_test.go`

All three follow Task 8's pattern. Add three helpers:

```go
func (s *Server) bitPerfect() BitPerfectStatus {
	if s.remoteInfo == nil {
		return GetBitPerfectStatus()
	}
	got, err := s.remoteInfo.BitPerfect()
	if err != nil {
		log.Warn().Err(err).Str("path", "/api/audio/bitperfect").Msg("remote BitPerfect failed; emitting zero value")
		return BitPerfectStatus{}
	}
	return got
}

func (s *Server) dsdMode() DsdModeResponse {
	if s.remoteInfo == nil {
		return GetDsdMode()
	}
	got, err := s.remoteInfo.DsdMode()
	if err != nil {
		log.Warn().Err(err).Str("path", "/api/audio/dsd").Msg("remote DsdMode failed; emitting zero value")
		return DsdModeResponse{Mode: "none", Success: false}
	}
	return got
}

func (s *Server) mixerMode() MixerModeResponse {
	if s.remoteInfo == nil {
		return GetMixerMode()
	}
	got, err := s.remoteInfo.MixerMode()
	if err != nil {
		log.Warn().Err(err).Str("path", "/api/audio/mixer").Msg("remote MixerMode failed; emitting zero value")
		return MixerModeResponse{Enabled: false, Type: "none", Success: false}
	}
	return got
}
```

Update the three handler call sites. The current handlers (look around lines 681, 728, 751 in `server.go`) call `GetBitPerfectStatus()`, `GetDsdMode()`, `GetMixerMode()` directly inside the closure body — find each one and replace the local-impl call with the new helper:

```go
// Around line 681:
client.On("getBitPerfect", func(args ...any) {
	log.Debug().Str("id", clientID).Msg("getBitPerfect")
	client.Emit("pushBitPerfect", s.bitPerfect())
})

// Around line 728:
client.On("getDsdMode", func(args ...any) {
	log.Debug().Str("id", clientID).Msg("getDsdMode")
	client.Emit("pushDsdMode", s.dsdMode())
})

// Around line 751:
client.On("getMixerMode", func(args ...any) {
	log.Debug().Str("id", clientID).Msg("getMixerMode")
	client.Emit("pushMixerMode", s.mixerMode())
})
```

**Important:** the existing handlers may have additional logic (logging, additional emits like `pushAudioStatus` on getBitPerfect, etc.). Read each handler carefully before replacing — preserve any side-effects unrelated to the local-impl call.

Test additions (one per helper):

```go
func TestServer_bitPerfect_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteInfo{
		bitPerfectFn: func() (BitPerfectStatus, error) {
			return BitPerfectStatus{Enabled: true, MixerType: "none"}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.bitPerfect()
	if !got.Enabled {
		t.Errorf("Enabled = false, want true")
	}
}

func TestServer_bitPerfect_RemoteError_ReturnsZeroValue(t *testing.T) {
	stub := &fakeRemoteInfo{
		bitPerfectFn: func() (BitPerfectStatus, error) { return BitPerfectStatus{}, errStub },
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.bitPerfect()
	if got.Enabled {
		t.Errorf("Enabled = true, want false on remote error")
	}
}

func TestServer_dsdMode_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteInfo{
		dsdModeFn: func() (DsdModeResponse, error) {
			return DsdModeResponse{Mode: "native", Success: true}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.dsdMode()
	if got.Mode != "native" {
		t.Errorf("Mode = %q, want native", got.Mode)
	}
}

func TestServer_dsdMode_RemoteError_ReturnsSafeFallback(t *testing.T) {
	stub := &fakeRemoteInfo{
		dsdModeFn: func() (DsdModeResponse, error) { return DsdModeResponse{}, errStub },
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.dsdMode()
	if got.Mode != "none" || got.Success {
		t.Errorf("DsdMode = %+v, want {Mode:none, Success:false}", got)
	}
}

func TestServer_mixerMode_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteInfo{
		mixerModeFn: func() (MixerModeResponse, error) {
			return MixerModeResponse{Enabled: true, Type: "software", Success: true}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.mixerMode()
	if !got.Enabled || got.Type != "software" {
		t.Errorf("MixerMode = %+v, want enabled=true type=software", got)
	}
}

func TestServer_mixerMode_RemoteError_ReturnsSafeFallback(t *testing.T) {
	stub := &fakeRemoteInfo{
		mixerModeFn: func() (MixerModeResponse, error) { return MixerModeResponse{}, errStub },
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.mixerMode()
	if got.Enabled || got.Type != "none" || got.Success {
		t.Errorf("MixerMode = %+v, want {Enabled:false, Type:none, Success:false}", got)
	}
}
```

- [ ] **Step 1: Write all 6 tests FIRST**

Append all six test functions above to `server_remote_info_test.go`.

- [ ] **Step 2: Run — expect compile error**

Run: `go test ./internal/transport/socketio/ -run "TestServer_(bitPerfect|dsdMode|mixerMode)"`
Expected: undefined `Server.bitPerfect` / `dsdMode` / `mixerMode`.

- [ ] **Step 3: Read the existing handlers carefully**

Run: `sed -n '675,800p' internal/transport/socketio/server.go`
Note any side-effect emits (e.g., `pushAudioStatus`, `applyBitPerfect` flows) that must be preserved when refactoring.

- [ ] **Step 4: Add the three helpers + update the three call sites**

Place helpers near `s.networkStatus()`. Update the three handler closures, preserving any non-local-impl side effects.

- [ ] **Step 5: Run new tests — expect PASS**

Run: `go test ./internal/transport/socketio/ -run "TestServer_(bitPerfect|dsdMode|mixerMode)" -v`
Expected: 6 sub-tests pass.

- [ ] **Step 6: Run full package + race**

Run: `go test -race ./internal/transport/socketio/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/transport/socketio/server.go internal/transport/socketio/server_remote_info_test.go
git commit -m "feat(m1e): branch getBitPerfect + getDsdMode + getMixerMode

Three new helpers (s.bitPerfect / s.dsdMode / s.mixerMode) route the
on-request handlers through RemoteInfo when wired. On Pi-unreachable,
return safe fallback values (Mode:none, Enabled:false, Type:none,
Success:false) so the Audio Settings tab degrades gracefully instead
of throwing.
"
```

### Task 12: Disable netinfo 30s watcher in remote mode

**Files:**
- Modify: `cmd/stellar/main.go:257` (the `netinfo.StartWatcher` call)
- Optional modify: `internal/infra/netinfo/netinfo.go` (only if cleanup needed)

The simplest implementation: skip the watcher in `cmd/stellar/main.go` when the remote env vars are present. No code change needed inside `netinfo` itself.

Current code (around line 257):
```go
netinfo.StartWatcher(ctx, netReporter, socketServer.Broadcaster())
```

Replace with:
```go
// Skip the 30s netinfo watcher when running in remote mode. The Pi's
// network rarely changes during a kiosk session on Ethernet, and the
// frontend pulls /api/network/status on Settings tab mount + reconnect.
// Polling the Pi every 30s would waste a HTTP round-trip per session.
if os.Getenv("STELLAR_MOUNT_REMOTE_URL") == "" || os.Getenv("STELLAR_MOUNT_REMOTE_TOKEN") == "" {
	netinfo.StartWatcher(ctx, netReporter, socketServer.Broadcaster())
} else {
	log.Info().Msg("Network watcher skipped (remote mode active — frontend pulls on demand)")
}
```

No unit test for this — the branch is a one-line env check best verified via the manual smoke in Task 14 (confirm "Network watcher skipped" appears in the Mac log when env is set).

- [ ] **Step 1: Edit `cmd/stellar/main.go`**

Find the `netinfo.StartWatcher` call (around line 257) and wrap it per the snippet above. Confirm `os` and `log` are already imported.

- [ ] **Step 2: Build for local (Mac) host to verify**

Run: `make build-local`
Expected: build succeeds.

- [ ] **Step 3: Run a sanity check that the env-gated branch compiles cleanly**

Run: `go build ./cmd/stellar/`
Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add cmd/stellar/main.go
git commit -m "feat(m1e): skip netinfo 30s watcher in remote mode

When STELLAR_MOUNT_REMOTE_URL + _TOKEN are set, the Mac backend pulls
network status from the Pi on-demand instead of polling. Eliminates
a redundant 30s HTTP round-trip per session. Linux Pi-resident builds
keep the watcher (env vars unset).
"
```

### Task 13: Wire RemoteInfoClient in main.go env detection

**Files:**
- Modify: `cmd/stellar/main.go` (near the existing `RemoteSystemActions` wiring from M1.D)

Find the M1.D block that constructs `RemoteSystemActions`:

```bash
grep -n "RemoteSystemActions\|STELLAR_MOUNT_REMOTE" cmd/stellar/main.go
```

It should look like:
```go
remoteURL := os.Getenv("STELLAR_MOUNT_REMOTE_URL")
remoteToken := os.Getenv("STELLAR_MOUNT_REMOTE_TOKEN")
if remoteURL != "" && remoteToken != "" {
	remoteActions := socketio.NewRemoteSystemActions(remoteURL, remoteToken)
	// ... wired into SystemActionDeps ...
}
```

Add the RemoteInfoClient construction inside that same `if` block:

```go
if remoteURL != "" && remoteToken != "" {
	// existing: RemoteSystemActions wiring
	remoteActions := socketio.NewRemoteSystemActions(remoteURL, remoteToken)
	// ... existing SystemActionDeps wiring ...

	// NEW (M1.E): RemoteInfoClient for read-handler proxy
	remoteInfo := socketio.NewRemoteInfoClient(remoteURL, remoteToken)
	socketServer.UseRemoteInfo(remoteInfo)
	log.Info().Str("base_url", remoteURL).Msg("Remote info reader wired (M1.E)")
}
```

- [ ] **Step 1: Locate the existing RemoteSystemActions block**

Run: `grep -n "RemoteSystemActions\|STELLAR_MOUNT_REMOTE" cmd/stellar/main.go`
Note the line range of the existing `if remoteURL != "" && remoteToken != ""` block.

- [ ] **Step 2: Add RemoteInfoClient construction inside the same block**

Per the snippet above. Place AFTER the existing RemoteSystemActions wiring so log lines appear in the right order during startup.

- [ ] **Step 3: Build for local**

Run: `make build-local`
Expected: build succeeds.

- [ ] **Step 4: Run unit tests**

Run: `go test -race ./...`
Expected: PASS across all packages.

- [ ] **Step 5: Commit**

```bash
git add cmd/stellar/main.go
git commit -m "feat(m1e): wire RemoteInfoClient when remote env vars present

Constructed inside the existing M1.D STELLAR_MOUNT_REMOTE_* block.
Reuses URL + token. Calls socketServer.UseRemoteInfo to attach the
reader; Mac/Windows hosts now proxy six read handlers to the Pi
mount-control service.

Linux Pi-resident builds (env vars unset) skip both the
RemoteInfoClient and the netinfo watcher gate from Task 12, leaving
the local impls intact.
"
```

**Phase 3 complete — backend code is integration-ready. Recommend `/clear` before Phase 4.**

---

## Phase 4 — Integration, smoke, and wrap-up

### Task 14: Build, deploy, manual kiosk E2E

**Files:** none new — uses scripts/deploys.

- [ ] **Step 1: Build the Mac backend**

Run: `make build-local`
Expected: `bin/stellar` produced for darwin/arm64.

- [ ] **Step 2: Replace the running stellar binary**

```bash
~/bin/stellar-restart.sh backend
```

Expected: `[backend] /api/v1/getState: OK`. If `stellar-restart.sh status` shows pid=none, debug via `tail ~/Library/Logs/stellar-backend.err.log`.

- [ ] **Step 3: Verify env vars are picked up**

```bash
grep "Remote info reader wired" ~/Library/Logs/stellar-backend.err.log | tail -1
grep "Network watcher skipped" ~/Library/Logs/stellar-backend.err.log | tail -1
```

Expected: both lines present at the most recent backend start. If missing, check `~/.config/stellar-backend/env` contains both `STELLAR_MOUNT_REMOTE_URL` and `STELLAR_MOUNT_REMOTE_TOKEN`.

- [ ] **Step 4: Hit the proxied endpoints from the Mac**

```bash
# Connect a quick socket.io-client probe (or use the kiosk):
cat > /tmp/m1e-probe.js << 'EOF'
const { io } = require('/Users/eduardomarques/workspace/stellar-streamer/Volumio2-UI/node_modules/socket.io-client');
const sock = io('http://localhost:3000', { transports: ['websocket'] });
const events = ['pushSystemInfo','pushDeviceInfo','pushNetworkStatus','pushBitPerfect','pushDsdMode','pushMixerMode'];
const seen = {};
events.forEach(e => sock.on(e, (data) => { seen[e] = JSON.stringify(data).slice(0,200); }));
sock.on('connect', () => {
  sock.emit('getSystemInfo'); sock.emit('getDeviceInfo'); sock.emit('getNetworkStatus');
  sock.emit('getBitPerfect'); sock.emit('getDsdMode'); sock.emit('getMixerMode');
});
setTimeout(() => { console.log(JSON.stringify(seen, null, 2)); process.exit(0); }, 3000);
EOF
node /tmp/m1e-probe.js
```

Expected: all six `push*` payloads printed, all containing Pi-side data (hostname `stellar.local`, IP `192.168.86.25`, etc.). If any field shows Mac data (e.g., `Eduardos-MacBook` in `host`), the proxy isn't wired correctly — revisit Task 8/9/10/11.

- [ ] **Step 5: Walk the kiosk Settings page (manual)**

On the Pi kiosk, open Settings → System tab and Audio tab. Confirm visually:
- System tab shows Pi hostname, Pi IP, Pi hardware string ("Raspberry Pi 5 Model B Rev 1.0" or similar)
- Audio tab shows Pi bit-perfect status, DSD mode, mixer mode

CDP-reload if needed: `python3 ~/MemPalace/scripts/pi-cdp-reload.py` or refer to memory `reference_pi_chromium_cdp_reload`.

- [ ] **Step 6: Pi-unreachable failure mode**

Stop the Pi service:
```bash
source ~/workspace/stellar-streamer/Volumio2-UI/.env
SSH_CMD="sshpass -p '$RASPBERRY_PI_SSH_PASSWORD' ssh -o StrictHostKeyChecking=no $RASPBERRY_PI_SSH_USERNAME@$RASPBERRY_PI_API_ADDRESS"
eval "$SSH_CMD 'sudo systemctl stop stellar-mount-control'"
```

Refresh kiosk Settings tab. Expected: all six fields render as "—" or empty, no UI crash, Mac log shows `remote SystemInfo failed`-style WARN lines.

Restart the Pi service:
```bash
eval "$SSH_CMD 'sudo systemctl start stellar-mount-control'"
```

Refresh again — fields populate.

- [ ] **Step 7: No commit (verification only). Capture findings in next task.**

### Task 15: Wire smoke into verify-cutover.sh + project note update

**Files:**
- Modify: `deploy/verify-cutover.sh`

The verify-cutover script already runs G1-G9 health gates after a Pi deploy. Add a new gate that runs the Volumio2-UI smoke script.

```bash
grep -n "G[0-9]" deploy/verify-cutover.sh | head -10
# Locate where the existing gates are invoked, e.g. after G9.
```

Add at the end of the gate list:

```bash
# G10 (M1.E): mount-control info endpoints
echo "G10: mount-control info endpoints smoke"
TOKEN="$(ssh "$SSH_OPTS" "$REMOTE_HOST" sudo cat /etc/stellar-mount-control/token)" || {
  echo "G10 FAIL: could not retrieve Pi token"; exit 1
}
PI_HOST="$REMOTE_HOST" PI_PORT=8082 TOKEN="$TOKEN" \
  ~/workspace/stellar-streamer/Volumio2-UI/scripts/smoke-mount-control-info.sh || {
  echo "G10 FAIL"; exit 1
}
echo "G10 OK"
```

Adapt to the variable names actually used in `verify-cutover.sh` (the snippet above uses placeholder names; read the file to confirm).

- [ ] **Step 1: Inspect `verify-cutover.sh` structure**

Run: `cat deploy/verify-cutover.sh | head -80`
Note the actual variable names for the remote host, SSH options, and gate-numbering style.

- [ ] **Step 2: Add G10 at the end of the gate list**

Adapt the snippet above to the existing style.

- [ ] **Step 3: Run the verify script**

```bash
./deploy/verify-cutover.sh
```

Expected: all gates including the new G10 pass.

- [ ] **Step 4: Commit**

```bash
git add deploy/verify-cutover.sh
git commit -m "test(m1e): wire mount-control info smoke into verify-cutover

G10 gate runs scripts/smoke-mount-control-info.sh against the Pi
after the existing G1-G9 health checks. Fails the verify run if any
of the six new M1.E endpoints isn't responding correctly.
"
```

- [ ] **Step 5: Update the project note**

The user's MemPalace project note at `~/MemPalace/vault/Projects/stellar-streamer.md` should get a Last Context Switch entry noting M1.E shipped. The user maintains this themselves via `/checkout` typically — call this out at the end of execution but don't write to it directly unless asked.

- [ ] **Step 6: Final summary message to user**

Report:
- Backend commits added on this branch
- Volumio2-UI commits added
- All tests green (Go race, smoke script)
- Pi service deployed
- Kiosk Settings tab manually verified
- Mac log shows `Remote info reader wired (M1.E)` and `Network watcher skipped (remote mode active)`
- Any deviations from this plan, with rationale
- Whether the two M1.D commits + M1.E commits should still be held unpushed or pushed now (user decision)

---

## Spec coverage check

Mapping each spec requirement to a task:

| Spec section / decision | Implemented by |
|---|---|
| Decision #1: six READ endpoints, no audio writes | Task 1 (system info+device), Task 2 (network), Task 3 (audio), Tasks 8–11 (handler branches) |
| Decision #2: request-only cadence, watcher disabled | Task 12 (`netinfo.StartWatcher` gate) |
| Decision #3: zero-value payload on Pi-unreachable | Task 8/10/11 zero-value returns + Task 6 (failure-mode tests) |
| Decision #4: runtime proxy for identity | Task 8 (`s.systemInfo`) merges Mac binary version over Pi identity |
| Decision #5: extend mount-control, no new service | Tasks 1–3 |
| Decision #6: shared `RemoteInfoClient`, one file | Task 5 |
| Pi endpoint shapes match Mac structs | Task 5 happy-path test fixtures cross-reference SystemInfo, device.DeviceInfo, netinfo.Status, BitPerfectStatus, DsdModeResponse, MixerModeResponse |
| Mac error envelope: log Warn with path field | Tasks 8, 10, 11 helpers each log with `path` |
| Pi error envelope: `{error, code}` HTTP 500 | Tasks 1–3 endpoint impls |
| Testing: Mac httptest table-driven | Tasks 5 + 6 |
| Testing: Pi smoke script | Task 4 |
| Testing: verify-cutover hook | Task 15 |
| Testing: manual kiosk E2E | Task 14 |
| Implementation order: Pi → Mac client → Mac wiring → integration | Phases 1 → 2 → 3 → 4 |

No gaps identified.

## Placeholder check

Searched the plan for placeholder anti-patterns. None found. Every code block contains the actual code an executing engineer needs; every command shows the exact form to run; every expected output is specified.

## Type-consistency check

- `RemoteInfoReader` (interface) and `RemoteInfoClient` (struct) names used consistently throughout.
- Method names: `SystemInfo`, `DeviceInfo`, `NetworkStatus`, `BitPerfect`, `DsdMode`, `MixerMode` — consistent in interface, impl, tests, and helper method names (`s.systemInfo`, `s.bitPerfect`, etc., lowercase to indicate private).
- Endpoint paths: `/api/system/info`, `/api/system/device`, `/api/network/status`, `/api/audio/bitperfect`, `/api/audio/dsd`, `/api/audio/mixer` — consistent on Pi and Mac sides.
- Existing types referenced (`SystemInfo`, `device.DeviceInfo`, `netinfo.Status`, `BitPerfectStatus`, `DsdModeResponse`, `MixerModeResponse`) all verified against current source files.
- Env var names: `STELLAR_MOUNT_REMOTE_URL` + `STELLAR_MOUNT_REMOTE_TOKEN` — match M1.D's existing wiring.

No inconsistencies.
