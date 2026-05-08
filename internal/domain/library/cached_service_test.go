package library

import (
	"path/filepath"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

// TestCachedService_GetAlbums_MapsGenre proves that CachedAlbum.Genre is
// mapped onto the Library Album payload returned to socket clients. The
// Library album-meta strip relies on this for the new "Ambient / Post-Rock"
// segment.
func TestCachedService_GetAlbums_MapsGenre(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	db := cache.NewDB(filepath.Join(tmpDir, "library.db"))
	if err := db.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dao := cache.NewDAO(db)

	withGenre := &cache.CachedAlbum{
		ID:          "id-with-genre",
		Title:       "Midnight Shores",
		AlbumArtist: "Hollow Tides",
		URI:         "NAS/Hollow Tides/Midnight Shores",
		Source:      "nas",
		Genre:       "Ambient / Post-Rock",
	}
	noGenre := &cache.CachedAlbum{
		ID:          "id-no-genre",
		Title:       "Untitled",
		AlbumArtist: "Anonymous",
		URI:         "NAS/Anonymous/Untitled",
		Source:      "nas",
	}
	if err := dao.InsertAlbum(withGenre); err != nil {
		t.Fatalf("insert with genre: %v", err)
	}
	if err := dao.InsertAlbum(noGenre); err != nil {
		t.Fatalf("insert no genre: %v", err)
	}

	svc := NewCachedService(&MockMPDClient{}, &MockPathClassifier{}, db)

	resp := svc.GetAlbums(GetAlbumsRequest{Scope: ScopeAll, Sort: SortAlphabetical})

	got := map[string]string{}
	for _, a := range resp.Albums {
		got[a.ID] = a.Genre
	}
	if got["id-with-genre"] != "Ambient / Post-Rock" {
		t.Errorf("with-genre.Genre = %q, want %q", got["id-with-genre"], "Ambient / Post-Rock")
	}
	if got["id-no-genre"] != "" {
		t.Errorf("no-genre.Genre = %q, want empty string", got["id-no-genre"])
	}
}
