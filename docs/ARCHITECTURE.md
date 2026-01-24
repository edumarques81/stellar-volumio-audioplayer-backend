# Stellar Backend Architecture

A minimal, high-performance audio streaming backend focused on bit-perfect playback.

## Design Philosophy

**Simplicity over flexibility.** Unlike Volumio3-backend which supports plugins, multi-room, cloud services, and external integrations, Stellar focuses exclusively on:

1. Bit-perfect local audio playback
2. MPD control via Socket.io API (Volumio-compatible)
3. Single-device operation
4. Audiophile-grade audio configuration

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     WebSocket Clients                        │
│         (Svelte UI, mobile apps, other controllers)         │
└─────────────────────────────────────────────────────────────┘
                              ↑ pushState, pushQueue, etc.
                              │
┌─────────────────────────────────────────────────────────────┐
│                 Socket.io Transport Layer                    │
│            (Volumio-compatible event API)                   │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                      Service Layer                           │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │  Player  │ │  Queue   │ │ Library  │ │  Audio   │       │
│  │ Service  │ │ Service  │ │ Service  │ │ Config   │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                   Infrastructure Layer                       │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │   MPD    │ │   ALSA   │ │  Config  │ │  System  │       │
│  │  Client  │ │  Control │ │  (YAML)  │ │ (Power)  │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                         MPD                                  │
│               (Single Source of Truth)                       │
└─────────────────────────────────────────────────────────────┘
```

## Key Design Decision: No State Machine

### Why Volumio Uses a State Machine

Volumio3-backend has a complex 1500+ line state machine (`statemachine.js`) because it handles:

1. **Volatile mode** - External services (Spotify Connect) can take over playback
2. **Consume mode** - UPnP/DLNA where external devices push content
3. **Multiple plugins** - Each music service reports state differently
4. **State synchronization** - Between plugins with different APIs

### Why Stellar Doesn't Need One

We use **MPD as the Single Source of Truth**:

```go
// Simple state cache - NOT a state machine
type PlayerState struct {
    mu sync.RWMutex

    // Cached from MPD (read-only copy)
    Status     string // "play", "pause", "stop"
    Position   int    // Queue position
    Seek       int    // Milliseconds into track
    Duration   int    // Track duration
    Volume     int    // 0-100
    Random     bool
    Repeat     bool
    RepeatSingle bool

    // Track metadata
    Title      string
    Artist     string
    Album      string
    AlbumArt   string
    URI        string
    TrackType  string
    SampleRate string
    BitDepth   string

    // Bit-perfect indicator
    BitPerfect bool
}
```

### Event-Driven State Updates

```go
// Subscribe to MPD idle events - blocks until change occurs
func (s *PlayerService) watchMPD(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
            // MPD idle blocks until something changes
            subsystems, err := s.mpd.Idle("player", "mixer", "playlist", "options")
            if err != nil {
                continue
            }

            // Fetch fresh state from MPD
            status, _ := s.mpd.Status()
            song, _ := s.mpd.CurrentSong()

            // Update cache atomically
            s.state.UpdateFromMPD(status, song)

            // Broadcast to all WebSocket clients
            s.broadcast("pushState", s.state.ToJSON())
        }
    }
}
```

### Command Handling

Commands go directly to MPD. State updates come back via the idle watcher:

```go
func (s *PlayerService) Play(index *int) error {
    if index != nil {
        return s.mpd.Play(*index)
    }
    // Play current or first track
    return s.mpd.Play(-1)
}

func (s *PlayerService) Pause() error {
    return s.mpd.Pause(true)
}

func (s *PlayerService) SetVolume(vol int) error {
    return s.mpd.SetVolume(vol)
}
```

**Benefits:**
- ~200 lines vs ~1500 in Volumio
- Can never get out of sync with MPD
- No complex state transition logic
- Easier to test and reason about

---

## Feature Comparison: Volumio3 vs Stellar

### Included Features

| Feature | Volumio3 | Stellar | Notes |
|---------|----------|---------|-------|
| Play/Pause/Stop | ✅ | ✅ | Core playback |
| Seek | ✅ | ✅ | Position control |
| Volume | ✅ | ✅ | Hardware/software mixer |
| Shuffle/Repeat | ✅ | ✅ | Playback modes |
| Queue Management | ✅ | ✅ | Add/remove/reorder |
| Library Browsing | ✅ | ✅ | Folder/metadata views |
| Playlists | ✅ | ✅ | M3U-based |
| Favorites | ✅ | ✅ | Starred tracks |
| Audio Output Config | ✅ | ✅ | ALSA device selection |
| Bit-Perfect Mode | ✅ | ✅ | No resampling, hw mixer |
| DSD Playback | ✅ | ✅ | DoP and native DSD |
| Socket.io API | ✅ | ✅ | Volumio-compatible |

### Excluded Features

| Feature | Reason for Exclusion |
|---------|---------------------|
| Plugin System | Adds complexity; we have fixed features |
| Multi-Room | Single-device focus |
| MyVolumio Cloud | No cloud dependencies |
| Spotify Connect | Volatile mode complexity |
| UPnP/DLNA Renderer | Consume mode complexity |
| Plugin Marketplace | No plugin system |
| Wizard/Onboarding | Simple config file |
| REST API | Socket.io is sufficient |
| OTA Updates | Handle externally |
| CD Ripper | Not needed |
| Alarm Clock | Not core function |
| Sleep Timer | Not core function |
| Network Config | Handle externally |
| Firmware Updates | Handle externally |

### Planned Features (Fixed Services)

| Feature | Status | Notes |
|---------|--------|-------|
| Qobuz | Planned | Hi-Res streaming, highest quality (24-bit/192kHz) |
| Tidal | Planned | Hi-Res streaming, MQA support |
| Audirvana | Planned | Debian package integration |

See [STREAMING-SERVICES.md](STREAMING-SERVICES.md) for implementation details.

---

## Socket.io API (Volumio-Compatible)

### Client → Server Events

```typescript
// Playback Control
emit('play', { value?: number })     // Play at index or resume
emit('pause')                        // Pause playback
emit('stop')                         // Stop playback
emit('prev')                         // Previous track
emit('next')                         // Next track
emit('seek', number)                 // Seek to position (seconds)
emit('volume', number)               // Set volume (0-100)
emit('mute', 'mute' | 'unmute')     // Toggle mute

// Playback Options
emit('setRandom', { value: boolean })           // Shuffle
emit('setRepeat', { value: boolean, repeatSingle?: boolean })

// Queue
emit('getQueue')                     // Request queue
emit('addToQueue', { uri, service, ... })
emit('removeFromQueue', { value: number })
emit('moveQueue', { from: number, to: number })
emit('clearQueue')
emit('playPlaylist', { name: string })
emit('enqueue', { name: string })

// Browse
emit('getBrowseSources')             // Get root sources
emit('browseLibrary', { uri: string })
emit('search', { value: string })

// Playlists
emit('listPlaylist')                 // Get playlist names
emit('createPlaylist', { name: string })
emit('deletePlaylist', { name: string })
emit('addToPlaylist', { name: string, uri: string, ... })

// Favorites
emit('addToFavourites', { uri, service, ... })
emit('removeFromFavourites', { uri, service })

// System
emit('getState')                     // Request current state
emit('getSystemInfo')                // System information
emit('getAudioDevices')              // List audio outputs
emit('setAudioDevice', { device: string })
```

### Server → Client Events

```typescript
on('pushState', PlayerState)         // State updates
on('pushQueue', QueueItem[])         // Queue updates
on('pushBrowseSources', Source[])    // Root sources
on('pushBrowseLibrary', BrowseResult)// Browse results
on('pushListPlaylist', string[])     // Playlist names
on('pushToastMessage', Toast)        // Notifications
on('pushTrackInfo', TrackInfo)       // Extended track info
```

---

## Bit-Perfect Audio Configuration

### MPD Configuration for Bit-Perfect

```conf
# /etc/mpd.conf

# Disable all processing
replaygain             "off"
volume_normalization   "no"

# Audio output - direct hardware access
audio_output {
    type            "alsa"
    name            "Singxer SU-6"
    device          "hw:1,0"          # Direct hardware access
    mixer_type      "none"            # No software/hardware mixer
    auto_resample   "no"              # No resampling
    auto_format     "no"              # No format conversion
    auto_channels   "no"              # No channel mixing
    dop             "yes"             # DSD over PCM for compatible DACs
}

# Buffer settings for stability
audio_buffer_size      "8192"
buffer_before_play     "20%"
```

### ALSA Configuration

```conf
# /etc/asound.conf

# Direct hardware access - no dmix, no conversions
pcm.!default {
    type hw
    card 1
    device 0
}

ctl.!default {
    type hw
    card 1
}
```

### Supported Formats

| Format | Max Resolution | Notes |
|--------|---------------|-------|
| PCM | 384kHz / 32-bit | Via USB Audio Class 2 |
| DSD64 | 2.8224 MHz | DoP or native |
| DSD128 | 5.6448 MHz | DoP or native |
| DSD256 | 11.2896 MHz | DoP or native |
| DSD512 | 22.5792 MHz | Native only (Singxer SU-6) |

### Bit-Perfect Detection

```go
func (s *AudioService) IsBitPerfect() bool {
    // Check MPD config
    mpdConf := s.readMPDConfig()

    return mpdConf.AutoResample == "no" &&
           mpdConf.AutoFormat == "no" &&
           mpdConf.MixerType == "none" &&
           strings.HasPrefix(mpdConf.Device, "hw:")
}
```

---

## Project Structure

```
stellar-volumio-audioplayer-backend/
├── cmd/
│   └── stellar/
│       └── main.go              # Entry point
├── internal/
│   ├── domain/
│   │   ├── player/
│   │   │   ├── state.go         # Player state (cache)
│   │   │   ├── state_test.go
│   │   │   └── service.go       # Player service
│   │   ├── queue/
│   │   │   ├── queue.go         # Queue management
│   │   │   └── service.go
│   │   ├── library/
│   │   │   ├── browse.go        # Library browsing
│   │   │   └── service.go
│   │   ├── playlist/
│   │   │   └── service.go       # Playlist CRUD
│   │   └── audio/
│   │       ├── config.go        # Audio output config
│   │       └── service.go
│   ├── infra/
│   │   ├── mpd/
│   │   │   ├── client.go        # MPD client wrapper
│   │   │   └── client_test.go
│   │   ├── alsa/
│   │   │   └── control.go       # ALSA device enumeration
│   │   └── config/
│   │       └── config.go        # YAML configuration
│   └── transport/
│       ├── socketio/
│       │   ├── server.go        # Socket.io server
│       │   ├── handlers.go      # Event handlers
│       │   └── broadcast.go     # Client broadcasting
│       └── http/
│           └── server.go        # Static file serving
├── pkg/
│   └── volumio/
│       └── types.go             # Volumio-compatible types
├── configs/
│   ├── stellar.yaml             # Main config
│   └── mpd.conf.tmpl            # MPD config template
├── docs/
│   └── ARCHITECTURE.md          # This file
├── go.mod
├── go.sum
└── README.md
```

---

## Implementation Phases

### Phase 1: Core Player (Current)
- [x] Player state struct with thread-safe access
- [x] Basic tests
- [ ] MPD client integration
- [ ] Socket.io server with pushState

### Phase 2: Queue & Library
- [ ] Queue service (MPD playlist)
- [ ] Library browsing (MPD database)
- [ ] Search functionality

### Phase 3: Playlists & Favorites
- [ ] Playlist CRUD (M3U files)
- [ ] Favorites management

### Phase 4: Audio Configuration
- [ ] ALSA device enumeration
- [ ] Audio output selection
- [ ] Bit-perfect configuration
- [ ] MPD config generation

### Phase 5: Polish
- [ ] Error handling
- [ ] Logging
- [ ] Graceful shutdown
- [ ] Performance optimization

---

## Dependencies

```go
// go.mod
require (
    github.com/fhs/gompd/v2 v2.3.0     // MPD client
    github.com/googollee/go-socket.io v1.7.0  // Socket.io
    github.com/spf13/viper v1.18.0      // Configuration
    github.com/rs/zerolog v1.31.0       // Logging
)
```

---

## Configuration

```yaml
# configs/stellar.yaml

server:
  port: 3000
  host: "0.0.0.0"

mpd:
  host: "localhost"
  port: 6600
  music_directory: "/var/lib/mpd/music"
  playlist_directory: "/var/lib/mpd/playlists"

audio:
  default_device: "hw:1,0"
  bit_perfect: true
  dop: true

logging:
  level: "info"
  format: "json"
```

---

## References

- [Volumio3-backend](https://github.com/volumio/volumio3-backend) - API inspiration
- [MPD Protocol](https://mpd.readthedocs.io/en/latest/protocol.html)
- [gompd](https://github.com/fhs/gompd) - Go MPD client
- [go-socket.io](https://github.com/googollee/go-socket.io) - Socket.io for Go
