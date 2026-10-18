package upnp

import (
	"encoding/base64"
	"errors"
	"strings"
)

// UPnP ContentDirectory object IDs.
//
// A control point treats an ObjectID as an opaque string, so the only hard
// requirements are that it round-trips byte-for-byte through XML and that it
// carries enough information to answer the next Browse without server-side
// session state. Album and track titles in this library contain colons,
// slashes, ampersands and non-ASCII characters, so the payload is base64url
// encoded rather than interpolated — a delimiter scheme would break on the
// first "Symphony No. 9: Allegro" the user ingests.
const (
	// ObjectIDRoot is the mandatory root container every Browse starts from.
	ObjectIDRoot = "0"
	// ObjectIDAlbums is the flat "all albums" container.
	ObjectIDAlbums = "albums"
	// ObjectIDArtists is the "by artist" container.
	ObjectIDArtists = "artists"
)

// Object ID prefixes for the encoded (dynamic) containers and items.
const (
	prefixAlbum  = "A:"
	prefixArtist = "R:"
	prefixTrack  = "T:"
)

// fieldSep separates the packed fields inside an encoded object ID. It is a
// control character, so it cannot occur in a tag value MPD surfaced as UTF-8
// text, and the whole packed string is base64url encoded anyway.
const fieldSep = "\x1f"

// ErrUnknownObjectID is returned when an ObjectID does not name a container or
// item this server recognises.
var ErrUnknownObjectID = errors.New("unknown object id")

// AlbumRef identifies one album well enough to list its tracks without any
// server-side lookup table. All three fields are needed: Title and
// AlbumArtist are how the library groups tracks, and URI scopes the result to
// one specific quality version when the same release exists more than once.
type AlbumRef struct {
	Title       string
	AlbumArtist string
	URI         string
}

// EncodeAlbumID packs an album reference into an opaque object ID.
func EncodeAlbumID(ref AlbumRef) string {
	return prefixAlbum + encodeFields(ref.Title, ref.AlbumArtist, ref.URI)
}

// DecodeAlbumID unpacks an album object ID.
func DecodeAlbumID(id string) (AlbumRef, error) {
	fields, err := decodeFields(id, prefixAlbum, 3)
	if err != nil {
		return AlbumRef{}, err
	}
	return AlbumRef{Title: fields[0], AlbumArtist: fields[1], URI: fields[2]}, nil
}

// EncodeArtistID packs an artist name into an opaque object ID.
func EncodeArtistID(name string) string {
	return prefixArtist + encodeFields(name)
}

// DecodeArtistID unpacks an artist object ID.
func DecodeArtistID(id string) (string, error) {
	fields, err := decodeFields(id, prefixArtist, 1)
	if err != nil {
		return "", err
	}
	return fields[0], nil
}

// EncodeTrackID packs a track's library-relative URI into an opaque object ID.
func EncodeTrackID(uri string) string {
	return prefixTrack + encodeFields(uri)
}

// DecodeTrackID unpacks a track object ID.
func DecodeTrackID(id string) (string, error) {
	fields, err := decodeFields(id, prefixTrack, 1)
	if err != nil {
		return "", err
	}
	return fields[0], nil
}

func encodeFields(fields ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(fields, fieldSep)))
}

// decodeFields reverses encodeFields. It pads a short result rather than
// failing: an ID written by an older build may carry fewer fields, and
// degrading to an empty URI (a broader album match) beats refusing to browse.
func decodeFields(id, prefix string, want int) ([]string, error) {
	if !strings.HasPrefix(id, prefix) {
		return nil, ErrUnknownObjectID
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, prefix))
	if err != nil {
		return nil, ErrUnknownObjectID
	}
	fields := strings.Split(string(raw), fieldSep)
	if len(fields) > want {
		return nil, ErrUnknownObjectID
	}
	for len(fields) < want {
		fields = append(fields, "")
	}
	return fields, nil
}
