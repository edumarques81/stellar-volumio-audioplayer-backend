package musicfile

import "testing"

// TestIsResourceFork mirrors the edge-case table from
// internal/domain/player/service_test.go:TestIsAppleDouble (the existing,
// unshared analog for this predicate) plus one exFAT-realistic case observed
// on the live Pi.
func TestIsResourceFork(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		// AppleDouble ghosts at various path depths
		{"deep path with ._01.flac", "/some/path/._01.flac", true},
		{"root-level ghost", "/._something.flac", true},
		{"bare basename ghost", "._01.flac", true},
		{"NAS path with ghost", "NAS/MusicLibrary/Album/._01-Track.flac", true},

		// Real files (not resource forks)
		{"regular numbered track", "/some/path/01.flac", false},
		{"deep path no prefix", "Music/Album/01-Track.flac", false},
		{"bare basename non-ghost", "01.flac", false},

		// Disambiguation: `__` prefix (Python dunder etc.) must NOT match
		{"double underscore prefix", "/dir/__init__.py", false},
		{"single dot file", "/.hidden", false},
		{"single dot in basename", "/dir/.gitignore", false},

		// Edge cases
		{"empty string", "", false},
		{"just the prefix", "._", true},
		{"trailing slash with ghost basename", "/dir/._foo.flac", true},

		// exFAT-realistic case observed on the live Pi SSD
		{"real Pi junk basename", "USB/RStrauss Also Sprach Zarathustra Till Eulenspiegel Karajn VPO__FLAC_352k-24b/._RStrauss Also Sprach Zarathustra.flac", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsResourceFork(tc.path); got != tc.want {
				t.Errorf("IsResourceFork(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestCountUntagged pins the DATA-02 skipped-file predicate: real (non
// resource-fork) songs with an empty "Album" tag are counted; resource-fork
// junk with an empty Album is not "untagged real music" and must not be
// counted.
func TestCountUntagged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		songs []map[string]string
		want  int
	}{
		{
			name:  "empty slice",
			songs: []map[string]string{},
			want:  0,
		},
		{
			name: "all real and tagged",
			songs: []map[string]string{
				{"file": "USB/Album/01.flac", "Album": "Some Album"},
				{"file": "USB/Album/02.flac", "Album": "Some Album"},
			},
			want: 0,
		},
		{
			name: "one real untagged among several tagged",
			songs: []map[string]string{
				{"file": "USB/Album/01.flac", "Album": "Some Album"},
				{"file": "USB/Untagged/01.flac", "Album": ""},
				{"file": "USB/Album/02.flac", "Album": "Some Album"},
			},
			want: 1,
		},
		{
			name: "resource-fork entries with empty Album are not counted",
			songs: []map[string]string{
				{"file": "USB/Album/._01.flac", "Album": ""},
				{"file": "USB/Album/01.flac", "Album": "Some Album"},
			},
			want: 0,
		},
		{
			name: "song with no file key at all is treated as real",
			songs: []map[string]string{
				{"Album": ""},
			},
			want: 1,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CountUntagged(tc.songs); got != tc.want {
				t.Errorf("CountUntagged(%+v) = %d, want %d", tc.songs, got, tc.want)
			}
		})
	}
}
