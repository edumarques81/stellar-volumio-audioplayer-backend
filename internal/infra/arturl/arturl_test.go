package arturl

import (
	"net/url"
	"strings"
	"testing"
)

// Real MPD track URIs from the live library (2026-08-12). The first is the
// reported failure: its folder name contains a literal "+", which is the
// query-string encoding of a space, so an unencoded URL made the backend
// look for "Miles Davis   19-DSF-11289k-1b" and return 404.
const milesAheadTrack = "USB/Miles Ahead - Miles Davis + 19-DSF-11289k-1b/01-Springsville.dsf"

func TestAlbumArt_RoundTripsThroughQueryParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		track string
	}{
		{"literal plus", milesAheadTrack},
		{"spaces and hyphens", "USB/toe - The Future Is Now/01 Song.flac"},
		{"unicode", "USB/Sigxer SU-6 test/DSD-测试文件 ANNOUNCEMENT (Voice).dsf"},
		{"ampersand", "USB/Simon & Garfunkel/Bookends/01.flac"},
		{"percent", "USB/100% Album/01.flac"},
		{"hash", "USB/Album #1/01.flac"},
		{"plain ascii", "USB/Artist/Album/01.flac"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			built := AlbumArt(tt.track)

			// The handler reads the path with r.URL.Query().Get("path"),
			// so parse exactly the way the server does.
			u, err := url.Parse(built)
			if err != nil {
				t.Fatalf("AlbumArt produced an unparsable URL %q: %v", built, err)
			}
			if got := u.Query().Get("path"); got != tt.track {
				t.Fatalf("round-trip changed the path:\n  in  %q\n  out %q\n  url %q",
					tt.track, got, built)
			}
		})
	}
}

func TestAlbumArt_KeepsPrefixOtherCodeMatchesOn(t *testing.T) {
	t.Parallel()

	// isArtworkRedirectURL (internal/transport/socketio/server.go) and the
	// frontend's asset-URL rewriter both key off these prefixes.
	got := AlbumArt(milesAheadTrack)
	if !strings.HasPrefix(got, "/albumart?path=") {
		t.Fatalf("prefix changed, downstream matchers will break: %q", got)
	}
}

func TestAlbumArt_EncodesTheLiteralPlus(t *testing.T) {
	t.Parallel()

	// Pin the specific regression: a raw "+" in the query value is read back
	// as a space, so it must leave here percent-encoded.
	got := AlbumArt(milesAheadTrack)
	if strings.Contains(got, "Davis + 19") {
		t.Fatalf("literal '+' survived unencoded and will decode to a space: %q", got)
	}
	if !strings.Contains(got, "%2B") {
		t.Fatalf("expected the '+' to be percent-encoded as %%2B: %q", got)
	}
}
