#!/usr/bin/env python3
"""Stellar kiosk watchdog.

Detects a stuck/white-screened Chromium kiosk and reloads it via the Chrome
DevTools Protocol (CDP). Runs periodically from a systemd timer.

Health signal: the frontend bumps `window.__stellarHeartbeat` (epoch-ms) on
every animation frame (see Volumio2-UI/src/lib/kioskHeartbeat.ts). A frozen or
crashed renderer stops updating it. We read it over CDP and reload when:
  * the active page URL is about:blank or a chrome-error:// interstitial
    (failed load — e.g. backend wasn't up yet), OR
  * the heartbeat exists but is older than STALE_MS (renderer frozen).

Deliberately conservative: if the heartbeat is ABSENT (older frontend, or the
page hasn't run JS yet) we do NOT reload on that basis alone — only the URL
check applies — so we never get into a reload loop against a page that simply
doesn't expose the heartbeat.

Zero third-party deps: a minimal WebSocket client on top of the stdlib so the
Pi needs no pip packages.

Env overrides:
  CDP_HOST (default 127.0.0.1), CDP_PORT (default 9222),
  STALE_MS (default 60000), CDP_TIMEOUT_S (default 5).
"""

import base64
import json
import os
import socket
import struct
import sys
import time
import urllib.request

CDP_HOST = os.environ.get("CDP_HOST", "127.0.0.1")
CDP_PORT = int(os.environ.get("CDP_PORT", "9222"))
STALE_MS = int(os.environ.get("STALE_MS", "60000"))
TIMEOUT_S = float(os.environ.get("CDP_TIMEOUT_S", "5"))


def log(msg):
    print(f"[kiosk-watchdog] {msg}", flush=True)


def http_get_json(path):
    url = f"http://{CDP_HOST}:{CDP_PORT}{path}"
    with urllib.request.urlopen(url, timeout=TIMEOUT_S) as resp:
        return json.load(resp)


def pick_page_target(targets):
    """Return the first attachable 'page' target, preferring a real http(s) URL."""
    pages = [t for t in targets if t.get("type") == "page" and t.get("webSocketDebuggerUrl")]
    if not pages:
        return None
    for t in pages:
        if str(t.get("url", "")).startswith(("http://", "https://")):
            return t
    return pages[0]


class CDPSocket:
    """Minimal synchronous WebSocket client for a local ws:// CDP endpoint."""

    def __init__(self, ws_url):
        # ws://host:port/devtools/page/<id>
        assert ws_url.startswith("ws://"), ws_url
        rest = ws_url[len("ws://"):]
        hostport, _, path = rest.partition("/")
        host, _, port = hostport.partition(":")
        self.host = host
        self.port = int(port or "80")
        self.path = "/" + path
        self.sock = socket.create_connection((self.host, self.port), timeout=TIMEOUT_S)
        self.sock.settimeout(TIMEOUT_S)
        self._handshake()
        self._next_id = 0

    def _handshake(self):
        key = base64.b64encode(os.urandom(16)).decode()
        req = (
            f"GET {self.path} HTTP/1.1\r\n"
            f"Host: {self.host}:{self.port}\r\n"
            "Upgrade: websocket\r\n"
            "Connection: Upgrade\r\n"
            f"Sec-WebSocket-Key: {key}\r\n"
            "Sec-WebSocket-Version: 13\r\n\r\n"
        )
        self.sock.sendall(req.encode())
        # Read until end of headers.
        buf = b""
        while b"\r\n\r\n" not in buf:
            chunk = self.sock.recv(4096)
            if not chunk:
                raise ConnectionError("CDP handshake: connection closed")
            buf += chunk
        if b" 101 " not in buf.split(b"\r\n", 1)[0]:
            raise ConnectionError(f"CDP handshake failed: {buf.split(chr(13).encode())[0]!r}")

    def _send_text(self, text):
        payload = text.encode()
        header = bytearray()
        header.append(0x81)  # FIN + text opcode
        mask_bit = 0x80
        n = len(payload)
        if n < 126:
            header.append(mask_bit | n)
        elif n < 65536:
            header.append(mask_bit | 126)
            header += struct.pack(">H", n)
        else:
            header.append(mask_bit | 127)
            header += struct.pack(">Q", n)
        mask = os.urandom(4)
        header += mask
        masked = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
        self.sock.sendall(bytes(header) + masked)

    def _recv_exact(self, n):
        buf = b""
        while len(buf) < n:
            chunk = self.sock.recv(n - len(buf))
            if not chunk:
                raise ConnectionError("CDP recv: connection closed")
            buf += chunk
        return buf

    def _recv_text(self):
        # We only expect unmasked text frames from the server.
        b0, b1 = self._recv_exact(2)
        opcode = b0 & 0x0F
        length = b1 & 0x7F
        if length == 126:
            length = struct.unpack(">H", self._recv_exact(2))[0]
        elif length == 127:
            length = struct.unpack(">Q", self._recv_exact(8))[0]
        data = self._recv_exact(length)
        if opcode == 0x8:  # close
            raise ConnectionError("CDP: server closed the socket")
        return data.decode("utf-8", "replace")

    def call(self, method, params=None):
        self._next_id += 1
        want = self._next_id
        self._send_text(json.dumps({"id": want, "method": method, "params": params or {}}))
        # Read frames until we see the reply with our id (skip async events).
        deadline = time.monotonic() + TIMEOUT_S
        while time.monotonic() < deadline:
            msg = json.loads(self._recv_text())
            if msg.get("id") == want:
                return msg
        raise TimeoutError(f"CDP call {method} timed out")

    def close(self):
        try:
            self.sock.close()
        except OSError:
            pass


def needs_reload(target, ws):
    url = str(target.get("url", ""))
    if url.startswith("chrome-error://") or url in ("about:blank", ""):
        log(f"page URL indicates failed load ({url!r}) -> reload")
        return True

    resp = ws.call(
        "Runtime.evaluate",
        {"expression": "window.__stellarHeartbeat || 0", "returnByValue": True},
    )
    result = resp.get("result", {}).get("result", {})
    hb = result.get("value", 0)
    try:
        hb = int(hb)
    except (TypeError, ValueError):
        hb = 0

    if hb <= 0:
        # No heartbeat exposed — don't reload on this basis (avoid loops).
        log("no heartbeat exposed; URL healthy -> no action")
        return False

    now_ms = int(time.time() * 1000)
    age = now_ms - hb
    if age > STALE_MS:
        log(f"heartbeat stale ({age} ms > {STALE_MS} ms) -> reload")
        return True
    log(f"heartbeat fresh ({age} ms) -> healthy")
    return False


def main():
    try:
        targets = http_get_json("/json")
    except Exception as e:  # noqa: BLE001 - chromium may simply not be up yet
        log(f"CDP not reachable ({e}); nothing to do")
        return 0

    target = pick_page_target(targets)
    if not target:
        log("no attachable page target; nothing to do")
        return 0

    ws = None
    try:
        ws = CDPSocket(target["webSocketDebuggerUrl"])
        if needs_reload(target, ws):
            ws.call("Page.reload", {"ignoreCache": True})
            log("Page.reload issued")
    except Exception as e:  # noqa: BLE001 - never crash the timer
        log(f"watchdog error (ignored): {e}")
        return 0
    finally:
        if ws:
            ws.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
