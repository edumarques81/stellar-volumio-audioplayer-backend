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
// callers are refused with a generic "unauthorized" error event — they do
// NOT silently fail, and they do NOT see the IP echoed back. The full
// detail (including remote IP) is logged at warn level for ops.
// This is a SECURITY guarantee documented in the Plan 3 spec: the redesigned
// frontend (which lives on the same Pi) must hit the backend over loopback,
// while remote Volumio Connect mobile apps cannot trigger power actions.
//
// The check happens BEFORE any system call.
type SystemActionHandlers struct {
	deps SystemActionDeps
}

// errNonLoopback is returned by handle*Internal when the caller is not
// loopback. RegisterHandlers maps this to the sanitized "unauthorized"
// payload — clients never see the underlying detail.
var errNonLoopback = errors.New("non-loopback caller refused")

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
				"error":  clientErrorMessage(err),
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
				"error":  clientErrorMessage(err),
			})
			return
		}
	})
}

// clientErrorMessage sanitizes the outgoing error payload. Non-loopback
// refusals collapse to "unauthorized" (no IP echo). Dep failures from
// authorized loopback callers (e.g. /sbin/shutdown returning permission
// denied) propagate as-is — those are operational signals, not auth leaks.
func clientErrorMessage(err error) string {
	if errors.Is(err, errNonLoopback) {
		return "unauthorized"
	}
	return err.Error()
}

// handleShutdownInternal: loopback check FIRST, then dispatch.
// Tests target this directly so the security gate is verifiable
// without a real socket. Non-loopback callers wrap errNonLoopback so
// the surface error message can be sanitized in RegisterHandlers while
// the warn log still captures the IP.
func (h *SystemActionHandlers) handleShutdownInternal(remoteIP string) error {
	if !isLoopback(remoteIP) {
		return fmt.Errorf("system:shutdown from %q: %w", remoteIP, errNonLoopback)
	}
	log.Info().Str("remote_ip", remoteIP).Msg("system:shutdown authorized; executing")
	return h.deps.Shutdown()
}

func (h *SystemActionHandlers) handleRebootInternal(remoteIP string) error {
	if !isLoopback(remoteIP) {
		return fmt.Errorf("system:reboot from %q: %w", remoteIP, errNonLoopback)
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
