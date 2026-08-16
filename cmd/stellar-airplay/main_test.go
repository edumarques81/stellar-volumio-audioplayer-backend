//go:build unix

package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// metadataItem renders one shairport-sync metadata item in the wire format
// internal/infra/airplay's parser expects.
func metadataItem(typ, code, payload string) string {
	t := hex.EncodeToString([]byte(typ))
	c := hex.EncodeToString([]byte(code))
	if payload == "" {
		return fmt.Sprintf("<item><type>%s</type><code>%s</code><length>0</length></item>\n", t, c)
	}
	enc := base64.StdEncoding.EncodeToString([]byte(payload))
	return fmt.Sprintf("<item><type>%s</type><code>%s</code><length>%d</length>\n<data encoding=\"base64\">\n%s</data></item>\n",
		t, c, len(payload), enc)
}

func makeMetadataPipe(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shairport-sync-metadata")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo(%s): %v", path, err)
	}
	return path
}

// openPipeWriter attaches a writer that stays attached, standing in for
// shairport-sync.
//
// O_RDWR rather than O_WRONLY: a write-only open fails with ENXIO whenever no
// reader is currently attached, and tailLoop closes and reopens its read end
// between attempts, so a write-only open would race that gap. Holding both
// ends also keeps the pipe from reporting EOF, which is what "shairport is
// connected but has nothing to say" looks like from the reader's side.
func openPipeWriter(t *testing.T, path string) *os.File {
	t.Helper()
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open write end of %s: %v", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// TestTailLoopReturnsOnCancelWhilePipeIsSilent is the regression test for the
// shutdown hang. shairport-sync holds the metadata pipe open continuously but
// only writes at track boundaries and on volume/pause events, so between songs
// the daemon sits in read(2) — where a cancelled context is invisible. The
// tail loop then never returned, main never reached fw.wait(), and systemd
// SIGKILLed the daemon after its 90s stop timeout on every deploy.
func TestTailLoopReturnsOnCancelWhilePipeIsSilent(t *testing.T) {
	path := makeMetadataPipe(t)
	b := newBundler(func(map[string]interface{}) {})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		tailLoop(ctx, path, b)
		close(done)
	}()

	// A writer that attaches and then says nothing — steady-state playback.
	openPipeWriter(t, path)

	// Let the reader settle into the blocking read before cancelling.
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("tailLoop did not return after cancel — the reader is still parked in a blocking read on the metadata pipe")
	}
}

// TestTailLoopReturnsWithNoWriter covers a cold boot, where the daemon starts
// before shairport-sync and nothing has ever attached to the pipe.
func TestTailLoopReturnsWithNoWriter(t *testing.T) {
	path := makeMetadataPipe(t)
	b := newBundler(func(map[string]interface{}) {})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		tailLoop(ctx, path, b)
		close(done)
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("tailLoop did not return with no writer attached — the reader is still parked in open()")
	}
}

// TestTailLoopDeliversMetadataAcrossSilence proves the interruptible read did
// not cost us the actual job: a bundle split by a gap longer than the read
// poll interval must still arrive intact.
func TestTailLoopDeliversMetadataAcrossSilence(t *testing.T) {
	path := makeMetadataPipe(t)

	var mu sync.Mutex
	var payloads []map[string]interface{}
	b := newBundler(func(p map[string]interface{}) {
		mu.Lock()
		defer mu.Unlock()
		payloads = append(payloads, p)
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		tailLoop(ctx, path, b)
		close(done)
	}()
	// Stop the loop before t.TempDir is removed, or it logs its way through
	// the teardown complaining the pipe has vanished.
	t.Cleanup(func() {
		cancel()
		<-done
	})

	w := openPipeWriter(t, path)

	// One bundle: begin, title, end. The pause straddles a read deadline.
	if _, err := w.Write([]byte(metadataItem("ssnc", "mdst", "") + metadataItem("core", "minm", "Blue in Green"))); err != nil {
		t.Fatalf("write first half: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if _, err := w.Write([]byte(metadataItem("ssnc", "mden", ""))); err != nil {
		t.Fatalf("write second half: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(payloads)
		var got map[string]interface{}
		if n > 0 {
			got = payloads[0]
		}
		mu.Unlock()
		if n > 0 {
			if title, ok := got["title"].(string); !ok || title != "Blue in Green" {
				t.Fatalf("payload title = %v, want %q", got["title"], "Blue in Green")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no bundle delivered — metadata written across a read-deadline boundary was lost")
}
