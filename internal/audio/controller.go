// Package audio provides audio format detection and device lock status.
package audio

import (
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// AudioFormat represents the current audio output format.
type AudioFormat struct {
	SampleRate   int    `json:"sampleRate"`   // Sample rate in Hz (44100, 96000, 192000, etc.)
	BitDepth     int    `json:"bitDepth"`     // Bit depth (16, 24, 32)
	Channels     int    `json:"channels"`     // Number of channels (usually 2)
	Format       string `json:"format"`       // Format string ("PCM", "DSD64", "DSD128", etc.)
	IsBitPerfect bool   `json:"isBitPerfect"` // True if bit-perfect output (no resampling)
}

// AudioStatus represents the current audio output status.
type AudioStatus struct {
	Locked bool         `json:"locked"` // True if device is locked for exclusive playback
	Format *AudioFormat `json:"format"` // Current audio format (nil if not playing)
}

// Controller manages audio format detection and device lock status.
type Controller struct {
	mu           sync.RWMutex
	isLocked     bool
	currentFormat *AudioFormat
	bitPerfect   bool // Configuration flag for bit-perfect mode
}

// NewController creates a new audio controller.
func NewController(bitPerfect bool) *Controller {
	return &Controller{
		bitPerfect: bitPerfect,
	}
}

// GetStatus returns the current audio status.
func (c *Controller) GetStatus() AudioStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return AudioStatus{
		Locked: c.isLocked,
		Format: c.currentFormat,
	}
}

// UpdateFromMPDStatus updates audio status from MPD status fields.
// mpdState is the playback state ("play", "pause", "stop")
// audio is the MPD audio field format "samplerate:bits:channels" (e.g., "192000:24:2")
func (c *Controller) UpdateFromMPDStatus(mpdState, audio string) (changed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Determine lock state based on playback
	wasLocked := c.isLocked
	c.isLocked = mpdState == "play"

	// Parse audio format
	var newFormat *AudioFormat
	if audio != "" {
		newFormat = c.parseAudioFormat(audio)
	}

	// Check if format changed
	formatChanged := !audioFormatEqual(c.currentFormat, newFormat)
	c.currentFormat = newFormat

	changed = (wasLocked != c.isLocked) || formatChanged

	if changed {
		log.Debug().
			Bool("locked", c.isLocked).
			Interface("format", c.currentFormat).
			Msg("Audio status changed")
	}

	return changed
}

// OnPlaybackStart marks the device as locked.
func (c *Controller) OnPlaybackStart() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isLocked = true
}

// OnPlaybackStop releases the device lock.
func (c *Controller) OnPlaybackStop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isLocked = false
}

// parseAudioFormat parses MPD's audio format string.
// Format: "samplerate:bits:channels" (e.g., "192000:24:2")
// DSD is indicated by special sample rates (DSD64 = 2822400 Hz, etc.)
func (c *Controller) parseAudioFormat(audio string) *AudioFormat {
	parts := strings.Split(audio, ":")
	if len(parts) < 2 {
		return nil
	}

	sampleRate, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil
	}

	bitDepth, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil
	}

	channels := 2 // Default to stereo
	if len(parts) >= 3 {
		if ch, err := strconv.Atoi(parts[2]); err == nil {
			channels = ch
		}
	}

	format := &AudioFormat{
		SampleRate:   sampleRate,
		BitDepth:     bitDepth,
		Channels:     channels,
		IsBitPerfect: c.bitPerfect,
	}

	// Determine format string
	format.Format = detectAudioFormatType(sampleRate, bitDepth)

	return format
}

// detectAudioFormatType returns a human-readable format type.
func detectAudioFormatType(sampleRate, bitDepth int) string {
	// DSD detection based on sample rate
	// DSD64 = 2822400 Hz (64x CD rate)
	// DSD128 = 5644800 Hz
	// DSD256 = 11289600 Hz
	// DSD512 = 22579200 Hz
	switch sampleRate {
	case 2822400:
		return "DSD64"
	case 5644800:
		return "DSD128"
	case 11289600:
		return "DSD256"
	case 22579200:
		return "DSD512"
	default:
		// PCM format
		return "PCM"
	}
}

// FormatSampleRate returns a human-readable sample rate string.
func FormatSampleRate(sampleRate int) string {
	if sampleRate >= 1000000 {
		// DSD rates - show as DSD multiplier
		return detectAudioFormatType(sampleRate, 0)
	}
	if sampleRate >= 1000 {
		return strconv.FormatFloat(float64(sampleRate)/1000, 'f', -1, 64) + "kHz"
	}
	return strconv.Itoa(sampleRate) + "Hz"
}

// FormatBitDepth returns a human-readable bit depth string.
func FormatBitDepth(bitDepth int) string {
	return strconv.Itoa(bitDepth) + "-bit"
}

// audioFormatEqual compares two AudioFormat pointers for equality.
func audioFormatEqual(a, b *AudioFormat) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.SampleRate == b.SampleRate &&
		a.BitDepth == b.BitDepth &&
		a.Channels == b.Channels &&
		a.Format == b.Format &&
		a.IsBitPerfect == b.IsBitPerfect
}
