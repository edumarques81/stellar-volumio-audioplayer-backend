// Package fifo provides interruptible reads on named pipes.
//
// Two daemons in this repo tail a FIFO that is silent for long stretches:
// the spectrum analyser on /tmp/mpd_spectrum.fifo (silent whenever MPD is
// idle or paused) and the AirPlay forwarder on
// /tmp/shairport-sync-metadata (silent between track boundaries). Both used
// the obvious os.Open + Read pair, and both wedged shutdown for the same
// reason.
//
// The problem is that neither half of that pair is cancellable. Opening a
// FIFO read-only blocks until a writer appears, and once open, a read blocks
// until bytes arrive. A goroutine parked in either syscall is parked in the
// kernel, not at a select, so cancelling its context does nothing and a
// wrapper that checks ctx.Err() *before* calling Read only ever helps on the
// call that follows the one that is stuck. Shutdown then waited on a
// goroutine that was never coming back, and systemd answered by SIGKILLing
// the service after its 90s stop timeout — on every single deploy.
//
// Open returns a descriptor that Read can interrupt; Reader does the
// interrupting. Use them together.
package fifo

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// DefaultPollInterval bounds a single read. It is not a data timeout — a
// silent FIFO is normal — it is how often the reader surfaces from the kernel
// to check whether it has been cancelled. Shutdown latency is one poll
// interval; the cost is one expired timer per interval per idle reader.
const DefaultPollInterval = 250 * time.Millisecond

// Reader adapts a FIFO opened by Open into an io.Reader whose blocking is
// bounded, so a cancelled context is observed while the pipe is silent.
//
// Read never reports the poll interval expiring as an error: an idle FIFO is
// the normal state, so it simply waits again. It returns ctx.Err() on cancel
// and io.EOF once every writer has closed.
type Reader struct {
	ctx  context.Context
	f    *os.File
	poll time.Duration
}

// NewReader wraps f, which must have come from Open — a descriptor from a
// plain os.Open is not registered with the runtime poller, so its read
// deadlines are inert and reads stay uninterruptible.
//
// A poll of zero means DefaultPollInterval.
func NewReader(ctx context.Context, f *os.File, poll time.Duration) *Reader {
	if poll <= 0 {
		poll = DefaultPollInterval
	}
	return &Reader{ctx: ctx, f: f, poll: poll}
}

// Read blocks until at least one byte is available, the writer closes, or the
// context is cancelled.
func (r *Reader) Read(p []byte) (int, error) {
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}

		if err := r.f.SetReadDeadline(time.Now().Add(r.poll)); err != nil && !errors.Is(err, os.ErrNoDeadline) {
			return 0, err
		}

		n, err := r.f.Read(p)
		if n > 0 {
			// Bytes in hand win over whatever the deadline did. A genuine
			// error alongside them will surface again on the next call.
			return n, nil
		}

		switch {
		case err == nil:
			// A zero-length read with no error carries no information;
			// go round again rather than report it upward.
		case errors.Is(err, os.ErrDeadlineExceeded):
			// Silence on the pipe — expected, and the whole point of the
			// deadline. Loop back to the cancellation check.
		case errors.Is(err, syscall.EAGAIN):
			// Descriptor is non-blocking but was not accepted by the
			// poller, so the deadline is inert and empty reads surface
			// directly. Pace them by hand.
			if !Sleep(r.ctx, r.poll) {
				return 0, r.ctx.Err()
			}
		default:
			return 0, err
		}
	}
}

// Sleep waits for d, returning false if ctx was cancelled first.
func Sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
