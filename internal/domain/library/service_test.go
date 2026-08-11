package library

import (
	"fmt"
	"strings"
	"testing"
)

// MockMPDClient implements the MPDClient interface for testing.
type MockMPDClient struct {
	// Album queries
	ListAlbumsResponse    []AlbumInfo
	ListAlbumsError       error
	ListAlbumsInBaseResp  map[string][]AlbumInfo
	ListAlbumsInBaseError error
	GetAlbumDetailsResp   map[string][]AlbumDetails
	GetAlbumDetailsError  error
	CountAlbumsResult     int
	CountAlbumsError      error

	// Artist queries
	ListArtistsResponse     []string
	ListArtistsError        error
	FindAlbumsByArtistResp  map[string][]AlbumInfo
	FindAlbumsByArtistError error

	// Track queries
	FindAlbumTracksResp  map[string][]map[string]string
	FindAlbumTracksError error
	SearchByBaseResp     map[string][]map[string]string
	SearchByBaseError    error

	// Playlist/radio queries
	ListPlaylistsResponse []string
	ListPlaylistsError    error
	ListPlaylistInfoResp  map[string][]map[string]string
	ListPlaylistInfoError error
}

func (m *MockMPDClient) ListAlbums() ([]AlbumInfo, error) {
	if m.ListAlbumsError != nil {
		return nil, m.ListAlbumsError
	}
	return m.ListAlbumsResponse, nil
}

func (m *MockMPDClient) ListAlbumsInBase(basePath string) ([]AlbumInfo, error) {
	if m.ListAlbumsInBaseError != nil {
		return nil, m.ListAlbumsInBaseError
	}
	if resp, ok := m.ListAlbumsInBaseResp[basePath]; ok {
		return resp, nil
	}
	return []AlbumInfo{}, nil
}

func (m *MockMPDClient) GetAlbumDetails(basePath string) ([]AlbumDetails, error) {
	if m.GetAlbumDetailsError != nil {
		return nil, m.GetAlbumDetailsError
	}
	if resp, ok := m.GetAlbumDetailsResp[basePath]; ok {
		return resp, nil
	}
	return []AlbumDetails{}, nil
}

func (m *MockMPDClient) CountAlbums() (int, error) {
	return m.CountAlbumsResult, m.CountAlbumsError
}

func (m *MockMPDClient) ListArtists() ([]string, error) {
	if m.ListArtistsError != nil {
		return nil, m.ListArtistsError
	}
	return m.ListArtistsResponse, nil
}

func (m *MockMPDClient) FindAlbumsByArtist(artist string) ([]AlbumInfo, error) {
	if m.FindAlbumsByArtistError != nil {
		return nil, m.FindAlbumsByArtistError
	}
	if resp, ok := m.FindAlbumsByArtistResp[artist]; ok {
		return resp, nil
	}
	return []AlbumInfo{}, nil
}

func (m *MockMPDClient) FindAlbumTracks(album, albumArtist string) ([]map[string]string, error) {
	if m.FindAlbumTracksError != nil {
		return nil, m.FindAlbumTracksError
	}
	key := album + "\x00" + albumArtist
	if resp, ok := m.FindAlbumTracksResp[key]; ok {
		return resp, nil
	}
	return []map[string]string{}, nil
}

func (m *MockMPDClient) SearchByBase(basePath string) ([]map[string]string, error) {
	if m.SearchByBaseError != nil {
		return nil, m.SearchByBaseError
	}
	if resp, ok := m.SearchByBaseResp[basePath]; ok {
		return resp, nil
	}
	return []map[string]string{}, nil
}

func (m *MockMPDClient) ListPlaylists() ([]string, error) {
	if m.ListPlaylistsError != nil {
		return nil, m.ListPlaylistsError
	}
	return m.ListPlaylistsResponse, nil
}

func (m *MockMPDClient) ListPlaylistInfo(name string) ([]map[string]string, error) {
	if m.ListPlaylistInfoError != nil {
		return nil, m.ListPlaylistInfoError
	}
	if resp, ok := m.ListPlaylistInfoResp[name]; ok {
		return resp, nil
	}
	return []map[string]string{}, nil
}

// MockPathClassifier implements source classification for testing.
type MockPathClassifier struct {
	SourceMap map[string]SourceType
}

func (m *MockPathClassifier) GetSourceType(uri string) SourceType {
	if m.SourceMap != nil {
		if src, ok := m.SourceMap[uri]; ok {
			return src
		}
	}
	// Default classification based on prefix
	if len(uri) >= 4 && uri[:4] == "NAS/" {
		return SourceNAS
	}
	if len(uri) >= 4 && uri[:4] == "USB/" {
		return SourceUSB
	}
	return SourceLocal
}

// --- GetAlbums Tests ---

func TestService_GetAlbums_All_Empty(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {},
			"USB":      {},
			"NAS":      {},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeAll,
		Sort:  SortAlphabetical,
	})

	if len(resp.Albums) != 0 {
		t.Errorf("Expected 0 albums, got %d", len(resp.Albums))
	}
	if resp.Pagination.Total != 0 {
		t.Errorf("Expected total 0, got %d", resp.Pagination.Total)
	}
}

func TestService_GetAlbums_All_WithAlbums(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {
				{Album: "Internal Album", AlbumArtist: "Artist1", TrackCount: 10, FirstTrack: "INTERNAL/Album1/track.flac"},
			},
			"USB": {
				{Album: "USB Album", AlbumArtist: "Artist2", TrackCount: 8, FirstTrack: "USB/Album2/track.flac"},
			},
			"NAS": {
				{Album: "NAS Album", AlbumArtist: "Artist3", TrackCount: 12, FirstTrack: "NAS/Album3/track.flac"},
			},
		},
	}

	classifier := &MockPathClassifier{
		SourceMap: map[string]SourceType{
			"INTERNAL/Album1": SourceLocal,
			"USB/Album2":      SourceUSB,
			"NAS/Album3":      SourceNAS,
		},
	}

	service := NewService(mockMPD, classifier)

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeAll,
		Sort:  SortAlphabetical,
	})

	if len(resp.Albums) != 3 {
		t.Errorf("Expected 3 albums, got %d", len(resp.Albums))
	}
	if resp.Pagination.Total != 3 {
		t.Errorf("Expected total 3, got %d", resp.Pagination.Total)
	}
}

func TestService_GetAlbums_NAS_FiltersBySource(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"NAS": {
				{Album: "NAS Album 1", AlbumArtist: "Artist1", TrackCount: 10, FirstTrack: "NAS/Album1/track.flac"},
				{Album: "NAS Album 2", AlbumArtist: "Artist2", TrackCount: 8, FirstTrack: "NAS/Album2/track.flac"},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeNAS,
		Sort:  SortAlphabetical,
	})

	if len(resp.Albums) != 2 {
		t.Errorf("Expected 2 NAS albums, got %d", len(resp.Albums))
	}

	for _, album := range resp.Albums {
		if album.Source != SourceNAS {
			t.Errorf("Expected source NAS, got %s for album %s", album.Source, album.Title)
		}
	}
}

func TestService_GetAlbums_Local_IncludesInternalAndUSB(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {
				{Album: "Internal Album", AlbumArtist: "Artist1", TrackCount: 10, FirstTrack: "INTERNAL/Album1/track.flac"},
			},
			"USB": {
				{Album: "USB Album", AlbumArtist: "Artist2", TrackCount: 8, FirstTrack: "USB/Album2/track.flac"},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeLocal,
		Sort:  SortAlphabetical,
	})

	if len(resp.Albums) != 2 {
		t.Errorf("Expected 2 local albums (INTERNAL + USB), got %d", len(resp.Albums))
	}
}

func TestService_GetAlbums_USB_OnlyUSB(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"USB": {
				{Album: "USB Album", AlbumArtist: "Artist1", TrackCount: 10, FirstTrack: "USB/Album1/track.flac"},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeUSB,
		Sort:  SortAlphabetical,
	})

	if len(resp.Albums) != 1 {
		t.Errorf("Expected 1 USB album, got %d", len(resp.Albums))
	}

	if len(resp.Albums) > 0 && resp.Albums[0].Source != SourceUSB {
		t.Errorf("Expected source USB, got %s", resp.Albums[0].Source)
	}
}

func TestService_GetAlbums_WithQuery(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {
				{Album: "Jazz Album", AlbumArtist: "Jazz Artist", TrackCount: 10, FirstTrack: "INTERNAL/Jazz/track.flac"},
				{Album: "Rock Album", AlbumArtist: "Rock Artist", TrackCount: 8, FirstTrack: "INTERNAL/Rock/track.flac"},
			},
			"USB": {},
			"NAS": {},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeAll,
		Sort:  SortAlphabetical,
		Query: "jazz",
	})

	if len(resp.Albums) != 1 {
		t.Errorf("Expected 1 album matching 'jazz', got %d", len(resp.Albums))
	}

	if len(resp.Albums) > 0 && resp.Albums[0].Title != "Jazz Album" {
		t.Errorf("Expected 'Jazz Album', got %s", resp.Albums[0].Title)
	}
}

func TestService_GetAlbums_Pagination(t *testing.T) {
	albums := make([]AlbumDetails, 15)
	for i := 0; i < 15; i++ {
		albums[i] = AlbumDetails{
			Album:       fmt.Sprintf("Album %02d", i+1),
			AlbumArtist: "Artist",
			TrackCount:  5,
			FirstTrack:  fmt.Sprintf("INTERNAL/Album%02d/track.flac", i+1),
		}
	}

	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": albums,
			"USB":      {},
			"NAS":      {},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	// Page 1 with limit 5
	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeAll,
		Sort:  SortAlphabetical,
		Page:  1,
		Limit: 5,
	})

	if len(resp.Albums) != 5 {
		t.Errorf("Expected 5 albums on page 1, got %d", len(resp.Albums))
	}
	if resp.Pagination.Total != 15 {
		t.Errorf("Expected total 15, got %d", resp.Pagination.Total)
	}
	if !resp.Pagination.HasMore {
		t.Error("Expected hasMore to be true")
	}

	// Page 3 (last page)
	resp = service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeAll,
		Sort:  SortAlphabetical,
		Page:  3,
		Limit: 5,
	})

	if len(resp.Albums) != 5 {
		t.Errorf("Expected 5 albums on page 3, got %d", len(resp.Albums))
	}
	if resp.Pagination.HasMore {
		t.Error("Expected hasMore to be false on last page")
	}
}

func TestService_GetAlbums_SortAlphabetical(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {
				{Album: "Zebra", AlbumArtist: "Artist", FirstTrack: "INTERNAL/Z/track.flac"},
				{Album: "Apple", AlbumArtist: "Artist", FirstTrack: "INTERNAL/A/track.flac"},
				{Album: "Mango", AlbumArtist: "Artist", FirstTrack: "INTERNAL/M/track.flac"},
			},
			"USB": {},
			"NAS": {},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeAll,
		Sort:  SortAlphabetical,
	})

	if len(resp.Albums) != 3 {
		t.Fatalf("Expected 3 albums, got %d", len(resp.Albums))
	}

	if resp.Albums[0].Title != "Apple" || resp.Albums[1].Title != "Mango" || resp.Albums[2].Title != "Zebra" {
		t.Errorf("Albums not sorted alphabetically: %v, %v, %v",
			resp.Albums[0].Title, resp.Albums[1].Title, resp.Albums[2].Title)
	}
}

func TestService_GetAlbums_SortByArtist(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {
				{Album: "Album C", AlbumArtist: "Zoe", FirstTrack: "INTERNAL/Z/track.flac"},
				{Album: "Album A", AlbumArtist: "Alice", FirstTrack: "INTERNAL/A/track.flac"},
				{Album: "Album B", AlbumArtist: "Bob", FirstTrack: "INTERNAL/B/track.flac"},
			},
			"USB": {},
			"NAS": {},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeAll,
		Sort:  SortByArtist,
	})

	if len(resp.Albums) != 3 {
		t.Fatalf("Expected 3 albums, got %d", len(resp.Albums))
	}

	if resp.Albums[0].Artist != "Alice" || resp.Albums[1].Artist != "Bob" || resp.Albums[2].Artist != "Zoe" {
		t.Errorf("Albums not sorted by artist: %v, %v, %v",
			resp.Albums[0].Artist, resp.Albums[1].Artist, resp.Albums[2].Artist)
	}
}

// --- GetAlbums grouping + badging Tests (03-03: discgroup + dupebadge wiring) ---

// TestService_GetAlbums_MahlerShaped_GroupsToOneAlbumWithDiscCount pins
// 03-03-PLAN.md's Mahler-shaped truth: 11 raw per-basePath AlbumDetails
// entries (one per CD-NN folder, distinct Disc tags 1..11, all sharing one
// common parent directory) collapse to exactly ONE Album with DiscCount==11
// and URI equal to the box set's common parent directory -- NOT a CD-NN
// subfolder path (the critical bug this plan fixes: URI must become
// group.RootDir, not path.Dir(group.FirstTrack)).
func TestService_GetAlbums_MahlerShaped_GroupsToOneAlbumWithDiscCount(t *testing.T) {
	details := make([]AlbumDetails, 11)
	wantTrackCount := 0
	for i := 0; i < 11; i++ {
		discNum := i + 1
		trackCount := 5 + i
		wantTrackCount += trackCount
		details[i] = AlbumDetails{
			Album:       "Mahler: The Symphonies",
			AlbumArtist: "Gustav Mahler",
			TrackCount:  trackCount,
			FirstTrack: fmt.Sprintf(
				"USB/Mahler The Symphonies/CD %02d/01 - track.flac", discNum,
			),
			Format: "44100:16:2",
			Disc:   fmt.Sprintf("%d", discNum),
		}
	}

	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"USB": details,
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeUSB,
		Sort:  SortAlphabetical,
	})

	if len(resp.Albums) != 1 {
		t.Fatalf("Expected 1 grouped Mahler album, got %d: %+v", len(resp.Albums), resp.Albums)
	}

	album := resp.Albums[0]
	if album.DiscCount != 11 {
		t.Errorf("DiscCount = %d, want 11", album.DiscCount)
	}
	if album.URI != "USB/Mahler The Symphonies" {
		t.Errorf("URI = %q, want the common parent %q (not a CD subfolder)", album.URI, "USB/Mahler The Symphonies")
	}
	if album.TrackCount != wantTrackCount {
		t.Errorf("TrackCount = %d, want %d (summed across all 11 discs)", album.TrackCount, wantTrackCount)
	}
	if album.Badge != "" {
		t.Errorf("Badge = %q, want empty (Mahler collapses to a single album, no duplicate cluster)", album.Badge)
	}
}

// TestService_GetAlbums_KindOfBlueShaped_StaysSeparateWithQualityBadges pins
// the load-bearing D-06 negative case: 3 raw AlbumDetails entries sharing
// title+artist, all carrying IDENTICAL Disc:"1" and none matching the CD
// path marker, must NOT be grouped by discgroup (Kind Of Blue is 3 distinct
// releases, not a box set). Since their quality differs, dupebadge's tier-1
// precedence fires: each Album's Badge is its own quality string verbatim.
func TestService_GetAlbums_KindOfBlueShaped_StaysSeparateWithQualityBadges(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"USB": {
				{
					Album: "Kind Of Blue", AlbumArtist: "Miles Davis", TrackCount: 5,
					FirstTrack: "USB/Miles Davis/Kind Of Blue (DSD64)/01.dsf",
					Format:     "2822400:f:2", Disc: "1",
				},
				{
					Album: "Kind Of Blue", AlbumArtist: "Miles Davis", TrackCount: 5,
					FirstTrack: "USB/Miles Davis/Kind Of Blue (DSD128)/01.dsf",
					Format:     "5644800:f:2", Disc: "1",
				},
				{
					Album: "Kind Of Blue", AlbumArtist: "Miles Davis", TrackCount: 5,
					FirstTrack: "USB/Miles Davis/Kind Of Blue (FLAC)/01.flac",
					Format:     "352800:24:2", Disc: "1",
				},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeUSB,
		Sort:  SortAlphabetical,
	})

	if len(resp.Albums) != 3 {
		t.Fatalf("Expected 3 separate Kind Of Blue albums (D-06 negative case), got %d: %+v", len(resp.Albums), resp.Albums)
	}

	for _, album := range resp.Albums {
		if album.DiscCount != 0 {
			t.Errorf("DiscCount = %d, want 0 (not a box set) for %q", album.DiscCount, album.URI)
		}
		if album.Badge == "" {
			t.Errorf("Badge is empty, want each album's own quality string for %q", album.URI)
		}
		if album.Badge != album.Quality {
			t.Errorf("Badge = %q, want it to equal this album's own Quality %q (quality tier)", album.Badge, album.Quality)
		}
	}

	// Distinctness sanity: the whole point of the quality tier is that the
	// three badges are not all identical.
	seen := make(map[string]bool)
	for _, album := range resp.Albums {
		seen[album.Badge] = true
	}
	if len(seen) < 2 {
		t.Errorf("Expected at least 2 distinct badge values among the 3 Kind Of Blue versions, got %v", seen)
	}
}

// TestService_GetAlbums_UniqueAlbum_NoBadgeNoDiscCount pins BROWSE-03/D-03:
// a single unrelated album (no title+artist duplicate, not a box set) gets
// Badge=="" and DiscCount==0 -- the common case stays clean.
func TestService_GetAlbums_UniqueAlbum_NoBadgeNoDiscCount(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"USB": {
				{
					Album: "Solo Album", AlbumArtist: "Some Artist", TrackCount: 8,
					FirstTrack: "USB/Some Artist/Solo Album/01.flac",
					Format:     "44100:16:2", Disc: "1",
				},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeUSB,
		Sort:  SortAlphabetical,
	})

	if len(resp.Albums) != 1 {
		t.Fatalf("Expected 1 album, got %d", len(resp.Albums))
	}
	album := resp.Albums[0]
	if album.Badge != "" {
		t.Errorf("Badge = %q, want empty for a unique album", album.Badge)
	}
	if album.DiscCount != 0 {
		t.Errorf("DiscCount = %d, want 0 for a unique, non-box-set album", album.DiscCount)
	}
}

// --- GetArtistAlbums grouping + badging Tests (03-03) ---

// TestService_GetArtistAlbums_MahlerShaped_GroupsToOneAlbumWithDiscCount
// mirrors the GetAlbums-level Mahler test but through GetArtistAlbums's own
// separate per-basePath loop, proving the identical grouping+URI fix was
// applied there too (it does not call getAlbumsFromBasePath).
func TestService_GetArtistAlbums_MahlerShaped_GroupsToOneAlbumWithDiscCount(t *testing.T) {
	details := make([]AlbumDetails, 11)
	for i := 0; i < 11; i++ {
		discNum := i + 1
		details[i] = AlbumDetails{
			Album:       "Mahler: The Symphonies",
			AlbumArtist: "Gustav Mahler",
			TrackCount:  5,
			FirstTrack: fmt.Sprintf(
				"USB/Mahler The Symphonies/CD %02d/01 - track.flac", discNum,
			),
			Format: "44100:16:2",
			Disc:   fmt.Sprintf("%d", discNum),
		}
	}

	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"USB": details,
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetArtistAlbums(GetArtistAlbumsRequest{
		Artist: "Gustav Mahler",
		Sort:   SortAlphabetical,
	})

	if len(resp.Albums) != 1 {
		t.Fatalf("Expected 1 grouped Mahler album via GetArtistAlbums, got %d: %+v", len(resp.Albums), resp.Albums)
	}
	album := resp.Albums[0]
	if album.DiscCount != 11 {
		t.Errorf("DiscCount = %d, want 11", album.DiscCount)
	}
	if album.URI != "USB/Mahler The Symphonies" {
		t.Errorf("URI = %q, want the common parent %q", album.URI, "USB/Mahler The Symphonies")
	}
}

// TestService_GetArtistAlbums_KindOfBlueShaped_StaysSeparate re-asserts the
// D-06 negative case at the GetArtistAlbums wiring layer too (hard
// constraint 5): 3 same-quality-tag-1 Kind Of Blue folders must remain 3
// separate Album entries when queried by artist, not collapsed to 1.
func TestService_GetArtistAlbums_KindOfBlueShaped_StaysSeparate(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"USB": {
				{
					Album: "Kind Of Blue", AlbumArtist: "Miles Davis", TrackCount: 5,
					FirstTrack: "USB/Miles Davis/Kind Of Blue (DSD64)/01.dsf",
					Format:     "2822400:f:2", Disc: "1",
				},
				{
					Album: "Kind Of Blue", AlbumArtist: "Miles Davis", TrackCount: 5,
					FirstTrack: "USB/Miles Davis/Kind Of Blue (DSD128)/01.dsf",
					Format:     "5644800:f:2", Disc: "1",
				},
				{
					Album: "Kind Of Blue", AlbumArtist: "Miles Davis", TrackCount: 5,
					FirstTrack: "USB/Miles Davis/Kind Of Blue (FLAC)/01.flac",
					Format:     "352800:24:2", Disc: "1",
				},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetArtistAlbums(GetArtistAlbumsRequest{
		Artist: "Miles Davis",
		Sort:   SortAlphabetical,
	})

	if len(resp.Albums) != 3 {
		t.Fatalf("Expected 3 separate Kind Of Blue albums via GetArtistAlbums, got %d: %+v", len(resp.Albums), resp.Albums)
	}
	for _, album := range resp.Albums {
		if album.DiscCount != 0 {
			t.Errorf("DiscCount = %d, want 0 for %q", album.DiscCount, album.URI)
		}
		if album.Badge == "" {
			t.Errorf("Badge is empty, want a quality badge for %q", album.URI)
		}
	}
}

// --- GetArtists Tests ---

func TestService_GetArtists_Empty(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListArtistsResponse: []string{},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetArtists(GetArtistsRequest{})

	if len(resp.Artists) != 0 {
		t.Errorf("Expected 0 artists, got %d", len(resp.Artists))
	}
}

func TestService_GetArtists_WithArtists(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListArtistsResponse: []string{"Artist A", "Artist B", "Artist C"},
		FindAlbumsByArtistResp: map[string][]AlbumInfo{
			"Artist A": {{Album: "Album 1", AlbumArtist: "Artist A"}, {Album: "Album 2", AlbumArtist: "Artist A"}},
			"Artist B": {{Album: "Album 3", AlbumArtist: "Artist B"}},
			"Artist C": {},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetArtists(GetArtistsRequest{})

	if len(resp.Artists) != 3 {
		t.Errorf("Expected 3 artists, got %d", len(resp.Artists))
	}

	// Verify album counts
	artistMap := make(map[string]int)
	for _, a := range resp.Artists {
		artistMap[a.Name] = a.AlbumCount
	}

	if artistMap["Artist A"] != 2 {
		t.Errorf("Expected Artist A to have 2 albums, got %d", artistMap["Artist A"])
	}
	if artistMap["Artist B"] != 1 {
		t.Errorf("Expected Artist B to have 1 album, got %d", artistMap["Artist B"])
	}
}

func TestService_GetArtists_WithQuery(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListArtistsResponse: []string{"Jazz Quartet", "Rock Band", "Jazz Trio"},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetArtists(GetArtistsRequest{
		Query: "jazz",
	})

	if len(resp.Artists) != 2 {
		t.Errorf("Expected 2 artists matching 'jazz', got %d", len(resp.Artists))
	}
}

func TestService_GetArtists_Pagination(t *testing.T) {
	artists := make([]string, 25)
	for i := 0; i < 25; i++ {
		artists[i] = fmt.Sprintf("Artist %02d", i+1)
	}

	mockMPD := &MockMPDClient{
		ListArtistsResponse: artists,
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetArtists(GetArtistsRequest{
		Page:  1,
		Limit: 10,
	})

	if len(resp.Artists) != 10 {
		t.Errorf("Expected 10 artists on page 1, got %d", len(resp.Artists))
	}
	if resp.Pagination.Total != 25 {
		t.Errorf("Expected total 25, got %d", resp.Pagination.Total)
	}
	if !resp.Pagination.HasMore {
		t.Error("Expected hasMore to be true")
	}
}

// TestService_GetArtists_CollapsesDoubleSpaceRoleSuffix verifies ARTIST-01
// on the MPD-direct fallback path: the real Karajan album shape --
// AlbumArtist already "Herbert von Karajan" but raw Artist tag
// "Herbert von Karajan  Wiener Philharmoniker" (double space) -- collapses
// to exactly one Artist row named "Herbert von Karajan", matching what
// AlbumArtist already says (per 02-CONTEXT.md's "visible acceptance case").
func TestService_GetArtists_CollapsesDoubleSpaceRoleSuffix(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListArtistsResponse: []string{"Herbert von Karajan  Wiener Philharmoniker"},
		FindAlbumsByArtistResp: map[string][]AlbumInfo{
			"Herbert von Karajan  Wiener Philharmoniker": {{Album: "Symphony No. 5", AlbumArtist: "Herbert von Karajan"}},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})
	resp := service.GetArtists(GetArtistsRequest{})

	if len(resp.Artists) != 1 {
		t.Fatalf("Expected 1 artist, got %d: %+v", len(resp.Artists), resp.Artists)
	}
	if resp.Artists[0].Name != "Herbert von Karajan" {
		t.Errorf("Artist name = %q, want %q", resp.Artists[0].Name, "Herbert von Karajan")
	}
}

// TestService_GetArtists_EmptyRawNameProducesNoRow verifies ARTIST-03 on the
// MPD-direct fallback: an empty string entry in ListArtists() must never
// produce an Artist{Name: ""} row.
func TestService_GetArtists_EmptyRawNameProducesNoRow(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListArtistsResponse: []string{"", "Moby"},
	}

	service := NewService(mockMPD, &MockPathClassifier{})
	resp := service.GetArtists(GetArtistsRequest{})

	if len(resp.Artists) != 1 {
		t.Fatalf("Expected 1 artist (empty entry skipped), got %d: %+v", len(resp.Artists), resp.Artists)
	}
	for _, a := range resp.Artists {
		if a.Name == "" {
			t.Errorf("found Artist row with empty Name — ARTIST-03 violated")
		}
	}
}

// TestService_GetArtists_MergesCollapsedVariantCounts verifies that two raw
// MPD Artist tag variants collapsing to the same canonical name ("Moby")
// produce ONE Artist row whose AlbumCount is the SUM of both variants'
// FindAlbumsByArtist results, not a last-write-wins overwrite (Go slice
// iteration is deterministic but the merge logic itself must not discard
// either variant's contribution).
func TestService_GetArtists_MergesCollapsedVariantCounts(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListArtistsResponse: []string{"Moby", "Moby, Jim James"},
		FindAlbumsByArtistResp: map[string][]AlbumInfo{
			"Moby":            {{Album: "Play", AlbumArtist: "Moby"}, {Album: "18", AlbumArtist: "Moby"}, {Album: "Hotel", AlbumArtist: "Moby"}},
			"Moby, Jim James": {{Album: "Wait for Me", AlbumArtist: "Moby"}},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})
	resp := service.GetArtists(GetArtistsRequest{})

	if len(resp.Artists) != 1 {
		t.Fatalf("Expected 1 merged artist, got %d: %+v", len(resp.Artists), resp.Artists)
	}
	if resp.Artists[0].Name != "Moby" {
		t.Errorf("Artist name = %q, want %q", resp.Artists[0].Name, "Moby")
	}
	if resp.Artists[0].AlbumCount != 4 {
		t.Errorf("AlbumCount = %d, want 4 (3+1 merged)", resp.Artists[0].AlbumCount)
	}
}

// TestService_GetArtists_QueryFiltersOnCanonicalName verifies that
// req.Query filtering matches against the CANONICAL (post-collapse) name,
// not the raw MPD tag -- e.g. querying "karajan" still matches the
// collapsed "Herbert von Karajan" row even though the raw tag also
// contained "Wiener Philharmoniker".
func TestService_GetArtists_QueryFiltersOnCanonicalName(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListArtistsResponse: []string{"Herbert von Karajan  Wiener Philharmoniker", "Radiohead"},
	}

	service := NewService(mockMPD, &MockPathClassifier{})
	resp := service.GetArtists(GetArtistsRequest{Query: "karajan"})

	if len(resp.Artists) != 1 {
		t.Fatalf("Expected 1 artist matching 'karajan', got %d: %+v", len(resp.Artists), resp.Artists)
	}
	if resp.Artists[0].Name != "Herbert von Karajan" {
		t.Errorf("Artist name = %q, want %q", resp.Artists[0].Name, "Herbert von Karajan")
	}

	// A query that only matches text present in the raw tag but NOT in the
	// canonical (post-collapse) name must NOT match -- proving the filter
	// runs against the canonical name, not the raw MPD tag.
	resp2 := service.GetArtists(GetArtistsRequest{Query: "philharmoniker"})
	if len(resp2.Artists) != 0 {
		t.Errorf("Expected 0 artists matching 'philharmoniker' (present only in raw tag, not canonical name), got %d: %+v", len(resp2.Artists), resp2.Artists)
	}
}

// --- GetArtistAlbums Tests ---

func TestService_GetArtistAlbums_Empty(t *testing.T) {
	// No album details anywhere -> empty response.
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetArtistAlbums(GetArtistAlbumsRequest{
		Artist: "Unknown Artist",
	})

	if len(resp.Albums) != 0 {
		t.Errorf("Expected 0 albums, got %d", len(resp.Albums))
	}
	if resp.Artist != "Unknown Artist" {
		t.Errorf("Expected artist 'Unknown Artist', got %s", resp.Artist)
	}
}

func TestService_GetArtistAlbums_WithAlbums(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {
				{Album: "Album 1", AlbumArtist: "Test Artist", TrackCount: 10, FirstTrack: "INTERNAL/Album1/track.flac"},
				{Album: "Album 2", AlbumArtist: "Test Artist", TrackCount: 8, FirstTrack: "INTERNAL/Album2/track.flac"},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetArtistAlbums(GetArtistAlbumsRequest{
		Artist: "Test Artist",
		Sort:   SortAlphabetical,
	})

	if len(resp.Albums) != 2 {
		t.Errorf("Expected 2 albums, got %d", len(resp.Albums))
	}
	if resp.Artist != "Test Artist" {
		t.Errorf("Expected artist 'Test Artist', got %s", resp.Artist)
	}
}

func TestService_GetArtistAlbums_PopulatesFullFields(t *testing.T) {
	// Two albums by the same artist, spread across INTERNAL and NAS base
	// paths, so we also verify multi-source scoping. A third album by a
	// different artist exists in USB and must NOT appear in the result.
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {
				{
					Album:       "In Rainbows",
					AlbumArtist: "Radiohead",
					TrackCount:  10,
					FirstTrack:  "INTERNAL/Radiohead/In Rainbows/01 - 15 Step.flac",
					Format:      "44100:16:2",
				},
			},
			"USB": {
				{
					Album:       "Some Other Album",
					AlbumArtist: "Other Artist",
					TrackCount:  5,
					FirstTrack:  "USB/Other/track.flac",
					Format:      "44100:16:2",
				},
			},
			"NAS": {
				{
					Album:       "OK Computer",
					AlbumArtist: "Radiohead",
					TrackCount:  12,
					FirstTrack:  "NAS/Radiohead/OK Computer/01 - Airbag.flac",
					Format:      "96000:24:2",
				},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetArtistAlbums(GetArtistAlbumsRequest{
		Artist: "Radiohead",
		Sort:   SortAlphabetical,
	})

	if resp.Artist != "Radiohead" {
		t.Fatalf("Expected artist 'Radiohead', got %q", resp.Artist)
	}
	if len(resp.Albums) != 2 {
		t.Fatalf("Expected 2 Radiohead albums (across INTERNAL + NAS), got %d", len(resp.Albums))
	}

	// Alphabetical sort: "In Rainbows" before "OK Computer"
	inRainbows := resp.Albums[0]
	okComputer := resp.Albums[1]

	if inRainbows.Title != "In Rainbows" {
		t.Errorf("Expected first album 'In Rainbows', got %q", inRainbows.Title)
	}
	if inRainbows.URI != "INTERNAL/Radiohead/In Rainbows" {
		t.Errorf("Expected URI 'INTERNAL/Radiohead/In Rainbows', got %q", inRainbows.URI)
	}
	if inRainbows.AlbumArt != "/albumart?path=INTERNAL/Radiohead/In Rainbows/01 - 15 Step.flac" {
		t.Errorf("Expected AlbumArt to point at first track, got %q", inRainbows.AlbumArt)
	}
	if inRainbows.TrackCount != 10 {
		t.Errorf("Expected TrackCount 10, got %d", inRainbows.TrackCount)
	}
	if inRainbows.TrackType != "flac" {
		t.Errorf("Expected TrackType 'flac', got %q", inRainbows.TrackType)
	}
	if inRainbows.Source != SourceLocal {
		t.Errorf("Expected Source SourceLocal for INTERNAL, got %q", inRainbows.Source)
	}

	if okComputer.URI != "NAS/Radiohead/OK Computer" {
		t.Errorf("Expected NAS URI, got %q", okComputer.URI)
	}
	if okComputer.AlbumArt == "" {
		t.Errorf("Expected non-empty AlbumArt on NAS album")
	}
	if okComputer.Source != SourceNAS {
		t.Errorf("Expected Source SourceNAS, got %q", okComputer.Source)
	}

	// Negative case: the USB album by 'Other Artist' must not leak in.
	for _, a := range resp.Albums {
		if a.Artist != "Radiohead" {
			t.Errorf("Unexpected non-Radiohead album in response: %+v", a)
		}
	}
}

// --- GetAlbumTracks Tests ---

func TestService_GetAlbumTracks_EmptyAlbum(t *testing.T) {
	mockMPD := &MockMPDClient{}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{
		Album: "",
	})

	if resp.Error == "" {
		t.Error("Expected error for empty album name")
	}
}

func TestService_GetAlbumTracks_WithTracks(t *testing.T) {
	mockMPD := &MockMPDClient{
		FindAlbumTracksResp: map[string][]map[string]string{
			"Test Album\x00Test Artist": {
				{
					"file":   "INTERNAL/Album/01-Track1.flac",
					"Title":  "Track One",
					"Artist": "Test Artist",
					"Album":  "Test Album",
					"Track":  "1",
					"Time":   "240",
				},
				{
					"file":   "INTERNAL/Album/02-Track2.flac",
					"Title":  "Track Two",
					"Artist": "Test Artist",
					"Album":  "Test Album",
					"Track":  "2",
					"Time":   "180",
				},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{
		Album:       "Test Album",
		AlbumArtist: "Test Artist",
	})

	if resp.Error != "" {
		t.Errorf("Unexpected error: %s", resp.Error)
	}
	if len(resp.Tracks) != 2 {
		t.Errorf("Expected 2 tracks, got %d", len(resp.Tracks))
	}
	if resp.TotalDuration != 420 {
		t.Errorf("Expected total duration 420, got %d", resp.TotalDuration)
	}

	// Verify tracks are sorted by track number
	if len(resp.Tracks) >= 2 {
		if resp.Tracks[0].TrackNumber != 1 {
			t.Errorf("First track should be track 1, got %d", resp.Tracks[0].TrackNumber)
		}
		if resp.Tracks[1].TrackNumber != 2 {
			t.Errorf("Second track should be track 2, got %d", resp.Tracks[1].TrackNumber)
		}
	}
}

// TestService_GetAlbumTracks_ExcludesResourceForkFiles pins the DATA-04
// behavior that GetAlbumTracks must never return macOS resource-fork
// sidecar entries. Written against the pre-refactor inline
// strings.HasPrefix(base, "._") check (it already passes before Plan 01-01
// Task 2's refactor to musicfile.IsResourceFork) so a regression during the
// refactor is caught immediately.
func TestService_GetAlbumTracks_ExcludesResourceForkFiles(t *testing.T) {
	mockMPD := &MockMPDClient{
		FindAlbumTracksResp: map[string][]map[string]string{
			"Ghost Album\x00Ghost Artist": {
				{
					"file":   "INTERNAL/Album/01-Track1.flac",
					"Title":  "Track One",
					"Artist": "Ghost Artist",
					"Album":  "Ghost Album",
					"Track":  "1",
					"Time":   "240",
				},
				{
					"file":   "INTERNAL/Album/._01-Track1.flac",
					"Title":  "Track One",
					"Artist": "Ghost Artist",
					"Album":  "Ghost Album",
					"Track":  "1",
					"Time":   "240",
				},
				{
					"file":   "INTERNAL/Album/02-Track2.flac",
					"Title":  "Track Two",
					"Artist": "Ghost Artist",
					"Album":  "Ghost Album",
					"Track":  "2",
					"Time":   "180",
				},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{
		Album:       "Ghost Album",
		AlbumArtist: "Ghost Artist",
	})

	if resp.Error != "" {
		t.Errorf("Unexpected error: %s", resp.Error)
	}
	if len(resp.Tracks) != 2 {
		t.Fatalf("Expected 2 tracks (resource-fork entry excluded), got %d", len(resp.Tracks))
	}
	for _, track := range resp.Tracks {
		if strings.Contains(track.URI, "._01") {
			t.Errorf("Resource-fork entry leaked into Tracks: %+v", track)
		}
	}
}

// TestService_GetAlbumTracks_SortsByDiscThenTrackNumber pins 03-03-PLAN.md
// Task 3: a grouped multi-disc album's combined SearchByBase result (discs
// interleaved in MPD's raw response order) must sort disc 1's tracks (by
// track number) before disc 2's, never interleaved -- proven here with a
// higher-track-number disc-1 track that must still sort ahead of a
// lower-track-number disc-2 track.
func TestService_GetAlbumTracks_SortsByDiscThenTrackNumber(t *testing.T) {
	mockMPD := &MockMPDClient{
		SearchByBaseResp: map[string][]map[string]string{
			"USB/Mahler The Symphonies": {
				// Raw MPD order interleaves discs; disc 2 track 1 appears
				// before disc 1 track 2 in the raw response.
				{
					"file": "USB/Mahler The Symphonies/CD 02/01.flac", "Title": "Symphony No. 2 - I",
					"Artist": "Gustav Mahler", "Album": "Mahler: The Symphonies", "Track": "1", "Disc": "2", "Time": "300",
				},
				{
					"file": "USB/Mahler The Symphonies/CD 01/02.flac", "Title": "Symphony No. 1 - II",
					"Artist": "Gustav Mahler", "Album": "Mahler: The Symphonies", "Track": "2", "Disc": "1", "Time": "280",
				},
				{
					"file": "USB/Mahler The Symphonies/CD 01/01.flac", "Title": "Symphony No. 1 - I",
					"Artist": "Gustav Mahler", "Album": "Mahler: The Symphonies", "Track": "1", "Disc": "1", "Time": "260",
				},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{
		Album: "Mahler: The Symphonies",
		URI:   "USB/Mahler The Symphonies",
	})

	if resp.Error != "" {
		t.Fatalf("Unexpected error: %s", resp.Error)
	}
	if len(resp.Tracks) != 3 {
		t.Fatalf("Expected 3 tracks, got %d", len(resp.Tracks))
	}

	// Expect: disc 1 track 1, disc 1 track 2, disc 2 track 1 -- never
	// interleaved despite disc 2's track appearing first in raw MPD order.
	wantOrder := []struct {
		disc  int
		track int
	}{
		{1, 1},
		{1, 2},
		{2, 1},
	}
	for i, want := range wantOrder {
		got := resp.Tracks[i]
		if got.Disc != want.disc || got.TrackNumber != want.track {
			t.Errorf("Tracks[%d] = (Disc:%d, TrackNumber:%d), want (Disc:%d, TrackNumber:%d): %+v",
				i, got.Disc, got.TrackNumber, want.disc, want.track, resp.Tracks)
		}
	}
}

// TestService_GetAlbumTracks_DiscZeroWhenAbsent verifies Track.Disc follows
// the existing TrackNumber/Duration convention: absent or empty MPD "Disc"
// tag produces the zero value (0), which `omitempty` drops from JSON.
func TestService_GetAlbumTracks_DiscZeroWhenAbsent(t *testing.T) {
	mockMPD := &MockMPDClient{
		FindAlbumTracksResp: map[string][]map[string]string{
			"No Disc Tag Album\x00Some Artist": {
				{
					"file": "INTERNAL/Album/01.flac", "Title": "Track One",
					"Artist": "Some Artist", "Album": "No Disc Tag Album", "Track": "1", "Time": "200",
					// No "Disc" key at all.
				},
			},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{
		Album:       "No Disc Tag Album",
		AlbumArtist: "Some Artist",
	})

	if resp.Error != "" {
		t.Fatalf("Unexpected error: %s", resp.Error)
	}
	if len(resp.Tracks) != 1 {
		t.Fatalf("Expected 1 track, got %d", len(resp.Tracks))
	}
	if resp.Tracks[0].Disc != 0 {
		t.Errorf("Disc = %d, want 0 (absent Disc tag)", resp.Tracks[0].Disc)
	}
}

// --- GetRadioStations Tests ---

func TestService_GetRadioStations_Empty(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListPlaylistsResponse: []string{},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetRadioStations(GetRadioRequest{})

	if len(resp.Stations) != 0 {
		t.Errorf("Expected 0 stations, got %d", len(resp.Stations))
	}
}

func TestService_GetRadioStations_FromPlaylists(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListPlaylistsResponse: []string{"Radio/BBC Radio 1", "Radio/Jazz FM", "My Playlist"},
		ListPlaylistInfoResp: map[string][]map[string]string{
			"Radio/BBC Radio 1": {{"file": "http://stream.bbc.co.uk/radio1"}},
			"Radio/Jazz FM":     {{"file": "http://jazz.fm/stream"}},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetRadioStations(GetRadioRequest{})

	// Should only include playlists with "Radio/" prefix
	if len(resp.Stations) != 2 {
		t.Errorf("Expected 2 radio stations, got %d", len(resp.Stations))
	}
}

func TestService_GetRadioStations_WithQuery(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListPlaylistsResponse: []string{"Radio/BBC Radio 1", "Radio/Jazz FM", "Radio/BBC Radio 2"},
		ListPlaylistInfoResp: map[string][]map[string]string{
			"Radio/BBC Radio 1": {{"file": "http://stream.bbc.co.uk/radio1"}},
			"Radio/Jazz FM":     {{"file": "http://jazz.fm/stream"}},
			"Radio/BBC Radio 2": {{"file": "http://stream.bbc.co.uk/radio2"}},
		},
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetRadioStations(GetRadioRequest{
		Query: "bbc",
	})

	if len(resp.Stations) != 2 {
		t.Errorf("Expected 2 BBC stations, got %d", len(resp.Stations))
	}
}

// --- Error Handling Tests ---

func TestService_GetAlbums_MPDError(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsError: fmt.Errorf("MPD connection failed"),
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetAlbums(GetAlbumsRequest{
		Scope: ScopeAll,
	})

	// Should return empty response on error, not panic
	if resp.Albums == nil {
		t.Error("Albums should not be nil on error")
	}
}

func TestService_GetArtists_MPDError(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListArtistsError: fmt.Errorf("MPD connection failed"),
	}

	service := NewService(mockMPD, &MockPathClassifier{})

	resp := service.GetArtists(GetArtistsRequest{})

	// Should return empty response on error
	if resp.Artists == nil {
		t.Error("Artists should not be nil on error")
	}
}
