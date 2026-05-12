package spectrum

import (
	"context"
	"encoding/binary"
	"math"
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
