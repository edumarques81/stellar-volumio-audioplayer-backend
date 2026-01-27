package library

import (
	"fmt"
	"testing"
)

// MockMPDClient implements the MPDClient interface for testing.
type MockMPDClient struct {
	// Album queries
	ListAlbumsResponse      []AlbumInfo
	ListAlbumsError         error
	ListAlbumsInBaseResp    map[string][]AlbumInfo
	ListAlbumsInBaseError   error
	GetAlbumDetailsResp     map[string][]AlbumDetails
	GetAlbumDetailsError    error

	// Artist queries
	ListArtistsResponse     []string
	ListArtistsError        error
	FindAlbumsByArtistResp  map[string][]AlbumInfo
	FindAlbumsByArtistError error

	// Track queries
	FindAlbumTracksResp     map[string][]map[string]string
	FindAlbumTracksError    error

	// Playlist/radio queries
	ListPlaylistsResponse   []string
	ListPlaylistsError      error
	ListPlaylistInfoResp    map[string][]map[string]string
	ListPlaylistInfoError   error
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

// --- GetArtistAlbums Tests ---

func TestService_GetArtistAlbums_Empty(t *testing.T) {
	mockMPD := &MockMPDClient{
		FindAlbumsByArtistResp: map[string][]AlbumInfo{
			"Unknown Artist": {},
		},
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
		FindAlbumsByArtistResp: map[string][]AlbumInfo{
			"Test Artist": {
				{Album: "Album 1", AlbumArtist: "Test Artist"},
				{Album: "Album 2", AlbumArtist: "Test Artist"},
			},
		},
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"": {
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
