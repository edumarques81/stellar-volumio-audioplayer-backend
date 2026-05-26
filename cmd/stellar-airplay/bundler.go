package main

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/airplay"
)

// bundler converts a stream of parsed shairport chunks into the
// map-shaped payloads the daemon POSTs to the Mac. The mapping is
// best-effort delta-merge: empty fields are omitted from the payload
// and the Mac's session updater preserves prior values.
//
// Emit strategy:
//   - Bundles between mdst…mden are buffered and flushed on mden.
//   - PICT (cover art) is large and arrives out of band; flushed
//     immediately as its own payload.
//   - acre/daid (Active-Remote + DACP-ID) arrive before mdst at session
//     start; flushed immediately so the Mac can start gating CanControl.
//   - snam (sender name) is also flushed immediately.
//   - pbeg/pend/paus/prsm lifecycle codes are flushed immediately as a
//     payload containing the corresponding flag. pend → {ended: true}.
//   - prgr (progress) is flushed immediately so seek updates land at
//     ~1Hz without waiting for the next bundle.
type bundler struct {
	emit func(map[string]interface{})

	// pending holds metadata chunks observed since the last mdst.
	pending []airplay.Chunk
	inBundle bool
}

func newBundler(emit func(map[string]interface{})) *bundler {
	return &bundler{emit: emit}
}

// Feed processes a single chunk.
func (b *bundler) Feed(c airplay.Chunk) {
	switch c.Type + "/" + c.Code {
	case "ssnc/mdst":
		b.inBundle = true
		b.pending = b.pending[:0]
		return
	case "ssnc/mden":
		if b.inBundle {
			b.flushBundle()
		}
		b.inBundle = false
		return
	case "ssnc/PICT":
		b.emit(map[string]interface{}{"coverDataURL": coverDataURL(c.Data)})
		return
	case "ssnc/acre":
		b.emit(map[string]interface{}{"activeRemote": string(c.Data)})
		return
	case "ssnc/daid":
		b.emit(map[string]interface{}{"dacpID": string(c.Data)})
		return
	case "ssnc/snam":
		b.emit(map[string]interface{}{"sender": string(c.Data)})
		return
	case "ssnc/pbeg":
		b.emit(map[string]interface{}{"sessionBegan": true})
		return
	case "ssnc/pend":
		// `pend` is shairport's "play stream end" — fires when a single
		// stream/track ends, NOT when the AirPlay session truly ends.
		// Mapping it to {ended:true} kills the Mac-side session on every
		// track change, which makes the iOS app's AirPlay UI flap.
		// Treat pend as a pause hint instead — the next `pbeg` (next
		// track begin) will flip isPlaying back to true via the bundler's
		// existing pbeg handler. True session-end is detected via the
		// daemon's heartbeat-gated-on-pipe-activity contract: when the
		// iPhone fully disconnects, shairport stops emitting, the
		// heartbeat loop falls silent, and Mac's 5s timeout fires.
		b.emit(map[string]interface{}{"paused": true})
		return
	case "ssnc/paus":
		b.emit(map[string]interface{}{"paused": true})
		return
	case "ssnc/prsm":
		b.emit(map[string]interface{}{"resumed": true})
		return
	case "ssnc/prgr":
		start, cur, end, ok := parseProgress(c.Data)
		if !ok {
			return
		}
		payload := map[string]interface{}{}
		if cur >= start {
			payload["seekSeconds"] = (cur - start) / 44100
		}
		if end >= start {
			payload["durationSeconds"] = (end - start) / 44100
		}
		if len(payload) > 0 {
			b.emit(payload)
		}
		return
	}

	// core/* chunks belong in the current bundle.
	if b.inBundle {
		b.pending = append(b.pending, c)
	}
}

func (b *bundler) flushBundle() {
	if len(b.pending) == 0 {
		return
	}
	payload := map[string]interface{}{}
	for _, c := range b.pending {
		switch c.Type + "/" + c.Code {
		case "core/asar":
			payload["artist"] = string(c.Data)
		case "core/asal":
			payload["album"] = string(c.Data)
		case "core/minm":
			payload["title"] = string(c.Data)
		case "core/astm":
			if ms, err := strconv.Atoi(strings.TrimSpace(string(c.Data))); err == nil {
				payload["durationSeconds"] = ms / 1000
			}
		}
	}
	if len(payload) > 0 {
		b.emit(payload)
	}
	b.pending = b.pending[:0]
}

func parseProgress(b []byte) (start, cur, end int, ok bool) {
	parts := strings.Split(strings.TrimSpace(string(b)), "/")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	var err error
	if start, err = strconv.Atoi(strings.TrimSpace(parts[0])); err != nil {
		return 0, 0, 0, false
	}
	if cur, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
		return 0, 0, 0, false
	}
	if end, err = strconv.Atoi(strings.TrimSpace(parts[2])); err != nil {
		return 0, 0, 0, false
	}
	return start, cur, end, true
}

func coverDataURL(b []byte) string {
	mime := "image/jpeg"
	switch {
	case len(b) >= 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G':
		mime = "image/png"
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		mime = "image/jpeg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}
