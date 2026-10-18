package upnp

import (
	"strings"
	"testing"
)

func TestAlbumIDRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		ref  AlbumRef
	}{
		{"plain", AlbumRef{Title: "Kind of Blue", AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue"}},
		{
			// A colon is the obvious delimiter choice and this library is full
			// of titles that contain one, which is why IDs are encoded.
			"colon in title",
			AlbumRef{Title: "Symphony No. 9: Allegro", AlbumArtist: "Dvorak", URI: "USB/Dvorak 9"},
		},
		{"slash in title", AlbumRef{Title: "AC/DC Live", AlbumArtist: "AC/DC", URI: "USB/ACDC"}},
		{"ampersand", AlbumRef{Title: "Miles Davis & company", AlbumArtist: "Miles Davis", URI: "a/b"}},
		{"unicode", AlbumRef{Title: "Études-tableaux", AlbumArtist: "Rachmaninoff", URI: "USB/Études"}},
		{"empty artist", AlbumRef{Title: "Untitled", AlbumArtist: "", URI: "USB/x"}},
		{"all empty", AlbumRef{}},
		{"percent and plus", AlbumRef{Title: "Miles Davis + 19", AlbumArtist: "100%", URI: "a/b%2Bc"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := EncodeAlbumID(tt.ref)
			if !strings.HasPrefix(id, prefixAlbum) {
				t.Fatalf("album id %q lacks prefix %q", id, prefixAlbum)
			}
			got, err := DecodeAlbumID(id)
			if err != nil {
				t.Fatalf("DecodeAlbumID(%q) error: %v", id, err)
			}
			if got != tt.ref {
				t.Errorf("round trip changed the ref:\n got %+v\nwant %+v", got, tt.ref)
			}
		})
	}
}

func TestArtistAndTrackIDRoundTrip(t *testing.T) {
	names := []string{"Miles Davis", "AC/DC", "Sigur Rós", "", "a:b:c"}
	for _, name := range names {
		id := EncodeArtistID(name)
		got, err := DecodeArtistID(id)
		if err != nil {
			t.Fatalf("DecodeArtistID(%q) error: %v", id, err)
		}
		if got != name {
			t.Errorf("artist round trip: got %q want %q", got, name)
		}
	}

	uris := []string{
		"USB/Kind of Blue/01 So What.flac",
		"USB/Rachmaninoff/01 Études-tableaux.wav",
		"a/b/c#d?e.dsf",
	}
	for _, uri := range uris {
		id := EncodeTrackID(uri)
		got, err := DecodeTrackID(id)
		if err != nil {
			t.Fatalf("DecodeTrackID(%q) error: %v", id, err)
		}
		if got != uri {
			t.Errorf("track round trip: got %q want %q", got, uri)
		}
	}
}

func TestObjectIDsAreOpaqueAndDistinct(t *testing.T) {
	// An album, an artist and a track can all share a name; their IDs must not
	// collide, or browsing one navigates into another.
	album := EncodeAlbumID(AlbumRef{Title: "Miles Davis"})
	artist := EncodeArtistID("Miles Davis")
	track := EncodeTrackID("Miles Davis")

	if album == artist || album == track || artist == track {
		t.Fatalf("object IDs collided: album=%q artist=%q track=%q", album, artist, track)
	}

	// Cross-decoding must be refused rather than silently succeed.
	if _, err := DecodeArtistID(album); err == nil {
		t.Error("DecodeArtistID accepted an album id")
	}
	if _, err := DecodeTrackID(artist); err == nil {
		t.Error("DecodeTrackID accepted an artist id")
	}
	if _, err := DecodeAlbumID(track); err == nil {
		t.Error("DecodeAlbumID accepted a track id")
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	// Note: a bare prefix ("A:", "R:") is NOT malformed. encodeFields of a
	// single empty string is the empty string, so EncodeArtistID("") really
	// does produce "R:" — the library does contain tracks with no artist tag.
	// Rejecting an empty payload would make those objects unreachable.
	tests := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"no prefix", "Kind of Blue"},
		{"root", ObjectIDRoot},
		{"albums container", ObjectIDAlbums},
		{"not base64", prefixAlbum + "!!!not base64!!!"},
		{"standard base64 padding", prefixAlbum + "YWJj=="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeAlbumID(tt.id); err == nil {
				t.Errorf("DecodeAlbumID(%q) = nil error, want ErrUnknownObjectID", tt.id)
			}
		})
	}
}

func TestDecodeRejectsTooManyFields(t *testing.T) {
	// A single-field ID carrying three fields is not an older-build ID, it is
	// a different object kind; accepting it would silently browse the wrong
	// thing.
	id := prefixArtist + encodeFields("a", "b", "c")
	if _, err := DecodeArtistID(id); err == nil {
		t.Error("DecodeArtistID accepted a three-field payload")
	}
}

func TestDecodeTolerateMissingFields(t *testing.T) {
	// An album ID written before URI scoping existed carries two fields.
	// Degrading to an empty URI keeps the library browsable.
	id := prefixAlbum + encodeFields("Kind of Blue", "Miles Davis")
	ref, err := DecodeAlbumID(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Title != "Kind of Blue" || ref.AlbumArtist != "Miles Davis" || ref.URI != "" {
		t.Errorf("got %+v, want title/artist set and empty URI", ref)
	}
}
