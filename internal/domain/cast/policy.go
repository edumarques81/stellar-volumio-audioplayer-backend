// Package cast owns the Cast session: which library files a Cast device can
// play, and the state of the one session the appliance maintains at a time.
//
// The protocol itself lives in internal/infra/cast.
package cast

import (
	"path/filepath"
	"strings"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/filestream"
)

// Playability is the verdict on one library file.
type Playability string

const (
	// PlayableDirect means the device can fetch and decode the original file.
	PlayableDirect Playability = "direct"
	// NeedsResample means PCM above what the device accepts. Playable in
	// principle, but not without processing the backend does not do yet.
	NeedsResample Playability = "needs_resample"
	// Unplayable means no amount of resampling helps — DSD, which no Cast
	// receiver has a decoder for.
	Unplayable Playability = "unplayable"
)

// MaxCastSampleRate is the highest PCM sample rate a Cast device accepts.
//
// Measured, not quoted: a Nest Hub on firmware 3.80.546557 played FLAC and WAV
// at 192 kHz / 24-bit cleanly on a 40-second soak — double Google's published
// 96 kHz limit — and failed at 352.8 kHz in both containers.
const MaxCastSampleRate = 192000

// dsdExtensions are the formats no Cast receiver can decode. They are 17.5% of
// this library, so the verdict has to be reported to the user rather than
// quietly producing a device that does nothing.
var dsdExtensions = map[string]bool{
	".dsf": true,
	".dff": true,
}

// imagePrefix catches the artwork types the file server also serves. They are
// legitimate for /stream and meaningless as a cast target.
const imagePrefix = "image/"

// Verdict is the full answer for one file: whether it plays, what to tell the
// receiver it is, and a reason fit to put in front of a person.
type Verdict struct {
	Playability Playability
	ContentType string
	Reason      string
}

// Playable reports whether the file can be cast as it stands.
func (v Verdict) Playable() bool { return v.Playability == PlayableDirect }

// Classify decides whether a Cast device can play a library file.
//
// The content type comes from filestream rather than a second table here: the
// file server and the Cast sender must agree about what a URL will serve, and
// two allowlists drift.
//
// sampleRate is in Hz and may be 0 when unknown — the library records it per
// album, so an album the cache has not measured yet would otherwise be hidden
// on a guess. An unknown rate counts as playable and a refusal comes back from
// the device with its own reason, which is more honest than a silent omission.
func Classify(uri string, sampleRate int) Verdict {
	ext := strings.ToLower(filepath.Ext(uri))

	if dsdExtensions[ext] {
		return Verdict{
			Playability: Unplayable,
			Reason:      "DSD cannot be played by a Cast device",
		}
	}

	contentType := filestream.ContentType(uri)
	switch {
	case contentType == "":
		return Verdict{
			Playability: Unplayable,
			Reason:      "unsupported file type " + ext,
		}
	case strings.HasPrefix(contentType, imagePrefix):
		return Verdict{
			Playability: Unplayable,
			Reason:      "not an audio file",
		}
	}

	if sampleRate > MaxCastSampleRate {
		return Verdict{
			Playability: NeedsResample,
			ContentType: contentType,
			Reason:      "above the 192 kHz a Cast device accepts",
		}
	}

	return Verdict{Playability: PlayableDirect, ContentType: contentType}
}
