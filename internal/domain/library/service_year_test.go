package library

import (
	"testing"
	"time"
)

// TestService_GetAlbums_YearAndAddedAt covers the MPD-direct browse path end
// to end: sortAlbums' SortYear / SortRecentlyAdded cases were always correct,
// but nothing ever populated the fields they sort on, so both orderings were
// arbitrary. These tests fail on the pre-fix code.
func TestService_GetAlbums_YearAndAddedAt(t *testing.T) {
	older := time.Date(2012, 8, 1, 9, 30, 0, 0, time.UTC)
	newer := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {},
			"NAS":      {},
			"USB": {
				{
					Album: "Newer Album", AlbumArtist: "Artist1", TrackCount: 10,
					FirstTrack: "USB/Newer/track.flac", Year: 2024, LastModified: newer,
				},
				{
					Album: "Older Album", AlbumArtist: "Artist2", TrackCount: 8,
					FirstTrack: "USB/Older/track.flac", Year: 1959, LastModified: older,
				},
				{
					Album: "Untagged Album", AlbumArtist: "Artist3", TrackCount: 6,
					FirstTrack: "USB/Untagged/track.flac",
				},
			},
		},
	}
	service := NewService(mockMPD, &MockPathClassifier{})

	t.Run("year and addedAt reach the Album", func(t *testing.T) {
		resp := service.GetAlbums(GetAlbumsRequest{Scope: ScopeUSB, Sort: SortAlphabetical})
		byTitle := map[string]Album{}
		for _, a := range resp.Albums {
			byTitle[a.Title] = a
		}
		if got := byTitle["Newer Album"].Year; got != 2024 {
			t.Errorf("Newer Album Year = %d, want 2024", got)
		}
		if got := byTitle["Newer Album"].AddedAt; !got.Equal(newer) {
			t.Errorf("Newer Album AddedAt = %v, want %v", got, newer)
		}
		if got := byTitle["Untagged Album"].Year; got != 0 {
			t.Errorf("Untagged Album Year = %d, want 0", got)
		}
		if got := byTitle["Untagged Album"].AddedAt; !got.IsZero() {
			t.Errorf("Untagged Album AddedAt = %v, want zero", got)
		}
	})

	t.Run("sort=year orders newest release first", func(t *testing.T) {
		resp := service.GetAlbums(GetAlbumsRequest{Scope: ScopeUSB, Sort: SortYear})
		want := []string{"Newer Album", "Older Album", "Untagged Album"}
		for i, title := range want {
			if resp.Albums[i].Title != title {
				t.Fatalf("sort=year position %d = %q, want %q (full order: %v)",
					i, resp.Albums[i].Title, title, titles(resp.Albums))
			}
		}
	})

	t.Run("sort=recently_added orders newest arrival first", func(t *testing.T) {
		resp := service.GetAlbums(GetAlbumsRequest{Scope: ScopeUSB, Sort: SortRecentlyAdded})
		if resp.Albums[0].Title != "Newer Album" {
			t.Errorf("sort=recently_added first = %q, want %q (full order: %v)",
				resp.Albums[0].Title, "Newer Album", titles(resp.Albums))
		}
		if resp.Albums[1].Title != "Older Album" {
			t.Errorf("sort=recently_added second = %q, want %q (full order: %v)",
				resp.Albums[1].Title, "Older Album", titles(resp.Albums))
		}
	})
}

func titles(albums []Album) []string {
	out := make([]string, len(albums))
	for i, a := range albums {
		out[i] = a.Title
	}
	return out
}
