package upnp

// MediaServerConfig configures the UPnP MediaServer (ContentDirectory) half of
// the device.
//
// The MediaServer and the MediaRenderer are two *separate* UPnP root devices
// that happen to be hosted by one process. They must not share a UDN: a
// control point keys its device cache on the UDN, and announcing one UUID as
// both a renderer and a server makes clients pick whichever description they
// saw last and then fail every action belonging to the other role.
type MediaServerConfig struct {
	// FriendlyName is what shows up in VLC, Kodi or BubbleUPnP's server list.
	FriendlyName string
	// UDN is the MediaServer's own stable UUID, distinct from the renderer's.
	UDN string
	// BackendPort is the port the *main backend* listens on — not the UPnP
	// port. The UPnP HTTP server only serves device descriptions and SOAP;
	// media bytes come from the backend's Range-capable /stream endpoint and
	// artwork from /albumart, both of which live on the main port.
	BackendPort int
	// BaseURL is the absolute, client-reachable origin those URLs are built
	// from, e.g. "http://192.168.86.25:3000". Leave it empty and the service
	// fills it in from the detected local IP and BackendPort once Start has
	// resolved the interface address; set it only to override that.
	BaseURL string
}

// ServerAlbum is one album as the ContentDirectory publishes it.
type ServerAlbum struct {
	Title       string
	AlbumArtist string
	// URI is the album's library-relative folder path. It scopes the track
	// listing to one specific quality version of a release.
	URI        string
	TrackCount int
	Year       int
	Genre      string
	// ArtPath is a backend-relative artwork URL such as
	// "/albumart?path=USB/Kind%20of%20Blue/01.flac". The library already
	// builds these (keyed on the album's first track, because /albumart
	// resolves a *track* path, not a folder), so it is carried through
	// verbatim rather than re-derived here.
	ArtPath string
}

// ServerArtist is one artist as the ContentDirectory publishes it.
type ServerArtist struct {
	Name       string
	AlbumCount int
	ArtPath    string
}

// ServerTrack is one playable track as the ContentDirectory publishes it.
type ServerTrack struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	// URI is the track's library-relative path, which is also the path
	// appended to the backend's /stream endpoint.
	URI         string
	TrackNumber int
	DurationSec int
	// SizeBytes is the file size, or 0 when it could not be determined.
	// DLNA clients use it for progress bars and for deciding whether to
	// range-request; omitting it degrades the UI but does not break playback.
	SizeBytes int64
	ArtPath   string
}

// LibraryProvider is the read-only view of the music library that the
// ContentDirectory needs. It is deliberately narrow and expressed in upnp's
// own types so this package stays free of library/cache dependencies and the
// SOAP layer can be tested against a fake.
type LibraryProvider interface {
	// Albums returns every album in the library.
	Albums() ([]ServerAlbum, error)
	// Artists returns every artist in the library.
	Artists() ([]ServerArtist, error)
	// AlbumsByArtist returns the albums credited to one artist.
	AlbumsByArtist(name string) ([]ServerAlbum, error)
	// AlbumTracks returns the tracks of one album, in playback order.
	AlbumTracks(ref AlbumRef) ([]ServerTrack, error)
}
