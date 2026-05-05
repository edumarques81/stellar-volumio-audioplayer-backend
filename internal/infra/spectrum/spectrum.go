// Package spectrum captures audio from MPD's FIFO output, performs FFT analysis,
// and streams frequency bin data to the frontend via Socket.IO.
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
	FPS        int    // Target frames per second (e.g. 30)
}

// SpectrumData is the payload emitted via Socket.IO as "pushSpectrum".
type SpectrumData struct {
	Bins []float64 `json:"bins"` // Frequency bins normalized 0.0-1.0
	Peak float64   `json:"peak"` // Overall peak level
	RMS  float64   `json:"rms"`  // Overall RMS level
}

// SocketEmitter is the interface the spectrum streamer needs to broadcast events.
// This matches the typical Socket.IO server's BroadcastToNamespace or similar.
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
		fifo.Close()

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
	samples := make([]float64, s.cfg.FFTSize)

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

		// Convert 16-bit stereo PCM to mono float64 samples
		numFrames := n / bytesPerFrame
		for i := 0; i < numFrames && i < s.cfg.FFTSize; i++ {
			offset := i * bytesPerFrame
			left := int16(binary.LittleEndian.Uint16(buf[offset : offset+2]))
			right := int16(binary.LittleEndian.Uint16(buf[offset+2 : offset+4]))
			// Mix to mono and normalize to [-1.0, 1.0]
			mono := (float64(left) + float64(right)) / 2.0
			samples[i] = mono / 32768.0
		}

		// Apply Hann window
		for i := range samples {
			samples[i] *= s.hannWindow[i]
		}

		// Run FFT
		spectrum := fft.FFTReal(samples)

		// Compute frequency bins with logarithmic grouping
		data := s.computeBins(spectrum)

		// Emit via Socket.IO
		emitter.BroadcastToAll("pushSpectrum", data)
	}
}

// computeBins converts raw FFT complex output into logarithmically-grouped
// frequency bins, normalized to 0.0-1.0.
func (s *Streamer) computeBins(spectrum []complex128) SpectrumData {
	bins := make([]float64, s.cfg.NumBins)
	var peak, sumSq float64

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

	// Normalize bins to 0.0-1.0 and compute peak/RMS
	if maxMag > 0 {
		for i := range bins {
			bins[i] /= maxMag
			if bins[i] > peak {
				peak = bins[i]
			}
			sumSq += bins[i] * bins[i]
		}
	}

	rms := 0.0
	if len(bins) > 0 {
		rms = math.Sqrt(sumSq / float64(len(bins)))
	}

	return SpectrumData{
		Bins: bins,
		Peak: peak,
		RMS:  rms,
	}
}
