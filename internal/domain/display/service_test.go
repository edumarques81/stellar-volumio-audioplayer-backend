package display

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeHW records what the schedules did to the panel.
type fakeHW struct {
	mu   sync.Mutex
	on   bool
	sets []bool
	err  error
}

func (f *fakeHW) Set(on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.on = on
	f.sets = append(f.sets, on)
	return nil
}

func (f *fakeHW) Status() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.on, nil
}

func (f *fakeHW) calls() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.sets...)
}

type fakePlayback struct{ playing bool }

func (f *fakePlayback) IsPlaying() bool { return f.playing }

func newTestService(t *testing.T, hw Hardware, pb Playback) *Service {
	t.Helper()
	return NewService(filepath.Join(t.TempDir(), "display.json"), hw, pb)
}

func TestNewServiceStartsLive(t *testing.T) {
	s := newTestService(t, &fakeHW{on: true}, &fakePlayback{})
	if got := s.Mode(); got != ModeLive {
		t.Fatalf("Mode() = %q, want %q", got, ModeLive)
	}
	if got := s.Settings(); got != DefaultSettings() {
		t.Fatalf("Settings() = %+v, want defaults", got)
	}
}

func TestSetModeDrivesHardwareOnlyWhenLitChanges(t *testing.T) {
	hw := &fakeHW{on: true}
	s := newTestService(t, hw, &fakePlayback{})

	// live -> standby is painted by the kiosk; the panel must stay lit.
	if err := s.SetMode(ModeStandby); err != nil {
		t.Fatalf("SetMode(standby) returned %v", err)
	}
	if calls := hw.calls(); len(calls) != 0 {
		t.Fatalf("standby touched the hardware: %v", calls)
	}

	if err := s.SetMode(ModeOff); err != nil {
		t.Fatalf("SetMode(off) returned %v", err)
	}
	if calls := hw.calls(); len(calls) != 1 || calls[0] != false {
		t.Fatalf("hardware calls = %v, want [false]", calls)
	}

	if err := s.SetMode(ModeLive); err != nil {
		t.Fatalf("SetMode(live) returned %v", err)
	}
	if calls := hw.calls(); len(calls) != 2 || calls[1] != true {
		t.Fatalf("hardware calls = %v, want [false true]", calls)
	}
}

func TestSetModeIsIdempotent(t *testing.T) {
	hw := &fakeHW{on: true}
	s := newTestService(t, hw, &fakePlayback{})
	if err := s.SetMode(ModeLive); err != nil {
		t.Fatalf("SetMode() returned %v", err)
	}
	if calls := hw.calls(); len(calls) != 0 {
		t.Fatalf("a redundant SetMode touched the hardware: %v", calls)
	}
}

func TestSetModeRollsBackOnHardwareFailure(t *testing.T) {
	hw := &fakeHW{on: true, err: errors.New("wlr-randr: no such output")}
	s := newTestService(t, hw, &fakePlayback{})

	if err := s.SetMode(ModeOff); err == nil {
		t.Fatal("SetMode() hid a hardware failure")
	}
	// Reporting "off" for a panel that is still lit would strand every
	// client on a wrong state with no way to notice.
	if got := s.Mode(); got != ModeLive {
		t.Fatalf("Mode() = %q after a failed transition, want %q", got, ModeLive)
	}
}

func TestStatusReportsHardwareTruthAlongsideMode(t *testing.T) {
	hw := &fakeHW{on: true}
	s := newTestService(t, hw, &fakePlayback{})
	if err := s.SetMode(ModeStandby); err != nil {
		t.Fatal(err)
	}

	got := s.Status()
	if got.Mode != ModeStandby || !got.IsOn {
		t.Fatalf("Status() = %+v, want {standby true}", got)
	}
}

func TestUpdateSettingsNormalizesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "display.json")
	s := NewService(path, &fakeHW{on: true}, &fakePlayback{})

	applied, err := s.UpdateSettings(Settings{
		AutoOffEnabled: true, AutoOffAt: "nope", AutoOffIdleMinutes: 45,
		AutoRestoreEnabled: true, AutoRestoreAt: "06:00",
	})
	if err != nil {
		t.Fatalf("UpdateSettings() returned %v", err)
	}
	if applied.AutoOffAt != DefaultSettings().AutoOffAt {
		t.Fatalf("bad time was not repaired: %+v", applied)
	}
	if applied.AutoOffIdleMinutes != 45 {
		t.Fatalf("valid field was clobbered: %+v", applied)
	}

	reloaded, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings() returned %v", err)
	}
	if reloaded != applied {
		t.Fatalf("persisted %+v, want %+v", reloaded, applied)
	}
}

func TestTickAutoOff(t *testing.T) {
	hw := &fakeHW{on: true}
	s := newTestService(t, hw, &fakePlayback{})

	var notified []Status
	s.SetNotifier(func(st Status) { notified = append(notified, st) })

	night := at(23, 0)
	s.mu.Lock()
	s.lastActivity = at(21, 0)
	s.mu.Unlock()

	mode, changed := s.Tick(night)
	if !changed || mode != ModeOff {
		t.Fatalf("Tick() = (%q, %v), want (off, true)", mode, changed)
	}
	if calls := hw.calls(); len(calls) != 1 || calls[0] != false {
		t.Fatalf("hardware calls = %v, want [false]", calls)
	}
	if len(notified) != 1 || notified[0].Mode != ModeOff {
		t.Fatalf("notifier saw %+v, want one off status", notified)
	}
}

func TestTickDoesNotAutoOffWhilePlaying(t *testing.T) {
	hw := &fakeHW{on: true}
	s := newTestService(t, hw, &fakePlayback{playing: true})
	s.mu.Lock()
	s.lastActivity = at(18, 0)
	s.mu.Unlock()

	if _, changed := s.Tick(at(23, 30)); changed {
		t.Fatal("Tick() blacked the panel during playback")
	}
}

func TestPlaybackRestartsTheIdleInterval(t *testing.T) {
	hw := &fakeHW{on: true}
	pb := &fakePlayback{playing: true}
	s := newTestService(t, hw, pb)
	s.mu.Lock()
	s.lastActivity = at(18, 0)
	s.mu.Unlock()
	s.now = func() time.Time { return at(23, 30) }

	// A long album produces no touches; the tick must still count it as
	// activity, so the hour starts when the music stops.
	if _, changed := s.Tick(at(23, 30)); changed {
		t.Fatal("Tick() changed mode during playback")
	}
	pb.playing = false
	if _, changed := s.Tick(at(23, 35)); changed {
		t.Fatal("Tick() blacked the panel five minutes after the music stopped")
	}
	if _, changed := s.Tick(atOn(19, 0, 31)); !changed {
		t.Fatal("Tick() never blacked the panel after a full idle hour")
	}
}

func TestTickAutoRestoreGoesToStandby(t *testing.T) {
	hw := &fakeHW{on: true}
	s := newTestService(t, hw, &fakePlayback{})
	if err := s.SetMode(ModeOff); err != nil {
		t.Fatal(err)
	}

	mode, changed := s.Tick(at(5, 0))
	if !changed || mode != ModeStandby {
		t.Fatalf("Tick() = (%q, %v), want (standby, true)", mode, changed)
	}
	calls := hw.calls()
	if len(calls) != 2 || calls[1] != true {
		t.Fatalf("hardware calls = %v, want the panel lit again", calls)
	}
}

func TestAutoRestoreFiresOncePerDay(t *testing.T) {
	hw := &fakeHW{on: true}
	s := newTestService(t, hw, &fakePlayback{})
	if err := s.SetMode(ModeOff); err != nil {
		t.Fatal(err)
	}
	if _, changed := s.Tick(at(5, 0)); !changed {
		t.Fatal("first restore did not fire")
	}

	// Someone turns it off again at 05:10; the schedule must not fight them.
	if err := s.SetMode(ModeOff); err != nil {
		t.Fatal(err)
	}
	if _, changed := s.Tick(at(5, 10)); changed {
		t.Fatal("auto-restore undid a deliberate off on the same day")
	}
}

func TestMarkActivityDefersAutoOff(t *testing.T) {
	s := newTestService(t, &fakeHW{on: true}, &fakePlayback{})
	s.now = func() time.Time { return at(23, 0) }
	s.MarkActivity()

	if _, changed := s.Tick(at(23, 5)); changed {
		t.Fatal("Tick() blacked a panel somebody had just touched")
	}
}

// A backend restart while the panel is off used to leave the service
// believing it was live, which made the first "turn it on" a no-op against a
// mode it already held -- the screen stayed dark with no way back.
func TestNewServiceSeedsModeFromTheHardware(t *testing.T) {
	s := newTestService(t, &fakeHW{on: false}, &fakePlayback{})
	if got := s.Mode(); got != ModeOff {
		t.Fatalf("Mode() = %q, want %q", got, ModeOff)
	}
}

func TestSetModeReconcilesHardwareDrift(t *testing.T) {
	hw := &fakeHW{on: true}
	s := newTestService(t, hw, &fakePlayback{})

	// Something else turned the panel off behind our back.
	hw.mu.Lock()
	hw.on = false
	hw.mu.Unlock()

	if err := s.SetMode(ModeLive); err != nil {
		t.Fatalf("SetMode(live) returned %v", err)
	}
	if calls := hw.calls(); len(calls) != 1 || !calls[0] {
		t.Fatalf("hardware calls = %v, want one true", calls)
	}
}

func TestSetModeIsStillAnoOpWhenTheHardwareAgrees(t *testing.T) {
	hw := &fakeHW{on: true}
	s := newTestService(t, hw, &fakePlayback{})

	if err := s.SetMode(ModeLive); err != nil {
		t.Fatalf("SetMode(live) returned %v", err)
	}
	if calls := hw.calls(); len(calls) != 0 {
		t.Fatalf("hardware touched for a mode it was already in: %v", calls)
	}
}
