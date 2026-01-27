package socketio

import (
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library"
	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"
)

// LibraryHandlers contains Socket.IO handlers for library operations.
type LibraryHandlers struct {
	libraryService *library.Service
}

// NewLibraryHandlers creates a new LibraryHandlers instance.
func NewLibraryHandlers(libraryService *library.Service) *LibraryHandlers {
	return &LibraryHandlers{
		libraryService: libraryService,
	}
}

// RegisterHandlers registers all library-related Socket.IO handlers.
func (h *LibraryHandlers) RegisterHandlers(client *socket.Socket) {
	// Albums listing
	client.On("library:albums:list", func(args ...interface{}) {
		h.handleGetAlbums(client, args...)
	})

	// Artists listing
	client.On("library:artists:list", func(args ...interface{}) {
		h.handleGetArtists(client, args...)
	})

	// Artist albums
	client.On("library:artist:albums", func(args ...interface{}) {
		h.handleGetArtistAlbums(client, args...)
	})

	// Album tracks
	client.On("library:album:tracks", func(args ...interface{}) {
		h.handleGetAlbumTracks(client, args...)
	})

	// Radio stations
	client.On("library:radio:list", func(args ...interface{}) {
		h.handleGetRadioStations(client, args...)
	})

	// Radio play
	client.On("library:radio:play", func(args ...interface{}) {
		h.handlePlayRadio(client, args...)
	})
}

// handleGetAlbums handles the library:albums:list event.
func (h *LibraryHandlers) handleGetAlbums(client *socket.Socket, args ...interface{}) {
	log.Debug().Msg("Received library:albums:list")

	req := library.GetAlbumsRequest{
		Scope: library.ScopeAll,
		Sort:  library.SortAlphabetical,
		Page:  1,
		Limit: library.DefaultLimit,
	}

	// Parse request payload
	if len(args) > 0 {
		if payload, ok := args[0].(map[string]interface{}); ok {
			if scope, ok := payload["scope"].(string); ok {
				req.Scope = library.Scope(scope)
			}
			if sort, ok := payload["sort"].(string); ok {
				req.Sort = library.SortOrder(sort)
			}
			if query, ok := payload["query"].(string); ok {
				req.Query = query
			}
			if page, ok := payload["page"].(float64); ok {
				req.Page = int(page)
			}
			if limit, ok := payload["limit"].(float64); ok {
				req.Limit = int(limit)
			}
		}
	}

	resp := h.libraryService.GetAlbums(req)

	log.Debug().
		Str("scope", string(req.Scope)).
		Int("albumCount", len(resp.Albums)).
		Int("total", resp.Pagination.Total).
		Msg("Sending pushLibraryAlbums")

	client.Emit("pushLibraryAlbums", resp)
}

// handleGetArtists handles the library:artists:list event.
func (h *LibraryHandlers) handleGetArtists(client *socket.Socket, args ...interface{}) {
	log.Debug().Msg("Received library:artists:list")

	req := library.GetArtistsRequest{
		Page:  1,
		Limit: library.DefaultLimit,
	}

	// Parse request payload
	if len(args) > 0 {
		if payload, ok := args[0].(map[string]interface{}); ok {
			if query, ok := payload["query"].(string); ok {
				req.Query = query
			}
			if page, ok := payload["page"].(float64); ok {
				req.Page = int(page)
			}
			if limit, ok := payload["limit"].(float64); ok {
				req.Limit = int(limit)
			}
		}
	}

	resp := h.libraryService.GetArtists(req)

	log.Debug().
		Int("artistCount", len(resp.Artists)).
		Int("total", resp.Pagination.Total).
		Msg("Sending pushLibraryArtists")

	client.Emit("pushLibraryArtists", resp)
}

// handleGetArtistAlbums handles the library:artist:albums event.
func (h *LibraryHandlers) handleGetArtistAlbums(client *socket.Socket, args ...interface{}) {
	log.Debug().Msg("Received library:artist:albums")

	req := library.GetArtistAlbumsRequest{
		Sort:  library.SortAlphabetical,
		Page:  1,
		Limit: library.DefaultLimit,
	}

	// Parse request payload
	if len(args) > 0 {
		if payload, ok := args[0].(map[string]interface{}); ok {
			if artist, ok := payload["artist"].(string); ok {
				req.Artist = artist
			}
			if sort, ok := payload["sort"].(string); ok {
				req.Sort = library.SortOrder(sort)
			}
			if page, ok := payload["page"].(float64); ok {
				req.Page = int(page)
			}
			if limit, ok := payload["limit"].(float64); ok {
				req.Limit = int(limit)
			}
		}
	}

	resp := h.libraryService.GetArtistAlbums(req)

	log.Debug().
		Str("artist", req.Artist).
		Int("albumCount", len(resp.Albums)).
		Msg("Sending pushLibraryArtistAlbums")

	client.Emit("pushLibraryArtistAlbums", resp)
}

// handleGetAlbumTracks handles the library:album:tracks event.
func (h *LibraryHandlers) handleGetAlbumTracks(client *socket.Socket, args ...interface{}) {
	log.Debug().Msg("Received library:album:tracks")

	req := library.GetAlbumTracksRequest{}

	// Parse request payload
	if len(args) > 0 {
		if payload, ok := args[0].(map[string]interface{}); ok {
			if album, ok := payload["album"].(string); ok {
				req.Album = album
			}
			if albumArtist, ok := payload["albumArtist"].(string); ok {
				req.AlbumArtist = albumArtist
			}
		}
	}

	resp := h.libraryService.GetAlbumTracks(req)

	log.Debug().
		Str("album", req.Album).
		Int("trackCount", len(resp.Tracks)).
		Msg("Sending pushLibraryAlbumTracks")

	client.Emit("pushLibraryAlbumTracks", resp)
}

// handleGetRadioStations handles the library:radio:list event.
func (h *LibraryHandlers) handleGetRadioStations(client *socket.Socket, args ...interface{}) {
	log.Debug().Msg("Received library:radio:list")

	req := library.GetRadioRequest{
		Page:  1,
		Limit: library.DefaultLimit,
	}

	// Parse request payload
	if len(args) > 0 {
		if payload, ok := args[0].(map[string]interface{}); ok {
			if query, ok := payload["query"].(string); ok {
				req.Query = query
			}
			if page, ok := payload["page"].(float64); ok {
				req.Page = int(page)
			}
			if limit, ok := payload["limit"].(float64); ok {
				req.Limit = int(limit)
			}
		}
	}

	resp := h.libraryService.GetRadioStations(req)

	log.Debug().
		Int("stationCount", len(resp.Stations)).
		Int("total", resp.Pagination.Total).
		Msg("Sending pushLibraryRadio")

	client.Emit("pushLibraryRadio", resp)
}

// handlePlayRadio handles the library:radio:play event.
// Note: This delegates to the player service which is not injected here.
// The actual implementation should use the player service from the main server.
func (h *LibraryHandlers) handlePlayRadio(client *socket.Socket, args ...interface{}) {
	log.Debug().Msg("Received library:radio:play")

	// Parse request payload
	var uri string
	if len(args) > 0 {
		if payload, ok := args[0].(map[string]interface{}); ok {
			if u, ok := payload["uri"].(string); ok {
				uri = u
			}
		}
	}

	if uri == "" {
		log.Warn().Msg("library:radio:play received without URI")
		return
	}

	log.Info().Str("uri", uri).Msg("Radio play requested - delegating to player")

	// Note: The actual playback should be handled by emitting a replaceAndPlay event
	// or calling the player service directly. For now, we emit an internal event.
	// The main server should handle this by calling player.ReplaceAndPlay(uri)
	client.Emit("_internal:radio:play", map[string]string{"uri": uri})
}
