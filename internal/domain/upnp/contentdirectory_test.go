package upnp

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- test fakes -------------------------------------------------------------

type fakeLibrary struct {
	albums        []ServerAlbum
	artists       []ServerArtist
	byArtist      map[string][]ServerAlbum
	tracks        map[string][]ServerTrack
	albumsErr     error
	artistsErr    error
	byArtistErr   error
	tracksErr     error
	lastTracksRef AlbumRef
}

func (f *fakeLibrary) Albums() ([]ServerAlbum, error) { return f.albums, f.albumsErr }

func (f *fakeLibrary) Artists() ([]ServerArtist, error) { return f.artists, f.artistsErr }

func (f *fakeLibrary) AlbumsByArtist(name string) ([]ServerAlbum, error) {
	return f.byArtist[name], f.byArtistErr
}

func (f *fakeLibrary) AlbumTracks(ref AlbumRef) ([]ServerTrack, error) {
	f.lastTracksRef = ref
	return f.tracks[ref.Title], f.tracksErr
}

func newTestLibrary() *fakeLibrary {
	kob := ServerAlbum{
		Title: "Kind of Blue", AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue",
		TrackCount: 2, Year: 1959, Genre: "Jazz",
		ArtPath: "/albumart?path=USB%2FKind+of+Blue%2F01.flac",
	}
	etudes := ServerAlbum{
		Title: "Études-tableaux", AlbumArtist: "Rachmaninoff", URI: "USB/Etudes",
		TrackCount: 1,
	}
	return &fakeLibrary{
		albums: []ServerAlbum{kob, etudes},
		artists: []ServerArtist{
			{Name: "Miles Davis", AlbumCount: 1, ArtPath: "/artistart?id=1"},
			{Name: "Rachmaninoff", AlbumCount: 1},
		},
		byArtist: map[string][]ServerAlbum{
			"Miles Davis":  {kob},
			"Rachmaninoff": {etudes},
		},
		tracks: map[string][]ServerTrack{
			"Kind of Blue": {
				{
					Title: "So What", Artist: "Miles Davis", Album: "Kind of Blue",
					AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue/01 So What.flac",
					TrackNumber: 1, DurationSec: 545, SizeBytes: 157_547_848,
					ArtPath: "/albumart?path=USB%2FKind+of+Blue%2F01.flac",
				},
				{
					Title: "Blue in Green", Artist: "Miles Davis", Album: "Kind of Blue",
					AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue/02 Blue in Green.dsf",
					TrackNumber: 2, DurationSec: 337,
				},
			},
			"Études-tableaux": {
				{Title: "No. 1", URI: "USB/Etudes/01.wav", TrackNumber: 1, DurationSec: 240},
			},
		},
	}
}

func newTestCD(lib LibraryProvider) *ContentDirectory {
	return NewContentDirectory(MediaServerConfig{
		FriendlyName: "Stellar Library",
		UDN:          "server-uuid",
		BaseURL:      "http://192.168.86.25:3000",
	}, lib)
}

// --- SOAP plumbing ----------------------------------------------------------

type soapEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Inner    soapBrowseResult `xml:"BrowseResponse"`
		Search   soapBrowseResult `xml:"SearchResponse"`
		Caps     string           `xml:"GetSearchCapabilitiesResponse>SearchCaps"`
		SortCaps string           `xml:"GetSortCapabilitiesResponse>SortCaps"`
		UpdateID string           `xml:"GetSystemUpdateIDResponse>Id"`
		Fault    struct {
			Code string `xml:"detail>UPnPError>errorCode"`
			Desc string `xml:"detail>UPnPError>errorDescription"`
		} `xml:"Fault"`
	} `xml:"Body"`
}

type soapBrowseResult struct {
	Result         string `xml:"Result"`
	NumberReturned int    `xml:"NumberReturned"`
	TotalMatches   int    `xml:"TotalMatches"`
	UpdateID       string `xml:"UpdateID"`
}

// browseBody builds a Browse SOAP request body.
func browseBody(objectID, flag string, start, count int) string {
	return fmt.Sprintf(`<?xml version="1.0"?><s:Envelope><s:Body>`+
		`<u:Browse xmlns:u="%s">`+
		`<ObjectID>%s</ObjectID><BrowseFlag>%s</BrowseFlag><Filter>*</Filter>`+
		`<StartingIndex>%d</StartingIndex><RequestedCount>%d</RequestedCount>`+
		`<SortCriteria></SortCriteria></u:Browse></s:Body></s:Envelope>`,
		contentDirectoryType, xmlEscape(objectID), flag, start, count)
}

func searchBody(containerID, criteria string, start, count int) string {
	return fmt.Sprintf(`<?xml version="1.0"?><s:Envelope><s:Body>`+
		`<u:Search xmlns:u="%s">`+
		`<ContainerID>%s</ContainerID><SearchCriteria>%s</SearchCriteria><Filter>*</Filter>`+
		`<StartingIndex>%d</StartingIndex><RequestedCount>%d</RequestedCount>`+
		`<SortCriteria></SortCriteria></u:Search></s:Body></s:Envelope>`,
		contentDirectoryType, xmlEscape(containerID), xmlEscape(criteria), start, count)
}

// call sends one SOAP action through the registered handler and returns the
// recorder plus the parsed envelope.
func call(t *testing.T, cd *ContentDirectory, action, body string) (*httptest.ResponseRecorder, soapEnvelope) {
	t.Helper()
	mux := http.NewServeMux()
	cd.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodPost, "/upnp/control/ContentDirectory", strings.NewReader(body))
	req.Header.Set("SOAPACTION", `"`+contentDirectoryType+"#"+action+`"`)
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var env soapEnvelope
	if err := xml.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response is not parseable SOAP: %v\n%s", err, rec.Body.String())
	}
	return rec, env
}

// browse runs a Browse and returns the parsed DIDL plus the SOAP counters.
func browse(t *testing.T, cd *ContentDirectory, objectID, flag string, start, count int) (didlDoc, soapBrowseResult) {
	t.Helper()
	rec, env := call(t, cd, "Browse", browseBody(objectID, flag, start, count))
	if rec.Code != http.StatusOK {
		t.Fatalf("Browse(%q) status = %d, body:\n%s", objectID, rec.Code, rec.Body.String())
	}
	return parseDIDLDoc(t, env.Body.Inner.Result), env.Body.Inner
}

// --- tests ------------------------------------------------------------------

func TestBrowseRootListsAlbumsAndArtists(t *testing.T) {
	cd := newTestCD(newTestLibrary())
	doc, res := browse(t, cd, ObjectIDRoot, "BrowseDirectChildren", 0, 0)

	if len(doc.Containers) != 2 || len(doc.Items) != 0 {
		t.Fatalf("root has %d containers / %d items, want 2 / 0", len(doc.Containers), len(doc.Items))
	}
	if doc.Containers[0].ID != ObjectIDAlbums || doc.Containers[1].ID != ObjectIDArtists {
		t.Errorf("root children = %q, %q", doc.Containers[0].ID, doc.Containers[1].ID)
	}
	for _, c := range doc.Containers {
		if c.ParentID != ObjectIDRoot {
			t.Errorf("%s parentID = %q, want %q", c.ID, c.ParentID, ObjectIDRoot)
		}
	}
	if res.NumberReturned != 2 || res.TotalMatches != 2 {
		t.Errorf("counters = %d/%d, want 2/2", res.NumberReturned, res.TotalMatches)
	}
}

func TestBrowseAnEmptyObjectIDFallsBackToRoot(t *testing.T) {
	// Several control points send an empty ObjectID on their first Browse.
	// Treating that as "unknown object" makes the server look broken on
	// connect, so it must mean the root.
	cd := newTestCD(newTestLibrary())
	doc, _ := browse(t, cd, "", "BrowseDirectChildren", 0, 0)
	if len(doc.Containers) != 2 {
		t.Fatalf("empty ObjectID did not browse the root: %+v", doc)
	}
}

func TestBrowseAlbumsContainer(t *testing.T) {
	cd := newTestCD(newTestLibrary())
	doc, res := browse(t, cd, ObjectIDAlbums, "BrowseDirectChildren", 0, 0)

	if len(doc.Containers) != 2 {
		t.Fatalf("got %d album containers, want 2", len(doc.Containers))
	}
	if res.TotalMatches != 2 {
		t.Errorf("TotalMatches = %d, want 2", res.TotalMatches)
	}

	kob := doc.Containers[0]
	if kob.Title != "Kind of Blue" {
		t.Errorf("title = %q", kob.Title)
	}
	if kob.ID != EncodeAlbumID(AlbumRef{Title: "Kind of Blue", AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue"}) {
		t.Errorf("album id does not encode title+artist+uri: %q", kob.ID)
	}
	if kob.ParentID != ObjectIDAlbums {
		t.Errorf("parentID = %q", kob.ParentID)
	}
	if kob.AlbumArt != "http://192.168.86.25:3000/albumart?path=USB%2FKind+of+Blue%2F01.flac" {
		t.Errorf("albumArtURI = %q", kob.AlbumArt)
	}
	// A unicode title must survive the SOAP + DIDL double escaping intact.
	if doc.Containers[1].Title != "Études-tableaux" {
		t.Errorf("unicode title mangled: %q", doc.Containers[1].Title)
	}
}

func TestBrowseAlbumReturnsPlayableTracks(t *testing.T) {
	lib := newTestLibrary()
	cd := newTestCD(lib)
	albumID := EncodeAlbumID(AlbumRef{Title: "Kind of Blue", AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue"})

	doc, res := browse(t, cd, albumID, "BrowseDirectChildren", 0, 0)

	// The decoded ref must reach the library verbatim — the URI is what scopes
	// the listing to one quality version of a release.
	want := AlbumRef{Title: "Kind of Blue", AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue"}
	if lib.lastTracksRef != want {
		t.Errorf("library received %+v, want %+v", lib.lastTracksRef, want)
	}

	if len(doc.Items) != 2 || res.TotalMatches != 2 {
		t.Fatalf("got %d items / TotalMatches %d, want 2 / 2", len(doc.Items), res.TotalMatches)
	}

	first := doc.Items[0]
	if first.ParentID != albumID {
		t.Errorf("item parentID = %q, want the album id", first.ParentID)
	}
	if first.Track != 1 {
		t.Errorf("originalTrackNumber = %d", first.Track)
	}
	if len(first.Res) != 1 {
		t.Fatalf("got %d res elements", len(first.Res))
	}
	wantURL := "http://192.168.86.25:3000/stream/USB/Kind%20of%20Blue/01%20So%20What.flac"
	if first.Res[0].URL != wantURL {
		t.Errorf("res URL\n got %q\nwant %q", first.Res[0].URL, wantURL)
	}
	if !strings.HasPrefix(first.Res[0].ProtocolInfo, "http-get:*:audio/flac:") {
		t.Errorf("flac protocolInfo = %q", first.Res[0].ProtocolInfo)
	}
	if first.Res[0].Size != 157_547_848 {
		t.Errorf("res size = %d", first.Res[0].Size)
	}

	// The media type must come from the extension, not from the album's.
	// DSD is published at full resolution — the ContentDirectory has no
	// format ceiling, unlike the Cast path.
	if !strings.HasPrefix(doc.Items[1].Res[0].ProtocolInfo, "http-get:*:audio/x-dsf:") {
		t.Errorf("dsf protocolInfo = %q", doc.Items[1].Res[0].ProtocolInfo)
	}
}

func TestBrowseArtistsAndArtistAlbums(t *testing.T) {
	cd := newTestCD(newTestLibrary())

	doc, res := browse(t, cd, ObjectIDArtists, "BrowseDirectChildren", 0, 0)
	if len(doc.Containers) != 2 || res.TotalMatches != 2 {
		t.Fatalf("got %d artists / total %d, want 2 / 2", len(doc.Containers), res.TotalMatches)
	}
	miles := doc.Containers[0]
	if miles.Class != "object.container.person.musicArtist" {
		t.Errorf("artist class = %q", miles.Class)
	}
	if miles.ChildCount != 1 {
		t.Errorf("artist childCount = %d, want 1", miles.ChildCount)
	}
	if miles.ID != EncodeArtistID("Miles Davis") {
		t.Errorf("artist id = %q", miles.ID)
	}

	albums, ares := browse(t, cd, miles.ID, "BrowseDirectChildren", 0, 0)
	if len(albums.Containers) != 1 || ares.TotalMatches != 1 {
		t.Fatalf("artist albums: got %d / total %d, want 1 / 1", len(albums.Containers), ares.TotalMatches)
	}
	if albums.Containers[0].Title != "Kind of Blue" {
		t.Errorf("artist album = %q", albums.Containers[0].Title)
	}
	// Browsing into an artist's album must keep that artist as the parent so
	// the client's back button returns to the artist, not the flat list.
	if albums.Containers[0].ParentID != miles.ID {
		t.Errorf("album parentID = %q, want %q", albums.Containers[0].ParentID, miles.ID)
	}
}

func TestBrowsePagination(t *testing.T) {
	lib := &fakeLibrary{}
	for i := 0; i < 25; i++ {
		lib.albums = append(lib.albums, ServerAlbum{Title: fmt.Sprintf("Album %02d", i), URI: fmt.Sprintf("u/%d", i)})
	}
	cd := newTestCD(lib)

	tests := []struct {
		name         string
		start, count int
		wantReturned int
		wantFirst    string
	}{
		{"first page", 0, 10, 10, "Album 00"},
		{"second page", 10, 10, 10, "Album 10"},
		{"last partial page", 20, 10, 5, "Album 20"},
		{"start past the end", 25, 10, 0, ""},
		{"start beyond the end is clamped, not an error", 99, 10, 0, ""},
		// RequestedCount=0 means "as many as you can", not "none". A literal
		// reading returns nothing and the client shows an empty library.
		{"zero count means all", 0, 0, 25, "Album 00"},
		// A negative RequestedCount cannot reach here: UPnP types it ui4, and
		// the shared atoi helper drops the sign anyway. paginate's own
		// handling of count <= 0 is covered by TestPaginate.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, res := browse(t, cd, ObjectIDAlbums, "BrowseDirectChildren", tt.start, tt.count)
			if len(doc.Containers) != tt.wantReturned {
				t.Errorf("returned %d containers, want %d", len(doc.Containers), tt.wantReturned)
			}
			if res.NumberReturned != tt.wantReturned {
				t.Errorf("NumberReturned = %d, want %d", res.NumberReturned, tt.wantReturned)
			}
			// TotalMatches is the whole collection regardless of the page;
			// that is how the client knows to ask for more.
			if res.TotalMatches != 25 {
				t.Errorf("TotalMatches = %d, want 25", res.TotalMatches)
			}
			if tt.wantFirst != "" && doc.Containers[0].Title != tt.wantFirst {
				t.Errorf("first on page = %q, want %q", doc.Containers[0].Title, tt.wantFirst)
			}
		})
	}
}

func TestBrowseCapsResponseSize(t *testing.T) {
	// A client may ask for everything. Answering with a four-figure object
	// count in one SOAP body produces a document several clients truncate
	// silently, so the response is capped and TotalMatches stays honest.
	lib := &fakeLibrary{}
	for i := 0; i < maxBrowseCount+123; i++ {
		lib.albums = append(lib.albums, ServerAlbum{Title: fmt.Sprintf("Album %04d", i)})
	}
	cd := newTestCD(lib)

	doc, res := browse(t, cd, ObjectIDAlbums, "BrowseDirectChildren", 0, 0)
	if len(doc.Containers) != maxBrowseCount {
		t.Errorf("returned %d containers, want the cap %d", len(doc.Containers), maxBrowseCount)
	}
	if res.TotalMatches != maxBrowseCount+123 {
		t.Errorf("TotalMatches = %d, want the true total %d", res.TotalMatches, maxBrowseCount+123)
	}

	// A count above the cap is clamped too.
	doc, _ = browse(t, cd, ObjectIDAlbums, "BrowseDirectChildren", 0, 10_000)
	if len(doc.Containers) != maxBrowseCount {
		t.Errorf("explicit oversized count returned %d, want the cap %d", len(doc.Containers), maxBrowseCount)
	}
}

func TestBrowseMetadata(t *testing.T) {
	cd := newTestCD(newTestLibrary())
	albumID := EncodeAlbumID(AlbumRef{Title: "Kind of Blue", AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue"})

	tests := []struct {
		name      string
		objectID  string
		wantTitle string
		wantClass string
		isItem    bool
	}{
		{"root", ObjectIDRoot, "Stellar Library", "object.container.storageFolder", false},
		{"albums", ObjectIDAlbums, "Albums", "object.container.storageFolder", false},
		{"artists", ObjectIDArtists, "Artists", "object.container.storageFolder", false},
		{"album", albumID, "Kind of Blue", "object.container.album.musicAlbum", false},
		{"artist", EncodeArtistID("Miles Davis"), "Miles Davis", "object.container.person.musicArtist", false},
		{"track", EncodeTrackID("USB/Kind of Blue/01 So What.flac"), "01 So What.flac", "object.item.audioItem.musicTrack", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, res := browse(t, cd, tt.objectID, "BrowseMetadata", 0, 0)
			if res.NumberReturned != 1 {
				t.Fatalf("NumberReturned = %d, want 1", res.NumberReturned)
			}
			if tt.isItem {
				if len(doc.Items) != 1 {
					t.Fatalf("got %d items, want 1", len(doc.Items))
				}
				if doc.Items[0].Title != tt.wantTitle || doc.Items[0].Class != tt.wantClass {
					t.Errorf("item = %q / %q, want %q / %q",
						doc.Items[0].Title, doc.Items[0].Class, tt.wantTitle, tt.wantClass)
				}
				return
			}
			if len(doc.Containers) != 1 {
				t.Fatalf("got %d containers, want 1", len(doc.Containers))
			}
			if doc.Containers[0].Title != tt.wantTitle || doc.Containers[0].Class != tt.wantClass {
				t.Errorf("container = %q / %q, want %q / %q",
					doc.Containers[0].Title, doc.Containers[0].Class, tt.wantTitle, tt.wantClass)
			}
			if doc.Containers[0].ID != tt.objectID {
				t.Errorf("metadata id = %q, want the requested %q", doc.Containers[0].ID, tt.objectID)
			}
		})
	}
}

func TestBrowseMetadataOnRootHasNoParent(t *testing.T) {
	// The root's parentID must be "-1"; a client that sees "0" there can
	// loop forever walking up the tree.
	cd := newTestCD(newTestLibrary())
	doc, _ := browse(t, cd, ObjectIDRoot, "BrowseMetadata", 0, 0)
	if doc.Containers[0].ParentID != "-1" {
		t.Errorf("root parentID = %q, want -1", doc.Containers[0].ParentID)
	}
}

func TestBrowseUnknownObjectIDReturnsSOAPFault(t *testing.T) {
	cd := newTestCD(newTestLibrary())
	for _, flag := range []string{"BrowseDirectChildren", "BrowseMetadata"} {
		t.Run(flag, func(t *testing.T) {
			rec, env := call(t, cd, "Browse", browseBody("Z:nonsense", flag, 0, 0))
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", rec.Code)
			}
			// 701 "No such object" is what a control point translates into a
			// useful message; a bare HTTP error reads as a connection problem
			// and sends the user looking in the wrong place.
			if env.Body.Fault.Code != "701" {
				t.Errorf("errorCode = %q, want 701 (body: %s)", env.Body.Fault.Code, rec.Body.String())
			}
		})
	}
}

func TestBrowseLibraryErrorReturnsActionFailed(t *testing.T) {
	lib := newTestLibrary()
	lib.albumsErr = errors.New("cache is rebuilding")
	cd := newTestCD(lib)

	rec, env := call(t, cd, "Browse", browseBody(ObjectIDAlbums, "BrowseDirectChildren", 0, 0))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if env.Body.Fault.Code != "501" {
		t.Errorf("errorCode = %q, want 501 (action failed)", env.Body.Fault.Code)
	}
	if !strings.Contains(env.Body.Fault.Desc, "cache is rebuilding") {
		t.Errorf("errorDescription = %q, want the underlying cause", env.Body.Fault.Desc)
	}
}

func TestBrowseTrackChildrenIsEmptyNotAnError(t *testing.T) {
	// Browsing a leaf's children is legal and must return an empty list.
	cd := newTestCD(newTestLibrary())
	doc, res := browse(t, cd, EncodeTrackID("USB/a/01.flac"), "BrowseDirectChildren", 0, 0)
	if len(doc.Items) != 0 || len(doc.Containers) != 0 || res.TotalMatches != 0 {
		t.Errorf("leaf children: %+v / total %d", doc, res.TotalMatches)
	}
}

func TestBrowseSkipsTracksTheStreamEndpointCannotServe(t *testing.T) {
	// An extension outside the shared media-type allowlist means /stream
	// would answer 404. Publishing the item anyway hands the client a URL
	// that fails at play time with no way to tell why.
	lib := newTestLibrary()
	lib.tracks["Kind of Blue"] = append(lib.tracks["Kind of Blue"],
		ServerTrack{Title: "liner notes", URI: "USB/Kind of Blue/notes.pdf"},
		ServerTrack{Title: "checksums", URI: "USB/Kind of Blue/album.sfv"},
	)
	cd := newTestCD(lib)
	albumID := EncodeAlbumID(AlbumRef{Title: "Kind of Blue", AlbumArtist: "Miles Davis", URI: "USB/Kind of Blue"})

	doc, res := browse(t, cd, albumID, "BrowseDirectChildren", 0, 0)
	if len(doc.Items) != 2 {
		t.Fatalf("got %d items, want the 2 playable ones", len(doc.Items))
	}
	// TotalMatches must count only what was published, or the client pages
	// forever looking for objects that do not exist.
	if res.TotalMatches != 2 {
		t.Errorf("TotalMatches = %d, want 2", res.TotalMatches)
	}
	for _, it := range doc.Items {
		if strings.HasSuffix(it.Res[0].URL, ".pdf") || strings.HasSuffix(it.Res[0].URL, ".sfv") {
			t.Errorf("published an unservable resource: %q", it.Res[0].URL)
		}
	}
}

func TestBrowseMetadataOnUnservableTrackIsRefused(t *testing.T) {
	cd := newTestCD(newTestLibrary())
	rec, env := call(t, cd, "Browse", browseBody(EncodeTrackID("USB/a/notes.pdf"), "BrowseMetadata", 0, 0))
	if rec.Code != http.StatusInternalServerError || env.Body.Fault.Code != "701" {
		t.Errorf("status %d / errorCode %q, want 500 / 701", rec.Code, env.Body.Fault.Code)
	}
}

func TestSearch(t *testing.T) {
	tests := []struct {
		name      string
		container string
		criteria  string
		wantCount int
	}{
		{
			"title contains",
			ObjectIDRoot,
			`dc:title contains "blue"`,
			1,
		},
		{
			"artist contains matches the album artist",
			ObjectIDRoot,
			`upnp:artist contains "Rachmaninoff"`,
			1,
		},
		{"genre contains", ObjectIDRoot, `upnp:genre contains "Jazz"`, 1},
		{"case insensitive", ObjectIDRoot, `dc:title contains "KIND OF"`, 1},
		{"no match", ObjectIDRoot, `dc:title contains "zzzz"`, 0},
		// "*" is the spec's "everything" criteria, used by clients that
		// browse-by-search.
		{"star returns everything", ObjectIDRoot, `*`, 2},
		{"empty criteria returns everything", ObjectIDRoot, ``, 2},
		{
			// Searching inside an artist container narrows the candidates
			// the way the client expects.
			"scoped to an artist container",
			EncodeArtistID("Miles Davis"),
			`*`,
			1,
		},
		{"scoped search that matches nothing in scope", EncodeArtistID("Miles Davis"), `dc:title contains "Études"`, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cd := newTestCD(newTestLibrary())
			rec, env := call(t, cd, "Search", searchBody(tt.container, tt.criteria, 0, 0))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body:\n%s", rec.Code, rec.Body.String())
			}
			doc := parseDIDLDoc(t, env.Body.Search.Result)
			if len(doc.Containers) != tt.wantCount {
				t.Errorf("got %d results, want %d: %+v", len(doc.Containers), tt.wantCount, doc.Containers)
			}
			if env.Body.Search.NumberReturned != tt.wantCount {
				t.Errorf("NumberReturned = %d, want %d", env.Body.Search.NumberReturned, tt.wantCount)
			}
		})
	}
}

func TestSearchTerms(t *testing.T) {
	tests := []struct {
		criteria string
		want     []string
	}{
		{`dc:title contains "blue"`, []string{"blue"}},
		{`dc:title contains "a" and upnp:artist contains "B"`, []string{"a", "b"}},
		{`upnp:class = "object.container.album.musicAlbum"`, []string{"object.container.album.musicalbum"}},
		{`*`, nil},
		{``, nil},
		{`dc:title contains ""`, nil},
		{`unterminated "quote`, nil},
	}
	for _, tt := range tests {
		got := searchTerms(tt.criteria)
		if len(got) != len(tt.want) {
			t.Errorf("searchTerms(%q) = %v, want %v", tt.criteria, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("searchTerms(%q)[%d] = %q, want %q", tt.criteria, i, got[i], tt.want[i])
			}
		}
	}
}

func TestCapabilityActions(t *testing.T) {
	cd := newTestCD(newTestLibrary())

	_, env := call(t, cd, "GetSearchCapabilities", `<?xml version="1.0"?><s:Envelope><s:Body/></s:Envelope>`)
	if !strings.Contains(env.Body.Caps, "dc:title") {
		t.Errorf("SearchCaps = %q, want dc:title listed", env.Body.Caps)
	}

	// Sorting is not implemented, and advertising caps we ignore makes the
	// client present a sort UI that does nothing.
	_, env = call(t, cd, "GetSortCapabilities", `<?xml version="1.0"?><s:Envelope><s:Body/></s:Envelope>`)
	if env.Body.SortCaps != "" {
		t.Errorf("SortCaps = %q, want empty", env.Body.SortCaps)
	}

	_, env = call(t, cd, "GetSystemUpdateID", `<?xml version="1.0"?><s:Envelope><s:Body/></s:Envelope>`)
	if env.Body.UpdateID != "1" {
		t.Errorf("SystemUpdateID = %q, want 1", env.Body.UpdateID)
	}
}

func TestUnknownActionIsAnsweredNotFaulted(t *testing.T) {
	// An unimplemented optional action must come back as an empty success,
	// the way the renderer's SOAP handler already does; a fault here makes
	// some clients abandon the whole device.
	cd := newTestCD(newTestLibrary())
	rec, _ := call(t, cd, "CreateObject", `<?xml version="1.0"?><s:Envelope><s:Body/></s:Envelope>`)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestEventSubscriptionIsAccepted(t *testing.T) {
	cd := newTestCD(newTestLibrary())
	mux := http.NewServeMux()
	cd.RegisterHandlers(mux)

	req := httptest.NewRequest("SUBSCRIBE", "/upnp/event/ContentDirectory", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("SUBSCRIBE status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("SID") == "" {
		t.Error("SUBSCRIBE response has no SID header")
	}
}

func TestPaginate(t *testing.T) {
	tests := []struct {
		name             string
		n, start, count  int
		wantFrom, wantTo int
	}{
		{"whole set", 10, 0, 10, 0, 10},
		{"mid page", 10, 3, 4, 3, 7},
		{"count overruns the end", 10, 8, 5, 8, 10},
		{"start at the end", 10, 10, 5, 10, 10},
		{"start past the end is clamped", 10, 50, 5, 10, 10},
		{"negative start is clamped", 10, -3, 5, 0, 5},
		{"zero count means the cap", 10, 0, 0, 0, 10},
		{"empty set", 0, 0, 10, 0, 0},
		{"empty set, start past end", 0, 7, 10, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, total := paginate(tt.n, tt.start, tt.count)
			if page.from != tt.wantFrom || page.to != tt.wantTo {
				t.Errorf("paginate(%d,%d,%d) = [%d:%d], want [%d:%d]",
					tt.n, tt.start, tt.count, page.from, page.to, tt.wantFrom, tt.wantTo)
			}
			if total != tt.n {
				t.Errorf("total = %d, want %d", total, tt.n)
			}
			// The range must always be a legal slice of n elements.
			if page.from < 0 || page.to < page.from || page.to > tt.n {
				t.Errorf("paginate produced an illegal slice range [%d:%d] for n=%d", page.from, page.to, tt.n)
			}
		})
	}
}

func TestBaseName(t *testing.T) {
	tests := map[string]string{
		"USB/a/01 Track.flac": "01 Track.flac",
		"01 Track.flac":       "01 Track.flac",
		"a/b/":                "",
		"":                    "",
	}
	for in, want := range tests {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}
