package socketio

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"
)

// SystemActionDeps lets tests substitute the side-effecting executors.
// Production deployment uses DefaultShutdown / DefaultReboot.
//
// Broadcast is optional — when set, RegisterHandlers emits a one-shot
// `pushShutdownNotice` warning to all clients before the (deferred) exec.
// This preserves the legacy server.go inline handler's 3-second UX.
//
// On post-M1.C topologies the Mac/Windows backend wires Shutdown/Reboot to
// RemoteSystemActions so the action lands on the Pi appliance instead of
// the backend host. main.go selects the impl via STELLAR_MOUNT_REMOTE_*.
type SystemActionDeps struct {
	Shutdown  func() error
	Reboot    func() error
	Broadcast func(event string, payload any)
}

// shutdownWarningDelay is the user-visible warning window between the
// `pushShutdownNotice` broadcast and the actual exec — matches the legacy
// inline handler that lived in server.go pre-M1.D.
const shutdownWarningDelay = 3 * time.Second

// SystemActionHandlers wires the `shutdown` / `reboot` socket events to root
// commands. Both UI surfaces (PowerModal on the home screen + SystemSettings
// on the settings page) emit the bare event names (no `system:` prefix); this
// is the post-M1.D convergence — earlier code had two parallel dispatch paths
// (`system:shutdown` here, a duplicate unauthenticated `shutdown` handler in
// server.go). The duplicate is deleted; this is the single canonical path.
//
// Auth: only loopback callers (127.0.0.1 / ::1) are allowed by default; the
// trusted-remotes allowlist (STELLAR_POWER_TRUSTED_REMOTES) extends to
// specific LAN clients. Non-loopback callers are refused with a generic
// "unauthorized" error event — they do NOT silently fail, and they do NOT
// see the IP echoed back. The full detail (including remote IP) is logged
// at warn level for ops.
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

// RegisterHandlers attaches the socket events. The action follows a
// fire-and-forget pattern so the kiosk has a 3-second user-visible warning
// before the Pi powers down: auth gate → optional broadcast → goroutine
// (sleep + exec). The HTTP/socket call itself returns immediately so the
// frontend can render the toast before the Pi connection drops.
func (h *SystemActionHandlers) RegisterHandlers(client *socket.Socket) {
	client.On("shutdown", func(_ ...interface{}) { h.handleEvent(client, "shutdown") })
	client.On("reboot", func(_ ...interface{}) { h.handleEvent(client, "reboot") })
}

// handleEvent is the single auth+broadcast+exec pipeline. action ∈ {"shutdown","reboot"}.
func (h *SystemActionHandlers) handleEvent(client *socket.Socket, action string) {
	ip := extractRemoteIP(client)
	if !h.isAuthorized(ip) {
		log.Warn().Err(errNonLoopback).Str("remote_ip", ip).Str("action", action).Msg("system action rejected")
		_ = client.Emit("system:action:error", map[string]string{
			"action": action,
			"error":  "unauthorized",
		})
		return
	}
	log.Info().Str("remote_ip", ip).Str("action", action).Dur("delay", shutdownWarningDelay).Msg("system action authorized; broadcasting + deferred exec")
	if h.deps.Broadcast != nil {
		message := "Shutting down in 3 seconds..."
		if action == "reboot" {
			message = "Rebooting in 3 seconds..."
		}
		h.deps.Broadcast("pushShutdownNotice", map[string]any{
			"action":  action,
			"message": message,
		})
	}
	go func() {
		time.Sleep(shutdownWarningDelay)
		var err error
		if action == "reboot" {
			err = h.deps.Reboot()
		} else {
			err = h.deps.Shutdown()
		}
		if err != nil {
			log.Error().Err(err).Str("action", action).Msg("system action exec failed")
		}
	}()
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

// handleShutdownInternal: synchronous test seam — auth check FIRST, then
// dispatch. The production code path is handleEvent (async); this stays
// because the security-gate tests target it without spinning a real socket.
// Unauthorized callers wrap errNonLoopback so callers can errors.Is-check.
func (h *SystemActionHandlers) handleShutdownInternal(remoteIP string) error {
	if !h.isAuthorized(remoteIP) {
		return fmt.Errorf("shutdown from %q: %w", remoteIP, errNonLoopback)
	}
	log.Info().Str("remote_ip", remoteIP).Msg("shutdown authorized; executing")
	return h.deps.Shutdown()
}

func (h *SystemActionHandlers) handleRebootInternal(remoteIP string) error {
	if !h.isAuthorized(remoteIP) {
		return fmt.Errorf("reboot from %q: %w", remoteIP, errNonLoopback)
	}
	log.Info().Str("remote_ip", remoteIP).Msg("reboot authorized; executing")
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
