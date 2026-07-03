// Package health_test — cross-platform tests for the Collector interface,
// stub behaviour, and Snapshot JSON serialisation.
package health

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Stub collector (darwin / CI) always returns Supported:false fields.
// We verify the interface contract holds and JSON round-trips cleanly.
// ---------------------------------------------------------------------------

func TestStubCollectorImplementsInterface(t *testing.T) {
	t.Parallel()
	// NewCollector returns the platform collector and tailer.
	// On darwin both are stubs.
	c, tailer := NewCollector(CollectorConfig{})
	if c == nil {
		t.Fatal("NewCollector returned nil collector")
	}
	if tailer == nil {
		t.Fatal("NewCollector returned nil tailer")
	}
	snap := c.Collect()
	// Snapshot must always be JSON-serialisable without error.
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal(Snapshot) = %v", err)
	}
	if len(b) == 0 {
		t.Fatal("json.Marshal(Snapshot) returned empty bytes")
	}
}

func TestSnapshotJSONRoundTrip(t *testing.T) {
	t.Parallel()
	snap := Snapshot{
		ALSA: ALSASnapshot{
			Supported: true,
			State:     "RUNNING",
			AvailMax:  4096,
		},
		Load: LoadSnapshot{
			Load1:  0.52,
			Load5:  0.83,
			Load15: 1.02,
		},
		Memory: MemorySnapshot{
			TotalKB:     8147152,
			AvailableKB: 4000000,
		},
		PSwpout:        0,
		ThrottleMask:   0,
		TempCelsius:    43.3,
		VcgencmdOK:     true,
		Xruns:          0,
		XrunsAvailable: false,
	}
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Snapshot
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ALSA.State != "RUNNING" {
		t.Errorf("ALSA.State = %q after round-trip, want RUNNING", got.ALSA.State)
	}
	if got.Load.Load1 != 0.52 {
		t.Errorf("Load.Load1 = %v, want 0.52", got.Load.Load1)
	}
	if got.TempCelsius != 43.3 {
		t.Errorf("TempCelsius = %v, want 43.3", got.TempCelsius)
	}
}

func TestCollectorConfigDefaults(t *testing.T) {
	t.Parallel()
	cfg := CollectorConfig{}
	// The zero value (0) for DACSoundCardIndex is valid; -1 means auto-detect.
	// Verify construction does not panic with default config.
	c, tailer := NewCollector(cfg)
	if c == nil || tailer == nil {
		t.Fatal("NewCollector with zero-value config returned nil")
	}
	t.Logf("DACSoundCardIndex default = %d (ok)", cfg.DACSoundCardIndex)
}

// ---------------------------------------------------------------------------
// isXrunLine — secondary coverage (inline-testable, no build tag)
// ---------------------------------------------------------------------------

func TestIsXrunLineBoundary(t *testing.T) {
	t.Parallel()
	// "xrun" at word boundary inside a longer kernel message.
	line := "<4>[12345.678] snd_usb_audio 2-1.3:1.0: xrun occurred"
	if !isXrunLine(line) {
		t.Errorf("expected isXrunLine(%q) = true", line)
	}
	// "xrundance" should NOT match — partial prefix of the word.
	if isXrunLine("xrundance is a dance move") {
		t.Errorf("isXrunLine matched a non-xrun word")
	}
}

// ---------------------------------------------------------------------------
// parseAlsaStatus edge cases
// ---------------------------------------------------------------------------

func TestParseAlsaStatusWhitespace(t *testing.T) {
	t.Parallel()
	// Keys with irregular spacing must still parse.
	input := "state: RUNNING\navail_max   :   2048\n"
	got := parseAlsaStatus(input)
	if got.State != "RUNNING" {
		t.Errorf("State = %q", got.State)
	}
	if got.AvailMax != 2048 {
		t.Errorf("AvailMax = %d", got.AvailMax)
	}
}

// ---------------------------------------------------------------------------
// parseMeminfo — partial file (only some keys present)
// ---------------------------------------------------------------------------

func TestParseMeminfoPartial(t *testing.T) {
	t.Parallel()
	input := "MemTotal:       4096000 kB\n"
	got := parseMeminfo(input)
	if got.TotalKB != 4096000 {
		t.Errorf("TotalKB = %d, want 4096000", got.TotalKB)
	}
	// Missing keys should be zero, not garbage.
	if got.FreeKB != 0 {
		t.Errorf("FreeKB = %d, want 0 (absent)", got.FreeKB)
	}
}

// ---------------------------------------------------------------------------
// Health endpoint JSON shape
// ---------------------------------------------------------------------------

func TestHealthSnapshotContainsXrunsAndAlsa(t *testing.T) {
	t.Parallel()
	snap := Snapshot{
		Xruns:          5,
		XrunsAvailable: true,
		ALSA: ALSASnapshot{
			Supported: true,
			State:     "XRUN",
		},
	}
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"xruns"`) {
		t.Errorf("JSON missing xruns key: %s", s)
	}
	if !strings.Contains(s, `"alsa"`) {
		t.Errorf("JSON missing alsa key: %s", s)
	}
}
