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

// synthFrameAmplitudeInt16 is the tone amplitude synthFrame emits, and
// synthFrameAmplitude is the same value normalised to [-1, 1] — the
// sample-peak a Process() call on a synthFrame tone must report.
const (
	synthFrameAmplitudeInt16 = 16000
	synthFrameAmplitude      = synthFrameAmplitudeInt16 / 32768.0
)

// synthFrame builds a single FFTSize-sized stereo PCM buffer encoded as
// 16-bit little-endian, interleaved L/R. fL and fR are tone frequencies in
// Hz (use 0 for silence). The amplitudes are 50% of full-scale so the FFT
// has comfortable headroom.
func synthFrame(fftSize, sampleRate int, fL, fR float64) []byte {
	buf := make([]byte, fftSize*4)
	amp := int16(synthFrameAmplitudeInt16)
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

	// synthFrame's tone is int16 amplitude 16000, i.e. 16000/32768 ≈ 0.4883
	// of full scale. Asserting the amplitude rather than a "non-trivial"
	// floor is the point: the original `> 0.5` passed for years against a
	// PeakL that was hardcoded 1.0 by a normalisation bug.
	if math.Abs(data.PeakL-synthFrameAmplitude) > 0.01 {
		t.Errorf("expected PeakL ≈ %.4f for left tone, got %.4f", synthFrameAmplitude, data.PeakL)
	}
	if data.PeakR >= 0.01 {
		t.Errorf("expected PeakR < 0.01 for silent right, got %.3f", data.PeakR)
	}
	// The deprecated mono fallback takes max(L, R) where Bins and RMS take
	// the average, so a hard-panned frame is what distinguishes them: max
	// gives 0.4883 here, an average would give 0.2441.
	if math.Abs(data.Peak-synthFrameAmplitude) > 0.01 {
		t.Errorf("mono Peak = %.4f, want max(L,R) ≈ %.4f", data.Peak, synthFrameAmplitude)
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

	if math.Abs(data.PeakR-synthFrameAmplitude) > 0.01 {
		t.Errorf("expected PeakR ≈ %.4f for right tone, got %.4f", synthFrameAmplitude, data.PeakR)
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

// TestProcessPeakReflectsSignalLevel is the peak-side twin of
// TestProcessRMSReflectsSignalLevel, and it exists because PeakL/PeakR had
// exactly the bug that test was written to kill — one field later.
//
// computeChannelBins normalised every bin by the loudest bin and then took
// the maximum of the *already normalised* values, so the answer was the
// definition of 1.0 for any non-silent input. A 60-second capture off the
// live Pi confirmed it: 938/938 frames reported peak == 1 with zero
// variance, including frames whose RMS was -74 dBFS.
//
// The surviving tests did not catch it because they only ever asserted
// "> 0.5" for a tone and "== 0" for silence, and a constant 1.0 satisfies
// both. Pin the amplitude instead: a sine of amplitude A has a time-domain
// peak of A, so peak must track A and must not equal 1.0 unless the signal
// actually reaches full scale.
func TestProcessPeakReflectsSignalLevel(t *testing.T) {
	cfg := Config{SampleRate: 44100, FFTSize: 2048, NumBins: 64, FPS: 30}
	s := New(cfg)

	tests := []struct {
		name string
		amp  float64 // int16 amplitude of the left-channel tone
	}{
		{name: "half scale", amp: 16000},
		{name: "quarter scale", amp: 8000},
		{name: "near full scale", amp: 32000},
		{name: "very quiet", amp: 300},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pcm := make([]byte, cfg.FFTSize*4)
			for i := 0; i < cfg.FFTSize; i++ {
				l := int16(tt.amp * math.Sin(2*math.Pi*1000.0*float64(i)/float64(cfg.SampleRate)))
				binary.LittleEndian.PutUint16(pcm[i*4:i*4+2], uint16(l))
			}
			left, right := decodeStereo(pcm, cfg.FFTSize)
			data := s.Process(left, right)

			// 2048 samples at 44.1 kHz is ~46 tone periods, so the sampled
			// maximum lands within a hair of the true amplitude.
			// Relative, not absolute: at amp 300 the expected peak is 0.0092,
			// so a flat 0.01 tolerance would be wider than the value itself
			// and a regression returning 0 would pass this row. The error
			// here is a constant 1 LSB (1/32768) from int16 truncation, so
			// 2% plus a floor leaves several times the needed headroom at
			// every level in the table.
			want := tt.amp / 32768.0
			if math.Abs(data.PeakL-want) > want*0.02+1e-4 {
				t.Errorf("PeakL = %.4f, want ≈ %.4f for amplitude %.0f", data.PeakL, want, tt.amp)
			}
			if data.PeakR != 0 {
				t.Errorf("PeakR = %.4f, want 0 for a silent right channel", data.PeakR)
			}
			// A sine's crest factor is sqrt(2): peak must sit above RMS but
			// nowhere near the old constant 1.0 for a quiet signal.
			if data.PeakL <= data.RMSL {
				t.Errorf("PeakL %.4f must exceed RMSL %.4f for a sine", data.PeakL, data.RMSL)
			}
		})
	}
}

// TestProcessPeakIsNotPeggedAtOne is the single assertion that would have
// caught the shipped bug on its own: two inputs 30 dB apart must not report
// the same peak.
func TestProcessPeakIsNotPeggedAtOne(t *testing.T) {
	cfg := Config{SampleRate: 44100, FFTSize: 2048, NumBins: 64, FPS: 30}
	s := New(cfg)

	loudPCM := synthFrame(cfg.FFTSize, cfg.SampleRate, 1000.0, 1000.0)
	loudL, loudR := decodeStereo(loudPCM, cfg.FFTSize)
	loud := s.Process(loudL, loudR)

	quietPCM := make([]byte, cfg.FFTSize*4)
	for i := 0; i < cfg.FFTSize; i++ {
		v := int16(500.0 * math.Sin(2*math.Pi*1000.0*float64(i)/float64(cfg.SampleRate)))
		binary.LittleEndian.PutUint16(quietPCM[i*4:i*4+2], uint16(v))
		binary.LittleEndian.PutUint16(quietPCM[i*4+2:i*4+4], uint16(v))
	}
	quietL, quietR := decodeStereo(quietPCM, cfg.FFTSize)
	quiet := s.Process(quietL, quietR)

	if loud.PeakL <= quiet.PeakL {
		t.Errorf("loud PeakL %.4f must exceed quiet PeakL %.4f", loud.PeakL, quiet.PeakL)
	}
	if quiet.PeakL >= 0.1 {
		t.Errorf("quiet PeakL = %.4f, want well under 0.1 (a ~-36 dBFS tone)", quiet.PeakL)
	}
	// The deprecated mono fallback is max(L, R) and must track too.
	if quiet.Peak >= 0.1 {
		t.Errorf("quiet mono Peak = %.4f, want well under 0.1", quiet.Peak)
	}
}

// --- emit rate ------------------------------------------------------------
//
// Frames only exist on window boundaries, so a limiter that asks "has a full
// frameInterval elapsed since the last emit?" can only ever round UP to a
// whole number of windows. At 44.1 kHz the window is 46.4 ms against a 50 ms
// interval, so it waits for two — 10.8 fps against a configured 20. Measured
// on the Pi: 17159 frames in 1593 s = 10.77/s, and 15.6 fps on 96 kHz
// material, both exactly ceil(interval/window) windows per frame.
//
// The gate must instead deliver FPS on average whatever the two periods are.

func TestFrameGateDeliversTheConfiguredRate(t *testing.T) {
	tests := []struct {
		name       string
		sampleRate int
		fftSize    int
		fps        int
		wantFPS    float64
	}{
		{name: "44.1 kHz, window does not divide the interval", sampleRate: 44100, fftSize: 2048, fps: 20, wantFPS: 20},
		{name: "96 kHz", sampleRate: 96000, fftSize: 2048, fps: 20, wantFPS: 20},
		{name: "192 kHz", sampleRate: 192000, fftSize: 2048, fps: 20, wantFPS: 20},
		{name: "48 kHz, window divides the interval exactly", sampleRate: 40960, fftSize: 2048, fps: 20, wantFPS: 20},
		// Windows arriving slower than FPS cannot be conjured: the ceiling is
		// the window rate itself, and every window must be emitted.
		{name: "window slower than FPS emits every window", sampleRate: 44100, fftSize: 16384, fps: 20, wantFPS: 44100.0 / 16384.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{SampleRate: tt.sampleRate, FFTSize: tt.fftSize, NumBins: 64, FPS: tt.fps}
			window := time.Duration(float64(time.Second) * float64(tt.fftSize) / float64(tt.sampleRate))
			g := newFrameGate(cfg.frameInterval())

			const simulated = 60 * time.Second
			windows := int(simulated / window)
			emitted := 0
			for i := 0; i < windows; i++ {
				if g.allow(window) {
					emitted++
				}
			}

			got := float64(emitted) / simulated.Seconds()
			if math.Abs(got-tt.wantFPS) > tt.wantFPS*0.02 {
				t.Errorf("emit rate %.2f fps, want %.2f (window %.2f ms, interval %.2f ms)",
					got, tt.wantFPS, float64(window.Microseconds())/1000, float64(cfg.frameInterval().Microseconds())/1000)
			}
		})
	}
}

// TestFrameGateEmitsTheFirstWindowImmediately keeps the meter from being dark
// for a whole frame interval at the start of a stream.
func TestFrameGateEmitsTheFirstWindowImmediately(t *testing.T) {
	g := newFrameGate(50 * time.Millisecond)
	if !g.allow(0) {
		t.Fatal("first window must be emitted immediately")
	}
}

// TestFrameGateDoesNotBankAStall pins the one thing a naive credit counter
// gets wrong: after a long silence the accumulated credit must not buy a
// burst of back-to-back frames.
func TestFrameGateDoesNotBankAStall(t *testing.T) {
	interval := 50 * time.Millisecond
	g := newFrameGate(interval)
	g.allow(0) // prime

	if !g.allow(10 * time.Second) {
		t.Fatal("the window after a stall should emit")
	}
	// A 10s stall is 200 intervals of credit. At most one more frame may be
	// owed; anything beyond that is a burst.
	burst := 0
	for i := 0; i < 20; i++ {
		if g.allow(0) {
			burst++
		}
	}
	if burst > 1 {
		t.Errorf("stall banked %d extra immediate frames, want at most 1", burst)
	}
}
