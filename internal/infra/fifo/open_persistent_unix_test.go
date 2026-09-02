//go:build unix

package fifo

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func mkfifo(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p")
	if err := syscall.Mkfifo(path, 0o666); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	return path
}

// A writer that attaches, writes, and detaches must not end the stream. This
// is the property whose absence killed AirPlay metadata: shairport-sync only
// attaches its write end when a reader is already there, so a reader that
// hangs up between sessions is a reader the writer can rarely find.
func TestOpenPersistentSurvivesWriterSessions(t *testing.T) {
	path := mkfifo(t)

	f, err := OpenPersistent(path)
	if err != nil {
		t.Fatalf("OpenPersistent: %v", err)
	}
	defer func() { _ = f.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewReader(ctx, f, 20*time.Millisecond)

	for i, want := range []string{"first", "second", "third"} {
		w, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("session %d: open writer: %v", i, err)
		}
		if _, err := io.WriteString(w, want); err != nil {
			t.Fatalf("session %d: write: %v", i, err)
		}
		// Detaching is the interesting part: with a read-only read end this
		// is where the reader would see EOF and close.
		_ = w.Close()

		buf := make([]byte, len(want))
		if _, err := io.ReadFull(r, buf); err != nil {
			t.Fatalf("session %d: read after writer detached: %v", i, err)
		}
		if got := string(buf); got != want {
			t.Fatalf("session %d: got %q, want %q", i, got, want)
		}
	}
}

// With no writer at all, the read must park rather than report EOF — the
// daemon starts before shairport-sync on a cold boot.
func TestOpenPersistentNoEOFWithoutWriter(t *testing.T) {
	path := mkfifo(t)

	f, err := OpenPersistent(path)
	if err != nil {
		t.Fatalf("OpenPersistent: %v", err)
	}
	defer func() { _ = f.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err = NewReader(ctx, f, 20*time.Millisecond).Read(make([]byte, 8))
	if errors.Is(err, io.EOF) {
		t.Fatal("read reported EOF with no writer attached; the read end is not persistent")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
}

// Open, by contrast, still reports EOF — the behaviour OpenPersistent exists
// to avoid. Pinning it keeps the distinction between the two honest.
func TestOpenReportsEOFWithoutWriter(t *testing.T) {
	path := mkfifo(t)

	f, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	if _, err := NewReader(ctx, f, 20*time.Millisecond).Read(make([]byte, 8)); !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF from a read-only FIFO with no writer, got %v", err)
	}
}

// Cancellation must still be the exit, since the stream now has no natural end.
func TestOpenPersistentRespectsCancel(t *testing.T) {
	path := mkfifo(t)

	f, err := OpenPersistent(path)
	if err != nil {
		t.Fatalf("OpenPersistent: %v", err)
	}
	defer func() { _ = f.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	r := NewReader(ctx, f, 20*time.Millisecond)

	done := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 8))
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read did not observe cancellation")
	}
}
