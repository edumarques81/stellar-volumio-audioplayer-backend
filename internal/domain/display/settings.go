package display

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Settings are the user-managed schedules. They persist as JSON next to the
// other backend state so a reboot does not silently drop them.
//
// Defaults encode the behaviour that was asked for: the panel turns itself
// fully off at 22:00 when nothing has been played for an hour, and comes back
// at 05:00.
type Settings struct {
	AutoOffEnabled     bool   `json:"autoOffEnabled"`
	AutoOffAt          string `json:"autoOffAt"`
	AutoOffIdleMinutes int    `json:"autoOffIdleMinutes"`
	AutoRestoreEnabled bool   `json:"autoRestoreEnabled"`
	AutoRestoreAt      string `json:"autoRestoreAt"`
}

// DefaultSettings returns the shipped schedule.
func DefaultSettings() Settings {
	return Settings{
		AutoOffEnabled:     true,
		AutoOffAt:          "22:00",
		AutoOffIdleMinutes: 60,
		AutoRestoreEnabled: true,
		AutoRestoreAt:      "05:00",
	}
}

// Normalize repairs anything a client sent that cannot be acted on, rather
// than rejecting the whole update. An unparsable time or an out-of-range
// interval falls back to the default for that one field; the alternative is
// a schedule that silently never fires.
func (s Settings) Normalize() Settings {
	d := DefaultSettings()
	out := s

	if _, ok := parseClock(out.AutoOffAt); !ok {
		out.AutoOffAt = d.AutoOffAt
	}
	if _, ok := parseClock(out.AutoRestoreAt); !ok {
		out.AutoRestoreAt = d.AutoRestoreAt
	}
	// 0 would mean "the moment playback stops", which combined with a time
	// window is almost certainly not what anyone wants; 24h is the ceiling.
	if out.AutoOffIdleMinutes < 1 || out.AutoOffIdleMinutes > 1440 {
		out.AutoOffIdleMinutes = d.AutoOffIdleMinutes
	}
	return out
}

// parseClock converts "HH:MM" to minutes since midnight.
func parseClock(s string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, false
	}
	hh, err := strconv.Atoi(parts[0])
	if err != nil || hh < 0 || hh > 23 {
		return 0, false
	}
	mm, err := strconv.Atoi(parts[1])
	if err != nil || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

// LoadSettings reads path, falling back to the defaults when it does not
// exist yet. A corrupt file is an error: overwriting a user's schedule
// because it failed to parse is worse than refusing to start with it.
func LoadSettings(path string) (Settings, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-controlled path
	if os.IsNotExist(err) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return DefaultSettings(), fmt.Errorf("display: read settings: %w", err)
	}

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return DefaultSettings(), fmt.Errorf("display: parse settings: %w", err)
	}
	return s.Normalize(), nil
}

// SaveSettings writes path atomically enough for a single-writer service.
func SaveSettings(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("display: create settings dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("display: marshal settings: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("display: write settings: %w", err)
	}
	return nil
}
