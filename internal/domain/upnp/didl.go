package upnp

import (
	"encoding/xml"
	"strings"
)

// didlLite represents the top-level DIDL-Lite XML element.
type didlLite struct {
	XMLName xml.Name   `xml:"DIDL-Lite"`
	Items   []didlItem `xml:"item"`
}

// didlItem represents a single <item> in the DIDL-Lite document.
type didlItem struct {
	Title       string   `xml:"title"`
	Creator     string   `xml:"creator"`
	Album       string   `xml:"album"`
	AlbumArtURI string   `xml:"albumArtURI"`
	Resources   []didlRes `xml:"res"`
}

// didlRes represents a <res> element containing the stream URI and attributes.
type didlRes struct {
	Duration string `xml:"duration,attr"`
	Value    string `xml:",chardata"`
}

// ParseDIDL parses DIDL-Lite XML (as sent in SetAVTransportURI's CurrentURIMetaData)
// and returns the extracted TrackMetadata. Returns a zero-value TrackMetadata if
// the XML is empty or unparseable.
func ParseDIDL(rawXML string) TrackMetadata {
	rawXML = strings.TrimSpace(rawXML)
	if rawXML == "" {
		return TrackMetadata{}
	}

	var didl didlLite
	if err := xml.Unmarshal([]byte(rawXML), &didl); err != nil {
		return TrackMetadata{}
	}

	if len(didl.Items) == 0 {
		return TrackMetadata{}
	}

	item := didl.Items[0]
	meta := TrackMetadata{
		Title:    item.Title,
		Artist:   item.Creator,
		Album:    item.Album,
		AlbumArt: item.AlbumArtURI,
	}

	if len(item.Resources) > 0 {
		meta.URI = item.Resources[0].Value
		meta.Duration = item.Resources[0].Duration
	}

	return meta
}
