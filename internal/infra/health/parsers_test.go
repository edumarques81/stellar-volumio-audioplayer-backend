// Package health_test contains table-driven tests for all pure parser
// functions. These run on every platform (no build tag) because the parsers
// take raw strings/bytes, never open files.
package health

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// parseAlsaStatus
// ---------------------------------------------------------------------------

func TestParseAlsaStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantState string
		wantAvail int64
		wantOK    bool
	}{
		{
			name: "running",
			input: `state: RUNNING
owner_pid   : 1234
trigger_time: 12345.678901
tstamp      : 12345.678902
delay       : 1024
avail       : 2048
avail_max   : 4096
`,
			wantState: "RUNNING",
			wantAvail: 4096,
			wantOK:    true,
		},
		{
			name: "xrun",
			input: `state: XRUN
avail_max   : 8192
`,
			wantState: "XRUN",
			wantAvail: 8192,
			wantOK:    true,
		},
		{
			name: "setup",
			input: `state: SETUP
avail_max   : 0
`,
			wantState: "SETUP",
			wantAvail: 0,
			wantOK:    true,
		},
		{
			name:      "empty",
			input:     "",
			wantState: "",
			wantAvail: 0,
			wantOK:    false,
		},
		{
			name:      "no-state-line",
			input:     "avail_max   : 512\n",
			wantState: "",
			wantAvail: 512,
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseAlsaStatus(tt.input)
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q", got.State, tt.wantState)
			}
			if got.AvailMax != tt.wantAvail {
				t.Errorf("AvailMax = %d, want %d", got.AvailMax, tt.wantAvail)
			}
			if got.Supported != tt.wantOK {
				t.Errorf("Supported = %v, want %v", got.Supported, tt.wantOK)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseLoadavg
// ---------------------------------------------------------------------------

func TestParseLoadavg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		want1     float64
		want5     float64
		want15    float64
		wantProcs int
		wantOK    bool
	}{
		{
			// /proc/loadavg format: "running/total" — RunningProcs is the
			// number of currently running (on-CPU) processes.
			name:      "typical",
			input:     "0.52 0.83 1.02 2/412 12345\n",
			want1:     0.52,
			want5:     0.83,
			want15:    1.02,
			wantProcs: 2,
			wantOK:    true,
		},
		{
			name:      "zero-load",
			input:     "0.00 0.00 0.00 1/1 1\n",
			want1:     0.00,
			want5:     0.00,
			want15:    0.00,
			wantProcs: 1,
			wantOK:    true,
		},
		{
			name:   "empty",
			input:  "",
			wantOK: false,
		},
		{
			name:   "malformed",
			input:  "not a loadavg\n",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseLoadavg(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got.Load1 != tt.want1 {
				t.Errorf("Load1 = %v, want %v", got.Load1, tt.want1)
			}
			if got.Load5 != tt.want5 {
				t.Errorf("Load5 = %v, want %v", got.Load5, tt.want5)
			}
			if got.Load15 != tt.want15 {
				t.Errorf("Load15 = %v, want %v", got.Load15, tt.want15)
			}
			if got.RunningProcs != tt.wantProcs {
				t.Errorf("RunningProcs = %v, want %v", got.RunningProcs, tt.wantProcs)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseMeminfo
// ---------------------------------------------------------------------------

func TestParseMeminfo(t *testing.T) {
	t.Parallel()

	input := `MemTotal:       8147152 kB
MemFree:         123456 kB
MemAvailable:   4000000 kB
SwapTotal:        524288 kB
SwapFree:         524280 kB
`
	got := parseMeminfo(input)
	if got.TotalKB != 8147152 {
		t.Errorf("TotalKB = %d, want 8147152", got.TotalKB)
	}
	if got.FreeKB != 123456 {
		t.Errorf("FreeKB = %d, want 123456", got.FreeKB)
	}
	if got.AvailableKB != 4000000 {
		t.Errorf("AvailableKB = %d, want 4000000", got.AvailableKB)
	}
	if got.SwapTotalKB != 524288 {
		t.Errorf("SwapTotalKB = %d, want 524288", got.SwapTotalKB)
	}
	if got.SwapFreeKB != 524280 {
		t.Errorf("SwapFreeKB = %d, want 524280", got.SwapFreeKB)
	}
}

func TestParseMeminfoEmpty(t *testing.T) {
	t.Parallel()
	got := parseMeminfo("")
	if got.TotalKB != 0 || got.FreeKB != 0 {
		t.Errorf("expected zero values for empty input, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// parseVmstat
// ---------------------------------------------------------------------------

func TestParseVmstat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantPswpout int64
		wantOK      bool
	}{
		{
			name: "present",
			input: `nr_free_pages 12345
pgfault 98765
pswpout 42
pswpin 0
`,
			wantPswpout: 42,
			wantOK:      true,
		},
		{
			name:        "absent",
			input:       "nr_free_pages 12345\n",
			wantPswpout: 0,
			wantOK:      false,
		},
		{
			name:        "empty",
			input:       "",
			wantPswpout: 0,
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pswpout, ok := parseVmstat(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if pswpout != tt.wantPswpout {
				t.Errorf("pswpout = %d, want %d", pswpout, tt.wantPswpout)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseThrottled
// ---------------------------------------------------------------------------

func TestParseThrottled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantMask uint32
		wantOK   bool
	}{
		{
			name:     "clean",
			input:    "throttled=0x0\n",
			wantMask: 0x0,
			wantOK:   true,
		},
		{
			name:     "under-voltage",
			input:    "throttled=0x1\n",
			wantMask: 0x1,
			wantOK:   true,
		},
		{
			name:     "throttled-now",
			input:    "throttled=0x4\n",
			wantMask: 0x4,
			wantOK:   true,
		},
		{
			name:     "multiple-bits",
			input:    "throttled=0x50005\n",
			wantMask: 0x50005,
			wantOK:   true,
		},
		{
			name:   "empty",
			input:  "",
			wantOK: false,
		},
		{
			name:   "malformed",
			input:  "not-throttled-output\n",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mask, ok := parseThrottled(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (input %q)", ok, tt.wantOK, tt.input)
			}
			if mask != tt.wantMask {
				t.Errorf("mask = 0x%x, want 0x%x", mask, tt.wantMask)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseTemp
// ---------------------------------------------------------------------------

func TestParseTemp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantTemp float64
		wantOK   bool
	}{
		{
			name:     "typical",
			input:    "temp=43.3'C\n",
			wantTemp: 43.3,
			wantOK:   true,
		},
		{
			name:     "round",
			input:    "temp=55.0'C\n",
			wantTemp: 55.0,
			wantOK:   true,
		},
		{
			name:   "empty",
			input:  "",
			wantOK: false,
		},
		{
			name:   "malformed",
			input:  "temperature=warm\n",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			temp, ok := parseTemp(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && temp != tt.wantTemp {
				t.Errorf("temp = %v, want %v", temp, tt.wantTemp)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isXrunLine
// ---------------------------------------------------------------------------

func TestIsXrunLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		line  string
		match bool
	}{
		// positive cases
		{"xrun-plain", "kernel: [12345.678] pcmC2D0p: xrun occurred", true},
		{"underrun-plain", "snd_usb_audio: underrun detected", true},
		{"snd-usb-reset", "snd_usb_audio: cannot submit urb", true},
		{"xrun-uppercase", "XRUN happened on DAC", true},
		// "hard_underrun" — underscore is a \w character so \b is NOT between
		// _ and u; this should NOT match so we don't false-positive on
		// unrelated kernel events that happen to contain the suffix.
		{"underrun-in-word", "hard_underrun signal", false},
		{"snd-usb-reset-word", "snd_usb_audio 2-1:1.0: cannot submit urb", true},
		// negative cases
		{"irrelevant", "kernel: [12345.678] usb 2-1: new full-speed USB device", false},
		{"empty", "", false},
		{"partial-match-no", "xrundance is a tool", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isXrunLine(tt.line)
			if got != tt.match {
				t.Errorf("isXrunLine(%q) = %v, want %v", tt.line, got, tt.match)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseAlsaCardIndex
// ---------------------------------------------------------------------------

func TestParseAlsaCardIndex(t *testing.T) {
	t.Parallel()

	// Content of /proc/asound/cards with vc4hdmi cards 0+1 and USB DAC card 2
	input := strings.Join([]string{
		" 0 [vc4hdmi0       ]: vc4-hdmi - vc4-hdmi-0",
		"                      vc4-hdmi-0",
		" 1 [vc4hdmi1       ]: vc4-hdmi - vc4-hdmi-1",
		"                      vc4-hdmi-1",
		" 2 [U20SU6         ]: USB-Audio - U20SU6",
		"                      Singxer U20SU6 at usb-0000:01:00.0-1, high speed",
	}, "\n")

	tests := []struct {
		name      string
		input     string
		wantIndex int
		wantOK    bool
	}{
		{
			name:      "three-cards-usbdac-at-2",
			input:     input,
			wantIndex: 2,
			wantOK:    true,
		},
		{
			name: "no-usb-card",
			input: " 0 [vc4hdmi0]: vc4-hdmi - vc4-hdmi-0\n" +
				" 1 [vc4hdmi1]: vc4-hdmi - vc4-hdmi-1\n",
			wantIndex: -1,
			wantOK:    false,
		},
		{
			name:      "empty",
			input:     "",
			wantIndex: -1,
			wantOK:    false,
		},
		{
			name:      "usb-only",
			input:     " 0 [U20SU6]: USB-Audio - U20SU6\n",
			wantIndex: 0,
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			idx, ok := parseAlsaCardIndex(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if idx != tt.wantIndex {
				t.Errorf("idx = %d, want %d", idx, tt.wantIndex)
			}
		})
	}
}
