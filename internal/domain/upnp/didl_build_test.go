package upnp

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestStreamURLFor(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		relPath string
		want    string
	}{
		{
			"spaces are escaped, separators are not",
			"http://192.168.86.25:3000",
			"USB/Kind of Blue/01 So What.flac",
			"http://192.168.86.25:3000/stream/USB/Kind%20of%20Blue/01%20So%20What.flac",
		},
		{
			// "+" in a path is a literal plus, and a query-style escape would
			// turn it into a space on the way back in.
			"plus stays a plus",
			"http://pi:3000",
			"USB/Miles Davis + 19/01.dsf",
			"http://pi:3000/stream/USB/Miles%20Davis%20+%2019/01.dsf",
		},
		{
			"hash and question mark cannot leak into the URL grammar",
			"http://pi:3000",
			"USB/a#b?c/01.flac",
			"http://pi:3000/stream/USB/a%23b%3Fc/01.flac",
		},
		{
			"non-ascii is percent-encoded",
			"http://pi:3000",
			"USB/Études/01.wav",
			"http://pi:3000/stream/USB/%C3%89tudes/01.wav",
		},
		{
			"leading slash on the path does not double up",
			"http://pi:3000",
			"/USB/a/01.flac",
			"http://pi:3000/stream/USB/a/01.flac",
		},
		{
			"trailing slash on the base does not double up",
			"http://pi:3000/",
			"USB/a/01.flac",
			"http://pi:3000/stream/USB/a/01.flac",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := streamURLFor(tt.baseURL, tt.relPath); got != tt.want {
				t.Errorf("streamURLFor(%q, %q)\n got %q\nwant %q", tt.baseURL, tt.relPath, got, tt.want)
			}
		})
	}
}

func TestArtURLFor(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		artPath string
		want    string
	}{
		{
			// The library already query-escaped this; re-escaping would
			// double-encode the %20 and the lookup would miss.
			"relative art path is only prefixed",
			"http://pi:3000",
			"/albumart?path=USB%2FKind+of+Blue%2F01.flac",
			"http://pi:3000/albumart?path=USB%2FKind+of+Blue%2F01.flac",
		},
		{"missing leading slash is added", "http://pi:3000", "albumart?path=x", "http://pi:3000/albumart?path=x"},
		{
			"absolute third-party URL passes through",
			"http://pi:3000",
			"https://e-cdns-images.dzcdn.net/images/artist/abc/264x264-000000-80-0-0.jpg",
			"https://e-cdns-images.dzcdn.net/images/artist/abc/264x264-000000-80-0-0.jpg",
		},
		{"empty art path yields nothing", "http://pi:3000", "", ""},
		{"empty base yields nothing", "", "/albumart?path=x", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := artURLFor(tt.baseURL, tt.artPath); got != tt.want {
				t.Errorf("artURLFor(%q, %q)\n got %q\nwant %q", tt.baseURL, tt.artPath, got, tt.want)
			}
		})
	}
}

func TestFormatDIDLDuration(t *testing.T) {
	tests := []struct {
		sec  int
		want string
	}{
		{0, "0:00:00.000"},
		{9, "0:00:09.000"},
		{59, "0:00:59.000"},
		{60, "0:01:00.000"},
		{3599, "0:59:59.000"},
		{3600, "1:00:00.000"},
		{4210, "1:10:10.000"},
		{-5, "0:00:00.000"},
	}
	for _, tt := range tests {
		if got := formatDIDLDuration(tt.sec); got != tt.want {
			t.Errorf("formatDIDLDuration(%d) = %q, want %q", tt.sec, got, tt.want)
		}
	}
}

// didlDoc mirrors enough of DIDL-Lite to assert on a parsed document.
type didlDoc struct {
	XMLName    xml.Name        `xml:"DIDL-Lite"`
	Containers []didlContainer `xml:"container"`
	Items      []didlItemFull  `xml:"item"`
}

type didlContainer struct {
	ID         string `xml:"id,attr"`
	ParentID   string `xml:"parentID,attr"`
	Restricted string `xml:"restricted,attr"`
	ChildCount int    `xml:"childCount,attr"`
	Title      string `xml:"title"`
	Class      string `xml:"class"`
	Artist     string `xml:"artist"`
	Date       string `xml:"date"`
	Genre      string `xml:"genre"`
	AlbumArt   string `xml:"albumArtURI"`
}

type didlItemFull struct {
	ID       string       `xml:"id,attr"`
	ParentID string       `xml:"parentID,attr"`
	Title    string       `xml:"title"`
	Class    string       `xml:"class"`
	Album    string       `xml:"album"`
	Creator  string       `xml:"creator"`
	Track    int          `xml:"originalTrackNumber"`
	AlbumArt string       `xml:"albumArtURI"`
	Res      []didlResAll `xml:"res"`
}

type didlResAll struct {
	ProtocolInfo string `xml:"protocolInfo,attr"`
	Duration     string `xml:"duration,attr"`
	Size         int64  `xml:"size,attr"`
	URL          string `xml:",chardata"`
}

func parseDIDLDoc(t *testing.T, raw string) didlDoc {
	t.Helper()
	var doc didlDoc
	if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("DIDL-Lite is not well-formed XML: %v\n%s", err, raw)
	}
	return doc
}

func TestDIDLBuilderProducesWellFormedXML(t *testing.T) {
	b := newDIDLBuilder()
	b.AddContainer("albums", "0", "Albums & More <stuff>", "object.container.storageFolder", 7, "")
	b.AddAlbumContainer(ServerAlbum{
		Title:       `AC/DC "Live"`,
		AlbumArtist: "AC/DC",
		URI:         "USB/ACDC",
		TrackCount:  12,
		Year:        1992,
		Genre:       "Rock & Roll",
	}, "albums", "http://pi:3000/albumart?path=x")
	b.AddTrackItem(ServerTrack{
		Title:       "Thunderstruck <1>",
		Artist:      "AC/DC",
		Album:       "Live",
		TrackNumber: 1,
		DurationSec: 292,
		SizeBytes:   41_234_567,
		URI:         "USB/ACDC/01 Thunderstruck.flac",
	}, "A:xyz", "http://pi:3000/stream/USB/ACDC/01%20Thunderstruck.flac", "", "audio/flac")

	if b.Count() != 3 {
		t.Fatalf("Count() = %d, want 3", b.Count())
	}

	doc := parseDIDLDoc(t, b.String())

	if len(doc.Containers) != 2 || len(doc.Items) != 1 {
		t.Fatalf("got %d containers and %d items, want 2 and 1", len(doc.Containers), len(doc.Items))
	}

	// Markup-significant characters must survive as data, not as structure.
	if doc.Containers[0].Title != "Albums & More <stuff>" {
		t.Errorf("container title mangled: %q", doc.Containers[0].Title)
	}
	if doc.Containers[1].Title != `AC/DC "Live"` {
		t.Errorf("album title mangled: %q", doc.Containers[1].Title)
	}
	if doc.Items[0].Title != "Thunderstruck <1>" {
		t.Errorf("item title mangled: %q", doc.Items[0].Title)
	}

	album := doc.Containers[1]
	if album.Class != "object.container.album.musicAlbum" {
		t.Errorf("album class = %q", album.Class)
	}
	if album.ChildCount != 12 {
		t.Errorf("album childCount = %d, want 12", album.ChildCount)
	}
	// dc:date must be a full date; a bare year is rejected by some clients.
	if album.Date != "1992-01-01" {
		t.Errorf("album date = %q, want 1992-01-01", album.Date)
	}
	if album.Genre != "Rock & Roll" {
		t.Errorf("album genre = %q", album.Genre)
	}
	if album.ID != EncodeAlbumID(AlbumRef{Title: `AC/DC "Live"`, AlbumArtist: "AC/DC", URI: "USB/ACDC"}) {
		t.Errorf("album id is not the encoded ref: %q", album.ID)
	}

	item := doc.Items[0]
	if item.Class != "object.item.audioItem.musicTrack" {
		t.Errorf("item class = %q", item.Class)
	}
	if item.ID != EncodeTrackID("USB/ACDC/01 Thunderstruck.flac") {
		t.Errorf("item id is not the encoded track URI: %q", item.ID)
	}
	if len(item.Res) != 1 {
		t.Fatalf("got %d res elements, want 1", len(item.Res))
	}
	res := item.Res[0]
	if res.URL != "http://pi:3000/stream/USB/ACDC/01%20Thunderstruck.flac" {
		t.Errorf("res URL = %q", res.URL)
	}
	if res.Duration != "0:04:52.000" {
		t.Errorf("res duration = %q, want 0:04:52.000", res.Duration)
	}
	if res.Size != 41_234_567 {
		t.Errorf("res size = %d", res.Size)
	}
	if !strings.HasPrefix(res.ProtocolInfo, "http-get:*:audio/flac:") {
		t.Errorf("res protocolInfo = %q", res.ProtocolInfo)
	}
	// OP=01 is the half that matters: it tells the client Range works. We
	// support it, and a client told otherwise disables its seek bar.
	if !strings.Contains(res.ProtocolInfo, "DLNA.ORG_OP=01") {
		t.Errorf("res protocolInfo does not advertise byte-range seek: %q", res.ProtocolInfo)
	}
}

func TestDIDLBuilderOmitsEmptyOptionalFields(t *testing.T) {
	b := newDIDLBuilder()
	b.AddAlbumContainer(ServerAlbum{Title: "Untitled"}, "albums", "")
	b.AddTrackItem(ServerTrack{Title: "Track", URI: "a.flac"}, "albums", "http://pi/stream/a.flac", "", "audio/flac")
	raw := b.String()

	for _, unwanted := range []string{"<dc:date>", "<upnp:genre>", "<upnp:albumArtURI>", "duration=", "size="} {
		if strings.Contains(raw, unwanted) {
			t.Errorf("empty field emitted %q:\n%s", unwanted, raw)
		}
	}
	parseDIDLDoc(t, raw)
}

func TestDIDLBuilderEmptyDocumentIsValid(t *testing.T) {
	// An empty container must still produce a parseable DIDL document;
	// clients treat an unparseable Result as a server error, not as "empty".
	doc := parseDIDLDoc(t, newDIDLBuilder().String())
	if len(doc.Containers) != 0 || len(doc.Items) != 0 {
		t.Errorf("empty builder produced objects: %+v", doc)
	}
}
