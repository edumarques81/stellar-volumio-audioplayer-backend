package main

import (
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/transport/socketio"
)

// socketIOEmitter adapts the Socket.IO server to the spectrum.SocketEmitter interface.
type socketIOEmitter struct {
	server *socketio.Server
}

// BroadcastToAll emits an event to all connected Socket.IO clients.
func (e *socketIOEmitter) BroadcastToAll(event string, data interface{}) {
	e.server.Emit(event, data)
}
