package upnp

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/filestream"
	"github.com/rs/zerolog/log"
)

const contentDirectoryType = "urn:schemas-upnp-org:service:ContentDirectory:1"

// maxBrowseCount caps how many objects one Browse response may carry.
//
// A client is allowed to ask for everything by sending RequestedCount=0, and
// this library has a four-figure track count. Returning all of it in one SOAP
// body makes a multi-megabyte XML document that several clients truncate
// silently. Capping and reporting the honest TotalMatches makes the client
// page, which is the protocol's intent.
const maxBrowseCount = 500

// ContentDirectory implements the UPnP ContentDirectory:1 service over the
// music library, exposing it to DLNA control points (VLC, Kodi, BubbleUPnP,
// mconnect) as a browsable MediaServer.
//
// Every <res> URL it publishes points at the backend's /stream endpoint rather
// than at anything MPD owns. That is deliberate: serving file bytes keeps the
// DAC's single ALSA consumer intact, whereas mirroring MPD's output would mean
// adding a second audio_output, which this appliance forbids.
type ContentDirectory struct {
	cfg MediaServerConfig
	lib LibraryProvider
}

// NewContentDirectory creates a ContentDirectory backed by the given library.
func NewContentDirectory(cfg MediaServerConfig, lib LibraryProvider) *ContentDirectory {
	return &ContentDirectory{cfg: cfg, lib: lib}
}

// RegisterHandlers registers the ContentDirectory control and event endpoints.
func (cd *ContentDirectory) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/upnp/control/ContentDirectory", cd.handleControl)
	mux.HandleFunc("/upnp/event/ContentDirectory", func(w http.ResponseWriter, r *http.Request) {
		// Eventing stub, mirroring the renderer's: ContentDirectory's only
		// evented variables are SystemUpdateID and ContainerUpdateIDs, and a
		// library that only changes on ingest does not need to push them.
		w.Header().Set("SID", "uuid:"+cd.cfg.UDN)
		w.Header().Set("TIMEOUT", "Second-300")
		w.WriteHeader(http.StatusOK)
	})
}

// handleControl dispatches ContentDirectory SOAP actions.
func (cd *ContentDirectory) handleControl(w http.ResponseWriter, r *http.Request) {
	action := extractSOAPAction(r)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	log.Debug().Str("action", action).Msg("ContentDirectory SOAP request")

	var resp string
	switch action {
	case "Browse":
		resp, err = cd.browse(string(body))
	case "Search":
		resp, err = cd.search(string(body))
	case "GetSearchCapabilities":
		resp = soapWrap("GetSearchCapabilitiesResponse", contentDirectoryType,
			"<SearchCaps>dc:title,upnp:artist,upnp:album</SearchCaps>")
	case "GetSortCapabilities":
		// Results come back in the library's own order (which is already
		// track-number order within an album). Advertising sort capabilities
		// we do not implement makes clients present a sort UI that does
		// nothing, so the honest answer is none.
		resp = soapWrap("GetSortCapabilitiesResponse", contentDirectoryType, "<SortCaps></SortCaps>")
	case "GetSystemUpdateID":
		resp = soapWrap("GetSystemUpdateIDResponse", contentDirectoryType, "<Id>1</Id>")
	default:
		log.Warn().Str("action", action).Msg("unknown ContentDirectory action")
		resp = soapWrap(action+"Response", contentDirectoryType, "")
	}

	if err != nil {
		log.Error().Err(err).Str("action", action).Msg("ContentDirectory action failed")
		writeSOAPFault(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	if _, werr := w.Write([]byte(resp)); werr != nil {
		log.Debug().Err(werr).Msg("ContentDirectory: response write failed")
	}
}

// browse implements the Browse action for both BrowseMetadata and
// BrowseDirectChildren.
func (cd *ContentDirectory) browse(body string) (string, error) {
	objectID := strings.TrimSpace(extractXMLElement(body, "ObjectID"))
	if objectID == "" {
		objectID = ObjectIDRoot
	}
	flag := strings.TrimSpace(extractXMLElement(body, "BrowseFlag"))
	start := atoi(extractXMLElement(body, "StartingIndex"))
	count := atoi(extractXMLElement(body, "RequestedCount"))

	if flag == "BrowseMetadata" {
		return cd.browseMetadata(objectID)
	}
	return cd.browseChildren(objectID, start, count)
}

// browseMetadata returns the single object named by objectID.
//
// Clients routinely BrowseMetadata on an object before browsing into it, and
// one that gets an empty result treats the object as gone. Containers answer
// from the object ID alone — it already carries the title — so this never
// needs a library query to describe a container.
func (cd *ContentDirectory) browseMetadata(objectID string) (string, error) {
	b := newDIDLBuilder()

	switch {
	case objectID == ObjectIDRoot:
		b.AddContainer(ObjectIDRoot, "-1", cd.cfg.FriendlyName, "object.container.storageFolder", 2, "")
	case objectID == ObjectIDAlbums:
		b.AddContainer(ObjectIDAlbums, ObjectIDRoot, "Albums", "object.container.storageFolder", 0, "")
	case objectID == ObjectIDArtists:
		b.AddContainer(ObjectIDArtists, ObjectIDRoot, "Artists", "object.container.storageFolder", 0, "")
	case strings.HasPrefix(objectID, prefixAlbum):
		ref, err := DecodeAlbumID(objectID)
		if err != nil {
			return "", err
		}
		b.AddAlbumContainer(ServerAlbum{
			Title:       ref.Title,
			AlbumArtist: ref.AlbumArtist,
			URI:         ref.URI,
		}, ObjectIDAlbums, "")
	case strings.HasPrefix(objectID, prefixArtist):
		name, err := DecodeArtistID(objectID)
		if err != nil {
			return "", err
		}
		b.AddContainer(objectID, ObjectIDArtists, name, "object.container.person.musicArtist", 0, "")
	case strings.HasPrefix(objectID, prefixTrack):
		uri, err := DecodeTrackID(objectID)
		if err != nil {
			return "", err
		}
		if filestream.ContentType(uri) == "" {
			return "", ErrUnknownObjectID
		}
		cd.addTrack(b, ServerTrack{Title: baseName(uri), URI: uri}, ObjectIDRoot)
	default:
		return "", ErrUnknownObjectID
	}

	return cd.browseResponse("BrowseResponse", b, b.Count()), nil
}

// browseChildren returns the direct children of a container.
func (cd *ContentDirectory) browseChildren(objectID string, start, count int) (string, error) {
	switch {
	case objectID == ObjectIDRoot:
		b := newDIDLBuilder()
		b.AddContainer(ObjectIDAlbums, ObjectIDRoot, "Albums", "object.container.storageFolder", 0, "")
		b.AddContainer(ObjectIDArtists, ObjectIDRoot, "Artists", "object.container.storageFolder", 0, "")
		return cd.browseResponse("BrowseResponse", b, b.Count()), nil

	case objectID == ObjectIDAlbums:
		albums, err := cd.lib.Albums()
		if err != nil {
			return "", err
		}
		return cd.albumsResponse("BrowseResponse", albums, ObjectIDAlbums, start, count), nil

	case objectID == ObjectIDArtists:
		artists, err := cd.lib.Artists()
		if err != nil {
			return "", err
		}
		page, total := paginate(len(artists), start, count)
		b := newDIDLBuilder()
		for _, a := range artists[page.from:page.to] {
			b.AddContainer(EncodeArtistID(a.Name), ObjectIDArtists, a.Name,
				"object.container.person.musicArtist", a.AlbumCount,
				artURLFor(cd.cfg.BaseURL, a.ArtPath))
		}
		return cd.browseResponse("BrowseResponse", b, total), nil

	case strings.HasPrefix(objectID, prefixArtist):
		name, err := DecodeArtistID(objectID)
		if err != nil {
			return "", err
		}
		albums, err := cd.lib.AlbumsByArtist(name)
		if err != nil {
			return "", err
		}
		return cd.albumsResponse("BrowseResponse", albums, objectID, start, count), nil

	case strings.HasPrefix(objectID, prefixAlbum):
		ref, err := DecodeAlbumID(objectID)
		if err != nil {
			return "", err
		}
		tracks, err := cd.lib.AlbumTracks(ref)
		if err != nil {
			return "", err
		}
		tracks = playableTracks(tracks)
		page, total := paginate(len(tracks), start, count)
		b := newDIDLBuilder()
		for _, t := range tracks[page.from:page.to] {
			cd.addTrack(b, t, objectID)
		}
		return cd.browseResponse("BrowseResponse", b, total), nil

	case strings.HasPrefix(objectID, prefixTrack):
		// A track is a leaf; browsing its children is legal and returns none.
		b := newDIDLBuilder()
		return cd.browseResponse("BrowseResponse", b, 0), nil
	}

	return "", ErrUnknownObjectID
}

// search implements the Search action.
//
// The full SearchCriteria grammar is a small expression language. Rather than
// pretend to implement it, this extracts the quoted operands of any
// contains/= comparison and matches them against album, artist and track
// titles — which is what every client's search box actually sends. A criteria
// string of "*" means "everything", per the spec.
func (cd *ContentDirectory) search(body string) (string, error) {
	containerID := strings.TrimSpace(extractXMLElement(body, "ContainerID"))
	criteria := decodeXMLEntities(extractXMLElement(body, "SearchCriteria"))
	start := atoi(extractXMLElement(body, "StartingIndex"))
	count := atoi(extractXMLElement(body, "RequestedCount"))

	terms := searchTerms(criteria)
	wantsAll := strings.TrimSpace(criteria) == "*" || len(terms) == 0

	albums, err := cd.lib.Albums()
	if err != nil {
		return "", err
	}

	// Searching inside one artist's container narrows the candidate set the
	// way the client expects; anywhere else searches the whole library.
	if strings.HasPrefix(containerID, prefixArtist) {
		if name, derr := DecodeArtistID(containerID); derr == nil {
			if scoped, serr := cd.lib.AlbumsByArtist(name); serr == nil {
				albums = scoped
			}
		}
	}

	var matched []ServerAlbum
	for _, a := range albums {
		if wantsAll || matchesAny(terms, a.Title, a.AlbumArtist, a.Genre) {
			matched = append(matched, a)
		}
	}

	return cd.albumsResponse("SearchResponse", matched, ObjectIDAlbums, start, count), nil
}

// playableTracks drops tracks the /stream endpoint will not serve.
//
// The ContentDirectory and the file server share one media-type allowlist, so
// an extension missing from it means /stream would answer 404. Publishing such
// an item anyway gives the client a URL that fails at play time and no way to
// tell why; omitting it is the honest answer. In practice this filters nothing
// — the allowlist covers every format in the library — and exists so an
// unexpected file in an album folder cannot poison the whole listing.
func playableTracks(tracks []ServerTrack) []ServerTrack {
	out := make([]ServerTrack, 0, len(tracks))
	for _, t := range tracks {
		if filestream.ContentType(t.URI) == "" {
			log.Debug().Str("uri", t.URI).Msg("ContentDirectory: skipping track with unservable media type")
			continue
		}
		out = append(out, t)
	}
	return out
}

// addTrack writes one track item, resolving its MIME type from the extension
// using the same table the /stream endpoint serves from, so a client is never
// told a type the server will not deliver.
func (cd *ContentDirectory) addTrack(b *didlBuilder, t ServerTrack, parentID string) {
	b.AddTrackItem(t, parentID,
		streamURLFor(cd.cfg.BaseURL, t.URI),
		artURLFor(cd.cfg.BaseURL, t.ArtPath),
		filestream.ContentType(t.URI))
}

// albumsResponse paginates and renders a list of album containers.
func (cd *ContentDirectory) albumsResponse(action string, albums []ServerAlbum, parentID string, start, count int) string {
	page, total := paginate(len(albums), start, count)
	b := newDIDLBuilder()
	for _, a := range albums[page.from:page.to] {
		b.AddAlbumContainer(a, parentID, artURLFor(cd.cfg.BaseURL, a.ArtPath))
	}
	return cd.browseResponse(action, b, total)
}

// browseResponse wraps a DIDL document in the SOAP response shape shared by
// Browse and Search. The DIDL XML is entity-escaped because it travels as the
// *text content* of <Result>, not as nested markup.
func (cd *ContentDirectory) browseResponse(action string, b *didlBuilder, totalMatches int) string {
	return soapWrap(action, contentDirectoryType,
		fmt.Sprintf("<Result>%s</Result>"+
			"<NumberReturned>%d</NumberReturned>"+
			"<TotalMatches>%d</TotalMatches>"+
			"<UpdateID>1</UpdateID>",
			xmlEscape(b.String()), b.Count(), totalMatches))
}

// pageRange is a half-open slice range into a result set.
type pageRange struct {
	from int
	to   int
}

// paginate clamps a client's StartingIndex/RequestedCount against a result set
// of n objects and returns the slice range plus the true total.
//
// RequestedCount=0 means "as many as you can" per the spec, not "none" — a
// literal reading returns an empty list and the client shows an empty library.
func paginate(n, start, count int) (pageRange, int) {
	if start < 0 {
		start = 0
	}
	if start > n {
		start = n
	}
	if count <= 0 || count > maxBrowseCount {
		count = maxBrowseCount
	}
	end := start + count
	if end > n {
		end = n
	}
	return pageRange{from: start, to: end}, n
}

// searchTerms pulls the quoted operands out of a SearchCriteria expression.
func searchTerms(criteria string) []string {
	var terms []string
	rest := criteria
	for {
		open := strings.Index(rest, `"`)
		if open < 0 {
			break
		}
		rest = rest[open+1:]
		shut := strings.Index(rest, `"`)
		if shut < 0 {
			break
		}
		if term := strings.TrimSpace(rest[:shut]); term != "" {
			terms = append(terms, strings.ToLower(term))
		}
		rest = rest[shut+1:]
	}
	return terms
}

// matchesAny reports whether any term appears in any of the given fields.
func matchesAny(terms []string, fields ...string) bool {
	for _, f := range fields {
		lf := strings.ToLower(f)
		for _, t := range terms {
			if t != "" && strings.Contains(lf, t) {
				return true
			}
		}
	}
	return false
}

// baseName returns the final path segment of a library-relative URI.
func baseName(uri string) string {
	if idx := strings.LastIndex(uri, "/"); idx >= 0 {
		return uri[idx+1:]
	}
	return uri
}

// writeSOAPFault returns a UPnP SOAP fault. Control points parse the fault body
// and show the error; a bare HTTP 500 makes them report a connection problem
// instead, which sends the user looking in the wrong place.
func writeSOAPFault(w http.ResponseWriter, err error) {
	code := "701" // No such object
	desc := "No such object"
	if err != ErrUnknownObjectID {
		code = "501" // Action failed
		desc = err.Error()
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	fault := `<?xml version="1.0"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" ` +
		`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
		`<s:Body><s:Fault>` +
		`<faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring>` +
		`<detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0">` +
		`<errorCode>` + code + `</errorCode>` +
		`<errorDescription>` + xmlEscape(desc) + `</errorDescription>` +
		`</UPnPError></detail>` +
		`</s:Fault></s:Body></s:Envelope>`
	if _, werr := w.Write([]byte(fault)); werr != nil {
		log.Debug().Err(werr).Msg("ContentDirectory: fault write failed")
	}
}
