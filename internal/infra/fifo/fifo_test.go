//go:build unix

package fifo

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// makeFIFO creates a named pipe in a temp dir and returns its path.
func makeFIFO(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo(%s): %v", path, err)
	}
	return path
}

// openWriter opens the write end of a FIFO. Non-blocking, so a test that
// forgets to open the read end fails rather than hangs.
func openWriter(t *testing.T, path string) *os.File {
	t.Helper()
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open write end of %s: %v", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// TestOpenReturnsWithoutWriter pins the property the whole package exists for:
// a plain os.Open on a writerless FIFO parks in the kernel until someone
// attaches, which is uninterruptible and is how a daemon started before its
// writer used to become unkillable.
func TestOpenReturnsWithoutWriter(t *testing.T) {
	path := makeFIFO(t)

	done := make(chan error, 1)
	go func() {
		f, err := Open(path)
		if f != nil {
			_ = f.Close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Open with no writer attached: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Open blocked with no writer attached — the O_NONBLOCK flag is not reaching the syscall")
	}
}

// TestReadUnblocksOnCancelWhileSilent is the regression test for the shutdown
// hang. A writer holds the pipe open but sends nothing — exactly what
// shairport-sync does between track boundaries, and MPD does while paused —
// so the reader is parked in read(2), where a cancelled context is invisible.
func TestReadUnblocksOnCancelWhileSilent(t *testing.T) {
	path := makeFIFO(t)

	f, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()

	openWriter(t, path) // attaches, then says nothing

	ctx, cancel := context.WithCancel(context.Background())
	r := NewReader(ctx, f, 50*time.Millisecond)

	done := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 16))
		done <- err
	}()

	// Let the read settle into the kernel before cancelling.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Read returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not return after cancel — the reader is still parked in a blocking read")
	}
}

// TestReadDeliversDataAcrossSilence checks the other half: bounding the read
// must not turn ordinary quiet periods into errors or lost bytes. The payload
// is split around a gap longer than the poll interval, so at least one read
// deadline expires mid-message.
func TestReadDeliversDataAcrossSilence(t *testing.T) {
	path := makeFIFO(t)

	f, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()

	w := openWriter(t, path)

	want := []byte("<item><type>core</type></item>")
	split := 7

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewReader(ctx, f, 50*time.Millisecond)

	go func() {
		_, _ = w.Write(want[:split])
		time.Sleep(150 * time.Millisecond) // three expired deadlines
		_, _ = w.Write(want[split:])
	}()

	got := make([]byte, len(want))
	n, err := io.ReadFull(r, got)
	if err != nil {
		t.Fatalf("ReadFull: %v (read %d of %d bytes)", err, n, len(want))
	}
	if !bytes.Equal(got, want) {
		t.Errorf("read %q, want %q", got, want)
	}
}

// TestReadReturnsEOFWhenWriterCloses keeps the tail-and-reopen loops working:
// they distinguish "the sender went away, reopen" from a real fault by the
// error, so EOF must survive the wrapper rather than be retried forever.
func TestReadReturnsEOFWhenWriterCloses(t *testing.T) {
	path := makeFIFO(t)

	f, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()

	w := openWriter(t, path)
	if _, err := w.Write([]byte("bye")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewReader(ctx, f, 50*time.Millisecond)

	buf := make([]byte, 16)
	n, err := r.Read(buf)
	if err != nil || string(buf[:n]) != "bye" {
		t.Fatalf("first read = (%q, %v), want (\"bye\", nil)", buf[:n], err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := r.Read(buf)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("read after writer closed = %v, want io.EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read after writer closed never returned — EOF is being swallowed as a retryable condition")
	}
}

// TestSleepReturnsFalseOnCancel covers the helper the retry paths use to pace
// themselves without reintroducing an uninterruptible wait.
func TestSleepReturnsFalseOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	if Sleep(ctx, 5*time.Second) {
		t.Fatal("Sleep reported it slept the full duration despite cancellation")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Sleep took %s to notice cancellation", elapsed)
	}

	if !Sleep(context.Background(), 10*time.Millisecond) {
		t.Fatal("Sleep reported cancellation on a live context")
	}
}
