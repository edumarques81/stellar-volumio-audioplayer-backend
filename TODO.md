# Stellar Audio Player Backend - TODO

## High Priority

### Security

- [ ] **Credential encryption** - Encrypt streaming service credentials before storing in config file
  - Use AES-256 encryption
  - Derive key from device-specific hardware ID (e.g., CPU serial, MAC address)
  - Never store passwords in plain text
  - Clear credentials from memory after use
  - See: [STREAMING-SERVICES.md](docs/STREAMING-SERVICES.md)

### Streaming Services

- [ ] **Qobuz integration** - Hi-Res streaming service
  - **⚠️ Auth note:** Eduardo's Qobuz account was created via "Continue with Google" — no password set. To fix: qobuz.com → Account Settings → set a standalone password. Then `gobuz` (email+password) will work.
  - [ ] Implement Qobuz client using [gobuz](https://pkg.go.dev/github.com/markhc/gobuz) library
  - [ ] Web player credential extraction (development mode)
  - [ ] User login/logout via Socket.IO
  - [ ] Browse library (albums, artists, playlists, featured)
  - [ ] Search functionality
  - [ ] Streaming URL resolution for MPD playback
  - [ ] Always use highest quality available (24-bit/192kHz FLAC)
  - [ ] Official API credentials request (api@qobuz.com) - for production

- [ ] **Tidal integration** - Hi-Res streaming service
  - [ ] Research Tidal API libraries for Go
  - [ ] OAuth2 authentication flow
  - [ ] Browse and search
  - [ ] Streaming URL resolution
  - [ ] MQA support (if applicable)

- [ ] **Audirvana integration** - UPnP/DLNA Renderer (Phase 2)
  - **Architecture:** Expose Stellar as a UPnP AV renderer so Audirvana auto-discovers it via SSDP and can stream to it
  - **Background:** Audirvana is a UPnP Control Point (strings `urn:schemas-upnp-org:service:AVTransport:1`, `RenderingControl:1`, `ConnectionManager:1` confirmed in binary). No reverse-engineering needed — standard UPnP.
  - **Data flow:** Audirvana discovers "Stellar" → user selects it as output → Audirvana sends `SetAVTransportURI` (stream URL + full metadata: title, artist, album, art, duration) → Stellar passes URL to MPD → emits `pushState` to frontend. `Play/Pause/Stop/Seek` forwarded via `AVTransport`.
  - **What to build in Go** (`internal/domain/upnp/`):
    - [ ] UPnP device description XML (`AVTransport` + `RenderingControl` + `ConnectionManager`)
    - [ ] HTTP server serving device description + SOAP action endpoints
    - [ ] SSDP announcements for auto-discovery on local network
    - [ ] `SetAVTransportURI` handler → extract stream URL + metadata → MPD play → `pushState`
    - [ ] `Play/Pause/Stop/Seek` SOAP handlers → forward to MPD coordinator
    - [ ] `GetPositionInfo` / `GetTransportInfo` handlers (Audirvana polls these for playback position)
  - **Go libraries:** `github.com/huin/goupnp` or `gitlab.com/mipimipi/go-upnp`
  - **Research doc:** `docs/AUDIRVANA_INVESTIGATION.md` — full prior investigation (Jan 2026)
  - **Complexity:** Medium-high. 2–3 evenings with agent. UPnP XML schema verbose but well-documented.

## Investigation / Experiments

- [ ] **WebSocket communication** - Investigate and experiment using WebSockets for all communication between Go backend and JS frontend (instead of Socket.IO)

## Future Improvements

- [ ] **MPD connection pooling** - Improve MPD connection handling for high-concurrency scenarios
- [ ] **Caching layer** - Add caching for frequently accessed data (library metadata, album art)
- [ ] **Graceful reconnection** - Better handling of MPD disconnections with automatic recovery

## Completed

- [x] Socket.IO v4 compatible server using zishang520/socket.io
- [x] MPD client wrapper with connection management
- [x] Player state broadcasting via Socket.IO
- [x] Browse library functionality
- [x] Album art endpoint (embedded + directory art)
- [x] Network status endpoint (WiFi/Ethernet detection)
- [x] Static file serving for frontend
