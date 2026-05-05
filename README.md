# Stellar Volumio Audio Player Backend

A high-performance, bit-perfect audio streaming backend for dedicated audio appliances, written in Go.

## Inspiration

This project is inspired by and builds upon the excellent work of the [Volumio](https://volumio.com/) team and their [volumio3-backend](https://github.com/volumio/volumio3-backend). While Volumio provides a comprehensive, plugin-based audio platform, Stellar focuses on:

- **Bit-perfect audio**: Optimized for audiophile-grade playback (PCM up to 384kHz/32-bit, DSD512)
- **Minimal footprint**: Single binary, low memory usage for dedicated appliances
- **Hardware integration**: Direct ALSA, GPIO, and LCD control
- **Socket.io compatibility**: Works with existing Volumio-compatible frontends

## Target Hardware

- Raspberry Pi 5 (primary target)
- USB DACs and DDCs (e.g., Singxer SU-6)
- 1920x440 LCD touchscreen displays

## Features

- [x] Socket.io API (Volumio-compatible)
- [x] MPD integration for playback control
- [x] Music library browsing with SQLite cache
- [x] Artwork caching with multi-source fallback
- [x] Queue management
- [ ] Playlist and favorites
- [x] Bit-perfect audio configuration
- [x] LCD brightness control
- [x] System management (power, network)
- [x] NAS share management (CIFS/NFS)
- [ ] Streaming services (Qobuz, Tidal, Audirvana)

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Transport Layer                         │
│         Socket.io Server  │  REST API  │  Static Files     │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                     Service Layer                           │
│   Player │ Library │ Queue │ Playlist │ System │ Audio     │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                  Infrastructure Layer                       │
│        MPD Client │ SQLite │ ALSA │ GPIO │ Config          │
└─────────────────────────────────────────────────────────────┘
```

## Development

### Prerequisites

- Go 1.21+
- MPD (for integration tests)

### Quick Start

```bash
# Run tests
go test ./...

# Run with coverage
go test -cover ./...

# Build
go build -o stellar ./cmd/stellar

# Run
./stellar
```

### Project Structure

```
├── cmd/
│   └── stellar/           # Application entry point
├── internal/
│   ├── domain/            # Business logic
│   │   ├── player/        # Player service
│   │   ├── library/       # Music library + cached service
│   │   ├── artwork/       # Artwork resolution + caching
│   │   ├── queue/         # Queue management
│   │   └── ...
│   ├── infra/             # Infrastructure adapters
│   │   ├── mpd/           # MPD client with capability detection
│   │   ├── cache/         # SQLite library cache
│   │   ├── alsa/          # ALSA control
│   │   └── ...
│   └── transport/         # HTTP/WebSocket handlers
├── pkg/                   # Public packages
└── configs/               # Configuration templates
```

## Bio summarization (album/artist info)

The Library screen displays 2-3 sentence album/artist bios sourced from
Wikipedia and summarized via an LLM. Configure with environment variables:

| Var | Required | Default | Notes |
|---|---|---|---|
| `ANTHROPIC_API_KEY` | No | (unset) | When unset, bios are returned empty (noop LLM). |
| `ANTHROPIC_MODEL`   | No | `claude-haiku-4-5-20251001` | Override to test other Claude models. |

Cache TTL is 90 days (defined in `internal/domain/bios/service.go`). The
backend never errors on bio lookups — Wikipedia 404s, LLM rate limits,
and missing API keys all degrade to an empty bio so the UI strip simply
collapses.

Socket.IO event contract:

- `library:bio:get` `{artist, album}` → `pushLibraryBio` `{artist, album, summary, source_url, kind}`
- `library:bio:rebuild` (same shape) — explicitly invalidates the cached row before refetch

## Power actions

`system:shutdown` and `system:reboot` Socket.IO events are gated to
**loopback callers only** (127.0.0.1 / ::1). Remote clients (e.g. mobile
controllers on a different host) cannot trigger power actions over the
network — that's by design. Failures emit `system:action:error` with
`{action, error}` so the UI Power modal can show a toast and stay open.

The legacy unauthenticated `shutdown` / `reboot` events from earlier
Volumio-compatible API still exist for backward compatibility; new
frontend code should use the namespaced `system:*` events.

## Documentation

- [Architecture](docs/ARCHITECTURE.md) - System design, MPD-as-source-of-truth pattern
- [Streaming Services](docs/STREAMING-SERVICES.md) - Qobuz, Tidal, Audirvana integration
- [Bit-Perfect Audio](../Volumio2-UI/volumio-poc/docs/BIT-PERFECT-AUDIO.md) - Audio configuration for audiophile playback
- [State Machine Issues](../Volumio2-UI/volumio-poc/docs/STATE-MACHINE-ISSUES.md) - Why we don't use Volumio's state machine

## License

MIT License - See [LICENSE](LICENSE) for details.

## Acknowledgments

- [Volumio](https://volumio.com/) - For pioneering open-source audiophile streaming
- [volumio3-backend](https://github.com/volumio/volumio3-backend) - API inspiration
- [gompd](https://github.com/fhs/gompd) - Go MPD client library
