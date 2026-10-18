package cast

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The Cast v2 protocol frames a single protobuf message, CastMessage, behind a
// 4-byte big-endian length prefix. CastMessage has exactly seven fields and we
// use six of them, so it is encoded by hand rather than pulling in a protobuf
// runtime for one message type — the same call the UPnP package made about
// SOAP.
//
//	message CastMessage {
//	  required ProtocolVersion protocol_version = 1;  // CASTV2_1_0 = 0
//	  required string source_id                 = 2;
//	  required string destination_id            = 3;
//	  required string namespace                 = 4;
//	  required PayloadType payload_type         = 5;  // STRING = 0, BINARY = 1
//	  optional string payload_utf8              = 6;
//	  optional bytes payload_binary             = 7;
//	}
const (
	fieldProtocolVersion = 1
	fieldSourceID        = 2
	fieldDestinationID   = 3
	fieldNamespace       = 4
	fieldPayloadType     = 5
	fieldPayloadUTF8     = 6
	fieldPayloadBinary   = 7
)

const (
	wireVarint = 0
	wireBytes  = 2
)

// payloadTypeString is the only payload type we send. BINARY exists for
// app-specific namespaces we never speak.
const payloadTypeString = 0

// maxFrameSize caps what we are willing to allocate for one inbound frame. A
// receiver status payload for a busy device runs a few kilobytes; a megabyte is
// already far beyond anything legitimate, and without a cap a corrupt length
// prefix becomes an out-of-memory.
const maxFrameSize = 1 << 20

// ErrFrameTooLarge means the peer announced a frame we refuse to allocate.
var ErrFrameTooLarge = errors.New("cast: frame exceeds the maximum size")

// frame is one decoded CastMessage. Only the fields we use are kept.
type frame struct {
	SourceID      string
	DestinationID string
	Namespace     string
	Payload       string
}

// encodeFrame serialises f into a length-prefixed CastMessage.
//
// Fields are written in tag order. Protobuf does not require it, but Cast
// receivers are a closed set of implementations and emitting the canonical
// order costs nothing.
func encodeFrame(f frame) []byte {
	var body []byte
	body = appendVarintField(body, fieldProtocolVersion, 0)
	body = appendBytesField(body, fieldSourceID, f.SourceID)
	body = appendBytesField(body, fieldDestinationID, f.DestinationID)
	body = appendBytesField(body, fieldNamespace, f.Namespace)
	body = appendVarintField(body, fieldPayloadType, payloadTypeString)
	body = appendBytesField(body, fieldPayloadUTF8, f.Payload)

	out := make([]byte, 4, 4+len(body))
	binary.BigEndian.PutUint32(out, uint32(len(body)))
	return append(out, body...)
}

func appendVarintField(dst []byte, field int, value uint64) []byte {
	dst = appendVarint(dst, uint64(field)<<3|wireVarint)
	return appendVarint(dst, value)
}

func appendBytesField(dst []byte, field int, value string) []byte {
	dst = appendVarint(dst, uint64(field)<<3|wireBytes)
	dst = appendVarint(dst, uint64(len(value)))
	return append(dst, value...)
}

func appendVarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

// readFrame reads one length-prefixed CastMessage from r.
//
// It returns io.EOF only when the stream ends cleanly on a frame boundary; a
// truncated frame is io.ErrUnexpectedEOF, which the read loop treats as a
// dropped connection rather than a tidy close.
func readFrame(r io.Reader) (frame, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return frame{}, err
	}
	size := binary.BigEndian.Uint32(lenBuf[:])
	if size > maxFrameSize {
		return frame{}, fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, size)
	}

	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		// Only the length prefix may legitimately meet a clean EOF. A stream
		// that ends mid-frame is a dropped connection, and the read loop has
		// to tell the two apart.
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return frame{}, err
	}
	return decodeFrame(body)
}

// decodeFrame parses a CastMessage body.
//
// Unknown fields are skipped rather than rejected: the receivers in the field
// span many firmware versions and a frame carrying something we do not model is
// not a protocol error.
func decodeFrame(body []byte) (frame, error) {
	var f frame
	for len(body) > 0 {
		tag, n := consumeVarint(body)
		if n == 0 {
			return frame{}, errors.New("cast: truncated field tag")
		}
		body = body[n:]

		field := int(tag >> 3)
		switch tag & 0x7 {
		case wireVarint:
			_, n := consumeVarint(body)
			if n == 0 {
				return frame{}, fmt.Errorf("cast: truncated varint for field %d", field)
			}
			body = body[n:]
		case wireBytes:
			length, n := consumeVarint(body)
			if n == 0 {
				return frame{}, fmt.Errorf("cast: truncated length for field %d", field)
			}
			body = body[n:]
			if uint64(len(body)) < length {
				return frame{}, fmt.Errorf("cast: field %d claims %d bytes, %d remain", field, length, len(body))
			}
			value := string(body[:length])
			body = body[length:]
			switch field {
			case fieldSourceID:
				f.SourceID = value
			case fieldDestinationID:
				f.DestinationID = value
			case fieldNamespace:
				f.Namespace = value
			case fieldPayloadUTF8:
				f.Payload = value
			case fieldPayloadBinary:
				// Not modelled; a binary payload belongs to an app namespace
				// we never subscribe to.
			}
		default:
			return frame{}, fmt.Errorf("cast: unsupported wire type %d on field %d", tag&0x7, field)
		}
	}
	return f, nil
}

// consumeVarint returns the decoded value and how many bytes it used, or n == 0
// when the input is truncated or the varint is unreasonably long.
func consumeVarint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * uint(i))
		if b[i] < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}
