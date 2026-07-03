//go:build darwin

package health

import (
	"context"
	"sync/atomic"
)

// newPlatformCollector returns a stub collector and tailer on Darwin. All
// Snapshot fields are zero-valued with Supported:false.
func newPlatformCollector(_ CollectorConfig) (Collector, XrunTailer) {
	t := &stubXrunTailer{}
	return &stubCollector{}, t
}

// stubCollector is the macOS/CI no-op implementation.
type stubCollector struct{}

// Collect returns a zeroed Snapshot with all Supported flags false.
func (s *stubCollector) Collect() Snapshot {
	return Snapshot{
		Xruns:          -1,
		XrunsAvailable: false,
	}
}

// stubXrunTailer is the macOS no-op tailer.
type stubXrunTailer struct {
	count atomic.Int64
}

// Start is a no-op on Darwin.
func (t *stubXrunTailer) Start(_ context.Context) {
	t.count.Store(-1)
}

// Count returns -1 (unavailable) on Darwin.
func (t *stubXrunTailer) Count() int64 {
	return -1
}
