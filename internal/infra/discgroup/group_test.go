package discgroup

import "testing"

// TestGroup_RealCorpus is built from the REAL folder layout, MPD `Disc` tag
// values, and `search base` track totals captured live from the Pi on
// 2026-08-12 (03-CONTEXT.md, D-04..D-07) -- not invented examples. It covers
// all four live multi-disc box sets that must collapse to one Group each
// (Mahler, Tosca, Rated R, Woody Allen) and the one live duplicate-title
// cluster that must NOT collapse (Miles Davis - Kind Of Blue, D-06): all
// three of its folders carry the identical Disc value "1", so the
// distinct-Disc-values check (interfaces contract, check 1) fails and the
// cluster is returned unmerged. This negative case is load-bearing --
// grouping on title+artist alone would silently merge 3 distinct releases
// (2x DSF + 1x FLAC 352.8/24) into one tile.
func TestGroup_RealCorpus(t *testing.T) {
	t.Parallel()

	t.Run("Mahler The Symphonies: 11 CD-folders collapse to 1 Group DiscCount=11", func(t *testing.T) {
		t.Parallel()

		// mpc -f "%disc% | %file%" search base "USB/Mahler The Symphonies"
		// returned Disc 1..11 across CD 01..CD 11, 63 songs total
		// (verified live 2026-08-12).
		folders := []Folder{
			{Album: "Mahler: The Symphonies", AlbumArtist: "Leonard Bernstein", Directory: "USB/Mahler The Symphonies/CD 01", Disc: "1", FirstTrack: "USB/Mahler The Symphonies/CD 01/01 - Symphony No. 1.dsf", TrackCount: 5, TotalTime: 3000, Format: "DSD64", Genre: "Classical"},
			{Album: "Mahler: The Symphonies", AlbumArtist: "Leonard Bernstein", Directory: "USB/Mahler The Symphonies/CD 02", Disc: "2", FirstTrack: "USB/Mahler The Symphonies/CD 02/01.dsf", TrackCount: 5, TotalTime: 3100, Format: "DSD64", Genre: "Classical"},
			{Album: "Mahler: The Symphonies", AlbumArtist: "Leonard Bernstein", Directory: "USB/Mahler The Symphonies/CD 03", Disc: "3", FirstTrack: "USB/Mahler The Symphonies/CD 03/01.dsf", TrackCount: 5, TotalTime: 3200, Format: "DSD64", Genre: "Classical"},
			{Album: "Mahler: The Symphonies", AlbumArtist: "Leonard Bernstein", Directory: "USB/Mahler The Symphonies/CD 04", Disc: "4", FirstTrack: "USB/Mahler The Symphonies/CD 04/01.dsf", TrackCount: 6, TotalTime: 3300, Format: "DSD64", Genre: "Classical"},
			{Album: "Mahler: The Symphonies", AlbumArtist: "Leonard Bernstein", Directory: "USB/Mahler The Symphonies/CD 05", Disc: "5", FirstTrack: "USB/Mahler The Symphonies/CD 05/01.dsf", TrackCount: 6, TotalTime: 3400, Format: "DSD64", Genre: "Classical"},
			{Album: "Mahler: The Symphonies", AlbumArtist: "Leonard Bernstein", Directory: "USB/Mahler The Symphonies/CD 06", Disc: "6", FirstTrack: "USB/Mahler The Symphonies/CD 06/01.dsf", TrackCount: 6, TotalTime: 3500, Format: "DSD64", Genre: "Classical"},
			{Album: "Mahler: The Symphonies", AlbumArtist: "Leonard Bernstein", Directory: "USB/Mahler The Symphonies/CD 07", Disc: "7", FirstTrack: "USB/Mahler The Symphonies/CD 07/01.dsf", TrackCount: 6, TotalTime: 3600, Format: "DSD64", Genre: "Classical"},
			{Album: "Mahler: The Symphonies", AlbumArtist: "Leonard Bernstein", Directory: "USB/Mahler The Symphonies/CD 08", Disc: "8", FirstTrack: "USB/Mahler The Symphonies/CD 08/01.dsf", TrackCount: 6, TotalTime: 3700, Format: "DSD64", Genre: "Classical"},
			{Album: "Mahler: The Symphonies", AlbumArtist: "Leonard Bernstein", Directory: "USB/Mahler The Symphonies/CD 09", Disc: "9", FirstTrack: "USB/Mahler The Symphonies/CD 09/01.dsf", TrackCount: 6, TotalTime: 3800, Format: "DSD64", Genre: "Classical"},
			{Album: "Mahler: The Symphonies", AlbumArtist: "Leonard Bernstein", Directory: "USB/Mahler The Symphonies/CD 10", Disc: "10", FirstTrack: "USB/Mahler The Symphonies/CD 10/01.dsf", TrackCount: 6, TotalTime: 3900, Format: "DSD64", Genre: "Classical"},
			{Album: "Mahler: The Symphonies", AlbumArtist: "Leonard Bernstein", Directory: "USB/Mahler The Symphonies/CD 11", Disc: "11", FirstTrack: "USB/Mahler The Symphonies/CD 11/01.dsf", TrackCount: 6, TotalTime: 4000, Format: "DSD64", Genre: "Classical"},
		}
		const wantTrackCount = 5 + 5 + 5 + 6 + 6 + 6 + 6 + 6 + 6 + 6 + 6 // = 63, matches live "search base" total
		if wantTrackCount != 63 {
			t.Fatalf("fixture arithmetic error: wantTrackCount = %d, live-measured total is 63", wantTrackCount)
		}

		got := Group(folders)
		if len(got) != 1 {
			t.Fatalf("Group() returned %d groups, want 1 (Mahler must collapse to a single tile): %+v", len(got), got)
		}
		g := got[0]
		if g.DiscCount != 11 {
			t.Errorf("DiscCount = %d, want 11", g.DiscCount)
		}
		if g.RootDir != "USB/Mahler The Symphonies" {
			t.Errorf("RootDir = %q, want %q", g.RootDir, "USB/Mahler The Symphonies")
		}
		if g.TrackCount != wantTrackCount {
			t.Errorf("TrackCount = %d, want %d (sum of all 11 folders)", g.TrackCount, wantTrackCount)
		}
		if g.FirstTrack != folders[0].FirstTrack {
			t.Errorf("FirstTrack = %q, want %q (disc-1 folder's FirstTrack, for album art)", g.FirstTrack, folders[0].FirstTrack)
		}
	})

	t.Run("Miles Davis - Kind Of Blue: identical Disc=1 on all 3 folders must NOT group (D-06 negative case)", func(t *testing.T) {
		t.Parallel()

		// mpc -f "disc=%disc% album=%album% albumartist=%albumartist%" search
		// album "Kind Of Blue" returned Disc="1" on every file across all
		// three folders (verified live 2026-08-12) -- these are 3 distinct
		// releases (2x DSF, 1x FLAC 352.8/24), not discs of one box set.
		// Because every folder's Disc value is identical ("1"), the
		// distinct-Disc-values check (interfaces contract, check 1) fails
		// even though all three share Album+AlbumArtist, so the cluster must
		// pass through UNCHANGED as 3 separate Groups.
		folders := []Folder{
			{
				Album: "Miles Davis - Kind Of Blue", AlbumArtist: "Miles Davis & company",
				Directory: "USB/HDTT13879 Miles Davis - Kind Of Blue-DSF-11289k-1b", Disc: "1",
				FirstTrack: "USB/HDTT13879 Miles Davis - Kind Of Blue-DSF-11289k-1b/01 - So What.dsf",
				TrackCount: 5, TotalTime: 2700, Format: "DSD64", Genre: "Jazz",
			},
			{
				Album: "Miles Davis - Kind Of Blue", AlbumArtist: "Miles Davis & company",
				Directory: "USB/HDTT13879 Miles Davis - Kind Of Blue-DSF-11289k-1b/Miles Davis - Kind Of Blue-DSF-11289k-1b Corrected Speed", Disc: "1",
				FirstTrack: "USB/HDTT13879 Miles Davis - Kind Of Blue-DSF-11289k-1b/Miles Davis - Kind Of Blue-DSF-11289k-1b Corrected Speed/01 - So What.dsf",
				TrackCount: 5, TotalTime: 2650, Format: "DSD64", Genre: "Jazz",
			},
			{
				Album: "Miles Davis - Kind Of Blue", AlbumArtist: "Miles Davis & company",
				Directory: "USB/Miles Davis - Kind Of Blue-FLAC-352k-24b Corrected Speed", Disc: "1",
				FirstTrack: "USB/Miles Davis - Kind Of Blue-FLAC-352k-24b Corrected Speed/01 - So What.flac",
				TrackCount: 5, TotalTime: 2680, Format: "352.8kHz/24bit FLAC", Genre: "Jazz",
			},
		}

		got := Group(folders)
		if len(got) != 3 {
			t.Fatalf("Group() returned %d groups, want 3 (Kind Of Blue must NOT merge -- Disc tag identical across all 3 folders, distinct-Disc-values check must fail): %+v", len(got), got)
		}
		for i, g := range got {
			if g.DiscCount > 1 {
				t.Errorf("group %d DiscCount = %d, want 0 or 1 (ungrouped): %+v", i, g.DiscCount, g)
			}
			f := folders[i]
			if g.Album != f.Album || g.AlbumArtist != f.AlbumArtist {
				t.Errorf("group %d Album/AlbumArtist changed: got (%q,%q), want (%q,%q)", i, g.Album, g.AlbumArtist, f.Album, f.AlbumArtist)
			}
			if g.RootDir != f.Directory {
				t.Errorf("group %d RootDir = %q, want folder's own Directory %q (ungrouped passthrough)", i, g.RootDir, f.Directory)
			}
			if g.TrackCount != f.TrackCount {
				t.Errorf("group %d TrackCount = %d, want %d (unchanged from input)", i, g.TrackCount, f.TrackCount)
			}
			if g.Format != f.Format {
				t.Errorf("group %d Format = %q, want %q (unchanged from input, quality badge still needed downstream)", i, g.Format, f.Format)
			}
		}
	})

	t.Run("Puccini Tosca (Callas): anchor is the parent of the CD folders, one level below the artist folder", func(t *testing.T) {
		t.Parallel()

		// The artist-named folder "USB/Maria Callas, Victor De Sabata,
		// Orchestra del Teatro della Scala di Milano" contains ONE folder,
		// "Puccini Tosca by Maria Callas (Remastered 2022, Version 1953)",
		// and CD 1..3 live inside THAT -- verified live 2026-08-12. The
		// grouping anchor (RootDir) must be the parent of the disc folders,
		// not the parent of the artist folder.
		const albumRoot = "USB/Maria Callas, Victor De Sabata, Orchestra del Teatro della Scala di Milano/Puccini Tosca by Maria Callas (Remastered 2022, Version 1953)"
		folders := []Folder{
			{Album: "Tosca", AlbumArtist: "Maria Callas", Directory: albumRoot + "/CD 1", Disc: "1", FirstTrack: albumRoot + "/CD 1/01.flac", TrackCount: 8, TotalTime: 2200, Format: "FLAC", Genre: "Classical"},
			{Album: "Tosca", AlbumArtist: "Maria Callas", Directory: albumRoot + "/CD 2", Disc: "2", FirstTrack: albumRoot + "/CD 2/01.flac", TrackCount: 7, TotalTime: 2100, Format: "FLAC", Genre: "Classical"},
			{Album: "Tosca", AlbumArtist: "Maria Callas", Directory: albumRoot + "/CD 3", Disc: "3", FirstTrack: albumRoot + "/CD 3/01.flac", TrackCount: 6, TotalTime: 1900, Format: "FLAC", Genre: "Classical"},
		}
		const wantTrackCount = 8 + 7 + 6

		got := Group(folders)
		if len(got) != 1 {
			t.Fatalf("Group() returned %d groups, want 1: %+v", len(got), got)
		}
		g := got[0]
		if g.DiscCount != 3 {
			t.Errorf("DiscCount = %d, want 3", g.DiscCount)
		}
		if g.RootDir != albumRoot {
			t.Errorf("RootDir = %q, want %q (parent of the CD folders, NOT the artist folder)", g.RootDir, albumRoot)
		}
		if g.TrackCount != wantTrackCount {
			t.Errorf("TrackCount = %d, want %d", g.TrackCount, wantTrackCount)
		}
	})

	t.Run("Rated R - Deluxe Edition: 2 CD-folders collapse to 1 Group DiscCount=2, TrackCount=26", func(t *testing.T) {
		t.Parallel()

		// mpc search base "USB/Queens Of The Stone Age/Rated R - Deluxe
		// Edition" returned exactly 26 songs total (verified live
		// 2026-08-12).
		const albumRoot = "USB/Queens Of The Stone Age/Rated R - Deluxe Edition"
		folders := []Folder{
			{Album: "Rated R - Deluxe Edition", AlbumArtist: "Queens Of The Stone Age", Directory: albumRoot + "/CD 1", Disc: "1", FirstTrack: albumRoot + "/CD 1/01.flac", TrackCount: 13, TotalTime: 2500, Format: "FLAC", Genre: "Rock"},
			{Album: "Rated R - Deluxe Edition", AlbumArtist: "Queens Of The Stone Age", Directory: albumRoot + "/CD 2", Disc: "2", FirstTrack: albumRoot + "/CD 2/01.flac", TrackCount: 13, TotalTime: 2400, Format: "FLAC", Genre: "Rock"},
		}
		const wantTrackCount = 13 + 13
		if wantTrackCount != 26 {
			t.Fatalf("fixture arithmetic error: wantTrackCount = %d, live-measured total is 26", wantTrackCount)
		}

		got := Group(folders)
		if len(got) != 1 {
			t.Fatalf("Group() returned %d groups, want 1: %+v", len(got), got)
		}
		g := got[0]
		if g.DiscCount != 2 {
			t.Errorf("DiscCount = %d, want 2", g.DiscCount)
		}
		if g.RootDir != albumRoot {
			t.Errorf("RootDir = %q, want %q", g.RootDir, albumRoot)
		}
		if g.TrackCount != wantTrackCount {
			t.Errorf("TrackCount = %d, want %d", g.TrackCount, wantTrackCount)
		}
	})

	t.Run("BD Music...Woody Allen Vol. 1: 2 CD-folders collapse to 1 Group DiscCount=2", func(t *testing.T) {
		t.Parallel()

		const albumRoot = "USB/Various Artists/BD Music Presents Woody Allen's Movies, Vol. 1"
		folders := []Folder{
			{Album: "BD Music Presents Woody Allen's Movies, Vol. 1", AlbumArtist: "Various Artists", Directory: albumRoot + "/CD 1", Disc: "1", FirstTrack: albumRoot + "/CD 1/01.flac", TrackCount: 10, TotalTime: 1800, Format: "FLAC", Genre: "Soundtrack"},
			{Album: "BD Music Presents Woody Allen's Movies, Vol. 1", AlbumArtist: "Various Artists", Directory: albumRoot + "/CD 2", Disc: "2", FirstTrack: albumRoot + "/CD 2/01.flac", TrackCount: 12, TotalTime: 2000, Format: "FLAC", Genre: "Soundtrack"},
		}
		const wantTrackCount = 10 + 12

		got := Group(folders)
		if len(got) != 1 {
			t.Fatalf("Group() returned %d groups, want 1: %+v", len(got), got)
		}
		g := got[0]
		if g.DiscCount != 2 {
			t.Errorf("DiscCount = %d, want 2", g.DiscCount)
		}
		if g.RootDir != albumRoot {
			t.Errorf("RootDir = %q, want %q", g.RootDir, albumRoot)
		}
		if g.TrackCount != wantTrackCount {
			t.Errorf("TrackCount = %d, want %d", g.TrackCount, wantTrackCount)
		}
	})
}

// TestGroup_DefensiveEdgeCases covers boundary conditions no live corpus data
// currently exercises. Every fixture here is SYNTHETIC (labeled as such),
// unlike TestGroup_RealCorpus's real-Pi-measured fixtures.
func TestGroup_DefensiveEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("synthetic: CD marker present but Disc tag missing on both folders -> NOT grouped", func(t *testing.T) {
		t.Parallel()

		folders := []Folder{
			{Album: "Some Box", AlbumArtist: "Some Artist", Directory: "USB/Some Box/CD 1", Disc: "", FirstTrack: "USB/Some Box/CD 1/01.flac", TrackCount: 4, Format: "FLAC"},
			{Album: "Some Box", AlbumArtist: "Some Artist", Directory: "USB/Some Box/CD 2", Disc: "", FirstTrack: "USB/Some Box/CD 2/01.flac", TrackCount: 5, Format: "FLAC"},
		}

		got := Group(folders)
		if len(got) != 2 {
			t.Fatalf("Group() returned %d groups, want 2 (empty Disc must fail check 1, even though CD marker matches and neither value is a duplicate): %+v", len(got), got)
		}
		for i, g := range got {
			if g.DiscCount > 1 {
				t.Errorf("group %d DiscCount = %d, want 0 or 1 (ungrouped)", i, g.DiscCount)
			}
			if g.RootDir != folders[i].Directory {
				t.Errorf("group %d RootDir = %q, want %q", i, g.RootDir, folders[i].Directory)
			}
		}
	})

	t.Run("synthetic: Disc tag distinct but no CD path marker -> NOT grouped", func(t *testing.T) {
		t.Parallel()

		folders := []Folder{
			{Album: "Some Album", AlbumArtist: "Some Artist", Directory: "USB/Some Album/Version A", Disc: "1", FirstTrack: "USB/Some Album/Version A/01.flac", TrackCount: 9, Format: "FLAC"},
			{Album: "Some Album", AlbumArtist: "Some Artist", Directory: "USB/Some Album/Version B", Disc: "2", FirstTrack: "USB/Some Album/Version B/01.flac", TrackCount: 9, Format: "FLAC"},
		}

		got := Group(folders)
		if len(got) != 2 {
			t.Fatalf("Group() returned %d groups, want 2 (no /CD ?\\d+/ path marker must fail check 2, even though Disc values are distinct and non-empty): %+v", len(got), got)
		}
		for i, g := range got {
			if g.DiscCount > 1 {
				t.Errorf("group %d DiscCount = %d, want 0 or 1 (ungrouped)", i, g.DiscCount)
			}
			if g.RootDir != folders[i].Directory {
				t.Errorf("group %d RootDir = %q, want %q", i, g.RootDir, folders[i].Directory)
			}
		}
	})

	t.Run("synthetic: cluster size 1 with a lone CD-marker folder is NOT treated as a box set", func(t *testing.T) {
		t.Parallel()

		folders := []Folder{
			{Album: "Solo Release", AlbumArtist: "Solo Artist", Directory: "USB/Solo Release/CD 1", Disc: "1", FirstTrack: "USB/Solo Release/CD 1/01.flac", TrackCount: 11, Format: "FLAC"},
		}

		got := Group(folders)
		if len(got) != 1 {
			t.Fatalf("Group() returned %d groups, want 1: %+v", len(got), got)
		}
		g := got[0]
		if g.DiscCount > 1 {
			t.Errorf("DiscCount = %d, want 0 or 1 (a lone folder is never a multi-disc box set, regardless of CD marker/Disc tag)", g.DiscCount)
		}
		if g.RootDir != folders[0].Directory {
			t.Errorf("RootDir = %q, want %q (unchanged passthrough)", g.RootDir, folders[0].Directory)
		}
		if g.TrackCount != folders[0].TrackCount {
			t.Errorf("TrackCount = %d, want %d", g.TrackCount, folders[0].TrackCount)
		}
	})

	t.Run("synthetic: completely unrelated single-folder album passes through unchanged", func(t *testing.T) {
		t.Parallel()

		folders := []Folder{
			{Album: "The Future Is Now", AlbumArtist: "Some Band", Directory: "USB/Some Band/The Future Is Now", Disc: "1", FirstTrack: "USB/Some Band/The Future Is Now/01.flac", TrackCount: 12, TotalTime: 2600, Format: "FLAC", Genre: "Rock"},
		}

		got := Group(folders)
		if len(got) != 1 {
			t.Fatalf("Group() returned %d groups, want 1: %+v", len(got), got)
		}
		g := got[0]
		if g.RootDir != folders[0].Directory {
			t.Errorf("RootDir = %q, want %q (equal to own Directory, unchanged existing behavior)", g.RootDir, folders[0].Directory)
		}
		if g.Album != folders[0].Album || g.AlbumArtist != folders[0].AlbumArtist {
			t.Errorf("Album/AlbumArtist changed: got (%q,%q), want (%q,%q)", g.Album, g.AlbumArtist, folders[0].Album, folders[0].AlbumArtist)
		}
		if g.TrackCount != folders[0].TrackCount {
			t.Errorf("TrackCount = %d, want %d", g.TrackCount, folders[0].TrackCount)
		}
	})
}
