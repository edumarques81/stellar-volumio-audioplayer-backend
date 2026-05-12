// Command stellar-spectrum is a tiny daemon that runs on the Raspberry Pi
// alongside MPD. It reads /tmp/mpd_spectrum.fifo, computes per-channel L/R
// FFT magnitudes via the shared internal/infra/spectrum package, and POSTs
// each frame to the Mac-hosted Stellar backend at /internal/spectrum.
//
// Configuration:
//
//	-mac-url   URL of the Mac backend ingest endpoint. Defaults to
//	           STELLAR_MAC_URL env, then "http://192.168.86.221:3000/internal/spectrum".
//	-fifo      Path to MPD's spectrum FIFO. Defaults to /tmp/mpd_spectrum.fifo.
//	-fps       Target frames per second. Defaults to 20 (CAVA reference).
//	-numbins   Number of log-grouped output bins. Defaults to 64.
//
// Auth:
//
//	STELLAR_SPECTRUM_KEY  Required. Shared bearer token sent in the
//	                      Authorization header of every POST. Must match
//	                      the value set on the Mac backend.
//
// Resilience: if the Mac is unreachable, the forwarder retries with
// exponential backoff (500ms → 10s, same shape as the MPD idle watcher's
// nextBackoff at internal/infra/mpd/client.go:474) and drops the frame
// only if the in-flight queue (size 4) fills up. The FFT compute loop is
// never blocked by network slowness.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/spectrum"
)

func main() {
	macURL := flag.String("mac-url", envOr("STELLAR_MAC_URL", "http://192.168.86.221:3000/internal/spectrum"),
		"URL of the Mac backend's /internal/spectrum endpoint")
	fifo := flag.String("fifo", envOr("STELLAR_SPECTRUM_FIFO", "/tmp/mpd_spectrum.fifo"),
		"Path to MPD spectrum FIFO")
	fps := flag.Int("fps", envIntOr("STELLAR_SPECTRUM_FPS", 20), "Target frames per second")
	numBins := flag.Int("numbins", envIntOr("STELLAR_SPECTRUM_NUMBINS", 64), "Number of output bins")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	key := os.Getenv("STELLAR_SPECTRUM_KEY")
	if key == "" {
		log.Fatal().Msg("STELLAR_SPECTRUM_KEY env var is required")
	}

	log.Info().
		Str("mac_url", *macURL).
		Str("fifo", *fifo).
		Int("fps", *fps).
		Int("num_bins", *numBins).
		Msg("stellar-spectrum starting")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// SIGINT/SIGTERM → graceful cancel.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Info().Str("signal", sig.String()).Msg("shutdown signal received")
		cancel()
	}()

	fw := newForwarder(forwarderConfig{
		url: *macURL,
		key: key,
	})
	fw.start(ctx)

	streamer := spectrum.New(spectrum.Config{
		FIFOPath:   *fifo,
		SampleRate: 44100,
		FFTSize:    2048,
		NumBins:    *numBins,
		FPS:        *fps,
	})
	streamer.Start(ctx, fw)

	// Block until ctx is done, then tear down in reverse order.
	<-ctx.Done()
	log.Info().Msg("stopping spectrum streamer")
	streamer.Stop()
	log.Info().Msg("draining forwarder")
	fw.wait()
	log.Info().Int64("dropped_frames", fw.droppedFrames()).Msg("stellar-spectrum stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
