// Package socketio provides the Socket.io server for client communication.
package socketio

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	socketio "github.com/googollee/go-socket.io"
	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/transport"
	"github.com/googollee/go-socket.io/engineio/transport/polling"
	"github.com/googollee/go-socket.io/engineio/transport/websocket"
	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
	mpdclient "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
)

// Server handles Socket.io connections and events.
type Server struct {
	io            *socketio.Server
	playerService *player.Service
	mpdClient     *mpdclient.Client
	mu            sync.RWMutex
	clients       map[string]socketio.Conn
}

// NewServer creates a new Socket.io server.
func NewServer(playerService *player.Service, mpdClient *mpdclient.Client) (*Server, error) {
	// Configure Socket.io with polling and websocket transports
	server := socketio.NewServer(&engineio.Options{
		Transports: []transport.Transport{
			&polling.Transport{
				CheckOrigin: func(r *http.Request) bool { return true },
			},
			&websocket.Transport{
				CheckOrigin: func(r *http.Request) bool { return true },
			},
		},
	})

	s := &Server{
		io:            server,
		playerService: playerService,
		mpdClient:     mpdClient,
		clients:       make(map[string]socketio.Conn),
	}

	s.setupHandlers()

	// Start the socket.io server goroutine to process connections
	go s.io.Serve()

	return s, nil
}

// setupHandlers registers all Socket.io event handlers.
func (s *Server) setupHandlers() {
	s.io.OnConnect("/", func(conn socketio.Conn) error {
		log.Info().Str("id", conn.ID()).Str("remote", conn.RemoteAddr().String()).Msg("Client connected")

		s.mu.Lock()
		s.clients[conn.ID()] = conn
		s.mu.Unlock()

		// Send initial state
		go func() {
			time.Sleep(100 * time.Millisecond) // Small delay to ensure connection is ready
			s.pushState(conn)
			s.pushQueue(conn)
		}()

		return nil
	})

	s.io.OnDisconnect("/", func(conn socketio.Conn, reason string) {
		log.Info().Str("id", conn.ID()).Str("reason", reason).Msg("Client disconnected")

		s.mu.Lock()
		delete(s.clients, conn.ID())
		s.mu.Unlock()
	})

	s.io.OnError("/", func(conn socketio.Conn, err error) {
		if conn != nil {
			log.Error().Err(err).Str("id", conn.ID()).Msg("Socket.io error")
		} else {
			log.Error().Err(err).Msg("Socket.io error (no connection)")
		}
	})

	// Player control events
	s.io.OnEvent("/", "getState", func(conn socketio.Conn) {
		log.Debug().Str("id", conn.ID()).Msg("getState")
		s.pushState(conn)
	})

	s.io.OnEvent("/", "play", func(conn socketio.Conn, data interface{}) {
		log.Debug().Str("id", conn.ID()).Interface("data", data).Msg("play")

		pos := -1 // Default: resume
		if m, ok := data.(map[string]interface{}); ok {
			if v, ok := m["value"].(float64); ok {
				pos = int(v)
			}
		}

		if err := s.playerService.Play(pos); err != nil {
			log.Error().Err(err).Msg("Play failed")
		}
	})

	s.io.OnEvent("/", "pause", func(conn socketio.Conn) {
		log.Debug().Str("id", conn.ID()).Msg("pause")
		if err := s.playerService.Pause(); err != nil {
			log.Error().Err(err).Msg("Pause failed")
		}
	})

	s.io.OnEvent("/", "stop", func(conn socketio.Conn) {
		log.Debug().Str("id", conn.ID()).Msg("stop")
		if err := s.playerService.Stop(); err != nil {
			log.Error().Err(err).Msg("Stop failed")
		}
	})

	s.io.OnEvent("/", "next", func(conn socketio.Conn) {
		log.Debug().Str("id", conn.ID()).Msg("next")
		if err := s.playerService.Next(); err != nil {
			log.Error().Err(err).Msg("Next failed")
		}
	})

	s.io.OnEvent("/", "prev", func(conn socketio.Conn) {
		log.Debug().Str("id", conn.ID()).Msg("prev")
		if err := s.playerService.Previous(); err != nil {
			log.Error().Err(err).Msg("Previous failed")
		}
	})

	s.io.OnEvent("/", "seek", func(conn socketio.Conn, pos float64) {
		log.Debug().Str("id", conn.ID()).Float64("pos", pos).Msg("seek")
		if err := s.playerService.Seek(int(pos)); err != nil {
			log.Error().Err(err).Msg("Seek failed")
		}
	})

	s.io.OnEvent("/", "volume", func(conn socketio.Conn, vol float64) {
		log.Debug().Str("id", conn.ID()).Float64("vol", vol).Msg("volume")
		if err := s.playerService.SetVolume(int(vol)); err != nil {
			log.Error().Err(err).Msg("SetVolume failed")
		}
	})

	s.io.OnEvent("/", "mute", func(conn socketio.Conn, data string) {
		log.Debug().Str("id", conn.ID()).Str("data", data).Msg("mute")
		// TODO: Implement mute tracking (MPD doesn't have native mute)
	})

	s.io.OnEvent("/", "setRandom", func(conn socketio.Conn, data interface{}) {
		log.Debug().Str("id", conn.ID()).Interface("data", data).Msg("setRandom")
		if m, ok := data.(map[string]interface{}); ok {
			if v, ok := m["value"].(bool); ok {
				if err := s.playerService.SetRandom(v); err != nil {
					log.Error().Err(err).Msg("SetRandom failed")
				}
			}
		}
	})

	s.io.OnEvent("/", "setRepeat", func(conn socketio.Conn, data interface{}) {
		log.Debug().Str("id", conn.ID()).Interface("data", data).Msg("setRepeat")
		if m, ok := data.(map[string]interface{}); ok {
			repeat, _ := m["value"].(bool)
			single, _ := m["repeatSingle"].(bool)
			if err := s.playerService.SetRepeat(repeat, single); err != nil {
				log.Error().Err(err).Msg("SetRepeat failed")
			}
		}
	})

	// Queue events
	s.io.OnEvent("/", "getQueue", func(conn socketio.Conn) {
		log.Debug().Str("id", conn.ID()).Msg("getQueue")
		s.pushQueue(conn)
	})

	s.io.OnEvent("/", "clearQueue", func(conn socketio.Conn) {
		log.Debug().Str("id", conn.ID()).Msg("clearQueue")
		if err := s.playerService.ClearQueue(); err != nil {
			log.Error().Err(err).Msg("ClearQueue failed")
		}
	})

	s.io.OnEvent("/", "addToQueue", func(conn socketio.Conn, data interface{}) {
		log.Debug().Str("id", conn.ID()).Interface("data", data).Msg("addToQueue")
		if m, ok := data.(map[string]interface{}); ok {
			if uri, ok := m["uri"].(string); ok {
				if err := s.playerService.AddToQueue(uri); err != nil {
					log.Error().Err(err).Msg("AddToQueue failed")
				}
			}
		}
	})

	// Browse events (placeholder - to be implemented)
	s.io.OnEvent("/", "getBrowseSources", func(conn socketio.Conn) {
		log.Debug().Str("id", conn.ID()).Msg("getBrowseSources")
		// Return basic music library source
		sources := []map[string]interface{}{
			{
				"name":        "Music Library",
				"uri":         "music-library",
				"plugin_type": "music_service",
				"plugin_name": "mpd",
				"albumart":    "/albumart?sourceicon=music_service/mpd/musiclibraryicon.svg",
			},
		}
		conn.Emit("pushBrowseSources", sources)
	})

	s.io.OnEvent("/", "browseLibrary", func(conn socketio.Conn, data interface{}) {
		log.Debug().Str("id", conn.ID()).Interface("data", data).Msg("browseLibrary")
		// TODO: Implement library browsing
		conn.Emit("pushBrowseLibrary", map[string]interface{}{
			"navigation": map[string]interface{}{
				"lists": []interface{}{},
			},
		})
	})
}

// pushState sends current state to a client.
func (s *Server) pushState(conn socketio.Conn) {
	state, err := s.playerService.GetState()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get state")
		return
	}
	conn.Emit("pushState", state)
}

// pushQueue sends current queue to a client.
func (s *Server) pushQueue(conn socketio.Conn) {
	queue, err := s.playerService.GetQueue()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get queue")
		return
	}
	conn.Emit("pushQueue", queue)
}

// BroadcastState sends state to all connected clients.
func (s *Server) BroadcastState() {
	state, err := s.playerService.GetState()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get state for broadcast")
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, conn := range s.clients {
		conn.Emit("pushState", state)
	}

	if log.Debug().Enabled() {
		data, _ := json.Marshal(state)
		log.Debug().RawJSON("state", data).Int("clients", len(s.clients)).Msg("Broadcast state")
	}
}

// BroadcastQueue sends queue to all connected clients.
func (s *Server) BroadcastQueue() {
	queue, err := s.playerService.GetQueue()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get queue for broadcast")
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, conn := range s.clients {
		conn.Emit("pushQueue", queue)
	}
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
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.io.ServeHTTP(w, r)
}

// Close closes the Socket.io server.
func (s *Server) Close() error {
	return s.io.Close()
}
