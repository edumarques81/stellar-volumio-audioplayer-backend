package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/upnp"
)

// fakeLibrarySvc implements socketio.LibraryService over canned pages.
type fakeLibrarySvc struct {
	albumPages      [][]library.Album
	artistPages     [][]library.Artist
	artistAlbums    map[string][]library.Album
	looseTracks     map[string][]library.Track
	albumTracks     library.AlbumTracksResponse
	lastTracksReq   library.GetAlbumTracksRequest
	albumPageCalls  int
	artistPageCalls int
}

// page returns the requested 1-based page plus whether more follow.
func page[T any](pages [][]T, n int) ([]T, bool) {
	if n < 1 || n > len(pages) {
		return nil, false
	}
	return pages[n-1], n < len(pages)
}

func (f *fakeLibrarySvc) GetAlbums(req library.GetAlbumsRequest) library.AlbumsResponse {
	f.albumPageCalls++
	items, more := page(f.albumPages, req.Page)
	return library.AlbumsResponse{Albums: items, Pagination: library.Pagination{HasMore: more}}
}

func (f *fakeLibrarySvc) GetArtists(req library.GetArtistsRequest) library.ArtistsResponse {
	f.artistPageCalls++
	items, more := page(f.artistPages, req.Page)
	return library.ArtistsResponse{Artists: items, Pagination: library.Pagination{HasMore: more}}
}

func (f *fakeLibrarySvc) GetArtistAlbums(req library.GetArtistAlbumsRequest) library.ArtistAlbumsResponse {
	return library.ArtistAlbumsResponse{
		Artist:      req.Artist,
		Albums:      f.artistAlbums[req.Artist],
		LooseTracks: f.looseTracks[req.Artist],
	}
}

func (f *fakeLibrarySvc) GetAlbumTracks(req library.GetAlbumTracksRequest) library.AlbumTracksResponse {
	f.lastTracksReq = req
	return f.albumTracks
}

func (f *fakeLibrarySvc) GetRadioStations(library.GetRadioRequest) library.RadioResponse {
	return library.RadioResponse{}
}

func TestUPnPLibraryAlbumsFollowsPagination(t *testing.T) {
	// The library service pages at MaxLimit, so one request is not enough for
	// a collection larger than a page.
	svc := &fakeLibrarySvc{albumPages: [][]library.Album{
		{{Title: "A", Artist: "x", URI: "u/a", TrackCount: 3, Year: 1999, Genre: "Jazz", AlbumArt: "/albumart?path=u%2Fa%2F01.flac"}},
		{{Title: "B", Artist: "y", URI: "u/b"}},
		{{Title: "C", Artist: "z", URI: "u/c"}},
	}}
	u := &upnpLibrary{lib: svc}

	albums, err := u.Albums()
	if err != nil {
		t.Fatalf("Albums() error: %v", err)
	}
	if len(albums) != 3 {
		t.Fatalf("got %d albums, want 3 (pagination not followed)", len(albums))
	}
	if svc.albumPageCalls != 3 {
		t.Errorf("made %d page requests, want 3", svc.albumPageCalls)
	}

	want := upnp.ServerAlbum{
		Title: "A", AlbumArtist: "x", URI: "u/a", TrackCount: 3,
		Year: 1999, Genre: "Jazz", ArtPath: "/albumart?path=u%2Fa%2F01.flac",
	}
	if albums[0] != want {
		t.Errorf("album conversion:\n got %+v\nwant %+v", albums[0], want)
	}
}

func TestUPnPLibraryUsesAlbumArtistNotTrackArtist(t *testing.T) {
	// library.Album.Artist is the *album* artist (groupAlbumDetails keys on
	// AlbumArtist). Mapping it anywhere but AlbumArtist would split a release
	// whose per-track credits carry soloists.
	svc := &fakeLibrarySvc{albumPages: [][]library.Album{
		{{Title: "Kind of Blue", Artist: "Miles Davis", URI: "u/kob"}},
	}}
	albums, _ := (&upnpLibrary{lib: svc}).Albums()
	if albums[0].AlbumArtist != "Miles Davis" {
		t.Errorf("AlbumArtist = %q", albums[0].AlbumArtist)
	}
}

func TestUPnPLibraryArtistsFollowsPagination(t *testing.T) {
	svc := &fakeLibrarySvc{artistPages: [][]library.Artist{
		{{Name: "Miles Davis", AlbumCount: 4, AlbumArt: "/albumart?path=x"}},
		{{Name: "Rachmaninoff", AlbumCount: 2}},
	}}
	artists, err := (&upnpLibrary{lib: svc}).Artists()
	if err != nil {
		t.Fatalf("Artists() error: %v", err)
	}
	if len(artists) != 2 {
		t.Fatalf("got %d artists, want 2", len(artists))
	}
	if artists[0] != (upnp.ServerArtist{Name: "Miles Davis", AlbumCount: 4, ArtPath: "/albumart?path=x"}) {
		t.Errorf("artist conversion: %+v", artists[0])
	}
}

func TestUPnPLibraryPagingIsBounded(t *testing.T) {
	// A service that keeps reporting HasMore — a cache mid-rebuild has done
	// this — must not spin a Browse forever.
	svc := &alwaysMoreSvc{}
	albums, err := (&upnpLibrary{lib: svc}).Albums()
	if err != nil {
		t.Fatalf("Albums() error: %v", err)
	}
	if len(albums) != maxLibraryPages {
		t.Errorf("got %d albums, want the page cap %d", len(albums), maxLibraryPages)
	}
}

// alwaysMoreSvc never stops reporting HasMore.
type alwaysMoreSvc struct{ fakeLibrarySvc }

func (a *alwaysMoreSvc) GetAlbums(req library.GetAlbumsRequest) library.AlbumsResponse {
	return library.AlbumsResponse{
		Albums:     []library.Album{{Title: fmt.Sprintf("Album %d", req.Page)}},
		Pagination: library.Pagination{HasMore: true},
	}
}

func TestUPnPLibraryAlbumsByArtist(t *testing.T) {
	svc := &fakeLibrarySvc{artistAlbums: map[string][]library.Album{
		"Miles Davis": {{Title: "Kind of Blue", Artist: "Miles Davis", URI: "u/kob"}},
	}}
	albums, err := (&upnpLibrary{lib: svc}).AlbumsByArtist("Miles Davis")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(albums) != 1 || albums[0].Title != "Kind of Blue" {
		t.Fatalf("got %+v", albums)
	}
}

func TestUPnPLibrarySurfacesLooseTracksAsASyntheticAlbum(t *testing.T) {
	// An artist MPD groups under no album surfaces as LooseTracks (the
	// ARTIST-04 fallback). Those files are real and playable; dropping them
	// would make the artist look empty in a DLNA client while the kiosk
	// shows the tracks.
	svc := &fakeLibrarySvc{looseTracks: map[string][]library.Track{
		"Unknown Soloist": {
			{Title: "t1", URI: "u/t1.flac", AlbumArt: "/albumart?path=u%2Ft1.flac"},
			{Title: "t2", URI: "u/t2.flac"},
		},
	}}
	albums, err := (&upnpLibrary{lib: svc}).AlbumsByArtist("Unknown Soloist")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("got %d albums, want 1 synthetic album", len(albums))
	}
	if albums[0].Title != "Unknown Soloist" || albums[0].TrackCount != 2 {
		t.Errorf("synthetic album = %+v", albums[0])
	}
	if albums[0].ArtPath != "/albumart?path=u%2Ft1.flac" {
		t.Errorf("synthetic album art = %q", albums[0].ArtPath)
	}
}

func TestUPnPLibraryIgnoresLooseTracksWhenAlbumsExist(t *testing.T) {
	svc := &fakeLibrarySvc{
		artistAlbums: map[string][]library.Album{"A": {{Title: "Real Album"}}},
		looseTracks:  map[string][]library.Track{"A": {{Title: "t1", URI: "u/t1.flac"}}},
	}
	albums, _ := (&upnpLibrary{lib: svc}).AlbumsByArtist("A")
	if len(albums) != 1 || albums[0].Title != "Real Album" {
		t.Errorf("got %+v, want only the real album", albums)
	}
}

func TestUPnPLibraryAlbumTracks(t *testing.T) {
	dir := t.TempDir()
	rel := filepath.Join("USB", "Kind of Blue", "01 So What.flac")
	if err := os.MkdirAll(filepath.Join(dir, "USB", "Kind of Blue"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 4321)
	if err := os.WriteFile(filepath.Join(dir, rel), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	svc := &fakeLibrarySvc{albumTracks: library.AlbumTracksResponse{
		Album:       "Kind of Blue",
		AlbumArtist: "Miles Davis",
		Tracks: []library.Track{
			{
				Title: "So What", Artist: "Miles Davis", Album: "Kind of Blue",
				URI: "USB/Kind of Blue/01 So What.flac", TrackNumber: 1, Duration: 545,
				AlbumArt: "/albumart?path=x",
			},
			{Title: "Missing", URI: "USB/Kind of Blue/99 gone.flac", TrackNumber: 99},
		},
	}}
	u := &upnpLibrary{lib: svc, musicDir: dir}

	ref := upnp.AlbumRef{Title: "Kind of Blue", AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue"}
	tracks, err := u.AlbumTracks(ref)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// The whole ref must reach the library: URI is what scopes the listing to
	// one quality version of a release.
	want := library.GetAlbumTracksRequest{Album: "Kind of Blue", AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue"}
	if svc.lastTracksReq != want {
		t.Errorf("request = %+v, want %+v", svc.lastTracksReq, want)
	}

	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}
	got := tracks[0]
	if got.Title != "So What" || got.TrackNumber != 1 || got.DurationSec != 545 {
		t.Errorf("track conversion: %+v", got)
	}
	// library.Track carries no album artist; it comes from the response.
	if got.AlbumArtist != "Miles Davis" {
		t.Errorf("AlbumArtist = %q, want the response's", got.AlbumArtist)
	}
	if got.SizeBytes != int64(len(payload)) {
		t.Errorf("SizeBytes = %d, want %d", got.SizeBytes, len(payload))
	}

	// A file that cannot be stat'ed costs the client its progress bar, never
	// playback, so it must still be published with size 0.
	if tracks[1].SizeBytes != 0 {
		t.Errorf("missing file SizeBytes = %d, want 0", tracks[1].SizeBytes)
	}
}

func TestUPnPLibraryFileSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.flac"), []byte("1234567890"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		musicDir string
		relPath  string
		want     int64
	}{
		{"existing file", dir, "a.flac", 10},
		{"missing file", dir, "nope.flac", 0},
		{"a directory is not a size", dir, "subdir", 0},
		{"no music dir configured", "", "a.flac", 0},
		{"empty path", dir, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &upnpLibrary{musicDir: tt.musicDir}
			if got := u.fileSize(tt.relPath); got != tt.want {
				t.Errorf("fileSize(%q) = %d, want %d", tt.relPath, got, tt.want)
			}
		})
	}
}

// Compile-time proof the adapter satisfies the interface the UPnP service wants.
var _ upnp.LibraryProvider = (*upnpLibrary)(nil)
