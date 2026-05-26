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
//	           "http://192.168.86.221:3000/internal/airplay".
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
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/airplay"
)

func main() {
	macURL := flag.String("mac-url", envOr("STELLAR_AIRPLAY_MAC_URL", "http://192.168.86.221:3000/internal/airplay"),
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

	// Tail the pipe forever. Re-open on EOF (shairport closes the writer
	// side when the sender disconnects; we want to immediately re-block
	// for the next session).
	b := newBundler(func(p map[string]interface{}) {
		fw.PostState(p)
	})
	for {
		if err := ctx.Err(); err != nil {
			break
		}
		if err := tailPipe(ctx, *pipePath, b); err != nil {
			log.Warn().Err(err).Str("pipe", *pipePath).Msg("pipe tail terminated; will reopen")
			select {
			case <-ctx.Done():
				break
			case <-time.After(500 * time.Millisecond):
			}
		}
	}

	log.Info().Msg("draining forwarder")
	fw.wait()
	log.Info().Int64("dropped", fw.droppedFrames()).Msg("stellar-airplay stopped")
}

func tailPipe(ctx context.Context, path string, b *bundler) error {
	// Opening a FIFO read-only blocks until a writer is attached. That
	// matches what we want — shairport launches with the pipe enabled
	// and we want to wait for it on cold boot.
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	log.Info().Str("pipe", path).Msg("pipe opened; tailing")

	// ParseStream blocks until EOF. We hand it a wrapper that calls our
	// bundler.Feed for each chunk.
	stream := contextReader{ctx: ctx, r: f}
	return airplay.ParseStream(stream, b.Feed)
}

// contextReader wraps an io.Reader so reads are interrupted on ctx
// cancel via Close (we close the underlying FD in a goroutine). Simpler
// shape: the outer loop relies on the Read returning io.EOF when
// shairport closes the writer side.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func heartbeatLoop(ctx context.Context, fw *forwarder, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fw.PostHeartbeat()
		}
	}
}

func handleSignals(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("shutdown signal received")
	cancel()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
