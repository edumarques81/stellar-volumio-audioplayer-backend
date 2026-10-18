// Bit-perfect configuration DETECTION — read-only. Nothing here mutates
// /etc/mpd.conf or /etc/asound.conf; the write path is bitperfect_apply.go.
package socketio

import (
	"github.com/rs/zerolog/log"
	"os"
	"os/exec"
	"strings"
)

// GetBitPerfectStatus checks bit-perfect audio configuration natively in Go.
func GetBitPerfectStatus() BitPerfectStatus {
	mpdConfig := ""
	mpdConfigErr := ""
	if data, err := os.ReadFile(mpdConfigPath); err == nil {
		mpdConfig = string(data)
	} else {
		log.Warn().Err(err).Msg("Failed to read MPD config")
		mpdConfigErr = err.Error()
	}

	alsaConfig := ""
	if data, err := os.ReadFile("/etc/asound.conf"); err == nil {
		alsaConfig = string(data)
	}

	aplayOutput := ""
	if out, err := exec.Command("aplay", "-l").Output(); err == nil {
		aplayOutput = string(out)
	}

	status := CheckBitPerfectFromConfig(mpdConfig, alsaConfig, aplayOutput)

	// A log line is not enough here. /etc/mpd.conf is mpd:audio 0640 and this
	// process reads it only via its membership of group `audio`, so a root-side
	// rewrite that resets the owner takes the read away while MPD itself keeps
	// starting normally. Without this the panel renders with no MPD rows at
	// all, which reads as "no problems found" when in fact nothing was checked.
	if mpdConfigErr != "" {
		status.Issues = append(status.Issues,
			"Could not read "+mpdConfigPath+" ("+mpdConfigErr+
				") - MPD settings below are unverified")
		status.Status = "error"
	}

	return status
}

// CheckBitPerfectFromConfig checks bit-perfect configuration from config strings.
func CheckBitPerfectFromConfig(mpdConfig, alsaConfig, aplayOutput string) BitPerfectStatus {
	status := BitPerfectStatus{
		Status:   "ok",
		Issues:   []string{},
		Warnings: []string{},
		Config:   []string{},
	}

	// Check 1: MPD resampler
	if strings.Contains(mpdConfig, "resampler") {
		if strings.Contains(mpdConfig, "plugin") && (strings.Contains(mpdConfig, "soxr") || strings.Contains(mpdConfig, "libsamplerate")) {
			status.Issues = append(status.Issues, "MPD: Resampler is enabled - audio will be resampled")
		}
	} else {
		status.Config = append(status.Config, "MPD: No resampler configured (good)")
	}

	// Check 2: Volume normalization
	if strings.Contains(mpdConfig, `volume_normalization`) && strings.Contains(mpdConfig, `"yes"`) {
		if matchConfigValue(mpdConfig, "volume_normalization", "yes") {
			status.Issues = append(status.Issues, "MPD: Volume normalization is enabled - audio will be modified")
		}
	} else {
		status.Config = append(status.Config, "MPD: Volume normalization disabled (good)")
	}

	// Check 3: Output device transparency
	appendOutputDeviceStatus(&status, mpdConfig, alsaConfig)

	// Check 4: Auto conversion settings
	for _, setting := range []string{"auto_resample", "auto_format", "auto_channels"} {
		if matchConfigValue(mpdConfig, setting, "no") {
			status.Config = append(status.Config, setting+": disabled (good)")
		} else if matchConfigValue(mpdConfig, setting, "yes") {
			status.Issues = append(status.Issues, setting+": enabled - audio may be converted")
		}
	}

	// Check 5: DSD playback mode (native vs DoP)
	if matchConfigValue(mpdConfig, "dop", "yes") {
		status.Warnings = append(status.Warnings, "DSD over PCM (DoP): enabled - consider native DSD for true bit-perfect")
	} else if matchConfigValue(mpdConfig, "dop", "no") {
		status.Config = append(status.Config, "DSD: Native DSD mode (DoP disabled) - true bit-perfect DSD")
	} else if mpdConfig != "" {
		status.Config = append(status.Config, "DSD: DoP not configured (native DSD assumed)")
	}

	// Check 6: Mixer type
	if matchConfigValue(mpdConfig, "mixer_type", "none") {
		status.Config = append(status.Config, "Mixer: disabled (bit-perfect volume)")
	} else if matchConfigValue(mpdConfig, "mixer_type", "software") {
		status.Warnings = append(status.Warnings, "Mixer: software mixing enabled (not bit-perfect)")
	}

	// Check 7: ALSA config
	if alsaConfig != "" {
		if strings.Contains(alsaConfig, "type") && strings.Contains(alsaConfig, "plug") {
			status.Warnings = append(status.Warnings, "ALSA: 'plug' type detected - may convert formats")
		}
		if strings.Contains(alsaConfig, "type") && strings.Contains(alsaConfig, "hw") {
			status.Config = append(status.Config, "ALSA: Direct hardware access configured (good)")
		}
	}

	// Check 8: USB DAC presence (Singxer SU-6)
	if aplayOutput != "" {
		if strings.Contains(aplayOutput, "U20SU6") || strings.Contains(aplayOutput, "SU-6") || strings.Contains(aplayOutput, "SU6") {
			status.Config = append(status.Config, "Hardware: Singxer SU-6 detected (native DSD capable)")
		} else {
			status.Warnings = append(status.Warnings, "Hardware: Singxer SU-6 not detected")
		}
	}

	// Check 9: VU-meter tap in MPD's output path
	appendVuTapStatus(&status, mpdConfig, alsaConfig)

	// Determine overall status
	if len(status.Issues) > 0 {
		status.Status = "error"
	} else if len(status.Warnings) > 0 {
		status.Status = "warning"
	} else {
		status.Status = "ok"
	}

	return status
}

// matchConfigValue checks if a config setting has a specific value.
func matchConfigValue(config, setting, value string) bool {
	lines := strings.Split(config, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, setting) {
			rest := strings.TrimPrefix(line, setting)
			rest = strings.TrimSpace(rest)
			expectedValue := `"` + value + `"`
			if strings.HasPrefix(rest, expectedValue) || rest == expectedValue {
				return true
			}
		}
	}
	return false
}

// NormalizeBitPerfectStatus converts script status values to frontend expected values.
func NormalizeBitPerfectStatus(status BitPerfectStatus) BitPerfectStatus {
	switch status.Status {
	case "bit-perfect":
		status.Status = "ok"
	case "not-bit-perfect":
		status.Status = "error"
	}
	return status
}
