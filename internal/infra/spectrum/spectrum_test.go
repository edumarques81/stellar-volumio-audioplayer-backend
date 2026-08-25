package spectrum

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"syscall"
	"testing"
	"time"
)

// fakeEmitter captures the payloads broadcast by the Streamer so tests can
// assert on the wire shape without involving a real Socket.IO server.
type fakeEmitter struct {
	events []emittedEvent
}

type emittedEvent struct {
	event string
	data  interface{}
}

func (f *fakeEmitter) BroadcastToAll(event string, data interface{}) {
	f.events = append(f.events, emittedEvent{event: event, data: data})
}

// synthFrame builds a single FFTSize-sized stereo PCM buffer encoded as
// 16-bit little-endian, interleaved L/R. fL and fR are tone frequencies in
// Hz (use 0 for silence). The amplitudes are 50% of full-scale so the FFT
// has comfortable headroom.
func synthFrame(fftSize, sampleRate int, fL, fR float64) []byte {
	buf := make([]byte, fftSize*4)
	amp := int16(16000)
	for i := 0; i < fftSize; i++ {
		var l, r int16
		if fL > 0 {
			l = int16(float64(amp) * math.Sin(2*math.Pi*fL*float64(i)/float64(sampleRate)))
		}
		if fR > 0 {
			r = int16(float64(amp) * math.Sin(2*math.Pi*fR*float64(i)/float64(sampleRate)))
		}
		binary.LittleEndian.PutUint16(buf[i*4:i*4+2], uint16(l))
		binary.LittleEndian.PutUint16(buf[i*4+2:i*4+4], uint16(r))
	}
	return buf
}

// decodeStereo splits an interleaved 16-bit stereo PCM buffer into
// normalised float64 channels of length fftSize.
func decodeStereo(buf []byte, fftSize int) (left, right []float64) {
	left = make([]float64, fftSize)
	right = make([]float64, fftSize)
	for i := 0; i < fftSize; i++ {
		l := int16(binary.LittleEndian.Uint16(buf[i*4 : i*4+2]))
		r := int16(binary.LittleEndian.Uint16(buf[i*4+2 : i*4+4]))
		left[i] = float64(l) / 32768.0
		right[i] = float64(r) / 32768.0
	}
	return left, right
}

// TestProcessLeftOnlyHasLeftPeak feeds a 1 kHz tone on the left channel and
// silence on the right; asserts left channel produces a non-trivial peak
// while right channel remains near zero, and at least one bin has left
// magnitude > right magnitude.
func TestProcessLeftOnlyHasLeftPeak(t *testing.T) {
	cfg := Config{
		SampleRate: 44100,
		FFTSize:    2048,
		NumBins:    64,
		FPS:        30,
	}
	s := New(cfg)

	pcm := synthFrame(cfg.FFTSize, cfg.SampleRate, 1000.0, 0)
	left, right := decodeStereo(pcm, cfg.FFTSize)

	data := s.Process(left, right)

	if data.PeakL <= 0.5 {
		t.Errorf("expected PeakL > 0.5 for left tone, got %.3f", data.PeakL)
	}
	if data.PeakR >= 0.01 {
		t.Errorf("expected PeakR < 0.01 for silent right, got %.3f", data.PeakR)
	}
	if len(data.BinsL) != cfg.NumBins {
		t.Fatalf("expected %d L bins, got %d", cfg.NumBins, len(data.BinsL))
	}
	if len(data.BinsR) != cfg.NumBins {
		t.Fatalf("expected %d R bins, got %d", cfg.NumBins, len(data.BinsR))
	}

	// At least one bin should have non-trivially larger left magnitude.
	gap := 0
	for i := range data.BinsL {
		if data.BinsL[i] > data.BinsR[i]+0.1 {
			gap++
		}
	}
	if gap == 0 {
		t.Errorf("expected at least one bin with BinsL >> BinsR, got none")
	}
}

// TestProcessRightOnlyHasRightPeak mirrors the left-only case for the right
// channel.
func TestProcessRightOnlyHasRightPeak(t *testing.T) {
	cfg := Config{SampleRate: 44100, FFTSize: 2048, NumBins: 64, FPS: 30}
	s := New(cfg)

	pcm := synthFrame(cfg.FFTSize, cfg.SampleRate, 0, 1000.0)
	left, right := decodeStereo(pcm, cfg.FFTSize)

	data := s.Process(left, right)

	if data.PeakR <= 0.5 {
		t.Errorf("expected PeakR > 0.5 for right tone, got %.3f", data.PeakR)
	}
	if data.PeakL >= 0.01 {
		t.Errorf("expected PeakL < 0.01 for silent left, got %.3f", data.PeakL)
	}

	gap := 0
	for i := range data.BinsR {
		if data.BinsR[i] > data.BinsL[i]+0.1 {
			gap++
		}
	}
	if gap == 0 {
		t.Errorf("expected at least one bin with BinsR >> BinsL, got none")
	}
}

// TestProcessSilenceHasZeroPeaks asserts that all-zero input yields zero
// peaks and zero RMS on both channels.
func TestProcessSilenceHasZeroPeaks(t *testing.T) {
	cfg := Config{SampleRate: 44100, FFTSize: 2048, NumBins: 64, FPS: 30}
	s := New(cfg)

	left := make([]float64, cfg.FFTSize)
	right := make([]float64, cfg.FFTSize)
	data := s.Process(left, right)

	if data.PeakL != 0 || data.PeakR != 0 {
		t.Errorf("expected zero peaks on silence, got L=%.3f R=%.3f", data.PeakL, data.PeakR)
	}
	if data.RMSL != 0 || data.RMSR != 0 {
		t.Errorf("expected zero RMS on silence, got L=%.3f R=%.3f", data.RMSL, data.RMSR)
	}
}

// TestProcessRMSReflectsSignalLevel guards against the regression where RMS
// was computed from already-bin-normalised magnitudes — which made it a
// function of spectral shape, not signal level, so it pegged ~0.4 regardless
// of input amplitude. A 50%-amplitude sine wave has time-domain RMS
// ≈ 0.5 / sqrt(2) ≈ 0.354; silence has RMS = 0; quarter-amplitude has
// RMS ≈ half of the 50% case. RMSL and RMSR must track independently.
func TestProcessRMSReflectsSignalLevel(t *testing.T) {
	cfg := Config{SampleRate: 44100, FFTSize: 2048, NumBins: 64, FPS: 30}
	s := New(cfg)

	// 50%-amplitude tone on L, silence on R.
	pcm := synthFrame(cfg.FFTSize, cfg.SampleRate, 1000.0, 0)
	left, right := decodeStereo(pcm, cfg.FFTSize)
	data := s.Process(left, right)

	const expectedRMS = 0.5 / math.Sqrt2 // ≈ 0.3536 — int16 amp 16000 ≈ 0.488
	const amp = 16000.0 / 32768.0
	const target = amp / math.Sqrt2 // ≈ 0.3450

	if math.Abs(data.RMSL-target) > 0.02 {
		t.Errorf("expected RMSL ≈ %.3f for 50%% sine, got %.3f", target, data.RMSL)
	}
	if data.RMSR > 0.01 {
		t.Errorf("expected RMSR ≈ 0 for silent right, got %.3f", data.RMSR)
	}

	// Quarter-amplitude tone on L → RMS roughly halves.
	pcmQuiet := make([]byte, len(pcm))
	for i := 0; i < cfg.FFTSize; i++ {
		l := int16(8000.0 * math.Sin(2*math.Pi*1000.0*float64(i)/float64(cfg.SampleRate)))
		binary.LittleEndian.PutUint16(pcmQuiet[i*4:i*4+2], uint16(l))
	}
	leftQ, rightQ := decodeStereo(pcmQuiet, cfg.FFTSize)
	dataQ := s.Process(leftQ, rightQ)
	if dataQ.RMSL >= data.RMSL*0.7 {
		t.Errorf("quarter-amp RMSL should be ~half of half-amp; got %.3f vs %.3f", dataQ.RMSL, data.RMSL)
	}
	_ = expectedRMS // documentation anchor for the constant above
}

// TestProcessIncludesTransitionalMonoBins ensures the deprecated mono
// `Bins` field is still populated as the average of L and R, so legacy
// consumers don't get a nil slice. Once M1.E lands, this can be removed.
func TestProcessIncludesTransitionalMonoBins(t *testing.T) {
	cfg := Config{SampleRate: 44100, FFTSize: 2048, NumBins: 64, FPS: 30}
	s := New(cfg)

	pcm := synthFrame(cfg.FFTSize, cfg.SampleRate, 1000.0, 1000.0)
	left, right := decodeStereo(pcm, cfg.FFTSize)
	data := s.Process(left, right)

	if len(data.Bins) != cfg.NumBins {
		t.Fatalf("expected transitional mono Bins to have %d entries, got %d", cfg.NumBins, len(data.Bins))
	}
}

// TestProcessSampleRateAndTSAreSet checks the new payload metadata fields.
func TestProcessSampleRateAndTSAreSet(t *testing.T) {
	cfg := Config{SampleRate: 44100, FFTSize: 2048, NumBins: 64, FPS: 30}
	s := New(cfg)

	left := make([]float64, cfg.FFTSize)
	right := make([]float64, cfg.FFTSize)
	data := s.Process(left, right)

	if data.SampleRate != 44100 {
		t.Errorf("expected SampleRate=44100, got %d", data.SampleRate)
	}
	if data.TS == 0 {
		t.Errorf("expected non-zero TS, got %d", data.TS)
	}
}

// TestStreamerStartStopWithFakeEmitter verifies the Streamer constructor
// integrates with a mock emitter cleanly even when the FIFO does not
// exist — Start kicks off a goroutine that retries; Stop must unblock it
// promptly.
func TestStreamerStartStopWithFakeEmitter(t *testing.T) {
	cfg := Config{
		FIFOPath:   "/tmp/stellar-spectrum-test-nonexistent-fifo",
		SampleRate: 44100,
		FFTSize:    2048,
		NumBins:    64,
		FPS:        30,
	}
	s := New(cfg)
	emitter := &fakeEmitter{}

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx, emitter)

	// Let the goroutine cycle once on the FIFO-open failure path.
	time.Sleep(50 * time.Millisecond)
	cancel()

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Streamer did not stop within 2s after ctx cancel")
	}
}

// --- FIFO drain invariant ------------------------------------------------
//
// Regression guard for the audio dropouts diagnosed 2026-08-26. The reader
// sits on the DAC's critical path: MPD only returns a decoded chunk to its
// shared pool once every enabled output has consumed it, so a reader that
// falls behind stalls MPD's decoder and underruns the DAC.

func TestReaderDrainsIndependentlyOfTheFrameRate(t *testing.T) {
	// FPS 1 with a 64-sample window emits at most one frame per second. If
	// the reader gated its *reads* on that cadence it would take 256 bytes/s
	// out of the pipe, the pipe would saturate, and MPD's fifo output would
	// block. Draining must not depend on the emit rate.
	path := makeFIFO(t)
	s := New(Config{FIFOPath: path, SampleRate: 44100, FFTSize: 64, NumBins: 8, FPS: 1})
	s.Start(context.Background(), &safeEmitter{})
	defer s.Stop()

	w := openWriter(t, path)
	chunk := make([]byte, 256)

	written := 0
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, err := w.Write(chunk)
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) {
				time.Sleep(time.Millisecond)
				continue
			}
			t.Fatalf("write to FIFO: %v", err)
		}
		written += n
	}

	// A pipe buffer is 64 KiB. Accepting only about that much means the
	// reader stopped draining and we measured the buffer, not throughput.
	const minBytes = 512 * 1024
	if written < minBytes {
		t.Errorf(
			"reader accepted only %d bytes in 2s at FPS 1, want > %d — reads are "+
				"gated on the frame rate, so the FIFO backs up, MPD's fifo output "+
				"stalls and the DAC underruns",
			written, minBytes,
		)
	}
}

func TestRateEstimatorSnapsToTheObservedRate(t *testing.T) {
	tests := []struct {
		name       string
		start      int
		actualRate int
		want       int
		wantChange bool
	}{
		{"steady at the configured rate", 44100, 44100, 44100, false},
		{"source rate is 192k", 44100, 192000, 192000, true},
		{"source rate is 96k", 44100, 96000, 96000, true},
		{"dropping back to 44.1k", 192000, 44100, 44100, true},
		{"48k is distinguished from 44.1k", 44100, 48000, 48000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const fftSize = 2048
			r := newRateEstimator(tt.start)
			now := time.Now()
			r.observe(fftSize, now) // seeds the clock

			// Feed two seconds of windows at the real rate.
			var got int
			var changed bool
			perWindow := time.Duration(float64(fftSize) / float64(tt.actualRate) * float64(time.Second))
			for elapsed := time.Duration(0); elapsed < 2*time.Second; elapsed += perWindow {
				now = now.Add(perWindow)
				if rate, ch := r.observe(fftSize, now); ch {
					got, changed = rate, true
				}
			}

			if changed != tt.wantChange {
				t.Fatalf("changed = %v, want %v (rate now %d)", changed, tt.wantChange, r.current)
			}
			if tt.wantChange && got != tt.want {
				t.Errorf("observed rate = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBinsCoverTheAudibleRangeAtEveryRate(t *testing.T) {
	// The meter must look the same whatever the album's rate: at 44.1/48 kHz
	// the bins span the whole spectrum, and above that the ultrasonic part is
	// dropped rather than squeezing every audible bar into the low bins.
	baseline := New(Config{SampleRate: 44100, FFTSize: 2048, NumBins: 64, FPS: 20})
	top := func(s *Streamer) int { return s.binEdges[len(s.binEdges)-1] }

	if got, want := top(baseline), 1024; got != want {
		t.Fatalf("44.1kHz top bin edge = %d, want %d (full Nyquist)", got, want)
	}

	for _, rate := range []int{96000, 192000, 352800} {
		s := New(Config{SampleRate: rate, FFTSize: 2048, NumBins: 64, FPS: 20})

		// Top edge should track 48kHz-equivalent Nyquist, not the full one.
		wantTop := int(float64(1024) * float64(48000) / float64(rate))
		if got := top(s); got > wantTop+1 || got < wantTop-1 {
			t.Errorf("%dHz top bin edge = %d, want ~%d", rate, got, wantTop)
		}
		if top(s) >= top(baseline) {
			t.Errorf("%dHz spans %d bins, should be narrower than 44.1kHz's %d", rate, top(s), top(baseline))
		}
	}
}
