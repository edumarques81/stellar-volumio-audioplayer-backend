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
	mpdclient "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
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

// Server handles Socket.io connections and events.
type Server struct {
	io              *socket.Server
	playerService   *player.Service
	mpdClient       *mpdclient.Client
	audioController *audio.Controller
	mu              sync.RWMutex
	clients         map[string]*socket.Socket
	lastNetwork     NetworkStatus
}

// NewServer creates a new Socket.io server.
// bitPerfect indicates whether the system is configured for bit-perfect audio output.
func NewServer(playerService *player.Service, mpdClient *mpdclient.Client, bitPerfect bool) (*Server, error) {
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
			// Also send network, LCD, and audio status
			client.Emit("pushNetworkStatus", GetNetworkStatus())
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
