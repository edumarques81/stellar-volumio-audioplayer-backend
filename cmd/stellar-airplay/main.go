// Command stellar-airplay is the Pi-side daemon that tails the
// shairport-sync metadata pipe (default /tmp/shairport-sync-metadata),
// parses the XML-framed items, batches them into delta payloads, and
// POSTs them to the Mac stellar backend at /internal/airplay/{state,
// heartbeat}.
//
// Configuration:
//
//	-mac-url   URL prefix of the Mac backend ingest endpoint. Defaults to
//	           STELLAR_AIRPLAY_MAC_URL env, then
//	           "http://192.168.86.84:3000/internal/airplay".
//
//	           KNOWN FRAGILITY: this is a pinned LAN IP. When the Mac's
//	           DHCP lease shifts (it did on 2026-05-27: .221 → .84), the
//	           daemon silently fails every POST until /etc/stellar-airplay/env
//	           is updated. An mDNS hostname (`Eduardos-Laptop.local`)
//	           cannot replace this because the daemon binary is built
//	           with CGO_ENABLED=0, so Go's pure resolver bypasses NSS
//	           and doesn't speak mDNS. Phase G follow-up: add Bonjour
//	           discovery via grandcat/zeroconf (browses `_stellar._tcp`,
//	           which the Mac already advertises).
//	-pipe      Path to the shairport metadata pipe. Defaults to
//	           STELLAR_AIRPLAY_METADATA_PIPE env, then
//	           "/tmp/shairport-sync-metadata".
//	-heartbeat Heartbeat tick interval. Default 2s; must be < the Mac's
//	           STELLAR_AIRPLAY_SESSION_TIMEOUT_MS (default 5s).
//
// Auth:
//
//	STELLAR_AIRPLAY_KEY  Required. Shared bearer token sent in the
//	                     Authorization header of every POST. Must match
//	                     the value set on the Mac backend's
//	                     STELLAR_AIRPLAY_KEY.
//
// Resilience: if the Mac is unreachable, the forwarder retries with
// exponential backoff (500ms → 10s). State payloads queue up to size 4;
// older payloads are dropped if the queue fills. The pipe-read loop is
// never blocked by network slowness. SIGINT/SIGTERM trigger a graceful
// shutdown.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/airplay"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/fifo"
)

// (Heartbeat gating was tried via a pipe-activity timeout but proved
// unreliable: shairport-sync only writes to the metadata pipe at track
// boundaries and on volume/pause events, not continuously during
// steady-state playback. A 10-second silence between metadata frames is
// common during a normal song, which caused the daemon to stop
// heartbeating and the Mac session to expire mid-track. True
// session-end is now signalled by shairport's session-control post-hook
// script (/usr/local/bin/stellar-airplay-post.sh), which curls
// /internal/airplay/state with {"ended":true} directly to the Mac. The
// heartbeat loop here just keeps firing every 2s as long as the daemon
// process is alive.)

func main() {
	macURL := flag.String("mac-url", envOr("STELLAR_AIRPLAY_MAC_URL", "http://192.168.86.84:3000/internal/airplay"),
		"URL prefix of the Mac backend's /internal/airplay endpoints")
	pipePath := flag.String("pipe", envOr("STELLAR_AIRPLAY_METADATA_PIPE", "/tmp/shairport-sync-metadata"),
		"Path to the shairport-sync metadata pipe")
	hbInterval := flag.Duration("heartbeat", 2*time.Second, "Heartbeat interval")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	key := os.Getenv("STELLAR_AIRPLAY_KEY")
	if key == "" {
		log.Fatal().Msg("STELLAR_AIRPLAY_KEY env var is required")
	}

	log.Info().
		Str("mac_url", *macURL).
		Str("pipe", *pipePath).
		Dur("heartbeat", *hbInterval).
		Msg("stellar-airplay starting")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleSignals(cancel)

	fw := newForwarder(forwarderConfig{
		baseURL: *macURL,
		key:     key,
	})
	fw.start(ctx)

	// Heartbeat ticker — fires regardless of pipe activity, so the Mac's
	// session never silently expires while we are still attached to
	// shairport.
	go heartbeatLoop(ctx, fw, *hbInterval)

	b := newBundler(func(p map[string]interface{}) {
		fw.PostState(p)
	})
	tailLoop(ctx, *pipePath, b)

	log.Info().Msg("draining forwarder")
	fw.wait()
	log.Info().Int64("dropped", fw.droppedFrames()).Msg("stellar-airplay stopped")
}

// reopenInterval paces reopening the metadata pipe. The open no longer waits
// for a writer to attach, so a pipe with nobody on the far end — shairport
// stopped, or this daemon started first on a cold boot — reads EOF straight
// away and would spin without this.
const reopenInterval = 500 * time.Millisecond

// tailLoop tails the metadata pipe until ctx is cancelled, reopening each time
// the writer side closes so the next AirPlay session is picked up immediately.
func tailLoop(ctx context.Context, path string, b *bundler) {
	for ctx.Err() == nil {
		chunks, err := tailPipe(ctx, path, b)

		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			log.Warn().Err(err).Str("pipe", path).Msg("pipe tail terminated; will reopen")
		case chunks > 0:
			log.Info().Str("pipe", path).Int("chunks", chunks).Msg("pipe writer closed; will reopen")
		default:
			// Nobody is writing. Expected whenever shairport-sync is not
			// running, so reopening quietly beats logging twice a second.
			log.Debug().Str("pipe", path).Msg("no writer on pipe; will reopen")
		}

		if !fifo.Sleep(ctx, reopenInterval) {
			return
		}
	}
}

// tailPipe reads one writer session off the pipe, returning the number of
// metadata chunks parsed.
//
// The pipe is silent for long stretches — shairport-sync writes at track
// boundaries and on volume/pause events, not continuously — so the read has to
// be interruptible or a cancelled context is never observed and shutdown hangs
// until systemd SIGKILLs the daemon. See the fifo package for the mechanism.
func tailPipe(ctx context.Context, path string, b *bundler) (int, error) {
	f, err := fifo.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	chunks := 0
	feed := func(c airplay.Chunk) {
		chunks++
		b.Feed(c)
	}

	err = airplay.ParseStream(fifo.NewReader(ctx, f, 0), feed)
	return chunks, err
}

func heartbeatLoop(ctx context.Context, fw *forwarder, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Heartbeat unconditionally while the daemon is alive.
			// True end-of-session is detected by shairport's
			// session-control post-hook, which POSTs {ended:true}
			// directly to the Mac. See cmd/stellar-airplay/main.go
			// comment above.
			fw.PostHeartbeat()
		}
	}
}

// shutdownDeadline is how long the daemon gives itself to wind down after a
// SIGINT/SIGTERM before exiting unconditionally. It has to stay comfortably
// under systemd's TimeoutStopSec (unset in stellar-airplay.service, so the 90s
// default applies) for a stop to count as clean rather than a SIGKILL.
const shutdownDeadline = 10 * time.Second

func handleSignals(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("shutdown signal received")
	cancel()

	// Backstop. Everything on the shutdown path is now cancellable, but a
	// blocked syscall here used to cost a 90s stop timeout and a SIGKILL on
	// every deploy. Exit under our own power well before that.
	time.AfterFunc(shutdownDeadline, func() {
		log.Warn().Dur("after", shutdownDeadline).Msg("shutdown did not complete in time; forcing exit")
		os.Exit(0)
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
