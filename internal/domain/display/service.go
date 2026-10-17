package display

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Hardware is the slice of the LCD controller this package needs. Narrow on
// purpose: the schedules must never reach for anything else on the panel.
type Hardware interface {
	Set(on bool) error
	Status() (bool, error)
}

// Playback reports whether audio is currently running, from whichever source.
type Playback interface {
	IsPlaying() bool
}

// Notifier is called after the service changes mode by itself, so the
// transport layer can push the new state to every client. It is not called
// for client-initiated changes — the caller already knows, and broadcasts.
type Notifier func(Status)

// TickInterval is how often the schedules are evaluated. The rules have
// minute resolution, so this only has to be comfortably under a minute.
const TickInterval = 30 * time.Second

// Service owns the current mode, the schedules, and the goroutine that
// applies them.
type Service struct {
	mu             sync.RWMutex
	mode           Mode
	settings       Settings
	lastActivity   time.Time
	autoRestoredOn string

	settingsPath string
	hw           Hardware
	playback     Playback
	notify       Notifier

	// now is swappable so the schedules can be tested without waiting for
	// 22:00 to come around.
	now func() time.Time
}

// NewService returns a service seeded from the panel's own state. Settings
// load failures are logged and the defaults used: a broken settings file must
// not stop the backend from serving music.
func NewService(settingsPath string, hw Hardware, playback Playback) *Service {
	settings, err := LoadSettings(settingsPath)
	if err != nil {
		log.Error().Err(err).Str("path", settingsPath).Msg("display: using default schedule")
	}

	// The kiosk boots live, but the backend can be restarted on its own while
	// the panel is off. Assuming live there would make the next "turn it on"
	// a no-op against a mode we already believe, leaving the screen dark with
	// no way back short of another restart.
	mode := ModeLive
	if on, statusErr := hw.Status(); statusErr == nil && !on {
		mode = ModeOff
	}

	return &Service{
		mode:         mode,
		settings:     settings,
		lastActivity: time.Now(),
		settingsPath: settingsPath,
		hw:           hw,
		playback:     playback,
		now:          time.Now,
	}
}

// SetNotifier registers the broadcast callback. Separate from the
// constructor because the transport layer is built after this service.
func (s *Service) SetNotifier(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notify = n
}

// Mode reads the current mode.
func (s *Service) Mode() Mode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

// Status reports the mode together with the hardware's own answer. The two
// are read separately on purpose — see the note on Status.
func (s *Service) Status() Status {
	mode := s.Mode()
	isOn, err := s.hw.Status()
	if err != nil {
		// Unsupported platform or a failed read: fall back to what the mode
		// says, which is still better than claiming the panel is off.
		isOn = mode.Lit()
	}
	return Status{Mode: mode, IsOn: isOn}
}

// Settings reads the current schedule.
func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// UpdateSettings normalizes, stores and persists a new schedule, returning
// what was actually applied. A failed write is reported but not fatal: the
// in-memory schedule is live either way.
func (s *Service) UpdateSettings(next Settings) (Settings, error) {
	applied := next.Normalize()

	s.mu.Lock()
	s.settings = applied
	path := s.settingsPath
	s.mu.Unlock()

	if err := SaveSettings(path, applied); err != nil {
		return applied, err
	}
	return applied, nil
}

// MarkActivity records that someone touched the panel. The kiosk reports
// this; it is what stops the night schedule from blacking out a screen
// somebody is actually using at 23:00.
func (s *Service) MarkActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActivity = s.now()
}

// SetMode applies a mode change and drives the hardware. A change that only
// moves between live and standby leaves the panel lit and cannot fail.
func (s *Service) SetMode(m Mode) error {
	s.mu.Lock()
	previous := s.mode
	s.mode = m
	s.lastActivity = s.now()
	s.mu.Unlock()

	if !s.hardwareNeedsChange(previous, m) {
		return nil
	}
	if err := s.hw.Set(m.Lit()); err != nil {
		// Roll back: a mode the panel did not actually enter is worse than
		// a refused request, because every client would then be told the
		// screen is off while it is plainly on.
		s.mu.Lock()
		s.mode = previous
		s.mu.Unlock()
		return err
	}
	return nil
}

// hardwareNeedsChange decides whether the panel itself has to be touched.
// Moving between live and standby never does: both are lit. Repeating the
// mode we are already in normally does not either -- unless the panel
// disagrees with us, which is exactly the drift a backend restart leaves
// behind, and the case where a silent no-op would strand the screen dark.
func (s *Service) hardwareNeedsChange(previous, next Mode) bool {
	if previous.Lit() != next.Lit() {
		return true
	}
	on, err := s.hw.Status()
	if err != nil {
		return false
	}
	return on != next.Lit()
}

// Tick evaluates both schedules once. Returns the mode it moved to, and
// whether it moved at all. Exported so tests can drive time directly.
func (s *Service) Tick(now time.Time) (Mode, bool) {
	s.mu.RLock()
	conditions := Conditions{
		Now:            now,
		Mode:           s.mode,
		Playing:        s.playback != nil && s.playback.IsPlaying(),
		LastActivity:   s.lastActivity,
		AutoRestoredOn: s.autoRestoredOn,
	}
	settings := s.settings
	s.mu.RUnlock()

	// Playback counts as activity for as long as it lasts, so the idle
	// interval starts when the music stops rather than when it started.
	if conditions.Playing {
		s.MarkActivity()
		conditions.LastActivity = now
	}

	switch {
	case ShouldAutoOff(conditions, settings):
		if err := s.SetMode(ModeOff); err != nil {
			log.Error().Err(err).Msg("display: scheduled auto-off failed")
			return conditions.Mode, false
		}
		log.Info().Time("at", now).Msg("display: auto-off")
		s.broadcast()
		return ModeOff, true

	case ShouldAutoRestore(conditions, settings):
		// Restoring to standby rather than live: at 05:00 nothing is
		// playing, and a clock face is the gentler thing to wake up to than
		// a fully lit idle player.
		if err := s.SetMode(ModeStandby); err != nil {
			log.Error().Err(err).Msg("display: scheduled auto-restore failed")
			return conditions.Mode, false
		}
		s.mu.Lock()
		s.autoRestoredOn = now.Format("2006-01-02")
		s.mu.Unlock()
		log.Info().Time("at", now).Msg("display: auto-restore")
		s.broadcast()
		return ModeStandby, true
	}

	return conditions.Mode, false
}

func (s *Service) broadcast() {
	s.mu.RLock()
	notify := s.notify
	s.mu.RUnlock()
	if notify != nil {
		notify(s.Status())
	}
}

// Run evaluates the schedules until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.Tick(now)
		}
	}
}
