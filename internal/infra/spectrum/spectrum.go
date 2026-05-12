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
	"io"
	"log"
	"math"
	"math/cmplx"
	"os"
	"sync"
	"time"

	"github.com/madelynnblue/go-dsp/fft"
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

// Stop shuts down the spectrum streamer and waits for it to finish.
func (s *Streamer) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
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

		// Open the FIFO — this blocks until a writer (MPD) opens the other end
		log.Printf("[Spectrum] Opening FIFO %s ...", s.cfg.FIFOPath)
		fifo, err := os.Open(s.cfg.FIFOPath)
		if err != nil {
			log.Printf("[Spectrum] Failed to open FIFO: %v (retrying in 5s)", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		log.Printf("[Spectrum] FIFO opened, streaming at %d fps", s.cfg.FPS)
		s.streamFromFIFO(ctx, fifo, emitter, frameInterval)
		_ = fifo.Close()

		// FIFO closed (MPD stopped?) — wait and retry
		log.Printf("[Spectrum] FIFO closed, retrying in 2s")
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Streamer) streamFromFIFO(ctx context.Context, fifo *os.File, emitter SocketEmitter, frameInterval time.Duration) {
	// Each PCM frame is 4 bytes: 2 bytes left + 2 bytes right (16-bit stereo)
	bytesPerFrame := 4
	bufSize := s.cfg.FFTSize * bytesPerFrame
	buf := make([]byte, bufSize)
	left := make([]float64, s.cfg.FFTSize)
	right := make([]float64, s.cfg.FFTSize)

	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// Read enough PCM data for one FFT window
		n, err := io.ReadFull(fifo, buf)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				// FIFO writer (MPD) closed — return to retry loop
				return
			}
			log.Printf("[Spectrum] Read error: %v", err)
			return
		}

		// Split 16-bit stereo PCM into normalised L/R float channels.
		numFrames := n / bytesPerFrame
		decodeStereoPCM(buf, numFrames, s.cfg.FFTSize, left, right)

		data := s.Process(left, right)
		emitter.BroadcastToAll("pushSpectrum", data)
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

	binsL, peakL, rmsL := s.computeChannelBins(specL)
	binsR, peakR, rmsR := s.computeChannelBins(specR)

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
// into logarithmically-grouped frequency bins normalised 0.0–1.0, plus
// per-channel peak and RMS.
func (s *Streamer) computeChannelBins(spectrum []complex128) (bins []float64, peak, rms float64) {
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

	// Normalize bins to 0.0–1.0 and compute peak/RMS.
	var sumSq float64
	if maxMag > 0 {
		for i := range bins {
			bins[i] /= maxMag
			if bins[i] > peak {
				peak = bins[i]
			}
			sumSq += bins[i] * bins[i]
		}
	}

	if len(bins) > 0 {
		rms = math.Sqrt(sumSq / float64(len(bins)))
	}

	return bins, peak, rms
}
