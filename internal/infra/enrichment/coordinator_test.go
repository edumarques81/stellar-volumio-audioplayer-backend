package enrichment

import (
	"strings"
	"testing"
)

// mockArtistProvider is a minimal ArtistProvider stub used by
// TestCreateArtistSaveFunc_InsertsArtworkRecord. Only UpdateArtistArtworkURL
// is interesting — the others return zero values.
type mockArtistProvider struct {
	updateArtworkURL func(artistID, url, source string) error
}

func (m *mockArtistProvider) GetArtistsWithoutArtwork() ([]Artist, error) { return nil, nil }
func (m *mockArtistProvider) UpdateArtistArtwork(artistID, artworkID string) error {
	return nil
}
func (m *mockArtistProvider) UpdateArtistArtworkURL(artistID, url, source string) error {
	return m.updateArtworkURL(artistID, url, source)
}
func (m *mockArtistProvider) GetFirstAlbumArtwork(artistName string) (string, error) {
	return "", nil
}

// TestCreateArtistSaveFunc_InsertsArtworkRecord asserts that the Fanart.tv
// save path calls UpdateArtistArtworkURL (which inserts a row in the artwork
// table) instead of only updating artists.artwork_id. Without this, the
// /artistart HTTP handler (which queries artwork.artist_id directly) returns
// 404 for every artist even when the .jpg is on disk.
func TestCreateArtistSaveFunc_InsertsArtworkRecord(t *testing.T) {
	var capturedArtistID, capturedURL, capturedSource string
	mock := &mockArtistProvider{
		updateArtworkURL: func(artistID, url, source string) error {
			capturedArtistID, capturedURL, capturedSource = artistID, url, source
			return nil
		},
	}

	coord := &Coordinator{
		artistProvider: mock,
		cacheDir:       t.TempDir(),
	}

	saveFunc := coord.CreateArtistSaveFunc()
	if err := saveFunc("artist-id-123", &FetchResult{
		Data:     []byte("fake jpeg bytes"),
		MimeType: "image/jpeg",
	}); err != nil {
		t.Fatalf("saveFunc: %v", err)
	}

	if capturedArtistID != "artist-id-123" {
		t.Errorf("artistID = %q, want artist-id-123", capturedArtistID)
	}
	if !strings.HasSuffix(capturedURL, "/artwork/artists/artist-id-123.jpg") {
		t.Errorf("URL = %q, want suffix /artwork/artists/artist-id-123.jpg", capturedURL)
	}
	if capturedSource != "fanarttv" {
		t.Errorf("source = %q, want fanarttv", capturedSource)
	}
}
