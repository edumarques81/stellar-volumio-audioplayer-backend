package library

import (
	"fmt"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/artistidentity"
)

// --- Artist drill-in vs artist listing name-agreement tests ---
//
// The artist LIST collapses raw multi-credit MPD tag values to a single
// primary performer via artistidentity.Collapse (GetArtists at
// service.go, and the cache-primary path in internal/infra/cache
// buildArtists). The artist DRILL-IN (GetArtistAlbums) used to compare that
// already-collapsed name against the RAW AlbumArtist tag, so every album
// whose tag carried extra credits was filtered out and the artist opened
// completely empty.
//
// The fixtures below are the 13 raw album_artist values from the live
// library that no artist row matched -- 13 of 71 albums, leaving 9 of 49
// artists opening empty and 2 more (Nat King Cole, Snarky Puppy) showing a
// partial list, which is worse because nothing looks wrong.

// liveMultiCreditAlbumArtists are verbatim album_artist values read from
// ~/stellar-backend/data/library.db on the Pi, paired with the collapsed
// name the artist list surfaces for each.
var liveMultiCreditAlbumArtists = []struct {
	name       string
	rawTag     string
	wantArtist string
}{
	{"adderley", "Adderley - Coltrane - Chambers - Cobb - Kelly", "Adderley"},
	{"duke ellington", "Duke Ellington, John Coltrane", "Duke Ellington"},
	{"ella role suffix", "Ella Fitzgerald - vocals  Paul Smith - piano", "Ella Fitzgerald"},
	{"ella with orchestra", "Ella Fitzgerald with Nelson Riddle And His Orchestra", "Ella Fitzgerald"},
	{"horenstein lso", "Jascha Horenstein - London Symphony Orchestra", "Jascha Horenstein"},
	{"horenstein rpo", "Jascha Horenstein - Royal Philharmonic Orchestra", "Jascha Horenstein"},
	{"los angeles phil", "Los Angeles Philharmonic, Zubin Mehta", "Los Angeles Philharmonic"},
	{"maria callas", "Maria Callas, Victor De Sabata, Orchestra del Teatro della Scala di Milano", "Maria Callas"},
	{"miles ahead", "Miles Davis - Arranged and Directed by Gil Evans", "Miles Davis"},
	{"nat king cole", "Nat King Cole - with orchestra conducted by Billy May", "Nat King Cole"},
	{"concertgebouw", "Royal Concertgebouw Orchestra, New York Philharmonic, Wiener Philharmonic Orchestra, Leonard Bernstein", "Royal Concertgebouw Orchestra"},
	{"seiji ozawa", "Seiji Ozawa, Saito Kinen Orchestra", "Seiji Ozawa"},
	{"snarky puppy", "Snarky Puppy, Metropole Orkest", "Snarky Puppy"},
}

// TestService_GetArtistAlbums_CollapsedName_MatchesMultiCreditAlbumArtistTag
// is the regression test for the empty-drill-in bug: querying with the
// collapsed name the artist list shows must return the album whose raw
// AlbumArtist tag collapses to that same name.
func TestService_GetArtistAlbums_CollapsedName_MatchesMultiCreditAlbumArtistTag(t *testing.T) {
	for _, tc := range liveMultiCreditAlbumArtists {
		t.Run(tc.name, func(t *testing.T) {
			// Guard the fixture itself: the listing path really does
			// surface tc.wantArtist for this raw tag.
			if got := artistidentity.Collapse(tc.rawTag); got != tc.wantArtist {
				t.Fatalf("fixture drift: Collapse(%q) = %q, want %q", tc.rawTag, got, tc.wantArtist)
			}

			mockMPD := &MockMPDClient{
				GetAlbumDetailsResp: map[string][]AlbumDetails{
					"USB": {
						{
							Album:       "Some Album",
							AlbumArtist: tc.rawTag,
							TrackCount:  8,
							FirstTrack:  "USB/Some Artist/Some Album/01.flac",
							Format:      "44100:16:2",
						},
					},
				},
			}

			service := NewService(mockMPD, &MockPathClassifier{})

			resp := service.GetArtistAlbums(GetArtistAlbumsRequest{
				Artist: tc.wantArtist,
				Sort:   SortAlphabetical,
			})

			if len(resp.Albums) != 1 {
				t.Fatalf("drill-in on collapsed name %q returned %d albums, want 1 (raw tag %q)",
					tc.wantArtist, len(resp.Albums), tc.rawTag)
			}
		})
	}
}

// TestService_GetArtistAlbums_RawTag_StillMatches keeps the pre-existing
// behaviour intact: a caller that passes the raw multi-credit tag (an older
// client, or a cache row written before the collapse rule landed) must
// still resolve to the album.
func TestService_GetArtistAlbums_RawTag_StillMatches(t *testing.T) {
	for _, tc := range liveMultiCreditAlbumArtists {
		t.Run(tc.name, func(t *testing.T) {
			mockMPD := &MockMPDClient{
				GetAlbumDetailsResp: map[string][]AlbumDetails{
					"USB": {
						{
							Album: "Some Album", AlbumArtist: tc.rawTag, TrackCount: 8,
							FirstTrack: "USB/Some Artist/Some Album/01.flac",
							Format:     "44100:16:2",
						},
					},
				},
			}

			service := NewService(mockMPD, &MockPathClassifier{})

			resp := service.GetArtistAlbums(GetArtistAlbumsRequest{Artist: tc.rawTag})

			if len(resp.Albums) != 1 {
				t.Fatalf("drill-in on raw tag %q returned %d albums, want 1", tc.rawTag, len(resp.Albums))
			}
		})
	}
}

// TestService_GetArtistAlbums_CollapsedName_DoesNotOverMatch is the
// negative half: collapsing both sides must not turn the comparison into a
// prefix or substring match. Distinct artists that merely share a first
// word must stay separate.
func TestService_GetArtistAlbums_CollapsedName_DoesNotOverMatch(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		rawTags []string
	}{
		{
			name:    "shared first name",
			query:   "Ella Fitzgerald",
			rawTags: []string{"Ella Mai", "Ella Henderson, Rudimental"},
		},
		{
			name:    "shared surname",
			query:   "Miles Davis",
			rawTags: []string{"Miles Kane", "Miles Davis Quintet"},
		},
		{
			name:    "different orchestra",
			query:   "Royal Concertgebouw Orchestra",
			rawTags: []string{"Royal Philharmonic Orchestra, Charles Dutoit"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			details := make([]AlbumDetails, 0, len(tc.rawTags))
			for i, raw := range tc.rawTags {
				details = append(details, AlbumDetails{
					Album: fmt.Sprintf("Album %d", i), AlbumArtist: raw, TrackCount: 8,
					FirstTrack: fmt.Sprintf("USB/Other/Album %d/01.flac", i),
					Format:     "44100:16:2",
				})
			}

			mockMPD := &MockMPDClient{
				GetAlbumDetailsResp: map[string][]AlbumDetails{"USB": details},
			}

			service := NewService(mockMPD, &MockPathClassifier{})

			resp := service.GetArtistAlbums(GetArtistAlbumsRequest{Artist: tc.query})

			if len(resp.Albums) != 0 {
				t.Fatalf("drill-in on %q matched %d unrelated albums, want 0: %+v",
					tc.query, len(resp.Albums), resp.Albums)
			}
		})
	}
}

// TestService_GetArtistAlbums_LooseTracksFallback_MatchesMultiCreditArtistTag
// covers the second flawed comparison. MPD's `search` is a case-insensitive
// SUBSTRING match, so querying the collapsed name does return songs whose
// raw Artist tag carries extra credits -- but the post-filter used to
// exact-match that raw tag against the collapsed name and drop every one of
// them, so the fallback could not rescue an empty drill-in either.
func TestService_GetArtistAlbums_LooseTracksFallback_MatchesMultiCreditArtistTag(t *testing.T) {
	const rawTag = "Ella Fitzgerald - vocals  Paul Smith - piano"

	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {
				{
					Album: "Some Album", AlbumArtist: "Other Artist", TrackCount: 5,
					FirstTrack: "INTERNAL/Other Artist/Some Album/01.flac",
					Format:     "44100:16:2",
				},
			},
		},
		FindTracksByArtistResp: map[string][]map[string]string{
			"Ella Fitzgerald": {
				{
					"file":   "INTERNAL/Loose/01-First.flac",
					"Title":  "First Loose Track",
					"Artist": rawTag,
					"Track":  "1",
					"Time":   "200",
				},
				{
					// A genuinely different artist that MPD's substring
					// search would NOT have returned; asserts the
					// post-filter still rejects non-matching credits.
					"file":   "INTERNAL/Loose/02-Other.flac",
					"Title":  "Unrelated Track",
					"Artist": "Ella Mai",
					"Track":  "2",
					"Time":   "150",
				},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetArtistAlbums(GetArtistAlbumsRequest{Artist: "Ella Fitzgerald"})

	if len(resp.LooseTracks) != 1 {
		t.Fatalf("Expected 1 loose track for the collapsed name, got %d: %+v",
			len(resp.LooseTracks), resp.LooseTracks)
	}
	if resp.LooseTracks[0].Title != "First Loose Track" {
		t.Errorf("LooseTracks[0].Title = %q, want %q", resp.LooseTracks[0].Title, "First Loose Track")
	}
}
