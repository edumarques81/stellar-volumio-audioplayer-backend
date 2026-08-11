package dupebadge

import (
	"fmt"
	"strings"
	"testing"
)

// TestCompute_RealDuplicateGroupsAndEdgeCases is built from the real
// duplicate-group data captured live from the Pi on 2026-08-12
// (.planning/phases/03-browse-experience/03-CONTEXT.md), for the four
// groups that remain duplicate (not disc-set) candidates after Plan 01's
// discgroup.GroupFolders() has already run: Kind Of Blue (quality tier),
// The Future Is Now (quality/format tier), The Light For Days (source
// tier), and Djesse Vol. 4 (Deluxe) (negative case — nothing tracked
// differs). Two additional cases are synthetic/defensive, labeled as such
// in their names: the disc tier (Plan 01's grouping removes all live disc
// cases from this function's remit, so no live example exists) and the
// precedence short-circuit (quality-and-source-both-vary must yield ONLY
// the quality badge).
func TestCompute_RealDuplicateGroupsAndEdgeCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		candidates []Candidate
		want       []string
	}{
		{
			name: "Miles Davis - Kind Of Blue: quality tier — 2 distinct qualities among 3 versions (2x DSD256 DSF + 1x 352.8kHz/24bit FLAC), same Disc=1, same Source=usb",
			candidates: []Candidate{
				{Title: "Miles Davis - Kind Of Blue", Artist: "Miles Davis & company", Quality: "DSD256", Disc: "1", Source: "usb"},
				{Title: "Miles Davis - Kind Of Blue", Artist: "Miles Davis & company", Quality: "DSD256", Disc: "1", Source: "usb"},
				{Title: "Miles Davis - Kind Of Blue", Artist: "Miles Davis & company", Quality: "352.8kHz/24bit FLAC", Disc: "1", Source: "usb"},
			},
			want: []string{"DSD256", "DSD256", "352.8kHz/24bit FLAC"},
		},
		{
			name: "toe - The Future Is Now: quality/format tier — FLAC vs WAV, same sample/bit rate, same Source=usb",
			candidates: []Candidate{
				{Title: "The Future Is Now", Artist: "toe", Quality: "44.1kHz/16bit FLAC", Disc: "", Source: "usb"},
				{Title: "The Future Is Now", Artist: "toe", Quality: "44.1kHz/16bit WAV", Disc: "", Source: "usb"},
			},
			want: []string{"44.1kHz/16bit FLAC", "44.1kHz/16bit WAV"},
		},
		{
			name: "Jacob Collier - The Light For Days: source tier — same quality, no Disc tag, differing Source (local vs usb)",
			candidates: []Candidate{
				{Title: "The Light For Days", Artist: "Jacob Collier", Quality: "96kHz/24bit FLAC", Disc: "", Source: "local"},
				{Title: "The Light For Days", Artist: "Jacob Collier", Quality: "96kHz/24bit FLAC", Disc: "", Source: "usb"},
			},
			want: []string{"LOCAL", "USB"},
		},
		{
			// NEGATIVE, load-bearing per D-02 rule 4: same quality, no Disc,
			// same Source (both usb) — only the real paths differ, and this
			// function does not track paths. Nothing tracked varies, so no
			// badge is emitted rather than a meaningless one. This is
			// intentional, not a bug — see D-02 rule 4 in 03-CONTEXT.md.
			name: "Jacob Collier - Djesse Vol. 4 (Deluxe): NEGATIVE — same quality, no Disc, same Source(usb) — nothing tracked differs, expect no badge",
			candidates: []Candidate{
				{Title: "Djesse Vol. 4 (Deluxe)", Artist: "Jacob Collier", Quality: "96kHz/24bit FLAC", Disc: "", Source: "usb"},
				{Title: "Djesse Vol. 4 (Deluxe)", Artist: "Jacob Collier", Quality: "96kHz/24bit FLAC", Disc: "", Source: "usb"},
			},
			want: []string{"", ""},
		},
		{
			name: "unique album (cluster size 1) — no badge (BROWSE-03, D-03)",
			candidates: []Candidate{
				{Title: "Some Unrelated Unique Album", Artist: "Some Unrelated Artist", Quality: "44.1kHz/16bit FLAC", Disc: "", Source: "nas"},
			},
			want: []string{""},
		},
		{
			// SYNTHETIC/DEFENSIVE: Plan 01's grouping (discgroup.GroupFolders)
			// removes every live disc-tier duplicate case (Mahler, Tosca,
			// Rated R, Woody Allen) from this function's remit, so no live
			// example of the disc tier firing exists. This case proves the
			// precedence tier exists and works, using invented data.
			name: "synthetic/defensive: disc tier — same quality, different non-empty Disc, same Source (no live example survives Plan 01's grouping)",
			candidates: []Candidate{
				{Title: "Synthetic Disc Tier Album", Artist: "Synthetic Artist", Quality: "96kHz/24bit FLAC", Disc: "1", Source: "usb"},
				{Title: "Synthetic Disc Tier Album", Artist: "Synthetic Artist", Quality: "96kHz/24bit FLAC", Disc: "2", Source: "usb"},
			},
			want: []string{"Disc 1", "Disc 2"},
		},
		{
			// SYNTHETIC/DEFENSIVE: proves precedence short-circuits rather
			// than combining — a cluster varying in BOTH quality AND source
			// must show ONLY the quality badge.
			name: "synthetic/defensive: precedence short-circuit — cluster varies in BOTH quality and source, expect ONLY the quality badge",
			candidates: []Candidate{
				{Title: "Synthetic Precedence Album", Artist: "Synthetic Artist Two", Quality: "44.1kHz/16bit FLAC", Disc: "", Source: "local"},
				{Title: "Synthetic Precedence Album", Artist: "Synthetic Artist Two", Quality: "96kHz/24bit FLAC", Disc: "", Source: "usb"},
			},
			want: []string{"44.1kHz/16bit FLAC", "96kHz/24bit FLAC"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Compute(tc.candidates)

			if len(got) != len(tc.want) {
				t.Fatalf("Compute() returned %d badges, want %d (got=%v, want=%v)", len(got), len(tc.want), got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("Compute()[%d] = %q, want %q (full got=%v, want=%v)", i, got[i], tc.want[i], got, tc.want)
				}
			}
		})
	}
}

// mirrorFormatQualityLabel is a byte-for-byte copy of
// internal/domain/library.formatQualityLabel (cached_service.go:12), kept
// here ONLY to guard against fixture drift (hard constraint 5): this
// package's Compute() accepts Candidate.Quality as an already-formatted
// string per the <interfaces> contract, so Compute() itself never formats
// anything — but the *fixtures* above encode formatQualityLabel's real
// output for real sample_rate/bit_depth/track_type triples. Layering
// (internal/infra must not import internal/domain) forbids importing the
// real function directly, so it is mirrored here and asserted identical to
// the literal fixture strings in TestQualityLabelMirror_MatchesFixtures. If
// the real formatQualityLabel ever changes, this test will not detect it
// automatically — but it prevents this package's own fixtures from being
// hand-typed incorrectly relative to the documented formatting rules.
func mirrorFormatQualityLabel(sampleRate, bitDepth int, trackType string) string {
	if sampleRate == 0 && bitDepth == 0 && trackType == "" {
		return ""
	}

	tt := strings.ToUpper(trackType)

	if trackType == "dsf" || trackType == "dff" || trackType == "dsd" {
		switch {
		case sampleRate >= 11289600 || sampleRate == 176400:
			return "DSD256"
		case sampleRate >= 5644800 || sampleRate == 88200:
			return "DSD128"
		case sampleRate >= 2822400:
			return "DSD64"
		default:
			return "DSD"
		}
	}

	var parts []string
	if sampleRate > 0 {
		if sampleRate%1000 == 0 {
			parts = append(parts, fmt.Sprintf("%dkHz", sampleRate/1000))
		} else {
			parts = append(parts, fmt.Sprintf("%.1fkHz", float64(sampleRate)/1000))
		}
	}
	if bitDepth > 0 {
		parts = append(parts, fmt.Sprintf("%dbit", bitDepth))
	}

	label := strings.Join(parts, "/")
	if tt != "" && label != "" {
		label += " " + tt
	} else if tt != "" {
		label = tt
	}
	return label
}

// TestQualityLabelMirror_MatchesFixtures asserts mirrorFormatQualityLabel,
// fed the real sample_rate/bit_depth/track_type triples measured live from
// the Pi 2026-08-12, produces exactly the literal Quality strings used in
// TestCompute_RealDuplicateGroupsAndEdgeCases's fixtures above — so the two
// cannot silently drift apart (hard constraint 5).
func TestQualityLabelMirror_MatchesFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sampleRate int
		bitDepth   int
		trackType  string
		want       string
	}{
		{"Kind Of Blue: DSF folder, native 11.2896MHz rate", 11289600, 0, "dsf", "DSD256"},
		{"Kind Of Blue: FLAC folder, 352.8kHz/24bit", 352800, 24, "flac", "352.8kHz/24bit FLAC"},
		{"The Future Is Now: FLAC, 44.1kHz/16bit", 44100, 16, "flac", "44.1kHz/16bit FLAC"},
		{"The Future Is Now: WAV, 44.1kHz/16bit", 44100, 16, "wav", "44.1kHz/16bit WAV"},
		{"The Light For Days: FLAC, 96kHz/24bit (both copies)", 96000, 24, "flac", "96kHz/24bit FLAC"},
		{"Djesse Vol. 4 (Deluxe): FLAC, 96kHz/24bit (both copies)", 96000, 24, "flac", "96kHz/24bit FLAC"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := mirrorFormatQualityLabel(tc.sampleRate, tc.bitDepth, tc.trackType); got != tc.want {
				t.Errorf("mirrorFormatQualityLabel(%d, %d, %q) = %q, want %q", tc.sampleRate, tc.bitDepth, tc.trackType, got, tc.want)
			}
		})
	}
}
