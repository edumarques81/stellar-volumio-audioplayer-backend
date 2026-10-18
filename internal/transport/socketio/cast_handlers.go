package socketio

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"

	domaincast "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/cast"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library"
	infracast "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cast"
)

// CastManager is the slice of domain/cast.Manager this transport needs,
// declared as an interface so the handlers can be tested without a Cast
// device or a TLS dial.
type CastManager interface {
	Devices() []infracast.Device
	RefreshDevices(ctx context.Context) ([]infracast.Device, error)
	Connect(ctx context.Context, deviceID string) error
	Disconnect()
	StopCasting() error
	Active() bool
	Play(items []domaincast.Item, startIndex int) ([]domaincast.Skipped, error)
	Resume() error
	Pause() error
	TogglePause() error
	Stop() error
	Next() error
	Previous() error
	Seek(seconds int) error
	SetVolume(percent int) error
	SetMuted(muted bool) error
	State() domaincast.State
	OnState(func(domaincast.State))
}

var _ CastManager = (*domaincast.Manager)(nil)

// castDevice is the device shape the clients see. It is deliberately not
// infracast.Device: the socket address is an implementation detail no client
// has any use for, and leaking it invites a client to try to reach the device
// itself.
type castDevice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Model   string `json:"model,omitempty"`
	IsGroup bool   `json:"isGroup,omitempty"`
}

// castDiscoveryTimeout bounds one mDNS browse. The Discoverer keeps its last
// good result, so the ceiling costs a stale list rather than an empty one.
const castDiscoveryTimeout = 4 * time.Second

// castConnectTimeout covers discovery plus the TLS handshake plus LAUNCH. A
// Nest Hub that has gone to sleep takes a couple of seconds to answer.
const castConnectTimeout = 15 * time.Second

func castDevices(devices []infracast.Device) []castDevice {
	out := make([]castDevice, 0, len(devices))
	for _, d := range devices {
		out = append(out, castDevice{ID: d.ID, Name: d.Name, Model: d.Model, IsGroup: d.IsGroup})
	}
	return out
}

// castBundle is captured by UseCast at construction time and looked up by the
// per-client listeners, mirroring airplayBundle.
//
// The library is held here rather than read back through LibraryService() so
// the dependency is explicit: casting an album means resolving it to tracks,
// and a Cast session with no library would fail at the first play with
// nothing useful to say.
type castBundle struct {
	mgr CastManager
	lib LibraryService
}

// castResponse is the ack shape for every cast:* event. Skipped carries the
// tracks the format policy refused, so a client can tell the user why an
// album is shorter on the Hub than it is on the DAC instead of silently
// losing them.
type castResponse struct {
	OK      bool               `json:"ok"`
	Error   string             `json:"error,omitempty"`
	Devices []castDevice       `json:"devices,omitempty"`
	Skipped []castSkippedTrack `json:"skipped,omitempty"`
}

type castSkippedTrack struct {
	Title  string `json:"title"`
	URI    string `json:"uri"`
	Reason string `json:"reason"`
}

// UseCast wires the Cast session manager. Must be called BEFORE the HTTP
// server starts accepting connections — same constraint as UseAirplay, for
// the same reason: the per-client listeners are attached on connect.
func (s *Server) UseCast(mgr CastManager, lib LibraryService) {
	if mgr == nil {
		return
	}
	s.cast = &castBundle{mgr: mgr, lib: lib}
	mgr.OnState(func(state domaincast.State) {
		s.io.Emit("pushCastState", state)
	})
}

// CastIsActive reports whether a Cast session is in progress. Returns false
// when UseCast was not called. INFORMATIONAL only — like AirplayIsActive,
// it must not gate readiness.
func (s *Server) CastIsActive() bool {
	if s.cast == nil || s.cast.mgr == nil {
		return false
	}
	return s.cast.mgr.Active()
}

// pushCastStateTo emits the current session snapshot to one client.
//
// It emits EVEN WHEN INACTIVE, for the same reason pushAirplayStateTo does: a
// client that missed the terminal frame on a flaky socket is stuck showing a
// cast session that ended, and an authoritative inactive snapshot on reconnect
// is the only thing that flips it back to the DAC.
func (s *Server) pushCastStateTo(client *socket.Socket) {
	if s.cast == nil || s.cast.mgr == nil {
		return
	}
	_ = client.Emit("pushCastState", s.cast.mgr.State())
}

// registerCastClient attaches the cast:* listeners to a freshly-connected
// socket. No-op when UseCast was not invoked.
func (s *Server) registerCastClient(client *socket.Socket) {
	if s.cast == nil || s.cast.mgr == nil {
		return
	}
	mgr := s.cast.mgr

	_ = client.On("getCastState", func(_ ...any) {
		s.pushCastStateTo(client)
	})

	// Discovery is a cached mDNS browse, so this is cheap to call on every
	// open of the output picker. The cached list is returned even when the
	// browse fails, so a transient mDNS hiccup does not empty the picker.
	_ = client.On("cast:listDevices", func(args ...any) {
		ctx, cancel := context.WithTimeout(context.Background(), castDiscoveryTimeout)
		defer cancel()

		devices := s.castLookupDevices(ctx)
		s.io.Emit("pushCastDevices", devices)
		castAck(client, args, castResponse{OK: true, Devices: devices})
	})

	_ = client.On("cast:connect", func(args ...any) {
		payload := castPayload(args)
		deviceID := getString(payload, "deviceId")
		if deviceID == "" {
			castAck(client, args, castResponse{Error: "missing deviceId"})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), castConnectTimeout)
		defer cancel()

		if err := mgr.Connect(ctx, deviceID); err != nil {
			log.Warn().Err(err).Str("deviceId", deviceID).Msg("cast connect failed")
			castAck(client, args, castResponse{Error: err.Error()})
			return
		}
		log.Info().Str("deviceId", deviceID).Msg("cast session connected")
		castAck(client, args, castResponse{OK: true})
	})

	// Disconnect drops our connection and leaves the device playing; stop
	// quits the receiver app. Both are offered because they are genuinely
	// different intents — "I'm done driving this" versus "be quiet".
	_ = client.On("cast:disconnect", func(args ...any) {
		mgr.Disconnect()
		castAck(client, args, castResponse{OK: true})
	})

	_ = client.On("cast:stop", func(args ...any) {
		if err := mgr.StopCasting(); err != nil {
			castAck(client, args, castResponse{Error: err.Error()})
			return
		}
		castAck(client, args, castResponse{OK: true})
	})

	_ = client.On("cast:playAlbum", func(args ...any) {
		castAck(client, args, s.castPlayAlbum(castPayload(args)))
	})

	_ = client.On("cast:command", func(args ...any) {
		payload := castPayload(args)
		resp := s.castCommand(payload)
		castAck(client, args, resp)
		log.Debug().Interface("payload", payload).Bool("ok", resp.OK).Str("error", resp.Error).Msg("cast:command")
	})
}

// castLookupDevices browses for devices and falls back to the Discoverer's
// cached list when the browse fails, so a transient mDNS hiccup shows a
// slightly stale output picker rather than an empty one.
func (s *Server) castLookupDevices(ctx context.Context) []castDevice {
	if s.cast == nil || s.cast.mgr == nil {
		return nil
	}
	found, err := s.cast.mgr.RefreshDevices(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("cast device discovery failed")
		found = s.cast.mgr.Devices()
	}
	return castDevices(found)
}

// castPlayAlbum resolves an album to its tracks and hands them to the
// session manager, which applies the format policy.
//
// sampleRate is taken from the payload because it is album-level in the cache
// and the client already holds the Album it tapped. When it is absent the
// policy sees 0 and treats the album as playable — an unmeasured album is not
// hidden on a guess, and DSD is still caught by its extension.
func (s *Server) castPlayAlbum(payload map[string]interface{}) castResponse {
	if s.cast == nil || s.cast.mgr == nil {
		return castResponse{Error: "cast is not available"}
	}
	lib := s.cast.lib
	if lib == nil {
		return castResponse{Error: "library is not available"}
	}

	album := getString(payload, "album")
	if album == "" {
		return castResponse{Error: "missing album"}
	}

	resp := lib.GetAlbumTracks(library.GetAlbumTracksRequest{
		Album:       album,
		AlbumArtist: getString(payload, "albumArtist"),
		URI:         getString(payload, "uri"),
	})
	if resp.Error != "" {
		return castResponse{Error: resp.Error}
	}
	if len(resp.Tracks) == 0 {
		return castResponse{Error: "album has no tracks"}
	}

	sampleRate := getIntFromMap(payload, "sampleRate", 0)
	items := make([]domaincast.Item, 0, len(resp.Tracks))
	for _, t := range resp.Tracks {
		items = append(items, domaincast.Item{
			URI:         t.URI,
			Title:       t.Title,
			Artist:      t.Artist,
			Album:       t.Album,
			AlbumArtist: resp.AlbumArtist,
			TrackNumber: t.TrackNumber,
			DiscNumber:  t.Disc,
			DurationSec: t.Duration,
			SampleRate:  sampleRate,
			ArtPath:     t.AlbumArt,
		})
	}

	skipped, err := s.cast.mgr.Play(items, getIntFromMap(payload, "startIndex", 0))
	out := castResponse{OK: err == nil, Skipped: castSkipped(skipped)}
	if err != nil {
		out.Error = err.Error()
		log.Warn().Err(err).Str("album", album).Msg("cast play failed")
	}
	return out
}

func castSkipped(skipped []domaincast.Skipped) []castSkippedTrack {
	if len(skipped) == 0 {
		return nil
	}
	out := make([]castSkippedTrack, 0, len(skipped))
	for _, s := range skipped {
		out = append(out, castSkippedTrack{Title: s.Item.Title, URI: s.Item.URI, Reason: s.Reason})
	}
	return out
}

// castCommand dispatches a transport command to the active session.
//
// Cast transport is a separate event rather than a takeover of `play` /
// `pause` / `seek`, which keeps the MPD handlers and the state-diffing
// broadcast path untouched — the same split AirPlay uses with
// `airplay:command`. A client in cast mode drives this; a client that is not
// keeps driving the DAC.
func (s *Server) castCommand(payload map[string]interface{}) castResponse {
	if s.cast == nil || s.cast.mgr == nil {
		return castResponse{Error: "cast is not available"}
	}
	cmd := getString(payload, "cmd")
	if cmd == "" {
		return castResponse{Error: "missing cmd"}
	}

	mgr := s.cast.mgr
	var err error
	switch cmd {
	case "play":
		err = mgr.Resume()
	case "pause":
		err = mgr.Pause()
	case "toggle", "playpause":
		err = mgr.TogglePause()
	case "stop":
		err = mgr.Stop()
	case "next":
		err = mgr.Next()
	case "prev", "previous":
		err = mgr.Previous()
	case "seek":
		err = mgr.Seek(getIntFromMap(payload, "value", 0))
	case "volume":
		err = mgr.SetVolume(getIntFromMap(payload, "value", 0))
	case "mute":
		muted, _ := payload["value"].(bool)
		err = mgr.SetMuted(muted)
	default:
		err = errors.New("unknown cmd: " + cmd)
	}
	if err != nil {
		return castResponse{Error: err.Error()}
	}
	return castResponse{OK: true}
}

// castPayload picks the event body out of the socket.io args, which carry an
// optional trailing ack callback.
func castPayload(args []any) map[string]interface{} {
	for _, a := range args {
		if m, ok := a.(map[string]interface{}); ok {
			return m
		}
	}
	return nil
}

// castAck replies through the ack callback when the caller supplied one, and
// falls back to a pushCastCommandResult event when it did not — so a caller
// that emitted without an ack can still observe the outcome.
func castAck(client *socket.Socket, args []any, resp castResponse) {
	for _, a := range args {
		if cb, ok := a.(func([]any, error)); ok {
			cb([]any{resp}, nil)
			return
		}
	}
	_ = client.Emit("pushCastCommandResult", resp)
}
