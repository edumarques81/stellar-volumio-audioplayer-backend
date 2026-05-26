package main

import (
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/airplay"
)

// TestBundlerEmitsOnMden — the daemon groups chunks between mdst…mden
// markers and emits one state payload per bundle.
func TestBundlerEmitsOnMden(t *testing.T) {
	var emitted []map[string]interface{}
	b := newBundler(func(p map[string]interface{}) { emitted = append(emitted, p) })

	b.Feed(airplay.Chunk{Type: "ssnc", Code: "mdst"})
	b.Feed(airplay.Chunk{Type: "core", Code: "minm", Data: []byte("Title")})
	b.Feed(airplay.Chunk{Type: "core", Code: "asar", Data: []byte("Artist")})
	b.Feed(airplay.Chunk{Type: "ssnc", Code: "mden"})

	if len(emitted) != 1 {
		t.Fatalf("expected 1 emit, got %d", len(emitted))
	}
	if emitted[0]["title"] != "Title" || emitted[0]["artist"] != "Artist" {
		t.Errorf("bundle wrong: %+v", emitted[0])
	}
}

// TestBundlerEmitsLifecycleImmediately — pbeg/pend/paus/prsm don't wait
// for an mden; they flush a payload right away.
func TestBundlerEmitsLifecycleImmediately(t *testing.T) {
	var emitted []map[string]interface{}
	b := newBundler(func(p map[string]interface{}) { emitted = append(emitted, p) })

	b.Feed(airplay.Chunk{Type: "ssnc", Code: "pbeg"})
	if len(emitted) != 1 {
		t.Fatalf("pbeg should flush; got %d", len(emitted))
	}
	if v, _ := emitted[0]["sessionBegan"].(bool); !v {
		t.Errorf("pbeg should set sessionBegan=true: %+v", emitted[0])
	}

	emitted = nil
	b.Feed(airplay.Chunk{Type: "ssnc", Code: "paus"})
	if len(emitted) != 1 {
		t.Fatalf("paus should flush")
	}
	if v, _ := emitted[0]["paused"].(bool); !v {
		t.Errorf("paus should set paused=true: %+v", emitted[0])
	}
}

// TestBundlerMapsPendToPaused — shairport emits `pend` at EVERY track
// boundary, not when the AirPlay session ends. Mapping it to
// {ended: true} (the original behaviour) killed the Mac-side session on
// every track change and made the iOS app's AirPlay UI flap. True
// session-end is detected via the daemon's heartbeat-gated-on-pipe-
// activity contract (see main.go's pipeActivityTimeout) — so `pend`
// here just hints "paused", and the next `pbeg` will flip isPlaying
// back to true.
func TestBundlerMapsPendToPaused(t *testing.T) {
	var emitted []map[string]interface{}
	b := newBundler(func(p map[string]interface{}) { emitted = append(emitted, p) })

	b.Feed(airplay.Chunk{Type: "ssnc", Code: "pend"})
	if len(emitted) != 1 {
		t.Fatalf("pend should flush")
	}
	if _, hasEnded := emitted[0]["ended"]; hasEnded {
		t.Errorf("pend must NOT emit ended (would kill session per track): %+v", emitted[0])
	}
	if v, _ := emitted[0]["paused"].(bool); !v {
		t.Errorf("pend should emit paused=true: %+v", emitted[0])
	}
}

// TestBundlerHonoursPICTOutOfBand — PICT chunks arrive separately and
// the bundler must flush a cover-only payload promptly (the UI relies
// on cover landing quickly after metadata).
func TestBundlerHonoursPICTOutOfBand(t *testing.T) {
	var emitted []map[string]interface{}
	b := newBundler(func(p map[string]interface{}) { emitted = append(emitted, p) })

	b.Feed(airplay.Chunk{Type: "ssnc", Code: "PICT", Data: []byte("\xff\xd8\xff\xe0fake")})
	if len(emitted) != 1 {
		t.Fatalf("PICT should flush; got %d", len(emitted))
	}
	cover, _ := emitted[0]["coverDataURL"].(string)
	if cover == "" || cover[:5] != "data:" {
		t.Errorf("coverDataURL should be a data URL; got %q", cover)
	}
}

// TestBundlerEmitsTokenChunksOutOfBand — acre/daid arrive before mden
// for a session (shairport emits them at session start). The bundler
// should flush them so the Mac can start gating CanControl ASAP.
func TestBundlerEmitsTokenChunksOutOfBand(t *testing.T) {
	var emitted []map[string]interface{}
	b := newBundler(func(p map[string]interface{}) { emitted = append(emitted, p) })

	b.Feed(airplay.Chunk{Type: "ssnc", Code: "acre", Data: []byte("3823061215")})
	b.Feed(airplay.Chunk{Type: "ssnc", Code: "daid", Data: []byte("63B5C32B3D40C25B")})

	if len(emitted) < 1 {
		t.Fatalf("token chunks should flush")
	}
	combined := map[string]interface{}{}
	for _, e := range emitted {
		for k, v := range e {
			combined[k] = v
		}
	}
	if combined["activeRemote"] != "3823061215" {
		t.Errorf("activeRemote = %v", combined["activeRemote"])
	}
	if combined["dacpID"] != "63B5C32B3D40C25B" {
		t.Errorf("dacpID = %v", combined["dacpID"])
	}
}
