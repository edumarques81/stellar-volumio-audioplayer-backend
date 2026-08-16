// Package spectrum captures audio from MPD's FIFO output, performs FFT analysis,
// and streams per-channel L/R frequency bin data to the frontend via Socket.IO.
//
// Two consumers use this package:
//
//   - cmd/stellar — the main backend, when it runs co-located with MPD on the Pi
//     (legacy / development path). It calls Streamer.Start with a SocketEmitter
//     that broadcasts directly to all connected Socket.IO clients.
//
//   - cmd/stellar-spectrum — the tiny Pi-side daemon introduced in M1.B. It
//     reads the same FIFO locally and forwards each computed frame to the
//     Mac-hosted backend over HTTP. The daemon supplies its own Emitter that
//     POSTs each payload, so the FFT/window/bin code is shared verbatim.
//
// Integration: see README.md in this directory.
package spectrum

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"math"
	"math/cmplx"
	"os"
	"sync"
	"time"

	"github.com/madelynnblue/go-dsp/fft"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/fifo"
)

// Config controls the spectrum analyzer parameters.
type Config struct {
	FIFOPath   string // Path to MPD FIFO (e.g. "/tmp/mpd_spectrum.fifo")
	SampleRate int    // PCM sample rate (typically 44100)
	FFTSize    int    // FFT window size (power of 2, e.g. 2048)
	NumBins    int    // Number of output frequency bins (e.g. 64)
	FPS        int    // Target frames per second (e.g. 20-30)
}

// SpectrumData is the payload emitted via Socket.IO as "pushSpectrum".
//
// The L/R fields (BinsL/BinsR, PeakL/PeakR, RMSL/RMSR, SampleRate, TS) are
// the canonical shape introduced in M1.B for the L/R VU meter.
//
// Bins/Peak/RMS are kept as a transitional mono fallback (left+right)/2
// so any pre-M1.B frontend consumer that subscribes to `pushSpectrum`
// does not crash on missing fields. Deprecated — remove once M1.E ships.
//
//nolint:revive // SpectrumData stutters with package name but is the
// established public type and renaming would break callers.
type SpectrumData struct {
	// Per-channel data (M1.B+)
	BinsL      []float64 `json:"binsL"`
	BinsR      []float64 `json:"binsR"`
	PeakL      float64   `json:"peakL"`
	PeakR      float64   `json:"peakR"`
	RMSL       float64   `json:"rmsL"`
	RMSR       float64   `json:"rmsR"`
	SampleRate int       `json:"sampleRate"`
	TS         int64     `json:"ts"` // Unix milliseconds at emit time

	// Deprecated mono fallback: (left+right)/2. Will be removed after M1.E.
	Bins []float64 `json:"bins"`
	Peak float64   `json:"peak"`
	RMS  float64   `json:"rms"`
}

// SocketEmitter is the interface the spectrum streamer needs to broadcast events.
// This matches the typical Socket.IO server's BroadcastToNamespace or similar,
// and is also implemented by the HTTP-forwarding emitter used by
// cmd/stellar-spectrum.
type SocketEmitter interface {
	BroadcastToAll(event string, data interface{})
}

// Streamer captures audio from a FIFO pipe, runs FFT, and emits spectrum data.
type Streamer struct {
	cfg    Config
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Pre-computed Hann window coefficients
	hannWindow []float64
	// Pre-computed log-spaced bin boundaries
	binEdges []int
}

// New creates a new spectrum Streamer with the given configuration.
func New(cfg Config) *Streamer {
	if cfg.FIFOPath == "" {
		cfg.FIFOPath = "/tmp/mpd_spectrum.fifo"
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 44100
	}
	if cfg.FFTSize <= 0 {
		cfg.FFTSize = 2048
	}
	if cfg.NumBins <= 0 {
		cfg.NumBins = 64
	}
	if cfg.FPS <= 0 {
		cfg.FPS = 30
	}

	s := &Streamer{cfg: cfg}
	s.precomputeWindow()
	s.precomputeBinEdges()
	return s
}

// precomputeWindow generates Hann window coefficients for the FFT size.
func (s *Streamer) precomputeWindow() {
	s.hannWindow = make([]float64, s.cfg.FFTSize)
	for i := range s.hannWindow {
		s.hannWindow[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(s.cfg.FFTSize-1)))
	}
}

// precomputeBinEdges creates logarithmically-spaced bin boundaries mapping
// FFT frequency indices to output bins. Low frequencies get fewer FFT bins,
// high frequencies get more — matching human perception.
func (s *Streamer) precomputeBinEdges() {
	halfFFT := s.cfg.FFTSize / 2
	s.binEdges = make([]int, s.cfg.NumBins+1)

	// Map bins logarithmically across the useful FFT range
	// Skip DC (index 0) and start from index 1
	minFreqIdx := 1.0
	maxFreqIdx := float64(halfFFT)

	for i := 0; i <= s.cfg.NumBins; i++ {
		t := float64(i) / float64(s.cfg.NumBins)
		idx := minFreqIdx * math.Pow(maxFreqIdx/minFreqIdx, t)
		s.binEdges[i] = int(math.Round(idx))
		if s.binEdges[i] > halfFFT {
			s.binEdges[i] = halfFFT
		}
	}
}

// Start begins reading from the FIFO and emitting spectrum data via the emitter.
// It runs in a background goroutine. Call Stop() to shut down.
func (s *Streamer) Start(ctx context.Context, emitter SocketEmitter) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go s.run(ctx, emitter)
}

const (
	// fifoReadPoll bounds a single read on the FIFO. See fifo.DefaultPollInterval.
	fifoReadPoll = fifo.DefaultPollInterval

	// fifoRetryInterval is the pause before reopening after the writer goes
	// away, so a permanently absent FIFO does not spin.
	fifoRetryInterval = 2 * time.Second

	// stopGrace bounds Stop's wait for the reader. The reader checks its
	// context every fifoReadPoll, so reaching this timeout means something
	// is genuinely stuck — and a stuck reader must not be able to hold up
	// process exit again. See the fifo package comment for the history.
	stopGrace = 5 * time.Second
)

// Stop shuts down the spectrum streamer and waits for it to finish, giving up
// after stopGrace so it can never block the caller indefinitely.
func (s *Streamer) Stop() {
	if s.cancel != nil {
		s.cancel()
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(stopGrace):
		log.Printf("[Spectrum] Reader did not stop within %s — abandoning it", stopGrace)
	}
}

func (s *Streamer) run(ctx context.Context, emitter SocketEmitter) {
	defer s.wg.Done()

	frameInterval := time.Duration(float64(time.Second) / float64(s.cfg.FPS))

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		pipe, err := fifo.Open(s.cfg.FIFOPath)
		if err != nil {
			log.Printf("[Spectrum] Failed to open FIFO %s: %v (retrying in %s)", s.cfg.FIFOPath, err, fifoRetryInterval)
			if !fifo.Sleep(ctx, fifoRetryInterval) {
				return
			}
			continue
		}

		frames := s.streamFromFIFO(ctx, pipe, emitter, frameInterval)
		_ = pipe.Close()

		// Only report the transition when audio was actually flowing. A
		// non-blocking open succeeds even with no writer attached, so the
		// no-MPD case would otherwise log every retry forever.
		if frames > 0 {
			log.Printf("[Spectrum] FIFO writer closed after %d frames", frames)
		}

		if !fifo.Sleep(ctx, fifoRetryInterval) {
			return
		}
	}
}

// streamFromFIFO reads and emits until the writer goes away or ctx is
// cancelled, returning the number of frames emitted.
func (s *Streamer) streamFromFIFO(ctx context.Context, pipe *os.File, emitter SocketEmitter, frameInterval time.Duration) int {
	// Each PCM frame is 4 bytes: 2 bytes left + 2 bytes right (16-bit stereo)
	bytesPerFrame := 4
	bufSize := s.cfg.FFTSize * bytesPerFrame
	buf := make([]byte, bufSize)
	left := make([]float64, s.cfg.FFTSize)
	right := make([]float64, s.cfg.FFTSize)

	reader := fifo.NewReader(ctx, pipe, fifoReadPoll)

	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	frames := 0
	for {
		select {
		case <-ctx.Done():
			return frames
		case <-ticker.C:
		}

		// Read enough PCM data for one FFT window. ReadFull keeps the bytes
		// it has already collected when an individual read comes back short,
		// which is what preserves frame alignment: the stream is interleaved
		// L/R, so restarting a partly-filled window would shift it by a
		// non-frame-aligned amount and swap the channels for good.
		n, err := io.ReadFull(reader, buf)
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
				// FIFO writer (MPD) closed — return to retry loop
			default:
				log.Printf("[Spectrum] Read error: %v", err)
			}
			return frames
		}

		if frames == 0 {
			log.Printf("[Spectrum] FIFO %s streaming at %d fps", s.cfg.FIFOPath, s.cfg.FPS)
		}

		// Split 16-bit stereo PCM into normalised L/R float channels.
		numFrames := n / bytesPerFrame
		decodeStereoPCM(buf, numFrames, s.cfg.FFTSize, left, right)

		data := s.Process(left, right)
		emitter.BroadcastToAll("pushSpectrum", data)
		frames++
	}
}

// decodeStereoPCM splits an interleaved 16-bit little-endian stereo PCM
// buffer into two normalised float64 channel slices. Frames past numFrames
// (i.e. trailing portion of the buffer that wasn't filled by ReadFull) are
// zeroed.
func decodeStereoPCM(buf []byte, numFrames, fftSize int, left, right []float64) {
	limit := numFrames
	if limit > fftSize {
		limit = fftSize
	}
	for i := 0; i < limit; i++ {
		offset := i * 4
		l := int16(binary.LittleEndian.Uint16(buf[offset : offset+2]))
		r := int16(binary.LittleEndian.Uint16(buf[offset+2 : offset+4]))
		left[i] = float64(l) / 32768.0
		right[i] = float64(r) / 32768.0
	}
	for i := limit; i < fftSize; i++ {
		left[i] = 0
		right[i] = 0
	}
}

// Process runs the Hann window + FFT on a single L/R frame pair and returns
// the per-channel SpectrumData. Exposed publicly so tests can feed
// synthesized PCM directly without involving a FIFO, and so cmd/stellar-spectrum
// can reuse the exact same compute path the legacy in-process emitter uses.
//
// left and right must be the same length as cfg.FFTSize, normalised to [-1, 1].
func (s *Streamer) Process(left, right []float64) SpectrumData {
	// Defensive copies because we mutate them in-place for windowing.
	winL := make([]float64, s.cfg.FFTSize)
	winR := make([]float64, s.cfg.FFTSize)
	for i := 0; i < s.cfg.FFTSize; i++ {
		if i < len(left) {
			winL[i] = left[i] * s.hannWindow[i]
		}
		if i < len(right) {
			winR[i] = right[i] * s.hannWindow[i]
		}
	}

	specL := fft.FFTReal(winL)
	specR := fft.FFTReal(winR)

	binsL, peakL := s.computeChannelBins(specL)
	binsR, peakR := s.computeChannelBins(specR)

	// RMS must reflect actual signal level, not spectral shape. Compute it
	// in the time domain from the (normalised, un-windowed) PCM samples;
	// the bin-derived RMS we used to return was always relative to the
	// loudest bin and pegged ~0.4 regardless of input level.
	rmsL := timeDomainRMS(left)
	rmsR := timeDomainRMS(right)

	// Transitional mono fallback for legacy consumers.
	mono := make([]float64, s.cfg.NumBins)
	for i := range mono {
		mono[i] = (binsL[i] + binsR[i]) / 2.0
	}
	monoPeak := peakL
	if peakR > monoPeak {
		monoPeak = peakR
	}
	monoRMS := (rmsL + rmsR) / 2.0

	return SpectrumData{
		BinsL:      binsL,
		BinsR:      binsR,
		PeakL:      peakL,
		PeakR:      peakR,
		RMSL:       rmsL,
		RMSR:       rmsR,
		SampleRate: s.cfg.SampleRate,
		TS:         time.Now().UnixMilli(),
		Bins:       mono,
		Peak:       monoPeak,
		RMS:        monoRMS,
	}
}

// computeChannelBins converts raw FFT complex output for a single channel
// into logarithmically-grouped frequency bins normalised 0.0–1.0, and the
// peak bin value. RMS is intentionally not returned here: a bin-normalised
// RMS describes spectral shape, not signal level — compute RMS in the time
// domain (see timeDomainRMS).
func (s *Streamer) computeChannelBins(spectrum []complex128) (bins []float64, peak float64) {
	bins = make([]float64, s.cfg.NumBins)

	// Compute magnitude for each FFT bin (only first half — real input)
	halfFFT := s.cfg.FFTSize / 2
	magnitudes := make([]float64, halfFFT)
	scale := 2.0 / float64(s.cfg.FFTSize)
	for i := 0; i < halfFFT && i < len(spectrum); i++ {
		magnitudes[i] = cmplx.Abs(spectrum[i]) * scale
	}

	// Group into logarithmically-spaced output bins
	var maxMag float64
	for i := 0; i < s.cfg.NumBins; i++ {
		lo := s.binEdges[i]
		hi := s.binEdges[i+1]
		if hi <= lo {
			hi = lo + 1
		}
		if hi > halfFFT {
			hi = halfFFT
		}

		var sum float64
		count := 0
		for j := lo; j < hi; j++ {
			sum += magnitudes[j]
			count++
		}
		if count > 0 {
			bins[i] = sum / float64(count)
		}
		if bins[i] > maxMag {
			maxMag = bins[i]
		}
	}

	// Normalize bins to 0.0–1.0 and record the peak.
	if maxMag > 0 {
		for i := range bins {
			bins[i] /= maxMag
			if bins[i] > peak {
				peak = bins[i]
			}
		}
	}

	return bins, peak
}

// timeDomainRMS returns the RMS of the [-1, 1]-normalised PCM samples.
// Result is in 0..1 linear amplitude — exactly what the frontend's
// rmsToBarFill (−60 dBFS floor → 0 dBFS full) expects.
func timeDomainRMS(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sumSq float64
	for _, x := range samples {
		sumSq += x * x
	}
	return math.Sqrt(sumSq / float64(len(samples)))
}
