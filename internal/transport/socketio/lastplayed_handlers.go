package socketio

import (
	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/lastplayed"
)

// LastPlayedService is the subset of *lastplayed.Service that handlers need.
// Interface so tests can substitute a fake.
type LastPlayedService interface {
	Get() (lastplayed.Album, bool, error)
}

// LastPlayedHandlers wires Socket.IO last-played events to the service.
type LastPlayedHandlers struct {
	svc LastPlayedService
}

// NewLastPlayedHandlers constructs a handler bundle. A nil svc is allowed; in
// that case events emit a null payload — handy when the cache DB is offline.
func NewLastPlayedHandlers(svc LastPlayedService) *LastPlayedHandlers {
	return &LastPlayedHandlers{svc: svc}
}

// RegisterHandlers binds events on a freshly connected client.
//
// Events:
//
//	library:lastPlayed:get → fetch the most-recently-played album row
//
// Emits pushLastPlayedAlbum with {artist, album, albumArt, trackUri,
// trackType, sampleRate, bitDepth} on hit, or null on miss / error.
// Field naming matches the frontend's Album shape (camelCase).
func (h *LastPlayedHandlers) RegisterHandlers(client *socket.Socket) {
	client.On("library:lastPlayed:get", func(args ...interface{}) {
		h.PushTo(client)
	})
}

// PushTo emits pushLastPlayedAlbum to a single client. Used both by the
// library:lastPlayed:get handler and by the connect-time emit batch in
// Server.OnConnection so freshly connected clients hydrate without an
// extra round-trip.
func (h *LastPlayedHandlers) PushTo(client *socket.Socket) {
	if h == nil {
		return
	}
	client.Emit("pushLastPlayedAlbum", h.payload())
}

// BroadcastTo emits pushLastPlayedAlbum to all connected clients. Called
// from the MPD watcher right after a new album is recorded.
func (h *LastPlayedHandlers) BroadcastTo(io *socket.Server) {
	if h == nil || io == nil {
		return
	}
	io.Emit("pushLastPlayedAlbum", h.payload())
}

// payload builds the wire-format map. Returns nil on miss, error, or no svc;
// nil emits as JSON null on the wire, which the frontend treats as "no resume".
func (h *LastPlayedHandlers) payload() map[string]interface{} {
	if h.svc == nil {
		return nil
	}
	album, ok, err := h.svc.Get()
	if err != nil {
		log.Debug().Err(err).Msg("library:lastPlayed:get error; returning null")
		return nil
	}
	if !ok {
		return nil
	}
	return map[string]interface{}{
		"artist":     album.Artist,
		"album":      album.Album,
		"albumArt":   album.AlbumArt,
		"trackUri":   album.TrackURI,
		"trackType":  album.TrackType,
		"sampleRate": album.SampleRate,
		"bitDepth":   album.BitDepth,
	}
}
