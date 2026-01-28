// Package socketio provides the Socket.io server for client communication.
// This file contains Volumio Connect app compatibility handlers.
package socketio

import (
	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/device"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
)

// VolumioHandlers handles Volumio Connect app compatibility events.
type VolumioHandlers struct {
	deviceService *device.Service
	playerService *player.Service
	server        *Server
}

// NewVolumioHandlers creates a new VolumioHandlers instance.
func NewVolumioHandlers(deviceSvc *device.Service, playerSvc *player.Service, server *Server) *VolumioHandlers {
	return &VolumioHandlers{
		deviceService: deviceSvc,
		playerService: playerSvc,
		server:        server,
	}
}

// RegisterHandlers registers all Volumio-specific Socket.IO event handlers.
func (h *VolumioHandlers) RegisterHandlers(client *socket.Socket) {
	clientID := string(client.Id())

	// Device discovery events
	h.registerDeviceHandlers(client, clientID)

	// Player control events
	h.registerPlayerHandlers(client, clientID)

	// Queue manipulation events
	h.registerQueueHandlers(client, clientID)
}

// registerDeviceHandlers registers device identity and discovery handlers.
func (h *VolumioHandlers) registerDeviceHandlers(client *socket.Socket, clientID string) {
	// initSocket - Register connecting device (acknowledgment for Volumio Connect apps)
	client.On("initSocket", func(args ...any) {
		log.Debug().Str("id", clientID).Interface("data", args).Msg("initSocket")
		// No-op for now - this is used by Volumio Connect apps to register themselves
		// We just acknowledge the connection
	})

	// getDeviceInfo - Return device UUID and name
	client.On("getDeviceInfo", func(args ...any) {
		log.Debug().Str("id", clientID).Msg("getDeviceInfo")

		if h.deviceService == nil {
			client.Emit("pushDeviceInfo", map[string]interface{}{
				"uuid": "",
				"name": "Stellar",
			})
			return
		}

		info := h.deviceService.GetDeviceInfo()
		client.Emit("pushDeviceInfo", map[string]interface{}{
			"uuid": info.UUID,
			"name": info.Name,
		})
	})

	// getMultiRoomDevices - Return self in device list (single device mode)
	client.On("getMultiRoomDevices", func(args ...any) {
		log.Debug().Str("id", clientID).Msg("getMultiRoomDevices")

		// Get current player state for the device list
		var state map[string]interface{}
		if h.playerService != nil {
			var err error
			state, err = h.playerService.GetState()
			if err != nil {
				log.Warn().Err(err).Msg("Failed to get state for multi-room device list")
				state = map[string]interface{}{}
			}
		}

		var deviceEntry map[string]interface{}
		if h.deviceService != nil {
			deviceEntry = h.deviceService.GetMultiRoomDevice(state)
		} else {
			// Fallback if device service not available
			deviceEntry = map[string]interface{}{
				"id":              "stellar-default",
				"name":            "Stellar",
				"isSelf":          true,
				"type":            "device",
				"volumeAvailable": true,
				"state":           state,
			}
		}

		response := map[string]interface{}{
			"misc": map[string]bool{"debug": true},
			"list": []map[string]interface{}{deviceEntry},
		}

		client.Emit("pushMultiRoomDevices", response)
	})
}

// registerPlayerHandlers registers Volumio-specific player control handlers.
func (h *VolumioHandlers) registerPlayerHandlers(client *socket.Socket, clientID string) {
	// toggle - Play/pause toggle (commonly used by Volumio Connect apps)
	client.On("toggle", func(args ...any) {
		log.Debug().Str("id", clientID).Msg("toggle")

		if h.playerService == nil {
			log.Warn().Msg("Player service not available for toggle")
			return
		}

		if err := h.playerService.Toggle(); err != nil {
			log.Error().Err(err).Msg("Toggle failed")
		}
	})
}

// registerQueueHandlers registers queue manipulation handlers.
func (h *VolumioHandlers) registerQueueHandlers(client *socket.Socket, clientID string) {
	// addPlay - Add to queue and play immediately
	client.On("addPlay", func(args ...any) {
		log.Debug().Str("id", clientID).Interface("data", args).Msg("addPlay")

		if h.playerService == nil {
			log.Warn().Msg("Player service not available for addPlay")
			return
		}

		if len(args) > 0 {
			if m, ok := args[0].(map[string]interface{}); ok {
				if uri, ok := m["uri"].(string); ok && uri != "" {
					if err := h.playerService.AddAndPlay(uri); err != nil {
						log.Error().Err(err).Str("uri", uri).Msg("AddPlay failed")
					} else {
						// Broadcast updated queue to all clients
						h.server.BroadcastQueue()
					}
				}
			}
		}
	})

	// playNext / addToQueueNext - Insert as next track
	client.On("playNext", func(args ...any) {
		h.handlePlayNext(args, clientID)
	})
	client.On("addToQueueNext", func(args ...any) {
		h.handlePlayNext(args, clientID)
	})

	// moveQueue - Reorder queue items
	client.On("moveQueue", func(args ...any) {
		log.Debug().Str("id", clientID).Interface("data", args).Msg("moveQueue")

		if h.playerService == nil {
			log.Warn().Msg("Player service not available for moveQueue")
			return
		}

		if len(args) > 0 {
			if m, ok := args[0].(map[string]interface{}); ok {
				from := getIntFromMap(m, "from", -1)
				to := getIntFromMap(m, "to", -1)

				if from >= 0 && to >= 0 {
					if err := h.playerService.MoveQueueItem(from, to); err != nil {
						log.Error().Err(err).Int("from", from).Int("to", to).Msg("MoveQueue failed")
					} else {
						// Broadcast updated queue to all clients
						h.server.BroadcastQueue()
					}
				}
			}
		}
	})

	// removeFromQueue - Remove item from queue
	client.On("removeFromQueue", func(args ...any) {
		log.Debug().Str("id", clientID).Interface("data", args).Msg("removeFromQueue")

		if h.playerService == nil {
			log.Warn().Msg("Player service not available for removeFromQueue")
			return
		}

		if len(args) > 0 {
			pos := -1

			// Handle both object and number argument
			switch v := args[0].(type) {
			case float64:
				pos = int(v)
			case int:
				pos = v
			case map[string]interface{}:
				pos = getIntFromMap(v, "value", -1)
				if pos < 0 {
					pos = getIntFromMap(v, "position", -1)
				}
			}

			if pos >= 0 {
				if err := h.playerService.RemoveQueueItem(pos); err != nil {
					log.Error().Err(err).Int("position", pos).Msg("RemoveFromQueue failed")
				} else {
					// Broadcast updated queue to all clients
					h.server.BroadcastQueue()
				}
			}
		}
	})
}

// handlePlayNext handles the playNext/addToQueueNext event.
func (h *VolumioHandlers) handlePlayNext(args []any, clientID string) {
	log.Debug().Str("id", clientID).Interface("data", args).Msg("playNext")

	if h.playerService == nil {
		log.Warn().Msg("Player service not available for playNext")
		return
	}

	if len(args) > 0 {
		if m, ok := args[0].(map[string]interface{}); ok {
			if uri, ok := m["uri"].(string); ok && uri != "" {
				if err := h.playerService.InsertNext(uri); err != nil {
					log.Error().Err(err).Str("uri", uri).Msg("PlayNext failed")
				} else {
					// Broadcast updated queue to all clients
					h.server.BroadcastQueue()
				}
			}
		}
	}
}

// getIntFromMap safely extracts an integer from a map.
func getIntFromMap(m map[string]interface{}, key string, defaultVal int) int {
	if m == nil {
		return defaultVal
	}
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	}
	return defaultVal
}
