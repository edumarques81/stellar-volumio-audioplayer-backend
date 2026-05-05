package socketio

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/bios"
)

// BioService is the subset of *bios.Service that handlers need (interface for tests).
type BioService interface {
	GetAlbumBio(ctx context.Context, artist, album string) (bios.Bio, error)
	RefreshAlbumBio(ctx context.Context, artist, album string) (bios.Bio, error)
}

// BioHandlers wires Socket.IO bio events to the bio service.
type BioHandlers struct {
	svc BioService
}

// NewBioHandlers constructs a handler bundle. A nil svc is allowed but
// every event will respond with an error event — useful for boot paths
// where the cache DB is unavailable.
func NewBioHandlers(svc BioService) *BioHandlers {
	return &BioHandlers{svc: svc}
}

// RegisterHandlers binds events on a freshly connected client.
// Mirrors LibraryHandlers.RegisterHandlers (server.go connection callback).
//
// Events:
//
//	library:bio:get      → fetch (cache-first) bio for {artist, album}
//	library:bio:rebuild  → refresh (cache-invalidate then fetch) bio
//
// Both emit pushLibraryBio with {artist, album, summary, source_url, kind}.
// Service errors are swallowed into empty bios so the UI strip simply
// collapses (per spec decision 62).
func (h *BioHandlers) RegisterHandlers(client *socket.Socket) {
	client.On("library:bio:get", func(args ...interface{}) {
		payload, _ := firstMapArg(args)
		resp, err := h.handleGetBioInternal(payload)
		if err != nil {
			log.Warn().Err(err).Msg("library:bio:get rejected")
			return
		}
		_ = client.Emit("pushLibraryBio", resp)
	})

	client.On("library:bio:rebuild", func(args ...interface{}) {
		payload, _ := firstMapArg(args)
		resp, err := h.handleRefreshBioInternal(payload)
		if err != nil {
			log.Warn().Err(err).Msg("library:bio:rebuild rejected")
			return
		}
		_ = client.Emit("pushLibraryBio", resp)
	})
}

func (h *BioHandlers) handleGetBioInternal(payload map[string]interface{}) (map[string]interface{}, error) {
	artist, album, err := requireArtistAlbum(payload)
	if err != nil {
		return nil, err
	}
	if h.svc == nil {
		return nil, errors.New("bio service unavailable")
	}

	bio, err := h.svc.GetAlbumBio(context.Background(), artist, album)
	if err != nil {
		// Graceful degradation: return empty bio rather than failing the socket call.
		log.Debug().Err(err).Str("artist", artist).Str("album", album).Msg("GetAlbumBio error; returning empty")
		return emptyBioPayload(artist, album), nil
	}
	return bioPayload(artist, album, bio), nil
}

func (h *BioHandlers) handleRefreshBioInternal(payload map[string]interface{}) (map[string]interface{}, error) {
	artist, album, err := requireArtistAlbum(payload)
	if err != nil {
		return nil, err
	}
	if h.svc == nil {
		return nil, errors.New("bio service unavailable")
	}

	bio, err := h.svc.RefreshAlbumBio(context.Background(), artist, album)
	if err != nil {
		log.Debug().Err(err).Str("artist", artist).Str("album", album).Msg("RefreshAlbumBio error; returning empty")
		return emptyBioPayload(artist, album), nil
	}
	return bioPayload(artist, album, bio), nil
}

func bioPayload(artist, album string, bio bios.Bio) map[string]interface{} {
	return map[string]interface{}{
		"artist":     artist,
		"album":      album,
		"summary":    bio.Summary,
		"source_url": bio.SourceURL,
		"kind":       bio.Kind,
	}
}

func emptyBioPayload(artist, album string) map[string]interface{} {
	return map[string]interface{}{
		"artist":     artist,
		"album":      album,
		"summary":    "",
		"source_url": "",
		"kind":       "",
	}
}

func requireArtistAlbum(p map[string]interface{}) (string, string, error) {
	if p == nil {
		return "", "", errors.New("payload missing")
	}
	artist, _ := p["artist"].(string)
	album, _ := p["album"].(string)
	if artist == "" || album == "" {
		return "", "", fmt.Errorf("artist and album required (got artist=%q album=%q)", artist, album)
	}
	return artist, album, nil
}

func firstMapArg(args []interface{}) (map[string]interface{}, bool) {
	if len(args) == 0 {
		return nil, false
	}
	m, ok := args[0].(map[string]interface{})
	return m, ok
}
