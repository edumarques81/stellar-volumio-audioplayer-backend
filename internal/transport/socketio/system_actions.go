package socketio

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"

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
	deps        SystemActionDeps
	trustedNets []*net.IPNet
}

// errNonLoopback is returned by handle*Internal when the caller is not
// loopback. RegisterHandlers maps this to the sanitized "unauthorized"
// payload — clients never see the underlying detail.
var errNonLoopback = errors.New("non-loopback caller refused")

// NewSystemActionHandlers constructs the bundle with loopback-only auth.
// Missing deps are filled in with DefaultShutdown / DefaultReboot so
// production wiring works without explicit deps.
//
// For deployments that need to authorize a non-loopback caller (e.g. a
// dev-mode frontend running on a separate host), use
// NewSystemActionHandlersWithTrusted instead.
func NewSystemActionHandlers(deps SystemActionDeps) *SystemActionHandlers {
	h, _ := NewSystemActionHandlersWithTrusted(deps, nil)
	return h
}

// NewSystemActionHandlersWithTrusted is the loopback+allowlist constructor.
// trustedSpecs is a list of IP or CIDR strings; bare IPs become single-host
// networks (/32 IPv4 or /128 IPv6). Empty/whitespace specs are skipped.
// Returns an error if any spec is malformed so misconfiguration is loud,
// not silently permissive.
func NewSystemActionHandlersWithTrusted(deps SystemActionDeps, trustedSpecs []string) (*SystemActionHandlers, error) {
	if deps.Shutdown == nil {
		deps.Shutdown = DefaultShutdown
	}
	if deps.Reboot == nil {
		deps.Reboot = DefaultReboot
	}
	nets, err := parseTrustedSpecs(trustedSpecs)
	if err != nil {
		return nil, err
	}
	return &SystemActionHandlers{deps: deps, trustedNets: nets}, nil
}

func parseTrustedSpecs(specs []string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, raw := range specs {
		spec := strings.TrimSpace(raw)
		if spec == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(spec); err == nil {
			nets = append(nets, n)
			continue
		}
		ip := net.ParseIP(spec)
		if ip == nil {
			return nil, fmt.Errorf("invalid trusted remote spec %q", raw)
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return nets, nil
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

// handleShutdownInternal: auth check FIRST, then dispatch.
// Tests target this directly so the security gate is verifiable
// without a real socket. Unauthorized callers wrap errNonLoopback so
// the surface error message can be sanitized in RegisterHandlers while
// the warn log still captures the IP.
func (h *SystemActionHandlers) handleShutdownInternal(remoteIP string) error {
	if !h.isAuthorized(remoteIP) {
		return fmt.Errorf("system:shutdown from %q: %w", remoteIP, errNonLoopback)
	}
	log.Info().Str("remote_ip", remoteIP).Msg("system:shutdown authorized; executing")
	return h.deps.Shutdown()
}

func (h *SystemActionHandlers) handleRebootInternal(remoteIP string) error {
	if !h.isAuthorized(remoteIP) {
		return fmt.Errorf("system:reboot from %q: %w", remoteIP, errNonLoopback)
	}
	log.Info().Str("remote_ip", remoteIP).Msg("system:reboot authorized; executing")
	return h.deps.Reboot()
}

// isAuthorized returns true for loopback callers and for any IP that
// matches one of the configured trustedNets. Empty/unparseable inputs
// are NOT authorized — fail closed.
func (h *SystemActionHandlers) isAuthorized(remoteIP string) bool {
	if isLoopback(remoteIP) {
		return true
	}
	if remoteIP == "" {
		return false
	}
	parsed := net.ParseIP(remoteIP)
	if parsed == nil {
		return false
	}
	for _, n := range h.trustedNets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
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
