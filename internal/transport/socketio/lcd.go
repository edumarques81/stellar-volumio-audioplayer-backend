// Package socketio provides the Socket.io server for client communication.
package socketio

import (
	"os"
	"os/exec"
	"strings"

	"github.com/rs/zerolog/log"
)

// LCDStatus represents the current LCD display status.
type LCDStatus struct {
	IsOn bool `json:"isOn"` // true if LCD is on
}

// isWaylandSession checks if we're running under a Wayland compositor.
func isWaylandSession() bool {
	// Check WAYLAND_DISPLAY environment variable
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return true
	}
	// Check XDG_SESSION_TYPE
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		return true
	}
	// Check if wlr-randr is available and a Wayland compositor is running
	// by looking for Cage, Sway, or other wlroots-based compositors
	if _, err := exec.LookPath("wlr-randr"); err == nil {
		// Try to run wlr-randr - if it succeeds, we're on Wayland
		cmd := exec.Command("wlr-randr")
		cmd.Env = getWaylandEnv()
		if err := cmd.Run(); err == nil {
			return true
		}
	}
	return false
}

// getWaylandEnv returns environment variables needed for Wayland commands.
func getWaylandEnv() []string {
	env := os.Environ()
	// Add XDG_RUNTIME_DIR if not set
	hasXdgRuntime := false
	hasWaylandDisplay := false
	for _, e := range env {
		if strings.HasPrefix(e, "XDG_RUNTIME_DIR=") {
			hasXdgRuntime = true
		}
		if strings.HasPrefix(e, "WAYLAND_DISPLAY=") {
			hasWaylandDisplay = true
		}
	}
	if !hasXdgRuntime {
		// Default for user ID 1000 (typical pi user)
		env = append(env, "XDG_RUNTIME_DIR=/run/user/1000")
	}
	if !hasWaylandDisplay {
		env = append(env, "WAYLAND_DISPLAY=wayland-0")
	}
	return env
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

// getLCDStatusWayland gets LCD status using wlr-randr (for Wayland/Cage).
func getLCDStatusWayland() (LCDStatus, bool) {
	status := LCDStatus{IsOn: true}

	cmd := exec.Command("wlr-randr")
	cmd.Env = getWaylandEnv()
	out, err := cmd.Output()
	if err != nil {
		log.Debug().Err(err).Msg("wlr-randr failed")
		return status, false
	}

	output := string(out)
	// Parse wlr-randr output to find HDMI-A-1 status
	// Example output:
	// HDMI-A-1 "DO NOT USE - RTK RTK FHD..."
	//   Enabled: yes
	//   Modes: ...
	lines := strings.Split(output, "\n")
	inHDMI := false
	for _, line := range lines {
		if strings.HasPrefix(line, "HDMI-A-1") {
			inHDMI = true
			continue
		}
		if inHDMI && strings.Contains(line, "Enabled:") {
			if strings.Contains(line, "no") {
				status.IsOn = false
			}
			log.Debug().Bool("isOn", status.IsOn).Msg("LCD status from wlr-randr")
			return status, true
		}
		// If we hit another display, stop
		if inHDMI && !strings.HasPrefix(line, " ") && line != "" {
			break
		}
	}

	// If we found HDMI-A-1 but no explicit Enabled line, assume on
	if inHDMI {
		return status, true
	}

	return status, false
}

// GetLCDStatus returns the current LCD display status.
func GetLCDStatus() LCDStatus {
	status := LCDStatus{IsOn: true} // Default to on

	// Try Wayland (wlr-randr) first - this is the correct method for Cage/Wayland
	if isWaylandSession() {
		if wlStatus, ok := getLCDStatusWayland(); ok {
			return wlStatus
		}
		log.Debug().Msg("Wayland detected but wlr-randr failed, trying fallbacks")
	}

	// Try DRM DPMS interface (Pi 5 and modern systems with X11)
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

// setLCDPowerDPMS sets LCD power using DRM DPMS sysfs interface.
// This is preferred over wlr-randr because it doesn't disconnect the display
// from the compositor, allowing the browser to continue running in standby.
func setLCDPowerDPMS(on bool) error {
	drmPath := getDRMDisplayPath()
	if drmPath == "" {
		return os.ErrNotExist
	}

	// DPMS values: On=0, Standby=1, Suspend=2, Off=3
	// We use "Off" for standby (backlight off) and "On" for wake
	value := "Off"
	if on {
		value = "On"
	}

	// Write to the DPMS file (requires root or appropriate permissions)
	err := os.WriteFile(drmPath, []byte(value), 0644)
	if err != nil {
		// Try using sudo if direct write fails
		cmd := exec.Command("sudo", "sh", "-c", "echo "+value+" > "+drmPath)
		output, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			log.Error().Err(cmdErr).Str("output", string(output)).Bool("on", on).Msg("DPMS write failed")
			return cmdErr
		}
	}

	log.Info().Bool("on", on).Str("path", drmPath).Msg("LCD power changed via DRM DPMS")
	return nil
}

// setLCDPowerWayland sets LCD power using wlr-randr (for Wayland/Cage).
// NOTE: This completely disables the display output which disconnects the browser.
// Prefer setLCDPowerDPMS for standby mode.
func setLCDPowerWayland(on bool) error {
	mode := "--off"
	if on {
		mode = "--on"
	}

	cmd := exec.Command("wlr-randr", "--output", "HDMI-A-1", mode)
	cmd.Env = getWaylandEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("output", string(output)).Bool("on", on).Msg("wlr-randr failed")
		return err
	}

	log.Info().Bool("on", on).Msg("LCD power changed via wlr-randr (Wayland)")
	return nil
}

// SetLCDPower turns the LCD display on or off.
func SetLCDPower(on bool) error {
	// Try DRM DPMS first - this is preferred because it keeps the display
	// connected to the compositor, allowing the browser to stay running.
	// This enables touch-to-wake functionality.
	if err := setLCDPowerDPMS(on); err == nil {
		return nil
	}
	log.Debug().Msg("DRM DPMS failed, trying other methods")

	// Try vcgencmd for older Pi models (this also keeps display connected)
	value := "0"
	if on {
		value = "1"
	}
	cmd := exec.Command("vcgencmd", "display_power", value)
	if err := cmd.Run(); err == nil {
		log.Info().Bool("on", on).Msg("LCD power changed via vcgencmd")
		return nil
	}
	log.Debug().Msg("vcgencmd failed, trying Wayland methods")

	// Try Wayland (wlr-randr) as last resort
	// WARNING: This disconnects the display from Cage, breaking touch-to-wake!
	if isWaylandSession() {
		if err := setLCDPowerWayland(on); err == nil {
			return nil
		}
		log.Debug().Msg("Wayland wlr-randr failed")
	}

	// Try X11 methods (xrandr, xset) for non-Wayland systems
	drmPath := getDRMDisplayPath()
	if drmPath != "" {
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
				log.Debug().Err(err).Bool("on", on).Msg("X11 methods failed")
			} else {
				log.Info().Bool("on", on).Msg("LCD power changed via xset")
				return nil
			}
		} else {
			log.Info().Bool("on", on).Msg("LCD power changed via xrandr")
			return nil
		}
	}

	log.Error().Bool("on", on).Msg("All LCD power methods failed")
	return os.ErrNotExist
}

// BroadcastLCDStatus sends LCD status to all connected clients.
func (s *Server) BroadcastLCDStatus() {
	status := GetLCDStatus()
	s.io.Emit("pushLcdStatus", status)
	log.Debug().Bool("isOn", status.IsOn).Msg("Broadcast LCD status")
}
