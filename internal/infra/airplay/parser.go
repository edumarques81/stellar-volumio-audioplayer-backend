// Package airplay parses the shairport-sync metadata pipe stream.
//
// shairport-sync 3.x writes a sequence of XML-framed "items" to a named
// pipe (typically /tmp/shairport-sync-metadata) describing the current
// AirPlay session: title/artist/album, cover art, progress, sender device
// name, the DACP Active-Remote token, and lifecycle markers. The exact
// format is documented in the shairport-sync README under "metadata".
//
// Each item looks like:
//
//	<item><type>XXXXXXXX</type><code>YYYYYYYY</code><length>N</length>
//	<data encoding="base64">
//	BASE64_PAYLOAD
//	</data></item>
//
// where XXXXXXXX and YYYYYYYY are 8-hex representations of the four-byte
// ASCII type/code words (high-order byte first). The data block is
// omitted when length is 0.
//
// This package is pure decode — no I/O, no goroutines. The caller (the
// Pi-side daemon) reads the pipe and feeds chunks to ParseAll or
// ParseStream, then groups chunks between an mdst/mden pair via
// BuildFrame.
package airplay

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Chunk is a single decoded metadata item from the pipe.
//
// Type is a 4-byte ASCII tag, typically "core" (DACP/iTunes codes — title,
// artist, album, duration) or "ssnc" (shairport-sync codes — cover art,
// progress, lifecycle markers, sender name, Active-Remote token).
// Code is also a 4-byte ASCII tag.
type Chunk struct {
	Type string
	Code string
	Data []byte
}

// PlayState is the tristate play/pause indicator surfaced by the parser
// in response to ssnc/pbeg, ssnc/prsm, and ssnc/paus chunks. The session
// updater consumes this to maintain its IsPlaying flag.
type PlayState int

const (
	// PlayStateUnknown is the zero value — the bundle contained no
	// lifecycle chunk. The session updater preserves its prior
	// IsPlaying value when it sees this.
	PlayStateUnknown PlayState = iota
	// PlayStatePlaying — the bundle contained pbeg or prsm.
	PlayStatePlaying
	// PlayStatePaused — the bundle contained paus.
	PlayStatePaused
)

// Frame is the coalesced metadata bundle produced from a slice of Chunks
// (typically the chunks observed between an mdst…mden pair, plus any
// out-of-band ssnc chunks).
//
// All string fields are best-effort: missing chunks yield zero values so
// downstream consumers can treat a Frame as a delta to apply over the
// current session state without crashing.
type Frame struct {
	Title           string
	Artist          string
	Album           string
	Sender          string
	CoverDataURL    string
	ActiveRemote    string
	DACPID          string
	SeekSeconds     int
	DurationSeconds int

	PlayState PlayState

	SessionBegan bool
	SessionEnded bool
	Paused       bool
	Resumed      bool
}

// airplaySampleRate is the per-channel PCM sample rate used by AirPlay 1
// (shairport-sync 3.x). Used to convert RTP frame counts in the prgr
// chunk to seconds.
const airplaySampleRate = 44100

// itemRegex extracts one item from the pipe stream. Tolerant of leading
// whitespace and CR/LF between fields. Captures: 1=type-hex, 2=code-hex,
// 3=length, 4=base64 body (may be empty when length==0). The body capture
// is intentionally permissive (any byte except '<') so malformed base64
// surfaces as a decode error in decodeMatch rather than a silent no-match.
var itemRegex = regexp.MustCompile(
	`<item><type>([0-9a-fA-F]{8})</type><code>([0-9a-fA-F]{8})</code><length>(\d+)</length>\s*(?:<data encoding="base64">\s*([^<]*?)</data>)?</item>`,
)

// ParseAll reads the entire stream and returns every chunk it can decode.
// Used by tests with bytes.Buffer fixtures. Production callers use
// ParseStream for a goroutine-friendly streaming decoder.
func ParseAll(r io.Reader) ([]Chunk, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("airplay: read: %w", err)
	}
	return decode(body)
}

// ParseStream calls onChunk for each decoded chunk as it arrives from r.
// Blocks until r returns io.EOF or a non-EOF error. Designed for the
// Pi-side daemon which keeps the pipe FD open across sessions.
//
// The decoder buffers up to 4 MiB of unparsed input — large enough for a
// single PICT cover (typical 300–400 KiB JPEG) plus a normal bundle.
// Beyond that limit the stream is treated as corrupt and the call returns
// io.ErrShortBuffer.
func ParseStream(r io.Reader, onChunk func(Chunk)) error {
	const maxBuf = 4 * 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 32*1024)

	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			// Drain as many complete items as we can from the buffer.
			for {
				match := itemRegex.FindSubmatchIndex(buf)
				if match == nil {
					break
				}
				one, derr := decodeMatch(buf, match)
				if derr != nil {
					return derr
				}
				onChunk(one)
				buf = buf[match[1]:]
			}
			if len(buf) > maxBuf {
				return io.ErrShortBuffer
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func decode(body []byte) ([]Chunk, error) {
	var out []Chunk
	for {
		match := itemRegex.FindSubmatchIndex(body)
		if match == nil {
			// Trailing whitespace / partial frame is non-fatal in the
			// "decode everything available" mode.
			return out, nil
		}
		c, err := decodeMatch(body, match)
		if err != nil {
			return out, err
		}
		out = append(out, c)
		body = body[match[1]:]
	}
}

// decodeMatch produces a Chunk from a single regex match. The match
// slice is the indexes returned by FindSubmatchIndex on the entire
// buffer (groups: 1=typeHex 2=codeHex 3=length 4=base64).
func decodeMatch(body []byte, match []int) (Chunk, error) {
	typeHex := string(body[match[2]:match[3]])
	codeHex := string(body[match[4]:match[5]])
	lengthStr := string(body[match[6]:match[7]])

	typeASCII, err := hexWordToASCII(typeHex)
	if err != nil {
		return Chunk{}, fmt.Errorf("airplay: type %q: %w", typeHex, err)
	}
	codeASCII, err := hexWordToASCII(codeHex)
	if err != nil {
		return Chunk{}, fmt.Errorf("airplay: code %q: %w", codeHex, err)
	}

	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return Chunk{}, fmt.Errorf("airplay: length %q: %w", lengthStr, err)
	}

	c := Chunk{Type: typeASCII, Code: codeASCII}
	if length > 0 && match[8] != -1 {
		raw := bytes.TrimSpace(body[match[8]:match[9]])
		// Some shairport builds emit a trailing newline inside the
		// <data> tag; the base64 decoder handles raw whitespace
		// transparently via NewDecoder, but StdEncoding.DecodeString
		// is strict. Strip all whitespace before decoding.
		cleaned := stripWhitespace(raw)
		data, derr := base64.StdEncoding.DecodeString(string(cleaned))
		if derr != nil {
			return Chunk{}, fmt.Errorf("airplay: base64: %w", derr)
		}
		c.Data = data
	}
	return c, nil
}

// hexWordToASCII decodes an 8-hex-character representation of a 4-byte
// big-endian word into the ASCII string of those four bytes.
func hexWordToASCII(hex string) (string, error) {
	if len(hex) != 8 {
		return "", fmt.Errorf("expected 8 hex chars, got %d", len(hex))
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return "", err
	}
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(v))
	return string(buf[:]), nil
}

func stripWhitespace(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			continue
		}
		out = append(out, c)
	}
	return out
}

// BuildFrame coalesces the supplied chunks into a single Frame.
//
// Mapping (per shairport-sync README + DACP/iTunes table):
//
//	core/asar → Artist
//	core/asal → Album
//	core/minm → Title
//	core/astm → duration in milliseconds (decimal ASCII)
//	ssnc/prgr → "start/cur/end" RTP frame counts at 44100Hz
//	ssnc/PICT → cover art (JPEG / PNG sniffed by magic bytes)
//	ssnc/snam → sender device name
//	ssnc/acre → Active-Remote token (ASCII decimal)
//	ssnc/daid → DACP-ID (ASCII hex)
//	ssnc/pbeg → SessionBegan
//	ssnc/pend → SessionEnded
//	ssnc/paus → Paused
//	ssnc/prsm → Resumed
//
// Unknown codes are silently ignored — shairport's metadata vocabulary is
// large and we only care about the ones above.
func BuildFrame(chunks []Chunk) Frame {
	var f Frame
	for _, c := range chunks {
		switch c.Type + "/" + c.Code {
		case "core/asar":
			f.Artist = string(c.Data)
		case "core/asal":
			f.Album = string(c.Data)
		case "core/minm":
			f.Title = string(c.Data)
		case "core/astm":
			if ms, err := strconv.Atoi(strings.TrimSpace(string(c.Data))); err == nil {
				f.DurationSeconds = ms / 1000
			}
		case "ssnc/prgr":
			start, cur, end, ok := parseProgress(c.Data)
			if ok {
				if cur >= start {
					f.SeekSeconds = (cur - start) / airplaySampleRate
				}
				if end >= start {
					f.DurationSeconds = (end - start) / airplaySampleRate
				}
			}
		case "ssnc/PICT":
			f.CoverDataURL = encodeCover(c.Data)
		case "ssnc/snam":
			f.Sender = string(c.Data)
		case "ssnc/acre":
			f.ActiveRemote = strings.TrimSpace(string(c.Data))
		case "ssnc/daid":
			f.DACPID = strings.TrimSpace(string(c.Data))
		case "ssnc/pbeg":
			f.SessionBegan = true
			f.PlayState = PlayStatePlaying
		case "ssnc/pend":
			f.SessionEnded = true
		case "ssnc/paus":
			f.Paused = true
			f.PlayState = PlayStatePaused
		case "ssnc/prsm":
			f.Resumed = true
			f.PlayState = PlayStatePlaying
		}
	}
	return f
}

// parseProgress decodes the shairport prgr payload "start/cur/end" (RTP
// frame counts as ASCII decimals).
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

// encodeCover sniffs the magic bytes of a cover-art payload and returns a
// data URL. Defaults to image/jpeg when the bytes don't match a known
// header — shairport's PICT is JPEG in the vast majority of real-world
// sessions.
func encodeCover(b []byte) string {
	mime := "image/jpeg"
	switch {
	case len(b) >= 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G':
		mime = "image/png"
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		mime = "image/jpeg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}
