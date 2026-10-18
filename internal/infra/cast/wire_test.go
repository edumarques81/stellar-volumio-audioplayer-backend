package cast

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   frame
	}{
		{"typical receiver message", frame{
			SourceID:      "sender-0",
			DestinationID: "receiver-0",
			Namespace:     nsReceiver,
			Payload:       `{"type":"GET_STATUS","requestId":1}`,
		}},
		{"empty payload", frame{
			SourceID:      "sender-0",
			DestinationID: "receiver-0",
			Namespace:     nsHeartbeat,
		}},
		{"all fields empty", frame{}},
		// Album and track titles in this library carry non-ASCII, and they
		// travel inside the LOAD payload.
		{"non-ASCII payload", frame{
			SourceID:      "sender-0",
			DestinationID: "transport-42",
			Namespace:     nsMedia,
			Payload:       `{"title":"Études-tableaux","artist":"Rachmaninoff"}`,
		}},
		// A payload longer than 127 bytes forces a multi-byte varint length,
		// which is the one piece of the encoder with a loop in it.
		{"payload over one varint byte", frame{
			SourceID:      "sender-0",
			DestinationID: "transport-42",
			Namespace:     nsMedia,
			Payload:       `{"pad":"` + strings.Repeat("x", 400) + `"}`,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := encodeFrame(tt.in)

			if got := binary.BigEndian.Uint32(encoded[:4]); got != uint32(len(encoded)-4) {
				t.Errorf("length prefix = %d, want %d", got, len(encoded)-4)
			}

			got, err := readFrame(bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("readFrame: %v", err)
			}
			if got != tt.in {
				t.Errorf("round trip = %+v, want %+v", got, tt.in)
			}
		})
	}
}

func TestReadFrameReadsOneFrameAtATime(t *testing.T) {
	// The read loop pulls frames off a long-lived stream, so readFrame must
	// stop at the frame boundary and leave the rest intact.
	var buf bytes.Buffer
	buf.Write(encodeFrame(frame{Namespace: nsHeartbeat, Payload: `{"type":"PING"}`}))
	buf.Write(encodeFrame(frame{Namespace: nsReceiver, Payload: `{"type":"RECEIVER_STATUS"}`}))

	first, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("first readFrame: %v", err)
	}
	if first.Payload != `{"type":"PING"}` {
		t.Errorf("first payload = %q", first.Payload)
	}

	second, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("second readFrame: %v", err)
	}
	if second.Payload != `{"type":"RECEIVER_STATUS"}` {
		t.Errorf("second payload = %q", second.Payload)
	}

	if _, err := readFrame(&buf); !errors.Is(err, io.EOF) {
		t.Errorf("third readFrame err = %v, want io.EOF", err)
	}
}

func TestReadFrameRejectsAnOversizedLengthPrefix(t *testing.T) {
	// Without the cap a corrupt or hostile prefix becomes a 4 GB allocation.
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], maxFrameSize+1)

	_, err := readFrame(bytes.NewReader(prefix[:]))
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameOnTruncatedInput(t *testing.T) {
	full := encodeFrame(frame{SourceID: "sender-0", Namespace: nsMedia, Payload: `{"type":"PLAY"}`})

	tests := []struct {
		name string
		in   []byte
		want error
	}{
		{"nothing at all", nil, io.EOF},
		{"partial length prefix", full[:2], io.ErrUnexpectedEOF},
		{"length prefix only", full[:4], io.ErrUnexpectedEOF},
		{"truncated body", full[:len(full)-3], io.ErrUnexpectedEOF},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := readFrame(bytes.NewReader(tt.in)); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestDecodeFrameSkipsUnknownFields(t *testing.T) {
	// Receivers span many firmware versions; a field we do not model must not
	// fail the frame.
	var body []byte
	body = appendVarintField(body, fieldProtocolVersion, 0)
	body = appendBytesField(body, fieldNamespace, nsMedia)
	body = appendVarintField(body, 11, 99)         // unknown varint field
	body = appendBytesField(body, 12, "ignore me") // unknown bytes field
	body = appendBytesField(body, fieldPayloadUTF8, `{"type":"MEDIA_STATUS"}`)

	got, err := decodeFrame(body)
	if err != nil {
		t.Fatalf("decodeFrame: %v", err)
	}
	if got.Namespace != nsMedia || got.Payload != `{"type":"MEDIA_STATUS"}` {
		t.Errorf("decoded = %+v", got)
	}
}

func TestDecodeFrameRejectsMalformedBodies(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{"truncated tag", []byte{0xff}},
		{"length beyond the buffer", []byte{byte(fieldNamespace<<3 | wireBytes), 0x40, 'a'}},
		{"truncated varint value", []byte{byte(fieldPayloadType<<3 | wireVarint), 0xff}},
		{"unsupported wire type", []byte{byte(fieldNamespace<<3 | 3)}},
		{"truncated length", []byte{byte(fieldNamespace<<3 | wireBytes), 0xff}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeFrame(tt.body); err == nil {
				t.Error("decodeFrame accepted a malformed body")
			}
		})
	}
}

func TestConsumeVarint(t *testing.T) {
	tests := []struct {
		name  string
		in    []byte
		want  uint64
		wantN int
	}{
		{"zero", []byte{0x00}, 0, 1},
		{"single byte", []byte{0x7f}, 127, 1},
		{"two bytes", []byte{0x80, 0x01}, 128, 2},
		{"three bytes", []byte{0xac, 0x02, 0x00}, 300, 2},
		{"empty", nil, 0, 0},
		{"never terminates", bytes.Repeat([]byte{0xff}, 12), 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, n := consumeVarint(tt.in)
			if n != tt.wantN || (tt.wantN != 0 && got != tt.want) {
				t.Errorf("consumeVarint = (%d, %d), want (%d, %d)", got, n, tt.want, tt.wantN)
			}
		})
	}
}
