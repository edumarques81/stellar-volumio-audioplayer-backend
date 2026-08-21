//go:build linux

package lcd

import (
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// linuxController is the production Linux LCD controller. It falls back
// through backlight sysfs -> DRM DPMS -> vcgencmd -> wlr-randr -> xrandr/xset.
type linuxController struct{}

func newPlatform() Controller { return &linuxController{} }

func (c *linuxController) Status() (Status, error) {
	return getLCDStatus(), nil
}

func (c *linuxController) Set(on bool) error {
	return setLCDPower(on)
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
func getLCDStatusWayland() (Status, bool) {
	status := Status{IsOn: true}

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

// getLCDStatus returns the current LCD display status.
func getLCDStatus() Status {
	status := Status{IsOn: true} // Default to on

	// Try backlight sysfs first (official Pi touchscreen)
	blPath := getBacklightPath()
	if blPath != "" {
		data, err := os.ReadFile(blPath)
		if err == nil {
			val := strings.TrimSpace(string(data))
			// bl_power: 0 = on, 1 = off
			if val == "1" {
				status.IsOn = false
			}
			log.Debug().Str("bl_power", val).Bool("isOn", status.IsOn).Msg("LCD status from backlight sysfs")
			return status
		}
	}

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

// getBacklightPath finds the sysfs backlight path for the official Pi DSI touchscreen.
// Returns empty string if not found.
func getBacklightPath() string {
	paths := []string{
		"/sys/class/backlight/rpi_backlight/bl_power",
		"/sys/class/backlight/10-0045/bl_power",
		"/sys/class/backlight/4-0045/bl_power",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// setLCDPowerBacklight controls the display backlight via sysfs.
// This only turns off the backlight — the touchscreen digitizer stays active,
// which means touch-to-wake works perfectly.
// bl_power: 0 = on, 1 = off (yes, it's inverted).
func setLCDPowerBacklight(on bool) error {
	blPath := getBacklightPath()
	if blPath == "" {
		return os.ErrNotExist
	}

	value := "1" // off
	if on {
		value = "0" // on
	}

	err := os.WriteFile(blPath, []byte(value), 0644)
	if err != nil {
		// Try with sudo if direct write fails
		cmd := exec.Command("sudo", "sh", "-c", "echo "+value+" > "+blPath)
		output, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			log.Error().Err(cmdErr).Str("output", string(output)).Bool("on", on).Msg("Backlight write failed")
			return cmdErr
		}
	}

	log.Info().Bool("on", on).Str("path", blPath).Msg("LCD power changed via backlight sysfs (touch stays active)")
	return nil
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

// setLCDPower turns the LCD display on or off using the one mechanism this
// host actually supports (see detectPowerMethod).
func setLCDPower(on bool) error {
	switch method := detectPowerMethod(); method {
	case methodBacklight:
		return setLCDPowerBacklight(on)

	case methodDPMS:
		return setLCDPowerDPMS(on)

	case methodVcgencmd:
		value := "0"
		if on {
			value = "1"
		}
		if err := exec.Command("vcgencmd", "display_power", value).Run(); err != nil {
			log.Error().Err(err).Bool("on", on).Msg("vcgencmd display_power failed")
			return err
		}
		log.Info().Bool("on", on).Msg("LCD power changed via vcgencmd")
		return nil

	case methodWayland:
		// Wake forces a modeset; standby is a plain --off. See
		// wakeWaylandForced for why the asymmetry matters.
		if on {
			return wakeWaylandForced()
		}
		return setLCDPowerWayland(false)

	default:
		log.Error().Bool("on", on).Msg("No usable LCD power method on this host")
		return os.ErrNotExist
	}
}

// sysfsIsWritableByAnyone reports whether a sysfs attribute has a write bit
// set for *anybody*.
//
// This is deliberately a mode check rather than an access check. The Pi 5
// exposes /sys/class/drm/card1-HDMI-A-1/dpms as r--r--r--, root included, so
// escalating to `sudo sh -c 'echo On > …'` cannot help — it just forks a
// process to produce "Permission denied" and an ERR line on every toggle.
// Checking our *own* access instead would wrongly skip hosts where the
// attribute is root-writable and the sudo fallback genuinely works.
func sysfsIsWritableByAnyone(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().Perm()&0o222 != 0
}

// powerMethod is the mechanism that actually moves this host's panel.
type powerMethod int

const (
	methodNone powerMethod = iota
	methodBacklight
	methodDPMS
	methodVcgencmd
	methodWayland
)

func (m powerMethod) String() string {
	switch m {
	case methodBacklight:
		return "backlight-sysfs"
	case methodDPMS:
		return "drm-dpms"
	case methodVcgencmd:
		return "vcgencmd"
	case methodWayland:
		return "wlr-randr"
	default:
		return "none"
	}
}

var (
	detectOnce     sync.Once
	detectedMethod powerMethod
)

// detectPowerMethod picks the one mechanism that works on this host, once.
//
// The old code tried all four on every call. On a Pi 5 driving an HDMI panel
// the first three are all dead — no DSI backlight, a read-only DPMS attribute,
// and firmware that dropped `vcgencmd display_power` ("Command not
// registered") — so every toggle forked a doomed sudo and logged an ERR before
// reaching the only method that works.
func detectPowerMethod() powerMethod {
	detectOnce.Do(func() {
		detectedMethod = probePowerMethod()
		log.Info().Str("method", detectedMethod.String()).Msg("LCD power method detected")
	})
	return detectedMethod
}

func probePowerMethod() powerMethod {
	if sysfsIsWritableByAnyone(getBacklightPath()) {
		return methodBacklight
	}
	if sysfsIsWritableByAnyone(getDRMDisplayPath()) {
		return methodDPMS
	}
	if err := exec.Command("vcgencmd", "display_power").Run(); err == nil {
		return methodVcgencmd
	}
	if isWaylandSession() {
		return methodWayland
	}
	return methodNone
}

// wakeWaylandForced re-enables the output *with an explicit mode set*.
//
// Load-bearing on the Pi's 1920x440 bar panel. `wlr-randr --on` against an
// output that is already enabled is a no-op at the KMS level: no modeset
// happens, so a panel that failed to re-lock after the last --off gets no
// fresh signal to lock onto and stays dark. The backend then truthfully
// reports isOn:true (wlr-randr says "Enabled: yes") while the user is staring
// at a black screen with no way to recover from the phone — tapping LCD just
// re-sends a no-op.
//
// Asking for --preferred alongside --on forces the modeset unconditionally,
// which is what makes wake a real recovery action rather than a status flip.
func wakeWaylandForced() error {
	cmd := exec.Command("wlr-randr", "--output", "HDMI-A-1", "--on", "--preferred")
	cmd.Env = getWaylandEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("output", string(output)).Msg("wlr-randr forced wake failed")
		return err
	}
	log.Info().Msg("LCD woken via wlr-randr with forced modeset")
	return nil
}
