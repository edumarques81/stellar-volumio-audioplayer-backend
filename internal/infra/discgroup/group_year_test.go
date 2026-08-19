package discgroup

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad fixture time %q: %v", s, err)
	}
	return ts
}

// TestGroupFolders_YearAndLastModified pins how the two sort keys survive
// grouping: an ungrouped folder passes both through untouched, and a merged
// box set takes the first non-zero Year and the NEWEST LastModified across
// its discs.
func TestGroupFolders_YearAndLastModified(t *testing.T) {
	disc1 := mustTime(t, "2024-01-10T08:00:00Z")
	disc2 := mustTime(t, "2024-03-22T19:45:00Z")

	tests := []struct {
		name             string
		folders          []Folder
		wantGroups       int
		wantYear         int
		wantLastModified time.Time
	}{
		{
			name: "ungrouped folder passes Year and LastModified through",
			folders: []Folder{
				{
					Album: "ok", AlbumArtist: "daoud", Directory: "USB/ok",
					Year: 2025, LastModified: disc2,
				},
			},
			wantGroups:       1,
			wantYear:         2025,
			wantLastModified: disc2,
		},
		{
			name: "box set takes disc 1's year and the newest disc mtime",
			folders: []Folder{
				{
					Album: "The Symphonies", AlbumArtist: "Mahler", Disc: "1",
					Directory: "USB/Mahler The Symphonies/CD 01",
					Year:      1991, LastModified: disc1,
				},
				{
					Album: "The Symphonies", AlbumArtist: "Mahler", Disc: "2",
					Directory: "USB/Mahler The Symphonies/CD 02",
					Year:      1991, LastModified: disc2,
				},
			},
			wantGroups:       1,
			wantYear:         1991,
			wantLastModified: disc2,
		},
		{
			name: "box set whose first disc is untagged still gets a year",
			folders: []Folder{
				{
					Album: "The Symphonies", AlbumArtist: "Mahler", Disc: "1",
					Directory: "USB/Mahler The Symphonies/CD 01",
					Year:      0, LastModified: disc1,
				},
				{
					Album: "The Symphonies", AlbumArtist: "Mahler", Disc: "2",
					Directory: "USB/Mahler The Symphonies/CD 02",
					Year:      1991, LastModified: disc1,
				},
			},
			wantGroups:       1,
			wantYear:         1991,
			wantLastModified: disc1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			groups := GroupFolders(tc.folders)
			if len(groups) != tc.wantGroups {
				t.Fatalf("got %d groups, want %d", len(groups), tc.wantGroups)
			}
			if groups[0].Year != tc.wantYear {
				t.Errorf("Year = %d, want %d", groups[0].Year, tc.wantYear)
			}
			if !groups[0].LastModified.Equal(tc.wantLastModified) {
				t.Errorf("LastModified = %v, want %v", groups[0].LastModified, tc.wantLastModified)
			}
		})
	}
}
