// Bit-perfect configuration WRITE path: applies every setting at once and
// reports what it changed. The detection side is bitperfect_status.go.
package socketio

import (
	"github.com/rs/zerolog/log"
	"os"
	"regexp"
)

// ApplyBitPerfect applies all optimal bit-perfect settings to MPD config.
func ApplyBitPerfect() ApplyBitPerfectResponse {
	// Serialised against every other rewrite of this file; see mpdConfigMu.
	mpdConfigMu.Lock()
	defer mpdConfigMu.Unlock()
	response := ApplyBitPerfectResponse{
		Success: false,
		Applied: []string{},
		Errors:  []string{},
	}

	data, err := os.ReadFile(mpdConfigPath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		response.Errors = append(response.Errors, "Failed to read MPD config")
		return response
	}

	content := string(data)
	newContent := content

	settingsToApply := []struct {
		name        string
		pattern     string
		replacement string
		checkOk     string
	}{
		{
			name:        "mixer_type",
			pattern:     `(mixer_type\s+)"software"`,
			replacement: `${1}"none"`,
			checkOk:     `mixer_type\s+"none"`,
		},
		{
			name:        "auto_resample",
			pattern:     `(auto_resample\s+)"yes"`,
			replacement: `${1}"no"`,
			checkOk:     `auto_resample\s+"no"`,
		},
		{
			name:        "auto_format",
			pattern:     `(auto_format\s+)"yes"`,
			replacement: `${1}"no"`,
			checkOk:     `auto_format\s+"no"`,
		},
		{
			name:        "auto_channels",
			pattern:     `(auto_channels\s+)"yes"`,
			replacement: `${1}"no"`,
			checkOk:     `auto_channels\s+"no"`,
		},
	}

	for _, setting := range settingsToApply {
		re := regexp.MustCompile(setting.pattern)
		if re.MatchString(newContent) {
			newContent = re.ReplaceAllString(newContent, setting.replacement)
			response.Applied = append(response.Applied, setting.name+" = bit-perfect")
		}
	}

	if len(response.Applied) == 0 {
		for _, setting := range settingsToApply {
			re := regexp.MustCompile(setting.checkOk)
			if re.MatchString(content) {
				response.Applied = append(response.Applied, setting.name+" already set to optimal")
			}
		}
		response.Success = true
		log.Info().Strs("applied", response.Applied).Msg("Bit-perfect settings already optimal")
		return response
	}

	if err := writeMPDConfig(newContent); err != nil {
		log.Error().Err(err).Msg("Failed to write MPD config")
		response.Errors = append(response.Errors, "Failed to write MPD config: "+err.Error())
		return response
	}

	if err := restartMPD(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD")
		response.Errors = append(response.Errors, "Config updated but failed to restart MPD: "+err.Error())
		return response
	}

	log.Info().Strs("applied", response.Applied).Msg("Bit-perfect settings applied successfully")
	response.Success = true
	return response
}
