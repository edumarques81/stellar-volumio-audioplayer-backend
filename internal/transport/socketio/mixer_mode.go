// Software-mixer mode. `mixer_type "none"` is load-bearing for bit-perfect
// playback, so enabling the mixer is a deliberate, user-driven regression.
package socketio

import (
	"github.com/rs/zerolog/log"
	"os"
	"regexp"
	"strings"
)

// GetMixerMode returns whether software mixer is enabled.
func GetMixerMode() MixerModeResponse {
	response := MixerModeResponse{
		Enabled: false,
		Success: true,
	}

	data, err := os.ReadFile(mpdConfigPath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		response.Error = "Failed to read MPD config"
		response.Success = false
		return response
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "mixer_type") {
			if strings.Contains(trimmed, `"software"`) {
				response.Enabled = true
			}
			break
		}
	}

	return response
}

// SetMixerMode enables or disables the software mixer in MPD config and restarts MPD.
func SetMixerMode(enabled bool) MixerModeResponse {
	// Serialised against every other rewrite of this file; see mpdConfigMu.
	mpdConfigMu.Lock()
	defer mpdConfigMu.Unlock()
	response := MixerModeResponse{
		Enabled: enabled,
		Success: false,
	}

	data, err := os.ReadFile(mpdConfigPath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		response.Error = "Failed to read MPD config"
		return response
	}

	content := string(data)

	mixerValue := "none"
	if enabled {
		mixerValue = "software"
	}

	re := regexp.MustCompile(`(mixer_type\s+)"(?:software|none)"`)
	if !re.MatchString(content) {
		response.Error = "Could not find mixer_type setting in MPD config"
		return response
	}
	newContent := re.ReplaceAllString(content, `${1}"`+mixerValue+`"`)

	if err := writeMPDConfig(newContent); err != nil {
		log.Error().Err(err).Msg("Failed to write MPD config")
		response.Error = "Failed to write MPD config: " + err.Error()
		return response
	}

	if err := restartMPD(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD")
		response.Error = "Config updated but failed to restart MPD: " + err.Error()
		return response
	}

	log.Info().Bool("enabled", enabled).Msg("Mixer mode changed successfully")
	response.Success = true
	return response
}
