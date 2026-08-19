package mpd

import (
	"testing"
	"time"

	"github.com/fhs/gompd/v2/mpd"
)

// TestParseTagYear pins the Date-tag parser. Every "real library" case below is
// a value actually present in the 842-song library on the Pi (surveyed
// 2026-08-19 over the MPD protocol): the tag mixes bare years with full ISO
// dates, so both must yield the same year.
func TestParseTagYear(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "real library: bare year", raw: "2024", want: 2024},
		{name: "real library: bare year, older", raw: "1959", want: 1959},
		{name: "real library: full ISO date", raw: "2020-11-09", want: 2020},
		{name: "real library: ingest-written TDRC", raw: "2025-08-29", want: 2025},
		{name: "slash-separated date", raw: "2019/08/17", want: 2019},
		{name: "date with time", raw: "1997-06-16T00:00:00Z", want: 1997},
		{name: "surrounding whitespace", raw: "  1991  ", want: 1991},
		{name: "absent tag", raw: "", want: 0},
		{name: "zero date is not a year", raw: "0000", want: 0},
		{name: "too short to be a year", raw: "99", want: 0},
		{name: "not a number", raw: "Unknown", want: 0},
		{name: "leading digits are not a year", raw: "19th Century Recordings", want: 0},
		{name: "five-digit run is not a year", raw: "20199", want: 0},
		{name: "year must lead, not trail", raw: "recorded 1965", want: 0},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseTagYear(tc.raw); got != tc.want {
				t.Errorf("ParseTagYear(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

// TestGroupAlbumDetails_Year covers the first-non-zero-wins rule. This
// deliberately diverges from the first-track-wins precedent used for
// Format/Genre/Disc: seven albums in the real library carry no Date at all,
// and a Date missing from track 1 alone must not blank the whole album's year.
func TestGroupAlbumDetails_Year(t *testing.T) {
	tests := []struct {
		name  string
		songs []mpd.Attrs
		want  int
	}{
		{
			name: "every track tagged",
			songs: []mpd.Attrs{
				{"file": "USB/A/Alb/01.flac", "Album": "Alb", "AlbumArtist": "A", "Date": "2024"},
				{"file": "USB/A/Alb/02.flac", "Album": "Alb", "AlbumArtist": "A", "Date": "2024"},
			},
			want: 2024,
		},
		{
			name: "first track missing Date, later track supplies it",
			songs: []mpd.Attrs{
				{"file": "USB/A/Alb/01.flac", "Album": "Alb", "AlbumArtist": "A"},
				{"file": "USB/A/Alb/02.flac", "Album": "Alb", "AlbumArtist": "A", "Date": "1965-03-02"},
			},
			want: 1965,
		},
		{
			name: "no track tagged leaves the year unset",
			songs: []mpd.Attrs{
				{"file": "USB/Mingus Ah Um/01.flac", "Album": "Mingus Ah Um", "AlbumArtist": "Charles Mingus"},
				{"file": "USB/Mingus Ah Um/02.flac", "Album": "Mingus Ah Um", "AlbumArtist": "Charles Mingus"},
			},
			want: 0,
		},
		{
			name: "unparseable Date leaves the year unset",
			songs: []mpd.Attrs{
				{"file": "USB/A/Alb/01.flac", "Album": "Alb", "AlbumArtist": "A", "Date": "Unknown"},
			},
			want: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			albums, _ := groupAlbumDetails(tc.songs)
			if len(albums) != 1 {
				t.Fatalf("expected exactly 1 album, got %d", len(albums))
			}
			if albums[0].Year != tc.want {
				t.Errorf("Year = %d, want %d", albums[0].Year, tc.want)
			}
		})
	}
}

// TestGroupAlbumDetails_LastModified pins the newest-mtime rule. MPD 0.23 has
// no `added` tag (that is 0.24+), so the file mtime MPD reports as
// Last-Modified is the only "when did this arrive" signal available. Newest
// rather than oldest, because a partially re-ripped or repaired album has
// arrived as late as its newest file.
func TestGroupAlbumDetails_LastModified(t *testing.T) {
	tests := []struct {
		name  string
		songs []mpd.Attrs
		want  time.Time
	}{
		{
			name: "newest mtime wins regardless of song order",
			songs: []mpd.Attrs{
				{"file": "USB/A/Alb/01.flac", "Album": "Alb", "AlbumArtist": "A", "Last-Modified": "2026-08-19T12:00:00Z"},
				{"file": "USB/A/Alb/02.flac", "Album": "Alb", "AlbumArtist": "A", "Last-Modified": "2012-08-01T09:30:00Z"},
			},
			want: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
		},
		{
			name: "newest arrives last",
			songs: []mpd.Attrs{
				{"file": "USB/A/Alb/01.flac", "Album": "Alb", "AlbumArtist": "A", "Last-Modified": "2012-08-01T09:30:00Z"},
				{"file": "USB/A/Alb/02.flac", "Album": "Alb", "AlbumArtist": "A", "Last-Modified": "2026-08-19T12:00:00Z"},
			},
			want: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
		},
		{
			name: "absent Last-Modified leaves the zero time",
			songs: []mpd.Attrs{
				{"file": "USB/A/Alb/01.flac", "Album": "Alb", "AlbumArtist": "A"},
			},
			want: time.Time{},
		},
		{
			name: "unparseable Last-Modified leaves the zero time",
			songs: []mpd.Attrs{
				{"file": "USB/A/Alb/01.flac", "Album": "Alb", "AlbumArtist": "A", "Last-Modified": "yesterday"},
			},
			want: time.Time{},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			albums, _ := groupAlbumDetails(tc.songs)
			if len(albums) != 1 {
				t.Fatalf("expected exactly 1 album, got %d", len(albums))
			}
			if !albums[0].LastModified.Equal(tc.want) {
				t.Errorf("LastModified = %v, want %v", albums[0].LastModified, tc.want)
			}
		})
	}
}
