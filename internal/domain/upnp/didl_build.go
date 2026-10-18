package upnp

import (
	"fmt"
	"strings"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/filestream"
)

// didlHeader opens a DIDL-Lite document with the namespaces every DLNA control
// point expects. The dlna namespace is declared even though only res@protocolInfo
// uses it, because some clients refuse a document whose prefixes are not all bound.
const didlHeader = `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"` +
	` xmlns:dc="http://purl.org/dc/elements/1.1/"` +
	` xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"` +
	` xmlns:dlna="urn:schemas-dlna-org:metadata-1-0/">`

const didlFooter = `</DIDL-Lite>`

// dlnaFlags advertises an HTTP resource that supports byte-range seeking.
//
// DLNA.ORG_OP=01 is the half that matters: it tells the client the server
// honours Range requests. Our /stream endpoint does (via http.ServeContent),
// and claiming otherwise would make clients buffer whole files before playing
// and disable the seek bar outright. DLNA.ORG_CI=0 means "not transcoded".
const dlnaFlags = "DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000"

// didlBuilder accumulates DIDL-Lite elements.
type didlBuilder struct {
	sb strings.Builder
	n  int
}

func newDIDLBuilder() *didlBuilder {
	b := &didlBuilder{}
	b.sb.WriteString(didlHeader)
	return b
}

// Count is the number of objects written so far, which is what Browse must
// report as NumberReturned.
func (b *didlBuilder) Count() int { return b.n }

// String closes the document and returns the raw (unescaped) DIDL-Lite XML.
func (b *didlBuilder) String() string {
	return b.sb.String() + didlFooter
}

// AddContainer writes a <container> element. childCount may be 0 when unknown.
func (b *didlBuilder) AddContainer(id, parentID, title, class string, childCount int, artURI string) {
	fmt.Fprintf(&b.sb,
		`<container id="%s" parentID="%s" restricted="1" childCount="%d">`+
			`<dc:title>%s</dc:title><upnp:class>%s</upnp:class>`,
		xmlEscape(id), xmlEscape(parentID), childCount, xmlEscape(title), xmlEscape(class))
	if artURI != "" {
		fmt.Fprintf(&b.sb, `<upnp:albumArtURI>%s</upnp:albumArtURI>`, xmlEscape(artURI))
	}
	b.sb.WriteString(`</container>`)
	b.n++
}

// AddAlbumContainer writes an album container, including the extra metadata
// (artist, date, genre) that makes a DLNA client's album view look populated.
func (b *didlBuilder) AddAlbumContainer(a ServerAlbum, parentID, artURI string) {
	fmt.Fprintf(&b.sb,
		`<container id="%s" parentID="%s" restricted="1" childCount="%d">`+
			`<dc:title>%s</dc:title>`+
			`<upnp:class>object.container.album.musicAlbum</upnp:class>`,
		xmlEscape(EncodeAlbumID(AlbumRef{Title: a.Title, AlbumArtist: a.AlbumArtist, URI: a.URI})),
		xmlEscape(parentID), a.TrackCount, xmlEscape(a.Title))
	if a.AlbumArtist != "" {
		fmt.Fprintf(&b.sb, `<upnp:artist>%s</upnp:artist><dc:creator>%s</dc:creator>`,
			xmlEscape(a.AlbumArtist), xmlEscape(a.AlbumArtist))
	}
	if a.Year > 0 {
		// dc:date must be a full date; a bare year is rejected by some clients.
		fmt.Fprintf(&b.sb, `<dc:date>%04d-01-01</dc:date>`, a.Year)
	}
	if a.Genre != "" {
		fmt.Fprintf(&b.sb, `<upnp:genre>%s</upnp:genre>`, xmlEscape(a.Genre))
	}
	if artURI != "" {
		fmt.Fprintf(&b.sb, `<upnp:albumArtURI>%s</upnp:albumArtURI>`, xmlEscape(artURI))
	}
	b.sb.WriteString(`</container>`)
	b.n++
}

// AddTrackItem writes a playable <item> whose single <res> points at the
// backend's /stream endpoint.
func (b *didlBuilder) AddTrackItem(t ServerTrack, parentID, streamURL, artURI, mimeType string) {
	fmt.Fprintf(&b.sb,
		`<item id="%s" parentID="%s" restricted="1">`+
			`<dc:title>%s</dc:title>`+
			`<upnp:class>object.item.audioItem.musicTrack</upnp:class>`,
		xmlEscape(EncodeTrackID(t.URI)), xmlEscape(parentID), xmlEscape(t.Title))

	if t.Artist != "" {
		fmt.Fprintf(&b.sb, `<dc:creator>%s</dc:creator><upnp:artist>%s</upnp:artist>`,
			xmlEscape(t.Artist), xmlEscape(t.Artist))
	}
	if t.AlbumArtist != "" {
		fmt.Fprintf(&b.sb, `<upnp:artist role="AlbumArtist">%s</upnp:artist>`, xmlEscape(t.AlbumArtist))
	}
	if t.Album != "" {
		fmt.Fprintf(&b.sb, `<upnp:album>%s</upnp:album>`, xmlEscape(t.Album))
	}
	if t.TrackNumber > 0 {
		fmt.Fprintf(&b.sb, `<upnp:originalTrackNumber>%d</upnp:originalTrackNumber>`, t.TrackNumber)
	}
	if artURI != "" {
		fmt.Fprintf(&b.sb, `<upnp:albumArtURI>%s</upnp:albumArtURI>`, xmlEscape(artURI))
	}

	b.sb.WriteString(`<res protocolInfo="http-get:*:` + xmlEscape(mimeType) + `:` + dlnaFlags + `"`)
	if t.DurationSec > 0 {
		fmt.Fprintf(&b.sb, ` duration="%s"`, formatDIDLDuration(t.DurationSec))
	}
	if t.SizeBytes > 0 {
		fmt.Fprintf(&b.sb, ` size="%d"`, t.SizeBytes)
	}
	fmt.Fprintf(&b.sb, `>%s</res></item>`, xmlEscape(streamURL))
	b.n++
}

// formatDIDLDuration renders seconds as "H:MM:SS.000", the form DIDL-Lite's
// res@duration requires. formatSeconds (used by AVTransport) omits the
// fractional part, which some clients parse as a zero duration.
func formatDIDLDuration(totalSec int) string {
	if totalSec < 0 {
		totalSec = 0
	}
	return fmt.Sprintf("%d:%02d:%02d.000", totalSec/3600, (totalSec%3600)/60, totalSec%60)
}

// streamURLFor builds the absolute /stream URL for a library-relative track path.
//
// Each path segment is escaped individually so that "/" keeps its structural
// meaning while spaces, "#", "?" and non-ASCII characters in album and track
// names survive the round trip. url.PathEscape on the whole path would escape
// the separators too and the request would 404.
func streamURLFor(baseURL, relPath string) string {
	return filestream.StreamURL(baseURL, relPath)
}

// artURLFor turns a backend-relative artwork URL into an absolute one.
//
// The library hands over paths that are already query-escaped (e.g.
// "/albumart?path=USB/Kind%20of%20Blue/01.flac"), so this only prefixes the
// origin. Re-escaping here would double-encode the percent signs and the
// request would miss.
func artURLFor(baseURL, artPath string) string {
	return filestream.AssetURL(baseURL, artPath)
}
