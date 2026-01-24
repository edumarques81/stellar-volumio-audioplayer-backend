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

- [ ] **Audirvana integration** - Desktop audio player
  - [ ] Research Audirvana integration options
  - [ ] Debian package installation
  - [ ] API/control integration

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
