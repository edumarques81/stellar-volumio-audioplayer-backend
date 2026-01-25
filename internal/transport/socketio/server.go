// Package socketio provides the Socket.io server for client communication.
package socketio

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"
	"github.com/zishang520/socket.io/v3/pkg/types"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/audio"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/audirvana"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/localmusic"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/sources"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/streaming/qobuz"
	mpdclient "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/version"
)

// NetworkStatus represents the current network connection status.
type NetworkStatus struct {
	Type     string `json:"type"`     // "wifi", "ethernet", "none"
	SSID     string `json:"ssid"`     // WiFi network name (if wifi)
	Signal   int    `json:"signal"`   // WiFi signal strength 0-100 (if wifi)
	IP       string `json:"ip"`       // IP address
	Strength int    `json:"strength"` // Signal strength level 0-3 (for icon)
}

// LCDStatus represents the current LCD display status.
type LCDStatus struct {
	IsOn bool `json:"isOn"` // true if LCD is on
}

// SystemInfo represents basic system information.
type SystemInfo struct {
	ID            string `json:"id"`            // Unique device ID
	Host          string `json:"host"`          // Hostname
	Name          string `json:"name"`          // Display name
	Type          string `json:"type"`          // Device type
	ServiceName   string `json:"serviceName"`   // Service name for mDNS
	SystemVersion string `json:"systemversion"` // System version
	BuildDate     string `json:"builddate"`     // Build date
	Variant       string `json:"variant"`       // System variant
	Hardware      string `json:"hardware"`      // Hardware model
}

// Server handles Socket.io connections and events.
type Server struct {
	io                *socket.Server
	playerService     *player.Service
	mpdClient         *mpdclient.Client
	audioController   *audio.Controller
	sourcesService    *sources.Service
	qobuzService      *qobuz.Service
	localMusicService *localmusic.Service
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
	opts.SetPingTimeout(20 * time.Second)
	opts.SetPingInterval(25 * time.Second)
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

	s := &Server{
		io:                server,
		playerService:     playerService,
		mpdClient:         mpdClient,
		audioController:   audio.NewController(bitPerfect),
		sourcesService:    sourcesService,
		qobuzService:      qobuzSvc,
		localMusicService: localMusicSvc,
		audirvanaService:  audirvana.NewService(),
		clients:           make(map[string]*socket.Socket),
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

// GetSystemInfo returns basic system information.
func GetSystemInfo() SystemInfo {
	info := SystemInfo{
		Type:          "audio_player",
		ServiceName:   "stellar",
		SystemVersion: version.GetInfo().Version,
		BuildDate:     version.GetInfo().BuildTime,
		Variant:       "stellar-pi",
		Hardware:      "Raspberry Pi",
	}

	// Get hostname
	if hostname, err := os.Hostname(); err == nil {
		info.Host = hostname
		info.Name = hostname
		info.ID = hostname
	}

	// Try to get more specific hardware info from /proc/cpuinfo
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "Model") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					info.Hardware = strings.TrimSpace(parts[1])
					break
				}
			}
		}
	}

	return info
}

// GetNetworkStatus returns the current network connection status.
func GetNetworkStatus() NetworkStatus {
	status := NetworkStatus{
		Type:     "none",
		Signal:   0,
		Strength: 0,
	}

	// Check ethernet first (usually eth0 or end0 on newer Pi)
	for _, iface := range []string{"eth0", "end0"} {
		carrierPath := "/sys/class/net/" + iface + "/carrier"
		if data, err := os.ReadFile(carrierPath); err == nil {
			if strings.TrimSpace(string(data)) == "1" {
				status.Type = "ethernet"
				status.IP = getIPAddress(iface)
				status.Signal = 100
				status.Strength = 3
				return status
			}
		}
	}

	// Check WiFi (usually wlan0)
	for _, iface := range []string{"wlan0", "wlan1"} {
		operstatePath := "/sys/class/net/" + iface + "/operstate"
		if data, err := os.ReadFile(operstatePath); err == nil {
			if strings.TrimSpace(string(data)) == "up" {
				status.Type = "wifi"
				status.IP = getIPAddress(iface)
				status.SSID, status.Signal = getWifiInfo(iface)
				// Convert signal to strength level (0-3)
				switch {
				case status.Signal >= 70:
					status.Strength = 3 // Full signal
				case status.Signal >= 50:
					status.Strength = 2 // Medium
				case status.Signal >= 30:
					status.Strength = 1 // Weak
				default:
					status.Strength = 0 // Very weak
				}
				return status
			}
		}
	}

	return status
}

// getIPAddress returns the IP address for a given interface.
func getIPAddress(iface string) string {
	out, err := exec.Command("ip", "-4", "addr", "show", iface).Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ip := strings.Split(parts[1], "/")[0]
				return ip
			}
		}
	}
	return ""
}

// getWifiInfo returns SSID and signal strength (0-100) for a WiFi interface.
func getWifiInfo(iface string) (string, int) {
	ssid := ""
	signal := 0

	// Get SSID using iwgetid
	out, err := exec.Command("iwgetid", iface, "-r").Output()
	if err == nil {
		ssid = strings.TrimSpace(string(out))
	}

	// Get signal from /proc/net/wireless
	file, err := os.Open("/proc/net/wireless")
	if err != nil {
		return ssid, signal
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, iface) {
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				linkQuality := strings.TrimSuffix(fields[2], ".")
				if q, err := strconv.Atoi(linkQuality); err == nil {
					// Link quality can be 0-70 (iwconfig format) or 0-100 (percentage)
					// Check for 0-70 range first (more common in /proc/net/wireless)
					if q >= 0 && q <= 70 {
						signal = (q * 100) / 70
					} else if q > 70 && q <= 100 {
						signal = q
					}
				}

				if signal == 0 && len(fields) >= 4 {
					sigLevel := strings.TrimSuffix(fields[3], ".")
					if dbm, err := strconv.Atoi(sigLevel); err == nil {
						if dbm < 0 {
							signal = 2 * (dbm + 100)
							if signal < 0 {
								signal = 0
							}
							if signal > 100 {
								signal = 100
							}
						}
					}
				}
			}
			break
		}
	}

	return ssid, signal
}

// getDRMDisplayPath finds the HDMI display path in /sys/class/drm/
func getDRMDisplayPath() string {
	// Common HDMI display paths on Pi 5 and other DRM-based systems
	paths := []string{
		"/sys/class/drm/card1-HDMI-A-1/dpms",
		"/sys/class/drm/card0-HDMI-A-1/dpms",
		"/sys/class/drm/card1-HDMI-A-2/dpms",
		"/sys/class/drm/card0-HDMI-A-2/dpms",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// GetLCDStatus returns the current LCD display status.
func GetLCDStatus() LCDStatus {
	status := LCDStatus{IsOn: true} // Default to on

	// Try DRM DPMS interface first (works on Pi 5 and modern systems)
	drmPath := getDRMDisplayPath()
	if drmPath != "" {
		data, err := os.ReadFile(drmPath)
		if err == nil {
			dpmsState := strings.TrimSpace(string(data))
			// DPMS states: On, Off, Standby, Suspend
			if dpmsState == "Off" || dpmsState == "Standby" || dpmsState == "Suspend" {
				status.IsOn = false
			}
			return status
		}
		log.Debug().Err(err).Str("path", drmPath).Msg("Failed to read DRM DPMS")
	}

	// Fall back to vcgencmd for older Pi models
	out, err := exec.Command("vcgencmd", "display_power").Output()
	if err != nil {
		log.Debug().Err(err).Msg("Failed to get LCD status via vcgencmd")
		return status
	}

	output := strings.TrimSpace(string(out))
	if strings.Contains(output, "=0") {
		status.IsOn = false
	}

	return status
}

// SetLCDPower turns the LCD display on or off.
func SetLCDPower(on bool) error {
	// Try DRM DPMS interface first (Pi 5 and modern systems)
	drmPath := getDRMDisplayPath()
	if drmPath != "" {
		// On Pi 5, we need to use xrandr or write to DPMS sysfs
		// The dpms sysfs file is read-only, so we use xrandr
		display := "HDMI-A-1"
		mode := "off"
		if on {
			mode = "on"
		}

		dpmsMode := "Off"
		if on {
			dpmsMode = "On"
		}
		cmd := exec.Command("xrandr", "--output", display, "--set", "DPMS", dpmsMode)
		cmd.Env = append(os.Environ(), "DISPLAY=:0")
		if err := cmd.Run(); err != nil {
			// Try alternative: use xset for DPMS
			cmd = exec.Command("xset", "dpms", "force", mode)
			cmd.Env = append(os.Environ(), "DISPLAY=:0")
			if err := cmd.Run(); err != nil {
				log.Error().Err(err).Bool("on", on).Msg("Failed to set LCD power via xrandr/xset")
				return err
			}
		}

		log.Info().Bool("on", on).Msg("LCD power changed via DRM")
		return nil
	}

	// Fall back to vcgencmd for older Pi models
	value := "0"
	if on {
		value = "1"
	}

	cmd := exec.Command("vcgencmd", "display_power", value)
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Bool("on", on).Msg("Failed to set LCD power via vcgencmd")
		return err
	}

	log.Info().Bool("on", on).Msg("LCD power changed via vcgencmd")
	return nil
}

// BroadcastNetworkStatus sends network status to all connected clients.
func (s *Server) BroadcastNetworkStatus() {
	status := GetNetworkStatus()
	s.io.Emit("pushNetworkStatus", status)
	log.Debug().Str("type", status.Type).Str("ip", status.IP).Int("strength", status.Strength).Msg("Broadcast network status")
}

// BroadcastLCDStatus sends LCD status to all connected clients.
func (s *Server) BroadcastLCDStatus() {
	status := GetLCDStatus()
	s.io.Emit("pushLcdStatus", status)
	log.Debug().Bool("isOn", status.IsOn).Msg("Broadcast LCD status")
}

// BroadcastAudioStatus sends audio status to all connected clients.
func (s *Server) BroadcastAudioStatus() {
	status := s.audioController.GetStatus()
	s.io.Emit("pushAudioStatus", status)
	log.Debug().Bool("locked", status.Locked).Interface("format", status.Format).Msg("Broadcast audio status")
}

// BitPerfectStatus represents the result of a bit-perfect configuration check.
type BitPerfectStatus struct {
	Status   string   `json:"status"`   // "ok", "warning", "error"
	Issues   []string `json:"issues"`   // Critical issues preventing bit-perfect
	Warnings []string `json:"warnings"` // Non-critical warnings
	Config   []string `json:"config"`   // Current configuration details
}

// PlaybackOption represents an audio output option.
type PlaybackOption struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

// PlaybackAttribute represents an attribute in playback options.
type PlaybackAttribute struct {
	Name    string           `json:"name"`
	Type    string           `json:"type"`
	Value   string           `json:"value"`
	Options []PlaybackOption `json:"options,omitempty"`
}

// PlaybackOptionsSection represents a section in playback options.
type PlaybackOptionsSection struct {
	ID         string              `json:"id"`
	Name       string              `json:"name,omitempty"`
	Attributes []PlaybackAttribute `json:"attributes"`
}

// PlaybackOptionsResponse represents the playback options response.
type PlaybackOptionsResponse struct {
	Options     []PlaybackOptionsSection `json:"options"`
	SystemCards []string                 `json:"systemCards"`
}

// GetPlaybackOptions returns available audio output devices.
func GetPlaybackOptions() PlaybackOptionsResponse {
	response := PlaybackOptionsResponse{
		Options:     []PlaybackOptionsSection{},
		SystemCards: []string{},
	}

	// Get list of sound cards using aplay -l
	out, err := exec.Command("aplay", "-l").Output()
	if err != nil {
		log.Error().Err(err).Msg("Failed to list audio devices")
		return response
	}

	var options []PlaybackOption
	var systemCards []string

	// Parse aplay -l output
	// Format: card N: CARDNAME [DESCRIPTION], device M: DEVICENAME [DESCRIPTION]
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "card ") {
			// Parse card line: "card 0: vc4hdmi0 [vc4-hdmi-0], device 0: MAI PCM i2s-hifi-0 [MAI PCM i2s-hifi-0]"
			parts := strings.SplitN(line, ":", 2)
			if len(parts) < 2 {
				continue
			}

			// Extract card name (e.g., "vc4hdmi0" from "card 0: vc4hdmi0 [vc4-hdmi-0]")
			cardPart := strings.TrimSpace(parts[1])
			cardNameEnd := strings.Index(cardPart, " [")
			if cardNameEnd == -1 {
				cardNameEnd = strings.Index(cardPart, ",")
			}
			if cardNameEnd == -1 {
				continue
			}

			cardName := strings.TrimSpace(cardPart[:cardNameEnd])

			// Get the description from brackets
			descStart := strings.Index(cardPart, "[")
			descEnd := strings.Index(cardPart, "]")
			description := cardName
			if descStart != -1 && descEnd != -1 && descEnd > descStart {
				description = cardPart[descStart+1 : descEnd]
			}

			// Make a friendly name
			friendlyName := description
			if strings.Contains(strings.ToLower(cardName), "hdmi") {
				friendlyName = "HDMI: " + description
			} else if strings.Contains(strings.ToLower(cardName), "usb") || strings.HasPrefix(strings.ToLower(cardName), "u20") {
				friendlyName = "USB: " + description
			}

			options = append(options, PlaybackOption{
				Value: cardName,
				Name:  friendlyName,
			})
			systemCards = append(systemCards, cardName)
		}
	}

	// Get currently selected device from MPD config
	selectedDevice := GetCurrentAudioOutput()

	// If no device found in config, select the first USB device, or first available
	if selectedDevice == "" {
		for _, opt := range options {
			if strings.Contains(strings.ToLower(opt.Value), "usb") || strings.HasPrefix(strings.ToLower(opt.Value), "u20") {
				selectedDevice = opt.Value
				break
			}
		}
		if selectedDevice == "" && len(options) > 0 {
			selectedDevice = options[0].Value
		}
	}

	response.Options = []PlaybackOptionsSection{
		{
			ID:   "output",
			Name: "Audio Output",
			Attributes: []PlaybackAttribute{
				{
					Name:    "output_device",
					Type:    "select",
					Value:   selectedDevice,
					Options: options,
				},
			},
		},
	}
	response.SystemCards = systemCards

	log.Debug().Interface("options", options).Str("selected", selectedDevice).Msg("Playback options")
	return response
}

// GetCurrentAudioOutput reads the current audio output device from MPD config.
func GetCurrentAudioOutput() string {
	data, err := os.ReadFile("/etc/mpd.conf")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to read MPD config for audio output")
		return ""
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	// Look for audio_output block with device setting
	inAudioOutput := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip comments
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "audio_output") {
			inAudioOutput = true
			continue
		}

		if inAudioOutput {
			if trimmed == "}" {
				inAudioOutput = false
				continue
			}

			// Look for device line like: device      "hw:2,0"
			if strings.HasPrefix(trimmed, "device") {
				// Extract the device value
				device := extractConfigValue(content, "device")
				if device != "" && strings.HasPrefix(device, "hw:") {
					// Extract card number from hw:X,Y format
					cardNum := strings.TrimPrefix(device, "hw:")
					if idx := strings.Index(cardNum, ","); idx != -1 {
						cardNum = cardNum[:idx]
					}
					// Find the card name by card number
					return getCardNameByNumber(cardNum)
				}
				return device
			}
		}
	}

	return ""
}

// getCardNameByNumber returns the card name for a given card number.
func getCardNameByNumber(cardNum string) string {
	out, err := exec.Command("aplay", "-l").Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		// Look for: card N: CARDNAME
		if strings.HasPrefix(line, "card "+cardNum+":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) < 2 {
				continue
			}
			cardPart := strings.TrimSpace(parts[1])
			cardNameEnd := strings.Index(cardPart, " [")
			if cardNameEnd == -1 {
				cardNameEnd = strings.Index(cardPart, ",")
			}
			if cardNameEnd != -1 {
				return strings.TrimSpace(cardPart[:cardNameEnd])
			}
		}
	}
	return ""
}

// SetPlaybackSettings changes the audio output device in MPD config.
func SetPlaybackSettings(deviceName string) error {
	// Find the card number for this device name
	cardNum := getCardNumberByName(deviceName)
	if cardNum == "" {
		return exec.ErrNotFound
	}

	// Read current MPD config
	data, err := os.ReadFile("/etc/mpd.conf")
	if err != nil {
		return err
	}

	content := string(data)
	newDevice := `"hw:` + cardNum + `,0"`

	// Update the device line in audio_output block
	lines := strings.Split(content, "\n")
	var newLines []string
	inAudioOutput := false
	foundDevice := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip comments when checking, but keep them in output
		if !strings.HasPrefix(trimmed, "#") {
			if strings.HasPrefix(trimmed, "audio_output") {
				inAudioOutput = true
			} else if inAudioOutput && trimmed == "}" {
				inAudioOutput = false
			} else if inAudioOutput && strings.HasPrefix(trimmed, "device") {
				// Replace the device line
				line = `    device      ` + newDevice
				foundDevice = true
			}
		}
		newLines = append(newLines, line)
	}

	if !foundDevice {
		return exec.ErrNotFound
	}

	// Write updated config
	newContent := strings.Join(newLines, "\n")
	if err := writeMPDConfig(newContent); err != nil {
		return err
	}

	// Restart MPD to apply changes
	cmd := exec.Command("sudo", "systemctl", "restart", "mpd")
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD after changing audio output")
		return err
	}

	log.Info().Str("device", deviceName).Str("hwDevice", "hw:"+cardNum+",0").Msg("Audio output changed")
	return nil
}

// getCardNumberByName returns the card number for a given card name.
func getCardNumberByName(cardName string) string {
	out, err := exec.Command("aplay", "-l").Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		// Look for: card N: CARDNAME
		if strings.HasPrefix(line, "card ") && strings.Contains(line, cardName) {
			// Extract card number
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				numStr := strings.TrimSuffix(parts[1], ":")
				return numStr
			}
		}
	}
	return ""
}

// GetBitPerfectStatus checks bit-perfect audio configuration natively in Go.
func GetBitPerfectStatus() BitPerfectStatus {
	// Read MPD config
	mpdConfig := ""
	if data, err := os.ReadFile("/etc/mpd.conf"); err == nil {
		mpdConfig = string(data)
	} else {
		log.Warn().Err(err).Msg("Failed to read MPD config")
	}

	// Read ALSA config
	alsaConfig := ""
	if data, err := os.ReadFile("/etc/asound.conf"); err == nil {
		alsaConfig = string(data)
	}

	// Get aplay output for device detection
	aplayOutput := ""
	if out, err := exec.Command("aplay", "-l").Output(); err == nil {
		aplayOutput = string(out)
	}

	return CheckBitPerfectFromConfig(mpdConfig, alsaConfig, aplayOutput)
}

// CheckBitPerfectFromConfig checks bit-perfect configuration from config strings.
// This is the main logic, separated for easier testing.
func CheckBitPerfectFromConfig(mpdConfig, alsaConfig, aplayOutput string) BitPerfectStatus {
	status := BitPerfectStatus{
		Status:   "ok",
		Issues:   []string{},
		Warnings: []string{},
		Config:   []string{},
	}

	// Check 1: MPD resampler
	if strings.Contains(mpdConfig, "resampler") {
		if strings.Contains(mpdConfig, "plugin") && (strings.Contains(mpdConfig, "soxr") || strings.Contains(mpdConfig, "libsamplerate")) {
			status.Issues = append(status.Issues, "MPD: Resampler is enabled - audio will be resampled")
		}
	} else {
		status.Config = append(status.Config, "MPD: No resampler configured (good)")
	}

	// Check 2: Volume normalization
	if strings.Contains(mpdConfig, `volume_normalization`) && strings.Contains(mpdConfig, `"yes"`) {
		// Check if it's actually volume_normalization "yes"
		if matchConfigValue(mpdConfig, "volume_normalization", "yes") {
			status.Issues = append(status.Issues, "MPD: Volume normalization is enabled - audio will be modified")
		}
	} else {
		status.Config = append(status.Config, "MPD: Volume normalization disabled (good)")
	}

	// Check 3: Direct hardware output
	if strings.Contains(mpdConfig, `device`) && strings.Contains(mpdConfig, `"hw:`) {
		// Extract device name
		device := extractConfigValue(mpdConfig, "device")
		if device != "" && strings.HasPrefix(device, "hw:") {
			status.Config = append(status.Config, "MPD: Direct hardware output: "+device+" (good)")
		}
	} else if strings.Contains(mpdConfig, `device`) && strings.Contains(mpdConfig, `"volumio"`) {
		status.Issues = append(status.Issues, "MPD: Using 'volumio' device (goes through plug layer)")
	} else if mpdConfig != "" {
		status.Warnings = append(status.Warnings, "MPD: Could not determine audio device")
	}

	// Check 4: Auto conversion settings
	for _, setting := range []string{"auto_resample", "auto_format", "auto_channels"} {
		if matchConfigValue(mpdConfig, setting, "no") {
			status.Config = append(status.Config, setting+": disabled (good)")
		} else if matchConfigValue(mpdConfig, setting, "yes") {
			status.Issues = append(status.Issues, setting+": enabled - audio may be converted")
		}
	}

	// Check 5: DSD playback mode (native vs DoP)
	if matchConfigValue(mpdConfig, "dop", "yes") {
		status.Warnings = append(status.Warnings, "DSD over PCM (DoP): enabled - consider native DSD for true bit-perfect")
	} else if matchConfigValue(mpdConfig, "dop", "no") {
		status.Config = append(status.Config, "DSD: Native DSD mode (DoP disabled) - true bit-perfect DSD")
	} else if mpdConfig != "" {
		status.Config = append(status.Config, "DSD: DoP not configured (native DSD assumed)")
	}

	// Check 6: Mixer type
	if matchConfigValue(mpdConfig, "mixer_type", "none") {
		status.Config = append(status.Config, "Mixer: disabled (bit-perfect volume)")
	} else if matchConfigValue(mpdConfig, "mixer_type", "software") {
		status.Warnings = append(status.Warnings, "Mixer: software mixing enabled (not bit-perfect)")
	}

	// Check 7: ALSA config
	if alsaConfig != "" {
		if strings.Contains(alsaConfig, "type") && strings.Contains(alsaConfig, "plug") {
			status.Warnings = append(status.Warnings, "ALSA: 'plug' type detected - may convert formats")
		}
		if strings.Contains(alsaConfig, "type") && strings.Contains(alsaConfig, "hw") {
			status.Config = append(status.Config, "ALSA: Direct hardware access configured (good)")
		}
	}

	// Check 8: USB DAC presence (Singxer SU-6)
	if aplayOutput != "" {
		if strings.Contains(aplayOutput, "U20SU6") || strings.Contains(aplayOutput, "SU-6") || strings.Contains(aplayOutput, "SU6") {
			status.Config = append(status.Config, "Hardware: Singxer SU-6 detected (native DSD capable)")
		} else {
			status.Warnings = append(status.Warnings, "Hardware: Singxer SU-6 not detected")
		}
	}

	// Determine overall status based on issues and warnings
	if len(status.Issues) > 0 {
		status.Status = "error"
	} else if len(status.Warnings) > 0 {
		status.Status = "warning"
	} else {
		status.Status = "ok"
	}

	return status
}

// matchConfigValue checks if a config setting has a specific value.
// Handles various MPD config formats like: setting "value" or setting    "value"
func matchConfigValue(config, setting, value string) bool {
	// Look for patterns like: setting "value" or setting    "value"
	lines := strings.Split(config, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip comments
		if strings.HasPrefix(line, "#") {
			continue
		}
		// Check if line contains the setting
		if strings.HasPrefix(line, setting) {
			// Extract the value after the setting name
			rest := strings.TrimPrefix(line, setting)
			rest = strings.TrimSpace(rest)
			// Check if it matches the expected value
			expectedValue := `"` + value + `"`
			if strings.HasPrefix(rest, expectedValue) || rest == expectedValue {
				return true
			}
		}
	}
	return false
}

// extractConfigValue extracts the value for a config setting.
func extractConfigValue(config, setting string) string {
	lines := strings.Split(config, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip comments
		if strings.HasPrefix(line, "#") {
			continue
		}
		// Check if line contains the setting
		if strings.HasPrefix(line, setting) {
			// Extract the value after the setting name
			rest := strings.TrimPrefix(line, setting)
			rest = strings.TrimSpace(rest)
			// Find value in quotes
			start := strings.Index(rest, `"`)
			if start != -1 {
				end := strings.Index(rest[start+1:], `"`)
				if end != -1 {
					return rest[start+1 : start+1+end]
				}
			}
		}
	}
	return ""
}

// NormalizeBitPerfectStatus converts script status values to frontend expected values.
// Script returns: "bit-perfect", "not-bit-perfect", "warning"
// Frontend expects: "ok", "error", "warning"
func NormalizeBitPerfectStatus(status BitPerfectStatus) BitPerfectStatus {
	switch status.Status {
	case "bit-perfect":
		status.Status = "ok"
	case "not-bit-perfect":
		status.Status = "error"
	}
	return status
}

// writeMPDConfig writes the MPD config file using sudo to handle permissions.
func writeMPDConfig(content string) error {
	cmd := exec.Command("sudo", "tee", "/etc/mpd.conf")
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = nil // Suppress stdout (tee echoes input)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// DsdModeResponse represents the DSD playback mode.
type DsdModeResponse struct {
	Mode    string `json:"mode"`    // "native" or "dop"
	Success bool   `json:"success"` // true if operation succeeded (for setDsdMode)
	Error   string `json:"error"`   // error message if failed
}

// GetDsdMode returns the current DSD playback mode from MPD config.
func GetDsdMode() DsdModeResponse {
	response := DsdModeResponse{
		Mode:    "native", // Default to native
		Success: true,
	}

	// Read MPD config to check dop setting
	data, err := os.ReadFile("/etc/mpd.conf")
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		response.Error = "Failed to read MPD config"
		response.Success = false
		return response
	}

	content := string(data)
	if strings.Contains(content, `dop`) {
		// Check if dop is set to "yes"
		if strings.Contains(content, `dop             "yes"`) || strings.Contains(content, `dop "yes"`) {
			response.Mode = "dop"
		}
	}

	return response
}

// SetDsdMode sets the DSD playback mode in MPD config and restarts MPD.
func SetDsdMode(mode string) DsdModeResponse {
	response := DsdModeResponse{
		Mode:    mode,
		Success: false,
	}

	// Validate mode
	if mode != "native" && mode != "dop" {
		response.Error = "Invalid mode. Must be 'native' or 'dop'"
		return response
	}

	// Read current config
	data, err := os.ReadFile("/etc/mpd.conf")
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		response.Error = "Failed to read MPD config"
		return response
	}

	content := string(data)
	var newContent string

	dopValue := "no"
	if mode == "dop" {
		dopValue = "yes"
	}

	// Replace dop setting - handle various formats
	if strings.Contains(content, `dop             "yes"`) {
		newContent = strings.Replace(content, `dop             "yes"`, `dop             "`+dopValue+`"`, 1)
	} else if strings.Contains(content, `dop             "no"`) {
		newContent = strings.Replace(content, `dop             "no"`, `dop             "`+dopValue+`"`, 1)
	} else if strings.Contains(content, `dop "yes"`) {
		newContent = strings.Replace(content, `dop "yes"`, `dop "`+dopValue+`"`, 1)
	} else if strings.Contains(content, `dop "no"`) {
		newContent = strings.Replace(content, `dop "no"`, `dop "`+dopValue+`"`, 1)
	} else {
		response.Error = "Could not find dop setting in MPD config"
		return response
	}

	// Write updated config
	if err := writeMPDConfig(newContent); err != nil {
		log.Error().Err(err).Msg("Failed to write MPD config")
		response.Error = "Failed to write MPD config: " + err.Error()
		return response
	}

	// Restart MPD
	cmd := exec.Command("sudo", "systemctl", "restart", "mpd")
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD")
		response.Error = "Config updated but failed to restart MPD: " + err.Error()
		return response
	}

	log.Info().Str("mode", mode).Msg("DSD mode changed successfully")
	response.Success = true
	return response
}

// MixerModeResponse represents the mixer configuration.
type MixerModeResponse struct {
	Enabled bool   `json:"enabled"` // true if software mixer enabled
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// GetMixerMode returns whether software mixer is enabled.
func GetMixerMode() MixerModeResponse {
	response := MixerModeResponse{
		Enabled: false, // Default to disabled (bit-perfect)
		Success: true,
	}

	// Read MPD config to check mixer_type setting
	data, err := os.ReadFile("/etc/mpd.conf")
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		response.Error = "Failed to read MPD config"
		response.Success = false
		return response
	}

	content := string(data)
	// Check if mixer_type is "software" (enabled) or "none" (disabled)
	// Use line-by-line parsing to handle variable whitespace
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip comments
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Check for mixer_type setting
		if strings.HasPrefix(trimmed, "mixer_type") {
			if strings.Contains(trimmed, `"software"`) {
				response.Enabled = true
			}
			break
		}
	}

	return response
}

// SetMixerMode enables or disables the software mixer in MPD config and restarts MPD.
func SetMixerMode(enabled bool) MixerModeResponse {
	response := MixerModeResponse{
		Enabled: enabled,
		Success: false,
	}

	// Read current config
	data, err := os.ReadFile("/etc/mpd.conf")
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		response.Error = "Failed to read MPD config"
		return response
	}

	content := string(data)

	mixerValue := "none"
	if enabled {
		mixerValue = "software"
	}

	// Replace mixer_type setting using regex to handle variable whitespace
	re := regexp.MustCompile(`(mixer_type\s+)"(?:software|none)"`)
	if !re.MatchString(content) {
		response.Error = "Could not find mixer_type setting in MPD config"
		return response
	}
	newContent := re.ReplaceAllString(content, `${1}"`+mixerValue+`"`)

	// Write updated config
	if err := writeMPDConfig(newContent); err != nil {
		log.Error().Err(err).Msg("Failed to write MPD config")
		response.Error = "Failed to write MPD config: " + err.Error()
		return response
	}

	// Restart MPD
	cmd := exec.Command("sudo", "systemctl", "restart", "mpd")
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD")
		response.Error = "Config updated but failed to restart MPD: " + err.Error()
		return response
	}

	log.Info().Bool("enabled", enabled).Msg("Mixer mode changed successfully")
	response.Success = true
	return response
}

// ApplyBitPerfectResponse represents the result of applying all bit-perfect settings.
type ApplyBitPerfectResponse struct {
	Success bool     `json:"success"`
	Applied []string `json:"applied"` // Settings that were changed
	Errors  []string `json:"errors"`  // Any errors encountered
}

// ApplyBitPerfect applies all optimal bit-perfect settings to MPD config.
func ApplyBitPerfect() ApplyBitPerfectResponse {
	response := ApplyBitPerfectResponse{
		Success: false,
		Applied: []string{},
		Errors:  []string{},
	}

	// Read current config
	data, err := os.ReadFile("/etc/mpd.conf")
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		response.Errors = append(response.Errors, "Failed to read MPD config")
		return response
	}

	content := string(data)
	newContent := content

	// Apply bit-perfect settings using regex to handle variable whitespace
	settingsToApply := []struct {
		name        string
		pattern     string // Regex pattern for current non-optimal value
		replacement string // Replacement pattern
		checkOk     string // Regex pattern to check if already optimal
	}{
		{
			name:        "mixer_type",
			pattern:     `(mixer_type\s+)"software"`,
			replacement: `${1}"none"`,
			checkOk:     `mixer_type\s+"none"`,
		},
		{
			name:        "auto_resample",
			pattern:     `(auto_resample\s+)"yes"`,
			replacement: `${1}"no"`,
			checkOk:     `auto_resample\s+"no"`,
		},
		{
			name:        "auto_format",
			pattern:     `(auto_format\s+)"yes"`,
			replacement: `${1}"no"`,
			checkOk:     `auto_format\s+"no"`,
		},
		{
			name:        "auto_channels",
			pattern:     `(auto_channels\s+)"yes"`,
			replacement: `${1}"no"`,
			checkOk:     `auto_channels\s+"no"`,
		},
	}

	for _, setting := range settingsToApply {
		re := regexp.MustCompile(setting.pattern)
		if re.MatchString(newContent) {
			newContent = re.ReplaceAllString(newContent, setting.replacement)
			response.Applied = append(response.Applied, setting.name+" = bit-perfect")
		}
	}

	// If no changes were needed, check if settings are already optimal
	if len(response.Applied) == 0 {
		for _, setting := range settingsToApply {
			re := regexp.MustCompile(setting.checkOk)
			if re.MatchString(content) {
				response.Applied = append(response.Applied, setting.name+" already set to optimal")
			}
		}
		response.Success = true
		log.Info().Strs("applied", response.Applied).Msg("Bit-perfect settings already optimal")
		return response
	}

	// Write updated config
	if err := writeMPDConfig(newContent); err != nil {
		log.Error().Err(err).Msg("Failed to write MPD config")
		response.Errors = append(response.Errors, "Failed to write MPD config: "+err.Error())
		return response
	}

	// Restart MPD
	cmd := exec.Command("sudo", "systemctl", "restart", "mpd")
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD")
		response.Errors = append(response.Errors, "Config updated but failed to restart MPD: "+err.Error())
		return response
	}

	log.Info().Strs("applied", response.Applied).Msg("Bit-perfect settings applied successfully")
	response.Success = true
	return response
}

// StartNetworkWatcher starts watching network status and broadcasts changes.
func (s *Server) StartNetworkWatcher(ctx context.Context) {
	const checkInterval = 5 * time.Second
	const signalChangeThreshold = 10 // Only broadcast if signal changes by more than 10%

	go func() {
		log.Info().Msg("Network watcher started")
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("Network watcher stopped")
				return
			case <-ticker.C:
				current := GetNetworkStatus()

				// Check if status changed
				changed := false
				if current.Type != s.lastNetwork.Type ||
					current.IP != s.lastNetwork.IP ||
					current.SSID != s.lastNetwork.SSID ||
					current.Strength != s.lastNetwork.Strength {
					changed = true
				}

				// For WiFi, also check significant signal change
				if current.Type == "wifi" && s.lastNetwork.Type == "wifi" {
					signalDiff := current.Signal - s.lastNetwork.Signal
					if signalDiff < 0 {
						signalDiff = -signalDiff
					}
					if signalDiff >= signalChangeThreshold {
						changed = true
					}
				}

				if changed {
					s.mu.Lock()
					s.lastNetwork = current
					s.mu.Unlock()
					s.BroadcastNetworkStatus()
				}
			}
		}
	}()
}
