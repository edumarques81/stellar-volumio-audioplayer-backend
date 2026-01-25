// Package socketio provides the Socket.io server for client communication.
package socketio

import (
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

// BitPerfectStatus represents the result of a bit-perfect configuration check.
type BitPerfectStatus struct {
	Status   string   `json:"status"`   // "ok", "warning", "error"
	Issues   []string `json:"issues"`   // Critical issues preventing bit-perfect
	Warnings []string `json:"warnings"` // Non-critical warnings
	Config   []string `json:"config"`   // Current configuration details
}

// PlaybackOption represents an audio output option.
type PlaybackOption struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

// PlaybackAttribute represents an attribute in playback options.
type PlaybackAttribute struct {
	Name    string           `json:"name"`
	Type    string           `json:"type"`
	Value   string           `json:"value"`
	Options []PlaybackOption `json:"options,omitempty"`
}

// PlaybackOptionsSection represents a section in playback options.
type PlaybackOptionsSection struct {
	ID         string              `json:"id"`
	Name       string              `json:"name,omitempty"`
	Attributes []PlaybackAttribute `json:"attributes"`
}

// PlaybackOptionsResponse represents the playback options response.
type PlaybackOptionsResponse struct {
	Options     []PlaybackOptionsSection `json:"options"`
	SystemCards []string                 `json:"systemCards"`
}

// DsdModeResponse represents the DSD playback mode.
type DsdModeResponse struct {
	Mode    string `json:"mode"`    // "native" or "dop"
	Success bool   `json:"success"` // true if operation succeeded (for setDsdMode)
	Error   string `json:"error"`   // error message if failed
}

// MixerModeResponse represents the mixer configuration.
type MixerModeResponse struct {
	Enabled bool   `json:"enabled"` // true if software mixer enabled
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ApplyBitPerfectResponse represents the result of applying all bit-perfect settings.
type ApplyBitPerfectResponse struct {
	Success bool     `json:"success"`
	Applied []string `json:"applied"` // Settings that were changed
	Errors  []string `json:"errors"`  // Any errors encountered
}

// GetPlaybackOptions returns available audio output devices.
func GetPlaybackOptions() PlaybackOptionsResponse {
	response := PlaybackOptionsResponse{
		Options:     []PlaybackOptionsSection{},
		SystemCards: []string{},
	}

	// Get list of sound cards using aplay -l
	out, err := exec.Command("aplay", "-l").Output()
	if err != nil {
		log.Error().Err(err).Msg("Failed to list audio devices")
		return response
	}

	var options []PlaybackOption
	var systemCards []string

	// Parse aplay -l output
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "card ") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) < 2 {
				continue
			}

			cardPart := strings.TrimSpace(parts[1])
			cardNameEnd := strings.Index(cardPart, " [")
			if cardNameEnd == -1 {
				cardNameEnd = strings.Index(cardPart, ",")
			}
			if cardNameEnd == -1 {
				continue
			}

			cardName := strings.TrimSpace(cardPart[:cardNameEnd])

			descStart := strings.Index(cardPart, "[")
			descEnd := strings.Index(cardPart, "]")
			description := cardName
			if descStart != -1 && descEnd != -1 && descEnd > descStart {
				description = cardPart[descStart+1 : descEnd]
			}

			friendlyName := description
			if strings.Contains(strings.ToLower(cardName), "hdmi") {
				friendlyName = "HDMI: " + description
			} else if strings.Contains(strings.ToLower(cardName), "usb") || strings.HasPrefix(strings.ToLower(cardName), "u20") {
				friendlyName = "USB: " + description
			}

			options = append(options, PlaybackOption{
				Value: cardName,
				Name:  friendlyName,
			})
			systemCards = append(systemCards, cardName)
		}
	}

	selectedDevice := GetCurrentAudioOutput()

	if selectedDevice == "" {
		for _, opt := range options {
			if strings.Contains(strings.ToLower(opt.Value), "usb") || strings.HasPrefix(strings.ToLower(opt.Value), "u20") {
				selectedDevice = opt.Value
				break
			}
		}
		if selectedDevice == "" && len(options) > 0 {
			selectedDevice = options[0].Value
		}
	}

	response.Options = []PlaybackOptionsSection{
		{
			ID:   "output",
			Name: "Audio Output",
			Attributes: []PlaybackAttribute{
				{
					Name:    "output_device",
					Type:    "select",
					Value:   selectedDevice,
					Options: options,
				},
			},
		},
	}
	response.SystemCards = systemCards

	log.Debug().Interface("options", options).Str("selected", selectedDevice).Msg("Playback options")
	return response
}

// GetCurrentAudioOutput reads the current audio output device from MPD config.
func GetCurrentAudioOutput() string {
	data, err := os.ReadFile("/etc/mpd.conf")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to read MPD config for audio output")
		return ""
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	inAudioOutput := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "audio_output") {
			inAudioOutput = true
			continue
		}

		if inAudioOutput {
			if trimmed == "}" {
				inAudioOutput = false
				continue
			}

			if strings.HasPrefix(trimmed, "device") {
				device := extractConfigValue(content, "device")
				if device != "" && strings.HasPrefix(device, "hw:") {
					cardNum := strings.TrimPrefix(device, "hw:")
					if idx := strings.Index(cardNum, ","); idx != -1 {
						cardNum = cardNum[:idx]
					}
					return getCardNameByNumber(cardNum)
				}
				return device
			}
		}
	}

	return ""
}

// getCardNameByNumber returns the card name for a given card number.
func getCardNameByNumber(cardNum string) string {
	out, err := exec.Command("aplay", "-l").Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "card "+cardNum+":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) < 2 {
				continue
			}
			cardPart := strings.TrimSpace(parts[1])
			cardNameEnd := strings.Index(cardPart, " [")
			if cardNameEnd == -1 {
				cardNameEnd = strings.Index(cardPart, ",")
			}
			if cardNameEnd != -1 {
				return strings.TrimSpace(cardPart[:cardNameEnd])
			}
		}
	}
	return ""
}

// SetPlaybackSettings changes the audio output device in MPD config.
func SetPlaybackSettings(deviceName string) error {
	cardNum := getCardNumberByName(deviceName)
	if cardNum == "" {
		return exec.ErrNotFound
	}

	data, err := os.ReadFile("/etc/mpd.conf")
	if err != nil {
		return err
	}

	content := string(data)
	newDevice := `"hw:` + cardNum + `,0"`

	lines := strings.Split(content, "\n")
	var newLines []string
	inAudioOutput := false
	foundDevice := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !strings.HasPrefix(trimmed, "#") {
			if strings.HasPrefix(trimmed, "audio_output") {
				inAudioOutput = true
			} else if inAudioOutput && trimmed == "}" {
				inAudioOutput = false
			} else if inAudioOutput && strings.HasPrefix(trimmed, "device") {
				line = `    device      ` + newDevice
				foundDevice = true
			}
		}
		newLines = append(newLines, line)
	}

	if !foundDevice {
		return exec.ErrNotFound
	}

	newContent := strings.Join(newLines, "\n")
	if err := writeMPDConfig(newContent); err != nil {
		return err
	}

	cmd := exec.Command("sudo", "systemctl", "restart", "mpd")
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD after changing audio output")
		return err
	}

	log.Info().Str("device", deviceName).Str("hwDevice", "hw:"+cardNum+",0").Msg("Audio output changed")
	return nil
}

// getCardNumberByName returns the card number for a given card name.
func getCardNumberByName(cardName string) string {
	out, err := exec.Command("aplay", "-l").Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "card ") && strings.Contains(line, cardName) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				numStr := strings.TrimSuffix(parts[1], ":")
				return numStr
			}
		}
	}
	return ""
}

// GetBitPerfectStatus checks bit-perfect audio configuration natively in Go.
func GetBitPerfectStatus() BitPerfectStatus {
	mpdConfig := ""
	if data, err := os.ReadFile("/etc/mpd.conf"); err == nil {
		mpdConfig = string(data)
	} else {
		log.Warn().Err(err).Msg("Failed to read MPD config")
	}

	alsaConfig := ""
	if data, err := os.ReadFile("/etc/asound.conf"); err == nil {
		alsaConfig = string(data)
	}

	aplayOutput := ""
	if out, err := exec.Command("aplay", "-l").Output(); err == nil {
		aplayOutput = string(out)
	}

	return CheckBitPerfectFromConfig(mpdConfig, alsaConfig, aplayOutput)
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

	// Check 3: Direct hardware output
	if strings.Contains(mpdConfig, `device`) && strings.Contains(mpdConfig, `"hw:`) {
		device := extractConfigValue(mpdConfig, "device")
		if device != "" && strings.HasPrefix(device, "hw:") {
			status.Config = append(status.Config, "MPD: Direct hardware output: "+device+" (good)")
		}
	} else if strings.Contains(mpdConfig, `device`) && strings.Contains(mpdConfig, `"volumio"`) {
		status.Issues = append(status.Issues, "MPD: Using 'volumio' device (goes through plug layer)")
	} else if mpdConfig != "" {
		status.Warnings = append(status.Warnings, "MPD: Could not determine audio device")
	}

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

// extractConfigValue extracts the value for a config setting.
func extractConfigValue(config, setting string) string {
	lines := strings.Split(config, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, setting) {
			rest := strings.TrimPrefix(line, setting)
			rest = strings.TrimSpace(rest)
			start := strings.Index(rest, `"`)
			if start != -1 {
				end := strings.Index(rest[start+1:], `"`)
				if end != -1 {
					return rest[start+1 : start+1+end]
				}
			}
		}
	}
	return ""
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

// writeMPDConfig writes the MPD config file using sudo to handle permissions.
func writeMPDConfig(content string) error {
	cmd := exec.Command("sudo", "tee", "/etc/mpd.conf")
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = nil
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// GetDsdMode returns the current DSD playback mode from MPD config.
func GetDsdMode() DsdModeResponse {
	response := DsdModeResponse{
		Mode:    "native",
		Success: true,
	}

	data, err := os.ReadFile("/etc/mpd.conf")
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		response.Error = "Failed to read MPD config"
		response.Success = false
		return response
	}

	content := string(data)
	if strings.Contains(content, `dop`) {
		if strings.Contains(content, `dop             "yes"`) || strings.Contains(content, `dop "yes"`) {
			response.Mode = "dop"
		}
	}

	return response
}

// SetDsdMode sets the DSD playback mode in MPD config and restarts MPD.
func SetDsdMode(mode string) DsdModeResponse {
	response := DsdModeResponse{
		Mode:    mode,
		Success: false,
	}

	if mode != "native" && mode != "dop" {
		response.Error = "Invalid mode. Must be 'native' or 'dop'"
		return response
	}

	data, err := os.ReadFile("/etc/mpd.conf")
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		response.Error = "Failed to read MPD config"
		return response
	}

	content := string(data)
	var newContent string

	dopValue := "no"
	if mode == "dop" {
		dopValue = "yes"
	}

	if strings.Contains(content, `dop             "yes"`) {
		newContent = strings.Replace(content, `dop             "yes"`, `dop             "`+dopValue+`"`, 1)
	} else if strings.Contains(content, `dop             "no"`) {
		newContent = strings.Replace(content, `dop             "no"`, `dop             "`+dopValue+`"`, 1)
	} else if strings.Contains(content, `dop "yes"`) {
		newContent = strings.Replace(content, `dop "yes"`, `dop "`+dopValue+`"`, 1)
	} else if strings.Contains(content, `dop "no"`) {
		newContent = strings.Replace(content, `dop "no"`, `dop "`+dopValue+`"`, 1)
	} else {
		response.Error = "Could not find dop setting in MPD config"
		return response
	}

	if err := writeMPDConfig(newContent); err != nil {
		log.Error().Err(err).Msg("Failed to write MPD config")
		response.Error = "Failed to write MPD config: " + err.Error()
		return response
	}

	cmd := exec.Command("sudo", "systemctl", "restart", "mpd")
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD")
		response.Error = "Config updated but failed to restart MPD: " + err.Error()
		return response
	}

	log.Info().Str("mode", mode).Msg("DSD mode changed successfully")
	response.Success = true
	return response
}

// GetMixerMode returns whether software mixer is enabled.
func GetMixerMode() MixerModeResponse {
	response := MixerModeResponse{
		Enabled: false,
		Success: true,
	}

	data, err := os.ReadFile("/etc/mpd.conf")
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
	response := MixerModeResponse{
		Enabled: enabled,
		Success: false,
	}

	data, err := os.ReadFile("/etc/mpd.conf")
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

	cmd := exec.Command("sudo", "systemctl", "restart", "mpd")
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD")
		response.Error = "Config updated but failed to restart MPD: " + err.Error()
		return response
	}

	log.Info().Bool("enabled", enabled).Msg("Mixer mode changed successfully")
	response.Success = true
	return response
}

// ApplyBitPerfect applies all optimal bit-perfect settings to MPD config.
func ApplyBitPerfect() ApplyBitPerfectResponse {
	response := ApplyBitPerfectResponse{
		Success: false,
		Applied: []string{},
		Errors:  []string{},
	}

	data, err := os.ReadFile("/etc/mpd.conf")
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

	cmd := exec.Command("sudo", "systemctl", "restart", "mpd")
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD")
		response.Errors = append(response.Errors, "Config updated but failed to restart MPD: "+err.Error())
		return response
	}

	log.Info().Strs("applied", response.Applied).Msg("Bit-perfect settings applied successfully")
	response.Success = true
	return response
}

// BroadcastAudioStatus sends audio status to all connected clients.
func (s *Server) BroadcastAudioStatus() {
	status := s.audioController.GetStatus()
	s.io.Emit("pushAudioStatus", status)
	log.Debug().Bool("locked", status.Locked).Interface("format", status.Format).Msg("Broadcast audio status")
}
