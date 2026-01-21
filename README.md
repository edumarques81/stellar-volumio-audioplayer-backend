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

- [ ] Socket.io API (Volumio-compatible)
- [ ] MPD integration for playback control
- [ ] Music library browsing
- [ ] Queue management
- [ ] Playlist and favorites
- [ ] Bit-perfect audio configuration
- [ ] LCD brightness control
- [ ] System management (power, network)

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
│   │   ├── library/       # Music library
│   │   ├── queue/         # Queue management
│   │   └── ...
│   ├── infra/             # Infrastructure adapters
│   │   ├── mpd/           # MPD client
│   │   ├── alsa/          # ALSA control
│   │   └── ...
│   └── transport/         # HTTP/WebSocket handlers
├── pkg/                   # Public packages
└── configs/               # Configuration templates
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md) - System design, MPD-as-source-of-truth pattern
- [Bit-Perfect Audio](../Volumio2-UI/volumio-poc/docs/BIT-PERFECT-AUDIO.md) - Audio configuration for audiophile playback
- [State Machine Issues](../Volumio2-UI/volumio-poc/docs/STATE-MACHINE-ISSUES.md) - Why we don't use Volumio's state machine

## License

MIT License - See [LICENSE](LICENSE) for details.

## Acknowledgments

- [Volumio](https://volumio.com/) - For pioneering open-source audiophile streaming
- [volumio3-backend](https://github.com/volumio/volumio3-backend) - API inspiration
- [gompd](https://github.com/fhs/gompd) - Go MPD client library
