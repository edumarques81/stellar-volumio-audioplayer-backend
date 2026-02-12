package socketio

import (
	"sync"
)

// ConnectionLimiter limits the number of concurrent external (non-localhost) connections.
// Local connections (127.0.0.1, ::1) are always allowed without limit.
// When a new external connection exceeds the limit, the oldest external connection is evicted.
type ConnectionLimiter struct {
	mu          sync.Mutex
	maxExternal int
	// ordered slice of external client IDs (oldest first)
	externalClients []string
	// all tracked connections: clientID -> remoteIP
	connections map[string]string
}

// NewConnectionLimiter creates a limiter that allows up to maxExternal concurrent
// non-localhost connections.
func NewConnectionLimiter(maxExternal int) *ConnectionLimiter {
	return &ConnectionLimiter{
		maxExternal:     maxExternal,
		externalClients: make([]string, 0),
		connections:     make(map[string]string),
	}
}

// TryAdd registers a new connection. Returns whether the connection is allowed
// and the ID of any evicted client (empty string if none).
// Local connections are always allowed. External connections may evict the oldest
// external client if the limit is exceeded.
func (cl *ConnectionLimiter) TryAdd(clientID, remoteIP string) (allowed bool, evictedID string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	// Already tracked - allow
	if _, exists := cl.connections[clientID]; exists {
		return true, ""
	}

	cl.connections[clientID] = remoteIP

	if isLocalIP(remoteIP) {
		// Local connections are always allowed, not tracked in external list
		return true, ""
	}

	// External connection
	cl.externalClients = append(cl.externalClients, clientID)

	if len(cl.externalClients) > cl.maxExternal {
		// Evict oldest external client
		evictedID = cl.externalClients[0]
		cl.externalClients = cl.externalClients[1:]
		delete(cl.connections, evictedID)
		return true, evictedID
	}

	return true, ""
}

// Remove unregisters a connection when a client disconnects.
func (cl *ConnectionLimiter) Remove(clientID string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	ip, exists := cl.connections[clientID]
	if !exists {
		return
	}

	delete(cl.connections, clientID)

	if isLocalIP(ip) {
		return
	}

	// Remove from external clients list
	for i, id := range cl.externalClients {
		if id == clientID {
			cl.externalClients = append(cl.externalClients[:i], cl.externalClients[i+1:]...)
			break
		}
	}
}

// isLocalIP returns true if the IP address is localhost.
func isLocalIP(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1"
}
