// Package socketio provides the Socket.io server for client communication.
package socketio

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"
	"github.com/zishang520/socket.io/v3/pkg/types"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
	mpdclient "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
)

// Server handles Socket.io connections and events.
type Server struct {
	io            *socket.Server
	playerService *player.Service
	mpdClient     *mpdclient.Client
	mu            sync.RWMutex
	clients       map[string]*socket.Socket
}

// NewServer creates a new Socket.io server.
func NewServer(playerService *player.Service, mpdClient *mpdclient.Client) (*Server, error) {
	// Configure Socket.io server options
	opts := socket.DefaultServerOptions()
	opts.SetPingTimeout(20 * time.Second)
	opts.SetPingInterval(25 * time.Second)
	opts.SetCors(&types.Cors{
		Origin:      "*",
		Credentials: true,
	})

	server := socket.NewServer(nil, opts)

	s := &Server{
		io:            server,
		playerService: playerService,
		mpdClient:     mpdClient,
		clients:       make(map[string]*socket.Socket),
	}

	s.setupHandlers()

	return s, nil
}

// setupHandlers registers all Socket.io event handlers.
func (s *Server) setupHandlers() {
	s.io.On("connection", func(clients ...any) {
		client := clients[0].(*socket.Socket)
		clientID := string(client.Id())

		log.Info().Str("id", clientID).Msg("Client connected")

		s.mu.Lock()
		s.clients[clientID] = client
		s.mu.Unlock()

		// Send initial state after small delay
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.pushState(client)
			s.pushQueue(client)
		}()

		// Handle disconnect
		client.On("disconnect", func(args ...any) {
			reason := ""
			if len(args) > 0 {
				if r, ok := args[0].(string); ok {
					reason = r
				}
			}
			log.Info().Str("id", clientID).Str("reason", reason).Msg("Client disconnected")

			s.mu.Lock()
			delete(s.clients, clientID)
			s.mu.Unlock()
		})

		// Player control events
		client.On("getState", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("getState")
			s.pushState(client)
		})

		client.On("play", func(args ...any) {
			log.Debug().Str("id", clientID).Interface("data", args).Msg("play")

			pos := -1 // Default: resume
			if len(args) > 0 {
				if m, ok := args[0].(map[string]interface{}); ok {
					if v, ok := m["value"].(float64); ok {
						pos = int(v)
					}
				}
			}

			if err := s.playerService.Play(pos); err != nil {
				log.Error().Err(err).Msg("Play failed")
			}
		})

		client.On("pause", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("pause")
			if err := s.playerService.Pause(); err != nil {
				log.Error().Err(err).Msg("Pause failed")
			}
		})

		client.On("stop", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("stop")
			if err := s.playerService.Stop(); err != nil {
				log.Error().Err(err).Msg("Stop failed")
			}
		})

		client.On("next", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("next")
			if err := s.playerService.Next(); err != nil {
				log.Error().Err(err).Msg("Next failed")
			}
		})

		client.On("prev", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("prev")
			if err := s.playerService.Previous(); err != nil {
				log.Error().Err(err).Msg("Previous failed")
			}
		})

		client.On("seek", func(args ...any) {
			if len(args) > 0 {
				if pos, ok := args[0].(float64); ok {
					log.Debug().Str("id", clientID).Float64("pos", pos).Msg("seek")
					if err := s.playerService.Seek(int(pos)); err != nil {
						log.Error().Err(err).Msg("Seek failed")
					}
				}
			}
		})

		client.On("volume", func(args ...any) {
			if len(args) > 0 {
				if vol, ok := args[0].(float64); ok {
					log.Debug().Str("id", clientID).Float64("vol", vol).Msg("volume")
					if err := s.playerService.SetVolume(int(vol)); err != nil {
						log.Error().Err(err).Msg("SetVolume failed")
					}
				}
			}
		})

		client.On("mute", func(args ...any) {
			log.Debug().Str("id", clientID).Interface("data", args).Msg("mute")
			// TODO: Implement mute tracking (MPD doesn't have native mute)
		})

		client.On("setRandom", func(args ...any) {
			log.Debug().Str("id", clientID).Interface("data", args).Msg("setRandom")
			if len(args) > 0 {
				if m, ok := args[0].(map[string]interface{}); ok {
					if v, ok := m["value"].(bool); ok {
						if err := s.playerService.SetRandom(v); err != nil {
							log.Error().Err(err).Msg("SetRandom failed")
						}
					}
				}
			}
		})

		client.On("setRepeat", func(args ...any) {
			log.Debug().Str("id", clientID).Interface("data", args).Msg("setRepeat")
			if len(args) > 0 {
				if m, ok := args[0].(map[string]interface{}); ok {
					repeat, _ := m["value"].(bool)
					single, _ := m["repeatSingle"].(bool)
					if err := s.playerService.SetRepeat(repeat, single); err != nil {
						log.Error().Err(err).Msg("SetRepeat failed")
					}
				}
			}
		})

		// Queue events
		client.On("getQueue", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("getQueue")
			s.pushQueue(client)
		})

		client.On("clearQueue", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("clearQueue")
			if err := s.playerService.ClearQueue(); err != nil {
				log.Error().Err(err).Msg("ClearQueue failed")
			}
		})

		client.On("addToQueue", func(args ...any) {
			log.Debug().Str("id", clientID).Interface("data", args).Msg("addToQueue")
			if len(args) > 0 {
				if m, ok := args[0].(map[string]interface{}); ok {
					if uri, ok := m["uri"].(string); ok {
						if err := s.playerService.AddToQueue(uri); err != nil {
							log.Error().Err(err).Msg("AddToQueue failed")
						}
					}
				}
			}
		})

		// Browse events
		client.On("getBrowseSources", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("getBrowseSources")
			sources := []map[string]interface{}{
				{
					"name":        "Music Library",
					"uri":         "music-library",
					"plugin_type": "music_service",
					"plugin_name": "mpd",
					"albumart":    "/albumart?sourceicon=music_service/mpd/musiclibraryicon.svg",
				},
			}
			client.Emit("pushBrowseSources", sources)
		})

		client.On("browseLibrary", func(args ...any) {
			log.Debug().Str("id", clientID).Interface("data", args).Msg("browseLibrary")
			// TODO: Implement library browsing
			client.Emit("pushBrowseLibrary", map[string]interface{}{
				"navigation": map[string]interface{}{
					"lists": []interface{}{},
				},
			})
		})
	})
}

// pushState sends current state to a client.
func (s *Server) pushState(client *socket.Socket) {
	state, err := s.playerService.GetState()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get state")
		return
	}
	client.Emit("pushState", state)
}

// pushQueue sends current queue to a client.
func (s *Server) pushQueue(client *socket.Socket) {
	queue, err := s.playerService.GetQueue()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get queue")
		return
	}
	client.Emit("pushQueue", queue)
}

// BroadcastState sends state to all connected clients.
func (s *Server) BroadcastState() {
	state, err := s.playerService.GetState()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get state for broadcast")
		return
	}

	s.io.Emit("pushState", state)

	if log.Debug().Enabled() {
		data, _ := json.Marshal(state)
		s.mu.RLock()
		clientCount := len(s.clients)
		s.mu.RUnlock()
		log.Debug().RawJSON("state", data).Int("clients", clientCount).Msg("Broadcast state")
	}
}

// BroadcastQueue sends queue to all connected clients.
func (s *Server) BroadcastQueue() {
	queue, err := s.playerService.GetQueue()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get queue for broadcast")
		return
	}

	s.io.Emit("pushQueue", queue)
}

// StartMPDWatcher starts watching MPD for changes and broadcasts updates.
func (s *Server) StartMPDWatcher(ctx context.Context) error {
	subsystems := []string{"player", "mixer", "playlist", "options"}
	events, err := s.mpdClient.Watch(subsystems...)
	if err != nil {
		return err
	}

	go func() {
		log.Info().Strs("subsystems", subsystems).Msg("MPD watcher started")
		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("MPD watcher stopped")
				return
			case subsystem, ok := <-events:
				if !ok {
					log.Warn().Msg("MPD watcher channel closed")
					return
				}

				log.Debug().Str("subsystem", subsystem).Msg("MPD subsystem changed")

				switch subsystem {
				case "player", "mixer", "options":
					s.BroadcastState()
				case "playlist":
					s.BroadcastQueue()
					s.BroadcastState() // Also update state as position might change
				}
			}
		}
	}()

	return nil
}

// ServeHTTP implements http.Handler for the Socket.io server.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.io.ServeHandler(nil).ServeHTTP(w, r)
}

// Close closes the Socket.io server.
func (s *Server) Close() error {
	s.io.Close(nil)
	return nil
}
