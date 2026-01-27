package socketio

import (
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library"
	mpdclient "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
)

// LibraryMPDAdapter adapts the mpd.Client to the library.MPDClient interface.
type LibraryMPDAdapter struct {
	client *mpdclient.Client
}

// NewLibraryMPDAdapter creates a new adapter.
func NewLibraryMPDAdapter(client *mpdclient.Client) *LibraryMPDAdapter {
	return &LibraryMPDAdapter{client: client}
}

// ListAlbums returns all unique albums from the MPD database.
func (a *LibraryMPDAdapter) ListAlbums() ([]library.AlbumInfo, error) {
	albums, err := a.client.ListAlbums()
	if err != nil {
		return nil, err
	}

	result := make([]library.AlbumInfo, len(albums))
	for i, album := range albums {
		result[i] = library.AlbumInfo{
			Album:       album.Album,
			AlbumArtist: album.AlbumArtist,
		}
	}
	return result, nil
}

// ListAlbumsInBase returns albums in a specific base path.
func (a *LibraryMPDAdapter) ListAlbumsInBase(basePath string) ([]library.AlbumInfo, error) {
	albums, err := a.client.ListAlbumsInBase(basePath)
	if err != nil {
		return nil, err
	}

	result := make([]library.AlbumInfo, len(albums))
	for i, album := range albums {
		result[i] = library.AlbumInfo{
			Album:       album.Album,
			AlbumArtist: album.AlbumArtist,
		}
	}
	return result, nil
}

// GetAlbumDetails returns detailed album info for a base path.
func (a *LibraryMPDAdapter) GetAlbumDetails(basePath string) ([]library.AlbumDetails, error) {
	details, err := a.client.GetAlbumDetails(basePath)
	if err != nil {
		return nil, err
	}

	result := make([]library.AlbumDetails, len(details))
	for i, d := range details {
		result[i] = library.AlbumDetails{
			Album:       d.Album,
			AlbumArtist: d.AlbumArtist,
			TrackCount:  d.TrackCount,
			FirstTrack:  d.FirstTrack,
			TotalTime:   d.TotalTime,
		}
	}
	return result, nil
}

// ListArtists returns all unique album artists.
func (a *LibraryMPDAdapter) ListArtists() ([]string, error) {
	return a.client.ListArtists()
}

// FindAlbumsByArtist returns albums by a specific artist.
func (a *LibraryMPDAdapter) FindAlbumsByArtist(artist string) ([]library.AlbumInfo, error) {
	albums, err := a.client.FindAlbumsByArtist(artist)
	if err != nil {
		return nil, err
	}

	result := make([]library.AlbumInfo, len(albums))
	for i, album := range albums {
		result[i] = library.AlbumInfo{
			Album:       album.Album,
			AlbumArtist: album.AlbumArtist,
		}
	}
	return result, nil
}

// FindAlbumTracks returns tracks for a specific album.
func (a *LibraryMPDAdapter) FindAlbumTracks(album, albumArtist string) ([]map[string]string, error) {
	tracks, err := a.client.FindAlbumTracks(album, albumArtist)
	if err != nil {
		return nil, err
	}

	// Convert mpd.Attrs to map[string]string
	result := make([]map[string]string, len(tracks))
	for i, track := range tracks {
		result[i] = make(map[string]string)
		for k, v := range track {
			result[i][k] = v
		}
	}
	return result, nil
}

// ListPlaylists returns all saved playlists.
func (a *LibraryMPDAdapter) ListPlaylists() ([]string, error) {
	return a.client.ListPlaylists()
}

// ListPlaylistInfo returns the contents of a playlist.
func (a *LibraryMPDAdapter) ListPlaylistInfo(name string) ([]map[string]string, error) {
	tracks, err := a.client.ListPlaylistInfo(name)
	if err != nil {
		return nil, err
	}

	// Convert mpd.Attrs to map[string]string
	result := make([]map[string]string, len(tracks))
	for i, track := range tracks {
		result[i] = make(map[string]string)
		for k, v := range track {
			result[i][k] = v
		}
	}
	return result, nil
}
