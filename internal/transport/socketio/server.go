// Package socketio provides the Socket.io server for client communication.
package socketio

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"
	"github.com/zishang520/socket.io/v3/pkg/types"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/audio"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/audirvana"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/localmusic"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/sources"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/streaming/qobuz"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
	mpdclient "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/version"
)

// Server handles Socket.io connections and events.
type Server struct {
	io                *socket.Server
	playerService     *player.Service
	mpdClient         *mpdclient.Client
	audioController   *audio.Controller
	sourcesService    *sources.Service
	qobuzService      *qobuz.Service
	localMusicService *localmusic.Service
	libraryService    *library.Service
	cachedService     *library.CachedService
	libraryHandlers   *LibraryHandlers
	cacheHandlers     *CacheHandlers
	cacheDB           *cache.DB
	audirvanaService  *audirvana.Service
	mu                sync.RWMutex
	clients           map[string]*socket.Socket
	lastNetwork       NetworkStatus
}

// NewServer creates a new Socket.io server.
// bitPerfect indicates whether the system is configured for bit-perfect audio output.
func NewServer(playerService *player.Service, mpdClient *mpdclient.Client, sourcesService *sources.Service, localMusicSvc *localmusic.Service, bitPerfect bool) (*Server, error) {
	// Configure Socket.io server options
	opts := socket.DefaultServerOptions()
	opts.SetPingTimeout(60 * time.Second)  // Increased to prevent premature disconnects
	opts.SetPingInterval(30 * time.Second) // Ping every 30s, allow 60s for response
	opts.SetCors(&types.Cors{
		Origin:      "*",
		Credentials: true,
	})
	// Enable Engine.IO v3 protocol compatibility for Volumio Connect apps
	// which use Socket.IO v2.x clients (Engine.IO v3 protocol)
	opts.SetAllowEIO3(true)

	server := socket.NewServer(nil, opts)

	// Initialize Qobuz service
	qobuzConfigPath := os.ExpandEnv("$HOME/.stellar/qobuz.json")
	qobuzSvc, err := qobuz.NewService(qobuzConfigPath)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize Qobuz service, streaming features disabled")
	}

	// Initialize cache database
	cacheDB := cache.NewDB(os.ExpandEnv("$HOME/stellar-backend/data/library.db"))
	if err := cacheDB.Open(); err != nil {
		log.Warn().Err(err).Msg("Failed to open cache database, caching disabled")
		cacheDB = nil
	} else {
		log.Info().Msg("Library cache database initialized")
	}

	// Initialize library service with adapters (only if localMusicSvc is provided)
	var librarySvc *library.Service
	var cachedSvc *library.CachedService
	var libraryHandlers *LibraryHandlers
	if localMusicSvc != nil {
		mpdAdapter := NewLibraryMPDAdapter(mpdClient)
		classifierAdapter := NewLibraryClassifierAdapter(localMusicSvc.GetClassifier())
		librarySvc = library.NewService(mpdAdapter, classifierAdapter)
		cachedSvc = library.NewCachedService(mpdAdapter, classifierAdapter, cacheDB)
		// Use CachedService for library handlers to enable caching and artwork resolution
		libraryHandlers = NewLibraryHandlers(cachedSvc)
	}

	s := &Server{
		io:                server,
		playerService:     playerService,
		mpdClient:         mpdClient,
		audioController:   audio.NewController(bitPerfect),
		sourcesService:    sourcesService,
		qobuzService:      qobuzSvc,
		localMusicService: localMusicSvc,
		libraryService:    librarySvc,
		cachedService:     cachedSvc,
		libraryHandlers:   libraryHandlers,
		cacheDB:           cacheDB,
		audirvanaService:  audirvana.NewService(),
		clients:           make(map[string]*socket.Socket),
	}

	// Initialize cache handlers if cached service is available
	if cachedSvc != nil {
		s.cacheHandlers = NewCacheHandlers(cachedSvc, s)
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
			// Also send network, LCD, system info, and audio status
			client.Emit("pushNetworkStatus", GetNetworkStatus())
			client.Emit("pushSystemInfo", GetSystemInfo())
			client.Emit("pushLcdStatus", GetLCDStatus())
			client.Emit("pushAudioStatus", s.audioController.GetStatus())
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

		// Register library handlers (MPD-driven browsing)
		if s.libraryHandlers != nil {
			s.libraryHandlers.RegisterHandlers(client)
		}

		// Register cache handlers
		if s.cacheHandlers != nil {
			s.cacheHandlers.RegisterHandlers(client)
		}

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
			sources := s.getBrowseSources()
			client.Emit("pushBrowseSources", sources)
		})

		client.On("browseLibrary", func(args ...any) {
			log.Debug().Str("id", clientID).Interface("data", args).Msg("browseLibrary")

			uri := ""
			if len(args) > 0 {
				if m, ok := args[0].(map[string]interface{}); ok {
					if u, ok := m["uri"].(string); ok {
						uri = u
					}
				}
			}

			// Handle Qobuz URIs
			if strings.HasPrefix(uri, "qobuz://") {
				if s.qobuzService == nil {
					client.Emit("pushBrowseLibrary", map[string]interface{}{
						"navigation": map[string]interface{}{
							"lists": []interface{}{},
						},
						"error": "Qobuz service not available",
					})
					return
				}

				if !s.qobuzService.IsLoggedIn() {
					client.Emit("pushBrowseLibrary", map[string]interface{}{
						"navigation": map[string]interface{}{
							"lists": []interface{}{},
						},
						"error": "not logged in to Qobuz",
					})
					return
				}

				result, err := s.qobuzService.HandleBrowseURI(uri)
				if err != nil {
					log.Error().Err(err).Str("uri", uri).Msg("Qobuz browse failed")
					client.Emit("pushBrowseLibrary", map[string]interface{}{
						"navigation": map[string]interface{}{
							"lists": []interface{}{},
						},
						"error": err.Error(),
					})
					return
				}

				client.Emit("pushBrowseLibrary", result)
				return
			}

			// Handle local library URIs
			result, err := s.playerService.BrowseLibrary(uri)
			if err != nil {
				log.Error().Err(err).Str("uri", uri).Msg("BrowseLibrary failed")
				client.Emit("pushBrowseLibrary", map[string]interface{}{
					"navigation": map[string]interface{}{
						"lists": []interface{}{},
					},
				})
				return
			}
			client.Emit("pushBrowseLibrary", result)
		})

		client.On("replaceAndPlay", func(args ...any) {
			log.Debug().Str("id", clientID).Interface("data", args).Msg("replaceAndPlay")
			if len(args) > 0 {
				if m, ok := args[0].(map[string]interface{}); ok {
					if uri, ok := m["uri"].(string); ok {
						if err := s.playerService.ReplaceAndPlay(uri); err != nil {
							log.Error().Err(err).Msg("ReplaceAndPlay failed")
							return
						}

						// Record play history for local sources
						if s.localMusicService != nil && s.localMusicService.IsLocalSource(uri) {
							title := getString(m, "title")
							artist := getString(m, "artist")
							album := getString(m, "album")
							albumArt := getString(m, "albumart")
							if albumArt == "" {
								albumArt = getString(m, "albumArt")
							}

							// Determine play origin - default to manual track
							origin := localmusic.PlayOriginManualTrack
							if originStr := getString(m, "origin"); originStr != "" {
								switch originStr {
								case "album_context":
									origin = localmusic.PlayOriginAlbumContext
								case "queue":
									origin = localmusic.PlayOriginQueue
								}
							}

							s.localMusicService.RecordTrackPlay(uri, title, artist, album, albumArt, origin)
						}
					}
				}
			}
		})

		// Network status events
		client.On("getNetworkStatus", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("getNetworkStatus")
			status := GetNetworkStatus()
			client.Emit("pushNetworkStatus", status)
		})

		// LCD control events
		client.On("getLcdStatus", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("getLcdStatus")
			status := GetLCDStatus()
			client.Emit("pushLcdStatus", status)
		})

		client.On("lcdStandby", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("lcdStandby")
			if err := SetLCDPower(false); err != nil {
				log.Error().Err(err).Msg("lcdStandby failed")
				return
			}
			s.BroadcastLCDStatus()
		})

		client.On("lcdWake", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("lcdWake")
			if err := SetLCDPower(true); err != nil {
				log.Error().Err(err).Msg("lcdWake failed")
				return
			}
			s.BroadcastLCDStatus()
		})

		// Audio status events
		client.On("getAudioStatus", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("getAudioStatus")
			status := s.audioController.GetStatus()
			client.Emit("pushAudioStatus", status)
		})

		// Version info event
		client.On("getVersion", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("getVersion")
			client.Emit("pushVersion", version.GetInfo())
		})

		// System info event
		client.On("getSystemInfo", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("getSystemInfo")
			client.Emit("pushSystemInfo", GetSystemInfo())
		})

		// Bit-perfect configuration check event
		client.On("getBitPerfect", func(args ...any) {
			log.Info().Str("id", clientID).Msg("getBitPerfect requested")
			result := GetBitPerfectStatus()
			log.Info().Str("status", result.Status).Int("issues", len(result.Issues)).Int("config", len(result.Config)).Msg("pushBitPerfect")
			client.Emit("pushBitPerfect", result)
		})

		// Playback options event (audio devices)
		client.On("getPlaybackOptions", func(args ...any) {
			log.Info().Str("id", clientID).Msg("getPlaybackOptions requested")
			options := GetPlaybackOptions()
			log.Info().Int("sections", len(options.Options)).Int("cards", len(options.SystemCards)).Msg("pushPlaybackOptions")
			client.Emit("pushPlaybackOptions", options)
		})

		// Set playback settings (change audio output)
		client.On("setPlaybackSettings", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("setPlaybackSettings requested")

			response := map[string]interface{}{"success": false}

			if len(args) > 0 {
				if m, ok := args[0].(map[string]interface{}); ok {
					if device, ok := m["output_device"].(string); ok {
						if err := SetPlaybackSettings(device); err != nil {
							log.Error().Err(err).Str("device", device).Msg("Failed to set audio output")
							response["error"] = err.Error()
						} else {
							response["success"] = true
							// Broadcast updated playback options to all clients
							options := GetPlaybackOptions()
							s.io.Emit("pushPlaybackOptions", options)
						}
					}
				}
			}

			// Handle callback if provided
			if len(args) > 1 {
				if callback, ok := args[len(args)-1].(func([]any, error)); ok {
					callback([]any{response}, nil)
				}
			}
			client.Emit("pushPlaybackSettings", response)
		})

		// DSD mode events
		client.On("getDsdMode", func(args ...any) {
			log.Info().Str("id", clientID).Msg("getDsdMode requested")
			mode := GetDsdMode()
			log.Info().Str("mode", mode.Mode).Msg("pushDsdMode")
			client.Emit("pushDsdMode", mode)
		})

		client.On("setDsdMode", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("setDsdMode requested")
			if len(args) > 0 {
				if m, ok := args[0].(map[string]interface{}); ok {
					if mode, ok := m["mode"].(string); ok {
						result := SetDsdMode(mode)
						log.Info().Bool("success", result.Success).Str("mode", result.Mode).Msg("pushDsdMode")
						client.Emit("pushDsdMode", result)
						// Broadcast to all clients
						s.io.Emit("pushDsdMode", result)
					}
				}
			}
		})

		// Mixer mode events
		client.On("getMixerMode", func(args ...any) {
			log.Info().Str("id", clientID).Msg("getMixerMode requested")
			mode := GetMixerMode()
			log.Info().Bool("enabled", mode.Enabled).Msg("pushMixerMode")
			client.Emit("pushMixerMode", mode)
		})

		client.On("setMixerMode", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("setMixerMode requested")
			if len(args) > 0 {
				if m, ok := args[0].(map[string]interface{}); ok {
					if enabled, ok := m["enabled"].(bool); ok {
						result := SetMixerMode(enabled)
						log.Info().Bool("success", result.Success).Bool("enabled", result.Enabled).Msg("pushMixerMode")
						client.Emit("pushMixerMode", result)
						// Broadcast to all clients
						s.io.Emit("pushMixerMode", result)
					}
				}
			}
		})

		// Apply all bit-perfect settings
		client.On("applyBitPerfect", func(args ...any) {
			log.Info().Str("id", clientID).Msg("applyBitPerfect requested")
			result := ApplyBitPerfect()
			log.Info().Bool("success", result.Success).Strs("applied", result.Applied).Msg("pushApplyBitPerfect")
			client.Emit("pushApplyBitPerfect", result)
			// Refresh bit-perfect status for all clients
			s.io.Emit("pushBitPerfect", GetBitPerfectStatus())
			// Refresh mixer mode for all clients
			s.io.Emit("pushMixerMode", GetMixerMode())
		})

		// ============================================================
		// Music Sources (NAS) Events
		// ============================================================

		// List all configured NAS shares
		client.On("getListNasShares", func(args ...any) {
			log.Info().Str("id", clientID).Msg("getListNasShares requested")
			if s.sourcesService == nil {
				client.Emit("pushListNasShares", []sources.NasShare{})
				return
			}
			shares, err := s.sourcesService.ListNasShares()
			if err != nil {
				log.Error().Err(err).Msg("Failed to list NAS shares")
				client.Emit("pushListNasShares", []sources.NasShare{})
				return
			}
			log.Info().Int("count", len(shares)).Msg("pushListNasShares")
			client.Emit("pushListNasShares", shares)
		})

		// Add a new NAS share
		client.On("addNasShare", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("addNasShare requested")
			if s.sourcesService == nil {
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   "sources service not available",
				})
				return
			}

			if len(args) == 0 {
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   "missing share data",
				})
				return
			}

			// Parse the request
			data, ok := args[0].(map[string]interface{})
			if !ok {
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   "invalid share data format",
				})
				return
			}

			req := sources.AddNasShareRequest{
				Name:     getString(data, "name"),
				IP:       getString(data, "ip"),
				Path:     getString(data, "path"),
				FSType:   getString(data, "fstype"),
				Username: getString(data, "username"),
				Password: getString(data, "password"),
				Options:  getString(data, "options"),
			}

			result, err := s.sourcesService.AddNasShare(req)
			if err != nil {
				log.Error().Err(err).Msg("Failed to add NAS share")
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   err.Error(),
				})
				return
			}

			log.Info().Bool("success", result.Success).Msg("pushNasShareResult")
			client.Emit("pushNasShareResult", result)

			// Also push updated list to all clients
			if result.Success {
				shares, _ := s.sourcesService.ListNasShares()
				s.io.Emit("pushListNasShares", shares)
				// Trigger MPD database update
				exec.Command("mpc", "update").Run()
			}
		})

		// Delete a NAS share
		client.On("deleteNasShare", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("deleteNasShare requested")
			if s.sourcesService == nil {
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   "sources service not available",
				})
				return
			}

			if len(args) == 0 {
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   "missing share ID",
				})
				return
			}

			var shareID string
			if data, ok := args[0].(map[string]interface{}); ok {
				shareID = getString(data, "id")
			} else if id, ok := args[0].(string); ok {
				shareID = id
			}

			if shareID == "" {
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   "invalid share ID",
				})
				return
			}

			result, err := s.sourcesService.DeleteNasShare(shareID)
			if err != nil {
				log.Error().Err(err).Msg("Failed to delete NAS share")
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   err.Error(),
				})
				return
			}

			log.Info().Bool("success", result.Success).Msg("pushNasShareResult")
			client.Emit("pushNasShareResult", result)

			// Also push updated list to all clients
			if result.Success {
				shares, _ := s.sourcesService.ListNasShares()
				s.io.Emit("pushListNasShares", shares)
			}
		})

		// Mount a NAS share
		client.On("mountNasShare", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("mountNasShare requested")
			if s.sourcesService == nil {
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   "sources service not available",
				})
				return
			}

			var shareID string
			if len(args) > 0 {
				if data, ok := args[0].(map[string]interface{}); ok {
					shareID = getString(data, "id")
				} else if id, ok := args[0].(string); ok {
					shareID = id
				}
			}

			if shareID == "" {
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   "missing share ID",
				})
				return
			}

			result, err := s.sourcesService.MountNasShare(shareID)
			if err != nil {
				log.Error().Err(err).Msg("Failed to mount NAS share")
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   err.Error(),
				})
				return
			}

			log.Info().Bool("success", result.Success).Msg("pushNasShareResult")
			client.Emit("pushNasShareResult", result)

			// Push updated list and trigger MPD update
			if result.Success {
				shares, _ := s.sourcesService.ListNasShares()
				s.io.Emit("pushListNasShares", shares)
				exec.Command("mpc", "update").Run()
			}
		})

		// Discover NAS devices on the network
		client.On("discoverNasDevices", func(args ...any) {
			log.Info().Str("id", clientID).Msg("discoverNasDevices requested")
			if s.sourcesService == nil {
				client.Emit("pushNasDevices", sources.DiscoverResult{
					Devices: []sources.NasDevice{},
					Error:   "sources service not available",
				})
				return
			}

			result, err := s.sourcesService.DiscoverNasDevices()
			if err != nil {
				log.Error().Err(err).Msg("Failed to discover NAS devices")
				client.Emit("pushNasDevices", sources.DiscoverResult{
					Devices: []sources.NasDevice{},
					Error:   err.Error(),
				})
				return
			}

			log.Info().Int("count", len(result.Devices)).Msg("pushNasDevices")
			client.Emit("pushNasDevices", result)
		})

		// Browse shares on a NAS device
		client.On("browseNasShares", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("browseNasShares requested")
			if s.sourcesService == nil {
				client.Emit("pushBrowseNasShares", sources.BrowseSharesResult{
					Shares: []sources.ShareInfo{},
					Error:  "sources service not available",
				})
				return
			}

			if len(args) == 0 {
				client.Emit("pushBrowseNasShares", sources.BrowseSharesResult{
					Shares: []sources.ShareInfo{},
					Error:  "missing host data",
				})
				return
			}

			data, ok := args[0].(map[string]interface{})
			if !ok {
				client.Emit("pushBrowseNasShares", sources.BrowseSharesResult{
					Shares: []sources.ShareInfo{},
					Error:  "invalid request format",
				})
				return
			}

			host := getString(data, "host")
			if host == "" {
				host = getString(data, "ip")
			}
			username := getString(data, "username")
			password := getString(data, "password")

			if host == "" {
				client.Emit("pushBrowseNasShares", sources.BrowseSharesResult{
					Shares: []sources.ShareInfo{},
					Error:  "host is required",
				})
				return
			}

			result, err := s.sourcesService.BrowseNasShares(host, username, password)
			if err != nil {
				log.Error().Err(err).Str("host", host).Msg("Failed to browse NAS shares")
				client.Emit("pushBrowseNasShares", sources.BrowseSharesResult{
					Shares: []sources.ShareInfo{},
					Error:  err.Error(),
				})
				return
			}

			log.Info().Int("count", len(result.Shares)).Str("host", host).Msg("pushBrowseNasShares")
			client.Emit("pushBrowseNasShares", result)
		})

		// Unmount a NAS share
		client.On("unmountNasShare", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("unmountNasShare requested")
			if s.sourcesService == nil {
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   "sources service not available",
				})
				return
			}

			var shareID string
			if len(args) > 0 {
				if data, ok := args[0].(map[string]interface{}); ok {
					shareID = getString(data, "id")
				} else if id, ok := args[0].(string); ok {
					shareID = id
				}
			}

			if shareID == "" {
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   "missing share ID",
				})
				return
			}

			result, err := s.sourcesService.UnmountNasShare(shareID)
			if err != nil {
				log.Error().Err(err).Msg("Failed to unmount NAS share")
				client.Emit("pushNasShareResult", sources.SourceResult{
					Success: false,
					Error:   err.Error(),
				})
				return
			}

			log.Info().Bool("success", result.Success).Msg("pushNasShareResult")
			client.Emit("pushNasShareResult", result)

			// Push updated list
			if result.Success {
				shares, _ := s.sourcesService.ListNasShares()
				s.io.Emit("pushListNasShares", shares)
			}
		})

		// ============================================================
		// Streaming Services (Qobuz) Events
		// ============================================================

		// Get Qobuz login status
		client.On("getQobuzStatus", func(args ...any) {
			log.Info().Str("id", clientID).Msg("getQobuzStatus requested")
			if s.qobuzService == nil {
				client.Emit("pushQobuzStatus", map[string]interface{}{
					"loggedIn": false,
					"error":    "Qobuz service not available",
				})
				return
			}
			status := s.qobuzService.GetStatus()
			log.Info().Bool("loggedIn", status.LoggedIn).Str("email", status.Email).Msg("pushQobuzStatus")
			client.Emit("pushQobuzStatus", status)
		})

		// Login to Qobuz
		client.On("qobuzLogin", func(args ...any) {
			log.Info().Str("id", clientID).Msg("qobuzLogin requested")
			if s.qobuzService == nil {
				client.Emit("pushQobuzLoginResult", map[string]interface{}{
					"success": false,
					"error":   "Qobuz service not available",
				})
				return
			}

			if len(args) == 0 {
				client.Emit("pushQobuzLoginResult", map[string]interface{}{
					"success": false,
					"error":   "missing credentials",
				})
				return
			}

			data, ok := args[0].(map[string]interface{})
			if !ok {
				client.Emit("pushQobuzLoginResult", map[string]interface{}{
					"success": false,
					"error":   "invalid request format",
				})
				return
			}

			email := getString(data, "email")
			password := getString(data, "password")

			if email == "" || password == "" {
				client.Emit("pushQobuzLoginResult", map[string]interface{}{
					"success": false,
					"error":   "email and password are required",
				})
				return
			}

			result, err := s.qobuzService.Login(email, password)
			if err != nil {
				log.Error().Err(err).Msg("Qobuz login failed")
				client.Emit("pushQobuzLoginResult", map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				})
				return
			}

			log.Info().Bool("success", result.Success).Msg("pushQobuzLoginResult")
			client.Emit("pushQobuzLoginResult", result)

			// Broadcast updated status to all clients
			if result.Success {
				s.io.Emit("pushQobuzStatus", s.qobuzService.GetStatus())
				// Also update browse sources
				s.broadcastBrowseSources()
			}
		})

		// Logout from Qobuz
		client.On("qobuzLogout", func(args ...any) {
			log.Info().Str("id", clientID).Msg("qobuzLogout requested")
			if s.qobuzService == nil {
				client.Emit("pushQobuzLogoutResult", map[string]interface{}{
					"success": false,
					"error":   "Qobuz service not available",
				})
				return
			}

			if err := s.qobuzService.Logout(); err != nil {
				log.Error().Err(err).Msg("Qobuz logout failed")
				client.Emit("pushQobuzLogoutResult", map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				})
				return
			}

			log.Info().Msg("Qobuz logout successful")
			client.Emit("pushQobuzLogoutResult", map[string]interface{}{
				"success": true,
				"message": "Successfully logged out from Qobuz",
			})

			// Broadcast updated status to all clients
			s.io.Emit("pushQobuzStatus", s.qobuzService.GetStatus())
			// Also update browse sources
			s.broadcastBrowseSources()
		})

		// Search Qobuz
		client.On("qobuzSearch", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("qobuzSearch requested")
			if s.qobuzService == nil {
				client.Emit("pushQobuzSearchResult", map[string]interface{}{
					"error": "Qobuz service not available",
				})
				return
			}

			if !s.qobuzService.IsLoggedIn() {
				client.Emit("pushQobuzSearchResult", map[string]interface{}{
					"error": "not logged in to Qobuz",
				})
				return
			}

			if len(args) == 0 {
				client.Emit("pushQobuzSearchResult", map[string]interface{}{
					"error": "missing search query",
				})
				return
			}

			data, ok := args[0].(map[string]interface{})
			if !ok {
				client.Emit("pushQobuzSearchResult", map[string]interface{}{
					"error": "invalid request format",
				})
				return
			}

			query := getString(data, "query")
			if query == "" {
				client.Emit("pushQobuzSearchResult", map[string]interface{}{
					"error": "query is required",
				})
				return
			}

			limit := 50
			if l, ok := data["limit"].(float64); ok {
				limit = int(l)
			}

			result, err := s.qobuzService.Search(query, limit)
			if err != nil {
				log.Error().Err(err).Str("query", query).Msg("Qobuz search failed")
				client.Emit("pushQobuzSearchResult", map[string]interface{}{
					"error": err.Error(),
				})
				return
			}

			log.Info().Str("query", query).Msg("pushQobuzSearchResult")
			client.Emit("pushQobuzSearchResult", result)
		})

		// ============================================================
		// Local Music Events (Local + USB only, excludes NAS/Streaming)
		// ============================================================

		// Get local albums (local disk + USB only)
		client.On("getLocalAlbums", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("getLocalAlbums requested")
			if s.localMusicService == nil {
				client.Emit("pushLocalAlbums", map[string]interface{}{
					"albums":      []interface{}{},
					"totalCount":  0,
					"filteredOut": 0,
					"error":       "local music service not available",
				})
				return
			}

			// Parse request parameters
			req := localmusic.GetLocalAlbumsRequest{
				Sort:  localmusic.AlbumSortAlphabetical,
				Limit: 0, // No limit by default
			}

			if len(args) > 0 {
				if data, ok := args[0].(map[string]interface{}); ok {
					if sort, ok := data["sort"].(string); ok {
						req.Sort = localmusic.AlbumSortOrder(sort)
					}
					if query, ok := data["query"].(string); ok {
						req.Query = query
					}
					if limit, ok := data["limit"].(float64); ok {
						req.Limit = int(limit)
					}
				}
			}

			resp := s.localMusicService.GetLocalAlbums(req)
			log.Info().
				Int("albumCount", len(resp.Albums)).
				Int("filteredOut", resp.FilteredOut).
				Str("sort", string(req.Sort)).
				Msg("pushLocalAlbums")
			client.Emit("pushLocalAlbums", resp)
		})

		// Get last played tracks (local sources + manual plays only)
		client.On("getLastPlayedTracks", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("getLastPlayedTracks requested")
			if s.localMusicService == nil {
				client.Emit("pushLastPlayedTracks", map[string]interface{}{
					"tracks":     []interface{}{},
					"totalCount": 0,
					"error":      "local music service not available",
				})
				return
			}

			// Parse request parameters
			req := localmusic.GetLastPlayedRequest{
				Sort:  localmusic.TrackSortLastPlayed,
				Limit: 50,
			}

			if len(args) > 0 {
				if data, ok := args[0].(map[string]interface{}); ok {
					if sort, ok := data["sort"].(string); ok {
						req.Sort = localmusic.TrackSortOrder(sort)
					}
					if limit, ok := data["limit"].(float64); ok {
						req.Limit = int(limit)
					}
				}
			}

			resp := s.localMusicService.GetLastPlayedTracks(req)
			log.Info().
				Int("trackCount", len(resp.Tracks)).
				Str("sort", string(req.Sort)).
				Msg("pushLastPlayedTracks")
			client.Emit("pushLastPlayedTracks", resp)
		})

		// Get tracks for a specific album
		client.On("getAlbumTracks", func(args ...any) {
			log.Info().Str("id", clientID).Interface("args", args).Msg("getAlbumTracks requested")
			if s.localMusicService == nil {
				client.Emit("pushAlbumTracks", map[string]interface{}{
					"tracks":     []interface{}{},
					"totalCount": 0,
					"error":      "local music service not available",
				})
				return
			}

			// Parse request parameters
			req := localmusic.GetAlbumTracksRequest{}

			if len(args) > 0 {
				if data, ok := args[0].(map[string]interface{}); ok {
					if albumUri, ok := data["albumUri"].(string); ok {
						req.AlbumURI = albumUri
					}
				}
			}

			resp := s.localMusicService.GetAlbumTracks(req)
			log.Info().
				Str("albumUri", req.AlbumURI).
				Int("trackCount", len(resp.Tracks)).
				Msg("pushAlbumTracks")
			client.Emit("pushAlbumTracks", resp)
		})

		// Record a manual track play (for history tracking)
		client.On("recordTrackPlay", func(args ...any) {
			log.Debug().Str("id", clientID).Interface("args", args).Msg("recordTrackPlay requested")
			if s.localMusicService == nil {
				return
			}

			if len(args) == 0 {
				return
			}

			data, ok := args[0].(map[string]interface{})
			if !ok {
				return
			}

			uri := getString(data, "uri")
			if uri == "" {
				return
			}

			title := getString(data, "title")
			artist := getString(data, "artist")
			album := getString(data, "album")
			albumArt := getString(data, "albumArt")
			originStr := getString(data, "origin")

			origin := localmusic.PlayOriginManualTrack
			switch originStr {
			case "album_context":
				origin = localmusic.PlayOriginAlbumContext
			case "autoplay_next":
				origin = localmusic.PlayOriginAutoplayNext
			case "queue":
				origin = localmusic.PlayOriginQueue
			}

			s.localMusicService.RecordTrackPlay(uri, title, artist, album, albumArt, origin)
			log.Debug().
				Str("uri", uri).
				Str("origin", string(origin)).
				Msg("Track play recorded")
		})

		// Get history statistics
		client.On("getHistoryStats", func(args ...any) {
			log.Debug().Str("id", clientID).Msg("getHistoryStats requested")
			if s.localMusicService == nil {
				client.Emit("pushHistoryStats", map[string]interface{}{
					"error": "local music service not available",
				})
				return
			}

			stats := s.localMusicService.GetHistoryStats()
			client.Emit("pushHistoryStats", stats)
		})

		// Clear history
		client.On("clearHistory", func(args ...any) {
			log.Info().Str("id", clientID).Msg("clearHistory requested")
			if s.localMusicService == nil {
				return
			}

			s.localMusicService.ClearHistory()
			client.Emit("pushHistoryCleared", map[string]interface{}{
				"success": true,
			})
		})

		// ============================================================
		// Audirvana Integration Events
		// ============================================================

		// Get Audirvana status (detection and discovery)
		client.On("getAudirvanaStatus", func(args ...any) {
			log.Info().Str("id", clientID).Msg("getAudirvanaStatus requested")
			if s.audirvanaService == nil {
				client.Emit("pushAudirvanaStatus", audirvana.Status{
					Installed: false,
					Service: audirvana.ServiceStatus{
						Loaded:  false,
						Enabled: false,
						Active:  false,
						Running: false,
					},
					Instances: []audirvana.Instance{},
					Error:     "audirvana service not available",
				})
				return
			}

			status := s.audirvanaService.GetStatus()
			log.Info().
				Bool("installed", status.Installed).
				Bool("running", status.Service.Running).
				Int("instances", len(status.Instances)).
				Msg("pushAudirvanaStatus")
			client.Emit("pushAudirvanaStatus", status)
		})

		// Start Audirvana service
		client.On("audirvanaStartService", func(args ...any) {
			log.Info().Str("id", clientID).Msg("audirvanaStartService requested")
			if s.audirvanaService == nil {
				client.Emit("pushAudirvanaStatus", audirvana.Status{
					Error: "audirvana service not available",
				})
				return
			}

			if err := s.audirvanaService.StartService(); err != nil {
				log.Error().Err(err).Msg("Failed to start Audirvana service")
				client.Emit("pushAudirvanaStatus", audirvana.Status{
					Error: "Failed to start service: " + err.Error(),
				})
				return
			}

			// Wait a moment for service to start, then get status
			time.Sleep(2 * time.Second)
			status := s.audirvanaService.GetStatus()
			client.Emit("pushAudirvanaStatus", status)
			// Broadcast to all clients
			s.io.Emit("pushAudirvanaStatus", status)
		})

		// Stop Audirvana service
		client.On("audirvanaStopService", func(args ...any) {
			log.Info().Str("id", clientID).Msg("audirvanaStopService requested")
			if s.audirvanaService == nil {
				client.Emit("pushAudirvanaStatus", audirvana.Status{
					Error: "audirvana service not available",
				})
				return
			}

			if err := s.audirvanaService.StopService(); err != nil {
				log.Error().Err(err).Msg("Failed to stop Audirvana service")
				client.Emit("pushAudirvanaStatus", audirvana.Status{
					Error: "Failed to stop service: " + err.Error(),
				})
				return
			}

			// Wait a moment for service to stop, then get status
			time.Sleep(1 * time.Second)
			status := s.audirvanaService.GetStatus()
			client.Emit("pushAudirvanaStatus", status)
			// Broadcast to all clients
			s.io.Emit("pushAudirvanaStatus", status)
		})
	})
}

// getString safely extracts a string from a map.
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getBrowseSources returns the list of available music sources.
func (s *Server) getBrowseSources() []map[string]interface{} {
	sources := []map[string]interface{}{
		{
			"name":        "Music Library",
			"uri":         "music-library",
			"plugin_type": "music_service",
			"plugin_name": "mpd",
			"albumart":    "/albumart?sourceicon=music_service/mpd/musiclibraryicon.svg",
		},
	}

	// Add Qobuz if logged in
	if s.qobuzService != nil && s.qobuzService.IsLoggedIn() {
		qobuzSource := s.qobuzService.GetBrowseSource()
		if qobuzSource != nil {
			sources = append(sources, map[string]interface{}{
				"name":        qobuzSource.Name,
				"uri":         qobuzSource.URI,
				"plugin_type": qobuzSource.PluginType,
				"plugin_name": qobuzSource.PluginName,
				"albumart":    qobuzSource.AlbumArt,
				"icon":        qobuzSource.Icon,
			})
		}
	}

	return sources
}

// broadcastBrowseSources sends updated browse sources to all clients.
func (s *Server) broadcastBrowseSources() {
	sources := s.getBrowseSources()
	s.io.Emit("pushBrowseSources", sources)
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

	// Update audio controller with current state
	mpdState, _ := state["status"].(string)
	audioFormat := ""
	if sr, ok := state["samplerate"].(string); ok {
		audioFormat = sr
		if bd, ok := state["bitdepth"].(string); ok {
			audioFormat += ":" + bd
		}
		if ch, ok := state["channels"].(string); ok {
			audioFormat += ":" + ch
		}
	}

	if s.audioController.UpdateFromMPDStatus(mpdState, audioFormat) {
		s.BroadcastAudioStatus()
	}

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
	subsystems := []string{"player", "mixer", "playlist", "options", "database"}
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
				case "database":
					// MPD database was updated (rescan completed), rebuild cache
					s.handleDatabaseUpdate()
				}
			}
		}
	}()

	return nil
}

// handleDatabaseUpdate handles MPD database changes by rebuilding the library cache.
func (s *Server) handleDatabaseUpdate() {
	if s.cachedService == nil {
		return
	}

	log.Info().Msg("MPD database updated, rebuilding library cache")

	go func() {
		if err := s.cachedService.RebuildCache(); err != nil {
			log.Error().Err(err).Msg("Failed to rebuild cache after database update")
			return
		}

		// Get updated stats and broadcast
		stats, err := s.cachedService.GetCacheStatus()
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get cache status after rebuild")
			return
		}

		log.Info().
			Int("albums", stats.AlbumCount).
			Int("artists", stats.ArtistCount).
			Int("tracks", stats.TrackCount).
			Msg("Library cache rebuilt after database update")

		// Broadcast cache updated event to all clients
		event := map[string]interface{}{
			"timestamp":   stats.LastUpdated.Format("2006-01-02T15:04:05Z07:00"),
			"albumCount":  stats.AlbumCount,
			"artistCount": stats.ArtistCount,
			"trackCount":  stats.TrackCount,
		}
		s.io.Emit("library:cache:updated", event)
	}()
}

// ServeHTTP implements http.Handler for the Socket.io server.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.io.ServeHandler(nil).ServeHTTP(w, r)
}

// InitializeCache checks if the cache is empty and triggers a background rebuild if needed.
// This should be called after the server is created to ensure the cache is populated.
func (s *Server) InitializeCache() {
	if s.cachedService == nil {
		return
	}

	stats, err := s.cachedService.GetCacheStatus()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get cache status for initialization")
		return
	}

	// If cache is empty, trigger a background build
	if stats.AlbumCount == 0 && stats.ArtistCount == 0 {
		log.Info().Msg("Library cache is empty, triggering background build")
		go func() {
			if err := s.cachedService.RebuildCache(); err != nil {
				log.Error().Err(err).Msg("Background cache rebuild failed")
				return
			}

			// Get updated stats and broadcast
			newStats, err := s.cachedService.GetCacheStatus()
			if err != nil {
				log.Warn().Err(err).Msg("Failed to get cache status after rebuild")
				return
			}

			log.Info().
				Int("albums", newStats.AlbumCount).
				Int("artists", newStats.ArtistCount).
				Int("tracks", newStats.TrackCount).
				Msg("Library cache built successfully")

			// Broadcast cache updated event to all clients
			event := map[string]interface{}{
				"timestamp":   newStats.LastUpdated.Format("2006-01-02T15:04:05Z07:00"),
				"albumCount":  newStats.AlbumCount,
				"artistCount": newStats.ArtistCount,
				"trackCount":  newStats.TrackCount,
			}
			s.io.Emit("library:cache:updated", event)
		}()
	} else {
		log.Info().
			Int("albums", stats.AlbumCount).
			Int("artists", stats.ArtistCount).
			Msg("Library cache loaded from disk")
	}
}

// Close closes the Socket.io server and cache database.
func (s *Server) Close() error {
	s.io.Close(nil)
	if s.cacheDB != nil {
		if err := s.cacheDB.Close(); err != nil {
			log.Warn().Err(err).Msg("Failed to close cache database")
		}
	}
	return nil
}

