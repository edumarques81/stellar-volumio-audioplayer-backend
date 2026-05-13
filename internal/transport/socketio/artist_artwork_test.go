package socketio

import "testing"

func TestIsArtworkRedirectURL(t *testing.T) {
	cases := []struct {
		name     string
		filePath string
		want     bool
	}{
		{"empty", "", false},
		{"absolute disk path", "/home/volumio/stellar-backend/data/cache/artwork/artists/abc.jpg", false},
		{"relative disk path", "artwork/artists/abc.jpg", false},
		{"http url", "http://e-cdns-images.dzcdn.net/images/artist/abc/250x250.jpg", true},
		{"https url", "https://coverartarchive.org/release/x/front-500", true},
		{"albumart fallback path", "/albumart?path=NAS/Music/Some%20Album/01.flac", true},
		{"albumart fallback with web prefix", "/albumart?web=tidal:track:1", true},
		// Defensive: any other absolute path that isn't /albumart? should still be
		// treated as a local file so the disk-read fallback can run.
		{"absolute non-albumart path", "/albumart-other?x=1", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isArtworkRedirectURL(tc.filePath); got != tc.want {
				t.Errorf("isArtworkRedirectURL(%q) = %v, want %v", tc.filePath, got, tc.want)
			}
		})
	}
}
