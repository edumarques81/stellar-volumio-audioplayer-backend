# Streaming Services Integration

This document describes the architecture and implementation of streaming service integrations in Stellar.

## Supported Services

| Service | Status | Quality | Notes |
|---------|--------|---------|-------|
| Qobuz | Planned | Up to 24-bit/192kHz FLAC | Hi-Res streaming |
| Tidal | Planned | Up to 24-bit/192kHz FLAC | Hi-Res streaming |
| Audirvana | Planned | TBD | Debian package integration |

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     Frontend (Svelte)                        │
│  Login UI │ Browse │ Search │ Queue │ Settings              │
└─────────────────────────────────────────────────────────────┘
                              │
                     Socket.IO Events
                              │
┌─────────────────────────────────────────────────────────────┐
│                  Stellar Backend (Go)                        │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Streaming Service Layer                  │   │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────────┐            │   │
│  │  │  Qobuz  │ │  Tidal  │ │  Audirvana  │            │   │
│  │  │ Service │ │ Service │ │   Service   │            │   │
│  │  └─────────┘ └─────────┘ └─────────────┘            │   │
│  └──────────────────────────────────────────────────────┘   │
│                              │                               │
│                    Streaming URLs                            │
│                              │                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │                    MPD Client                         │   │
│  │              (Plays HTTPS streaming URLs)             │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Audio Quality Policy

**Always use the highest quality available.** Stellar is an audiophile-focused player, so we default to:

- **Qobuz**: 24-bit/192kHz FLAC (format ID: 27) when available
- **Tidal**: MQA or 24-bit FLAC (Hi-Res) when available

Quality fallback order:
1. 24-bit/192kHz FLAC (Hi-Res)
2. 24-bit/96kHz FLAC (Hi-Res)
3. 16-bit/44.1kHz FLAC (CD Quality)
4. MP3 320kbps (fallback only)

---

## Qobuz Integration

### API Credentials

Qobuz requires application credentials (APP_ID and APP_SECRET) to access their API.

#### Option 1: Official API Access (Recommended for Production)

Contact Qobuz directly to request API access:

- **Email**: api@qobuz.com
- **Subject**: API Access Request - Open Source Project
- **Include**:
  - Project name: Stellar Volumio Audio Player
  - GitHub URL: https://github.com/edumarques81/stellar-volumio-audioplayer-backend
  - Use case: Open source audiophile music player for Raspberry Pi
  - License: MIT

**Note**: Official API access provides stable credentials that won't break unexpectedly.

#### Option 2: Web Player Credential Extraction (Development)

For development and testing, credentials can be extracted from the Qobuz web player. This method is less stable as credentials may change when Qobuz updates their web player.

**Warning**: Web player credentials should only be used for personal/development use. For production deployments, obtain official API credentials.

### Go Library

We use the [gobuz](https://pkg.go.dev/github.com/markhc/gobuz) library (MIT licensed) for Qobuz API access.

```bash
go get -u github.com/markhc/gobuz
```

Features:
- User authentication (email/password)
- Album, artist, track, playlist browsing
- Search functionality
- Streaming URL generation
- Multiple audio format support

### Socket.IO Events

| Event (Client → Server) | Event (Server → Client) | Description |
|------------------------|-------------------------|-------------|
| `qobuzLogin` | `pushQobuzLoginResult` | Authenticate with email/password |
| `qobuzLogout` | `pushQobuzLogoutResult` | Clear session |
| `getQobuzStatus` | `pushQobuzStatus` | Check login status |
| `browseLibrary` (uri: `qobuz://...`) | `pushBrowseLibrary` | Browse Qobuz content |

### URI Scheme

| URI Pattern | Description |
|-------------|-------------|
| `qobuz://` | Root menu (My Albums, Playlists, Featured) |
| `qobuz://myalbums` | User's purchased/favorite albums |
| `qobuz://myplaylists` | User's playlists |
| `qobuz://featured` | Editorial/curated content |
| `qobuz://album/{id}` | Album tracks |
| `qobuz://artist/{id}` | Artist discography |
| `qobuz://playlist/{id}` | Playlist tracks |
| `qobuz://track/{id}` | Single track (for queue) |

### Playback Flow

1. User browses Qobuz library via `browseLibrary` event
2. User adds track to queue (URI: `qobuz://track/{id}`)
3. When MPD plays the track:
   - Backend intercepts the Qobuz URI
   - Calls Qobuz API to get streaming URL
   - Passes HTTPS URL to MPD for playback
4. MPD decodes and plays the audio stream

---

## Credential Storage

### Current Implementation

Credentials are stored in the same configuration file as NAS shares:

```
~/.stellar/config.json
```

```json
{
  "nas_shares": { ... },
  "streaming": {
    "qobuz": {
      "email": "user@example.com",
      "password": "...",
      "auth_token": "..."
    },
    "tidal": {
      "email": "user@example.com",
      "password": "...",
      "auth_token": "..."
    }
  }
}
```

### Security TODO

**IMPORTANT**: Credentials must be encrypted before storing.

Current status: Credentials stored in plain text (development only).

Required improvements:
1. Implement AES-256 encryption for credentials
2. Use device-specific key (derived from hardware ID)
3. Store only encrypted blobs in config file
4. Clear credentials from memory after use

See: [TODO.md](../TODO.md) for tracking.

---

## Tidal Integration (Planned)

Similar architecture to Qobuz, using a Tidal API library.

### Differences from Qobuz:
- OAuth2 authentication flow
- Different URI scheme: `tidal://...`
- MQA format support (if DAC supports it)

---

## Audirvana Integration (Planned)

Audirvana requires a different approach as it runs as a separate service.

### Installation
- Install via Debian package (`.deb`)
- Configure integration endpoint

### Integration Points
- TBD - needs research on Audirvana's API/integration capabilities

---

## References

- [gobuz Go library](https://pkg.go.dev/github.com/markhc/gobuz) - MIT licensed Qobuz API client
- [Qobuz API Documentation](https://github.com/csngoh/api-documentation) - Official API reference
- [QobuzApiSharp](https://github.com/DJDoubleD/QobuzApiSharp) - C# reference implementation
- [python-qobuz](https://github.com/taschenb/python-qobuz) - Python reference implementation
