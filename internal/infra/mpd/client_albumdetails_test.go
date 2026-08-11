package mpd

import (
	"testing"

	"github.com/fhs/gompd/v2/mpd"
)

// TestGroupAlbumDetails pins groupAlbumDetails' grouping + skip-counting
// behavior per 01-02-PLAN.md's <behavior> cases (a)-(e). It requires no live
// MPD connection: groupAlbumDetails is a pure function over already-fetched
// song attrs.
func TestGroupAlbumDetails(t *testing.T) {
	tests := []struct {
		name        string
		songs       []mpd.Attrs
		wantAlbums  int
		wantSkipped int
	}{
		{
			name: "case a: 3 real tagged songs across 2 albums -> 2 albums, skipped=0",
			songs: []mpd.Attrs{
				{"file": "USB/Artist1/Album1/01.flac", "Album": "Album1", "AlbumArtist": "Artist1", "Time": "180"},
				{"file": "USB/Artist1/Album1/02.flac", "Album": "Album1", "AlbumArtist": "Artist1", "Time": "200"},
				{"file": "USB/Artist2/Album2/01.flac", "Album": "Album2", "AlbumArtist": "Artist2", "Time": "220"},
			},
			wantAlbums:  2,
			wantSkipped: 0,
		},
		{
			name: "case b: 2 real tagged + 1 real untagged -> skipped=1, untagged absent from albums",
			songs: []mpd.Attrs{
				{"file": "USB/Artist1/Album1/01.flac", "Album": "Album1", "AlbumArtist": "Artist1", "Time": "180"},
				{"file": "USB/Artist1/Album1/02.flac", "Album": "Album1", "AlbumArtist": "Artist1", "Time": "200"},
				{"file": "USB/Karajan/RStrauss/03.flac", "Album": "", "Artist": "Herbert von Karajan", "Time": "300"},
			},
			wantAlbums:  1,
			wantSkipped: 1,
		},
		{
			name: "case c: 2 real tagged + 1 resource-fork junk with empty Album -> junk excluded, skipped stays 0",
			songs: []mpd.Attrs{
				{"file": "USB/Artist1/Album1/01.flac", "Album": "Album1", "AlbumArtist": "Artist1", "Time": "180"},
				{"file": "USB/Artist1/Album1/02.flac", "Album": "Album1", "AlbumArtist": "Artist1", "Time": "200"},
				{"file": "USB/Artist1/Album1/._02.flac", "Album": "", "Artist": ""},
			},
			wantAlbums:  1,
			wantSkipped: 0,
		},
		{
			name:        "case d: empty input -> 0 albums, skipped=0",
			songs:       []mpd.Attrs{},
			wantAlbums:  0,
			wantSkipped: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			albums, skipped := groupAlbumDetails(tc.songs)
			if len(albums) != tc.wantAlbums {
				t.Errorf("len(albums) = %d, want %d", len(albums), tc.wantAlbums)
			}
			if skipped != tc.wantSkipped {
				t.Errorf("skipped = %d, want %d", skipped, tc.wantSkipped)
			}
		})
	}
}

// TestGroupAlbumDetails_AggregationUnchanged pins case (e): TrackCount,
// TotalTime, Format, and Genre aggregate the same way the current inline
// loop does (first-track-wins for Format/Genre, summed Time for TotalTime).
func TestGroupAlbumDetails_AggregationUnchanged(t *testing.T) {
	songs := []mpd.Attrs{
		{
			"file": "USB/Artist1/Album1/01.flac", "Album": "Album1", "AlbumArtist": "Artist1",
			"Time": "180", "Format": "44100:16:2", "Genre": "Ambient; Post-Rock",
		},
		{
			"file": "USB/Artist1/Album1/02.flac", "Album": "Album1", "AlbumArtist": "Artist1",
			"Time": "200", "Format": "48000:24:2", "Genre": "Electronic",
		},
	}

	albums, skipped := groupAlbumDetails(songs)
	if skipped != 0 {
		t.Fatalf("skipped = %d, want 0", skipped)
	}
	if len(albums) != 1 {
		t.Fatalf("len(albums) = %d, want 1", len(albums))
	}

	got := albums[0]
	if got.TrackCount != 2 {
		t.Errorf("TrackCount = %d, want 2", got.TrackCount)
	}
	if got.TotalTime != 380 {
		t.Errorf("TotalTime = %d, want 380", got.TotalTime)
	}
	if got.Format != "44100:16:2" {
		t.Errorf("Format = %q, want first-track-wins %q", got.Format, "44100:16:2")
	}
	if got.Genre != "Ambient / Post-Rock" {
		t.Errorf("Genre = %q, want %q", got.Genre, "Ambient / Post-Rock")
	}
}
