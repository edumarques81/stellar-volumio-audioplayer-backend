// Package health provides lightweight in-process performance and health
// telemetry for the Stellar backend. It reads Linux procfs, vcgencmd, ALSA
// hardware status, and Go runtime/metrics. On non-Linux platforms it returns
// stub zero-value snapshots with Supported:false.
//
// The design mirrors internal/infra/paths: a platform-agnostic file (this
// one) declares the shared types and the Collector interface; platform files
// (health_linux.go / health_darwin.go) provide the implementations.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Snapshot and sub-structs
// ---------------------------------------------------------------------------

// ALSASnapshot holds the parsed state from /proc/asound/card<N>/pcm0p/sub0/status.
type ALSASnapshot struct {
	// Supported is false when no USB DAC card was found or the status file
	// could not be read. All other fields are zero in that case.
	Supported bool   `json:"supported"`
	State     string `json:"state"`     // e.g. "RUNNING", "XRUN", "SETUP"
	AvailMax  int64  `json:"avail_max"` // frames
}

// LoadSnapshot holds the parsed /proc/loadavg values.
type LoadSnapshot struct {
	Load1        float64 `json:"load1"`
	Load5        float64 `json:"load5"`
	Load15       float64 `json:"load15"`
	RunningProcs int     `json:"running_procs"`
}

// MemorySnapshot holds selected /proc/meminfo fields (all values in kB).
type MemorySnapshot struct {
	TotalKB     int64 `json:"total_kb"`
	FreeKB      int64 `json:"free_kb"`
	AvailableKB int64 `json:"available_kb"`
	SwapTotalKB int64 `json:"swap_total_kb"`
	SwapFreeKB  int64 `json:"swap_free_kb"`
}

// RuntimeSnapshot holds Go runtime metrics.
type RuntimeSnapshot struct {
	Goroutines   int     `json:"goroutines"`
	GCPauseP99Ms float64 `json:"gc_pause_p99_ms"`
	SchedP99Ms   float64 `json:"sched_p99_ms"`
}

// Snapshot is a point-in-time sample of all health metrics.
type Snapshot struct {
	ALSA   ALSASnapshot   `json:"alsa"`
	Load   LoadSnapshot   `json:"load"`
	Memory MemorySnapshot `json:"memory"`

	// PSwpout is the cumulative swap-out event counter from /proc/vmstat.
	// A non-zero delta between two snapshots indicates memory pressure caused
	// swapping — a key "audio got starved" signal.
	PSwpout      int64 `json:"pswpout"`
	PSwpoutAvail bool  `json:"pswpout_avail"`

	// PSI memory pressure (full stall percentage, if /proc/pressure/memory exists).
	PSIMemFull float64 `json:"psi_mem_full_pct"`
	PSIAvail   bool    `json:"psi_avail"`

	// ThrottleMask is the raw bitmask from `vcgencmd get_throttled`.
	// 0x1 = under-voltage now, 0x4 = throttled now, 0x8 = soft-temp-limit now.
	// Historical bits are 0x10000, 0x40000, 0x80000.
	ThrottleMask uint32  `json:"throttle_mask"`
	TempCelsius  float64 `json:"temp_celsius"`
	VcgencmdOK   bool    `json:"vcgencmd_ok"`

	// Xruns is the number of ALSA xrun/underrun events observed since the
	// xrun tailer started. -1 means the tailer could not open /dev/kmsg
	// (requires CAP_SYSLOG; backend runs as user eduardo without it by default).
	Xruns          int64 `json:"xruns"`
	XrunsAvailable bool  `json:"xruns_available"`

	Runtime RuntimeSnapshot `json:"runtime"`
}

// ---------------------------------------------------------------------------
// Collector interface
// ---------------------------------------------------------------------------

// Collector collects a health Snapshot on demand.
type Collector interface {
	// Collect returns a point-in-time health snapshot. It must never panic;
	// failed reads produce zero/default values with Supported:false flags.
	Collect() Snapshot
}

// CollectorConfig carries optional configuration for the Collector.
type CollectorConfig struct {
	// DACSoundCardIndex is the ALSA card number of the USB DAC. Set to -1
	// to auto-detect by scanning /proc/asound/cards for the first card whose
	// name does not start with "vc4hdmi". The zero value (0) is also valid
	// and means "use card 0 directly" — callers that want auto-detection must
	// pass -1 explicitly.
	DACSoundCardIndex int
}

// NewCollector returns the platform-appropriate Collector together with its
// associated XrunTailer. The caller must call XrunTailer.Start(ctx) to begin
// xrun monitoring. On Darwin both return stubs that degrade gracefully.
func NewCollector(cfg CollectorConfig) (Collector, XrunTailer) {
	return newPlatformCollector(cfg)
}

// ---------------------------------------------------------------------------
// Xrun tailer
// ---------------------------------------------------------------------------

// XrunTailer tails /dev/kmsg for ALSA xrun/underrun messages.
// It degrades gracefully if /dev/kmsg is not readable (EACCES without
// CAP_SYSLOG). The tailer is obtained via NewCollector and must be started
// by the caller with Start(ctx).
type XrunTailer interface {
	// Start launches the tailer goroutine; it stops when ctx is cancelled.
	Start(ctx context.Context)
	// Count returns the number of xrun lines seen since Start was called.
	// Returns -1 if /dev/kmsg could not be opened.
	Count() int64
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

// NewMetricsHandler returns an http.Handler that serves the full Snapshot as
// JSON on GET. Only GET is allowed; other methods return 405.
func NewMetricsHandler(c Collector) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snap := c.Collect()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(snap); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
}

// BuildHealthExtra constructs the extra fields appended to the /health
// response when a Collector is available. It is a pure function called by
// main.go's /health handler.
func BuildHealthExtra(snap Snapshot) map[string]interface{} {
	return buildHealthExtra(snap)
}

func buildHealthExtra(snap Snapshot) map[string]interface{} {
	alsaMap := map[string]interface{}{
		"supported": snap.ALSA.Supported,
		"state":     snap.ALSA.State,
		"avail_max": snap.ALSA.AvailMax,
	}
	return map[string]interface{}{
		"xruns": snap.Xruns,
		"alsa":  alsaMap,
	}
}

// ---------------------------------------------------------------------------
// Pure parser functions (testable on all platforms)
// ---------------------------------------------------------------------------

// parseAlsaStatus parses the content of
// /proc/asound/card<N>/pcm0p/sub0/status into an ALSASnapshot.
// Returns Supported:false if the "state:" line is absent.
func parseAlsaStatus(content string) ALSASnapshot {
	var snap ALSASnapshot
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if k, v, ok := splitKV(line, ":"); ok {
			switch strings.TrimSpace(k) {
			case "state":
				snap.State = strings.TrimSpace(v)
				snap.Supported = true
			case "avail_max":
				if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
					snap.AvailMax = n
				}
			}
		}
	}
	return snap
}

// parseLoadavg parses a /proc/loadavg line.
// Format: "0.52 0.83 1.02 2/412 12345"
func parseLoadavg(content string) (LoadSnapshot, bool) {
	content = strings.TrimSpace(content)
	fields := strings.Fields(content)
	if len(fields) < 4 {
		return LoadSnapshot{}, false
	}
	l1, err1 := strconv.ParseFloat(fields[0], 64)
	l5, err5 := strconv.ParseFloat(fields[1], 64)
	l15, err15 := strconv.ParseFloat(fields[2], 64)
	if err1 != nil || err5 != nil || err15 != nil {
		return LoadSnapshot{}, false
	}
	// field[3] is "running/total"
	parts := strings.SplitN(fields[3], "/", 2)
	running := 0
	if len(parts) == 2 {
		if n, err := strconv.Atoi(parts[0]); err == nil {
			running = n
		}
	}
	return LoadSnapshot{
		Load1:        l1,
		Load5:        l5,
		Load15:       l15,
		RunningProcs: running,
	}, true
}

// parseMeminfo parses the content of /proc/meminfo and extracts key values.
// Missing keys produce zero values (kB units throughout).
func parseMeminfo(content string) MemorySnapshot {
	var m MemorySnapshot
	for _, line := range strings.Split(content, "\n") {
		k, v, ok := splitKV(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		// Strip the trailing " kB"
		val := strings.Fields(strings.TrimSpace(v))
		if len(val) == 0 {
			continue
		}
		n, err := strconv.ParseInt(val[0], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(k) {
		case "MemTotal":
			m.TotalKB = n
		case "MemFree":
			m.FreeKB = n
		case "MemAvailable":
			m.AvailableKB = n
		case "SwapTotal":
			m.SwapTotalKB = n
		case "SwapFree":
			m.SwapFreeKB = n
		}
	}
	return m
}

// parseVmstat extracts the pswpout counter from /proc/vmstat content.
// Returns (0, false) if the key is absent.
func parseVmstat(content string) (int64, bool) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := splitKV(line, " ")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == "pswpout" {
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// parseThrottled parses the output of `vcgencmd get_throttled`.
// Format: "throttled=0x50005"
// Returns (0, false) if the line cannot be parsed.
func parseThrottled(content string) (uint32, bool) {
	const prefix = "throttled=0x"
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return parseThrottledHex(line[len(prefix):])
		}
	}
	return 0, false
}

func parseThrottledHex(s string) (uint32, bool) {
	s = strings.TrimRight(s, " \t\r\n'")
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

// parseTemp parses the output of `vcgencmd measure_temp`.
// Format: "temp=43.3'C"
func parseTemp(content string) (float64, bool) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "temp=") {
			continue
		}
		rest := line[len("temp="):]
		// strip the "'C" suffix
		rest = strings.TrimRight(rest, "'C \t")
		if f, err := strconv.ParseFloat(rest, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// parseAlsaCardIndex scans /proc/asound/cards content and returns the card
// index of the first card whose bracketed short-name does NOT start with
// "vc4hdmi" (i.e. the USB DAC). Returns (-1, false) if no such card found.
//
// Example /proc/asound/cards content:
//
//	0 [vc4hdmi0       ]: vc4-hdmi - vc4-hdmi-0
//	1 [vc4hdmi1       ]: vc4-hdmi - vc4-hdmi-1
//	2 [U20SU6         ]: USB-Audio - U20SU6
func parseAlsaCardIndex(content string) (int, bool) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		bracketOpen := strings.Index(line, "[")
		bracketClose := strings.Index(line, "]")
		if bracketOpen < 0 || bracketClose <= bracketOpen {
			continue
		}
		prefix := strings.TrimSpace(line[:bracketOpen])
		idx, err := strconv.Atoi(prefix)
		if err != nil {
			continue
		}
		shortName := strings.TrimSpace(line[bracketOpen+1 : bracketClose])
		if !strings.HasPrefix(strings.ToLower(shortName), "vc4hdmi") {
			return idx, true
		}
	}
	return -1, false
}

// ---------------------------------------------------------------------------
// isXrunLine
// ---------------------------------------------------------------------------

// xrunRe matches kernel log lines that signal ALSA xrun events.
// Positive: bare "xrun" or "underrun" words, or snd_usb_audio reset/submit
// errors. The spectrum FIFO is intentionally excluded.
var xrunRe = regexp.MustCompile(
	`(?i)\b(xrun|underrun)\b|snd_usb_audio.*\b(reset|cannot submit)\b`,
)

// isXrunLine returns true when line looks like an ALSA xrun event.
// Pure function; no I/O.
func isXrunLine(line string) bool {
	return xrunRe.MatchString(line)
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// splitKV splits "key<sep>value" returning the two parts and true.
// Returns ("","",false) if sep is not found.
func splitKV(line, sep string) (string, string, bool) {
	idx := strings.Index(line, sep)
	if idx < 0 {
		return "", "", false
	}
	return line[:idx], line[idx+len(sep):], true
}
