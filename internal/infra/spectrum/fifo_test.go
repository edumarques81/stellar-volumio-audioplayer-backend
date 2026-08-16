//go:build unix

package spectrum

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// safeEmitter is the concurrency-safe sibling of fakeEmitter: the FIFO tests
// run the streamer goroutine for real, so payloads arrive off the test
// goroutine and every access has to be locked.
type safeEmitter struct {
	mu     sync.Mutex
	frames []SpectrumData
}

func (e *safeEmitter) BroadcastToAll(_ string, data interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d, ok := data.(SpectrumData); ok {
		e.frames = append(e.frames, d)
	}
}

// waitForFrame blocks until at least one frame has been emitted, or the
// timeout expires.
func (e *safeEmitter) waitForFrame(timeout time.Duration) (SpectrumData, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		n := len(e.frames)
		var first SpectrumData
		if n > 0 {
			first = e.frames[0]
		}
		e.mu.Unlock()
		if n > 0 {
			return first, true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return SpectrumData{}, false
}

// makeFIFO creates a named pipe in a temp dir and returns its path.
func makeFIFO(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spectrum.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo(%s): %v", path, err)
	}
	return path
}

// openWriter opens the write end of a FIFO, retrying while the kernel reports
// ENXIO (no reader has opened the pipe yet). Non-blocking so a streamer that
// never opens its end fails the test instead of hanging it.
func openWriter(t *testing.T, path string) *os.File {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			f := os.NewFile(uintptr(fd), path)
			t.Cleanup(func() { _ = f.Close() })
			return f
		}
		if !errors.Is(err, syscall.ENXIO) || time.Now().After(deadline) {
			t.Fatalf("open write end of %s: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func testStreamer(fifoPath string) *Streamer {
	return New(Config{
		FIFOPath:   fifoPath,
		SampleRate: 44100,
		FFTSize:    64,
		NumBins:    8,
		FPS:        50,
	})
}

// TestStopReturnsWhileFIFOIsSilent is the regression test for the shutdown
// hang: MPD holds /tmp/mpd_spectrum.fifo open but writes nothing while idle,
// the reader parks in read(2), and a goroutine parked in the kernel cannot see
// a cancelled context. Stop() then waited on its WaitGroup forever, pinning
// main's deferred cleanup until systemd's 90s stop timeout SIGKILLed the
// process — which is what turned every deploy into a hard kill.
func TestStopReturnsWhileFIFOIsSilent(t *testing.T) {
	path := makeFIFO(t)
	s := testStreamer(path)
	s.Start(context.Background(), &safeEmitter{})

	// A writer that opens the pipe and then says nothing — exactly MPD's
	// behaviour with the fifo output enabled and playback stopped.
	openWriter(t, path)

	// Give the reader time to settle into a blocking read.
	time.Sleep(300 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() did not return while the FIFO was silent — the reader is still parked in a blocking read")
	}
}

// TestStopReturnsWithNoWriter covers the other blocking syscall on this path:
// os.Open on a FIFO blocks until a writer appears, so a streamer started
// before MPD (or with the fifo output disabled) could never observe
// cancellation either.
func TestStopReturnsWithNoWriter(t *testing.T) {
	path := makeFIFO(t)
	s := testStreamer(path)
	s.Start(context.Background(), &safeEmitter{})

	time.Sleep(300 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() did not return with no writer on the FIFO — the reader is still parked in open()")
	}
}

// TestStreamsFrameAcrossPartialWrites pins the alignment guarantee that the
// interruptible read has to preserve. Reads are now deadline-bounded so they
// can return mid-window; if a partial window were discarded (or re-read from
// offset zero) the interleaved L/R stream would shift and the channels would
// swap. The write is deliberately split at 100 bytes — not a multiple of the
// 4-byte stereo frame — and straddles a read deadline.
func TestStreamsFrameAcrossPartialWrites(t *testing.T) {
	const (
		fftSize = 64
		amp     = 0.5
		cycles  = 4
	)

	path := makeFIFO(t)
	s := testStreamer(path)
	emitter := &safeEmitter{}
	s.Start(context.Background(), emitter)
	defer s.Stop()

	w := openWriter(t, path)

	// One full window: a known-amplitude sine on the left channel, silence on
	// the right. RMS of a full-scale-relative sine is amp/√2.
	pcm := make([]byte, fftSize*4)
	for i := 0; i < fftSize; i++ {
		v := amp * math.Sin(2*math.Pi*cycles*float64(i)/float64(fftSize))
		binary.LittleEndian.PutUint16(pcm[i*4:], uint16(int16(v*32767)))
		binary.LittleEndian.PutUint16(pcm[i*4+2:], 0)
	}

	if _, err := w.Write(pcm[:100]); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	// Longer than fifoReadPoll, so the reader's deadline definitely expires
	// with a partial window in hand.
	time.Sleep(2 * fifoReadPoll)
	if _, err := w.Write(pcm[100:]); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}

	frame, ok := emitter.waitForFrame(3 * time.Second)
	if !ok {
		t.Fatal("no spectrum frame emitted after a full window was written in two chunks")
	}

	wantRMS := amp / math.Sqrt2
	if math.Abs(frame.RMSL-wantRMS) > 0.02 {
		t.Errorf("RMSL = %.4f, want %.4f ± 0.02 — window misaligned or channels swapped", frame.RMSL, wantRMS)
	}
	if frame.RMSR > 0.02 {
		t.Errorf("RMSR = %.4f, want ~0 (right channel was written as silence) — channels swapped", frame.RMSR)
	}
}
