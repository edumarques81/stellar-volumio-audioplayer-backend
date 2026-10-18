package main

import (
	"os"
	"path/filepath"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/upnp"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/transport/socketio"
)

// upnpLibrary adapts the cached library service to upnp.LibraryProvider so the
// UPnP ContentDirectory serves exactly the same albums, artists and tracks the
// kiosk and the iOS remote see.
type upnpLibrary struct {
	lib socketio.LibraryService
	// musicDir is MPD's music root, used only to stat file sizes for the
	// DIDL res@size attribute.
	musicDir string
}

// maxLibraryPages bounds the paging loops below.
//
// The library service pages at library.MaxLimit (200) and the collection is
// four figures of tracks at most, so 50 pages is ~10,000 objects — far past
// anything real. The cap exists so a service that keeps reporting HasMore
// (a cache mid-rebuild has done this) cannot spin a Browse forever.
const maxLibraryPages = 50

// Albums returns every album in the library.
func (u *upnpLibrary) Albums() ([]upnp.ServerAlbum, error) {
	var out []upnp.ServerAlbum
	for page := 1; page <= maxLibraryPages; page++ {
		resp := u.lib.GetAlbums(library.GetAlbumsRequest{
			Scope: library.ScopeAll,
			Sort:  library.SortAlphabetical,
			Page:  page,
			Limit: library.MaxLimit,
		})
		for _, a := range resp.Albums {
			out = append(out, serverAlbum(a))
		}
		if !resp.Pagination.HasMore {
			break
		}
	}
	return out, nil
}

// Artists returns every artist in the library.
func (u *upnpLibrary) Artists() ([]upnp.ServerArtist, error) {
	var out []upnp.ServerArtist
	for page := 1; page <= maxLibraryPages; page++ {
		resp := u.lib.GetArtists(library.GetArtistsRequest{
			Page:  page,
			Limit: library.MaxLimit,
		})
		for _, a := range resp.Artists {
			out = append(out, upnp.ServerArtist{
				Name:       a.Name,
				AlbumCount: a.AlbumCount,
				ArtPath:    a.AlbumArt,
			})
		}
		if !resp.Pagination.HasMore {
			break
		}
	}
	return out, nil
}

// AlbumsByArtist returns the albums credited to one artist.
func (u *upnpLibrary) AlbumsByArtist(name string) ([]upnp.ServerAlbum, error) {
	var out []upnp.ServerAlbum
	for page := 1; page <= maxLibraryPages; page++ {
		resp := u.lib.GetArtistAlbums(library.GetArtistAlbumsRequest{
			Artist: name,
			Sort:   library.SortYear,
			Page:   page,
			Limit:  library.MaxLimit,
		})
		for _, a := range resp.Albums {
			out = append(out, serverAlbum(a))
		}
		// An artist whose tracks MPD groups under no album at all surfaces
		// as LooseTracks instead (the ARTIST-04 fallback). Those are real,
		// playable files; dropping them here would make the artist look
		// empty in a DLNA client while the kiosk shows the tracks.
		if page == 1 && len(resp.Albums) == 0 && len(resp.LooseTracks) > 0 {
			out = append(out, upnp.ServerAlbum{
				Title:       name,
				AlbumArtist: name,
				TrackCount:  len(resp.LooseTracks),
				ArtPath:     resp.LooseTracks[0].AlbumArt,
			})
		}
		if !resp.Pagination.HasMore {
			break
		}
	}
	return out, nil
}

// AlbumTracks returns the tracks of one album, in playback order.
func (u *upnpLibrary) AlbumTracks(ref upnp.AlbumRef) ([]upnp.ServerTrack, error) {
	resp := u.lib.GetAlbumTracks(library.GetAlbumTracksRequest{
		Album:       ref.Title,
		AlbumArtist: ref.AlbumArtist,
		URI:         ref.URI,
	})

	out := make([]upnp.ServerTrack, 0, len(resp.Tracks))
	for _, t := range resp.Tracks {
		out = append(out, upnp.ServerTrack{
			Title:       t.Title,
			Artist:      t.Artist,
			Album:       t.Album,
			AlbumArtist: resp.AlbumArtist,
			URI:         t.URI,
			TrackNumber: t.TrackNumber,
			DurationSec: t.Duration,
			SizeBytes:   u.fileSize(t.URI),
			ArtPath:     t.AlbumArt,
		})
	}
	return out, nil
}

// fileSize stats a library-relative track path, returning 0 when it cannot be
// read. A missing size costs a DLNA client its progress bar, never playback,
// so this never fails a Browse.
func (u *upnpLibrary) fileSize(relPath string) int64 {
	if u.musicDir == "" || relPath == "" {
		return 0
	}
	info, err := os.Stat(filepath.Join(u.musicDir, relPath))
	if err != nil || info.IsDir() {
		return 0
	}
	return info.Size()
}

// serverAlbum converts a library album to its ContentDirectory form.
func serverAlbum(a library.Album) upnp.ServerAlbum {
	return upnp.ServerAlbum{
		Title:       a.Title,
		AlbumArtist: a.Artist,
		URI:         a.URI,
		TrackCount:  a.TrackCount,
		Year:        a.Year,
		Genre:       a.Genre,
		ArtPath:     a.AlbumArt,
	}
}
