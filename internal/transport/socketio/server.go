// Package socketio provides the Socket.io server for client communication.
package socketio

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"
	"github.com/zishang520/socket.io/v3/pkg/types"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/audio"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/sources"
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
	io              *socket.Server
	playerService   *player.Service
	mpdClient       *mpdclient.Client
	audioController *audio.Controller
	sourcesService  *sources.Service
	mu              sync.RWMutex
	clients         map[string]*socket.Socket
	lastNetwork     NetworkStatus
}

// NewServer creates a new Socket.io server.
// bitPerfect indicates whether the system is configured for bit-perfect audio output.
func NewServer(playerService *player.Service, mpdClient *mpdclient.Client, sourcesService *sources.Service, bitPerfect bool) (*Server, error) {
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
		io:              server,
		playerService:   playerService,
		mpdClient:       mpdClient,
		audioController: audio.NewController(bitPerfect),
		sourcesService:  sourcesService,
		clients:         make(map[string]*socket.Socket),
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

			uri := ""
			if len(args) > 0 {
				if m, ok := args[0].(map[string]interface{}); ok {
					if u, ok := m["uri"].(string); ok {
						uri = u
					}
				}
			}

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
	selectedDevice := ""

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

			// Select USB device by default if available
			if strings.Contains(strings.ToLower(cardName), "usb") || strings.HasPrefix(strings.ToLower(cardName), "u20") {
				if selectedDevice == "" {
					selectedDevice = cardName
				}
			}
		}
	}

	// If no USB device, select the first one
	if selectedDevice == "" && len(options) > 0 {
		selectedDevice = options[0].Value
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
	if err := os.WriteFile("/etc/mpd.conf", []byte(newContent), 0644); err != nil {
		log.Error().Err(err).Msg("Failed to write MPD config")
		response.Error = "Failed to write MPD config: " + err.Error()
		return response
	}

	// Restart MPD
	cmd := exec.Command("systemctl", "restart", "mpd")
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD")
		response.Error = "Config updated but failed to restart MPD: " + err.Error()
		return response
	}

	log.Info().Str("mode", mode).Msg("DSD mode changed successfully")
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
