package socketio

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"
)

// SystemActionDeps lets tests substitute the side-effecting executors.
// Production deployment uses DefaultShutdown / DefaultReboot.
type SystemActionDeps struct {
	Shutdown func() error
	Reboot   func() error
}

// SystemActionHandlers wires system:shutdown / system:reboot to root commands.
//
// Auth: only loopback callers (127.0.0.1 / ::1) are allowed. Non-loopback
// callers are refused with a clear error event — they do NOT silently fail.
// This is a SECURITY guarantee documented in the Plan 3 spec: the redesigned
// frontend (which lives on the same Pi) must hit the backend over loopback,
// while remote Volumio Connect mobile apps cannot trigger power actions.
//
// The check happens BEFORE any system call.
type SystemActionHandlers struct {
	deps SystemActionDeps
}

// NewSystemActionHandlers constructs the bundle. Missing deps are filled
// in with DefaultShutdown / DefaultReboot so production wiring works
// without explicit deps.
func NewSystemActionHandlers(deps SystemActionDeps) *SystemActionHandlers {
	if deps.Shutdown == nil {
		deps.Shutdown = DefaultShutdown
	}
	if deps.Reboot == nil {
		deps.Reboot = DefaultReboot
	}
	return &SystemActionHandlers{deps: deps}
}

// RegisterHandlers attaches the socket events.
func (h *SystemActionHandlers) RegisterHandlers(client *socket.Socket) {
	client.On("system:shutdown", func(_ ...interface{}) {
		ip := extractRemoteIP(client)
		if err := h.handleShutdownInternal(ip); err != nil {
			log.Warn().Err(err).Str("remote_ip", ip).Msg("system:shutdown rejected")
			_ = client.Emit("system:action:error", map[string]string{
				"action": "shutdown",
				"error":  err.Error(),
			})
			return
		}
	})
	client.On("system:reboot", func(_ ...interface{}) {
		ip := extractRemoteIP(client)
		if err := h.handleRebootInternal(ip); err != nil {
			log.Warn().Err(err).Str("remote_ip", ip).Msg("system:reboot rejected")
			_ = client.Emit("system:action:error", map[string]string{
				"action": "reboot",
				"error":  err.Error(),
			})
			return
		}
	})
}

// handleShutdownInternal: loopback check FIRST, then dispatch.
// Tests target this directly so the security gate is verifiable
// without a real socket.
func (h *SystemActionHandlers) handleShutdownInternal(remoteIP string) error {
	if !isLoopback(remoteIP) {
		return fmt.Errorf("system:shutdown refused: non-loopback caller %q", remoteIP)
	}
	log.Info().Str("remote_ip", remoteIP).Msg("system:shutdown authorized; executing")
	return h.deps.Shutdown()
}

func (h *SystemActionHandlers) handleRebootInternal(remoteIP string) error {
	if !isLoopback(remoteIP) {
		return fmt.Errorf("system:reboot refused: non-loopback caller %q", remoteIP)
	}
	log.Info().Str("remote_ip", remoteIP).Msg("system:reboot authorized; executing")
	return h.deps.Reboot()
}

// isLoopback returns true only for 127.0.0.0/8 IPv4 or ::1 IPv6.
// Empty/unparseable inputs are NOT loopback — fail closed.
func isLoopback(ip string) bool {
	if ip == "" {
		return false
	}
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

// DefaultShutdown executes a host shutdown. Uses /sbin/shutdown on Linux/macOS;
// returns an error on Windows (unsupported deployment target).
func DefaultShutdown() error {
	if runtime.GOOS == "windows" {
		return errors.New("shutdown unsupported on windows")
	}
	return exec.Command("/sbin/shutdown", "-h", "now").Run()
}

// DefaultReboot executes a host reboot.
func DefaultReboot() error {
	if runtime.GOOS == "windows" {
		return errors.New("reboot unsupported on windows")
	}
	return exec.Command("/sbin/shutdown", "-r", "now").Run()
}
