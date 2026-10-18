// ALSA device enumeration and the `device` line in /etc/mpd.conf.
//
// deviceKeyRe lives here rather than in alsa_pcm.go because this is the file
// that owns reading and rewriting that key; alsa_pcm.go only matches against it.
package socketio

import (
	"github.com/rs/zerolog/log"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// deviceKeyRe matches the `device` setting key, without also matching a longer
// key that merely starts with it.
var deviceKeyRe = regexp.MustCompile(`^device[ \t]`)

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

// GetCurrentAudioOutput reports the audio output card MPD is configured to use,
// as a card name matching the values GetPlaybackOptions offers.
func GetCurrentAudioOutput() string {
	data, err := os.ReadFile(mpdConfigPath)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to read MPD config for audio output")
		return ""
	}

	out, err := exec.Command("aplay", "-l").Output()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to list ALSA cards for audio output")
	}

	return resolveCurrentAudioOutput(string(data), readAlsaConfig(), string(out))
}

// SetPlaybackSettings changes the audio output device in MPD config.
func SetPlaybackSettings(deviceName string) error {
	// Resolved before the lock: getCardNumberByName shells out to `aplay -l`,
	// and a slow or wedged aplay must not block every other config path.
	cardNum := getCardNumberByName(deviceName)
	if cardNum == "" {
		return exec.ErrNotFound
	}

	// Serialised against every other rewrite of this file; see mpdConfigMu.
	mpdConfigMu.Lock()
	defer mpdConfigMu.Unlock()

	data, err := os.ReadFile(mpdConfigPath)
	if err != nil {
		return err
	}

	content := string(data)

	// NOT `hw:<cardNum>,0`. On this appliance the DAC sits behind our extplug
	// tap, and writing the bare card silently takes the tap out of MPD's path:
	// nothing then writes the spectrum FIFO, the LCD VU needles park on the
	// left end stop, and no error is raised anywhere. The selector's wire
	// contract is a card name, which cannot express "the tap in front of it",
	// so the right device has to be derived here.
	// -> deploy/README-alsa-tap.md
	deviceValue := resolveOutputDeviceValue(cardNum, readAlsaConfig())
	newDevice := `"` + deviceValue + `"`

	// Scoped with the same block finder the DSD helpers use. The previous
	// `strings.HasPrefix(trimmed, "audio_output")` also matched the top-level
	// `audio_output_format` directive, which flipped block tracking on outside
	// any block and put the next `device` line it met at risk.
	lines := strings.Split(content, "\n")
	open, closeIdx, ok := findAudioOutputBlock(lines)
	if !ok {
		return exec.ErrNotFound
	}

	foundDevice := false
	for i := open + 1; i < closeIdx; i++ {
		bare, cr := splitCR(lines[i])
		trimmed := strings.TrimSpace(bare)
		if strings.HasPrefix(trimmed, "#") || !deviceKeyRe.MatchString(trimmed) {
			continue
		}
		lines[i] = `    device      ` + newDevice + cr
		foundDevice = true
		break
	}

	if !foundDevice {
		return exec.ErrNotFound
	}

	newContent := strings.Join(lines, "\n")
	if err := writeMPDConfig(newContent); err != nil {
		return err
	}

	if err := restartMPD(); err != nil {
		log.Error().Err(err).Msg("Failed to restart MPD after changing audio output")
		return err
	}

	log.Info().Str("device", deviceName).Str("alsaDevice", deviceValue).Msg("Audio output changed")
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
