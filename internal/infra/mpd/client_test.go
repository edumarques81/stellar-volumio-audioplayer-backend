package mpd_test

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
)

func TestNewClient(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	if client == nil {
		t.Error("NewClient should return a non-nil client")
	}
}

func TestClientConnectFailure(t *testing.T) {
	// Test connection to non-existent server
	client := mpd.NewClient("localhost", 16600, "") // Wrong port

	err := client.Connect()
	if err == nil {
		t.Error("Connect should fail for non-existent server")
		client.Close()
	}
}

func TestClientPingWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Ping()
	if err == nil {
		t.Error("Ping should fail when not connected")
	}
}

func TestClientStatusWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.Status()
	if err == nil {
		t.Error("Status should fail when not connected")
	}
}

func TestClientPlayWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Play(0)
	if err == nil {
		t.Error("Play should fail when not connected")
	}
}

func TestClientPauseWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Pause(true)
	if err == nil {
		t.Error("Pause should fail when not connected")
	}
}

func TestClientStopWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Stop()
	if err == nil {
		t.Error("Stop should fail when not connected")
	}
}

func TestClientNextWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Next()
	if err == nil {
		t.Error("Next should fail when not connected")
	}
}

func TestClientPreviousWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Previous()
	if err == nil {
		t.Error("Previous should fail when not connected")
	}
}

func TestClientSetVolumeWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.SetVolume(50)
	if err == nil {
		t.Error("SetVolume should fail when not connected")
	}
}

func TestClientSetRandomWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.SetRandom(true)
	if err == nil {
		t.Error("SetRandom should fail when not connected")
	}
}

func TestClientSetRepeatWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.SetRepeat(true)
	if err == nil {
		t.Error("SetRepeat should fail when not connected")
	}
}

func TestClientPlaylistInfoWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.PlaylistInfo()
	if err == nil {
		t.Error("PlaylistInfo should fail when not connected")
	}
}

func TestClientClearWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Clear()
	if err == nil {
		t.Error("Clear should fail when not connected")
	}
}

func TestClientAddWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Add("test.flac")
	if err == nil {
		t.Error("Add should fail when not connected")
	}
}

func TestClientDetectCapabilitiesWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.DetectCapabilities()
	if err == nil {
		t.Error("DetectCapabilities should fail when not connected")
	}
}

func TestClientWatchDatabaseWithoutConnect(t *testing.T) {
	// Watch() no longer returns an error when MPD is unreachable at call time;
	// instead it enters the retry backoff loop. WatchDatabase delegates to Watch,
	// so it too must succeed and return a valid channel.
	client := mpd.NewClient("localhost", 6600, "")

	ch, err := client.WatchDatabase()
	if err != nil {
		t.Errorf("WatchDatabase returned error when MPD is down: %v — want nil (should retry internally)", err)
	}
	if ch == nil {
		t.Error("WatchDatabase returned nil channel; want a valid channel for later event delivery")
	}
	// Close immediately so the goroutine exits and the channel drains.
	if cerr := client.Close(); cerr != nil {
		t.Errorf("Close returned error: %v", cerr)
	}
	// Wait for the channel to close (goroutine has exited).
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Error("channel did not close after Client.Close")
	}
}

func TestCapabilityFlagsDefaults(t *testing.T) {
	// Test that default CapabilityFlags has false values
	flags := mpd.CapabilityFlags{}

	if flags.HasReadPicture {
		t.Error("Default HasReadPicture should be false")
	}
	if flags.HasAlbumArt {
		t.Error("Default HasAlbumArt should be false")
	}
	if flags.ProtocolVersion != "" {
		t.Error("Default ProtocolVersion should be empty")
	}
}

func TestClientGetDatabaseStatsWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.GetDatabaseStats()
	if err == nil {
		t.Error("GetDatabaseStats should fail when not connected")
	}
}

func TestClientCountAlbumsWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.CountAlbums()
	if err == nil {
		t.Error("CountAlbums should fail when not connected")
	}
}

func TestClientCountArtistsWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.CountArtists()
	if err == nil {
		t.Error("CountArtists should fail when not connected")
	}
}

// Tests for Volumio integration queue manipulation methods

func TestClientAddIdWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.AddId("test.flac", -1)
	if err == nil {
		t.Error("AddId should fail when not connected")
	}
}

func TestClientAddIdAtPositionWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.AddId("test.flac", 0)
	if err == nil {
		t.Error("AddId at position should fail when not connected")
	}
}

func TestClientMoveWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Move(0, 1)
	if err == nil {
		t.Error("Move should fail when not connected")
	}
}

func TestClientDeleteWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Delete(0)
	if err == nil {
		t.Error("Delete should fail when not connected")
	}
}

func TestClientGetCurrentPositionWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.GetCurrentPosition()
	if err == nil {
		t.Error("GetCurrentPosition should fail when not connected")
	}
}

func TestClientGetQueueLengthWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.GetQueueLength()
	if err == nil {
		t.Error("GetQueueLength should fail when not connected")
	}
}

// TestNormalizeGenre exercises the multi-value genre handling required by
// the Library album-meta strip: MPD may return a `;`-separated string for
// multi-genre tracks, and the frontend wants `Ambient / Post-Rock` style.
func TestNormalizeGenre(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "single genre", in: "Ambient", want: "Ambient"},
		{name: "trims surrounding whitespace", in: "  Ambient  ", want: "Ambient"},
		{name: "semicolon-space split", in: "Ambient; Post-Rock", want: "Ambient / Post-Rock"},
		{name: "bare semicolon split", in: "Ambient;Post-Rock", want: "Ambient / Post-Rock"},
		{name: "three values", in: "Ambient; Post-Rock; Drone", want: "Ambient / Post-Rock / Drone"},
		{name: "trims values around separators", in: " Ambient ;  Post-Rock ", want: "Ambient / Post-Rock"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mpd.NormalizeGenre(tc.in)
			if got != tc.want {
				t.Errorf("NormalizeGenre(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// fakeMPDServer is a minimal MPD-protocol stub used to test the watcher
// reconnect path. It accepts TCP connections, sends the MPD greeting, and
// then runs a per-connection scripted handler.
//
// Each handler is invoked with the accept count (1-based) and the connection.
// Handlers may close the connection abruptly (broken-pipe simulation) or
// respond to "idle" and other commands. When all scripted handlers have been
// consumed, additional connections are closed immediately.
type fakeMPDServer struct {
	t        *testing.T
	listener net.Listener
	handlers []func(connNum int, conn net.Conn)
	accepts  atomic.Int32
	wg       sync.WaitGroup
}

// startFakeMPD starts a fake MPD server on a random localhost port.
// The handlers are applied in order (handler[0] for the first accept,
// handler[1] for the second, etc.). Any extra connections are closed.
func startFakeMPD(t *testing.T, handlers ...func(connNum int, conn net.Conn)) *fakeMPDServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeMPDServer{t: t, listener: ln, handlers: handlers}
	s.wg.Add(1)
	go s.serve()
	return s
}

func (s *fakeMPDServer) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		n := int(s.accepts.Add(1))
		s.wg.Add(1)
		go func(connNum int, c net.Conn) {
			defer s.wg.Done()
			defer c.Close()
			// Send greeting.
			if _, err := c.Write([]byte("OK MPD 0.21.0\n")); err != nil {
				return
			}
			if connNum-1 < len(s.handlers) {
				s.handlers[connNum-1](connNum, c)
			}
		}(n, conn)
	}
}

func (s *fakeMPDServer) hostPort() (string, int) {
	tcpAddr, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		s.t.Fatalf("listener addr is not TCP: %v", s.listener.Addr())
	}
	return tcpAddr.IP.String(), tcpAddr.Port
}

func (s *fakeMPDServer) close() {
	_ = s.listener.Close()
	s.wg.Wait()
}

// readCommand reads a single \n-terminated command from the connection.
func readCommand(t *testing.T, r *bufio.Reader) (string, error) {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// TestWatchReconnectsOnBrokenPipe verifies that when the underlying MPD
// watcher connection dies (simulated by closing the server side of the
// socket), the Watch() goroutine creates a NEW watcher within ~600ms and
// resumes event delivery on the returned channel.
//
// This is a regression test for the bug where the goroutine read from a
// closed-over *Watcher and never recreated it after a broken-pipe error,
// causing track-end auto-advance to permanently stick.
func TestWatchReconnectsOnBrokenPipe(t *testing.T) {
	// First connection: read the idle command, then close abruptly to
	// simulate the broken-pipe failure mode observed on the Pi.
	first := func(_ int, conn net.Conn) {
		r := bufio.NewReader(conn)
		// Drain whatever the watcher sends (typically "idle <subsystems>"),
		// then slam the socket shut.
		_, _ = readCommand(t, r)
		_ = conn.Close()
	}

	// Second connection: respond to idle by reporting a "player" change.
	// This is the post-reconnect connection that should deliver the event.
	deliveredEvent := make(chan struct{}, 1)
	second := func(_ int, conn net.Conn) {
		r := bufio.NewReader(conn)
		for {
			cmd, err := readCommand(t, r)
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(cmd, "idle"):
				if _, err := conn.Write([]byte("changed: player\nOK\n")); err != nil {
					return
				}
				select {
				case deliveredEvent <- struct{}{}:
				default:
				}
			case strings.HasPrefix(cmd, "noidle"):
				_, _ = conn.Write([]byte("OK\n"))
			case cmd == "close":
				return
			default:
				_, _ = conn.Write([]byte("OK\n"))
			}
		}
	}

	server := startFakeMPD(t, first, second)
	defer server.close()

	host, port := server.hostPort()
	client := mpd.NewClient(host, port, "")
	defer client.Close()

	ch, err := client.Watch("player")
	if err != nil {
		t.Fatalf("Watch returned error: %v", err)
	}

	// Wait for the post-reconnect event. With the fix in place, the
	// watcher should be re-created within the first backoff step
	// (~500ms) and the "player" event should arrive shortly after.
	select {
	case sub, ok := <-ch:
		if !ok {
			t.Fatal("watch channel closed before event")
		}
		if sub != "player" {
			t.Errorf("got subsystem %q, want %q", sub, "player")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("did not receive event after broken-pipe reconnect; accepts=%d", server.accepts.Load())
	}

	// Verify the server actually saw a second accept (the reconnect).
	if got := server.accepts.Load(); got < 2 {
		t.Errorf("expected at least 2 connections (initial + reconnect), got %d", got)
	}

	// Sanity: confirm the second connection delivered through.
	select {
	case <-deliveredEvent:
	case <-time.After(time.Second):
		t.Error("second-connection idle response was not sent")
	}
}

// TestWatchNilDerefAfterFailedReconnect is a regression test for the
// nil-deref panic in watchLoop that kills the Mac stellar backend every time
// the Pi reboots.
//
// Sequence that triggered the bug:
//  1. MPD connection is live; watcher drains events normally.
//  2. Pi reboots → MPD TCP socket dies → watcher.Error fires.
//  3. watchLoop sets watcher = nil, sleeps, then calls mpd.NewWatcher.
//  4. Pi is still mid-reboot → NewWatcher returns an error → continue.
//  5. Outer for-loop top: code immediately selects on watcher.Event — but
//     watcher is still nil from step 3. → PANIC: nil pointer dereference.
//
// Fix: gate the inner drain block on `if watcher != nil {}`. When the
// previous reconnect failed, the loop skips straight to the next
// sleep+NewWatcher without touching the nil watcher.
//
// Test approach (Option A): use a real TCP listener that speaks just enough
// MPD protocol for mpd.NewWatcher to succeed on the first accept, then close
// the listener so that all subsequent reconnect attempts fail with ECONNREFUSED.
// We then call Client.Close() and assert it returns without panicking.
func TestWatchNilDerefAfterFailedReconnect(t *testing.T) {
	// First connection: answer the idle command, then slam the socket shut
	// to force a watcher error (simulates Pi dropping off the network).
	first := func(_ int, conn net.Conn) {
		r := bufio.NewReader(conn)
		// Drain whatever the watcher sends (typically "idle <subsystems>").
		_, _ = readCommand(t, r)
		// Abruptly close to force watcher.Error to fire.
		_ = conn.Close()
	}

	server := startFakeMPD(t, first)
	host, port := server.hostPort()

	client := mpd.NewClient(host, port, "")
	ch, err := client.Watch("player")
	if err != nil {
		t.Fatalf("Watch returned error: %v", err)
	}

	// Wait long enough for the first watcher error to be received and the
	// first reconnect to be attempted, then close the server so that
	// subsequent NewWatcher calls all fail with ECONNREFUSED.  The loop
	// will cycle through at least one failed-reconnect iteration (the
	// nil-deref path) before we signal Close().
	time.Sleep(200 * time.Millisecond)
	server.close()

	// Give the loop one more backoff cycle so it hits the watcher==nil
	// branch at the top of the outer for-loop.
	time.Sleep(700 * time.Millisecond)

	// Close() must return without panicking and within a reasonable timeout.
	done := make(chan error, 1)
	go func() { done <- client.Close() }()

	select {
	case closeErr := <-done:
		if closeErr != nil {
			t.Errorf("Close returned unexpected error: %v", closeErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Client.Close did not return within 5s — watchLoop may have panicked or hung")
	}

	// The watch channel must drain/close after Client.Close().
	for range ch {
	}
}

// TestWatchCloseDuringReconnectBackoff verifies that Client.Close() unblocks
// the watch goroutine cleanly while it is sleeping in a reconnect backoff.
// This guards against the double-close race that the lifecycle refactor
// introduced: only the goroutine may call watcher.Close() on the live
// instance, and Close() must signal it to exit rather than racing on the
// pointer.
func TestWatchCloseDuringReconnectBackoff(t *testing.T) {
	// First connection: kill the socket immediately to drive the loop
	// into its backoff-then-reconnect path.
	first := func(_ int, conn net.Conn) {
		_ = conn.Close()
	}
	// Block any subsequent connection forever so the watch loop is
	// definitely in the middle of reconnecting when Close() fires.
	hold := func(_ int, conn net.Conn) {
		// Just hang on the read until the test closes us.
		buf := make([]byte, 1024)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}

	server := startFakeMPD(t, first, hold, hold, hold)
	defer server.close()

	host, port := server.hostPort()
	client := mpd.NewClient(host, port, "")

	ch, err := client.Watch("player")
	if err != nil {
		t.Fatalf("Watch returned error: %v", err)
	}

	// Give the loop a moment to hit the broken-pipe backoff.
	time.Sleep(200 * time.Millisecond)

	// Close should not panic, hang, or double-close the watcher.
	done := make(chan error, 1)
	go func() { done <- client.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Close returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Client.Close did not return within 3s")
	}

	// The watch channel must close shortly after.
	select {
	case _, ok := <-ch:
		if ok {
			// Drain any in-flight events; loop until close.
			for range ch {
			}
		}
	case <-time.After(2 * time.Second):
		t.Error("watch channel did not close after Client.Close")
	}
}

// TestWatch_NonFatalOnInitialDialFailure verifies that Watch() does NOT return
// an error when MPD is unreachable at startup. Instead it should return a
// valid channel and enter its retry backoff loop, so the caller (StartMPDWatcher
// / main.go) can continue booting regardless of whether MPD is up.
//
// This is the boot-before-MPD-ready scenario on the Pi where stellar-backend
// starts before the mpd.service has fully initialised.
func TestWatch_NonFatalOnInitialDialFailure(t *testing.T) {
	// Point the client at a port nothing is listening on.
	client := mpd.NewClient("127.0.0.1", 19876, "")
	defer client.Close()

	ch, err := client.Watch("player", "mixer")
	if err != nil {
		t.Fatalf("Watch returned error on initial dial failure: %v — want nil (should retry internally)", err)
	}
	if ch == nil {
		t.Fatal("Watch returned nil channel; want a valid channel")
	}

	// The channel should remain open (retrying). We close the client to
	// trigger the goroutine to exit and the channel to close.
	if closeErr := client.Close(); closeErr != nil {
		t.Errorf("Close returned error: %v", closeErr)
	}

	// Drain and wait for the channel to close.
	select {
	case _, ok := <-ch:
		if ok {
			for range ch {
			}
		}
	case <-time.After(2 * time.Second):
		t.Error("watch channel did not close after Client.Close")
	}
}

// TestWatch_RetriesAfterInitialFailure verifies that after a failed initial
// dial, Watch's goroutine reconnects once MPD becomes available and delivers
// events through the returned channel.
//
// Approach: reserve a port by listening, immediately close the listener (so
// Watch's initial dial fails), then re-open a fake MPD server on the SAME
// port (OS typically makes a just-released port available again on loopback).
// The retry backoff will eventually reach the fake server and deliver an event.
func TestWatch_RetriesAfterInitialFailure(t *testing.T) {
	// Reserve a port then immediately close — the initial Watch dial will fail.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	host := addr.IP.String()
	port := addr.Port
	ln.Close()

	client := mpd.NewClient(host, port, "")
	defer client.Close()

	ch, err := client.Watch("player")
	if err != nil {
		t.Fatalf("Watch returned error on initial dial failure: %v", err)
	}

	// Bring up a fake MPD server on the same address.  The retry goroutine
	// will find it on its next backoff cycle (~500ms after startup).
	serverLn, listenErr := net.Listen("tcp", addr.String())
	if listenErr != nil {
		// Port unavailable — skip rather than fail (port-reuse is best-effort).
		t.Skipf("could not re-bind port %d: %v (port-reuse test skipped)", port, listenErr)
	}

	deliveredEvent := make(chan struct{}, 1)
	go func() {
		conn, aerr := serverLn.Accept()
		if aerr != nil {
			return
		}
		defer conn.Close()
		// Send greeting.
		if _, werr := conn.Write([]byte("OK MPD 0.21.0\n")); werr != nil {
			return
		}
		r := bufio.NewReader(conn)
		for {
			cmd, rerr := readCommand(t, r)
			if rerr != nil {
				return
			}
			switch {
			case strings.HasPrefix(cmd, "idle"):
				if _, werr := conn.Write([]byte("changed: player\nOK\n")); werr != nil {
					return
				}
				select {
				case deliveredEvent <- struct{}{}:
				default:
				}
			case strings.HasPrefix(cmd, "noidle"):
				_, _ = conn.Write([]byte("OK\n"))
			default:
				_, _ = conn.Write([]byte("OK\n"))
			}
		}
	}()
	defer serverLn.Close()

	select {
	case sub, ok := <-ch:
		if !ok {
			t.Fatal("watch channel closed before event arrived")
		}
		if sub != "player" {
			t.Errorf("got subsystem %q, want %q", sub, "player")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event did not arrive within 5s after delayed MPD startup")
	}
}
