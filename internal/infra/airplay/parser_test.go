package airplay

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

// buildItem renders one shairport-sync metadata pipe XML item exactly as
// shairport writes it, but in a test-deterministic form. The pipe format
// (from shairport-sync README, "metadata") is:
//
//	<item><type>TTTT</type><code>CCCC</code><length>N</length>
//	<data encoding="base64">
//	BASE64
//	</data></item>
//
// where TTTT/CCCC are 8-hex-char representations of the 4 ASCII bytes of
// the type/code. The data block is optional (omitted when N==0). The
// type and code 4-byte words are big-endian in their hex form (the high-
// order byte of the word is the first ASCII char).
func buildItem(typeCode, code string, data []byte) string {
	if len(typeCode) != 4 || len(code) != 4 {
		panic("typeCode and code must be 4 ASCII bytes")
	}
	typeHex := fmt.Sprintf("%08x", binary.BigEndian.Uint32([]byte(typeCode)))
	codeHex := fmt.Sprintf("%08x", binary.BigEndian.Uint32([]byte(code)))
	if len(data) == 0 {
		return fmt.Sprintf("<item><type>%s</type><code>%s</code><length>0</length></item>\n",
			typeHex, codeHex)
	}
	enc := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf(
		"<item><type>%s</type><code>%s</code><length>%d</length>\n<data encoding=\"base64\">\n%s</data></item>\n",
		typeHex, codeHex, len(data), enc)
}

func TestParseDecodesTitleArtistAlbum(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(buildItem("ssnc", "mdst", nil))
	buf.WriteString(buildItem("core", "asar", []byte("Daft Punk")))
	buf.WriteString(buildItem("core", "asal", []byte("Discovery")))
	buf.WriteString(buildItem("core", "minm", []byte("One More Time")))
	buf.WriteString(buildItem("ssnc", "mden", nil))

	chunks, err := ParseAll(&buf)
	if err != nil {
		t.Fatalf("ParseAll: unexpected err: %v", err)
	}
	if len(chunks) != 5 {
		t.Fatalf("expected 5 chunks, got %d", len(chunks))
	}

	wantCodes := []string{"mdst", "asar", "asal", "minm", "mden"}
	for i, c := range chunks {
		if c.Code != wantCodes[i] {
			t.Errorf("chunk[%d].Code = %q, want %q", i, c.Code, wantCodes[i])
		}
	}
	if string(chunks[1].Data) != "Daft Punk" {
		t.Errorf("chunks[1].Data = %q, want Daft Punk", chunks[1].Data)
	}
	if string(chunks[2].Data) != "Discovery" {
		t.Errorf("chunks[2].Data = %q, want Discovery", chunks[2].Data)
	}
	if string(chunks[3].Data) != "One More Time" {
		t.Errorf("chunks[3].Data = %q, want One More Time", chunks[3].Data)
	}
}

func TestParseDecodesPictAndDuration(t *testing.T) {
	cover := []byte("\xff\xd8\xff\xe0FAKE-JPEG-BYTES")
	durationMS := []byte("245000")

	var buf bytes.Buffer
	buf.WriteString(buildItem("ssnc", "mdst", nil))
	buf.WriteString(buildItem("core", "astm", durationMS))
	buf.WriteString(buildItem("ssnc", "PICT", cover))
	buf.WriteString(buildItem("ssnc", "mden", nil))

	chunks, err := ParseAll(&buf)
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	var foundPICT, foundAstm bool
	for _, c := range chunks {
		switch c.Code {
		case "PICT":
			foundPICT = true
			if !bytes.Equal(c.Data, cover) {
				t.Errorf("PICT data mismatch")
			}
			if c.Type != "ssnc" {
				t.Errorf("PICT type = %q, want ssnc", c.Type)
			}
		case "astm":
			foundAstm = true
			if string(c.Data) != "245000" {
				t.Errorf("astm = %q, want 245000", c.Data)
			}
		}
	}
	if !foundPICT || !foundAstm {
		t.Fatalf("missing chunks: PICT=%v astm=%v", foundPICT, foundAstm)
	}
}

// TestParseDecodesProgress covers the prgr binary code which encodes the
// session start frame, the current RTP frame, and the end frame as three
// decimal numbers separated by "/" (ASCII): "<start>/<cur>/<end>".
func TestParseDecodesProgress(t *testing.T) {
	progress := []byte("1234567/2345678/3456789")

	var buf bytes.Buffer
	buf.WriteString(buildItem("ssnc", "prgr", progress))

	chunks, err := ParseAll(&buf)
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Code != "prgr" {
		t.Fatalf("Code = %q, want prgr", chunks[0].Code)
	}
	if string(chunks[0].Data) != "1234567/2345678/3456789" {
		t.Errorf("Data = %q, want '1234567/2345678/3456789'", chunks[0].Data)
	}
}

// TestParseDecodesAcreDaidSnam covers the three text codes the daemon
// needs to drive DACP and label the source: Active-Remote token, DACP-ID,
// sender device name.
func TestParseDecodesAcreDaidSnam(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(buildItem("ssnc", "acre", []byte("3823061215")))
	buf.WriteString(buildItem("ssnc", "daid", []byte("63B5C32B3D40C25B")))
	buf.WriteString(buildItem("ssnc", "snam", []byte("Eduardo's iPhone")))

	chunks, err := ParseAll(&buf)
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if string(chunks[0].Data) != "3823061215" || chunks[0].Code != "acre" {
		t.Errorf("acre wrong: %v %q", chunks[0].Code, chunks[0].Data)
	}
	if string(chunks[1].Data) != "63B5C32B3D40C25B" || chunks[1].Code != "daid" {
		t.Errorf("daid wrong: %v %q", chunks[1].Code, chunks[1].Data)
	}
	if string(chunks[2].Data) != "Eduardo's iPhone" || chunks[2].Code != "snam" {
		t.Errorf("snam wrong: %v %q", chunks[2].Code, chunks[2].Data)
	}
}

// TestParseSessionLifecycle covers pbeg/pend/paus/prsm — these are
// zero-length sentinel codes.
func TestParseSessionLifecycle(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(buildItem("ssnc", "pbeg", nil))
	buf.WriteString(buildItem("ssnc", "paus", nil))
	buf.WriteString(buildItem("ssnc", "prsm", nil))
	buf.WriteString(buildItem("ssnc", "pend", nil))

	chunks, err := ParseAll(&buf)
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	wantCodes := []string{"pbeg", "paus", "prsm", "pend"}
	if len(chunks) != len(wantCodes) {
		t.Fatalf("got %d chunks, want %d", len(chunks), len(wantCodes))
	}
	for i, c := range chunks {
		if c.Code != wantCodes[i] {
			t.Errorf("chunks[%d].Code = %q, want %q", i, c.Code, wantCodes[i])
		}
		if len(c.Data) != 0 {
			t.Errorf("chunks[%d].Data should be empty; got %d bytes", i, len(c.Data))
		}
	}
}

// TestCoalesceFrameBuildsTitleArtistAlbum verifies the frame builder picks
// up the core metadata bundle between mdst/mden markers.
func TestCoalesceFrameBuildsTitleArtistAlbum(t *testing.T) {
	chunks := []Chunk{
		{Type: "ssnc", Code: "mdst"},
		{Type: "core", Code: "asar", Data: []byte("Daft Punk")},
		{Type: "core", Code: "asal", Data: []byte("Discovery")},
		{Type: "core", Code: "minm", Data: []byte("One More Time")},
		{Type: "core", Code: "astm", Data: []byte("245000")},
		{Type: "ssnc", Code: "mden"},
	}
	f := BuildFrame(chunks)
	if f.Title != "One More Time" {
		t.Errorf("Title = %q", f.Title)
	}
	if f.Artist != "Daft Punk" {
		t.Errorf("Artist = %q", f.Artist)
	}
	if f.Album != "Discovery" {
		t.Errorf("Album = %q", f.Album)
	}
	if f.DurationSeconds != 245 {
		t.Errorf("DurationSeconds = %d, want 245", f.DurationSeconds)
	}
}

// TestCoalesceFrameProgressAt44100Hz converts RTP-frame counts to seconds
// using a 44100Hz sample rate (AirPlay 1 standard).
func TestCoalesceFrameProgressAt44100Hz(t *testing.T) {
	// 882000 frames @ 44100Hz = 20s, end 1764000 @ 44100Hz = 40s; the
	// shairport prgr encoding is "start/cur/end". start is the session
	// start (not necessarily 0); seek = (cur - start) / 44100.
	chunks := []Chunk{
		{Type: "ssnc", Code: "prgr", Data: []byte("100/882100/1764100")},
	}
	f := BuildFrame(chunks)
	if f.SeekSeconds != 20 {
		t.Errorf("SeekSeconds = %d, want 20", f.SeekSeconds)
	}
	if f.DurationSeconds != 40 {
		t.Errorf("DurationSeconds = %d, want 40", f.DurationSeconds)
	}
}

func TestCoalesceFrameCoverArt(t *testing.T) {
	cover := []byte("\xff\xd8\xff\xe0fake-jpeg")
	chunks := []Chunk{
		{Type: "ssnc", Code: "PICT", Data: cover},
	}
	f := BuildFrame(chunks)
	if !strings.HasPrefix(f.CoverDataURL, "data:image/jpeg;base64,") {
		t.Errorf("CoverDataURL prefix wrong: %q", f.CoverDataURL[:32])
	}
	// Verify the encoded payload round-trips.
	enc := strings.TrimPrefix(f.CoverDataURL, "data:image/jpeg;base64,")
	dec, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(dec, cover) {
		t.Errorf("cover round-trip failed")
	}
}

func TestCoalesceFrameCoverArtPNG(t *testing.T) {
	cover := []byte("\x89PNG\r\n\x1a\nfake-png")
	chunks := []Chunk{
		{Type: "ssnc", Code: "PICT", Data: cover},
	}
	f := BuildFrame(chunks)
	if !strings.HasPrefix(f.CoverDataURL, "data:image/png;base64,") {
		t.Errorf("CoverDataURL prefix wrong (PNG): %q", f.CoverDataURL[:32])
	}
}

func TestCoalesceFrameSenderAndRemote(t *testing.T) {
	chunks := []Chunk{
		{Type: "ssnc", Code: "snam", Data: []byte("Eduardo's iPhone")},
		{Type: "ssnc", Code: "acre", Data: []byte("3823061215")},
		{Type: "ssnc", Code: "daid", Data: []byte("63B5C32B3D40C25B")},
	}
	f := BuildFrame(chunks)
	if f.Sender != "Eduardo's iPhone" {
		t.Errorf("Sender = %q", f.Sender)
	}
	if f.ActiveRemote != "3823061215" {
		t.Errorf("ActiveRemote = %q", f.ActiveRemote)
	}
	if f.DACPID != "63B5C32B3D40C25B" {
		t.Errorf("DACPID = %q", f.DACPID)
	}
}

func TestCoalesceFrameLifecycle(t *testing.T) {
	for _, code := range []string{"pbeg", "paus", "prsm", "pend"} {
		t.Run(code, func(t *testing.T) {
			f := BuildFrame([]Chunk{{Type: "ssnc", Code: code}})
			switch code {
			case "pbeg":
				if !f.SessionBegan {
					t.Error("pbeg should set SessionBegan")
				}
			case "pend":
				if !f.SessionEnded {
					t.Error("pend should set SessionEnded")
				}
			case "paus":
				if !f.Paused {
					t.Error("paus should set Paused")
				}
			case "prsm":
				if !f.Resumed {
					t.Error("prsm should set Resumed")
				}
			}
		})
	}
}

// TestParseTolerantToWhitespace ensures the parser handles the real-world
// pipe stream which interleaves leading whitespace + newlines.
func TestParseTolerantToWhitespace(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("\n\n  ")
	buf.WriteString(buildItem("ssnc", "snam", []byte("iPhone")))
	buf.WriteString("\n")
	buf.WriteString(buildItem("ssnc", "acre", []byte("42")))

	chunks, err := ParseAll(&buf)
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
}

// TestParseRejectsMalformedBase64 — bad base64 in a data block surfaces as
// an error rather than a silent truncation.
func TestParseRejectsMalformedBase64(t *testing.T) {
	bad := `<item><type>73736e63</type><code>736e616d</code><length>4</length>
<data encoding="base64">
!@!@</data></item>
`
	_, err := ParseAll(strings.NewReader(bad))
	if err == nil {
		t.Fatalf("expected error from malformed base64")
	}
}
