package upnp

import "testing"

func TestParseDIDL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected TrackMetadata
	}{
		{
			name: "full Audirvana DIDL-Lite",
			input: `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"
				xmlns:dc="http://purl.org/dc/elements/1.1/"
				xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">
				<item>
					<dc:title>Blue Rondo A La Turk</dc:title>
					<dc:creator>Dave Brubeck</dc:creator>
					<upnp:album>Time Out</upnp:album>
					<upnp:albumArtURI>http://192.168.1.10:3000/art/cover.jpg</upnp:albumArtURI>
					<res duration="0:06:50.000">http://stellar.local:3000/stream/track.flac</res>
				</item>
			</DIDL-Lite>`,
			expected: TrackMetadata{
				Title:    "Blue Rondo A La Turk",
				Artist:   "Dave Brubeck",
				Album:    "Time Out",
				AlbumArt: "http://192.168.1.10:3000/art/cover.jpg",
				Duration: "0:06:50.000",
				URI:      "http://stellar.local:3000/stream/track.flac",
			},
		},
		{
			name: "minimal DIDL with only title and URI",
			input: `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"
				xmlns:dc="http://purl.org/dc/elements/1.1/">
				<item>
					<dc:title>Test Track</dc:title>
					<res>http://example.com/audio.mp3</res>
				</item>
			</DIDL-Lite>`,
			expected: TrackMetadata{
				Title: "Test Track",
				URI:   "http://example.com/audio.mp3",
			},
		},
		{
			name:     "empty string",
			input:    "",
			expected: TrackMetadata{},
		},
		{
			name:     "whitespace only",
			input:    "   \n  ",
			expected: TrackMetadata{},
		},
		{
			name:     "invalid XML",
			input:    "<not-valid>",
			expected: TrackMetadata{},
		},
		{
			name: "DIDL with no items",
			input: `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/">
			</DIDL-Lite>`,
			expected: TrackMetadata{},
		},
		{
			name: "multiple resources picks first",
			input: `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"
				xmlns:dc="http://purl.org/dc/elements/1.1/">
				<item>
					<dc:title>Multi Res</dc:title>
					<res duration="0:03:00.000">http://example.com/hires.flac</res>
					<res duration="0:03:00.000">http://example.com/lossy.mp3</res>
				</item>
			</DIDL-Lite>`,
			expected: TrackMetadata{
				Title:    "Multi Res",
				Duration: "0:03:00.000",
				URI:      "http://example.com/hires.flac",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseDIDL(tt.input)
			if got != tt.expected {
				t.Errorf("ParseDIDL() = %+v, want %+v", got, tt.expected)
			}
		})
	}
}
