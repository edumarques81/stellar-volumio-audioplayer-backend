//go:build linux

package health

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"runtime/metrics"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

// ---------------------------------------------------------------------------
// Linux Collector
// ---------------------------------------------------------------------------

// linuxCollector implements Collector using procfs and vcgencmd.
type linuxCollector struct {
	cfg    CollectorConfig
	tailer XrunTailer
}

// newPlatformCollector returns the Linux Collector and its associated XrunTailer.
// The caller must call XrunTailer.Start(ctx) before using xrun data.
func newPlatformCollector(cfg CollectorConfig) (Collector, XrunTailer) {
	tailer := newPlatformXrunTailer()
	return &linuxCollector{cfg: cfg, tailer: tailer}, tailer
}

// Collect samples all health metrics. Failed individual reads produce zero
// values; the function never panics.
func (c *linuxCollector) Collect() Snapshot {
	snap := Snapshot{}

	// ALSA status
	snap.ALSA = c.collectALSA()

	// /proc/loadavg
	if content, err := readFile("/proc/loadavg"); err == nil {
		if load, ok := parseLoadavg(content); ok {
			snap.Load = load
		}
	}

	// /proc/meminfo
	if content, err := readFile("/proc/meminfo"); err == nil {
		snap.Memory = parseMeminfo(content)
	}

	// /proc/vmstat — pswpout
	if content, err := readFile("/proc/vmstat"); err == nil {
		if n, ok := parseVmstat(content); ok {
			snap.PSwpout = n
			snap.PSwpoutAvail = true
		}
	}

	// /proc/pressure/memory — PSI (Linux 4.20+, optional)
	snap.PSIMemFull, snap.PSIAvail = collectPSI()

	// vcgencmd (Pi-only; fails gracefully on non-Pi Linux)
	snap.ThrottleMask, snap.TempCelsius, snap.VcgencmdOK = collectVcgencmd()

	// Go runtime metrics
	snap.Runtime = collectRuntime()

	// Xrun counter from the tailer goroutine (always-on)
	xruns := c.tailer.Count()
	snap.Xruns = xruns
	snap.XrunsAvailable = xruns >= 0

	return snap
}

// collectALSA reads the ALSA PCM status for the USB DAC.
func (c *linuxCollector) collectALSA() ALSASnapshot {
	cardIdx := c.cfg.DACSoundCardIndex
	if cardIdx < 0 {
		// auto-detect
		content, err := readFile("/proc/asound/cards")
		if err != nil {
			return ALSASnapshot{}
		}
		idx, ok := parseAlsaCardIndex(content)
		if !ok {
			log.Debug().Msg("health: no USB DAC card found in /proc/asound/cards")
			return ALSASnapshot{}
		}
		cardIdx = idx
	}
	path := fmt.Sprintf("/proc/asound/card%d/pcm0p/sub0/status", cardIdx)
	content, err := readFile(path)
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("health: ALSA status unreadable")
		return ALSASnapshot{}
	}
	return parseAlsaStatus(content)
}

// collectPSI reads /proc/pressure/memory and returns the "full" avg10 value.
func collectPSI() (float64, bool) {
	content, err := readFile("/proc/pressure/memory")
	if err != nil {
		return 0, false
	}
	// Format: "some avg10=X avg60=Y avg300=Z total=T"
	//         "full avg10=X avg60=Y avg300=Z total=T"
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "full ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if strings.HasPrefix(field, "avg10=") {
				val := field[len("avg10="):]
				var f float64
				if _, err := fmt.Sscanf(val, "%f", &f); err == nil {
					return f, true
				}
			}
		}
	}
	return 0, false
}

// collectVcgencmd shells out to vcgencmd (reuses the idiom from lcd_linux.go).
// Returns (throttleMask, tempCelsius, ok). ok is false if vcgencmd is absent.
func collectVcgencmd() (uint32, float64, bool) {
	throttleOut, err1 := runCmd("vcgencmd", "get_throttled")
	tempOut, err2 := runCmd("vcgencmd", "measure_temp")
	if err1 != nil && err2 != nil {
		return 0, 0, false
	}

	var mask uint32
	if err1 == nil {
		mask, _ = parseThrottled(throttleOut)
	}
	var temp float64
	if err2 == nil {
		temp, _ = parseTemp(tempOut)
	}
	return mask, temp, true
}

// collectRuntime samples Go runtime/metrics for GC pause p99 and sched latency p99.
func collectRuntime() RuntimeSnapshot {
	descs := metrics.All()

	// Build the sample list for the metrics we care about.
	sampleKeys := []string{
		"/gc/pauses:seconds",
		"/sched/latencies:seconds",
	}
	var samples []metrics.Sample
	for _, d := range descs {
		for _, k := range sampleKeys {
			if d.Name == k {
				samples = append(samples, metrics.Sample{Name: k})
			}
		}
	}

	if len(samples) > 0 {
		metrics.Read(samples)
	}

	snap := RuntimeSnapshot{
		Goroutines: runtime.NumGoroutine(),
	}

	for _, s := range samples {
		if s.Value.Kind() != metrics.KindFloat64Histogram {
			continue
		}
		h := s.Value.Float64Histogram()
		p99 := histogramP99Ms(h)
		switch s.Name {
		case "/gc/pauses:seconds":
			snap.GCPauseP99Ms = p99
		case "/sched/latencies:seconds":
			snap.SchedP99Ms = p99
		}
	}
	return snap
}

// histogramP99Ms computes the approximate p99 of a runtime/metrics histogram
// and converts it to milliseconds.
func histogramP99Ms(h *metrics.Float64Histogram) float64 {
	if h == nil || len(h.Counts) == 0 {
		return 0
	}
	var total uint64
	for _, c := range h.Counts {
		total += c
	}
	if total == 0 {
		return 0
	}
	threshold := uint64(math.Ceil(float64(total) * 0.99))
	var cum uint64
	for i, c := range h.Counts {
		cum += c
		if cum >= threshold {
			// Return the upper bound of this bucket in ms
			if i+1 < len(h.Buckets) {
				return h.Buckets[i+1] * 1000
			}
			if i < len(h.Buckets) {
				return h.Buckets[i] * 1000
			}
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Linux XrunTailer
// ---------------------------------------------------------------------------

// linuxXrunTailer tails /dev/kmsg and counts xrun-matching lines.
type linuxXrunTailer struct {
	count atomic.Int64
	ready atomic.Bool // true once Start has been called
}

// newPlatformXrunTailer returns the Linux xrun tailer.
func newPlatformXrunTailer() XrunTailer {
	t := &linuxXrunTailer{}
	t.count.Store(-1) // sentinel until Start is called
	return t
}

// Start opens /dev/kmsg and counts new xrun lines until ctx is cancelled.
// If /dev/kmsg cannot be opened (EACCES — no CAP_SYSLOG), a single warning
// is logged and the goroutine exits; Count() continues returning -1.
//
// /dev/kmsg semantics: a fresh open starts at the OLDEST record in the ring
// buffer. To count only xruns that occur AFTER Start, we open non-blocking and
// seek to SEEK_END (the kernel positions the fd after the last existing
// record). Subsequent reads then return only new records. O_NONBLOCK also lets
// the Go runtime poller park the reader goroutine instead of a blocking read
// that ctx cancellation can't interrupt.
func (t *linuxXrunTailer) Start(ctx context.Context) {
	go t.run(ctx)
}

// Count returns the xrun count since Start, or -1 if not available.
func (t *linuxXrunTailer) Count() int64 {
	return t.count.Load()
}

func (t *linuxXrunTailer) run(ctx context.Context) {
	f, err := os.OpenFile("/dev/kmsg", os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		log.Warn().Err(err).Msg("health: cannot open /dev/kmsg for xrun monitoring " +
			"(backend needs CAP_SYSLOG — will be granted via AmbientCapabilities in Phase 3)")
		// count stays -1
		return
	}
	defer f.Close()

	// Skip existing records: SEEK_END on /dev/kmsg positions after the last
	// buffered record, so we only observe xruns from now on. If the seek fails
	// (very old kernels), fall back to counting from the current position — the
	// worst case is a one-time inflated baseline, harmless for delta sampling.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		log.Debug().Err(err).Msg("health: /dev/kmsg SEEK_END failed; counting from ring start")
	}

	// We are now at the "current" position — begin counting.
	t.count.Store(0)

	// Stop the blocked read when ctx is cancelled by closing the fd, which
	// unblocks the poller-registered Read with an error.
	go func() {
		<-ctx.Done()
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		if isXrunLine(scanner.Text()) {
			t.count.Add(1)
			log.Debug().Str("line", scanner.Text()).Msg("health: ALSA xrun detected in kmsg")
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// readFile reads an entire file and returns its content as a string.
func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// runCmd runs a command and returns its combined stdout as a string.
func runCmd(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
